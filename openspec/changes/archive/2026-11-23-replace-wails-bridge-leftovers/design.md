# Design

## Context

See proposal.md for the motivation and the inventory. The current shapes that matter for the approach:

- **Event bus.** `serviceruntime.Bus` delivers `Emit(name string, data ...any)` to `On(name, func(Event))` listeners synchronously, on the publisher's goroutine. Stores assert `event.Data.(T)` and keep their own copies of the name strings, such as `eventProxyStatus = "proxy:status"` in `store/proxy.go` and the traffic names in `store/traffic.go`.
- **Live events.** `proxy_service/frontend_event_batcher.go` batches `FrontendEvent{Name, Data any}` every 16 ms and bounds pending events at 512 events and 4 MiB. It sizes payloads by a reflection walk that counts JSON-like overhead such as quotes and commas. It emits one `FrontendEventBatch` on the bus, and the batch is claimed until the main thread applies it. `store.EventRouter` dispatches that batch by name.
- **Request streams.** `SendHTTPRequest` returns a `StreamSessionID`, and `ConnectWebSocket` returns a `SessionID`. Their events, `HTTPRequestStreamEvent` and `WebSocketSessionEvent`, carry the session ID and string `EventType` values and go through the batcher unbounded. Events can arrive before the call returns, so `WorkspaceStore` keeps `pendingHTTP`, `pendingWS`, bounded `ignoredHTTP`/`ignoredWS` hash sets, and `maxPendingSessions`.
- **Plugin logs.** `WorkspaceStore.ConnectPluginLogs` posts every `python-plugins:log` event to the dispatcher on its own. Each post is a `Window.Update`, which rebuilds the view, and the store only then coalesces at 16 ms.
- **Local-data clearing.** `ClearCacheFiles` and `ClearCacheAndHistory` emit `app:local-data-cleared` as a `map[string]any`, even on partial failure, when `stateCleared` is set. Nothing receives it. `WorkspaceStore.ClearRequestDraftCacheFileReferences` and `widgets.ProcessIcons.Clear` have no callers, and the history list is reloaded only at startup and after HAR import.
- **Settings copy.** `settings.message()` builds catalog keys with `camel()` and calls `locales.Catalog.Format`, which returns the key itself when the key is missing. `camel()` splits only on `_`, `-`, and `.`, so the restart reasons `"proxy mode changed"`, `"listen address changed"`, and `"mitm ca changed"` never resolve.
- **Copies and comparisons.** `SettingService.Snapshot`, the persist snapshot in `setting_service.go`, `store.CloneSettings`, and `cloneShortcuts` copy through `json.Marshal`/`json.Unmarshal`. `WorkspaceStore.IsDirty` calls `logic.CreateSnapshotForTab`, which JSON-encodes the tab, body text included, and compares the result with the `SavedSnapshot` string.
- **Quit guards.** `rewriteQuitGuard` keeps a mutex-guarded dirty flag, written by an overlay on every frame (`quitGuard.setDirty(rules.Dirty())`), and a request/response protocol with string request IDs, string reasons, and string decisions. The Settings guard mirrors dirty state through `Host.DirtyChanged` into an `atomic.Bool`. MyGo runs tray, `OnBeforeQuit`, and window callbacks on the main thread. The update restart request runs on a goroutine.

## Goals / Non-Goals

**Goals:**
- Every service-to-interface value is a Go type known at compile time, from publisher to view.
- Each operation owns its events, so no store has to buffer events for operations it cannot identify.
- Main-thread work is batched where MyGo would otherwise rebuild once per event.
- Behavior stays the same except for the two fixes in the delta specs.

**Non-Goals:**
- No new concurrency model for services: publishers still publish from their own goroutines, and the dispatcher remains the only way onto the main thread.
- No change to batch limits, claim semantics, drop recovery, or the traffic patch model.

## Decisions

### 1. Typed topics replace the string bus

