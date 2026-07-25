// Package searxng provides a custom HTTP client for the SearXNG metasearch backend,
// including search, health probing, result normalization, and snippet sanitization.
package searxng

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
	"log/slog"
)

const (
	// maxSnippetLen caps each search result snippet at this many characters.
	maxSnippetLen = 220
	// maxSearchLimit is the hard upper bound on the number of results returned.
	maxSearchLimit = 10
	// defaultMaxBytes is the fallback response body cap when config is unset.
	defaultMaxBytes = 1 << 20 // 1 MiB
	// backoffBaseMs is the base delay for exponential backoff (attempt 1 = 100ms).
	backoffBaseMs = 100
	// backoffJitterMs is the maximum random jitter added to each backoff delay.
	backoffJitterMs = 50
)

// SearchResult represents a normalized web search result item.
type SearchResult struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Engine  string `json:"engine,omitempty"`
	Snippet string `json:"snippet"`
}

// HealthStatus represents the status returned by the health check tool.
type HealthStatus struct {
	Status            string `json:"status"`
	SearxngURL        string `json:"searxng_url"`
	SearxngLatencyMs  int64  `json:"searxng_latency_ms"`
	EnginesConfigured int    `json:"engines_configured,omitempty"`
	Error             string `json:"error,omitempty"`
}

// rawSearxngResponse represents the JSON response structure from SearXNG search API.
type rawSearxngResponse struct {
	Query               string            `json:"query"`
	NumberOfResults     int               `json:"number_of_results"`
	Results             []rawResult       `json:"results"`
	UnresponsiveEngines [][]string        `json:"unresponsive_engines"`
	Engines             []rawEngineStatus `json:"engines"`
}

type rawResult struct {
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	Content string   `json:"content"`
	Engine  string   `json:"engine"`
	Engines []string `json:"engines"`
}

type rawEngineStatus struct {
	Name string `json:"name"`
}

// rawConfigResponse represents the JSON returned by SearXNG's /config endpoint,
// used for lightweight health probing without triggering upstream search calls.
type rawConfigResponse struct {
	Engines []rawConfigEngine `json:"engines"`
}

type rawConfigEngine struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Typed errors for retry classification. Using typed errors instead of string
// matching makes the retry logic robust against error message rewording.
var (
	// errNetworkFailure wraps connection errors and is retryable.
	errNetworkFailure = errors.New("searxng request failed")
	// errServerError wraps HTTP 5xx responses and is retryable.
	errServerError = errors.New("searxng returned server error HTTP status")
)

// Client wraps HTTP calls to the SearXNG backend.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cfg        *config.Config
}

// NewClient returns a new SearXNG API client.
func NewClient(cfg *config.Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.SearxngTimeout,
		}
	}
	return &Client{
		baseURL:    cfg.SearxngURL,
		httpClient: httpClient,
		cfg:        cfg,
	}
}

// maxBytes returns the configured search response cap, applying a 1 MiB default
// when unset (e.g. in tests that construct a Config literal).
func (c *Client) maxBytes() int {
	if c.cfg.SearchMaxBytes <= 0 {
		return defaultMaxBytes
	}
	return c.cfg.SearchMaxBytes
}

// Search executes a query against SearXNG and returns normalized, deduplicated results.
// Transient failures (connection errors, 5xx) are retried up to MaxSearchRetries
// times with a small jittered backoff.
func (c *Client) Search(ctx context.Context, query string, limit int, category string, language string) ([]SearchResult, error) {
	if limit <= 0 {
		limit = c.cfg.SearchDefaultLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	searchURL, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("invalid searxng base url: %w", err)
	}

	q := searchURL.Query()
	q.Set("q", query)
	q.Set("format", "json")

	if category != "" {
		q.Set("categories", category)
	}
	if language != "" {
		q.Set("language", language)
	}
	searchURL.RawQuery = q.Encode()

	var raw rawSearxngResponse
	var lastErr error
	maxAttempts := c.cfg.MaxSearchRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, lastErr = c.doSearch(ctx, searchURL.String())
		if lastErr == nil {
			break
		}
		if !isRetryable(lastErr) || attempt == maxAttempts {
			break
		}
		backoff := time.Duration(backoffBaseMs*(1<<(attempt-1))) * time.Millisecond
		backoff += time.Duration(rand.Int64N(int64(backoffJitterMs * time.Millisecond)))
		slog.Warn("searxng search attempt failed, retrying",
			"attempt", attempt, "backoff", backoff.String(), "error", lastErr.Error())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}

	return normalizeAndDeduplicate(raw.Results, limit), nil
}

