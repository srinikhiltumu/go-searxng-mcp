# Contributing to go-searxng-mcp

Thanks for your interest in contributing to **go-searxng-mcp**, a high-performance Model Context Protocol (MCP) server that exposes private, token-efficient web search and readable page extraction via a local SearXNG metasearch backend.

This document covers the development workflow, from prerequisites to opening a pull request.

---

## Prerequisites

- **Go 1.25+** — the module is pinned to `go 1.25.5` in [`go.mod`](go.mod). Verify with `go version`.
- **Docker** (with `docker` CLI and `docker compose`) — used to run the SearXNG backend (Container 2) and to build/run the MCP server image (Container 1) for end-to-end testing.
- Optional: `govulncheck` (installed automatically by `go install golang.org/x/vuln/cmd/govulncheck@latest` if not present) for `make vuln`.

---

## Project Structure

The codebase follows a standard Go project layout with a thin entrypoint and dependency-injected internal packages:

```
.
├── cmd/
│   └── searxng-mcp/        # main() entrypoint: wires config + adapters + mcp-go stdio server
│       └── main.go
├── internal/
│   ├── config/             # Env-var loading, defaults, and validation (Config struct)
│   ├── mcp/                # MCP protocol layer: tool definitions, handlers, ports/DTOs, adapters
│   │   ├── server.go       # Searcher / PageReader / HealthChecker ports, tool registration, handlers
│   │   └── adapters.go     # Concrete adapters wrapping searxng.Client and fetch.Fetcher
│   ├── searxng/            # SearXNG HTTP client: Search, Health, retry/backoff, snippet sanitization
│   └── fetch/              # web_read fetcher: SSRF-safe dialer, HTML → markdown extraction, truncation
├── searxng-config/         # SearXNG settings.yml mounted into Container 2
├── Dockerfile              # Builds Container 1 (searxng-mcp:latest)
├── docker-compose.yml      # Starts the SearXNG backend (Container 2)
├── mcp.json                # Reference MCP client config (8 env vars)
└── Makefile                # build / test / lint / vuln / docker-* targets
```

### Key design conventions

- **Ports over concrete types in the MCP layer.** `internal/mcp/server.go` defines the `Searcher`, `PageReader`, and `HealthChecker` interfaces. Handlers depend on these ports, **not** on `*searxng.Client` or `*fetch.Fetcher` directly. This keeps handlers unit-testable with mocks.
- **Adapters translate between layers.** `internal/mcp/adapters.go` wraps the concrete `searxng.Client` / `fetch.Fetcher` into the MCP-layer ports and remaps their DTOs (`SearchResult`, `HealthStatus`, `ReadResult`) into the MCP-layer DTOs (`searchResult`, `healthStatus`, `readResult`).
- **DTOs are the token-optimization boundary.** The MCP-layer DTOs expose only the fields that should reach the LLM; raw SearXNG fields are dropped at the adapter boundary.

---

## Build

Build a stripped native binary into the repo root:

```bash
make build
```

This runs `CGO_ENABLED=0 go build -ldflags="-w -s -X main.version=$(VERSION)" -o searxng-mcp ./cmd/searxng-mcp`. Override the version with `VERSION=v1.2.3 make build`.

To build the Docker image instead (produces `searxng-mcp:latest`):

```bash
make docker-build
```

---

## Test

Run the full test suite with coverage:

```bash
make test
```

This is equivalent to:

```bash
go test -v -cover ./...
```

Tests do **not** require a running SearXNG backend. The `searxng`, `fetch`, and `mcp` packages use `httptest` servers and mock implementations of the `Searcher`, `PageReader`, and `HealthChecker` interfaces, so the suite is fully hermetic.

To focus on a single package:

```bash
go test -v ./internal/searxng/...
```

---

## Lint

Lint runs `go vet` for static analysis and `gofmt` to flag formatting drift:

```bash
make lint
```

This is equivalent to:

```bash
go vet ./... && gofmt -l .
```

If `gofmt -l .` prints any filenames, run `gofmt -w <file>` to fix them before committing. `go vet` findings should be resolved, not suppressed.

---

## Vulnerability Check

Scan dependencies for known CVEs using `govulncheck`:

```bash
make vuln
```