Add `serviceruntime.Topic[T]`, with `Publish(T)` and `Subscribe(func(T)) (off func())`. The zero value is ready to use. Delivery is synchronous, in subscription order, and safe from any goroutine. Unsubscribing during delivery behaves as `Bus` does today. Each service exposes its topics as fields of an exported `Events` struct:

| Owner | Topics |
| --- | --- |
| `proxy_service` | `Status ProxyStatus`, `SystemProxy SystemProxyStatus`, `DNSCache DNSCacheStatus`, `Live LiveBatch` |
| `python_plugin_service` | `Registry RegistryEvent`, `Status StatusEvent`, `Log PluginLogEntry` |
| `app_service` | `UpdateState UpdateSnapshot` |
| `mem_stats_service` | `Snapshot MemSnapshot` |
| `rewrite_service` | `Changed Changed`, replacing `SetChangedHandler` |

`Bus`, `EventBus`, `Event`, `EventData`, the `SetEventBus` setters, and `bustest` are deleted. A small `serviceruntime/topictest` recorder replaces `bustest` in tests.

The following are deleted without a replacement:
- the `proxy:error` listener, which has no publisher;
- `app:shortcuts-changed`, since `shortcutsChanged(result)` already reaches `SettingsStore.ShortcutsChanged`;
- `request-editor:file-drop`, `EmitRequestEditorFileDrop`, and `RequestEditorFileDropEvent`, since file drop uses `DroppedFiles`;
- `app:local-data-cleared` (see Decision 5).

Update restart uses direct calls:
- `AppService.SetRestartRequester(func())` replaces the `app:update-user-restart` event. `internal/app` installs a requester that moves to the main thread (Decision 8).
- `AppService.RestartFailed(err error)` replaces the `app:update-restart-failed` string event.

Revisions, `UpdateSnapshot` staleness checks, and the single active update operation stay as they are.

*Alternatives:*
- Keep the bus and add typed wrappers around `Emit`/`On`. Rejected, because the names, the variadic `any`, and the type assertions would stay.
- Use Go channels per topic. Rejected, because channels add buffering and goroutines without benefit: every subscriber already only posts to the dispatcher.
- Use `mygo.NewEvent[T]`. Rejected, because it is webview IPC and does not apply to native UI.

**Subscriber rule.** A topic subscriber must not block. Store subscribers only `Post` or append under a short lock. This keeps "Clean event shutdown" true: after `Dispatcher.Close`, posts are dropped and publishers never wait.

### 2. A typed live batch replaces `FrontendEventBatch`

The files move to `live_event_batcher.go`/`live_events.go`. Each event is one of a closed set:
- `TrafficEntryEvent`
- `TrafficPatchEvent`
- `TrafficLiveUpdateEvent`
- `TrafficResetEvent{CaptureGeneration uint64}`
- `HTTPStreamEvent`
- `WebSocketEvent`

They implement an unexported marker method and `liveSize() int`. `LiveBatch` holds `Events []LiveEvent` and `Dropped map[LiveEventKind]uint64`, and keeps `Claim()`.

`store.EventRouter` turns into typed registration, `OnTrafficEntry(func(*TrafficEntry))` and so on, plus `OnDropped(kind, func(uint64))`. It dispatches with a type switch.

`liveSize` counts what a pending event keeps alive:
- chunk and body lengths;
- URL, host, and header name and value bytes;
- a fixed overhead per event and per header field.

The batcher's three limits and its `Publish` and `PublishUnbounded` paths stay the same. Tests cover:
- the byte limit before the event limit, as the new spec scenario describes;
- an oversized single event;
- `liveSize` growing with the chunk length, as in the existing size test.

The `frontend*` identifiers become `live*`, for example `frontendTrafficEntryLimit` → `liveTrafficWindow` and `GetTraffic`'s "frontend-sized window". The comments that say "frontend" are rewritten, including the `header_sizes_test.go` comment (Decision 9).