// doSearch performs a single HTTP search request against SearXNG, applying the
// configured response size cap to prevent runaway payloads.
func (c *Client) doSearch(ctx context.Context, searchURL string) (rawSearxngResponse, error) {
	var raw rawSearxngResponse

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return raw, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return raw, fmt.Errorf("%w: %v", errNetworkFailure, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return raw, fmt.Errorf("%w: HTTP %d", errServerError, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("searxng returned unexpected HTTP status: %d", resp.StatusCode)
	}

	maxBytes := c.maxBytes()
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return raw, fmt.Errorf("failed reading searxng response: %w", err)
	}
	if len(data) > maxBytes {
		return raw, fmt.Errorf("searxng response exceeded max bytes cap (%d)", maxBytes)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return raw, fmt.Errorf("failed to decode searxng response: %w", err)
	}

	return raw, nil
}

// isRetryable reports whether a search error is worth retrying (connection or 5xx).
// Uses errors.Is against typed sentinel errors rather than string matching.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNetworkFailure) || errors.Is(err, errServerError)
}

// Health checks SearXNG backend status via the lightweight /config endpoint and
// measures round-trip latency. Unlike a search query, /config does not fan out
// to upstream search providers, so it is cheaper and reports the real number of
// configured engines.
func (c *Client) Health(ctx context.Context) *HealthStatus {
	start := time.Now()
	status := &HealthStatus{SearxngURL: c.baseURL}

	configURL, err := url.Parse(c.baseURL + "/config")
	if err != nil {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("invalid base url: %v", err)
		return status
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL.String(), nil)
	if err != nil {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("failed to create health request: %v", err)
		return status
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	status.SearxngLatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		status.Status = "degraded"
		status.Error = "searxng backend unreachable"
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("health probe returned HTTP %d", resp.StatusCode)
		return status
	}

	maxBytes := c.maxBytes()
	cappedData, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("failed reading /config response: %v", err)
		return status
	}
	if len(cappedData) > maxBytes {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("/config response exceeded max bytes cap (%d)", maxBytes)
		return status
	}
	var cfgResp rawConfigResponse
	if err := json.Unmarshal(cappedData, &cfgResp); err != nil {
		status.Status = "degraded"
		status.Error = fmt.Sprintf("failed to decode /config response: %v", err)
		return status
	}

	enabledCount := 0
	for _, e := range cfgResp.Engines {
		if e.Enabled {
			enabledCount++
		}
	}
	status.Status = "ok"
	status.EnginesConfigured = enabledCount
	return status
}

// normalizeAndDeduplicate cleans, ranks, deduplicates by URL, and trims snippets.
func normalizeAndDeduplicate(rawResults []rawResult, limit int) []SearchResult {
	seenURLs := make(map[string]bool)
	var output []SearchResult

	for _, res := range rawResults {
		if len(output) >= limit {
			break
		}

		cleanURL := strings.TrimSpace(res.URL)
		if cleanURL == "" || seenURLs[cleanURL] {
			continue
		}
		seenURLs[cleanURL] = true

		engineName := res.Engine
		if engineName == "" && len(res.Engines) > 0 {
			engineName = res.Engines[0]
		}

		title := strings.TrimSpace(res.Title)
		snippet := cleanSnippet(res.Content)

		output = append(output, SearchResult{
			Rank:    len(output) + 1,
			Title:   title,
			URL:     cleanURL,
			Engine:  engineName,
			Snippet: snippet,
		})
	}

	return output
}

// cleanSnippet sanitizes raw HTML/spaces, strips non-printable control characters,
// and limits snippets to maxSnippetLen characters, truncating at a word boundary
// when the limit is exceeded. Operates on rune boundaries for UTF-8 safety.
func cleanSnippet(raw string) string {
	s := strings.TrimSpace(raw)

	// Strip non-printable control characters (except tab and newline which are handled below).
	s = stripControlChars(s)

	// Replace linebreaks with spaces.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	// Collapse whitespace using Fields for O(n) performance.
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxSnippetLen {
		return s
	}

	// Truncate at rune-safe word boundary.
	runes := []rune(s)
	if len(runes) <= maxSnippetLen {
		return s
	}
	truncated := string(runes[:maxSnippetLen])
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxSnippetLen/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}

// stripControlChars removes non-printable ASCII control characters (0x00-0x08,
// 0x0B, 0x0C, 0x0E-0x1F) and delete (0x7F) from the string. Newline (0x0A) and
// carriage return (0x0D) and tab (0x09) are preserved for later whitespace normalization.
func stripControlChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0x7F) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
