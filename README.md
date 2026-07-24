# SearXNG MCP Search Stack – Multi-Agent, Token-Optimized Retrieval System

A high-performance, CPU- and memory-optimized Model Context Protocol (MCP) server written in Go (1.25.5). This system provides private, token-efficient web search and readable page content extraction via a local SearXNG metasearch backend, engineered specifically for integration with AI agent orchestration systems (such as Antigravity, Gemini CLI, Claude Code, and Cursor).

---

## Architecture & Dual-Container Model

The production setup uses a **two-container architecture** to separate the metasearch engine software from the AI-facing MCP protocol translation layer:

```
+-----------------------------------------------------------------------------------------------+
|                                      HOST SYSTEM / IDE                                        |
|                                     (Google Antigravity)                                      |
+-----------------------------------------------------------------------------------------------+
                                               |
                                               | STDIO Pipe (JSON-RPC 2.0)
                                               v
+-----------------------------------------------------------------------------------------------+
| CONTAINER 1: searxng-mcp:latest (Your Go MCP Server)                                         |
| Launched by Antigravity mcp.json                                                              |
|                                                                                               |
| • Exposes MCP tools: web_search, web_read, health                                             |
| • Handles SSRF IP protection, HTML stripping, & sentence truncation                          |
| • DOES NOT possess a search engine index or web scrapers                                      |
+-----------------------------------------------------------------------------------------------+
                                               |
                                               | Outbound HTTP GET /search?format=json
                                               | via http://host.docker.internal:8080
                                               v
+-----------------------------------------------------------------------------------------------+
| CONTAINER 2: searxng/searxng:latest (The SearXNG Engine)                                      |
| Running on host port 8080                                                                     |
|                                                                                               |
| • The actual metasearch engine software                                                       |
| • Queries upstream search providers (DuckDuckGo, Bing, Mojeek, Wikipedia)                     |
| • Exposes the HTTP REST API on http://localhost:8080                                          |
+-----------------------------------------------------------------------------------------------+
```

### Why Two Containers?

1. **`searxng-mcp:latest` (Container 1 - Go MCP Server)**:
   - Built from this repository. Communicates with Antigravity over stdio using JSON-RPC 2.0.
   - Exposes tools (`web_search`, `web_read`, `health`).
   - Handles result ranking, URL deduplication, snippet sanitization (220 chars max), and SSRF IP blocklists.
2. **`searxng/searxng:latest` (Container 2 - SearXNG Engine)**:
   - Official SearXNG metasearch service running on `http://localhost:8080`.
   - Aggregates results from 70+ search engines (DuckDuckGo, Bing, Wikipedia, etc.).

### Why `http://host.docker.internal:8080`?
Inside Container 1 (`searxng-mcp`), `localhost` refers to Container 1's isolated network namespace. Container 1 uses **`host.docker.internal:8080`** to make outbound HTTP requests to Container 2 (`searxng-backend`) listening on host port 8080.

---

## Dual-Container Setup Guide

Follow these step-by-step commands to configure and run both containers on your machine:

### Step 1: Create SearXNG Configuration (`settings.yml`)

Create a local configuration directory and file enabling JSON output format:

```bash
mkdir -p searxng-config
cat << 'EOF' > searxng-config/settings.yml
use_default_settings: true

search:
  safe_search: 0
  autocomplete: ""
  formats:
    - html
    - json

server:
  port: 8080
  bind_address: "0.0.0.0"
  secret_key: "searxngsecretkeyforlocaltesting"
EOF
```

### Step 2: Start Container 2 (SearXNG Backend Engine)

Run the official SearXNG engine container on port 8080 with the configuration volume mounted:

```bash
docker run -d \
  --name searxng-backend \
  -p 8080:8080 \
  -v $(pwd)/searxng-config/settings.yml:/etc/searxng/settings.yml \
  searxng/searxng:latest
```

### Step 3: Build Container 1 (Go SearXNG MCP Server)

Build the Go MCP server container image from this repository:

```bash
docker build -t searxng-mcp:latest .
```

### Step 4: Configure Antigravity `mcp.json`

Add the Docker-based SearXNG MCP server entry into your project or global `~/.gemini/config/mcp_config.json` file:

```json
{
  "mcpServers": {
    "context7": {
      "serverUrl": "https://mcp.context7.com/mcp"
    },
    "searxng": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-e", "SEARXNG_URL=http://host.docker.internal:8080",
        "-e", "SEARXNG_TIMEOUT=10s",
        "-e", "SEARCH_DEFAULT_LIMIT=5",
        "-e", "FETCH_MAX_CHARS=6000",
        "searxng-mcp:latest"
      ],
      "env": {}
    }
  }
}
```

### Step 5: Refresh Antigravity MCP Connections

In Antigravity, go to **Customizations ➔ Installed MCP Servers** and click **`Refresh 🔄`**. Both `context7` and `searxng` will show as active.

---

## Token Optimization Strategies

The codebase incorporates seven explicit token optimization measures:

| Strategy | Implementation Location | Mechanism & Parameters | Rationale |
|---|---|---|---|
| **Result Limit Bounding** | `internal/config/config.go:28-34`<br>`internal/searxng/client.go:81-86` | `SEARCH_DEFAULT_LIMIT` defaults to `5` and is hard-capped at max `10`. | Prevents large lists of 50-100 items from saturating LLM context window. |
| **Snippet Sanitization** | `internal/searxng/client.go:178-195` (`cleanSnippet`) | Strips newlines (`\n`, `\r`), collapses whitespace, caps snippet to `220` chars, and cuts at word boundaries. | Restricts search outputs to ~50-60 tokens per snippet (~250-400 tokens total per search). |
| **URL Deduplication** | `internal/searxng/client.go:149-175` (`normalizeAndDeduplicate`) | Uses an in-memory hash set `seenURLs := make(map[string]bool)` to drop duplicate links across engines. | Eliminates redundant identical links in search output payloads. |
| **DOM Tag Stripping** | `internal/fetch/fetcher.go:121` | Strips `script`, `style`, `noscript`, `svg`, `iframe`, `nav`, `footer`, `header`, `form`, `button`, `[role='navigation']`. | Removes non-content elements, boilerplate navigation, and inline CSS/SVG. |
| **Primary Node Isolation** | `internal/fetch/fetcher.go:124-132` | Targets primary content containers via `["main", "article", "#content", ".content", ".post-content", "#main"]`. | Isolates article body text, skipping sidebars, footers, and advertisement blocks. |
| **Sentence Truncation** | `internal/fetch/fetcher.go:211-227` (`truncateText`) | Enforces `max_chars` (default `6000`), looking back for `.`, `!`, `?`, `\n` near the boundary to cut at sentence ends. | Avoids truncated outputs ending in mid-word fragments, improving LLM reasoning quality. |
| **Compact Schema Serialization** | `internal/mcp/server.go:102-106`<br>`internal/mcp/server.go:129-133` | Filters out raw HTML templates, raw engine position arrays, and SearXNG internal scores. | Serializes only clean JSON fields (`rank`, `title`, `url`, `engine`, `snippet`). |

---

## Configuration Guide & Environment Variables

| Variable | Description | Default Value |
|---|---|---|
| `SEARXNG_URL` | Base URL of SearXNG instance | `http://localhost:8080` (or `http://host.docker.internal:8080`) |
| `SEARXNG_TIMEOUT` | Timeout for SearXNG API requests | `10s` |
| `SEARCH_DEFAULT_LIMIT` | Default number of search results (1–10) | `5` |
| `FETCH_MAX_CHARS` | Default max character count for `web_read` | `6000` |
| `USER_AGENT` | Custom HTTP User-Agent header | `SearXNG-MCP/1.0` |

---

## Troubleshooting & FAQ

### 1. `searxng request failed: connection refused`
- **Cause**: Container 2 (`searxng-backend`) is not running on host port 8080.
- **Solution**: Start Container 2 using `docker run -d --name searxng-backend -p 8080:8080 ... searxng/searxng:latest`.

### 2. SearXNG returns HTTP 403 Forbidden
- **Cause**: The `json` format is disabled in SearXNG's `settings.yml`.
- **Solution**: Ensure `formats: [html, json]` is defined in `searxng-config/settings.yml` mounted to Container 2.

### 3. `web_read` returns SSRF Error
- **Cause**: Target URL resolved to a loopback (`127.0.0.1`, `::1`) or private IP range.
- **Solution**: Intended security behavior blocking local network scanning.

---

## License & Credits

- **License**: MIT License.
- **Upstream Projects**: [SearXNG](https://github.com/searxng/searxng), [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go), [PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery).