*Alternative:* keep the reflection walk with typed events. Rejected, because it walks every `TrafficEntry` on the capture hot path and models JSON text rather than memory.

### 3. Operation-scoped stream sinks

`SendHTTPRequest` takes a trailing `HTTPStreamSink`, a `func(HTTPRequestStreamEvent)`. `ConnectWebSocket` takes a `WebSocketSink`. The proxy service stores the sink on the session object it already keeps in `httpRequestStreams` or `webSocketSessions`, and publishes `HTTPStreamEvent{Event, sink}` or `WebSocketEvent{Event, sink}` through `PublishUnbounded`. The router calls `event.Deliver()` on the main thread in batch order, so ordering relative to traffic events and "Lossless lifecycle events" are unchanged. The service never calls a sink itself. When no live batcher runs, as in headless tests, delivery is synchronous. A nil sink means the caller ignores events.

In `WorkspaceStore`, the operation state exists before the backend call:
- **HTTP.** Each send creates an `httpStreamRuntime` with an operation generation. Its sink closure appends to `runtime.pending` until the send result sets `state.Response`, then flushes on the existing 100 ms timer. Stop, a new send, tab close, and `Cleanup` mark the runtime inactive. An inactive runtime discards events in the sink. `RegisterHTTPStream` shrinks to attaching the returned session ID, which Stop and Disconnect use.
- **WebSocket.** Each connect creates the same kind of runtime around the tab's `WebSocketClientState`, with the current `wsGeneration` check moved into it.
- **Removed.** `pendingHTTP`, `pendingWS`, `pendingWSKeys`, `ignoredHTTP`, `ignoredWS`, `hashSession`, `boundedKeys`, and `maxPendingSessions`.

String event types and connection states become typed constants in the owning packages:
- `HTTPStreamEventKind`: chunk, complete, closed, error;
- `WebSocketEventKind`: connected, message, closed, error;
- `logic.WebSocketConnectionStatus`: idle, connected, closed, error, cancelled.

These types are not persisted. Check this against `SavedWebSocketRequest` and the API Collection payloads, which store request configuration only.

*Alternative:* keep session-ID routing and make the batch typed only. Rejected, because the pre-registration buffers and ignore lists exist only because the bridge could not bind events to a call.

### 4. Coalesced plugin console output

Add `runtime.Coalescer[T]` to `internal/desktop/runtime`. Producers on any goroutine add values under a mutex. The first value arms a `time.AfterFunc` of 16 ms, which posts one batch to the dispatcher. `Close` stops the timer and drops pending values.

`WorkspaceStore` subscribes to `python_plugin_service.Events.Log` through a coalescer. To bound memory during a main-thread stall, pending entries are capped per execution ID at `logic.MaxRequestConsoleEntries`, keeping the newest, which is what the console keeps anyway. Routing by execution ID, `FilterPythonLogBatch`, and `AppendPythonLogBatch` are unchanged. Output for an execution that no open tab is running is dropped on the main thread, as today. The store's own 16 ms timer (`pluginLogFlushInterval`, `stopLogFlush`) is removed, because the coalescer replaces it.

### 5. Local-data clearing returns a typed result, and the store applies it

`ClearCacheFiles` and `ClearCacheAndHistory` return `(LocalDataClearResult, error)`. The result has:
- `Scope`: `LocalDataScopeCache` or `LocalDataScopeCacheAndHistory`;
- `Cleared`, which is the current `stateCleared`;
- `HistoryCleared`;
- `RequestDraftCacheRoot`.

The result is filled even when the call returns an error, as the event payload was.

