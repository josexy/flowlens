# FlowLens User Guide

This guide covers day-to-day FlowLens behavior and operational details. For installation and development setup, start with the project [README](../README.md).

## Workspaces and Request Editing

- Capture, history, categories, API collections, Python plugins, and memory statistics share the same workbench shell.
- HTTP Request Editor and WebSocket Client tabs can be saved into API Collection folders from the tab context menu.
- Saving an editor tab already linked to an API Collection entry updates that request directly.
- HTTP and WebSocket requests support `No Proxy`, `System Proxy`, `MITM Proxy`, and `Custom Proxy` modes.
- HTTP requests can use automatic protocol negotiation or force `HTTP/1.1` / `HTTP/2`.
- HTTPS and WSS requests can select Go, Chrome, Firefox, Safari, Edge, iOS, Android 11 OkHttp, or randomized-ALPN ClientHello profiles.
- HTTP requests can also use a canonical four-part HTTP/2 fingerprint in `SETTINGS|WINDOW_UPDATE|PRIORITY|PSEUDO_HEADER_ORDER` form. Explicit HTTP/1.1 requests ignore this field.
- API Collection requests retain protocol, TLS profile, fingerprint, ordered headers, body, and current-request script source where applicable. Script enable switches remain off when a request is reopened.

Request header rows are sent in their displayed order. The URL remains the routing source of truth; the backend controls pseudo-headers, `Host`, body framing, generated content types, and the fallback `User-Agent`. Invalid layouts are rejected instead of being silently reordered.

HTTP Request Editor can run global and current-request Python hooks. Configure Python under `Settings > Python` and see the [Python plugin guide](technical/python-plugins.md) for setup, API details, limits, and examples.

## Keyboard Shortcuts

Open `Settings > Shortcuts` to search commands, record or clear a binding, and restore one or all known commands.

`Primary` means Command on macOS and Ctrl on Windows/Linux. Menus and tooltips resolve labels from the active saved configuration.

| Command | Default binding |
| --- | --- |
| Save the active dirty request or settings | `Primary+S` |
| Open settings | `Primary+,` |
| Switch workbench sections | `Primary+1` through `Primary+6` |
| New HTTP / WebSocket request | `Primary+N` / `Primary+Shift+N` |
| Close the active closable tab | `Primary+W` |
| Next / previous tab | `Control+Tab` / `Control+Shift+Tab` |
| Send the active request | `Primary+Enter` |
| Focus the active capture/history filter | `Primary+F` |

Application shortcuts work only while a FlowLens window has focus and an enabled handler exists for the current page, tab, modal, and editable control. Inputs and Monaco retain their normal keyboard behavior when no command can run.

Optional system-wide shortcuts use Wails `GlobalShortcut`. They are disabled by default and limited to showing the main window and toggling the proxy. Registration may require operating-system approval or conflict resolution; Wayland portals may choose the final accelerator.

Version 1 supports one single-step binding per command. Multi-step chords and alternate bindings are not supported.

## System Tray and Windows

- Closing the main window hides FlowLens to the system tray instead of quitting.
- Use the tray menu to restore the main window or fully close the application.
- FlowLens runs as one instance per build mode; starting it again restores and focuses the existing main window.
- Window size, position, and maximized state are persisted when possible.
- Settings use a dedicated window and confirm unsaved changes before closing.
- Switching between the custom and native title bar requires an application restart.

## Managed System Proxy

The toolbar system-proxy control can temporarily point supported operating-system proxy settings at the active FlowLens listener.

- Windows supports managed HTTP/HTTPS proxy settings. SOCKS5 is disabled because modern WinINet clients do not reliably support a SOCKS system proxy.
- macOS supports managed HTTP/HTTPS and SOCKS5 settings.
- Linux does not currently expose the managed system-proxy control.
- Listener host `ALL` (`0.0.0.0`) maps to `127.0.0.1` for local system-proxy settings.
- If another application or the user changes the proxy while FlowLens manages it, FlowLens does not overwrite that newer configuration during restoration.

The original proxy snapshot is kept in process memory and is restored during a normal shutdown. A crash, forced termination, or power loss can leave the system proxy pointing at FlowLens. Restore it manually through the operating-system network settings; restarting FlowLens cannot recover a snapshot lost with the previous process.

## Timing, Sizes, and HAR Export

FlowLens records request-write start/end and response first-byte/body-end events at the upstream transport boundary. Live traffic and HBIN history retain microsecond Unix timestamps, terminal states, logical header field-line sizes, and encoded Body sizes.

- Retries replace stale attempt data.
- Failed, canceled, pending, or incomplete exchanges retain unknown values instead of synthetic completion data.
- Detail views may add the logical HTTP/1 request or status line to displayed totals.
- Persisted metrics and HAR `headersSize` remain field-line totals and do not represent TCP/TLS bytes, HPACK compression, or HTTP/2 frame sizes.

