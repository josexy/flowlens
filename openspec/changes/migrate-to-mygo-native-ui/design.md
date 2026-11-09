# Design

## Context

See `proposal.md` for motivation and `specs/` for required behavior. This section records only the facts that shape the approach, all checked against the pinned sources.

**Old project** (`/home/josexy/work/flowlens`, module `github.com/josexy/flowlens`):

- `frontend/src`: 200 Vue/TS files, about 46,000 lines. `frontend/tests` holds about 7,500 lines of logic tests.
- `backend/`: about 43,000 lines plus 37,000 lines of tests. 27 files import Wails, but services use a narrow surface: `Event.Emit`/`On`, `RegisterEvent`, `ServiceStartup`/`ServiceShutdown`, `Env.Info`, `Window.*`, and the updater.
- Shortcuts already go through a `Registrar` interface, and the updater through an `updateEngine` interface with a fake in tests.
- Three macOS files use cgo, and they are the only cgo in the project. With cgo off they compile to stubs reporting unavailable (`provider_darwin_nocgo.go`, `system_proxy_darwin_nocgo.go`).

**This repository** holds a `mygo init` scaffold with module `github.com/josexy/flowlens` already set, and no commits.

**Library constraints** (mygo v0.8.3, nuxt-ui-go v0.0.2):

| Fact | Consequence |
| --- | --- |
| Views rebuild from app state on the main thread; `Element` values expire each pass; `Window.Update` publishes changes | All view state lives in Go structs; workers never touch it directly |
| `nui.TableState.SetRows` copies all rows, validates IDs through a map, and re-sorts | Too costly per batch for the live 5000-row table |
| `ui.Table` builds only visible rows and supports column resize, reorder, sort, and `ListState.Columns` persistence | Suitable base for the traffic table |
| `ui/editor` has highlighting for JSON, XML, HTML, JS, CSS, Python and more, plus find, folding, completions, hovers, and a diff view, but no signature help | Replaces Monaco; signature help needs a substitute |
| `ui.RegisterFont` accepts TrueType/OpenType only | The bundled WOFF2 fonts must be converted |
| `mygo.Updater.Check` returns `ErrUpdatesDisabled` when the install location is not writable | Non-self-updating installs need a separate release check |
| `Update.Install` reports only downloaded bytes, then verifies and swaps | Verify and Prepare phases must be inferred |
| `OnWindowShortcut` fires while a text input has focus unless the input consumes that key, and `Context.FocusTakesText` reports whether the focused element takes text | The dispatcher evaluates editable policy with `FocusTakesText` |
| Release notes come from `CHANGELOG.md` | The repository needs a changelog |
| The MyGo CLI defaults `CGO_ENABLED` to `0` only when it is unset, documents `1` for apps that need cgo, and applies one value to every target of a `mygo build` | macOS can build with cgo, as a separate build from Windows and Linux |

**No web rendering is needed.** The only `v-html` is update notes; HTML bodies are shown as source, so `ui.WebView` is not used.

## Goals / Non-Goals

**Goals:**

- Reuse the backend with minimal, mechanical edits so existing Go tests keep passing as the regression suite.
- Keep each migration phase buildable and testable on its own.
- Keep backend packages free of UI toolkit imports, except the composition root.

**Non-Goals:**

- Pixel-identical rendering with the Vue build. Behavior matches; visuals follow nuxt-ui-go.
- Changing HBIN, SQLite, settings, or Python worker formats.
- Refactoring backend services beyond what removing Wails requires.
- Automatic migration from Wails-installed binaries. Data carries over because the storage path is unchanged.
- Modifying the old repository.

## Decisions

### 1. Repository layout

`backend/` is copied unchanged, keeping import paths valid. New UI code goes under `desktop/`:

```
main.go                   embeds resources, calls backend/app.Run
backend/app               composition root on MyGo (lifecycle, windows, tray)
backend/host              EventBus and service lifecycle interfaces (new)
desktop/runtime           main-thread dispatcher, background jobs, window registry
desktop/store             application state (replaces Pinia stores)
desktop/logic             pure logic ported from utils and composables
desktop/theme             theme building, fonts, semantic tokens
desktop/widgets           shared controls (code view, hex view, key-value table, process icon)
desktop/shortcuts         command catalog, binding resolution, dispatcher
desktop/views/...         shell, traffic, detail, history, workspace, apicollection,
                          rewrite, python, settings, update, resetstartup, memstats
locales/                  ARB catalogs and generated Go package
resources/                icon, tray icons, fonts
```

