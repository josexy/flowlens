# Tasks

Unless a task says otherwise, builds and tests run with `CGO_ENABLED=0` on Windows and Linux and `CGO_ENABLED=1` on macOS. View tasks are verified headlessly with `ui.NewTester`. Port tests from `/home/josexy/work/flowlens/frontend/tests` when a task names them; the old project is reference only and must not be modified.

## 1. Project setup

- [x] 1.1 Copy `backend/`, `docs/`, `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `.golangci.yml`, and `.golangci-lint-version` from the old project; verify with `diff -r` that `backend/` is identical
- [x] 1.2 Add `github.com/josexy/nuxt-ui-go v0.0.2` as a direct dependency, merge the backend's requirements into `go.mod`, and run `go mod tidy`; verify `go list ./backend/...` resolves every package and `go list -m` shows mygo v0.8.3 and nuxt-ui-go v0.0.2
- [x] 1.3 Set `mygo.json` name, `slug: flowlens`, identifier `com.josexy.flowlens`, and version `1.0.5`, and copy the application and tray icons into `resources/`; verify with `go tool mygo doctor` and `cmp` against the old icons
- [x] 1.4 Write `Taskfile.yml` with `CGO_ENABLED` set by OS (`1` with `CGO_CFLAGS`/`CGO_LDFLAGS` of `-mmacosx-version-min=12.0` and `MACOSX_DEPLOYMENT_TARGET=12.0` on macOS, `0` elsewhere) and tasks `dev`, `build`, `package`, `run`, `test`, `lint:go`, `lint:go:fix`, `modernize`, `modernize:fix`, `vet:ui`, `i18n:generate`, `i18n:check`, `version`, and `verify`; verify `task --list` shows all of them and `CGO_ENABLED` resolves to `0` on this Linux machine

## 2. Remove Wails from the backend

- [x] 2.1 Create `backend/host` with `EventBus`, an in-process implementation, a test fake, and the service lifecycle runner (start in order; on stop cancel context, then stop in reverse); verify with unit tests for ordering and unsubscribe
- [x] 2.2 Convert `setting_service`, `api_collection_service`, `rewrite_service`, and `shortcut_service` to the new `ServiceStartup(ctx)` signature and injected `EventBus`, removing `RegisterEvent`; verify those packages' tests pass
- [x] 2.3 Convert `mem_stats_service`, `history_service`, and `python_plugin_service` the same way; verify those packages' tests pass, including Python 3 integration tests without skips
- [x] 2.4 Convert `proxy_service`, and change the event batcher to carry typed payloads with estimated sizes, keeping order, bounds, and unbounded publish; verify `go test ./backend/services/proxy_service` and `go test -race` on that package
- [x] 2.5 Remove `WailsLogger` and inject the directory opener into `backend/pkg/logger`; verify logger tests pass
- [x] 2.6 Replace Wails updater types in `app_service` with local `Release` and `Progress` types behind `updateEngine`, remove window control methods, and build environment info from `runtime` and `debug.BuildInfo`; verify `app_service` tests pass with the fake engine
- [x] 2.7 Remove Wails-bound code from `backend/app` (`app.go`, `tray.go`, `updater.go`, `reset_startup_window.go`, and Wails types in `window.go`), retaining pure helpers for geometry, reset storage, and the shutdown coordinator with their tests; verify `go test ./backend/app`
- [x] 2.8 Remove Wails from `go.mod`; verify `go list -m all` has no Wails module, `grep -r wailsapp backend` is empty, and `go test ./backend/...` passes on this headless machine

## 3. macOS native integrations with cgo

- [x] 3.1 Confirm with `diff` that `provider_darwin.go`, `bridge_darwin.{h,m}`, `system_proxy_darwin.go`, `system_proxy_control_darwin.go`, and both no-cgo stubs are unchanged from the old project
- [x] 3.2 Add `cgo_guard_darwin.go` to package `main` with `//go:build darwin && !cgo && mygo_noinspector` and an undefined name that states the requirement; verify `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags mygo_noinspector .` fails naming the guard and succeeds without the tag
- [x] 3.3 Verify `CGO_ENABLED=0 go build ./backend/...` succeeds for `windows/amd64`, `linux/amd64`, `linux/arm64`, and `darwin/arm64` (the last type-checks the stubs only)

