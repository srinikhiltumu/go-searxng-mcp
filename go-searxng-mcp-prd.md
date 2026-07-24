# PRD: Go-based SearXNG Web Search MCP Server (Local Binary / Docker)

## 1. Product vision

Build a **high-performance, CPU- and memory-optimized MCP server in Go** that:

- Provides **private web search and page reading** via a SearXNG backend using a **Go SDK** (e.g. `github.com/morikuni/go-searxng`).
- Exposes a **minimal set of MCP tools** (`web_search`, `web_read`, `health`) for IDEs (Claude Code, Cursor, VS Code MCP) and terminal agents (Aider, OpenCode).
- **Runs locally** on the developer’s machine:
  - as a **Go binary** referenced directly in `mcp.json`; or  
  - as a **Docker container** using `docker run -i --rm <image>` in `mcp.json`.
- **Does not use npx/uvx wrappers** or generic MCP proxies; everything is built in Go (plus the SearXNG SDK) and optionally packaged as a Docker image.

The server should **coexist** with host-native web search (Anthropic, Google/Gemini, etc.) rather than overriding it:

- Claude Code and Cursor keep their own web search; your server appears as a **separate MCP server** the host can call when appropriate.
- Gemini Deep Research continues to use `google_search` and `url_context` internally and can optionally call your MCP server as an extra source; your server must not interfere with that default behavior.

---

## 2. Constraints and assumptions

### Language and libraries

- **Language**: Go 1.22+ only.
- **SearXNG SDK**: use a **Go client for SearXNG**, e.g. `github.com/morikuni/go-searxng`, instead of generic HTTP wrappers or third-party MCP adapters.
- **MCP SDK**: use a Go MCP SDK such as `mcp-golang` for JSON-RPC 2.0, tool registration, and stdio/HTTP/SSE transport — but **do not use any SearXNG-specific MCP wrappers**; the SearXNG integration is your own code.

### Deployment and runtime

- **Local-only MCP server**:
  - No requirement to deploy a separate MCP service with `npx` or `uvx`.
  - No requirement to register a remote HTTP MCP endpoint; primary usage is as a **local process** launched by `mcp.json` or host CLI.

- **Two supported run modes**:
  1. **Go binary mode**: compiled binary on PATH (e.g. `/usr/local/bin/searxng-mcp`), referenced in `mcp.json` as `"command": "searxng-mcp", "args": []`.
  2. **Docker mode**: Docker image containing the MCP server (and optionally SearXNG), run with `docker run -i --rm searxng-mcp` from `mcp.json`.

- **SearXNG backend**:
  - Either:
    - A **local SearXNG instance**, possibly in another container, configured by the user; or  
    - A **SearXNG inside the same Docker image** (integrated mode), started automatically when the container runs.
  - In both cases, your MCP server connects via HTTP using the Go SDK; users are **not forced** to manage a separate “dedicated MCP instance” — they just run one command in `mcp.json` that starts everything needed.

### Host coexistence

- The server must **not disable or override**:
  - Claude Code’s built-in web search tools.
  - Gemini Deep Research’s `google_search` / `url_context` pipeline.
- Tools and descriptions must clearly state that this is **“Private SearXNG web search”**, so hosts treat it as an optional tool, not a replacement.

---

## 3. User personas and scenarios

### Personas

- **Local developer** using Claude Code / Cursor / VS Code MCP, wanting **private, controllable web search** via SearXNG while keeping host-native search intact.
- **Terminal-focused dev** using Aider/OpenCode, wanting a **single local MCP command** (Go or Docker) instead of npx/uvx for search+read.
- **Homelab / privacy user** who self-hosts SearXNG and wants to plug it into MCP clients without remote services.

### Typical flows

1. **Claude Code / Cursor project-level MCP**:
   - Add MCP server via CLI or by editing `mcp.json`:
     - `"command": "searxng-mcp", "args": []`, or  
     - `"command": "docker", "args": ["run","-i","--rm","searxng-mcp:latest"]`.
   - The host launches the process when needed; Deep Research or Claude uses this MCP only when its tool-selection logic prefers SearXNG search.

2. **VS Code MCP configuration**:
   - `mcp.json` local config referencing Go binary or Docker command using the configuration pattern documented in VS Code agents.

3. **Terminal agent**:
   - Agent (Aider/OpenCode) uses MCP host; host launches the same Go binary or Docker command to provide `web_search` and `web_read` tools.

---

## 4. High-level architecture (pure Go + local run)

### Components

1. **MCP server core (Go)**  
   - Implements MCP JSON-RPC 2.0 primitives: tools, resources (optional), prompts.
   - Uses stdio for primary transport; optional HTTP/SSE for extended scenarios.

2. **SearXNG client (Go SDK)**  
   - Encapsulates all HTTP details via `go-searxng`: query building, engine settings, JSON decoding.

3. **Search orchestration layer**  
   - Maps MCP `web_search` parameters to SearXNG queries (category, language, result limit, etc.).  
   - Normalizes results and trims them for context efficiency.

4. **Fetch & extract module**  
   - Uses `net/http` to fetch pages and Go HTML parsing to extract main content, convert to markdown/text, and truncate.