Capture, history, and traffic context menus can export all or selected entries as HAR 1.2. HTTP/HTTPS exchanges and WebSocket upgrade handshakes are exportable; Raw TCP tunnels and WebSocket frames are skipped.

HAR output is streamed to a temporary file in the destination directory and atomically replaces the target after completion. Missing Body-cache payloads are counted without dropping otherwise exportable entries.

HAR files are not automatically redacted. They can contain authorization headers, cookies, request/response bodies, and process paths; review them before sharing.

## Process Attribution and Icons

Process attribution is enabled by default under `Settings > Proxy` and applies only to direct local connections accepted by FlowLens. Remote clients are marked as remote and skip operating-system lookup.

- Lookup is asynchronous and never blocks connection acceptance.
- The traffic list displays the app icon and display name when available.
- Details include process name, PID, app ID, executable path, and lookup status.
- Text metadata may arrive before the icon.
- Process metadata is stored with history; icons are loaded separately.
- Missing permissions or metadata fall back to a placeholder without interrupting capture.

The backend caches identities by PID plus process start token so PID reuse does not inherit stale metadata. Extracted PNG files are stored under `cache/process-icons`. Each frontend window keeps an independent LRU cache of up to 256 successful icon data URLs and merges concurrent requests for the same icon.

Storage cleanup invalidates backend files and frontend memory entries. If a PNG is deleted externally, the backend can recreate it on the next read while the process identity is still available. A frontend memory-cache hit does not contact the backend, so recovery waits until the entry is cleared, evicted, or the window reloads.

## Runtime Logging

- Logging starts enabled at `info` level.
- The current file is `flowlens.log` under the application configuration `logs` directory.
- Files rotate at 10 MB and retain up to five backups.
- Development/debug builds also log to the console; production builds use files by default.
- `Settings > Logs` controls enablement, level, clearing, refresh, and opening the log directory.

Logs can contain target hosts, paths, API Collection names, and local file paths. Review or redact them before sharing.

## Settings and Local Storage

- `Settings > General`: app/code fonts and title-bar mode.
- `Settings > Proxy`: process attribution, upstream proxy, CA paths, extra trusted roots, and per-host client certificates.
- `Settings > Python`: interpreter and global Python plugin behavior.
- `Settings > Shortcuts`: application bindings and optional global shortcuts.
- `Settings > Logs`: logging state and file actions.
- `Settings > Storage`: Body-cache threshold, WebSocket retention, history retention, cache cleanup, and local data statistics.

`No Proxy` sends traffic directly, `System Proxy` resolves the operating-system or environment proxy, and `Custom Proxy` uses the configured URL.

FlowLens stores `flowlens.db`, API Collection managed files, history, logs, and caches under the user configuration directory resolved by the backend. API Collection Body files are stored under `api_collections/files`; process icons are stored under `cache/process-icons`.

On Unix-like systems, managed directories are tightened to owner-only access (`0700`) and database, history, log, and Body-cache files to `0600`. Windows relies on the access controls of the current user's configuration directory.

Storage cleanup can remove caches alone or caches together with the current capture and readable saved history. Cache-only cleanup first archives a non-empty current capture, then clears the live list. Unsupported or unindexed history files are preserved to avoid destructive format loss. Cleanup does not delete `flowlens.db`, API Collections, preferences, certificates, or logs.

The current history layout is HBIN v1. Earlier development-only layouts are unsupported; unknown versions are skipped without being deleted.

## MITM Certificates

HTTPS capture requires a locally trusted CA certificate. Generate or inspect the FlowLens CA under Settings, add it to the trust store used by the client application, then restart the proxy service when required.

Default proxy settings:

| Setting | Default |
| --- | --- |
| Mode | HTTP |
| Host | `127.0.0.1` |
| Port | `8080` |
| Process attribution | Enabled |
| Upstream proxy | System Proxy |
| CA certificate | `certs/ca.crt` |
| CA key | `certs/ca.key` |

Changing the listener, proxy mode, or CA may require rebinding or restarting the running proxy service.

## Security

- Use FlowLens only for traffic you own or are authorized to inspect.
- Listening on `ALL` exposes the proxy to reachable devices; apply suitable firewall and network controls.
- Anyone with the generated CA private key can impersonate endpoints trusted by that CA. Keep it private and remove or regenerate it when necessary.
- Captures, history, HAR, API Collections, logs, cached bodies, and process metadata may contain sensitive information.
- Python plugins run as trusted local code. Interpreter isolation is not a security sandbox.

Report vulnerabilities according to [SECURITY.md](../SECURITY.md), not through public issues.
