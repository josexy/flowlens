# Proposal

## Why

FlowLens is a Wails v3 app whose interface is a Vue 3 / Nuxt UI / Tailwind single-page app inside a system WebView. That adds a Node.js toolchain, generated TypeScript bindings, a JSON bridge for every traffic event, and a WebView runtime, and it splits application state between Pinia stores and Go services. Rebuilding the interface on `github.com/egoist/mygo` native UI with `github.com/josexy/nuxt-ui-go` makes FlowLens a single-language Go application with in-process calls, while reusing the existing Go backend.

## What Changes

- Rebuild the entire interface as native UI in Go: workbench shell, capture, traffic detail, history, categories, HTTP Request Editor, WebSocket Client, API Collections, Rewrite Rules, Python Plugins, memory statistics, Settings, Update, and Restore Initial State windows. All current features are in scope (full parity); no new product features are added.
- Copy `backend/` from the existing project at `/home/josexy/work/flowlens` into this repository under the same module path (`github.com/josexy/flowlens`), leaving that project untouched.
- Remove Wails from the backend: services stop importing `github.com/wailsapp/wails/v3`, and publish events through a small host interface. The application composition root (`backend/app`) is rewritten on MyGo for windows, tray, single instance, global shortcuts, file drop, and graceful shutdown.
- Port frontend-only logic (traffic ordering and patching, filters, hexdump, SSE parsing, headers and cookies utilities, formatting, request-editor state, shortcut bindings) from TypeScript to Go, along with its tests.
- Replace Monaco with `ui/editor`, uPlot with charts, TanStack Virtual with native virtual lists, `marked`/DOMPurify with `ui/markdown`, and vue-i18n with generated MyGo catalogs for Chinese and English.
- Replace the three macOS files that use cgo with `purego` implementations, because the new stack builds with `CGO_ENABLED=0` and those features otherwise compile to "unavailable" stubs.
- Replace the build system with a Go-only Taskfile using the MyGo CLI. Node.js, npm, Vite, and the Wails CLI are no longer needed.
- **BREAKING**: Self-update moves to MyGo's Ed25519-signed manifests. Installed 1.0.x copies cannot read them and need one manual install of the first MyGo release.
- **BREAKING**: Release artifacts follow `mygo build` output (macOS `.dmg`, Windows NSIS installer, Linux `.deb`, `.tar.gz`, and `PKGBUILD`). AppImage, RPM, and the portable Windows ZIP are no longer produced.
- **BREAKING**: The Wails-template `task server` and Docker build tasks are removed; FlowLens is desktop-only.

User data remains compatible: the `FlowLens` storage directory, SQLite schema, settings, HBIN history files, and Python plugin files are reused unchanged, so the new build replaces the old one in place.

## Capabilities

### New Capabilities

- `desktop-shell`: Workbench layout, application windows, tray, single instance, title bar modes, close and quit behavior, file drop, and the Restore Initial State startup flow.
- `service-event-delivery`: How backend services run without a UI framework and deliver bounded, ordered live events to the interface without blocking it.
- `appearance`: Light, dark, and system themes, primary and neutral palettes, font settings, and live preview.
- `localization`: Chinese and English interface text and runtime language switching.
- `keyboard-shortcuts`: In-app command bindings, scoping rules, and optional system-wide shortcuts.
- `traffic-capture`: Proxy control toolbar and the live traffic table, including filtering, columns, selection, and row actions.
- `traffic-inspection`: The traffic detail panel, including headers, cookies, body viewers, hex, SSE, WebSocket messages, TLS, timing, and process details.
- `traffic-history`: Saved histories, categories, HAR import, and HAR/CSV/message export.
- `request-workspace`: Editor tabs, HTTP Request Editor, WebSocket Client, resend flows, and the Python console.
- `api-collections`: The API Collection tree and saving and reopening requests.
- `rewrite-rules`: The rule editor, draft handling, match test, and regex preview.
- `python-plugins`: The plugin workbench, parameters, rules, packages, and API help.
- `application-settings`: The Settings window, including proxy, certificates, Python, logs, and storage, with dirty-state handling.
- `application-updates`: Update checks, signed installation, progress, restart guards, and manual fallback.
- `memory-statistics`: The live memory statistics view.
- `build-and-release`: Taskfile tasks, CGO-free builds, verification tasks, version metadata, packaging, and CI.

### Modified Capabilities

None. This project has no existing specs.

## Impact

- **Code**: New Go UI packages replace the old `frontend/` tree (200 Vue/TS source files, about 46,000 lines). About 43,000 lines of backend Go are reused; 27 backend files import Wails today and change, with the largest edits in `backend/app`, `app_service/update_controller.go`, and `proxy_service/frontend_event_batcher.go`.
- **Dependencies added**: `github.com/josexy/nuxt-ui-go v0.0.2` becomes a direct dependency beside `github.com/egoist/mygo v0.8.3`; `github.com/ebitengine/purego` becomes direct for macOS code.
- **Dependencies removed**: `github.com/wailsapp/wails/v3`, Node.js, and all npm packages.
- **Runtime**: Linux needs GTK 3 and `libayatana-appindicator3` for the tray; a desktop session is required on all platforms.
- **Release operations**: A new update signing key pair is required (`MYGO_UPDATER_PRIVATE_KEY` in CI). `.github/workflows` are rewritten.
- **Verification limits**: The current development machine is headless Linux arm64, so real-window, Windows, and macOS behavior must be confirmed on those platforms; `ui.Tester` covers view logic.