This is equivalent to `govulncheck ./...`. If `govulncheck` is not installed, install it with:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Any reported vulnerability should be addressed by bumping the affected dependency in `go.mod` (and re-running `go mod tidy`) before the PR is opened.

---

## Adding a New MCP Tool

The MCP server uses the [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) library. To add a new tool (for example, a `news_search` tool), follow these four steps:

1. **Define the interface method (port).** In `internal/mcp/server.go`, declare a new port interface and its result DTO. Keep the DTO minimal — only include fields an LLM should see. For example:

   ```go
   // NewsSearcher is the port for news search operations.
   type NewsSearcher interface {
       NewsSearch(ctx context.Context, query string, limit int) ([]newsResult, error)
   }

   // newsResult is the DTO returned by NewsSearcher.
   type newsResult struct {
       Title   string `json:"title"`
       URL     string `json:"url"`
       Source  string `json:"source,omitempty"`
       Snippet string `json:"snippet"`
   }
   ```

   Add a `newsSearcher NewsSearcher` field to the `Server` struct, accept it in `NewServer`, and assign it.

2. **Implement the adapter.** In `internal/mcp/adapters.go`, add a concrete adapter that wraps the backing client (e.g. a new method on `searxng.Client`) and translates its DTO into the MCP-layer DTO. Provide a constructor like `NewSearxngNewsSearcher(client *searxng.Client) NewsSearcher`. Wire it up in `cmd/searxng-mcp/main.go` when constructing the server.

3. **Register the tool and its handler in `server.go`.** Inside `registerTools()`, define the tool with `mcp.NewTool(...)` and its input schema (`mcp.WithString`, `mcp.WithNumber`, etc.), then register a handler with `s.mcpServer.AddTool(tool, s.handleNewsSearch)`. Write the handler method following the pattern of `handleWebSearch` / `handleWebRead`: extract arguments with `getStringArg` / `getIntArg`, call the port, and return `mcp.NewToolResultText` (or `mcp.NewToolResultError` on failure).

4. **Add tests.** In `internal/mcp/server_test.go`, register a fake/mock implementation of the new port and assert the tool's behavior for the success, missing-argument, and underlying-error cases. Mirror the structure of the existing `TestHandleWebSearch_*` tests. If you added a new method to `searxng.Client`, also add a test in `internal/searxng/client_test.go` using an `httptest.Server`.

> **Convention:** Every tool that returns data to the LLM must keep its DTO minimal and must avoid leaking raw upstream fields — this is how the project enforces its token-optimization guarantees (see the *Token Optimization Strategies* table in [README.md](README.md)).

---

## Git Workflow

1. **Branch from `main`.** Create a descriptively named feature branch, e.g. `feat/news-search-tool` or `fix/ssrf-dial-rebinding`. Do not work directly on `main`.

2. **Write conventional commit messages.** This repo follows [Conventional Commits](https://www.conventionalcommits.org/). Use prefixes like `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, and `perf:`. Examples:
   - `feat(mcp): add news_search tool`
   - `fix(fetch): block DNS rebinding in SSRF dialer`
   - `docs(readme): correct USER_AGENT default`

   Keep the subject line to ~50 characters, lowercase after the prefix, and add a body for non-trivial changes explaining *why*.

3. **Validate locally before pushing.** Run the full pre-PR gate:

   ```bash
   make lint && make test
   ```

   For changes that touch `go.mod` or any dependency, also run:

   ```bash
   make vuln
   ```

4. **Open a pull request against `main`.** Describe the motivation, the change, and how it was tested. Link any related issues. Keep PRs focused — one logical change per PR makes review faster.

5. **Address review feedback** with additional commits (avoid force-pushing during review unless asked). Once CI — `make lint && make test` — is green and the reviewer approves, a maintainer will squash-merge into `main`.

---

## Additional Resources

- [README.md](README.md) — architecture overview, setup guide, token optimization, and resilience tables.
- [ARCHITECTURE.md](ARCHITECTURE.md) — deeper design notes.
- [CHANGES.md](CHANGES.md) — release history.
- Upstream libraries: [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go), [PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery), [SearXNG](https://github.com/searxng/searxng).

If you have questions, open a discussion or issue in the [GitHub repository](https://github.com/srinikhiltumu/go-searxng-mcp). Happy hacking!
