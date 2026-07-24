package main

import (
	"fmt"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/config"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/fetch"
	internalmcp "github.com/srinikhiltumu/go-searxng-mcp/internal/mcp"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/searxng"
)

func main() {
	cfg := config.LoadConfig()

	searxClient := searxng.NewClient(cfg, nil)
	pageFetcher := fetch.NewFetcher(cfg)

	serverInstance := internalmcp.NewServer(cfg, searxClient, pageFetcher)

	if err := mcpserver.ServeStdio(serverInstance.MCPServer()); err != nil {
		fmt.Fprintf(os.Stderr, "MCP Server error: %v\n", err)
		os.Exit(1)
	}
}