Alternative considered: merge UI into `backend/`. Rejected because `backend` would then depend on the UI toolkit, defeating headless tests.

### 2. Decouple services through `backend/host`

```go
type EventBus interface {
    Emit(name string, data ...any) bool
    On(name string, fn func(Event)) (off func())
}
```

Each service's `app *application.App` field becomes an injected `events host.EventBus`, so most call sites change only their receiver. `ServiceStartup(ctx, application.ServiceOptions)` becomes `ServiceStartup(ctx)`, `RegisterEvent` calls and `//wails:ignore` comments are deleted, and tests change mechanically. Event names are unchanged, so tests asserting them keep working.

`backend/app` runs services in registration order and on shutdown cancels the context first, then calls `ServiceShutdown` in reverse order, matching Wails. The comment at `proxy_service.go:4753` depends on that order, and the rewrite service must stay last to register.

Environment info uses `runtime` and `debug.BuildInfo`. `AppService` window controls are removed, since views call `mygo.Window` directly. `logger.OpenDir`, the only backend use of Wails' file manager call, takes a function injected by `backend/app` that wraps `mygo.Shell.OpenPath`.

Alternative considered: typed per-service interfaces. Rejected for this change because it rewrites every call site and test without changing behavior.

### 3. Live events stay batched, without JSON

`frontend_event_batcher.go` retains its order, flush interval, and bounds, and carries typed `any` values instead of `json.RawMessage`, because JSON existed only for the WebView bridge. `MaxPendingBytes` uses a size estimate per payload type. Its existing limits and `PublishUnbounded` semantics stay, so the bounded and lossless guarantees carry over; tests are adapted only where they assert JSON bytes.

The batch consumer runs on the main thread through the dispatcher. The Go store keeps the old 100 ms grouping for entry insertion to limit rebuild cost.

Alternative considered: keep JSON and decode in the UI. Rejected as needless allocation at capture rates.

### 4. Application state and threading

Each Pinia store becomes a struct in `desktop/store`, mutated only on the main thread. Rules follow `.agents/skills/mygo-maintenance`:

- `desktop/runtime.Dispatcher.Post(fn)` runs `fn` through the main window's `Window.Update` and invalidates other open windows. The main window lives for the whole process because close is intercepted.
- `runtime.Job[T]` runs a service call off the main thread and publishes its result only if it is still the latest for its key, replacing `latestOperation.ts`.
- `*ui.Context`, elements, and widget state never go into stores. Per-window widget state (scroll, focus) lives in a view struct owned by that window.

Multi-window synchronization events (`app:settings-saved` and others) disappear for Go-side consumers, because all windows share one store in one process. The Settings window edits a draft copy, preserving dirty tracking.

### 5. Traffic table uses `ui.Table` directly

The table is built on `ui.Table` with an index-based cell builder over the filtered slice, styled from nui theme tokens. `ListState.Columns` is converted to and from the saved table configuration, and hidden columns are simply omitted. `nui.Table` remains the choice for small tables such as settings lists, where its `SetRows` cost is irrelevant.

Sorting, ordering, eviction, and hold-while-reading logic is ported from `traffic-table-state.ts` and `stores/traffic.ts` into `desktop/logic`, with the existing tests ported, including the 10,000-row payload-update case.

`ListState.Key`, `FollowEnd`, and `DragSelect` replace the old table's manual scroll anchoring and drag handling: rows keep their place by entry ID as older rows are evicted, so the eviction scroll calculation is not ported.

The hex view is a `ui.List` of byte rows, which scrolls by row in `float64`, so the old segment paging that kept a WebView under its layout height limit is not ported. Keeping the reading position across width changes uses the byte offset of the first visible row (`logic.HexRowOf`).

`BenchmarkHexFrame500MB` measures one hex view frame over a 500 MB body at 0.30 ms on the same machine; `TestHexBuildsOnlyVisibleRowsOf500MB` confirms only rows in view are built.

Measured with `BenchmarkTrafficTableFrame` (headless `ui.Tester`, 1400x800, arm64, one live patch applied per frame): 2.28 ms per frame at 500 rows and 2.60 ms at 5000 rows. `TestVisibleCellBuildsDoNotGrowWithTotalRows` confirms the number of cells built per frame does not grow with the total row count.

### 6. Editors and viewers

