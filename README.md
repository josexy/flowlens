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
| Windows x64 / ARM64 | NSIS installer |
| macOS | Apple Silicon DMG or universal DMG |
| Linux x64 / ARM64 | Debian package, or a tar archive with `install.sh` |

Verify downloads with `SHA256SUMS`. Unsigned Windows/macOS packages may trigger operating-system warnings.

Use the status-bar update control to check releases. Installations FlowLens can write to download, verify, and apply signed updates after a restart; package-managed installations are pointed to the release page. FlowLens 1.0.x cannot update itself to 2.0.0; install 2.0.0 once from the release page.

## Get Started

Start the proxy and point your client at `127.0.0.1:8080`. New installations generate a built-in CA; trust it from Settings to capture HTTPS, or select your own certificate and key, which FlowLens never regenerates. On Windows and macOS, Settings can install the CA for the current user. See the [User Guide](docs/user-guide.md#set-up-capture) for details.

## Build from Source

Requirements: Go 1.27.3 or newer, [Task](https://taskfile.dev) (recommended), and the Xcode Command Line Tools on macOS. Python 3 is needed only for Python request hooks; FlowLens detects installed interpreters but does not install one.

```shell
git clone https://github.com/josexy/flowlens.git
cd flowlens
task dev
```

Without Task, run `go tool mygo dev`. See [CONTRIBUTING.md](CONTRIBUTING.md) for the other development commands and [Building and Packaging](docs/technical/building.md) for release builds.

## Documentation

| Document | Contents |
| --- | --- |
| [User Guide](docs/user-guide.md) | Capture setup, certificates, inspection, history and HAR, Request Editor, API Collections, rewrite rules, shortcuts, settings, and storage |
| [Python Plugin Guide](docs/technical/python-plugins.md) · [简体中文](docs/technical/python-plugins.zh-CN.md) | Python setup, hook API, limits, and [examples](docs/examples/python-plugins) |
| [HTTP Rewrite Rules](docs/technical/http-rewrite-rules.md) | Rule execution, limits, and lifecycle |
| [Traffic Metrics and HAR](docs/technical/traffic-metrics-and-har.md) | Timing and size definitions, HAR mapping, and the HBIN history format |
| [Building and Packaging](docs/technical/building.md) | Cgo, packages, signed updates, and releases |
| [Memory Diagnostics](docs/technical/memory-diagnostics.md) | Memory after heavy use, what FlowLens keeps, and profiling a running instance |
| [Contributing](CONTRIBUTING.md) · [Engineering Guidelines](AGENTS.md) | Development workflow and repository rules |
| [Changelog](CHANGELOG.md) · [Security Policy](SECURITY.md) | Release notes and vulnerability reporting |

## Data and Security

FlowLens stores its data in the operating system's user configuration directory; see [Local Storage](docs/user-guide.md#local-storage).

- Use FlowLens only for traffic you own or are authorized to inspect.
- Listening on `ALL` (`0.0.0.0`) exposes the proxy to every device that can reach it.
- Keep the MITM CA private key private, and remove the CA from trust stores when you no longer need it.
- Captures, history, exports, and logs may contain credentials and other sensitive data; exports are not redacted.
- Python plugins run as trusted local code without a sandbox.

See the [User Guide](docs/user-guide.md#security) for more, and report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.

## Contributing and Support

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing code or documentation changes. Use [GitHub Issues](https://github.com/josexy/flowlens/issues) for reproducible bugs and focused feature requests, and remove credentials, captured traffic, private paths, certificates, and logs before posting.

## License

FlowLens is licensed under the terms in [LICENSE](LICENSE).