`store.Store.ClearLocalData(scope, done func(error))` replaces the Settings host's `ClearCache` and `ClearAllData`. It is keyed as a store job, so the outcome applies even if the Settings window closes first. On completion, on the main thread, if `result.Cleared`, it:
1. calls the `ClearProcessIcons` dependency, which `internal/app` wires to `widgets.ProcessIcons.Clear`;
2. updates the history list:
   - if `HistoryCleared`, it applies the same reset as `HistoryStore.ClearAll`'s success path: cancel `jobHistoryList`, clear `Metadata`, clear the selection;
   - otherwise, it calls `LoadList`, which closes a selected history that no longer exists;
3. if the workbench shows a saved history that is now gone, selects `CaptureItemLive`, as `askClearHistories` does;
4. calls `Workspace.ClearRequestDraftCacheFileReferences(root)`.

Then it calls `done(err)`, and the Settings storage section keeps its pending and done states and notices. The live list is reset by the existing `TrafficResetEvent`.

The old Vue handler did the same, minus the workbench step. This fills the existing spec scenario "Local data cleared" in `service-event-delivery`, which the MyGo port stopped meeting.

### 6. Typed copy for service codes

The owning services define the codes:

| Owner | Code |
| --- | --- |
| `proxy_service` | `ProxyRestartReason`, whose existing string values stay for logs |
| `shortcut_service` | `ShortcutApplyErrorCode` |
| `shortcut_service` | `ShortcutWarning` |
| `shortcut_service` | the existing `ShortcutRuntimeStatus` |

`internal/desktop/settings` maps each code with an exhaustive `switch` to the generated accessors that already exist, such as `locales.SettingsRestartReasonProxyModeChanged()`. Palette labels use a switch over the offered palette names. Unknown values fall back:
- restart reasons, warnings, and palette names show the raw value;
- apply errors use `ShortcutsApplyErrorsUnknown`.

This follows the localization requirement "Backend error presentation". `message()`, `camel()`, and the `locales.Catalog.Format` call are deleted. No locale keys are added or removed.

A test in `internal/desktop/settings` asserts that every declared code maps to an accessor, in both `en` and `zh-Hans`, without returning a key-shaped string.

### 7. Go-native copies and comparisons

`setting_service` owns `(*Settings).Clone()`, with one clone method per section that deep-copies pointers, slices, and maps, including `ShortcutConfig.Overrides` with its nil `Binding` for disabled shortcuts. `Snapshot`, the persist snapshot, `store.CloneSettings`, and `cloneShortcuts` use it.

A reflection test fills every exported field with non-zero values and then asserts two things:
- the clone is `reflect.DeepEqual` to the original;
- mutating any reachable pointer, slice, or map of the clone leaves the original unchanged.

The test fails when a new field is not cloned. JSON stays the persistence format.

`logic` replaces `CreateSnapshotForTab` and the `SavedSnapshot string` with `RequestSnapshot`, holding typed HTTP or WebSocket snapshot values with `Equal`. A snapshot deep-copies rows, form-data items, and file references when taken, because the JSON string used to be an implicit copy. The `json` tags kept only for the snapshot are removed. `HasMeaningfulRequestDraft` keeps its rules.

One known difference: JSON replaced invalid UTF-8 with U+FFFD, so texts differing only in invalid bytes compared equal. Typed equality treats them as different, which is correct.

### 8. Quit guards run on the main thread with typed decisions

Every `quitGuard` call happens on the main thread. Today, `appLifecycle.RequestQuit` runs from the tray, `OnBeforeQuit`, and window close, all on the main thread, and from `proceed` callbacks. `updateRestartHandler.request` runs on a goroutine. Both now reach the guards through an injected `onMain` (`mygo.RunOnMain` in production; immediate in tests), and `l.mu` remains for coordinator ordering.

The two guards become:
- **Rewrite.** `rules.Dirty()` is read directly. A pending prompt is a field of the rewrite view. `rewrite.QuitPrompt.Respond` takes `rewrite.QuitDecision`, with constants for save, discard, and cancel. `rewriteQuitGuard`, its JSON tags, its request IDs, and the per-frame `setDirty` overlay hook are deleted. The view's `QuitPending` then becomes the guard's "already asking" check instead of dead code.
- **Settings.** The guard reads `s.view.Dirty()` and `asking` as plain main-thread state. `Host.DirtyChanged` and the `dirty`/`asking` atomics are deleted.

