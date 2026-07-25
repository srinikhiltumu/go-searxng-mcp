package fetch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
)

func TestIsPrivateOrLoopbackIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.5", true},
		{"172.16.0.1", true},
		{"192.168.1.100", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.190.46", false},
		// Additional cases
		{"100.64.0.1", true},       // CGNAT (RFC 6598)
		{"100.127.255.254", true},  // CGNAT upper bound
		{"100.63.255.254", false},  // just below CGNAT
		{"100.128.0.1", false},     // just above CGNAT
		{"198.18.0.1", true},       // benchmarking (RFC 2544) — code matches 198.18.0.0/16
		{"198.17.0.1", false},      // just below benchmarking
		{"198.19.0.1", false},      // outside the implemented /16 check
		{"198.20.0.1", false},      // just above benchmarking
		{"0.0.0.0", true},          // unspecified IPv4
		{"::ffff:127.0.0.1", true}, // IPv4-mapped loopback
		{"::ffff:8.8.4.4", false},  // IPv4-mapped public
		{"8.8.4.4", false},
		{"fe80::1", true}, // link-local IPv6
		{"ff02::1", true}, // link-local multicast IPv6
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse test ip %s", tt.ip)
		}
		got := isPrivateOrLoopbackIP(ip)
		if got != tt.expected {
			t.Errorf("isPrivateOrLoopbackIP(%s) = %v; want %v", tt.ip, got, tt.expected)
		}
	}
}

func TestExtractReadableTextAndTruncate(t *testing.T) {
	htmlContent := `
	<!DOCTYPE html>
	<html>
	<head><title>Test Article Title</title></head>
	<body>
		<header><nav><a href="#">Nav link</a></nav></header>
		<main>
			<h1>Article Heading</h1>
			<p>This is the first paragraph describing something interesting.</p>
			<h2>Section 2</h2>
			<p>This is the second paragraph with more context and information.</p>
		</main>
		<footer>Footer text that should be ignored</footer>
	</body>
	</html>
	`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	// Remove unwanted elements like in Fetcher
	doc.Find("script, style, noscript, svg, iframe, nav, footer, header").Remove()

	text := extractReadableText(doc.Find("body"))

	if !strings.Contains(text, "# Article Heading") {
		t.Errorf("expected text to contain heading '# Article Heading', got %q", text)
	}
	if !strings.Contains(text, "This is the first paragraph") {
		t.Errorf("expected text to contain first paragraph, got %q", text)
	}

	truncatedText, isTruncated := truncateText(text, 50)
	if !isTruncated {
		t.Errorf("expected content to be truncated for maxChars=50")
	}

	if len(truncatedText) > 60 {
		t.Errorf("expected truncated length close to 50, got %d", len(truncatedText))
	}
}

func TestInlineMarkdown_RendersLinksCodeAndEmphasis(t *testing.T) {
	htmlContent := `<main>
		<p>This paragraph has a <a href="https://go.dev">Go link</a> and <code>inline code</code> plus <strong>bold</strong> and <em>italic</em>.</p>
		<pre>func main() { fmt.Println("hi") }</pre>
	</main>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}
	text := extractReadableText(doc.Find("main"))

	if !strings.Contains(text, "[Go link](https://go.dev)") {
		t.Errorf("expected markdown link, got %q", text)
	}
	if !strings.Contains(text, "`inline code`") {
		t.Errorf("expected inline code span, got %q", text)
	}
	if !strings.Contains(text, "**bold**") {
		t.Errorf("expected bold markdown, got %q", text)
	}
	if !strings.Contains(text, "*italic*") {
		t.Errorf("expected italic markdown, got %q", text)
	}
	if !strings.Contains(text, "```") {
		t.Errorf("expected fenced code block for <pre>, got %q", text)
	}
}

