# Tasks

Every command runs from the repository root with `CGO_ENABLED=0`, which the Taskfile sets, unless a task says otherwise. Each group is one Conventional Commit, or a few, with one class of change per commit. Passing `go test` proves compilation and the headless behavior under test. It does not prove real-window acceptance, which is listed separately in group 10.

## 1. Restore local-data clearing in the interface

- [x] 1.1 In `proxy_service`, add `LocalDataClearResult` and `LocalDataClearScope`. `ClearCacheFiles` and `ClearCacheAndHistory` return `(LocalDataClearResult, error)`, filling the result whenever state was cleared, including with an error. Delete `emitLocalDataCleared` and `localDataClearedEventName`. Update the proxy tests that assert the old event so they assert the result for cache-only, cache-and-history, history-not-cleared, and partial failure. Verify: `go test ./internal/services/proxy_service ./internal/services/history_service`.
- [x] 1.2 Add `store.Store.ClearLocalData(scope, done)` with a `Storage` backend dependency and a `ClearProcessIcons` hook. It runs as a store job and applies the result on completion as design Decision 5 orders it, then calls `done(err)`. Add `HistoryStore` reset helpers shared with the `ClearAll` success path. Store tests must cover:
  - a cache-only clear reloads the list and keeps an existing open history;
  - a cleared history empties the list, closes the open history, selects the live capture, and cancels an in-flight list load;
  - draft-cache file references are dropped and other files kept;
  - the icon hook is called;
  - a partial failure applies the result and then reports the error;
  - the clear still applies after the Settings window closes.

  Verify: `go test ./internal/desktop/store`.
- [x] 1.3 Switch the Settings storage section from `Host.ClearCache` and `Host.ClearAllData` to `Data.ClearLocalData`, keeping its pending, done, and error states. In `internal/app`, wire `Storage` to the proxy service and `ClearProcessIcons` to the main window's `widgets.ProcessIcons.Clear`. Verify: `go test ./internal/desktop/settings ./internal/desktop/widgets ./internal/app`, `task vet:ui`, and `FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./internal/desktop/settings -run TestRender -count=1`, with the storage section unchanged in light and dark, `en` and `zh-Hans`.
- [x] 1.4 Document in `docs/user-guide.md` (Local Storage) that cleanup also refreshes the history list, drops editor references to removed draft-cache files, and reloads process icons. Add an unreleased entry at the top of `CHANGELOG.md` for the clearing fix. Verify by reading both diffs against the `application-settings` delta.

## 2. Typed copy for service codes

- [x] 2.1 Add `ProxyRestartReason`, with its current string values, to `proxy_service` and use it in `proxyRestartReasons` and `ProxyConfigApplyResult.RestartReasons`. In `shortcut_service`, add `ShortcutApplyErrorCode` and `ShortcutWarning` and use them in the result types. Verify: `go test ./internal/services/proxy_service ./internal/services/shortcut_service ./internal/services/setting_service`.
- [x] 2.2 In `internal/desktop/settings`, replace `message()`, `camel()`, and `locales.Catalog.Format` with exhaustive switches to the existing generated accessors for:
  - restart reasons;
  - shortcut apply errors, with an `Unknown` fallback;
  - shortcut warnings and runtime states;
  - palette names.

  Unrecognized restart reasons, warnings, and palettes show the raw value. Add a test that maps every declared code in `en` and `zh-Hans` and asserts no result starts with a catalog key prefix. Add a test that a save with proxy mode and port changes produces the translated reasons in order. Verify: `go test ./internal/desktop/settings ./locales`, `task i18n:check`, `task vet:ui`, and `grep -rn 'Catalog.Format' internal/desktop` returns nothing.
- [x] 2.3 Add the restart-reason fix to the unreleased `CHANGELOG.md` entry. Verify by reading the diff.

## 3. Typed service topics instead of the string bus

