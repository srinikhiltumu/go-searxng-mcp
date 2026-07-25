// Command searxng-mcp is the MCP server entrypoint, serving search, read, and health
// tools over stdio using JSON-RPC 2.0.
package main

import (
	"fmt"
	"log/slog"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/fetch"
	internalmcp "github.com/srinikhiltumu/go-searxng-mcp/internal/mcp"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/searxng"
)

// version is the semantic version of the MCP server, injected via -ldflags at build time.
var version = "dev"

func main() {
	cfg := config.LoadConfig()
	setupLogger(cfg.LogLevel)

	slog.Info("starting searxng mcp server",
		"version", version,
		"searxng_url", cfg.SearxngURL,
		"search_limit", cfg.SearchDefaultLimit,
		"fetch_max_chars", cfg.FetchMaxChars,
		"user_agent", cfg.UserAgent,
		"search_max_bytes", cfg.SearchMaxBytes,
		"searxng_timeout", cfg.SearxngTimeout.String(),
		"web_read_concurrency", cfg.WebReadConcurrency,
		"max_search_retries", cfg.MaxSearchRetries,
		"log_level", cfg.LogLevel)

	searxClient := searxng.NewClient(cfg, nil)
	pageFetcher := fetch.NewFetcher(cfg)

	serverInstance := internalmcp.NewServer(cfg,
		internalmcp.NewSearxngSearcher(searxClient),
		internalmcp.NewFetchPageReader(pageFetcher),
		internalmcp.NewSearxngHealthChecker(searxClient),
	)

	if err := mcpserver.ServeStdio(serverInstance.MCPServer()); err != nil {
		slog.Error("mcp server stopped with error", "error", err)
		_, _ = fmt.Fprintf(os.Stderr, "MCP Server error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("searxng mcp server shut down cleanly")
}

// setupLogger configures the global slog logger from the configured level.
func setupLogger(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
}
