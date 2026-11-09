# Changelog

Each `## <version>` section becomes the release notes that the Software Update window shows for that version.

## 1.1.0

FlowLens is now a native desktop application built on MyGo instead of a web view.

- Every window, including capture, the request editor, rewrite rules, Python plugins, API Collections, settings, and Software Update, is drawn natively.
- Updates are signed: an update installs only after its signature matches the key built into the application.
- macOS keeps process attribution with icons and the managed system proxy. Windows and Linux builds no longer need cgo.
- A response in the request editor now shows the time the client measured for it.

### Installing this version

- Versions 1.0.x cannot update to this release on their own. Install it once from the release page; your data is picked up.
- AppImage, RPM, and portable ZIP packages are no longer published. Windows uses the installer; Linux uses the Debian package or the tar archive.