- [x] 3.1 Add `serviceruntime.Topic[T]` with `Publish` and `Subscribe`, plus `serviceruntime/topictest`. Port the bus tests: subscription order, one publisher's order, off during delivery, repeated off, and concurrent publish and subscribe. Verify: `go test ./internal/serviceruntime/...`.
- [x] 3.2 Give `proxy_service`, `python_plugin_service`, `app_service`, `mem_stats_service`, and `rewrite_service` their `Events` topics as listed in design Decision 1. Replace every `events.Emit` and `SetEventBus`, including the raw `"proxy:dns-cache-status"` string. Remove `rewriteservice.SetChangedHandler`. Port the service tests from `bustest` to `topictest`. Verify: `go test ./internal/services/...`.
- [x] 3.3 Subscribe `store.ProxyStore`, `UpdaterStore`, `MemStatsStore`, the plugins store, and the rewrite store to the typed topics, posting to the dispatcher. Delete their copies of event-name strings and the `proxy:error` listener. Verify: `go test ./internal/desktop/store`.
- [x] 3.4 In `internal/app`, delete the bus, `shortcutsChangedEventName`, and the bus forwarding for rewrite and plugin events. Add `AppService.SetRestartRequester` and `AppService.RestartFailed`, and wire them in place of the update restart events. Delete `EmitRequestEditorFileDrop`, `requestEditorFileDropEventName`, and `RequestEditorFileDropEvent`. Delete `serviceruntime.Bus`, `EventBus`, `Event`, `EventData`, and `bustest`. Verify with the updates/windows/shutdown set: `go test ./internal/app ./internal/services/app_service ./internal/desktop/update ./internal/desktop/shell`, plus `go build ./...` and `task vet:ui`. `grep -rn 'serviceruntime.Event\b\|EventBus\|\.Emit(' internal` must return no service-event use.
- [x] 3.5 In `AGENTS.md`, update the `internal/serviceruntime` row to "Service lifecycle and typed event topics". Replace the `EventBus.On` contract with "Call every function `Topic.Subscribe` returns when its listener goes away; subscribers never block." Verify by reading the diff.

## 4. Typed live-event batch

- [x] 4.1 Rename the batcher files to `live_event_batcher.go` and `live_events.go`. Define the closed `LiveEvent` set with `liveSize`, plus `LiveBatch` with `Claim` and `Dropped` per `LiveEventKind`. Publish `TrafficResetEvent{CaptureGeneration}` instead of the map. Delete the reflection estimator. Rename the `frontend*` identifiers, including `frontendTrafficEntryLimit` and `captureMemoryWindow`'s source, and rewrite every comment that says "frontend". Port the batcher, capture-registration, and event-ordering tests. Add tests for the byte limit reached before the event limit, and for one event larger than the byte limit. Verify: `go test ./internal/services/proxy_service ./internal/services/history_service`.
- [x] 4.2 Turn `store.EventRouter` into typed registration with type-switch dispatch and `OnDropped(kind, …)`. Move `TrafficStore` and `WorkspaceStore` to it. Port the router, traffic, and drop-recovery tests. Verify with the metrics set: `go test ./internal/services/proxy_service ./internal/services/history_service ./internal/desktop/capture ./internal/desktop/logic ./internal/desktop/store`, plus `task vet:ui`. `grep -rni 'frontend' --include='*.go' internal` must return nothing.

## 5. Operation-scoped request streams

