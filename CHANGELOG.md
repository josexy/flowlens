# Changelog

Each `## <version>` section becomes the release notes that the Software Update window shows for that version.

## 2.0.2

- Capture and history tables, detail viewers, and window icons use less memory.
- History details show headers right away and load bodies only for Raw, Body, or WebSocket. Switching rows no longer flashes a blank panel or a brief spinner.
- Fixed zstd decoding: a small response could hold up to 512 MiB of memory, a corrupt body could hang a request editor send, and disconnecting an encoded SSE stream could reuse a decoder still in use.
- zstd bodies that declare a window over 8 MiB are no longer decoded: capture forwards them unchanged, and request editor sends and body rewrite rules report a decoding error.
- API Collections show WebSocket requests as WS instead of GET.
- Switching traffic tabs or closing history keeps the current sidebar section and its collapsed state.

## 2.0.1

- Pause recording while the proxy keeps forwarding traffic. Stop Proxy is now in the proxy button's menu.
- Tighter spacing in every window shows more traffic and editor content.
- Clearing cache files or all data in `Settings > Storage` refreshes open windows right away.
- Restart notices name the changed settings in the current language.
- Fixed proxy restarts failing with "address in use", and flicker in the traffic table and proxy controls.
- Linux packages no longer depend on WebKitGTK.

## 2.0.0

FlowLens is now a native desktop application built on MyGo instead of a web view.

- Every window, including capture, the request editor, rewrite rules, Python plugins, API Collections, settings, and Software Update, is drawn natively.
- Updates are signed: an update installs only after its signature matches the key built into the application.
- macOS keeps process attribution with icons and the managed system proxy. Windows and Linux builds no longer need cgo.
- A response in the request editor now shows the time the client measured for it.
- The "Use pointer cursor" setting is removed: controls show the system's default cursor. Your other settings are kept.
- Traffic tables use native selection: Ctrl/Cmd-click adds or removes rows and Shift-click selects a range. Enter, double-click, or Show opens details; Close details preserves selection. This replaces the former Shift-toggle and Ctrl/Cmd-detail gestures.

### Installing this version

- Versions 1.0.x cannot update to this release on their own. Install it once from the release page; your data is picked up.
- AppImage and portable ZIP packages are no longer published. Windows uses the installer; Linux offers Debian and RPM packages or the tar archive. Linux packages require GTK 3, without WebKitGTK.
- Histories saved by this version use a new file format and need 2.0.0 or later to open. Histories saved earlier still open.