## 4. Interface foundation

- [x] 4.1 Implement `desktop/runtime` dispatcher, keyed background jobs with stale-result rejection, and window registry; verify with unit tests, including `go test -race` and closed-window delivery
- [x] 4.2 Convert the bundled WOFF2 fonts to TTF in a scratch virtual environment, add them with the OFL license to `resources/fonts`, and register them at startup; verify a test confirms the families appear in `ui.FontFamilies()`
- [x] 4.3 Implement `desktop/theme` for theme mode, palettes, fonts, and sizes; verify table tests cover all 26 palette names, unknown color fallback, and font size bounds
- [x] 4.4 Write the `scripts/` locale converter, generate `locales/en.json` and `locales/zh-Hans.json`, and configure `i18n` in `mygo.json`; verify a test confirms 1,374 messages per language and matching placeholders, and `mygo i18n check` passes
- [x] 4.5 Implement language mapping from the saved `zh`/`en` value to `mygo.App.SetLocale`, with Chinese as the default; verify Tester cases show catalog and nuxt-ui-go component text switching at runtime
- [x] 4.6 Implement the `desktop/store` root and settings store with loading, draft copies, and narrow preference writes; verify tests that newer shortcut, Python, and layout values survive a full save, porting `caTrust` and `themeColors` logic cases
- [x] 4.7 Port `format`, `headers`, `cookies`, `httpRaw`, `urlHighlight`, and `websocket` utilities to `desktop/logic`; verify ported cases from `traffic.test.ts`
- [x] 4.8 Implement `desktop/widgets` confirm dialog and notifications; verify Tester cases for dialog outcomes and notification display
- [x] 4.9 Implement the `desktop/widgets` code view, covering read-only and editable modes, wrap toggle, and the plain-text thresholds; verify Tester cases at and below 512K characters and 128K per line
- [x] 4.10 Implement the ordered key-value table and process icon widget with a 256-entry LRU cache; verify Tester cases for row order and duplicates, and port `processIconCache.test.ts`
- [x] 4.11 Implement `desktop/shortcuts` catalog, binding resolution, and policy dispatcher; verify Tester cases for `Primary` per OS, modal blocking, editable block and allow, and `when` guards

## 5. Application shell and lifecycle

- [x] 5.1 Implement `backend/app.Run` on MyGo with the instance lock before opening the database, service startup order, and failure reporting; verify tests with fakes assert the order and that a second instance never opens the database
- [x] 5.2 Implement the main window, hidden until ready, with settings-based geometry and off-screen correction; verify geometry helper tests pass
- [x] 5.3 Implement the tray with language-following labels and the tray-unavailable state; verify tests that close quits when the tray is unavailable
- [x] 5.4 Wire close and quit through `OnClose`, `OnBeforeQuit`, the shutdown coordinator, and unsaved-work guards; verify tests for hide, quit, repeated quit, and each guard outcome
- [x] 5.5 Implement custom and system title bars with draggable regions and window controls; verify Tester cases and that the mode change waits for restart
- [x] 5.6 Implement the workbench shell, including primary sidebar, resizable collapsible secondary sidebar, content host, and section selection retention; verify Tester cases for switching and collapse
- [x] 5.7 Implement the status bar with counts, layout toggle, environment information, and GitHub link; verify Tester cases
- [x] 5.8 Add the `mygo.GlobalShortcut` adapter for the `Registrar` interface and the debounced toggle; verify `shortcut_service` tests and adapter tests with a fake, including accelerator format conversion
- [x] 5.9 Replace the scaffold `main.go` and `main_test.go`; verify `task build`, `task vet:ui`, and a Tester smoke test of the workbench

## 6. Capture workspace

