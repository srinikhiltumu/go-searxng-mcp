# PRD: Go-based SearXNG Web Search MCP Server (Local Binary / Docker / go run)

## 1. Product vision

Build a **high-performance, CPU- and memory-optimized MCP server in Go** that:

- Provides **private web search and page reading** via a SearXNG backend using a **custom Go HTTP client** (`net/http` + `encoding/json`) that calls the SearXNG `/search` and `/config` JSON REST API directly.
- Exposes a **minimal set of MCP tools** (`web_search`, `web_read`, `health`) for IDEs and AI agent orchestration systems (Google Antigravity, Gemini CLI, Claude Code, Cursor, VS Code MCP, opencode, Aider).
- **Runs locally** on the developer's machine in **three supported modes**:
  - as a **Go binary** referenced directly in `mcp.json` / `opencode.json`;
  - as a **Docker container** using `docker run -i --rm <image>` in `mcp.json`;
  - via **`go run`** for development, referenced as `["go", "run", "./cmd/searxng-mcp"]` in `opencode.json` (no pre-built binary required).
- **Does not use npx/uvx wrappers** or generic MCP proxies; everything is built in Go and optionally packaged as a Docker image.

The server should **coexist** with host-native web search (Anthropic, Google/Gemini, etc.) rather than overriding it:

- Claude Code, Cursor, and Antigravity keep their own web search; your server appears as a **separate MCP server** the host can call when appropriate.
- Gemini Deep Research continues to use `google_search` and `url_context` internally and can optionally call your MCP server as an extra source; your server must not interfere with that default behavior.

---

## 2. Constraints and assumptions

### Language and libraries

- **Language**: Go 1.25.5+ (the `go.mod` floor and Dockerfile base image both pin 1.25).
- **SearXNG client**: a **custom hand-rolled HTTP client** in `internal/searxng/client.go` using `net/http` + `encoding/json`. This directly calls the SearXNG `/search?format=json` and `/config` endpoints. No third-party SearXNG Go SDK (e.g. `github.com/morikuni/go-searxng`) is used; the client is self-contained and avoids an external dependency.
- **MCP SDK**: `github.com/mark3labs/mcp-go` (v0.57.0) for JSON-RPC 2.0, tool registration, and stdio transport. No SearXNG-specific MCP wrappers; the SearXNG integration is entirely custom code.
- **HTML parsing**: `github.com/PuerkitoBio/goquery` for DOM traversal, tag stripping, and content extraction in `web_read`.

### Deployment and runtime

- **Local-only MCP server**:
  - No requirement to deploy a separate MCP service with `npx` or `uvx`.
  - No requirement to register a remote HTTP MCP endpoint; primary usage is as a **local process** launched by `mcp.json`, `opencode.json`, or host CLI.

- **Three supported run modes**:
  1. **Go binary mode**: compiled binary on PATH (e.g. `/usr/local/bin/searxng-mcp`), referenced in `mcp.json` as `"command": "searxng-mcp", "args": []`.
  2. **Docker mode**: Docker image containing the MCP server, run with `docker run -i --rm searxng-mcp:latest` from `mcp.json`. SearXNG runs in a **separate** container (two-container model).
  3. **`go run` dev mode**: launched via `["go", "run", "./cmd/searxng-mcp"]` from `opencode.json` — compiles and runs on the fly, no pre-built binary needed. Ideal for development and replication from a fresh clone.

- **SearXNG backend (two-container model)**:
  - SearXNG runs as a **separate container** (`searxng/searxng:latest`) on host port 8080, configured via `searxng-config/settings.yml`.
  - The MCP server container connects to it via `http://host.docker.internal:8080` (Docker mode) or `http://localhost:8080` (binary/go-run mode).
  - A `docker-compose.yml` in the repo starts the backend with correct relative-path volume mounts.
  - **Integrated single-image mode** (SearXNG inside the MCP image) is not implemented; the two-container model is the only supported topology.

