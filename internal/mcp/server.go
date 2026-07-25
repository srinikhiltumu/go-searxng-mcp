// Package mcp implements the Model Context Protocol server with three tools:
// web_search, web_read, and health, using interface-based dependency injection.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
)

// Searcher is the port for web search operations, enabling mock substitution in tests.
type Searcher interface {
	Search(ctx context.Context, query string, limit int, category string, language string) ([]searchResult, error)
}

// PageReader is the port for page fetching and content extraction.
type PageReader interface {
	ReadPage(ctx context.Context, targetURL string, maxChars int) (*readResult, error)
}

// HealthChecker is the port for backend health probing.
type HealthChecker interface {
	Health(ctx context.Context) *healthStatus
}

// searchResult is the DTO returned by Searcher, decoupling the MCP layer from
// the concrete searxng.SearchResult type.
type searchResult struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Engine  string `json:"engine,omitempty"`
	Snippet string `json:"snippet"`
}

// readResult is the DTO returned by PageReader.
type readResult struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CharCount int    `json:"char_count"`
	Truncated bool   `json:"truncated"`
}

// healthStatus is the DTO returned by HealthChecker.
type healthStatus struct {
	Status            string `json:"status"`
	SearxngURL        string `json:"searxng_url"`
	SearxngLatencyMs  int64  `json:"searxng_latency_ms"`
	EnginesConfigured int    `json:"engines_configured,omitempty"`
	Error             string `json:"error,omitempty"`
}

// Server encapsulates the MCP server setup and dependencies.
type Server struct {
	cfg         *config.Config
	searcher    Searcher
	pageReader  PageReader
	healthCheck HealthChecker
	mcpServer   *mcpserver.MCPServer
}

// NewServer initializes the MCP server, tools, and handlers.
func NewServer(cfg *config.Config, searcher Searcher, pageReader PageReader, healthCheck HealthChecker) *Server {
	s := &Server{
		cfg:         cfg,
		searcher:    searcher,
		pageReader:  pageReader,
		healthCheck: healthCheck,
		mcpServer: mcpserver.NewMCPServer(
			"SearXNG Web Search",
			"1.0.0",
		),
	}

	s.registerTools()
	return s
}

// MCPServer returns the underlying mark3labs MCPServer instance.
func (s *Server) MCPServer() *mcpserver.MCPServer {
	return s.mcpServer
}

func (s *Server) registerTools() {
	searchTool := mcp.NewTool("web_search",
		mcp.WithDescription("Search the web via your SearXNG instance and return compact, ranked results."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The search query string"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (1-10, default 5)"),
		),
		mcp.WithString("category",
			mcp.Description("SearXNG search category (e.g., 'general', 'news', 'science', 'it')"),
		),
		mcp.WithString("language",
			mcp.Description("Search language code (e.g., 'en', 'es', 'de')"),
		),
	)
	s.mcpServer.AddTool(searchTool, s.handleWebSearch)

	readTool := mcp.NewTool("web_read",
		mcp.WithDescription("Fetch and extract readable content from a given URL."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The target HTTP or HTTPS URL to read"),
		),
		mcp.WithNumber("max_chars",
			mcp.Description("Maximum number of characters to extract (default 6000)"),
		),
	)
	s.mcpServer.AddTool(readTool, s.handleWebRead)

	healthTool := mcp.NewTool("health",
		mcp.WithDescription("Check MCP server and SearXNG backend status."),
	)
	s.mcpServer.AddTool(healthTool, s.handleHealth)
}

// handleWebSearch processes the web_search MCP tool call.
func (s *Server) handleWebSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := getStringArg(req, "query")
	if !ok || query == "" {
		return mcp.NewToolResultError("Missing required string parameter 'query'"), nil
	}

	limit := s.cfg.SearchDefaultLimit
	if limitVal, ok := getIntArg(req, "limit"); ok && limitVal > 0 {
		limit = limitVal
	}

	category, _ := getStringArg(req, "category")
	language, _ := getStringArg(req, "language")

	results, err := s.searcher.Search(ctx, query, limit, category, language)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("SearXNG search failed: %v", err)), nil
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format search results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleWebRead processes the web_read MCP tool call.
func (s *Server) handleWebRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	targetURL, ok := getStringArg(req, "url")
	if !ok || targetURL == "" {
		return mcp.NewToolResultError("Missing required string parameter 'url'"), nil
	}

	maxChars := s.cfg.FetchMaxChars
	if maxVal, ok := getIntArg(req, "max_chars"); ok && maxVal > 0 {
		maxChars = maxVal
	}

	result, err := s.pageReader.ReadPage(ctx, targetURL, maxChars)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read URL: %v", err)), nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format read result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleHealth processes the health MCP tool call.
func (s *Server) handleHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status := s.healthCheck.Health(ctx)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format health response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// getStringArg safely extracts a string argument from an MCP request.
func getStringArg(req mcp.CallToolRequest, name string) (string, bool) {
	if req.Params.Arguments == nil {
		return "", false
	}
	argsMap, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return "", false
	}
	val, ok := argsMap[name]
	if !ok || val == nil {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// getIntArg safely extracts an integer argument from an MCP request.
func getIntArg(req mcp.CallToolRequest, name string) (int, bool) {
	if req.Params.Arguments == nil {
		return 0, false
	}
	argsMap, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return 0, false
	}
	val, ok := argsMap[name]
	if !ok || val == nil {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i, true
		}
	}
	return 0, false
}
