// Package fetch provides HTTP page retrieval with SSRF protection, HTML-to-markdown
// extraction, and sentence-boundary truncation for the web_read MCP tool.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
)

const (
	// maxFetchBodyBytes caps the response body for web_read to prevent memory exhaustion.
	maxFetchBodyBytes = 2 * 1024 * 1024 // 2 MiB
	// fetchHTTPTimeout is the overall HTTP client timeout for web_read.
	fetchHTTPTimeout = 15 * time.Second
	// dialTimeout is the TCP connection timeout.
	dialTimeout = 5 * time.Second
	// dialKeepAlive is the TCP keep-alive interval.
	dialKeepAlive = 30 * time.Second
	// tlsHandshakeTimeout bounds the TLS handshake duration.
	tlsHandshakeTimeout = 5 * time.Second
	// maxIdleConns is the connection pool size for web_read.
	maxIdleConns = 100
	// idleConnTimeout is how long idle connections stay in the pool.
	idleConnTimeout = 90 * time.Second
	// maxRedirects limits how many HTTP redirects the client will follow.
	maxRedirects = 5
	// truncateBoundaryWindow is the lookback window (from the end) for word-boundary truncation.
	truncateBoundaryWindow = 100
)

// ReadResult represents extracted, readable page content.
type ReadResult struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CharCount int    `json:"char_count"`
	Truncated bool   `json:"truncated"`
}

// Fetcher handles HTTP page retrieval, SSRF validation, and content extraction.
type Fetcher struct {
	cfg        *config.Config
	httpClient *http.Client
	sem        chan struct{} // bounds concurrent web_read fetches
	// skipHostValidation disables the pre-flight validateHost DNS/SSRF check.
	// It is set only when a caller injects a custom *http.Client (e.g. for
	// tests pointed at httptest), in which case the secure dial guard is
	// already replaced by the caller's transport. Production callers using
	// NewFetcher always retain full SSRF protection.
	skipHostValidation bool
}

// NewFetcher creates a new Fetcher instance.
func NewFetcher(cfg *config.Config) *Fetcher {
	return NewFetcherWithClient(cfg, nil)
}

// NewFetcherWithClient creates a new Fetcher with an optional injectable HTTP client.
// If httpClient is nil, a secure default client with SSRF dial-time protection is constructed.
// This constructor enables testing by allowing callers to inject a client pointed at httptest.
func NewFetcherWithClient(cfg *config.Config, httpClient *http.Client) *Fetcher {
	concurrency := cfg.WebReadConcurrency
	if concurrency < 1 {
		concurrency = 8
	}

	if httpClient != nil {
		return &Fetcher{
			cfg:                cfg,
			httpClient:         httpClient,
			sem:                make(chan struct{}, concurrency),
			skipHostValidation: true,
		}
	}

	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}

	transport := &http.Transport{
		DialContext:         makeSSRFDialContext(dialer),
		MaxIdleConns:        maxIdleConns,
		IdleConnTimeout:     idleConnTimeout,
		TLSHandshakeTimeout: tlsHandshakeTimeout,
	}

	return &Fetcher{
		cfg: cfg,
		httpClient: &http.Client{
			Transport:     transport,
			Timeout:       fetchHTTPTimeout,
			CheckRedirect: makeRedirectPolicy(maxRedirects),
		},
		sem: make(chan struct{}, concurrency),
	}
}

// makeSSRFDialContext returns a DialContext function that resolves the hostname,
// validates every resolved IP against private/loopback ranges, and then dials the
// validated IP directly (not the hostname) to prevent DNS-rebinding attacks.
func makeSSRFDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address format: %w", err)
		}

		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("dns resolution failed for host")
		}

		var validatedIP net.IP
		for _, ip := range ips {
			if isPrivateOrLoopbackIP(ip) {
				return nil, fmt.Errorf("access to private/loopback IP blocked by SSRF guard")
			}
			if validatedIP == nil {
				validatedIP = ip
			}
		}

		if validatedIP == nil {
			return nil, fmt.Errorf("no valid IP addresses resolved for host")
		}

		// Dial the validated IP directly, not the hostname, to prevent DNS rebinding.
		return dialer.DialContext(ctx, network, net.JoinHostPort(validatedIP.String(), port))
	}
}