### Host coexistence

- The server must **not disable or override**:
  - Claude Code's built-in web search tools.
  - Gemini Deep Research's `google_search` / `url_context` pipeline.
- Tool descriptions identify the server as SearXNG-based so hosts treat it as an optional tool, not a replacement.

---

## 3. User personas and scenarios

### Personas

- **Local developer** using Claude Code / Cursor / VS Code MCP / Antigravity, wanting **private, controllable web search** via SearXNG while keeping host-native search intact.
- **Terminal-focused dev** using opencode / Aider, wanting a **single local MCP command** (Go binary, Docker, or `go run`) instead of npx/uvx for search+read.
- **Homelab / privacy user** who self-hosts SearXNG and wants to plug it into MCP clients without remote services.

### Typical flows

1. **Claude Code / Cursor / Antigravity project-level MCP**:
   - Add MCP server via CLI or by editing `mcp.json`:
     - `"command": "searxng-mcp", "args": []` (binary mode), or
     - `"command": "docker", "args": ["run","-i","--rm","searxng-mcp:latest"]` (Docker mode).
   - The host launches the process when needed; Deep Research or Claude uses this MCP only when its tool-selection logic prefers SearXNG search.

2. **opencode (go run dev mode)**:
   - `opencode.json` references `["go", "run", "./cmd/searxng-mcp"]` with all environment variables inline. No binary build step required — just clone and run opencode from the repo root.

3. **VS Code MCP configuration**:
   - `mcp.json` local config referencing Go binary or Docker command using the configuration pattern documented in VS Code agents.

4. **Terminal agent (Aider)**:
   - Agent uses MCP host; host launches the same Go binary or Docker command to provide `web_search` and `web_read` tools.

---

## 4. High-level architecture (pure Go + local run)

### Components

1. **MCP server core (Go)** — `internal/mcp/server.go`
   - Implements MCP JSON-RPC 2.0 via `mark3labs/mcp-go`.
   - Registers exactly **three tools**: `web_search`, `web_read`, `health`.
   - Uses **stdio** as the only transport. HTTP/SSE transport is not implemented (deferred).
   - MCP resources and prompts are not implemented (deferred — the PRD marked them optional).
   - Handler pattern: all failures return `(*mcp.CallToolResult, nil)` with the error embedded via `mcp.NewToolResultError`. No handler ever returns a non-nil Go `error`, keeping errors inside the MCP protocol.
   - Defensive argument parsing via `getStringArg` / `getIntArg` helpers that guard against nil maps, wrong types, and the JSON `float64`-for-numbers quirk.

2. **SearXNG client** — `internal/searxng/client.go`
   - A **custom HTTP client** (not a third-party SDK) that calls SearXNG's `/search?format=json` and `/config` endpoints.
   - Handles query building (sets `q`, `format`, optional `categories`, optional `language`), URL deduplication, snippet sanitization, retry/backoff, and response size capping.

3. **Search orchestration layer** — `internal/searxng/client.go` (`Search`, `normalizeAndDeduplicate`, `cleanSnippet`)
   - Maps MCP `web_search` parameters to SearXNG query parameters.
   - Normalizes each raw result to a compact `SearchResult` struct: `rank`, `title`, `url`, `engine`, `snippet`.
   - Deduplicates by **exact URL string** (not by domain — two different URLs on the same domain both survive).
   - Trims snippets to **220 characters** at a word boundary.

4. **Fetch & extract module** — `internal/fetch/fetcher.go`
   - Uses `net/http` with a custom `http.Transport` (dial-timeout, TLS timeout, connection pooling).
   - Parses HTML via `goquery`, strips non-content DOM tags, isolates primary content containers, converts inline HTML to markdown, and truncates to `max_chars` at sentence boundaries.

5. **Resource & context optimization**
   - **No in-memory TTL cache** is implemented (deferred). Every `web_search` and `web_read` hits the network fresh.
   - Strict limits on result count (max 10) and snippet length (220 chars) bound context consumption.

