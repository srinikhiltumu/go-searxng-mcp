# ARCHITECTURE.md - SearXNG MCP Search Stack Architecture

This document outlines the system architecture, dual-container topology, component interactions, data flows, and security boundaries of the **SearXNG MCP Search Stack**.

---

## 1. Dual-Container Architecture & Topology

The production stack uses a **two-container architecture** to separate the local metasearch engine from the AI-facing Model Context Protocol (MCP) translation layer:

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
| • Queries upstream search providers (Bing, Mojeek, Wikipedia) per settings.yml                |
| • Exposes the HTTP REST API on http://localhost:8080                                          |
+-----------------------------------------------------------------------------------------------+
```

---

## 2. Container Setup & Configuration Commands

### Container 2: SearXNG Backend Engine (`searxng-backend`)

Run on host port 8080 with mounted `settings.yml` enabling `search.formats: [html, json]`:

```bash
docker run -d \
  --name searxng-backend \
  -p 8080:8080 \
  -v $(pwd)/searxng-config/settings.yml:/etc/searxng/settings.yml \
  searxng/searxng:latest
```

### Container 1: Go SearXNG MCP Server (`searxng-mcp`)

Built from the local project `Dockerfile` and launched via stdio by Antigravity in `mcp.json`:

```bash
docker build -t searxng-mcp:latest .
```

### Antigravity Configuration (`mcp.json`)

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

---

## 3. Data Flows & Tool Executions

### Web Search Execution Flow (`web_search`)

```
Host Agent              internal/mcp           internal/searxng            SearXNG Backend
    |                        |                        |                           |
    |--- Call web_search --->|                        |                           |
    |  (query, limit, cat)   |--- Search(query, ...) ->|                           |
    |                        |                        |--- GET /search?format=json ->|
    |                        |                        |<-- Raw JSON Results ------|
    |                        |                        |                           |
    |                        |                        |-- Deduplicate URLs        |
    |                        |                        |-- Sanitize Snippets (220c)|
    |                        |                        |-- Rank & Trim to Limit    |
    |                        |<-- []SearchResult -----|                           |
    |<-- Tool Result Text ---|                        |                           |
    |    (Compact JSON)      |                        |                           |
```

### Web Read Execution Flow (`web_read`)

```
Host Agent              internal/mcp            internal/fetch             Target Web Server
    |                        |                        |                           |
    |--- Call web_read ------>|                        |                           |
    |    (url, max_chars)    |--- ReadPage(url, ...) ->|                           |
    |                        |                        |-- Validate Scheme (http)  |
    |                        |                        |-- Lookup DNS IPs          |
    |                        |                        |-- Check isPrivateIP()     |
    |                        |                        |   (Reject 127.0.0.1/10.0)|
    |                        |                        |                           |
    |                        |                        |--- GET Target Web Page -->|
    |                        |                        |<-- HTML Body Response ----|
    |                        |                        |                           |
    |                        |                        |-- Strip non-content tags  |
    |                        |                        |-- Extract Main Body Node  |
    |                        |                        |-- Format Markdown         |
    |                        |                        |-- Truncate Sentence End   |
    |                        |<-- *ReadResult --------|                           |
    |<-- Tool Result Text ---|                        |                           |
    |    (Clean Markdown)    |                        |                           |
```

---

## 4. Outbound SSRF Network Boundary Guard

```
+---------------------------------------------------------------------------------+
|                              OUTBOUND DIALER GUARD                              |
+---------------------------------------------------------------------------------+
|                                                                                 |
|  Target Hostname: example.com                                                   |
|       |                                                                         |
|       v                                                                         |
|  net.LookupIP("example.com") ──> [ 93.184.216.34 ]                              |
|                                           |                                     |
|                                           v                                     |
|                              isPrivateOrLoopbackIP()                            |
|                                           |                                     |
|             +-----------------------------+-----------------------------+       |
|             |                                                           |       |
|             v (IS Private / Loopback)                                   v (Public)
|  [ 127.0.0.1, 10.0.0.0/8, 172.16.0.0/12,                                ALLOW   |
|    192.168.0.0/16, 169.254.0.0/16, ::1 ]                               Connection  |
|             |                                                           |       |
|             v                                                           v       |
|  BLOCK CONNECTION (Return SSRF Error)                        Dial Target Server |
+---------------------------------------------------------------------------------+
```
