# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| main    | ✅ Yes             |
| < main  | ❌ No              |

Only the latest commit on `main` is actively supported with security updates.

## Reporting a Vulnerability

**Please do NOT file a public GitHub issue for security vulnerabilities.**

If you discover a security vulnerability in this project, please report it responsibly:

1. **Email:** Send details to the repository owner via the email address listed on their [GitHub profile](https://github.com/srinikhiltumu).
2. **GitHub Security Advisories:** Use the [GitHub Security Advisory](https://github.com/srinikhiltumu/go-searxng-mcp/security/advisories/new) feature to privately report the vulnerability.

### What to Include

- A clear description of the vulnerability
- Steps to reproduce (proof of concept if possible)
- The potential impact of the vulnerability
- Any suggested remediation

### Response Timeline

| Action                     | Target Time   |
|----------------------------|---------------|
| Initial acknowledgement    | 48 hours      |
| Triage and severity rating | 5 business days |
| Fix and release            | 14 business days (critical), 30 business days (others) |

## Security Architecture

This project handles web requests on behalf of AI agents, making SSRF (Server-Side Request Forgery) protection a critical concern. The following security layers are implemented:

### SSRF Protection (4-Layer Defense)

1. **Scheme Validation** — Only `http` and `https` URL schemes are accepted. `file://`, `ftp://`, `javascript:`, and all other schemes are rejected.

2. **Pre-flight DNS Check** — Before any HTTP connection, the target hostname is resolved and every returned IP is validated against blocked ranges.

3. **Dial-time Re-validation** — A custom `DialContext` re-resolves DNS at connection time and dials the validated IP directly (not the hostname), preventing DNS rebinding attacks.

4. **Redirect Policy** — HTTP redirects are limited to 5 hops and blocked if they target a non-http(s) scheme.

### Blocked IP Ranges

The following IP ranges are blocked to prevent SSRF access to internal infrastructure:

| Range | Description |
|-------|-------------|
| `127.0.0.0/8`, `::1` | Loopback |
| `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | RFC 1918 Private |
| `fc00::/7` | IPv6 Unique Local |
| `169.254.0.0/16`, `fe80::/10` | Link-local |
| `224.0.0.0/4`, `ff00::/8` | Multicast |
| `0.0.0.0/8` | "This host" network |
| `100.64.0.0/10` | CGNAT (RFC 6598) |
| `198.18.0.0/15` | Benchmarking (RFC 2544) |

### Additional Controls

- **Response body cap:** 2 MiB limit via `io.LimitReader` prevents memory exhaustion
- **SearXNG response cap:** 1 MiB with oversize detection
- **HTTP timeouts:** 15s overall, 5s dial, 5s TLS handshake
- **Concurrency limiting:** Semaphore-bounded concurrent fetches (default 8, max 64)
- **Non-root Docker:** Container runs as UID 10001 with `no-new-privileges` and read-only rootfs
- **Error hygiene:** Error messages never leak resolved IPs, hostnames, or internal paths

### Dependencies

This project maintains a minimal dependency footprint (2 direct, 8 indirect) to reduce supply chain risk:

- `github.com/PuerkitoBio/goquery` — HTML parsing (MIT license)
- `github.com/mark3labs/mcp-go` — MCP SDK (MIT license)

Dependencies are version-pinned via `go.sum` and regularly audited using `govulncheck`.

## Security Practices

- **Static analysis:** `golangci-lint` runs in CI with strict errcheck enforcement
- **Vulnerability scanning:** `govulncheck` runs in CI on every push/PR
- **No hardcoded secrets:** All sensitive configuration uses environment variables
- **Container hardening:** Multi-stage builds, non-root user, read-only filesystem, memory limits