- **Code**: one `desktop/widgets.CodeView` wraps `editor.View` for read-only and editable modes. The thresholds of 512K characters or 128K characters per line switch the buffer to plain text.
- **Python assistance**: completions and hovers come from the ported API catalog. `ui/editor` has no signature help, so signatures appear in completion details and hover text. The spec requires completion and hover, so this is within scope.
- **Images**: `ui.DecodeBitmap` runs off the main thread, cached per entry. SVG uses `ui`'s SVG support, and failures fall back to a message with Raw and Hex still available.
- **Hex**: a custom widget draws only visible rows with `ui.Painter` over a byte source, keeping byte-based selection. Decoding moves from a Web Worker to a `runtime.Job`.
- **Charts**: memory statistics uses `ui/chart` with downsampling and a bounded sample buffer.
- **Markdown**: update notes use `ui/markdown`.

### 7. Localization

A one-off converter in `scripts/` flattens the 1,374 vue-i18n keys into ARB (`locales/en.json`, `locales/zh-Hans.json`), turning dotted keys into camelCase and adding placeholder metadata for the 130 parameterized messages. There are no plurals or linked messages to convert. Output is committed and `mygo generate` produces typed functions.

The saved `zh`/`en` setting stays authoritative and maps to `zh-Hans`/`en` through `mygo.App.SetLocale`. Python API help stays as embedded per-language JSON documents, since it is structured content rather than messages.

### 8. Shortcuts

`desktop/shortcuts` ports the catalog and binding resolution. One dispatcher registers each resolved binding with `OnWindowShortcut` and evaluates `when`, modal, and editable policies before running a handler.

For editable policy, the dispatcher reads `Context.FocusTakesText`, which covers text inputs, text areas, and editors that are not read-only, so no per-widget tracking is needed.

Global shortcuts use a small adapter from `mygo.GlobalShortcut` to the existing `Registrar` interface, so `shortcut_service` and its tests are unchanged.

### 9. Windows and lifecycle mapping

| Wails | MyGo |
| --- | --- |
| `SingleInstance` + `OnSecondInstanceLaunch` | `RequestSingleInstanceLock` before opening the database, `OnSecondInstance` |
| `ShouldQuit` | `App.OnBeforeQuit` with `PreventDefault` |
| `WindowClosing` hook + `Cancel` | `Window.OnClose` with `PreventDefault` |
| `app:frontend-ready` | Create `Hidden`, show from `OnReadyToShow` |
| `SystemTray` | `mygo.NewTray`; on error, record tray as unavailable |
| Frameless custom title bar | `TitleBarHidden`, with `HideWindowButtons` and nui controls on Windows and Linux |
| `WindowFilesDropped` target attributes | `Element.DroppedFiles` on each target |
| `Dialogs`, `Browser`, `Clipboard` | `mygo.Dialog`, `mygo.Shell`, `mygo.Clipboard` |

Geometry stays in the existing settings-based persistence (`backend/app/window.go`) rather than `StateKey`, keeping saved positions and off-screen handling.

The reset progress window stays a separate process and reimplements `runResetStartupWindow` as a native view; it takes no single-instance lock because the parent holds it.

Tray failure on Linux (missing `libayatana-appindicator3`) is treated as "tray unavailable", so closing the main window quits rather than stranding a hidden process.

### 10. Updates

`updateEngine` drops Wails types for local `Release` and `Progress` types and gets two implementations:

- **`mygoEngine`**: when `mygo.Updater.Enabled()`, uses `Check` and `Update.Install` with a cancelable context, then `App.Relaunch`.
- **`manualEngine`**: otherwise, reads the GitHub Releases API for version and notes and opens the release page.

Phases are inferred: Download while bytes arrive, Verify once bytes reach the total, back to Download if progress restarts on delta fallback, and Prepare until `Install` returns. `self_update.go` is removed because `Enabled()` decides eligibility, and the reset manifest's `UpdateStaging` is left empty since staged files live beside the app.

Alternative considered: a single engine that always uses GitHub for checks. Rejected because mygo's manifests carry the signature and delta information needed for installation.

### 11. macOS keeps cgo

`provider_darwin.go`, `bridge_darwin.{h,m}`, `system_proxy_darwin.go`, and `system_proxy_control_darwin.go` are reused unchanged, and macOS builds with `CGO_ENABLED=1`. Windows and Linux stay at `CGO_ENABLED=0`. Neither MyGo nor nuxt-ui-go uses cgo itself. nuxt-ui-go's documentation says to build with `CGO_ENABLED=0`, but nothing in its code depends on that, and its test suite passes with cgo on under Linux.