- [x] 5.1 Add `HTTPStreamSink` and `WebSocketSink` parameters to `SendHTTPRequest` and `ConnectWebSocket`. Store the sinks on the stream and WebSocket session objects, and publish `HTTPStreamEvent` and `WebSocketEvent` with `Deliver()`. Deliver synchronously when no batcher runs. Add `HTTPStreamEventKind` and `WebSocketEventKind`. Verify: `go test ./internal/services/proxy_service`, including tests that a sink receives only its session's events and that the end event follows earlier chunks under a full pending bound.
- [x] 5.2 In `WorkspaceStore`, create the HTTP and WebSocket operation runtimes before the backend call, as design Decision 3 describes. Add `logic.WebSocketConnectionStatus`. Delete `pendingHTTP`, `pendingWS`, `pendingWSKeys`, `ignoredHTTP`, `ignoredWS`, `hashSession`, `boundedKeys`, and `maxPendingSessions`. Port every case in `workspace_stream_test.go` and `workspace_test.go`, and add tests for each scenario in the `service-event-delivery` "Operation-scoped request streams" requirement. Also add tests for binary streaming growth by offset and for UTF-8 split across chunks. Verify with the request-editor set: `go test ./internal/desktop/editor ./internal/desktop/store`, plus `task vet:ui`.
- [x] 5.3 Confirm that no persisted type carries the new kinds or statuses: check `SavedHTTPRequest`, `SavedWebSocketRequest`, and the API Collection payloads. Verify: `go test ./internal/pkg/database ./internal/services/setting_service ./internal/services/api_collection_service`.

## 6. Coalesced plugin console output

- [x] 6.1 Add `runtime.Coalescer[T]` with a 16 ms flush, one dispatcher post per batch, and `Close`. Test batching, order, a post count of one per interval, and that `Close` drops pending values without blocking. Verify: `go test ./internal/desktop/runtime`.
- [x] 6.2 Route `python_plugin_service.Events.Log` into `WorkspaceStore` through the coalescer, with per-execution pending capped at `MaxRequestConsoleEntries`, keeping the newest entries. Delete `pluginLogFlushInterval` and `stopLogFlush`. Test that 5,000 lines arrive in order, subject to the console limit, in batches; that output for an unknown execution is discarded; and that output pending at shutdown is dropped. Verify with the Python hooks set: `go test ./internal/services/python_plugin_service ./internal/services/proxy_service ./internal/services/setting_service ./internal/desktop/plugins ./internal/desktop/store`. With `/usr/bin/python3` available, `go test -v -count=1 ./internal/services/python_plugin_service 2>&1 | grep -E -- '--- SKIP'` must list no test that calls `requirePython3`, such as the `TestWorker*` integration tests. Platform-only Windows skips are expected on Linux. If any other test skips, record why.

## 7. Go-native copies and comparisons

- [x] 7.1 Add `(*Settings).Clone` and the section clone methods in `setting_service`. Use them in `Snapshot` and the persist snapshot. Add the reflection completeness and aliasing test from design Decision 7. Verify: `go test ./internal/services/setting_service`.
- [x] 7.2 Replace `store.CloneSettings` and `cloneShortcuts` with `Clone`, keeping disabled-shortcut (`binding: null`) round trips. Verify with the SQLite/settings and shortcuts sets: `go test ./internal/pkg/database ./internal/services/setting_service ./internal/services/api_collection_service ./internal/services/shortcut_service ./internal/desktop/shortcuts ./internal/desktop/store`.
- [x] 7.3 Replace `CreateSnapshotForTab` and the `SavedSnapshot string` with `logic.RequestSnapshot` and its `Equal`, deep-copying when the snapshot is taken. Remove the snapshot-only `json` tags. Test:
  - editing then reverting a saved tab clears the unsaved marker;
  - mutating rows or files after a save marks the tab unsaved;
  - whitespace-only and disabled rows keep their current meaning;
  - never-saved drafts follow `HasMeaningfulRequestDraft`.

  Verify: `go test ./internal/desktop/logic ./internal/desktop/editor ./internal/desktop/store` and `grep -rn 'json.Marshal' internal/desktop` showing no clone or snapshot use.

## 8. Quit guards in process

- [x] 8.1 Add an injected `onMain` to the guard entry points of `appLifecycle` and `updateRestartHandler`. Replace `rewriteQuitGuard` with a guard that reads `rules.Dirty()` and `QuitPending()` on the main thread and uses `rewrite.QuitDecision`. Delete the per-frame `setDirty` overlay hook. Replace the Settings guard's atomics and `Host.DirtyChanged` with main-thread reads. Port `rewrite_quit_guard_test.go`, `lifecycle_test.go`, and `update_restart_test.go`, and add tests for:
  - stale decisions after a cancel;
  - a second quit while a prompt is open;
  - update restart waiting for both prompts.

  Verify: `go test ./internal/app ./internal/desktop/rewrite ./internal/desktop/settings` and `task vet:ui`.