// makeRedirectPolicy returns a CheckRedirect function that limits the number of
// redirects followed and blocks redirects to non-http(s) schemes.
func makeRedirectPolicy(maxRedir int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedir {
			return fmt.Errorf("stopped after %d redirects", maxRedir)
		}
		scheme := strings.ToLower(req.URL.Scheme)
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("redirect to non-http(s) scheme blocked")
		}
		return nil
	}
}

// ReadPage fetches a URL safely and extracts main readable markdown text.
func (f *Fetcher) ReadPage(ctx context.Context, targetURL string, maxChars int) (*ReadResult, error) {
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if maxChars <= 0 {
		maxChars = f.cfg.FetchMaxChars
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q; only http and https are allowed", scheme)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, fmt.Errorf("URL missing hostname")
	}

	if !f.skipHostValidation {
		if err := validateHost(hostname); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("page fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error response %d from target site", resp.StatusCode)
	}

	limitReader := io.LimitReader(resp.Body, maxFetchBodyBytes)
	doc, err := goquery.NewDocumentFromReader(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	title := strings.TrimSpace(doc.Find("title").Text())

	doc.Find("script, style, noscript, svg, iframe, nav, footer, header, form, button, [role='navigation']").Remove()

	var contentNode *goquery.Selection
	for _, selector := range []string{"main", "article", "#content", ".content", ".post-content", "#main"} {
		if sel := doc.Find(selector); sel.Length() > 0 {
			contentNode = sel.First()
			break
		}
	}
	if contentNode == nil {
		contentNode = doc.Find("body")
	}

	text := extractReadableText(contentNode)
	truncatedText, isTruncated := truncateText(text, maxChars)

	return &ReadResult{
		URL:       targetURL,
		Title:     title,
		Content:   truncatedText,
		CharCount: len([]rune(truncatedText)),
		Truncated: isTruncated,
	}, nil
}

// validateHost performs a pre-flight DNS resolution and SSRF check on the hostname.
// It does not leak resolved IPs or hostnames in error messages.
func validateHost(hostname string) error {
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateOrLoopbackIP(ip) {
			return fmt.Errorf("access to private/loopback IP blocked by SSRF guard")
		}
		return nil
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve host")
	}
	for _, ip := range ips {
		if isPrivateOrLoopbackIP(ip) {
			return fmt.Errorf("host resolves to private/loopback IP blocked by SSRF guard")
		}
	}
	return nil
}

// isPrivateOrLoopbackIP checks whether an IP is private, loopback, link-local, multicast,
// unspecified, CGNAT, or benchmarking — all of which should be blocked for SSRF protection.
func isPrivateOrLoopbackIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
		// 100.64.0.0/10 — CGNAT (RFC 6598), used by Tailscale and some clouds
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		// 198.18.0.0/15 — benchmarking (RFC 2544)
		if ip4[0] == 198 && ip4[1] == 18 {
			return true
		}
	}
	return false
}

