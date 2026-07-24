package searxng

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
)

func TestClient_Search(t *testing.T) {
	mockResponse := rawSearxngResponse{
		Query:           "golang mcp",
		NumberOfResults: 2,
		Results: []rawResult{
			{
				Title:   "Go MCP SDK",
				URL:     "https://example.com/mcp-go",
				Content: "Learn how to build Model Context Protocol servers in Go with clear examples.",
				Engine:  "duckduckgo",
			},
			{
				Title:   "Duplicate Go MCP SDK",
				URL:     "https://example.com/mcp-go", // duplicate URL
				Content: "Duplicate content",
				Engine:  "bing",
			},
			{
				Title:   "SearXNG Documentation",
				URL:     "https://searxng.github.io/searxng",
				Content: "SearXNG privacy-respecting metasearch engine user and developer guide.",
				Engine:  "google",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("expected format=json, got %s", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	cfg := &config.Config{
		SearxngURL:         server.URL,
		SearxngTimeout:     5 * time.Second,
		SearchDefaultLimit: 5,
		UserAgent:          "test-agent",
	}

	client := NewClient(cfg, server.Client())
	results, err := client.Search(context.Background(), "golang mcp", 5, "general", "en")
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 deduplicated results, got %d", len(results))
	}

	if results[0].Rank != 1 || results[0].Title != "Go MCP SDK" || results[0].Engine != "duckduckgo" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[1].Rank != 2 || results[1].URL != "https://searxng.github.io/searxng" {
		t.Errorf("unexpected second result: %+v", results[1])
	}
}

func TestCleanSnippet(t *testing.T) {
	raw := "This is a long snippet\nwith line breaks   and   extra spaces that should be   cleaned up properly."
	cleaned := cleanSnippet(raw)
	expected := "This is a long snippet with line breaks and extra spaces that should be cleaned up properly."
	if cleaned != expected {
		t.Errorf("expected clean snippet %q, got %q", expected, cleaned)
	}
}
