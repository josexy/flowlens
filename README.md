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

<p align="center">
  <strong>English</strong> · <a href="README.zh-CN.md">简体中文</a>
</p>

FlowLens captures proxy traffic, attributes connections to local processes, and lets you inspect, edit, replay, and organize requests. It is a native desktop application written in Go with [MyGo](https://github.com/egoist/mygo) and [nuxt-ui-go](https://github.com/josexy/nuxt-ui-go).

## Highlights

- **Traffic capture:** HTTP/HTTPS MITM capture, SOCKS5 proxying, pause/resume recording without disconnecting clients, live SSE event streams with per-message inspection, and WebSocket message capture.
- **Deep inspection:** Ordered headers and trailers, microsecond transport timing, transfer sizes, and Text, Raw, Image (including SVG, AVIF, and ICO), and Hex body viewers.
- **Request workbench:** HTTP Request Editor, resend, WebSocket Client, flexible proxy selection, uTLS ClientHello profiles, HTTP/2 fingerprints, and an optional shared DNS cache with connection-preserving clearing.
- **Live rewrite rules:** Redirect URLs and edit headers, query parameters, and UTF-8 bodies on live HTTP/HTTPS proxy traffic with inline regex preview.
- **Collections, history, and scripting:** API Collections, local HBIN history, traffic categories, and optional Python 3 request/response hooks with a live per-tab console.
- **Import and export:** HAR import via file picker or drag-and-drop; CSV and HAR export for capture and history; single-row export of requests, responses, headers, bodies, and combined exchanges.
- **Platform integration:** Process attribution and app icons on Windows, macOS, and Linux, plus managed system proxy and current-user CA trust controls on Windows and macOS.
- **Desktop ergonomics:** Signed in-app updates, customizable keyboard shortcuts, theme colors, independent UI and code font sizes, persistent detail layouts, and English/Chinese localization.

## Install

Download the latest release from [GitHub Releases](https://github.com/josexy/flowlens/releases/latest).

| Platform | Packages |
| --- | --- |
| Windows x64 / ARM64 | NSIS installer |
| macOS | Apple Silicon DMG or universal DMG |
| Linux x64 / ARM64 | Debian package (`.deb`), RPM package (`.rpm`), or a `.tar.gz` archive with `install.sh` |

Linux packages use GTK 3 for the native UI and do not require WebKitGTK.

Verify downloads against `SHA256SUMS`. Windows packages are unsigned and macOS packages use ad-hoc signing (not notarized), so operating-system security warnings are expected on first launch.

Use the status-bar update control to check for new releases. Writable installations download, verify, and apply signed updates automatically on restart; package-managed installations open the release page instead. FlowLens 1.0.x cannot self-update to 2.0.0—install 2.0.0+ once from the release page to keep your existing data.

## Quick Start

1. Start the proxy from the toolbar and point your client at `127.0.0.1:8080`.
2. To decrypt HTTPS traffic, open **Settings > Proxy** and trust the built-in CA certificate (generated automatically on new installations), or select your own CA certificate and private key (which FlowLens never overwrites).
3. On Windows and macOS, use the shield button in **Settings > Proxy** to install the CA for the current user.

See the [User Guide](docs/user-guide.md#set-up-capture) for detailed capture, certificate, and system-proxy setup.

## Build from Source

**Requirements:** Go 1.27.3 or newer and [Task](https://taskfile.dev) (recommended). Ordinary builds and tests use `CGO_ENABLED=0` on every desktop platform and require no C/Objective-C compiler or Node.js toolchain. macOS signing and DMG packaging still require macOS platform tools; see [Building and Packaging](docs/technical/building.md). Python 3 is required only for Python request hooks (FlowLens detects installed interpreters without installing one).

```shell
git clone https://github.com/josexy/flowlens.git
cd flowlens
task dev
```

Without Task, run `go tool mygo dev` with `CGO_ENABLED=0`. See [CONTRIBUTING.md](CONTRIBUTING.md) for all development commands and [Building and Packaging](docs/technical/building.md) for release packaging.

## Documentation

| Document | Contents |
| --- | --- |
| [User Guide](docs/user-guide.md) | Capture setup, certificates, inspection, history and HAR, Request Editor, API Collections, rewrite rules, shortcuts, settings, and storage |
| [Python Plugin Guide](docs/technical/python-plugins.md) · [简体中文](docs/technical/python-plugins.zh-CN.md) | Python setup, hook API, limits, troubleshooting, and [tested examples](docs/examples/python-plugins) |
| [HTTP Rewrite Rules](docs/technical/http-rewrite-rules.md) | Matching semantics, body processing, limits, and lifecycle |
| [Traffic Metrics and HAR](docs/technical/traffic-metrics-and-har.md) | Timing and size definitions, HAR mapping, HBIN history, recording admission, and capture memory |
| [Building and Packaging](docs/technical/building.md) | Cgo policy, platform packages, signed updates, releases, and [macOS native verification](docs/technical/macos-native-verification.md) |
| [Memory Diagnostics](docs/technical/memory-diagnostics.md) | Idle memory behavior, intentionally retained state, and profiling a running instance |
| [Contributing](CONTRIBUTING.md) · [Engineering Guidelines](AGENTS.md) | Development workflow, testing, and repository contracts |
| [Changelog](CHANGELOG.md) · [Security Policy](SECURITY.md) | Release notes and vulnerability reporting |

## Data and Security

FlowLens stores its data in the operating system's user configuration directory; see [Local Storage](docs/user-guide.md#local-storage).

- Use FlowLens only on traffic and systems you own or are authorized to inspect.
- Listening on `ALL` (`0.0.0.0`) exposes the proxy to every device that can reach the host.
- Protect the MITM CA private key and remove the CA from trust stores when you no longer need it.
- Captures, history, exports, and logs can contain credentials and other sensitive data; exports are never redacted.
- Python plugins run as trusted local code with your user permissions and without a sandbox.

See [Security](docs/user-guide.md#security) in the User Guide for more details, and report vulnerabilities through [SECURITY.md](SECURITY.md) rather than a public issue.

## Contributing and Support

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing code or documentation changes. Open a [GitHub Issue](https://github.com/josexy/flowlens/issues) for reproducible bugs and focused feature requests, and strip credentials, captured traffic, private paths, certificates, and unredacted logs before posting.

## License

FlowLens is licensed under the terms in [LICENSE](LICENSE).