- [x] 6.1 Port traffic store logic (ID ordering, patches, eviction, snapshot recovery, hold-while-reading) to `desktop/store`; verify all ported `trafficTable.test.mjs` and `traffic.test.ts` cases pass
- [x] 6.2 Connect batched events to the store with 100 ms grouping and drop-triggered recovery; verify tests that drops converge on a backend snapshot and lifecycle events are retained
- [x] 6.3 Port filter logic and implement the search box and type tabs, registering Focus Filter; verify ported filter cases and Tester cases for each tab
- [x] 6.4 Implement the toolbar proxy control and status; verify Tester cases for start, stop, and start failure with a fake service
- [x] 6.5 Implement quick proxy settings and the domain filter dialog; verify Tester cases including invalid port rejection
- [x] 6.6 Implement DNS cache and managed system proxy controls with per-OS availability; verify Tester cases for duration bounds, Windows SOCKS5 disabling, and absence on Linux
- [x] 6.7 Implement the traffic table on `ui.Table` with sort cycle, resize, reorder, hide, and saved layout; verify Tester cases and incomplete-value sort cases
- [x] 6.8 Add a Tester benchmark with 5000 rows under live patches; verify visible-row builds are independent of total rows and record the result in `design.md`
- [x] 6.9 Implement single, modifier, and drag selection with edge auto-scroll and eviction handling; verify Tester cases
- [x] 6.10 Implement Show new requests while reading; verify Tester cases that rows hold position, patches apply in place, and queued entries drain in order
- [x] 6.11 Implement the row context menu actions Show, Copy (including cURL), Highlight, and Delete; verify Tester cases including unavailable request bodies
- [x] 6.12 Implement Resend, Resend N Times (1 to 1000), and the 50-entry Edit limit; verify Tester cases with a fake service
- [x] 6.13 Implement the Process column with deferred icons and remote markers; verify Tester cases
- [x] 6.14 Implement clear, session alias, and Restart Capture with save or discard; verify Tester cases

## 7. Traffic detail

- [x] 7.1 Implement the detail panel, applicable tabs, active-tab retention during live updates, and saved layout and split; verify Tester cases, porting `trafficDetailLayout.test.mjs`
- [x] 7.2 Implement Overview with timing, TLS, certificates, and process sections showing `—` for unknown values; verify Tester cases on incomplete exchanges
- [x] 7.3 Implement Headers, Trailers, Query, and Cookies panes with order, truncation, and wire-order notices; verify Tester cases with duplicates and mixed case
- [x] 7.4 Implement Raw HTTP and Raw TCP tunnel panes; verify Tester cases on HTTP/2 synthesized lines and CONNECT sources
- [x] 7.5 Implement body classification, tab selection, and copy and save of complete content, distinguishing empty from unavailable; verify Tester cases per body type
- [x] 7.6 Implement image and SVG preview with off-thread decode and failure messages; verify tests for each format using fixture images and an invalid image
- [x] 7.7 Port `hexdump`, layout, selection, and viewport logic; verify all ported `hexdump.test.mjs` cases pass
- [x] 7.8 Implement the hex view with byte selection, copy, and export; verify Tester cases and a benchmark that builds only visible rows for a 500 MB source
- [x] 7.9 Port SSE parsing and implement the SSE view with follow, pause, jump, detail, and incomplete notices; verify ported `sse.test.mjs` cases and Tester cases
- [x] 7.10 Implement the WebSocket message stream, detail dialog, and truncation notice; verify Tester cases

## 8. History and data exchange

- [x] 8.1 Implement history listing, opening, refresh, delete, and Clear All; verify Tester cases that unsupported files are preserved
- [x] 8.2 Port category logic and implement the host and process tree with search and table filtering; verify ported category cases and Tester cases
- [x] 8.3 Implement HAR import through the picker and table drop with progress and result reporting; verify tests for sequential import, partial failure, cancel, and drop indication
- [x] 8.4 Implement CSV, HAR, and per-message export with ID order, a stable scope, and per-format availability; verify tests against a temporary directory
- [x] 8.5 Verify compatibility using HBIN v1 and v2 fixtures and a settings database produced by the old build, confirming they load unchanged

