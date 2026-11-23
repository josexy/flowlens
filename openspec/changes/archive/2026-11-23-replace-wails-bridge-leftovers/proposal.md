# Proposal

## Why

The MyGo migration ported the Wails bridge's shapes along with its features. Services still talk to the views through string-named events with `any` payloads, the store copies those names as strings, request streams are routed by session ID through a global channel, state is cloned and compared by JSON round trips, and copy is looked up by building catalog keys at run time. In a single Go process these are workarounds for a boundary that no longer exists. Some have already broken behavior:

- Nothing listens to `app:local-data-cleared` anymore. After **Clear cache** or **Clear all data**, the history list, open request files, and process icons go stale until restart.
- Restart reasons are phrases such as `"proxy mode changed"`. Building catalog keys from them produces missing keys, so the saved-settings notice shows `settingsRestartReasonProxy mode changed` instead of translated text.

MyGo's own guidance is to design Go-first APIs, not string channels: native views change state through `Window.Update` and read Go values directly.

## What Changes

- **Restore local-data clearing in the interface.** Clearing cache or all data returns a typed result. The stores apply it on the main thread: they clear the process icon cache, reload or reset the history list, and drop request-editor references to files in the cleared draft cache. The `map[string]any` event that nobody receives is removed.
- **Typed service events.** The string-named `serviceruntime.EventBus` (Wails' `Emit(name, data...)` and `On(name)` shape) is replaced by typed topics that each service owns. Stores subscribe without type assertions or copies of event-name strings. Dead bridge events are removed:
  - the `proxy:error` listener, which has no publisher;
  - `app:shortcuts-changed`, which has no listener;
  - `request-editor:file-drop` with `EmitRequestEditorFileDrop` and `RequestEditorFileDropEvent`, since file drop is native through `DroppedFiles`.

  Update restart requests and failures pass between `internal/app` and `app_service` as direct calls instead of bus events with string payloads.
- **Typed live-event batches.** `FrontendEventBatch`, with string event names and `any` data, becomes a typed batch of a closed set of event kinds, with drops counted per kind. Payload sizes are counted per kind instead of by a reflection walk that estimates JSON text. The traffic reset carries a typed capture generation. `frontend*` names are retired. Limits (16 ms flush, 512 events, 4 MiB), ordering, claims, and drop recovery are unchanged.
- **Operation-scoped request streams.** HTTP Request Editor streams and WebSocket Client sessions deliver events to a sink bound when the operation starts. This removes the store's buffers and ignore lists for events whose session ID it does not know yet. Session IDs remain handles for Stop, Send, and Disconnect. WebSocket event kinds and connection states become typed constants.
- **Coalesced plugin console output.** Python plugin log lines reach the main thread in batches instead of one `Window.Update`, and one full rebuild, per line. No output is dropped.
- **Typed copy for service codes.** Restart reasons, shortcut apply errors, shortcut warnings, shortcut runtime states, and palette names become typed values. Exhaustive switches map them to generated locale accessors, with a fallback for unknown codes. The dynamic `message()`/`camel()` catalog lookups in Settings are removed.
- **Go-native copies and comparisons.** `setting_service` provides a typed deep `Clone` that replaces JSON round-trip copies in the service and the settings store. Request-tab unsaved-state detection compares typed snapshots instead of JSON-encoding every tab, body text included, on each check.
- **In-process quit guards.** The rewrite quit guard drops its string request IDs, string decisions, JSON tags, and per-frame copying of dirty state into a mutex. Guards read view state on the main thread and receive typed decisions. The Settings guard stops mirroring dirty state into an atomic.
- **Native numeric IDs.** `DeleteTraffic` takes `[]uint64` instead of JavaScript-number `[]int64`.
- **Ported helpers and lost checks.** Some ported frontend helpers have no native caller. Each is either deleted or made the single implementation where a view re-derived the same rule; for example, the Settings CA trust panel duplicates `logic.DeriveCATrustState`. The shared header-size fixture check that the TypeScript tests used to run against the Raw panel is restored as a Go test in `internal/desktop/logic`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `application-settings`: Clearing cache or all data also refreshes the history list, open history, request file references, and process icons in every window. Restart reasons appear as translated text.
- `service-event-delivery`: Drops are counted per event kind. Request stream and WebSocket Client events are scoped to the operation that started them. Plugin console output is delivered in batches without loss.

## Scope, Non-goals, and Completion

**Non-goals:**
- Persisted and external formats do not change: settings and API Collection JSON, the request draft cache, HBIN v1–v3, HAR, and the Python Worker protocol, along with the JSON tags they need. Base64 stays where those formats or "Copy as cURL" need it.
- The traffic patch model, revision-based staleness, the store's table batch and statistics throttles, and the batcher's limits stay as they are.
- The custom title bar modes, saved key names such as `Esc` and `Spacebar` in shortcut bindings, and Wails-era product behavior specified in `openspec/specs/` are kept.
- No general dead-code pruning outside bridge leftovers. Code that only looks unreachable on Linux, such as Darwin-only trust and PCB code, and unused HAR or codec helpers stays.

**Completion:**
- No string-named service event and no `any` payload assertion remains between services and stores.
- No `frontend*` identifiers or "frontend" comments remain in Go code.
- No `locales.Catalog.Format` call takes a key built at run time.
- No JSON round trip is used for cloning or unsaved-state detection.
- The scenarios in the delta specs are covered by tests.
- `task verify` passes.
- `openspec validate replace-wails-bridge-leftovers --strict --no-interactive` passes.

## Impact

- **Owners:**
  - `internal/serviceruntime`: typed topics replace the bus; `bustest` is replaced.
  - `proxy_service`: live batch, stream sinks, clear result, `DeleteTraffic`, restart reasons.
  - `python_plugin_service`, `app_service`, `rewrite_service`, `mem_stats_service`, `shortcut_service`, and `setting_service` (`Clone`, typed codes).
  - `internal/desktop/store`: event router, traffic, workspace, proxy, updater, memstats, settings, history.
  - `internal/desktop/settings`, `rewrite`, `widgets` (process icon cache), and `logic`.
  - `internal/app`: wiring, quit guards, update restart.
- **Platforms:** No platform-specific behavior changes. Quit guards keep their behavior on every platform. The Settings window, the tray, and quitting on Windows, macOS, and Linux keep the same flows.
- **Persistence and protocols:** None. In-process Go APIs change only.
- **Docs:** In `AGENTS.md`, the module table entry for `internal/serviceruntime` and the `EventBus.On` contract now describe typed topics. The user guide's Local Storage section notes what cleanup refreshes in open windows. 2.0.0 is already tagged, and this change does not bump the version in `mygo.json`. The two user-visible fixes go under an unreleased heading at the top of `CHANGELOG.md`, which the next release renames to its `## <version>`.
- **Dependencies:** None.