6. **Run-mode wrappers**
   - **Go binary mode**: `cmd/searxng-mcp/main.go` invoked directly.
   - **Docker entrypoint**: `Dockerfile` builds a static binary on `alpine:3.20`; `ENTRYPOINT` runs it, reading/writing stdio.
   - **`go run` dev mode**: `opencode.json` launches `go run ./cmd/searxng-mcp` — no build step required.

---

## 5. Run modes and configuration

### 5.1 Go binary mode

**Goal**: run MCP server like any CLI MCP tool, similar to other stdio MCP servers, but implemented in Go.

Example `mcp.json`:

```json
{
  "mcpServers": {
    "searxng": {
      "command": "/usr/local/bin/searxng-mcp",
      "args": [],
      "env": {
        "SEARXNG_URL": "http://localhost:8080",
        "SEARXNG_TIMEOUT": "10s",
        "SEARCH_DEFAULT_LIMIT": "5",
        "FETCH_MAX_CHARS": "6000"
      }
    }
  }
}
```

### 5.2 Docker mode (two-container)

**Goal**: run MCP server in an isolated container while still exposing stdio to the MCP host. SearXNG runs in a separate container on port 8080.

Example `mcp.json`:

```json
{
  "mcpServers": {
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
      ]
    }
  }
}
```