## 9. Request workspace

- [x] 9.1 Port request editor state and query synchronization; verify all ported `requestEditorState.test.mjs` cases pass
- [x] 9.2 Implement workspace tabs with drag reorder, close variants, unsaved markers, and guards, registering New, Close, Next, and Previous Tab; verify Tester cases
- [x] 9.3 Implement URL, method, query, ordered headers, and cookies editing with validation; verify Tester cases that displayed order reaches the send request
- [x] 9.4 Implement body editors for None, JSON, Text, XML, URL-encoded, Form Data, and File with file drop; verify Tester cases including a drop on one row only
- [x] 9.5 Implement proxy, protocol, redirect, TLS verification, TLS profile, and HTTP/2 fingerprint settings; verify Tester cases and defaults for older saved requests
- [x] 9.6 Implement send, stop, streaming response, errors, and timing using the shared detail viewers, registering Send; verify tests with a local test server for completion, cancel, and stream
- [x] 9.7 Implement Edit from captured traffic, including body recovery; verify tests that header order and cached bodies carry over
- [x] 9.8 Implement Python plugin switches, the current-request script, and the console with reset of switches on reopen; verify Tester cases and ported `pythonConsole.test.ts`
- [x] 9.9 Implement the WebSocket Client connection, ordered headers, message sending, and reconnect; verify tests with a local WebSocket server

## 10. API Collections

- [x] 10.1 Port collection state and implement the tree with refresh, expand and collapse, drag moves, rename, copy, and delete; verify Tester cases including rejecting a move into a descendant
- [x] 10.2 Implement multi-select delete; verify Tester cases
- [x] 10.3 Implement the save dialog and linked saves, registering Save for editor tabs; verify Tester cases for new saves, linked updates, and the no-folder prompt
- [x] 10.4 Implement reopening requests; verify round-trip tests of every saved field with the script switch off

## 11. Rewrite Rules

- [x] 11.1 Port rule state and draft logic; verify all ported `rewriteRules.test.mjs` cases pass
- [x] 11.2 Implement the rule list with the master switch, per-rule switches, drag and arrow reorder, and load-error states; verify Tester cases that toggles save immediately
- [x] 11.3 Implement the rule form with methods, pattern, redirect, Keep current Host, and ordered operations; verify Tester cases including invalid fields
- [x] 11.4 Implement Match test and the regex preview with match count and inline diff; verify Tester cases including a large sample and failed result
- [x] 11.5 Implement Save, Revert, conflict handling, and the delete and quit guards with save, discard, and cancel; verify Tester cases and the quit guard wiring

## 12. Python Plugins

- [x] 12.1 Port plugin state and implement the list with search, status, enable, drag order, create, and open directory; verify Tester cases
- [x] 12.2 Implement file browsing and editing with create, rename, delete, path validation, and Save; verify Tester cases including a path escape
- [x] 12.3 Implement validate, reload, metadata editing, and delete; verify Tester cases
- [x] 12.4 Implement the parameters and rules panes with drag ordering; verify Tester cases including non-object JSON
- [x] 12.5 Port the API catalog and provide completions and hovers including signatures in both languages; verify tests that API items and signatures are offered
- [x] 12.6 Implement API help with search and copyable complete system prompts per scope; verify tests of the prompt for both scopes and languages

## 13. Settings

- [x] 13.1 Implement the Settings window with six sections, a single instance, draft editing, and the unsaved guard; verify Tester cases for navigation and close confirmation
- [x] 13.2 Implement General settings with font pickers and preview, sizes, theme color live preview and discard, title bar, close behavior, and pointer cursor; verify Tester cases
- [x] 13.3 Implement proxy connection, upstream proxy, and process attribution settings with restart prompts; verify Tester cases including invalid custom upstream
- [x] 13.4 Implement CA modes, certificate details, extra roots, client certificates, Generate and Regenerate, and current-user trust states per OS; verify Tester cases for every trust status and custom mode
- [x] 13.5 Implement Python settings including trusted code confirmation, detection, test, timeout, and status; verify Tester cases
- [x] 13.6 Implement the Shortcuts section with search, record, conflict replacement, clear, restore, and runtime status; verify Tester cases, and update the user guide's shortcut wording
- [x] 13.7 Implement Logs settings; verify Tester cases that level changes apply immediately
- [x] 13.8 Implement Storage settings with retention, disk usage, cleanup actions, and the Restore Initial State entry; verify Tester cases for each cleanup confirmation

