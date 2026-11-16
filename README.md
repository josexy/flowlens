<p align="center">
  <img src="resources/icon.png" width="112" alt="FlowLens application icon">
</p>

<h1 align="center">FlowLens</h1>

<p align="center">
  A local-first desktop MITM traffic inspector for HTTP, HTTPS, WebSocket, and SOCKS5 workflows.
</p>

<p align="center">
  <a href="https://github.com/josexy/flowlens/actions/workflows/ci.yml"><img src="https://github.com/josexy/flowlens/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="https://github.com/josexy/flowlens/releases/latest"><img src="https://img.shields.io/github/v/release/josexy/flowlens?display_name=tag" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/josexy/flowlens" alt="License"></a>
</p>

FlowLens captures proxy traffic, identifies local processes, and lets you inspect, edit, replay, and save requests. It is a native desktop application written in Go with [MyGo](https://github.com/egoist/mygo) and [nuxt-ui-go](https://github.com/josexy/nuxt-ui-go).

> [!IMPORTANT]
> FlowLens is currently beta. Internal contracts and local development data may change without compatibility migration before a stable release.

## Highlights

- HTTP/HTTPS MITM capture, SOCKS5 proxying, live SSE message lists with per-message inspection, and WebSocket messages
- Ordered headers/trailers, microsecond timing, transfer sizes, and text, image, and hex body views
- Request editing/resend, WebSocket Client, proxy selection, uTLS profiles, and HTTP/2 fingerprints
- Optional shared DNS cache with configurable expiration and connection-preserving clearing
- Live HTTP(S) rewrite rules: redirects, header/query edits, and UTF-8 body replacement
- API Collections, local history, categorization, and optional Python 3 request hooks with a live console
- HAR import by file picker or drag-and-drop; capture/history export to CSV and HAR
- Traffic-row export of requests, responses, headers, bodies, and combined exchanges
- Process attribution and icons on Windows, macOS, and Linux; managed system proxy and current-user CA trust controls on Windows/macOS
- Signed app updates, shortcuts, customizable theme colors and font sizes, persistent detail layout, and Chinese/English UI

## Install

Download the latest build from [GitHub Releases](https://github.com/josexy/flowlens/releases/latest).

| Platform | Packages |
| --- | --- |
| Windows x64 | NSIS installer |
| macOS | Apple Silicon DMG or universal DMG |
| Linux x64 | Debian package, or a tar archive with `install.sh` |

Verify downloads with `SHA256SUMS`. Unsigned Windows/macOS packages may trigger operating-system warnings.

Use the status-bar update control to check releases. Installations FlowLens can write to download, verify, and apply signed updates after a restart; package-managed installations are pointed to the release page. Versions 1.0.x must install the first native release once by hand.

Start the proxy and configure your client to use `127.0.0.1:8080` by default. New installations automatically generate a built-in CA; trust it in Settings for HTTPS capture. You can also select custom CA certificate and key files, which FlowLens never regenerates. Windows/macOS offer a current-user install/uninstall control. See the [User Guide](docs/user-guide.md) for setup details.

## Quick Start from Source

### Requirements

- Go 1.27.3 or newer
- [Task](https://taskfile.dev) (recommended)
- Xcode Command Line Tools on macOS

Python 3 is needed only for Python request hooks; FlowLens detects existing interpreters but does not install them. See the [Python setup guide](docs/technical/python-plugins.md#set-up-python).

### Run the Desktop App

```shell
git clone https://github.com/josexy/flowlens.git
cd flowlens
task dev
```

If Task is unavailable, run `go tool mygo dev`.

## Documentation

- [User Guide](docs/user-guide.md) — request editing, rewrite rules, shortcuts, system proxy, DNS cache, HAR, process attribution, logs, storage, and certificates
- [HTTP Rewrite Rules](docs/technical/http-rewrite-rules.md) — execution, limits, and lifecycle
- [Python Plugin Guide](docs/technical/python-plugins.md) · [简体中文](docs/technical/python-plugins.zh-CN.md) — open API Help from the Python Plugins sidebar or Request Editor Script toolbar to search APIs and copy a complete system prompt for either scripting scope.
- [Python Plugin Examples](docs/examples/python-plugins)
- [Building and Packaging](docs/technical/building.md)
- [Contributing Guide](CONTRIBUTING.md)
- [Engineering Guidelines](AGENTS.md)
- [Security Policy](SECURITY.md)

## Development

Run commands from the repository root.

| Command | Purpose |
| --- | --- |
| `task dev` | Run with live reload |
| `task run` | Build with MyGo and launch once |
| `task package` | Build release packages for the current platform |
| `task test` | Run all Go tests |
| `task verify` | Run formatting, lint, UI lifecycle, catalog, and test checks |
| `task lint:go` / `task lint:go:fix` | Check Go lint and formatting, or apply safe fixes |
| `task vet:ui` | Check native UI lifecycle rules |
| `task i18n:generate` / `task i18n:check` | Regenerate or check the message catalogs |

Set the application version in `mygo.json`; MyGo embeds it during development and packaging. See [Building and Packaging](docs/technical/building.md#releasing) for release checks and tags.

## Data and Security

Local data is stored under the operating-system user configuration directory. See [storage and cleanup](docs/user-guide.md#settings-and-local-storage) for paths and retention behavior.

**Settings > Storage > Restore Initial State** removes the built-in CA's current-user trust and schedules a full reset after confirmation. FlowLens prompts you to exit and reopen it manually; the next launch shows a startup progress window while clearing FlowLens-managed data and restoring defaults. Reset errors use the same window with **Retry / Exit**. External files and Python installations are kept. The new built-in CA is not automatically trusted; see the user guide for cleanup scope and failure recovery.

- Use FlowLens only for traffic you own or are authorized to inspect.
- Listening on `ALL` (`0.0.0.0`) exposes the proxy to reachable devices.
- Keep the generated MITM CA private key private and remove the CA from trust stores when it is no longer needed.
- Captures, exports, history, API Collections, logs, caches, and process metadata may contain sensitive data; exports are not automatically redacted.
- Python plugins execute as trusted local code; interpreter isolation is not a security sandbox.
- An abnormal shutdown can prevent managed system-proxy restoration. See [Managed System Proxy](docs/user-guide.md#managed-system-proxy) for recovery details.

Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.

## Contributing and Support

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing code or documentation changes. Use [GitHub Issues](https://github.com/josexy/flowlens/issues) for reproducible bugs and focused feature requests, and remove credentials, captured traffic, private paths, certificates, and logs before posting.

## License

FlowLens is licensed under the terms in [LICENSE](LICENSE).