// extractReadableText walks DOM nodes and collects paragraphs/headers cleanly formatted.
// Inline elements (links, inline code, emphasis) are rendered as markdown rather
// than flattened to bare text, so AI agents receive usable references and code spans.
func extractReadableText(s *goquery.Selection) string {
	var sb strings.Builder

	s.Find("h1, h2, h3, h4, h5, h6, p, li, blockquote, pre").Each(func(_ int, sel *goquery.Selection) {
		var text string
		tagName := goquery.NodeName(sel)
		if tagName == "pre" {
			text = strings.TrimSpace(sel.Text())
		} else {
			text = strings.TrimSpace(inlineMarkdown(sel))
		}
		if text == "" {
			return
		}

		switch tagName {
		case "h1":
			sb.WriteString("\n# " + text + "\n\n")
		case "h2":
			sb.WriteString("\n## " + text + "\n\n")
		case "h3":
			sb.WriteString("\n### " + text + "\n\n")
		case "h4", "h5", "h6":
			sb.WriteString("\n#### " + text + "\n\n")
		case "li":
			sb.WriteString("- " + text + "\n")
		case "blockquote":
			sb.WriteString("> " + text + "\n\n")
		case "pre":
			sb.WriteString("\n```\n" + text + "\n```\n\n")
		default: // p
			sb.WriteString(text + "\n\n")
		}
	})

	result := sb.String()
	if strings.TrimSpace(result) == "" {
		result = strings.TrimSpace(s.Text())
	}

	lines := strings.Split(result, "\n")
	var cleanedLines []string
	blankCount := 0

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" {
			blankCount++
			if blankCount <= 2 {
				cleanedLines = append(cleanedLines, "")
			}
		} else {
			blankCount = 0
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	return strings.TrimSpace(strings.Join(cleanedLines, "\n"))
}

// inlineMarkdown recursively renders the inline content of a node as markdown.
// It converts anchor tags to [text](href), inline <code> to `code`, and
// <strong>/<em> to **bold**/*italic*, while collapsing whitespace in text nodes.
func inlineMarkdown(s *goquery.Selection) string {
	if s.Length() == 0 {
		return ""
	}
	var sb strings.Builder
	s.Contents().Each(func(_ int, c *goquery.Selection) {
		switch goquery.NodeName(c) {
		case "#text":
			sb.WriteString(collapseInlineWhitespace(c.Text()))
		case "a":
			href, _ := c.Attr("href")
			inner := strings.TrimSpace(inlineMarkdown(c))
			if href == "" || inner == "" {
				sb.WriteString(inner)
			} else {
				sb.WriteString("[" + inner + "](" + href + ")")
			}
		case "code":
			inner := c.Text()
			if inner != "" {
				sb.WriteString("`" + inner + "`")
			}
		case "br":
			sb.WriteString("\n")
		case "strong", "b":
			inner := inlineMarkdown(c)
			if strings.TrimSpace(inner) != "" {
				sb.WriteString("**" + inner + "**")
			}
		case "em", "i":
			inner := inlineMarkdown(c)
			if strings.TrimSpace(inner) != "" {
				sb.WriteString("*" + inner + "*")
			}
		default:
			sb.WriteString(inlineMarkdown(c))
		}
	})
	return sb.String()
}

// collapseInlineWhitespace turns newlines/tabs into spaces and collapses runs of
// spaces into a single space, preserving readable inline text.
func collapseInlineWhitespace(s string) string {
	if !strings.ContainsAny(s, "\n\r\t ") {
		return s
	}
	var sb strings.Builder
	prevSpace := false
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t', ' ':
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
		default:
			sb.WriteRune(r)
			prevSpace = false
		}
	}
	return sb.String()
}

// truncateText cuts text at sentence boundary when exceeding maxChars limit.
// It operates on rune boundaries to avoid splitting multi-byte UTF-8 sequences.
func truncateText(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}

	// Convert to runes to avoid splitting multi-byte UTF-8 sequences.
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}

	truncatedRunes := runes[:maxChars]
	truncated := string(truncatedRunes)

	lastDot := strings.LastIndexAny(truncated, ".!?\n")
	if lastDot > maxChars/2 {
		truncated = truncated[:lastDot+1]
	} else if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxChars-truncateBoundaryWindow {
		truncated = truncated[:lastSpace] + "..."
	} else {
		truncated = truncated + "..."
	}

	return strings.TrimSpace(truncated), true
}
