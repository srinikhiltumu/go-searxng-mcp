package searxng

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCleanSnippet_TruncatesAt220CharBoundary(t *testing.T) {
	long := strings.Repeat("word ", 100) // 500 chars
	cleaned := cleanSnippet(long)
	if len(cleaned) > 223 { // 220 + possible "..."
		t.Errorf("expected snippet <= ~223 chars, got %d", len(cleaned))
	}
	if !strings.HasSuffix(cleaned, "...") {
		t.Errorf("expected truncated snippet to end with '...', got %q", cleaned)
	}
}

func TestClient_HealthViaConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/config" {
			w.Write([]byte(`{"engines":[
				{"name":"duckduckgo","enabled":true},
				{"name":"bing","enabled":false},
				{"name":"wikipedia","enabled":true}
			]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{SearxngURL: server.URL, SearxngTimeout: 5 * time.Second}
	client := NewClient(cfg, server.Client())

	status := client.Health(context.Background())
	if status.Status != "ok" {
		t.Fatalf("expected status ok, got %q (err=%s)", status.Status, status.Error)
	}
	if status.EnginesConfigured != 2 {
		t.Errorf("expected 2 enabled engines, got %d", status.EnginesConfigured)
	}
	if status.SearxngLatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %d", status.SearxngLatencyMs)
	}
}

func TestClient_HealthDegradedOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.Config{SearxngURL: server.URL, SearxngTimeout: 5 * time.Second}
	client := NewClient(cfg, server.Client())

	status := client.Health(context.Background())
	if status.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", status.Status)
	}
	if status.Error == "" {
		t.Errorf("expected an error message for degraded status")
	}
}

func TestClient_SearchRetriesOnServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rawSearxngResponse{
			Results: []rawResult{{Title: "Late Result", URL: "https://late.example", Content: "ok", Engine: "ddg"}},
		})
	}))
	defer server.Close()

	cfg := &config.Config{SearxngURL: server.URL, SearxngTimeout: 5 * time.Second, SearchDefaultLimit: 5, MaxSearchRetries: 3}
	client := NewClient(cfg, server.Client())

	results, err := client.Search(context.Background(), "test", 5, "", "")
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
	if len(results) != 1 || results[0].Title != "Late Result" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestClient_SearchResponseCapExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write valid JSON followed by extra trailing data to overflow a tiny cap.
		json.NewEncoder(w).Encode(rawSearxngResponse{Results: []rawResult{{Title: "x", URL: "https://x.example", Content: "y", Engine: "z"}}})
		w.Write([]byte(strings.Repeat(" ", 4*1024)))
	}))
	defer server.Close()

	cfg := &config.Config{SearxngURL: server.URL, SearxngTimeout: 5 * time.Second, SearchMaxBytes: 512}
	client := NewClient(cfg, server.Client())

	_, err := client.Search(context.Background(), "test", 5, "", "")
	if err == nil {
		t.Fatalf("expected a max-bytes-cap error, got nil")
	}
}
