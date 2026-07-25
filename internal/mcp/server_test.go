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

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	mockSearxng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/config" {
			w.Write([]byte(`{"engines":[
				{"name":"bing","enabled":true},
				{"name":"wikipedia","enabled":true}
			]}`))
			return
		}
		w.Write([]byte(`{
			"query": "golang",
			"results": [
				{
					"title": "Go Programming Language",
					"url": "https://go.dev",
					"content": "Build fast, reliable, and efficient software at scale.",
					"engine": "bing"
				}
			]
		}`))
	}))
	t.Cleanup(mockSearxng.Close)

	cfg := &config.Config{
		SearxngURL:         mockSearxng.URL,
		SearxngTimeout:     5 * time.Second,
		SearchDefaultLimit: 5,
		FetchMaxChars:      6000,
		UserAgent:          "test-agent",
	}

	searxClient := searxng.NewClient(cfg, mockSearxng.Client())
	pageFetcher := fetch.NewFetcherWithClient(cfg, &http.Client{Timeout: 5 * time.Second})
	server := NewServer(cfg,
		NewSearxngSearcher(searxClient),
		NewFetchPageReader(pageFetcher),
		NewSearxngHealthChecker(searxClient),
	)
	return server, mockSearxng
}

func TestServer_handleWebSearch(t *testing.T) {
	server, _ := newTestServer(t)

	searchReq := mcp.CallToolRequest{}
	searchReq.Params.Name = "web_search"
	searchReq.Params.Arguments = map[string]any{
		"query": "golang",
		"limit": 1,
	}

	res, err := server.handleWebSearch(context.Background(), searchReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	var results []searxng.SearchResult
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &results); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	if len(results) != 1 || results[0].Title != "Go Programming Language" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestServer_handleWebSearch_MissingQuery(t *testing.T) {
	server, _ := newTestServer(t)

	req := mcp.CallToolRequest{}
	req.Params.Name = "web_search"
	req.Params.Arguments = map[string]any{}

	res, err := server.handleWebSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for missing query")
	}
}

func TestServer_handleHealth(t *testing.T) {
	server, _ := newTestServer(t)

	healthReq := mcp.CallToolRequest{}
	healthReq.Params.Name = "health"

	res, err := server.handleHealth(context.Background(), healthReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status searxng.HealthStatus
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &status); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected 'ok', got %q", status.Status)
	}
	if status.EnginesConfigured != 2 {
		t.Errorf("expected 2 engines, got %d", status.EnginesConfigured)
	}
}

func TestServer_handleWebRead(t *testing.T) {
	server, _ := newTestServer(t)

	htmlContent := `<!DOCTYPE html><html><head><title>Test Page</title></head>
	<body><main><h1>Hello</h1><p>This is test content for the page reader.</p></main></body></html>`

	mockPage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(htmlContent))
	}))
	t.Cleanup(mockPage.Close)

	req := mcp.CallToolRequest{}
	req.Params.Name = "web_read"
	req.Params.Arguments = map[string]any{
		"url": mockPage.URL,
	}

	res, err := server.handleWebRead(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	var result fetch.ReadResult
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &result); err != nil {
		t.Fatalf("failed to parse read result: %v", err)
	}

	if result.Title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", result.Title)
	}
	if !contains(result.Content, "Hello") {
		t.Errorf("expected content to contain 'Hello', got %q", result.Content)
	}
}

func TestServer_handleWebRead_MissingURL(t *testing.T) {
	server, _ := newTestServer(t)

	req := mcp.CallToolRequest{}
	req.Params.Name = "web_read"
	req.Params.Arguments = map[string]any{}

	res, err := server.handleWebRead(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for missing url")
	}
}

func TestGetStringArg_Nil(t *testing.T) {
	req := mcp.CallToolRequest{}
	if _, ok := getStringArg(req, "foo"); ok {
		t.Error("expected ok=false for nil arguments")
	}
}

func TestGetStringArg_NonMap(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = "not-a-map"
	if _, ok := getStringArg(req, "foo"); ok {
		t.Error("expected ok=false for non-map arguments")
	}
}

func TestGetStringArg_NilValue(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": nil}
	if _, ok := getStringArg(req, "foo"); ok {
		t.Error("expected ok=false for nil value")
	}
}

func TestGetStringArg_NonString(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": 42}
	if _, ok := getStringArg(req, "foo"); ok {
		t.Error("expected ok=false for non-string value")
	}
}

func TestGetIntArg_Nil(t *testing.T) {
	req := mcp.CallToolRequest{}
	if _, ok := getIntArg(req, "foo"); ok {
		t.Error("expected ok=false for nil arguments")
	}
}

func TestGetIntArg_NonMap(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = "not-a-map"
	if _, ok := getIntArg(req, "foo"); ok {
		t.Error("expected ok=false for non-map arguments")
	}
}

func TestGetIntArg_Float64(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": float64(42)}
	v, ok := getIntArg(req, "foo")
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestGetIntArg_Int(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": 42}
	v, ok := getIntArg(req, "foo")
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestGetIntArg_Int64(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": int64(42)}
	v, ok := getIntArg(req, "foo")
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestGetIntArg_StringAtoi(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": "42"}
	v, ok := getIntArg(req, "foo")
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestGetIntArg_InvalidString(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": "not-a-number"}
	if _, ok := getIntArg(req, "foo"); ok {
		t.Error("expected ok=false for invalid string")
	}
}

func TestGetIntArg_NilValue(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"foo": nil}
	if _, ok := getIntArg(req, "foo"); ok {
		t.Error("expected ok=false for nil value")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
