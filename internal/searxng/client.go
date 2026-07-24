package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
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
	Status             string `json:"status"`
	SearxngURL         string `json:"searxng_url"`
	SearxngLatencyMs   int64  `json:"searxng_latency_ms"`
	EnginesConfigured  int    `json:"engines_configured,omitempty"`
	Error              string `json:"error,omitempty"`
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

// Search executes a query against SearXNG and returns normalized, deduplicated results.
func (c *Client) Search(ctx context.Context, query string, limit int, category string, language string) ([]SearchResult, error) {
	if limit <= 0 {
		limit = c.cfg.SearchDefaultLimit
	}
	if limit > 10 {
		limit = 10
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned unexpected HTTP status: %d", resp.StatusCode)
	}

	var raw rawSearxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode searxng response: %w", err)
	}

	return normalizeAndDeduplicate(raw.Results, limit), nil
}

// Health checks SearXNG backend status and measures round-trip latency.
func (c *Client) Health(ctx context.Context) *HealthStatus {
	start := time.Now()
	results, err := c.Search(ctx, "test health check", 1, "", "")
	latency := time.Since(start).Milliseconds()

	status := &HealthStatus{
		SearxngURL:       c.baseURL,
		SearxngLatencyMs: latency,
	}

	if err != nil {
		status.Status = "degraded"
		status.Error = err.Error()
	} else {
		status.Status = "ok"
		if len(results) > 0 && results[0].Engine != "" {
			status.EnginesConfigured = 1
		}
	}

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

// cleanSnippet sanitizes raw HTML/spaces and limits snippets to ~200 characters.
func cleanSnippet(raw string) string {
	s := strings.TrimSpace(raw)
	// Replace linebreaks with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	const maxLen = 220
	if len(s) <= maxLen {
		return s
	}

	// Truncate at word boundary
	truncated := s[:maxLen]
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}
