# Changelog

Each `## <version>` section becomes the release notes that the Software Update window shows for that version.

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
- AppImage, RPM, and portable ZIP packages are no longer published. Windows uses the installer; Linux uses the Debian package or the tar archive.
- Histories saved by this version use a new file format and need 2.0.0 or later to open. Histories saved earlier still open.