5. **Resource & context optimization**  
   - In-memory TTL cache for repeated queries.
   - Strict limits on result count and snippet length.

6. **Run-mode wrappers**  
   - **Go binary mode**: main program invoked directly.  
   - **Docker entrypoint**: container starts MCP server (and optionally SearXNG), reading/writing stdio as required by MCP hosts.

---

## 5. Run modes and `mcp.json` configuration

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

### 5.2 Docker mode

**Goal**: run MCP server in an isolated container while still exposing stdio to the MCP host.

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
        "-e", "SEARXNG_URL=http://searxng:8080",
        "-e", "SEARXNG_TIMEOUT=10s",
        "-e", "SEARCH_DEFAULT_LIMIT=5",
        "-e", "FETCH_MAX_CHARS=6000",
        "ghcr.io/your-org/searxng-mcp:1.0.0"
      ]
    }
  }
}
```

---

## 6. Functional requirements (tools)

### 6.1 `web_search`

- **Description**:  
  “Search the web via your SearXNG instance and return compact, ranked results.”

- **Parameters**:
  - `query` (string, required).
  - `limit` (int, optional, default 5, max 10).
  - `category` (string, optional: `general`, `news`, `science`, etc.).
  - `language` (string, optional).

- **Behavior**:
  - Use SearXNG SDK to call `/search` with JSON output.
  - Respect engine configuration; rely on curated, reliable engine sets (e.g. DuckDuckGo, Bing, Mojeek) to avoid captchas and 403s.
  - Normalize each result to:
    - `rank`
    - `title`
    - `url`
    - `engine`
    - `snippet` (160–240 chars).
  - Deduplicate URLs/domains and trim to `limit`.

### 6.2 `web_read`

- **Description**:  
  “Fetch and extract readable content from a given URL.”

- **Parameters**:
  - `url` (string, required, http/https only).
  - `max_chars` (int, optional, default 6000).

- **Behavior**:
  - Validate and block internal/private IPs and non-http(s) schemes to prevent SSRF.
  - Fetch HTML with `net/http` using shared client and timeouts.
  - Extract main content (article body, docs) with tag heuristics.
  - Convert to markdown or clean text and truncate to `max_chars`.

### 6.3 `health`

- **Description**:  
  “Check MCP server and SearXNG backend status.”

- **Behavior**:
  - Send a small test search via SearXNG SDK and measure latency.
  - Return `status`, `searxng_latency_ms`, `engines_configured`.

---

## 7. Non-functional requirements

### 7.1 Performance (CPU & memory)

- Use **shared `http.Client`** for all SearXNG and fetch requests.
- Limit per-request goroutine count; no unbounded parallelism.
- Keep caches and results lightweight; no storing full HTML beyond parsing.

Targets (local dev machine):

- Idle memory: <20 MB.
- Typical usage: <64 MB.
- MCP overhead latency per tool call: small compared to SearXNG and network latency.

### 7.2 Context efficiency

- Tool schemas and descriptions are short and focused.
- `web_search` returns only top 5–10 results with short snippets.
- `web_read` returns main content only, truncated.
- Designed to work with **tool search / dynamic loading** in hosts, so tools don’t always consume context.

---

## 8. Security and isolation

- **Local isolation**:
  - Docker mode isolates MCP server and optional SearXNG from host environment.

- **MCP security**:
  - Follow MCP security best practices: validate input, avoid long-lived sessions, prefer tokens for remote usage.

- **Network safety**:
  - SSRF guards in `web_read`.
  - Optional outbound domain allowlist.

---

## 9. Configuration and setup

### 9.1 Go binary installation

- Build with `go build -o searxng-mcp ./cmd/searxng-mcp`.
- Place binary on PATH (e.g. `/usr/local/bin`).
- Add entry in `mcp.json` or host CLI wizard referencing the binary.

### 9.2 Docker image

- Dockerfile builds Go binary and optionally includes SearXNG and config.
- `ENTRYPOINT` runs MCP server, listening on stdio.
- `mcp.json` uses `docker run -i --rm searxng-mcp:tag` as command.

---

## 10. Testing and validation

- **Unit tests**:
  - SearXNG SDK wrapper: queries, error handling, normalization.
  - Extractor: HTML parsing and truncation.

- **Integration tests**:
  - Run against local SearXNG (native or container).
  - Verify MCP behavior with test hosts or MCP inspector.

- **Host integration tests**:
  - Claude Code, Cursor, VS Code MCP: confirm tools show up, work, and do not interfere with host web search.
  - Gemini Deep Research: configure as remote MCP server and verify that Deep Research still uses Google Search for general queries but can call your server when directed.

---

## 11. Risks and mitigations

- **Engine blocking / captchas**:  
  Tune SearXNG engines and settings; your MCP doesn’t alter SearXNG’s engine behavior, only consumes its API.

- **Host search interference**:  
  Keep tool naming and descriptions explicit about “private SearXNG search” and avoid claiming general-purpose web search dominance; rely on host tool selection to maintain Google/Anthropic search behavior.

- **npx/uvx-related security concerns**:  
  Avoid uvx/npx entirely; binary and Docker-based commands align with guidance favoring containerized or compiled MCP servers over ephemeral script runners.
