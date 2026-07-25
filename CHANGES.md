# Changes Made for opencode MCP Tool Integration

This document records all changes made to make the SearXNG MCP server work reliably with opencode, and to ensure the setup is fully reproducible from a fresh clone.

---

## 1. `searxng-config/settings.yml` — Engine Configuration

**Problem:** The default SearXNG configuration enables DuckDuckGo, Brave, Google CSE, Startpage, and Qwant as general-category search engines. These engines aggressively return CAPTCHA challenges and rate-limit (HTTP 429 / "Suspended: too many requests") under moderate query volume, causing SearXNG to return HTTP 200 with `results: []`. The MCP `web_search` tool serializes this as `null`, making it appear the tool is broken.

**Change:** Added an `engines:` block that:
- **Enables** `bing` and `mojeek` (reliable, permissive general-category engines)
- **Disables** `duckduckgo`, `brave`, `google cse`, `startpage`, `qwant` (CAPTCHA-prone)

```yaml
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
```

**Effect:** Search results are now consistently returned (18-20 results per query) with zero unresponsive engines.

---

## 2. `docker-compose.yml` — New File

**Problem:** The manual `docker run -v $(pwd)/searxng-config/settings.yml:...` command is fragile. A stale or moved directory path causes Docker to auto-create an empty directory at the mount target, breaking SearXNG startup with an opaque "not a directory: Are you trying to mount a directory onto a file" error.

**Change:** Created `docker-compose.yml` using **relative paths** so the volume mount always resolves correctly regardless of where the repo is cloned:

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

**Effect:** Backend startup is now a single command: `docker compose up -d`. No path resolution issues.

---

## 3. `opencode.json` — Portable MCP Command

**Problem:** The `command` field contained a **hardcoded absolute path** (`/Users/srinikhiltumu/Documents/GitHub/go-searxng-mcp/searxng-mcp`) that only works on the original developer's machine. Anyone else cloning the repo would have a broken MCP server launch.

**Change:** Replaced the absolute path with `go run`, which compiles and runs from the project root without requiring a pre-built binary:

```json
"command": ["go", "run", "./cmd/searxng-mcp"],
```

**Effect:** The MCP server now launches correctly on any machine with Go installed, from any clone location.

---

## 4. `README.md` — Setup Guide Updates

**Problem:** The README's Step 1 `cat <<EOF` block created a `settings.yml` without the engine configuration, so anyone following the guide would hit the CAPTCHA issue. The manual `docker run` command also lacked path-quoting guidance.

**Changes:**
- Added a **Recommended Quick Start** callout pointing to `docker compose up -d`
- Updated Step 1's `cat <<EOF` block to include the full `engines:` configuration matching the committed `settings.yml`
- Added explanation of why CAPTCHA-prone engines are disabled
- Added `$(pwd)` quoting guidance in the manual `docker run` command
- Updated the architecture diagram and engine description to reflect Bing/Mojeek as the primary engines

---

## 5. Docker Image Rebuild

**Problem:** The existing `searxng-mcp:latest` Docker image was stale (built from older code).

**Change:** Rebuilt from scratch with `--no-cache`:
- Removed old image and 2 orphaned containers
- Pruned 579.6 MB of Docker build cache
- Rebuilt: `docker build --no-cache -t searxng-mcp:latest .`
- New image ID: `0fb133ea8352` (28.1 MB)

**Note:** The Docker image is used by the Antigravity/Gemini integration (`mcp.json`). The opencode integration uses the local binary via `go run` (see item 3).

---

## 6. Local Binary Rebuild

**Problem:** The local `searxng-mcp` binary was stale.

**Change:** Rebuilt with:
```bash
CGO_ENABLED=0 go build -ldflags="-w -s" -o searxng-mcp ./cmd/searxng-mcp
```

**Note:** This binary is gitignored and not needed for opencode (which uses `go run`), but kept for the Docker and manual-binary run modes documented in `mcp.json` and `GEMINI.md`.

---

## Replication from Fresh Clone

To replicate this setup from a fresh clone:

```bash
# 1. Clone the repo
git clone <repo-url>
cd go-searxng-mcp

# 2. Start the SearXNG backend (uses relative paths in docker-compose.yml)
docker compose up -d

# 3. Wait for boot (~10 seconds), then verify
curl -s http://localhost:8080/config | jq '.engines | length'

# 4. For Antigravity/Gemini (Docker mode): build the MCP image
docker build -t searxng-mcp:latest .

# 5. For opencode: no build needed — opencode.json uses `go run ./cmd/searxng-mcp`
#    Just start opencode from the repo root.
```

**Prerequisites:**
- Docker (for SearXNG backend + optional MCP image)
- Go 1.25+ (for opencode `go run` mode, or to build the binary manually)