The SearXNG backend is started via `docker compose up -d` (using the repo's `docker-compose.yml`) or the manual `docker run` command in the README.

### 5.3 `go run` dev mode (opencode)

**Goal**: run the MCP server without a pre-built binary — ideal for development and fresh-clone replication.

`opencode.json` (included in the repo):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "searxng": {
      "type": "local",
      "command": ["go", "run", "./cmd/searxng-mcp"],
      "enabled": true,
      "environment": {
        "SEARXNG_URL": "http://localhost:8080",
        "SEARXNG_TIMEOUT": "10s",
        "SEARCH_DEFAULT_LIMIT": "5",
        "FETCH_MAX_CHARS": "6000",
        "SEARCH_MAX_BYTES": "1048576",
        "WEB_READ_CONCURRENCY": "8",
        "MAX_SEARCH_RETRIES": "2",
        "LOG_LEVEL": "info"
      },
      "timeout": 30000
    }
  }
}
```

---

## 6. Functional requirements (tools)

### 6.1 `web_search`

- **Description**:  
  "Search the web via your SearXNG instance and return compact, ranked results."

- **Parameters**:
  - `query` (string, **required**) — "The search query string".
  - `limit` (number, optional) — "Maximum number of results to return (1-10, default 5)".
  - `category` (string, optional) — "SearXNG search category (e.g., 'general', 'news', 'science', 'it')".
  - `language` (string, optional) — "Search language code (e.g., 'en', 'es', 'de')".

- **Behavior**:
  - Calls SearXNG `/search?format=json` with `q`, optional `categories`, optional `language`. The `pageno` and `engines` params are not set — SearXNG uses its default configured engine set.
  - **Limit bounding**: `limit <= 0` → default (5); `limit > 10` → capped to 10 (`client.go:102-107`).
  - **Retry / backoff**: retries transient failures (connection errors, HTTP 5xx) up to `MAX_SEARCH_RETRIES` (default 2, max 5) with jittered exponential backoff (100ms, 200ms, 400ms... + up to 50ms random jitter). 4xx errors are not retried.
  - **Response size cap**: response body bounded by `SEARCH_MAX_BYTES` (default 1 MiB) via `io.LimitReader`.
  - Normalize each result to:
    - `rank` (1-based insertion order; no re-sorting by score)
    - `title`
    - `url`
    - `engine` (falls back to `engines[0]` if the `engine` field is empty)
    - `snippet` (exactly **220 chars** max, truncated at a word boundary with `...` suffix)
  - **Deduplicate by exact URL string** (not by domain). Trim to `limit`.
  - **Snippet sanitization**: strips `\n`/`\r`, collapses multi-space runs. Does NOT strip non-printable control characters (see §12 — known issue from validation).

- **Engine configuration**: the repo's `searxng-config/settings.yml` **enables** Bing and Mojeek (reliable, permissive) and **disables** DuckDuckGo, Brave, Google CSE, Startpage, and Qwant (CAPTCHA-prone under moderate query volume). This is critical for reliability — without it, SearXNG returns empty results when engines get rate-limited.

### 6.2 `web_read`

- **Description**:  
  "Fetch and extract readable content from a given URL."

- **Parameters**:
  - `url` (string, **required**) — "The target HTTP or HTTPS URL to read".
  - `max_chars` (number, optional) — "Maximum number of characters to extract (default 6000)".

- **Behavior**:
  - **URL validation**: scheme must be `http` or `https`; hostname must be non-empty.
  - **SSRF protection — two layers**:
    - **Pre-flight**: resolves hostname via `net.LookupIP` and blocks if any IP is private/loopback/link-local/multicast/unspecified.
    - **Dial-time guard**: custom `http.Transport.DialContext` re-resolves and re-validates at connection time to defeat DNS rebinding attacks.
    - Blocked ranges: `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `0.0.0.0/8`, `::1`, `fc00::/7`, `fe80::/10`.
  - **Concurrency**: a semaphore caps concurrent `web_read` fetches at `WEB_READ_CONCURRENCY` (default 8, range 1-64). Context-aware — bails out if caller's context is cancelled while waiting for a slot.
  - **HTTP client**: 15s overall timeout, 5s dial timeout, 5s TLS handshake timeout, 100 max idle connections, 90s idle timeout.
  - **Response body cap**: 2 MiB via `io.LimitReader`. Only HTTP 200 accepted; all other status codes return an error.
  - **DOM tag stripping**: removes `script, style, noscript, svg, iframe, nav, footer, header, form, button, [role='navigation']`.
  - **Primary content isolation**: targets first match of `main, article, #content, .content, .post-content, #main`; falls back to `body`.
  - **Inline markdown rendering**: converts `<a>` → `[text](href)`, `<code>` → `` `code` ``, `<strong>`/`<b>` → `**bold**`, `<em>`/`<i>` → `*italic*`, `<br>` → newline. Block elements (`h1`-`h6`, `p`, `li`, `blockquote`, `pre`) get markdown headers, list items, blockquote prefix, and fenced code blocks.
  - **Sentence-boundary truncation**: enforces `max_chars`, looking back for `.`, `!`, `?`, `\n` near the boundary to cut at sentence ends. Falls back to word boundary, then hard cut with `...`.
  - Returns a `ReadResult` struct: `url`, `title`, `content`, `char_count`, `truncated`.

### 6.3 `health`

- **Description**:  
  "Check MCP server and SearXNG backend status."

- **Behavior**:
  - Probes SearXNG's **`/config` endpoint** (not `/search` — no upstream search fan-out, making it lightweight and cheap).
  - Measures round-trip latency to `/config`.
  - Counts **enabled engines** from the `/config` response.
  - Returns a `HealthStatus` struct:
    - `status` (`"ok"` or `"degraded"`)
    - `searxng_url`
    - `searxng_latency_ms`
    - `engines_configured` (count of enabled engines)
    - `error` (present only when degraded)
  - Degraded conditions: unreachable backend, non-200 status, response decode failure, response size cap exceeded.

---

## 7. Non-functional requirements

### 7.1 Performance (CPU & memory)

- Uses **dedicated `http.Client`** instances: one for SearXNG search/health calls (timeout from `SEARXNG_TIMEOUT`), one for `web_read` fetches (15s timeout, custom transport with connection pooling).
- **Bounded concurrency**: `web_read` capped at `WEB_READ_CONCURRENCY` (default 8) via semaphore; no unbounded parallelism.
- **No cache**: no in-memory TTL cache is implemented (deferred). No full HTML stored beyond parsing.
- **Response size caps**: search responses capped at 1 MiB; web_read responses capped at 2 MiB.