## 14. Updates and Restore Initial State

- [x] 14.1 Implement `mygoEngine` with phase inference and cancellation, and `manualEngine` using GitHub Releases; verify fake-backed tests for phases, delta restart, cancel, and manual fallback, removing `self_update.go`
- [x] 14.2 Implement the Software Update window with all phases, Markdown notes, background and cancel choices, and close handling; verify Tester cases, porting `updater.test.mjs`
- [x] 14.3 Implement status bar update states and guarded restart; verify tests that dirty settings and rewrite drafts block restart and that restart failure keeps the app running
- [x] 14.4 Implement the Restore Initial State request flow with operation blocking; verify tests that CA removal failure leaves data intact
- [x] 14.5 Implement the separate reset progress window with stages, Retry, and Exit; verify Tester cases and that the existing reset storage tests pass

## 15. Memory Statistics

- [x] 15.1 Implement monitoring controls, the bounded sample buffer, and the live chart with metric sections; verify Tester cases for interval change, clear, no data, and retained sample limits

## 16. Release and documentation

- [x] 16.1 Create `CHANGELOG.md` with a section for the first MyGo release, keep the application version only in `mygo.json`, and read MyGo's embedded version for application metadata, updates, and HAR exports; verify runtime version use and reject mismatched release versions before building
- [x] 16.2 Rewrite `.github/workflows/ci.yml` to run `task verify` on Linux and macOS runners and cross-compile Windows; verify with `actionlint` or a local dry run of each command
- [ ] 16.3 Add `updates` to `mygo.json` with a public key from `mygo keygen`, supplied by the maintainer, and rewrite `.github/workflows/release.yml` for the specified artifacts, building macOS targets on macOS with `CGO_ENABLED=1` in a separate `mygo build` from Windows and Linux, and failing when `MYGO_UPDATER_PRIVATE_KEY` is absent; verify with a local unsigned `mygo build` and a check of the missing-key failure
- [x] 16.4 Rewrite `README.md`, `AGENTS.md`, `CONTRIBUTING.md`, and `build/` guidance for the new stack, and remove Wails, Node, Monaco, and server-mode references; verify with `grep -ril 'wails\|npm \|monaco'` over docs and by running each documented command

## 17. Integration checks

- [x] 17.1 Run `task verify` and `go test -race ./desktop/... ./backend/pkg/process_attribution ./backend/services/proxy_service`, confirm all pass, and run `openspec validate migrate-to-mygo-native-ui --strict`
- [x] 17.2 Build every Windows and Linux release target from Linux with cgo disabled, using `mygo build` or `go build`, and confirm each binary is produced
- [ ] 17.3 On a Linux desktop session, run the app against a copy of a real data directory and check windows, tray, Chinese input, file drop, capture, and shutdown; record results
- [ ] 17.4 On Windows, check title bar, tray, global shortcuts, managed system proxy, CA trust, IME, and the NSIS install; record results
- [ ] 17.5 On macOS, run `mygo build` for `darwin/arm64` and `darwin/universal` with `CGO_ENABLED=1`, confirm the universal binary contains both architectures, run `CGO_ENABLED=1 go test github.com/josexy/nuxt-ui-go`, and check process attribution and icons, managed system proxy, CA trust, traffic lights, and the DMG; record results
- [ ] 17.6 Publish two prerelease versions signed with the update key and confirm self-update, tamper rejection, and the manual path; record results

## Workflow follow-up

- Archive the change after implementation and review are complete.
