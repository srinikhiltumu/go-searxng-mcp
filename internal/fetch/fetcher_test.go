package fetch

import (
	"net"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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
