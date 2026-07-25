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
| • Queries upstream search providers (Bing, Mojeek, Wikipedia) per settings.yml               |
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
   - Aggregates results from 80+ search engines; this repo's `settings.yml` enables Bing and Mojeek as the primary general-category engines (see [Step 1](#step-1-create-searxng-configuration-settingsyml) for rationale).

### Why `http://host.docker.internal:8080`?
Inside Container 1 (`searxng-mcp`), `localhost` refers to Container 1's isolated network namespace. Container 1 uses **`host.docker.internal:8080`** to make outbound HTTP requests to Container 2 (`searxng-backend`) listening on host port 8080.

---

## Prerequisites

Before starting, ensure the following are installed and available on your `PATH`:

- **Docker** (with the `docker` CLI and, optionally, `docker compose`) — used to run the SearXNG metasearch backend (Container 2) and to build/run the MCP server image (Container 1).
- **Go 1.25+** (the repo is pinned to `go 1.25.5` in [`go.mod`](go.mod)) — required only if you want to run the MCP server directly from source (`go run ./cmd/searxng-mcp`) or build a native binary via `make build`, instead of using Docker.

> **Tip:** Verify your setup with `docker --version` and `go version`. Both commands must succeed before continuing with the setup guide below.

---

## Dual-Container Setup Guide

Follow these step-by-step commands to configure and run both containers on your machine.