- [x] 8.2 Run the race detector on the lifecycle and store packages where the toolchain supports it: `task test:race -- ./internal/app ./internal/desktop/...`, which needs cgo and a C toolchain. If it is unavailable on this machine, leave this task open with the reason.

## 9. Smaller leftovers

- [x] 9.1 Change `DeleteTraffic` to `[]uint64` across the service, `TrafficBackend`, and the store. Verify: `go test ./internal/services/proxy_service ./internal/desktop/store ./internal/desktop/capture`.
- [x] 9.2 Move the Settings CA panel's derivation into `logic.DeriveCATrustState`, with the view's current results as the expected values, and call it from `settings/proxy.go`. Verify with the CA trust/appearance/layout set: `go test ./internal/services/setting_service ./internal/desktop/settings ./internal/desktop/theme ./internal/desktop/logic`, plus a Settings render check in light and dark.
- [x] 9.3 For `HasWireOrder`, `HexRowOf`, `ResponseSetCookieFields`, `NextSortOrder`, `shortcuts.Display`, `widgets.InlineInput`, `MenuStates.Len`, and `capture.Table.Rows`: where a view repeats the rule inline, call the helper; otherwise delete the helper and its tests. Record the choice for each in the commit body. Verify: `go test ./internal/desktop/...`. Rerun `deadcode .` (golang.org/x/tools/cmd/deadcode) with `CGO_ENABLED=0`; it must list none of these helpers.
- [x] 9.4 Add the Raw panel fixture test in `internal/desktop/logic` that reads `internal/services/proxy_service/testdata/logical_header_sizes.json`, and point the comment in `header_sizes_test.go` at it. Verify: `go test ./internal/desktop/logic ./internal/services/proxy_service`.

## 10. Integration and acceptance

- [x] 10.1 Run `task verify` (format, lint, `vet:ui`, `i18n:check`, and all tests) and record the result.
- [x] 10.2 Inspect renders with `FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./internal/desktop/... -run TestRender -count=1`. Confirm that the Settings, capture, request editor, and rewrite renders are unchanged in light and dark and in `en` and `zh-Hans`.
- [x] 10.3 Check the completion criteria from the proposal with greps:
  - no `serviceruntime.Bus`, `EventBus`, or `event.Data.(` assertions between services and stores;
  - no `frontend` in Go code;
  - no `locales.Catalog.Format` with a built key;
  - no `json.Marshal` used for clones or snapshots.

  Record the commands and their output.
- [x] 10.4 Real-window acceptance on a Windows desktop session:
  - clear cache, then all data, with an open history, a draft-cache file in a request tab, and process icons shown;
  - save a proxy restart change in both languages;
  - stream an SSE response and Stop it, then send again;
  - open, message, and close a WebSocket Client session;
  - run a Python script that prints 5,000 lines;
  - quit with dirty Settings and with dirty rewrite drafts, including during an update restart.

  The original implementation was verified on headless Linux arm64. At the user's direction on 2026-11-23, desktop acceptance is required only on Windows; Linux and macOS desktop acceptance are out of scope for this change. All six Windows scenarios passed on 2026-11-23 (Windows amd64, Python 3.12.10). Update restart used a temporary local update engine with the real windows, quit guards, shutdown, and relaunch; it did not download or replace an executable.
- [x] 10.5 Run `openspec validate replace-wails-bridge-leftovers --strict --no-interactive`. Then compare the implementation with the `application-settings` and `service-event-delivery` deltas and design.md, scenario by scenario, listing the test or acceptance evidence for each, and record any gap.

## Workflow follow-up

- Archive the change after review and acceptance; synchronize the two deltas into `openspec/specs/` and run `openspec validate --specs --strict --no-interactive`.