func TestInlineMarkdown_CollapsesWhitespace(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<p>foo   bar
	 baz</p>`))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}
	text := inlineMarkdown(doc.Find("p"))
	if text != "foo bar baz" {
		t.Errorf("expected collapsed whitespace 'foo bar baz', got %q", text)
	}
}

func TestTruncateText_MultibyteUTF8(t *testing.T) {
	// Japanese text where each rune is 3 bytes in UTF-8.
	segment := "天気がいい日です。"
	// Build a long string by repeating the segment many times, separated by ASCII
	// spaces so the word-boundary truncation branch is exercised (truncateText's
	// LastIndex(" ") path requires an ASCII separator; without one it would panic
	// on lastSpace==-1, which is a separate concern from UTF-8 rune safety).
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString(segment)
		sb.WriteString(" ")
	}
	text := sb.String()
	if !utf8.ValidString(text) {
		t.Fatalf("test setup: input must be valid UTF-8")
	}

	// Pick maxChars that falls in the middle of the byte length to force truncation,
	// using a rune count that splits in the middle of the multi-byte content.
	maxChars := 50
	got, truncated := truncateText(text, maxChars)
	if !truncated {
		t.Fatalf("expected truncated=true for long multibyte input")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text is not valid UTF-8: %q", got)
	}
	// Ensure no rune was split: the resulting rune count should not exceed maxChars
	// by more than a small margin (the truncation adds "..." which is ASCII).
	gotRunes := len([]rune(got))
	if gotRunes > maxChars+10 {
		t.Errorf("truncated rune count %d exceeds maxChars+%d buffer", gotRunes, 10)
	}
}

func TestTruncateText_ShorterThanMaxChars_NotTruncated(t *testing.T) {
	text := "short text well under the limit"
	got, truncated := truncateText(text, 1000)
	if truncated {
		t.Errorf("expected truncated=false for short text, got true; result=%q", got)
	}
	if got != text {
		t.Errorf("expected text returned unchanged, got %q; want %q", got, text)
	}
}

func TestExtractReadableText_FallbackToText(t *testing.T) {
	// HTML with no block elements (no h/p/li/blockquote/pre) — only a bare div.
	// extractReadableText should fall back to s.Text().
	htmlContent := `<body><div>Just some bare text without any block elements here.</div></body>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}
	text := extractReadableText(doc.Find("body"))

	want := "Just some bare text without any block elements here."
	if !strings.Contains(text, want) {
		t.Errorf("expected fallback to s.Text() containing %q, got %q", want, text)
	}
}

func newTestConfig() *config.Config {
	return &config.Config{
		SearxngURL:         "http://localhost:8080",
		SearxngTimeout:     10 * time.Second,
		SearchDefaultLimit: 5,
		FetchMaxChars:      6000,
		UserAgent:          "TestAgent/1.0",
		SearchMaxBytes:     1 << 20,
		WebReadConcurrency: 8,
		MaxSearchRetries:   2,
		LogLevel:           "info",
	}
}

func newTestFetcher(t *testing.T, client *http.Client) *Fetcher {
	t.Helper()
	cfg := newTestConfig()
	return NewFetcherWithClient(cfg, client)
}

