# kaizen-mangal

`kaizen-mangal` is a general-purpose CLI manga scraping and download engine maintained by the **Kaizen Architecture** team.

It is an independent fork derived from [`metafates/mangal`](https://github.com/metafates/mangal) **v4.0.6** (MIT License, original attribution preserved in [`LICENSE`](./LICENSE)), re-versioned starting at **`v1.0.0`** to follow Go module conventions cleanly.

## Scope

`kaizen-mangal` is a scraping *engine*: an HTTP/TLS stack, a headless browser bridge, a Lua 5.1 runtime, and a verified chapter downloader. It maintains and distributes **no curated list of sites or project-maintained site scrapers**. Lua scrapers are supplied by the user.

## Coexistence with Upstream `mangal`

`kaizen-mangal` is designed to coexist cleanly alongside upstream `mangal` on the same machine:

- **Binary name:** `kaizen-mangal`
- **Config directory:** `~/.config/kaizen-mangal` (Linux/macOS) or `%APPDATA%\kaizen-mangal` (Windows), overridable via `KAIZEN_MANGAL_CONFIG_PATH`
- **Cache directory:** `~/.cache/kaizen-mangal` (or OS cache dir), overridable via `KAIZEN_MANGAL_CACHE_PATH`
- **Sources directory:** `<config>/sources` (isolated by default; opt-in sharing via `KAIZEN_MANGAL_SOURCES_PATH`)
- **Environment variable prefix:** `KAIZEN_MANGAL_*` (upstream `MANGAL_*` variables are ignored)

## Building from Source

Requires Go **1.27+**:

```sh
CGO_ENABLED=0 go build -o kaizen-mangal .
```

Run the offline test suite:

```sh
go test ./...
```

## Architecture & Roadmap

See [`docs/rfcs/`](./docs/rfcs/README.md) — starting with [RFC 0001](./docs/rfcs/0001-network-resilience-and-modernization.md).

## License

MIT — see [`LICENSE`](./LICENSE). Original work by [metafates](https://github.com/metafates/mangal).
