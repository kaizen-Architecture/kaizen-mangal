# kaizen-mangal

[![Test](https://github.com/kaizen-Architecture/kaizen-mangal/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/kaizen-Architecture/kaizen-mangal/actions/workflows/test.yml)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**`kaizen-mangal` is the modern scraping and download core of [Kaizen](https://github.com/kaizen-Architecture/Kaizen)** — a resilient, actively maintained evolution of the discontinued [`metafates/mangal`](https://github.com/metafates/mangal) (`v4.0.6`).

Built as a pure-Go (`CGO_ENABLED=0`) static binary, `kaizen-mangal` preserves full drop-in CLI and Lua scraper compatibility with upstream `mangal` while modernizing the networking stack, headless browser engine, download integrity pipeline, and machine-readable observability contracts required by Kaizen and homelab deployments.

> **Lineage & Versioning:** `kaizen-mangal` is an independent fork derived from `metafates/mangal` v4.0.6 (MIT License; original copyright and attribution preserved in [`LICENSE`](./LICENSE)). Versioning starts at **`v1.0.0`** to follow Go module conventions cleanly.

---

## Scope & Neutral Engine Policy

`kaizen-mangal` is a **general-purpose scraping and download engine**: an HTTP/TLS client stack, a Lua 5.1 runtime, an optional headless browser bridge, and a verified chapter packager.

- It maintains and distributes **no curated list of sites or project-maintained site scrapers**.
- All custom Lua scrapers are supplied by the user (or managed externally by Kaizen).

---

## What Changed vs. Discontinued `mangal` 4.0.6

| Area | Upstream `mangal` 4.0.6 (discontinued) | `kaizen-mangal` |
|---|---|---|
| **Toolchain & Dependencies** | Go 1.18, vendored `mangal-lua-libs` | Go 1.27+, internalized `internal/lualibs/`, static `CGO_ENABLED=0` multi-arch builds |
| **HTTP / TLS Stack** | Standard Go `net/http` fingerprint | Swappable `network.HTTPClient` with pinned TLS/HTTP2 browser impersonation (`chrome_133` default, `chrome_131`, `firefox_135`, `safari_16_0`, or `none`) |
| **Cookies & Rate Limiting** | In-memory only, lost per invocation | File-locked, atomic, wipe-tolerant persistent cookie jar (`0600`) with domain UA binding, per-host concurrency caps, cross-process domain spacing, and `Retry-After` handling |
| **Deterministic Challenge Handling** | Fails on HTTP 403/503 challenges | Two-tier solver architecture: external FlareSolverr/Byparr (Tier 1) + opt-in headless fallback (Tier 2), protected by a per-domain circuit breaker (`[kaizen-mangal:breaker-open]`, exit code `75`) |
| **Headless Browser (`require("headless")`)** | Outdated `go-rod`, panicking `Must*` calls, broken container launch | `go-rod v0.116.2` + `go-rod/stealth`, deterministic resolution (`cdp_url` → `bin` → system → opt-in download), `Leakless(false)`, isolated temp profiles cleaned on exit, zero panics |
| **Download Integrity** | Could write HTML error pages into `.cbz` and report success | Magic-byte image validation (JPEG, PNG, GIF, WebP, AVIF), rejects HTML/JSON on HTTP 200, atomic `.kaizen-tmp` ZIP/CBZ writer with EOCD verification, fail-closed downloader |
| **Kaizen / Automation Contracts** | Human-oriented output only | `version --json` (`contract_version: 1`), `sources validate <file.lua> --json` (isolated stubbed Lua state), `doctor --json`, and structured `[kaizen-mangal:error]` stderr lines |

---

## Installation

### Pre-built Binaries (GitHub Releases)

Download the static binary for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, or `windows/amd64` (with SHA-256 `checksums.txt`) from [GitHub Releases](https://github.com/kaizen-Architecture/kaizen-mangal/releases).

### Docker Images (GHCR)

Multi-arch images (`linux/amd64`, `linux/arm64`) are published exclusively to GitHub Container Registry:

- **Minimal (Alpine + static binary, low RAM):**
  ```sh
  docker pull ghcr.io/kaizen-architecture/kaizen-mangal:latest
  ```
- **Chromium variant (includes Alpine Chromium + fonts for headless Lua scrapers):**
  ```sh
  docker pull ghcr.io/kaizen-architecture/kaizen-mangal:latest-chromium
  ```

Staging builds from the active development branch are also published under `staging-latest` and `staging-latest-chromium`.

### Build from Source

Requires **Go 1.27+**:

```sh
CGO_ENABLED=0 go build -o kaizen-mangal .
```

Run the 100% offline test suite:

```sh
go test ./...
```

---

## Kaizen-Facing CLI Commands

`kaizen-mangal` preserves every `mangal` CLI flag and JSON schema (`inline -j`, `inline -d --format cbz`, `sources list -r`, `where -s`, `where --cache`, `config info -j`, `[NNNN]_<name>.cbz` naming) and adds machine-readable operational commands (`contract_version: 1`):

```sh
# Machine-readable version & contract check
kaizen-mangal version --json

# Offline Lua scraper validation (isolated state: stubbed http & headless, zero network/browser calls)
kaizen-mangal sources validate /path/to/MyScraper.lua --json

# Environment, TLS profile, browser, external solver, and storage diagnostics
kaizen-mangal doctor --json
```

### Error & Circuit-Breaker Contract

- Failures emit a compact structured stderr line: `[kaizen-mangal:error] {"code":"...","status":...,"domain":"...","contract_version":1}`.
- When a domain's challenge circuit breaker is open, `kaizen-mangal` fails fast with `[kaizen-mangal:breaker-open]` and exit code **`75`** (`EX_TEMPFAIL`), omitting retryable keywords so orchestrators like Kaizen neither retry aggressively nor falsely deactivate the source.

---

## Configuration & Environment Variables

All configuration keys can be set in `kaizen-mangal.toml` (via `kaizen-mangal config set`) or overridden via `KAIZEN_MANGAL_*` environment variables:

| Config Key | Environment Variable | Type | Default | Description |
|---|---|---|---|---|
| `network.impersonate` | `KAIZEN_MANGAL_NETWORK_IMPERSONATE` | `string` | `"chrome_133"` | Pinned TLS/H2 fingerprint (`chrome_133`, `chrome_131`, `firefox_135`, `firefox_133`, `safari_16_0`, or `none`) |
| `network.cookie_jar_path` | `KAIZEN_MANGAL_NETWORK_COOKIE_JAR_PATH` | `string` | `""` | Custom path to persistent cookie jar JSON (defaults to `<cache>/kaizen-mangal-cookies.json`) |
| `network.domain_min_interval_ms` | `KAIZEN_MANGAL_NETWORK_DOMAIN_MIN_INTERVAL_MS` | `int` | `0` | Minimum spacing in ms between requests to the same domain across processes |
| `network.domain_max_concurrency` | `KAIZEN_MANGAL_NETWORK_DOMAIN_MAX_CONCURRENCY` | `int` | `8` | Max parallel page downloads per host |
| `network.retry_after_max` | `KAIZEN_MANGAL_NETWORK_RETRY_AFTER_MAX` | `int` | `30` | Max seconds to honor a `Retry-After` header |
| `network.cloudflare_solver_url` | `KAIZEN_MANGAL_NETWORK_CLOUDFLARE_SOLVER_URL` | `string` | `""` | External FlareSolverr / Byparr v1 endpoint (Tier 1) |
| `network.cloudflare_solver_timeout` | `KAIZEN_MANGAL_NETWORK_CLOUDFLARE_SOLVER_TIMEOUT` | `int` | `60` | Solver timeout in seconds |
| `network.cloudflare_solver_fallback` | `KAIZEN_MANGAL_NETWORK_CLOUDFLARE_SOLVER_FALLBACK` | `bool` | `false` | Opt-in local/CDP headless fallback (Tier 2) when Tier 1 is unset or fails |
| `network.cloudflare_breaker_cooldown` | `KAIZEN_MANGAL_NETWORK_CLOUDFLARE_BREAKER_COOLDOWN` | `int` | `900` | Circuit breaker cooldown in seconds after consecutive challenge failures |
| `headless.cdp_url` | `KAIZEN_MANGAL_HEADLESS_CDP_URL` | `string` | `""` | Remote Chrome DevTools Protocol WebSocket URL |
| `headless.bin` | `KAIZEN_MANGAL_HEADLESS_BIN` | `string` | `""` | Explicit path to Chromium/Chrome executable |
| `headless.timeout` | `KAIZEN_MANGAL_HEADLESS_TIMEOUT` | `int` | `30` | Default per-operation headless timeout in seconds |
| `headless.allow_download` | `KAIZEN_MANGAL_HEADLESS_ALLOW_DOWNLOAD` | `bool` | `true` (`false` in Docker) | Allow managed Chromium download if no system browser is found |

Additional path/process overrides: `KAIZEN_MANGAL_CONFIG_PATH`, `KAIZEN_MANGAL_CACHE_PATH`, `KAIZEN_MANGAL_SOURCES_PATH`, and `KAIZEN_MANGAL_DISABLE_UPDATE_CHECK`.

---

## Lua Scraper Runtime Extensions

All existing `mangal` Lua scrapers (`SearchManga`, `MangaChapters`, `ChapterPages`) work without modification. Scrapers using `require("headless")` additionally benefit from automatic `go-rod/stealth` evasion, non-panicking timeouts, and optional lifecycle/cookie helpers:

```lua
local headless = require("headless")
local browser = headless.browser()
local page = browser:page()
page:navigate(url)
page:waitLoad()
local html = page:html()

-- Optional kaizen-mangal helpers (guard if supporting legacy mangal 4.0.6):
if browser.sync_cookies then browser:sync_cookies() end
if page.close then page:close() end
if browser.close then browser:close() end
```

---

## Coexistence with Upstream `mangal`

`kaizen-mangal` can be installed side by side with legacy `mangal` on the same machine without interference:

- **Binary:** `kaizen-mangal`
- **Config dir:** `~/.config/kaizen-mangal` (Linux/macOS) or `%APPDATA%\kaizen-mangal` (Windows)
- **Cache dir:** `~/.cache/kaizen-mangal` (or OS user cache dir)
- **Sources dir:** `<config>/sources` (isolated by default; opt-in sharing via `KAIZEN_MANGAL_SOURCES_PATH`)
- **Env prefix:** `KAIZEN_MANGAL_*` (never reads `MANGAL_*`)

---

## Architecture & Roadmap

Design decisions and phased rollout plans are tracked in [`docs/rfcs/`](./docs/rfcs/README.md):

- [RFC 0001 — Network Resilience, Download Integrity & Modernization](./docs/rfcs/0001-network-resilience-and-modernization.md)

---

## License

MIT — see [`LICENSE`](./LICENSE). Original `mangal` v4.0.6 work by [metafates](https://github.com/metafates/mangal); continued and modernized by **Kaizen Architecture**.
