package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/fetch"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/searxng"
)

// Server encapsulates the MCP server setup and dependencies.
type Server struct {
	cfg         *config.Config
	searxClient *searxng.Client
	pageFetcher *fetch.Fetcher
	mcpServer   *mcpserver.MCPServer
}

// NewServer initializes the MCP server, tools, and handlers.
func NewServer(cfg *config.Config, searxClient *searxng.Client, pageFetcher *fetch.Fetcher) *Server {
	s := &Server{
		cfg:         cfg,
		searxClient: searxClient,
		pageFetcher: pageFetcher,
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
	// 1. web_search tool
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

	// 2. web_read tool
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

	// 3. health tool
	healthTool := mcp.NewTool("health",
		mcp.WithDescription("Check MCP server and SearXNG backend status."),
	)
	s.mcpServer.AddTool(healthTool, s.handleHealth)
}

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

	results, err := s.searxClient.Search(ctx, query, limit, category, language)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("SearXNG search failed: %v", err)), nil
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format search results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWebRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	targetURL, ok := getStringArg(req, "url")
	if !ok || targetURL == "" {
		return mcp.NewToolResultError("Missing required string parameter 'url'"), nil
	}

	maxChars := s.cfg.FetchMaxChars
	if maxVal, ok := getIntArg(req, "max_chars"); ok && maxVal > 0 {
		maxChars = maxVal
	}

	result, err := s.pageFetcher.ReadPage(ctx, targetURL, maxChars)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read URL: %v", err)), nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format read result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status := s.searxClient.Health(ctx)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format health response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

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