Targets (local dev machine):

- Idle memory: <20 MB (Docker image is 28.1 MB).
- Typical usage: <64 MB.
- MCP overhead latency per tool call: small compared to SearXNG and network latency.

### 7.2 Context efficiency

- Tool schemas and descriptions are short and focused.
- `web_search` returns only top 1-10 results with 220-char snippets (~50-60 tokens per snippet, ~250-400 tokens total per search).
- `web_read` returns main content only, truncated to `max_chars` (default 6000).
- **Compact schema serialization**: the `SearchResult` type exposes only `rank`, `title`, `url`, `engine`, `snippet`; raw SearXNG fields (scores, position arrays, raw HTML, `unresponsive_engines`) are dropped at the type boundary.
- Pretty-printed JSON output (`json.MarshalIndent` with 2-space indent) for LLM readability.

### 7.3 Resilience & operational hardening

| Safeguard | Location | Mechanism |
|---|---|---|
| **Lightweight Health Probe** | `client.go:204-271` (`Health`) | Probes `/config` (no upstream fan-out); reports real count of enabled engines. |
| **Retry / Backoff** | `client.go:98-152` (`Search`) | Retries connection errors and HTTP 5xx up to `MAX_SEARCH_RETRIES` (default 2, max 5) with jittered exponential backoff. |
| **Response Size Cap** | `client.go:180-187` (`doSearch`) | Bounds `/search` response body via `SEARCH_MAX_BYTES` (default 1 MiB). |
| **Web-Read Concurrency Bound** | `fetcher.go:77,84-89` | Semaphore caps concurrent fetches at `WEB_READ_CONCURRENCY` (default 8, max 64). |
| **SSRF Dial-time Guard** | `fetcher.go:42-60` | Re-validates IP at `DialContext` time to close DNS-rebinding windows. |
| **Structured Logging** | `main.go:40-54` | `log/slog` with level controlled by `LOG_LEVEL`; emits retries/warnings as structured key-value logs to stderr. |
| **Graceful Shutdown** | `main.go:32` | `mcp-go`'s `ServeStdio` handles `SIGTERM`/`SIGINT` and cancels the stdio context. |

---

## 8. Security and isolation

- **Local isolation**:
  - Docker mode isolates the MCP server from the host environment. SearXNG runs in a separate container.

- **MCP security**:
  - All input parameters are defensively parsed (type-assertions guard against nil, wrong type, JSON float64 quirk).
  - `web_read` validates URL scheme and hostname before any network call.
  - No long-lived sessions; stdio transport is request-response.

- **Network safety (SSRF)**:
  - **Two-layer SSRF protection** in `web_read`:
    1. **Pre-flight DNS resolution** — blocks if hostname resolves to any private/loopback IP.
    2. **Dial-time re-validation** — custom `Transport.DialContext` re-resolves and re-checks at connection time, defeating DNS rebinding.
  - Blocked IP ranges: `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16` (AWS/GCP metadata), `0.0.0.0/8`, `::1`, `fc00::/7`, `fe80::/10`.
  - **Outbound domain allowlist**: not implemented (deferred).

---

## 9. Configuration and setup

### 9.1 Environment variables