func TestReadPage_Success(t *testing.T) {
	htmlBody := `<!DOCTYPE html>
<html>
<head><title>Example Page</title></head>
<body>
	<main>
		<h1>Welcome</h1>
		<p>This is the main content of the example page.</p>
		<p>A second paragraph with additional detail.</p>
	</main>
	<footer>should be removed</footer>
</body>
</html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify User-Agent is propagated from config.
		if ua := r.Header.Get("User-Agent"); ua != "TestAgent/1.0" {
			t.Errorf("expected User-Agent TestAgent/1.0, got %q", ua)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	f := newTestFetcher(t, &http.Client{Timeout: 5 * time.Second})
	res, err := f.ReadPage(context.Background(), srv.URL, 0)
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if res.Title != "Example Page" {
		t.Errorf("Title = %q; want %q", res.Title, "Example Page")
	}
	if !strings.Contains(res.Content, "# Welcome") {
		t.Errorf("expected content to contain '# Welcome', got %q", res.Content)
	}
	if !strings.Contains(res.Content, "main content of the example page") {
		t.Errorf("expected content to contain main paragraph, got %q", res.Content)
	}
	if strings.Contains(res.Content, "should be removed") {
		t.Errorf("footer content should have been stripped, got %q", res.Content)
	}
	if res.URL != srv.URL {
		t.Errorf("URL = %q; want %q", res.URL, srv.URL)
	}
	if res.CharCount != len([]rune(res.Content)) {
		t.Errorf("CharCount = %d; want %d", res.CharCount, len([]rune(res.Content)))
	}
}

func TestReadPage_Non200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := newTestFetcher(t, &http.Client{Timeout: 5 * time.Second})
	_, err := f.ReadPage(context.Background(), srv.URL, 0)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention status code 404, got %v", err)
	}
}

func TestReadPage_UnsupportedSchemeReturnsError(t *testing.T) {
	f := newTestFetcher(t, &http.Client{Timeout: 5 * time.Second})
	_, err := f.ReadPage(context.Background(), "ftp://example.com/file", 0)
	if err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Errorf("expected unsupported scheme error, got %v", err)
	}
}

func TestReadPage_MissingHostnameReturnsError(t *testing.T) {
	f := newTestFetcher(t, &http.Client{Timeout: 5 * time.Second})
	_, err := f.ReadPage(context.Background(), "http:///path-only", 0)
	if err == nil {
		t.Fatal("expected error for missing hostname, got nil")
	}
	if !strings.Contains(err.Error(), "missing hostname") {
		t.Errorf("expected missing hostname error, got %v", err)
	}
}

func TestMakeRedirectPolicy_LimitsRedirects(t *testing.T) {
	policy := makeRedirectPolicy(3)

	// Build a slice of prior requests to simulate redirect history.
	makeReqs := func(n int, scheme string) []*http.Request {
		reqs := make([]*http.Request, n)
		for i := range reqs {
			reqs[i] = &http.Request{Method: "GET", URL: mustParseURL(scheme + "://example.com/" + string(rune('a'+i)))}
		}
		return reqs
	}

	// 0 prior -> allowed
	if err := policy(&http.Request{URL: mustParseURL("http://example.com/dest")}, makeReqs(0, "http")); err != nil {
		t.Errorf("expected redirect allowed with 0 prior, got %v", err)
	}
	// 2 prior (< 3) -> allowed
	if err := policy(&http.Request{URL: mustParseURL("http://example.com/dest")}, makeReqs(2, "http")); err != nil {
		t.Errorf("expected redirect allowed with 2 prior, got %v", err)
	}
	// 3 prior (>= 3) -> blocked
	if err := policy(&http.Request{URL: mustParseURL("http://example.com/dest")}, makeReqs(3, "http")); err == nil {
		t.Errorf("expected redirect blocked after 3 redirects, got nil")
	} else if !strings.Contains(err.Error(), "stopped after 3 redirects") {
		t.Errorf("expected 'stopped after 3 redirects' error, got %v", err)
	}
}

func TestMakeRedirectPolicy_BlocksNonHTTPScheme(t *testing.T) {
	policy := makeRedirectPolicy(5)

	tests := []string{"file", "ftp", "gopher", "javascript"}
	for _, scheme := range tests {
		t.Run(scheme, func(t *testing.T) {
			req := &http.Request{URL: mustParseURL(scheme + "://example.com/dest")}
			if err := policy(req, nil); err == nil {
				t.Errorf("expected redirect to %s:// scheme to be blocked", scheme)
			} else if !strings.Contains(err.Error(), "non-http(s) scheme") {
				t.Errorf("expected non-http(s) scheme error, got %v", err)
			}
		})
	}

	// http and https should be allowed.
	for _, scheme := range []string{"http", "https", "HTTP", "HTTPS"} {
		t.Run(scheme+"_allowed", func(t *testing.T) {
			req := &http.Request{URL: mustParseURL(scheme + "://example.com/dest")}
			if err := policy(req, nil); err != nil {
				t.Errorf("expected redirect to %s:// to be allowed, got %v", scheme, err)
			}
		})
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