> **Recommended (Quick Start):** This repository includes a `docker-compose.yml` that starts the SearXNG backend with the correct relative-path volume mount. From the repo root:
>
> ```bash
> docker compose up -d
> ```
>
> Then skip to [Step 3](#step-3-build-container-1-go-searxng-mcp-server) to build the MCP server image.
> If you prefer manual `docker run` commands, follow Steps 1–2 below.

### Step 1: Create SearXNG Configuration (`settings.yml`)

The `searxng-config/settings.yml` file is **already included** in this repository with the correct configuration: JSON output format enabled, and reliable general-category engines (Bing, Mojeek) enabled while CAPTCHA-prone engines (DuckDuckGo, Brave, Google CSE, Startpage, Qwant) are disabled to avoid upstream rate-limiting.

If you need to recreate it manually:

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

engines:
  - name: bing
    disabled: false
  - name: mojeek
    disabled: false
  - name: duckduckgo
    disabled: true
  - name: brave
    disabled: true
  - name: google cse
    disabled: true
  - name: startpage
    disabled: true
  - name: qwant
    disabled: true
EOF
```

> **Why disable DuckDuckGo/Brave/etc.?** These engines aggressively return CAPTCHA challenges under moderate query volume, causing SearXNG to return empty result sets. Bing and Mojeek are more permissive and provide reliable results for local development and testing.

### Step 2: Start Container 2 (SearXNG Backend Engine)

Run the official SearXNG engine container on port 8080 with the configuration volume mounted:

```bash
docker run -d \
  --name searxng-backend \
  -p 8080:8080 \
  -v "$(pwd)/searxng-config/settings.yml":/etc/searxng/settings.yml \
  searxng/searxng:latest
```

> **Note:** Always quote `"$(pwd)/..."` to avoid path resolution issues on shells with spaces or special characters. The `docker-compose.yml` in this repo uses relative paths (`./searxng-config/...`) which avoids this problem entirely.

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
    "searxng": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-e",
        "SEARXNG_URL=http://host.docker.internal:8080",
        "-e",
        "SEARXNG_TIMEOUT=10s",
        "-e",
        "SEARCH_DEFAULT_LIMIT=5",
        "-e",
        "FETCH_MAX_CHARS=6000",
        "-e",
        "SEARCH_MAX_BYTES=1048576",
        "-e",
        "WEB_READ_CONCURRENCY=8",
        "-e",
        "MAX_SEARCH_RETRIES=2",
        "-e",
        "LOG_LEVEL=info",
        "searxng-mcp:latest"
      ],
      "env": {}
    }
  }
}
```

> This example mirrors the committed [`mcp.json`](mcp.json) in the repo root and explicitly sets the eight runtime-tunable environment variables so the Docker container behaves identically to a local binary. `USER_AGENT` is intentionally omitted — the server's built-in default already identifies it (see [Configuration Guide](#configuration-guide--environment-variables)). Other MCP servers (e.g. `context7`) can be added as separate entries in `mcpServers` if needed.

### Step 5: Refresh Antigravity MCP Connections

In Antigravity, go to **Customizations ➔ Installed MCP Servers** and click **`Refresh 🔄`**. The `searxng` server will show as active.

---

## Running Tests

The project ships with unit tests for the `config`, `searxng`, `fetch`, and `mcp` packages. Run the full suite with coverage:

```bash
go test -v -cover ./...
```

Or via the provided [`Makefile`](Makefile) target (equivalent):

```bash
make test
```

> **Note:** Tests do not require a running SearXNG backend — they use `httptest` servers and mock interfaces (`Searcher`, `PageReader`, `HealthChecker`) defined in [`internal/mcp/server.go`](internal/mcp/server.go).

---

## Development

Common development tasks are wrapped in the [`Makefile`](Makefile):

| Command | Description |
|---|---|
| `make build` | Builds a stripped native binary (`searxng-mcp`) from `./cmd/searxng-mcp`. |
| `make lint` | Runs `go vet ./...` followed by `gofmt -l .` to catch issues and formatting drift. |
| `make vuln` | Runs `govulncheck ./...` to scan dependencies for known CVEs. |
| `make docker-build` | Builds the `searxng-mcp:latest` Docker image (passes `VERSION` as a build arg). |
| `make docker-up` | Starts the SearXNG backend via `docker compose up -d`. |
| `make docker-down` | Stops the SearXNG backend via `docker compose down`. |
| `make clean` | Removes the built `searxng-mcp` binary. |

> Before opening a pull request, always run `make lint && make test` locally. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the full contribution workflow, including how to add a new MCP tool.

---

## Token Optimization Strategies

The codebase incorporates eight explicit token optimization measures:

| Strategy | Implementation Location | Mechanism & Parameters | Rationale |
|---|---|---|---|
| **Result Limit Bounding** | `internal/config/config.go` (`Config.SearchDefaultLimit`)<br>`internal/searxng/client.go` (`Search`) | `SEARCH_DEFAULT_LIMIT` defaults to `5` and is hard-capped at max `10`. | Prevents large lists of 50-100 items from saturating LLM context window. |
| **Snippet Sanitization** | `internal/searxng/client.go` (`cleanSnippet`) | Strips newlines (`\n`, `\r`), collapses whitespace, caps snippet to `220` chars, and cuts at word boundaries. | Restricts search outputs to ~50-60 tokens per snippet (~250-400 tokens total per search). |
| **URL Deduplication** | `internal/searxng/client.go` (`normalizeAndDeduplicate`) | Uses an in-memory hash set `seenURLs := make(map[string]bool)` to drop duplicate links across engines. | Eliminates redundant identical links in search output payloads. |
| **DOM Tag Stripping** | `internal/fetch/fetcher.go` (`extractReadableText`) | Strips `script`, `style`, `noscript`, `svg`, `iframe`, `nav`, `footer`, `header`, `form`, `button`, `[role='navigation']`. | Removes non-content elements, boilerplate navigation, and inline CSS/SVG. |
| **Primary Node Isolation** | `internal/fetch/fetcher.go` (`extractReadableText`) | Targets primary content containers via `["main", "article", "#content", ".content", ".post-content", "#main"]`. | Isolates article body text, skipping sidebars, footers, and advertisement blocks. |
| **Inline Markdown Rendering** | `internal/fetch/fetcher.go` (`inlineMarkdown`) | Converts `<a>` → `[text](href)`, inline `<code>` → `` `code` ``, `<strong>`/`<em>` → `**bold**`/`*italic*`, collapses inline whitespace. | Preserves links and code spans for the LLM instead of flattening them to bare text. |
| **Sentence Truncation** | `internal/fetch/fetcher.go` (`truncateText`) | Enforces `max_chars` (default `6000`), looking back for `.`, `!`, `?`, `\n` near the boundary to cut at sentence ends. | Avoids truncated outputs ending in mid-word fragments, improving LLM reasoning quality. |
| **Compact Schema Serialization** | `internal/searxng/client.go` (`SearchResult` struct) | The exported `SearchResult` type exposes only `rank`, `title`, `url`, `engine`, `snippet`; raw SearXNG fields (scores, position arrays, raw HTML) are dropped by the type boundary. | Serializes only clean JSON fields without explicit per-call filtering. |

---

## Resilience & Operational Hardening

Beyond token optimization, the server now includes several production-oriented safeguards:

| Safeguard | Implementation Location | Mechanism |
|---|---|---|
| **Lightweight Health Probe** | `internal/searxng/client.go` (`Health`) | Probes SearXNG's `/config` endpoint (no upstream search fan-out) and reports the *real* count of enabled engines in `engines_configured`. |
| **Retry / Backoff** | `internal/searxng/client.go` (`Search`) | Retries transient failures (connection errors, HTTP 5xx) up to `MAX_SEARCH_RETRIES` (default 2) with jittered exponential backoff. |
| **Response Size Cap** | `internal/searxng/client.go` (`doSearch`) | Bounds the `/search` response body via `SEARCH_MAX_BYTES` (default 1 MiB) to prevent runaway backend replies from exhausting memory. |
| **Web-Read Concurrency Bound** | `internal/fetch/fetcher.go` (`NewFetcherWithClient` / `ReadPage`) | A semaphore caps concurrent `web_read` fetches at `WEB_READ_CONCURRENCY` (default 8) to protect upstream sites and local resources. |
| **Structured Logging** | `cmd/searxng-mcp/main.go` (`setupLogger`) | Uses `log/slog` with level controlled by `LOG_LEVEL`; emits retries/warnings as structured key-value logs. |
| **Graceful Shutdown** | `cmd/searxng-mcp/main.go` (`main` / `mcpserver.ServeStdio`) | `mcp-go`'s `ServeStdio` handles `SIGTERM`/`SIGINT` and cancels the stdio context for a clean exit. |

---

## Configuration Guide & Environment Variables

| Variable | Description | Default Value |
|---|---|---|
| `SEARXNG_URL` | Base URL of SearXNG instance | `http://localhost:8080` (or `http://host.docker.internal:8080`) |
| `SEARXNG_TIMEOUT` | Timeout for SearXNG API requests | `10s` |
| `SEARCH_DEFAULT_LIMIT` | Default number of search results (1–10) | `5` |
| `FETCH_MAX_CHARS` | Default max character count for `web_read` | `6000` |
| `USER_AGENT` | Custom HTTP User-Agent header | `Mozilla/5.0 (compatible; SearXNG-MCP/1.0; +https://github.com/srinikhiltumu/go-searxng-mcp)` |
| `SEARCH_MAX_BYTES` | Max bytes accepted from the `/search` response body | `1048576` (1 MiB) |
| `WEB_READ_CONCURRENCY` | Max concurrent `web_read` fetches (1–64) | `8` |
| `MAX_SEARCH_RETRIES` | Retries for transient search failures (0–5) | `2` |
| `LOG_LEVEL` | Structured log level: `debug`, `info`, `warn`, `error` | `info` |

---

## Troubleshooting & FAQ

### 1. `searxng request failed: connection refused`
- **Cause**: Container 2 (`searxng-backend`) is not running on host port 8080.
- **Solution**: Start Container 2 using `docker run -d --name searxng-backend -p 8080:8080 ... searxng/searxng:latest`.

### 2. SearXNG returns HTTP 403 Forbidden
- **Cause**: The `json` format is disabled in SearXNG's `settings.yml`.
- **Solution**: Ensure `formats: [html, json]` is defined in `searxng-config/settings.yml` mounted to Container 2.

### 3. `web_read` returns SSRF Error
- **Cause**: Target URL resolved to a loopback (`127.0.0.1`, `::1`) or private IP range (10/8, 172.16/12, 192.168/16, 169.254/16).
- **Solution**: Intended security behavior. The SSRF guard validates the hostname pre-flight **and** at `DialContext` time to close DNS-rebinding windows. To read a private address, proxy it through a public endpoint instead.

### 4. `health` reports `degraded` with `engines_configured: 0`
- **Cause**: The `/config` endpoint is unreachable or returns no enabled engines.
- **Solution**: Confirm Container 2 is healthy and that `SEARXNG_URL` points at the running SearXNG instance. A zero count means no upstream providers are enabled in SearXNG's `settings.yml`.

---

## License & Credits

- **License**: MIT License.
- **Upstream Projects**: [SearXNG](https://github.com/searxng/searxng), [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go), [PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery).