User-visible behavior is unchanged in all languages and on all platforms:
- the Settings window is focused and asks;
- the rewrite prompt offers save, discard, and cancel;
- update restart waits for both prompts.

### 9. Smaller leftovers

- **`DeleteTraffic`.** Takes `[]uint64`. `TrafficBackend` and the store's `backendIDs` follow.
- **Ported helpers without native callers.** The Settings CA panel's inline derivation in `settings/proxy.go` is the shipped behavior. It moves into `logic.DeriveCATrustState`, the view calls it, and the logic tests are updated to match the view's current results. `HasWireOrder`, `HexRowOf`, `ResponseSetCookieFields`, `NextSortOrder`, `shortcuts.Display`, `widgets.InlineInput`, `MenuStates.Len`, and `capture.Table.Rows` follow one rule:
  - if a view repeats the same rule inline, the view calls the helper;
  - otherwise the helper and its tests are deleted.
- **Raw panel fixture check.** A Go test in `internal/desktop/logic` reads `internal/services/proxy_service/testdata/logical_header_sizes.json` and checks two things for every fixture:
  - `FormatRawHTTPRequest` and `FormatRawHTTPResponse` reproduce `RequestHead` and `ResponseHead`;
  - `SummarizeHTTPMessageSize` and `TrafficTotalSizeBytes` match their UTF-8 byte lengths.

  This restores the check that the TypeScript test ran, and the comment in `header_sizes_test.go` then names that test.

## Data flow, compatibility, and recovery

- **Storage and codecs.** Unchanged: settings JSON, API Collection payloads, the request draft cache layout, HBIN v1–v3, HAR, and the Worker protocol. No migration is needed. Rollback is a revert.
- **Services.** In-process signatures change: topics, stream sinks, clear results, `DeleteTraffic`, and typed codes. Nothing outside the module calls them.
- **Views.**
  - The Settings window: notices for restart reasons, storage cleanup results.
  - The main window: history list, open history, editor file references, process icons.
  - Rewrite and Settings quit prompts: same layout and focus.

  No layout, theme, or shortcut changes. Light and dark themes are unaffected. Both locales use existing keys.
- **Failure.**
  - A partial clear still applies the cleared parts, then reports the error.
  - A stream whose operation ended discards late events.
  - If a sink's tab is closed, the event is a no-op.
  - Shutdown closes the dispatcher and coalescer first, so late publishes are dropped without blocking.

## Risks / Trade-offs

- **[Stream refactor regressions]** Ordering, Stop, and reconnect paths are subtle. → Port each existing case in `workspace_stream_test.go` and `workspace_test.go` before deleting the buffers. Add tests for every new scenario in `service-event-delivery` and for binary streaming growth by offset.
- **[Byte accounting drift]** Per-kind sizes differ from the reflection estimate. → The limits keep their values. Tests pin the byte limit and the oversized single event. `liveSize` counts real payload bytes, so it bounds memory at least as well.
- **[Main-thread guard evaluation]** A guard called off the main thread would read view state from the wrong thread. → `onMain` is the only entry. A test asserts that guards run through it, and `go test -race`, where the toolchain allows it, covers the lifecycle tests.
- **[Clone drift]** A new settings field could be missed. → The reflection completeness test fails on unfilled or aliased fields.
- **[Wide diff]** Many owners change. → Land in the commit order of `tasks.md`, one class of change per commit, each passing its AGENTS.md package set.

## Migration Plan

There is no data migration. Ship as ordinary commits in the order of `tasks.md`. The two user-visible fixes land first so that they can be backported alone. Rollback is reverting the commits. No setting, file, or format written by this change differs from 2.0.0.