| Variable | Description | Default | Validation |
|---|---|---|---|
| `SEARXNG_URL` | Base URL of SearXNG instance | `http://localhost:8080` | Trailing `/` stripped |
| `SEARXNG_TIMEOUT` | Timeout for SearXNG API requests | `10s` | Parsed via `time.ParseDuration`; invalid → 10s |
| `SEARCH_DEFAULT_LIMIT` | Default number of search results | `5` | `<1` → 5; `>10` → 10 |
| `FETCH_MAX_CHARS` | Default max character count for `web_read` | `6000` | `<100` → 6000 |
| `USER_AGENT` | Custom HTTP User-Agent header | `Mozilla/5.0 (compatible; SearXNG-MCP/1.0; +https://github.com/srinikhiltumu/go-searxng-mcp)` | None |
| `SEARCH_MAX_BYTES` | Max bytes from `/search` response body | `1048576` (1 MiB) | `<1024` → 1 MiB |
| `WEB_READ_CONCURRENCY` | Max concurrent `web_read` fetches | `8` | `<1` → 8; `>64` → 64 |
| `MAX_SEARCH_RETRIES` | Retries for transient search failures | `2` | `<0` → 0; `>5` → 5 |
| `LOG_LEVEL` | Structured log level | `info` | Lowercased; `debug`/`info`/`warn`/`error` |

### 9.2 Go binary installation

- Build with `CGO_ENABLED=0 go build -ldflags="-w -s" -o searxng-mcp ./cmd/searxng-mcp`.
- Place binary on PATH (e.g. `/usr/local/bin`).
- Add entry in `mcp.json` or host CLI wizard referencing the binary.

### 9.3 Docker image

- `Dockerfile` is a two-stage build: `golang:1.25-alpine` builder → `alpine:3.20` production image (28.1 MB).
- Builds a statically linked binary (`CGO_ENABLED=0`, stripped with `-ldflags="-w -s"`).
- `ENTRYPOINT` runs the MCP server, listening on stdio.
- `mcp.json` uses `docker run -i --rm searxng-mcp:latest` as the command.
- Image is built locally (`docker build -t searxng-mcp:latest .`); no registry publishing is configured.

### 9.4 SearXNG backend configuration

- `searxng-config/settings.yml` (included in the repo) enables:
  - JSON output format (`formats: [html, json]`)
  - **Bing** and **Mojeek** as general-category engines (reliable, permissive)
  - **Disables** DuckDuckGo, Brave, Google CSE, Startpage, Qwant (CAPTCHA-prone)
- `docker-compose.yml` (included in the repo) starts the backend with correct relative-path volume mounts:
  ```yaml
  services:
    searxng-backend:
      image: searxng/searxng:latest
      container_name: searxng-backend
      ports:
        - "8080:8080"
      volumes:
        - ./searxng-config/settings.yml:/etc/searxng/settings.yml
      restart: unless-stopped
  ```
- Start the backend with `docker compose up -d` from the repo root.

### 9.5 Replication from fresh clone

```bash
git clone <repo-url>
cd go-searxng-mcp
docker compose up -d          # start SearXNG backend
# For opencode: just start opencode from the repo root (uses `go run`).
# For Antigravity/Gemini: docker build -t searxng-mcp:latest .
```

**Prerequisites**: Docker (for SearXNG backend + optional MCP image), Go 1.25+ (for `go run` mode or manual binary build).

---

## 10. Testing and validation

### 10.1 Unit tests

- **SearXNG client** (`internal/searxng/client_test.go`): search happy path, URL deduplication, snippet cleaning + 220-char truncation, health (ok + degraded + engine counting), retry on 5xx, response size cap exceeded.
- **Fetcher** (`internal/fetch/fetcher_test.go`): SSRF IP classifier (table-driven for private/public IPs), HTML-to-markdown rendering (links, code, bold, italic, fenced code blocks), whitespace collapsing, truncation.
- **MCP server** (`internal/mcp/server_test.go`): `web_search` handler (mock SearXNG, verify result round-trip), `health` handler (verify status + engine count).

### 10.2 Integration tests

- Run against local SearXNG container (`docker compose up -d`).
- Verify MCP behavior with test hosts or MCP inspector.

### 10.3 40-scenario validation (completed)

A comprehensive 40-scenario validation was performed covering all three tools:

- **Health** (2 scenarios): status, latency, engine count, idempotency.
- **web_search basic queries** (10 scenarios): general, technical, multilingual, special characters, factoids, code queries, news.
- **web_search limit bounding** (6 scenarios): limit=1, 3, 10, 0 (→default), 15 (→capped to 10), -1 (→default).
- **web_search language** (5 scenarios): es, de, fr, zh, invalid code (graceful fallback).
- **web_search category** (4 scenarios): news, it, images, science — each correctly routes to specialized engines.
- **web_search content-type** (5 scenarios): GitHub, academic, product, entity, how-to.
- **web_read** (5 scenarios): Wikipedia default (5980 chars, truncated at sentence boundary), max_chars=500, max_chars=10000, Docker docs (DOM stripping, not truncated), 404 page (error surfaced).
- **Error handling** (3 scenarios): SSRF localhost blocked, empty query (parameter validation), malformed URL (scheme validation).

**Result: 40/40 scenarios passed.** Two issues identified (see §12).

---

## 11. Risks and mitigations

- **Engine blocking / CAPTCHAs**:  
  The repo's `searxng-config/settings.yml` **disables** CAPTCHA-prone engines (DuckDuckGo, Brave, Google CSE, Startpage, Qwant) and **enables** Bing and Mojeek. This was validated during 40-scenario testing: with DuckDuckGo enabled, parallel queries triggered CAPTCHA rate-limiting, causing SearXNG to return HTTP 200 with `results: []` (which the MCP tool serializes as `null`). Switching to Bing/Mojeek eliminated this issue entirely.

- **Host search interference**:  
  Tool naming and descriptions identify the server as SearXNG-based; rely on host tool selection to maintain Google/Anthropic search behavior.

- **npx/uvx-related security concerns**:  
  Avoid uvx/npx entirely; binary, Docker, and `go run` commands align with guidance favoring containerized or compiled MCP servers over ephemeral script runners.

- **Broken Docker volume mounts**:  
  The `docker-compose.yml` uses relative paths (`./searxng-config/settings.yml`) to avoid the stale-path mount failures that occur with absolute `$(pwd)` references when the repo is moved. The old `docker run -v $(pwd)/...` approach is fragile and documented as a fallback only.

- **Non-portable opencode config**:  
  `opencode.json` uses `["go", "run", "./cmd/searxng-mcp"]` instead of a hardcoded absolute binary path, ensuring it works on any machine with Go installed regardless of clone location.

---

## 12. Known issues and future work

### Known issues (identified during 40-scenario validation)

1. **Snippet contains non-printable control characters**:  
   `cleanSnippet` (`client.go:309-331`) strips `\n`/`\r` and collapses whitespace, but does NOT strip non-printable control characters (e.g. `\u001e`, `\u0018`, `\u0012`). Some Bing results contain these in snippet text. **Fix**: add a control-char strip step (e.g. regex `[\x00-\x08\x0B\x0C\x0E-\x1F]`).

2. **`web_search` returns `null` on empty results**:  
   When all engines return CAPTCHA/suspended, SearXNG returns HTTP 200 with `results: []`. The client returns an empty slice, which MCP serializes as `null`. This makes upstream engine failures look like a tool bug. **Consider**: optionally include `unresponsive_engines` in the response, or return a diagnostic when `len(results)==0 && len(unresponsive_engines)>0`.

### Deferred features (not yet implemented)

- **In-memory TTL cache** for repeated queries (PRD §4.5 originally specified this).
- **HTTP/SSE transport** for the MCP server (stdio only currently).
- **Integrated single-image Docker mode** (SearXNG inside the MCP image — two-container model only).
- **MCP resources and prompts** (tools only currently).
- **Outbound domain allowlist** for `web_read` (IP-based SSRF blocking only).
- **Domain-level deduplication** (current dedup is exact-URL only).
- **Content-type sniffing** for `web_read` (all responses parsed as HTML regardless of Content-Type).
- **Rune-safe truncation** (`truncateText` cuts at byte boundaries, may split multi-byte UTF-8).
