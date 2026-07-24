# Antigravity / Gemini Agent Configuration for Go SearXNG MCP

## Overview

This project uses a custom MCP server written in Go to provide private web search and page reading via a SearXNG backend. The MCP server exposes three tools:

- `web_search` – compact, ranked web search via SearXNG
- `web_read` – safe page fetching and readable content extraction
- `health` – status and latency check for the MCP server and SearXNG

The detailed product requirements are defined in `go-searxng-mcp-prd.md` in this project.

## Agent rules and context

- Always treat the SearXNG MCP server as a **private, complementary web search tool**. Do not disable or override built‑in Google / Gemini web search (e.g. `google_search`, `url_context`, or Deep Research defaults).
- When you need version‑specific docs or examples for Go, MCP, or SearXNG, **call the Context7 MCP server** (e.g. via `use context7`) to fetch up‑to‑date documentation rather than relying solely on training data.
- Use Go 1.22 or later and the latest stable versions of:
  - A Go MCP SDK (for JSON‑RPC 2.0, stdio transport, and tool registration).
  - A SearXNG Go SDK (e.g. `github.com/morikuni/go-searxng`) for calling the `/search` JSON API.
- Optimize for low CPU, low memory, and low token/context usage by:
  - Returning only 5–10 search results with short snippets.
  - Truncating page content in `web_read` by `max_chars` (default ~6000 characters).
  - Keeping tool descriptions and parameter schemas concise.
- Follow MCP security best practices:
  - Validate all input parameters.
  - Block non‑http(s) URLs and private IP ranges in `web_read` (SSRF protection).
  - Prefer environment variables for secrets and configuration.

## Tasks for the agent

When asked to implement or modify the MCP server:

1. **Load the PRD** from `go-searxng-mcp-prd.md` and summarise the key requirements.
2. Use **Context7 MCP** to fetch the latest documentation for:
   - The Go MCP SDK you choose (e.g. `mcp-golang`).
   - The SearXNG Go SDK (`go-searxng`).
   - Any HTML parsing or markdown conversion library you plan to use.
3. Design or update the Go project structure to include:
   - `cmd/searxng-mcp` – main entrypoint for the MCP server.
   - `internal/mcp` – MCP server setup, tool definitions, and handlers.
   - `internal/searxng` – SearXNG client wrapper and search normalization.
   - `internal/fetch` – HTTP fetch and readable content extraction.
4. Implement the MCP tools exactly as defined in the PRD:
   - `web_search` – uses SearXNG SDK to call `/search` with JSON, normalises results.
   - `web_read` – fetches and extracts readable text safely, with SSRF protection and truncation.
   - `health` – checks connectivity and basic metrics.
5. Support two run modes for the MCP server:
   - **Go binary mode**, launched directly via `searxng-mcp` from `mcp.json`.
   - **Docker mode**, launched via `docker run -i --rm ...` from `mcp.json`.
6. Add or refine unit tests and integration tests for the SearXNG client, MCP handlers, and HTML extraction logic.
7. Keep all code and configuration aligned with the latest official MCP, Gemini, and SearXNG documentation.

