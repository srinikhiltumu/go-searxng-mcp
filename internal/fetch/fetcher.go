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

// ReadResult represents extracted, readable page content.
type ReadResult struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	CharCount  int    `json:"char_count"`
	Truncated  bool   `json:"truncated"`
}

// Fetcher handles HTTP page retrieval, SSRF validation, and content extraction.
type Fetcher struct {
	cfg        *config.Config
	httpClient *http.Client
}

// NewFetcher creates a new Fetcher instance.
func NewFetcher(cfg *config.Config) *Fetcher {
	// Custom dialer to prevent SSRF DNS rebinding during connection phase
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, fmt.Errorf("dns resolution failed: %w", err)
			}

			for _, ip := range ips {
				if isPrivateOrLoopbackIP(ip) {
					return nil, fmt.Errorf("access to private/loopback IP (%s) blocked by SSRF guard", ip.String())
				}
			}

			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}

	return &Fetcher{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// ReadPage fetches a URL safely and extracts main readable markdown text.
func (f *Fetcher) ReadPage(ctx context.Context, targetURL string, maxChars int) (*ReadResult, error) {
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

	// Validate target hostname / IP
	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, fmt.Errorf("URL missing hostname")
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateOrLoopbackIP(ip) {
			return nil, fmt.Errorf("access to IP %s blocked by SSRF guard", hostname)
		}
	} else {
		// Resolve hostname and check IP ranges
		ips, err := net.LookupIP(hostname)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve host %s: %w", hostname, err)
		}
		for _, ip := range ips {
			if isPrivateOrLoopbackIP(ip) {
				return nil, fmt.Errorf("host %s resolves to private/loopback IP (%s)", hostname, ip.String())
			}
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

	// Limit response size to 2MB to prevent memory exhaustion
	limitReader := io.LimitReader(resp.Body, 2*1024*1024)
	doc, err := goquery.NewDocumentFromReader(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	title := strings.TrimSpace(doc.Find("title").Text())

	// Strip unwanted non-content elements
	doc.Find("script, style, noscript, svg, iframe, nav, footer, header, form, button, [role='navigation']").Remove()

	// Select primary content container if available, or fall back to body
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
		CharCount: len(truncatedText),
		Truncated: isTruncated,
	}, nil
}

// isPrivateOrLoopbackIP checks whether an IP is private, loopback, link-local, or multicast.
func isPrivateOrLoopbackIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	// Check IPv4 specific ranges if mapped
	if ip4 := ip.To4(); ip4 != nil {
		// 127.0.0.0/8 loopback
		if ip4[0] == 127 {
			return true
		}
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.0.0/16 link-local
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

// extractReadableText walks DOM nodes and collects paragraphs/headers cleanly formatted.
func extractReadableText(s *goquery.Selection) string {
	var sb strings.Builder

	s.Find("h1, h2, h3, h4, h5, h6, p, li, blockquote, pre, code").Each(func(_ int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if text == "" {
			return
		}

		tagName := goquery.NodeName(sel)
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
		case "pre", "code":
			if tagName == "pre" {
				sb.WriteString("\n```\n" + text + "\n```\n\n")
			}
		default: // p
			sb.WriteString(text + "\n\n")
		}
	})

	result := sb.String()
	// Fallback if no block elements were found
	if strings.TrimSpace(result) == "" {
		result = strings.TrimSpace(s.Text())
	}

	// Normalize excessive blank lines
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

// truncateText cuts text at sentence boundary when exceeding maxChars limit.
func truncateText(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}

	truncated := text[:maxChars]

	// Look for sentence end near boundary (. ! ?)
	lastDot := strings.LastIndexAny(truncated, ".!?\n")
	if lastDot > maxChars/2 {
		truncated = truncated[:lastDot+1]
	} else if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLenBoundary(maxChars) {
		truncated = truncated[:lastSpace] + "..."
	} else {
		truncated = truncated + "..."
	}

	return strings.TrimSpace(truncated), true
}

func maxLenBoundary(maxChars int) int {
	b := maxChars - 100
	if b < 0 {
		return 0
	}
	return b
}
