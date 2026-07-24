package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/fetch"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/searxng"
)

func TestServer_Handlers(t *testing.T) {
	mockSearxng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"query": "golang",
			"results": [
				{
					"title": "Go Programming Language",
					"url": "https://go.dev",
					"content": "Build fast, reliable, and efficient software at scale.",
					"engine": "google"
				}
			]
		}`))
	}))
	defer mockSearxng.Close()

	cfg := &config.Config{
		SearxngURL:         mockSearxng.URL,
		SearxngTimeout:     5 * time.Second,
		SearchDefaultLimit: 5,
		FetchMaxChars:      6000,
		UserAgent:          "test-agent",
	}

	searxClient := searxng.NewClient(cfg, mockSearxng.Client())
	pageFetcher := fetch.NewFetcher(cfg)
	server := NewServer(cfg, searxClient, pageFetcher)

	// Test web_search tool execution
	searchReq := mcp.CallToolRequest{}
	searchReq.Params.Name = "web_search"
	searchReq.Params.Arguments = map[string]interface{}{
		"query": "golang",
		"limit": 1,
	}

	res, err := server.handleWebSearch(context.Background(), searchReq)
	if err != nil {
		t.Fatalf("unexpected handleWebSearch error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected success result, got error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	var results []searxng.SearchResult
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &results); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if len(results) != 1 || results[0].Title != "Go Programming Language" {
		t.Errorf("unexpected search results output: %+v", results)
	}

	// Test health tool execution
	healthReq := mcp.CallToolRequest{}
	healthReq.Params.Name = "health"

	healthRes, err := server.handleHealth(context.Background(), healthReq)
	if err != nil {
		t.Fatalf("unexpected handleHealth error: %v", err)
	}

	var status searxng.HealthStatus
	if err := json.Unmarshal([]byte(healthRes.Content[0].(mcp.TextContent).Text), &status); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected health status 'ok', got %q", status.Status)
	}
}
