<div align="center">

# jsmap-pro

**Discover JavaScript. Recover source maps. Map sensitive patterns.**

A Go CLI that streams frontend assets, reconstructs exposed source maps, and normalizes static JavaScript strings.

[![CI](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml/badge.svg)](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Hzkun001/jsmap-pro)](https://github.com/Hzkun001/jsmap-pro/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](go.mod)

**English** · [Bahasa Indonesia](README.id.md)

</div>

## Quick Start

Install the latest release once, then scan a website with a Nmap-style positional target:

```sh
go install github.com/Hzkun001/jsmap-pro@latest
jsmap-pro -o results https://example.com
```

> [!TIP]
> Prefer not to install it? From a checkout, run `go run . -o results https://example.com`. You can also use the explicit form `jsmap-pro -u https://example.com -o results`.

Build a standalone binary:

```sh
go build -ldflags="-s -w" -o jsmap-pro .
```

Results are written to `results/`: downloaded JavaScript goes in `raw/`, recovered source maps go in `unpacked/`, and summaries are saved as `summary.json` and `summary.txt`.

## Input modes

Choose one input source per scan:

| Mode | Example | Use it to |
|:--|:--|:--|
| Website | `-u https://example.com` | Discover assets from `<script>` tags and static `.js` references in inline scripts. |
| URL list | `-l urls.txt` | Scan JavaScript URLs collected earlier. |
| Offline HAR | `-har capture.har` | Analyze JavaScript responses from an HTTP Archive without contacting the server. |
| Local directory | `-d ./assets` | Analyze `.js` files on disk, including subdirectories. |

```sh
jsmap-pro -l urls.txt -c 20 -o results-urls
jsmap-pro -har capture.har -o results-har
jsmap-pro -d ./assets -o results-local
```

`urls.txt` contains one HTTP(S) URL per line. Blank lines and lines starting with `#` are ignored. The output directory must be new or empty so earlier results are preserved.

## What it scans

| Category | Main patterns |
|:--|:--|
| WebCrypto | `crypto.subtle.encrypt/decrypt/sign/verify/importKey` |
| Third-party crypto | `CryptoJS`, `forge.cipher`, `JSEncrypt`, `U2FsdGVkX1` |
| Signature headers | `X-Signature`, `X-Sign`, `X-Timestamp`, `X-Request-Id` |
| Base64 helpers | `atob()`, `btoa()` |

Each finding includes a file, line number, category, and code snippet. Raw JavaScript is scanned while streaming to disk. When a supported string-array getter is found, the AST inliner also scans its normalized output in `analyzed/`.

## Source maps

When JavaScript ends with `//# sourceMappingURL=...`, jsmap-pro checks the map and reconstructs its `sourcesContent` under `unpacked/`. The status **Fully Reconstructed via SourceMap** is used only when every source was written successfully. Missing source content and unsafe paths are reported.

HAR and directory modes are offline: source maps are read only when present in the HAR or input directory.

## CLI options

| Option | Default | Description |
|:--|:--|:--|
| `-u` | — | Website URL |
| `-l` | — | File containing JavaScript URLs |
| `-har` | — | HTTP Archive for offline analysis |
| `-d` | — | Local JavaScript directory |
| `-c` | `10` | HTTP worker count |
| `-o` | `jsmap-output` | New or empty output directory |
| `-timeout` | `30s` | Timeout for each HTTP request |

## Build and contribute

Requires Go **1.24 or later**. This minimum is needed for `os.Root`, which keeps source-map extraction within the output directory.

```sh
go build -ldflags="-s -w" -o jsmap-pro .
go test -race ./...
go vet ./...
```

Contributions, bug reports, and feature requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md), the [Code of Conduct](CODE_OF_CONDUCT.md), and the [Security Policy](SECURITY.md).

## Scope and limitations

- Website crawling is static and limited to the main page; the tool does not run a browser or execute JavaScript.
- Runtime-generated chunk paths, inline `data:` source maps, and indexed `sections` maps are not supported yet.
- Inlining is limited to static string arrays near the start of a file and simple top-level getters. It helps scanning but does not prove runtime behavior.
- Response limits: HTML 16 MiB, JavaScript 512 MiB, source maps 64 MiB, and AST input 25 MiB. Long minified lines are still scanned in chunks.
- Random 150–300 ms request delays and browser headers reduce request rate but cannot guarantee a server will accept a scan.

## License

MIT. See [LICENSE](LICENSE).