A macOS build with cgo off would still compile and silently lose process attribution and the managed system proxy. To prevent that, `cgo_guard_darwin.go` in package `main` (`//go:build darwin && !cgo && mygo_noinspector`) refers to an undefined name, so a production `mygo build` for macOS with cgo off fails. The stubs stay, so non-production macOS type-checks still work from Linux. The guard does not apply to `mygo build -debug` or `MYGO_INSPECTOR=1`, which do not set the production tag and are not release builds.

One `mygo build` applies one `CGO_ENABLED` value to all its targets, so macOS is built separately from Windows and Linux. macOS builds keep the old `-mmacosx-version-min=12.0` compiler and linker flags and `MACOSX_DEPLOYMENT_TARGET=12.0`, matching MyGo's default minimum system version.

Alternatives considered:

- Port to `purego`. Rejected because it rewrites about 950 lines of working Go and 283 of Objective-C that cannot be run on the development machine.
- Check built binaries with `go version -m`. Rejected because it reads only the first architecture of a universal binary and runs after the build.

### 12. Theme and fonts

`desktop/theme` builds `nui.AppConfig` from the saved palette names (all 26 are available in nuxt-ui-go), font families, and sizes, and rebuilds it on change. WOFF2 fonts are converted once to TTF, kept in `resources/fonts` with the OFL license, and registered at startup. CJK text uses system fallback through the platform text engine.

### 13. Build, testing, and packaging

`mygo.json` gains `slug: flowlens`, identifier `com.josexy.flowlens`, version, `i18n`, and `updates` (GitHub `josexy/flowlens`). The Taskfile sets `CGO_ENABLED` by operating system (`1` on macOS, `0` elsewhere) and wraps `go tool mygo` commands.

Verification tiers:

1. `go test ./...` in each platform's cgo mode, including ported logic tests and `ui.NewTester` view tests.
2. `go tool mygo vet .` and `mygo i18n check`.
3. Cross-compilation for Windows and Linux with cgo off, and native macOS builds with cgo on.
4. Manual desktop checks on each platform, tracked as explicit tasks.

## Risks / Trade-offs

- [Migration size: about 46,000 lines of UI in one change] → Ordered phases, each with its own tests; capture ships first so the riskiest performance work is validated early.
- [nuxt-ui-go is at v0.0.2 with partial macOS verification and a known IME defect when canceling composition over a selection] → Pin the version, test Chinese input on each platform, and keep FlowLens-specific controls in `desktop/widgets` so library changes stay contained.
- [Traffic table frame cost at 5000 rows with live updates] → Benchmark in `ui.Tester` during the capture phase, before building other surfaces on the same patterns.
- [A macOS build made with cgo off would silently lose native features] → The production build guard in Decision 11 fails such a build, and the Taskfile sets cgo on for macOS.
- [macOS builds need a Mac with Xcode Command Line Tools and cannot be cross-compiled from Linux] → The release workflow already builds on a `macos-15` runner, and CI gains a macOS job.
- [nuxt-ui-go documents cgo-free builds and has not been run on macOS with cgo on, and `darwin/universal` through `mygo build` with cgo is unproven] → A final check builds both macOS targets and runs nuxt-ui-go's tests on a Mac with cgo on.
- [Headless development machine cannot exercise real windows, tray, or global shortcuts] → `ui.Tester` covers view logic; platform behavior is listed as manual checks.
- [Dropped signature help in the Python editor] → Signature text appears in completion details and hovers.
- [Installed 1.0.x users do not auto-update] → One manual install, noted in the release notes.
- [Tray and global shortcuts vary by Linux desktop] → Failures are reported and handled per the specs instead of being fatal.
- [Running the old and new builds together contends for one data directory] → They are not intended to run side by side; the migration plan states this.

## Migration Plan

1. Build in this repository until all phases pass; the old project stays untouched and remains the reference and fallback.
2. Generate the update signing key with `mygo keygen`, store the private key in CI as `MYGO_UPDATER_PRIVATE_KEY`, and place the public key in `mygo.json`.
3. Back up the `FlowLens` data directory before the first run against real data during development.
4. Publish the first MyGo build as a prerelease and verify data pickup, signed updates between two test versions, and the manual path.
5. Publish the release with notes telling 1.0.x users to install once manually.

**Rollback**: reinstall the last Wails release. Formats are unchanged, so its data remains readable.
