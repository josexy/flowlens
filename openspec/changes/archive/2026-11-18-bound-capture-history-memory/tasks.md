# Tasks

## 1. Capture decoding and deletion leaks

- [x] 1.1 In `internal/services/proxy_service/body_stream.go`, drain `pr` with `io.Copy(io.Discard, pr)` after both the decode success and failure paths (design §7). Add a test that proxies a `deflate` body followed by 64 KiB of trailing bytes and verifies the client receives every byte, the entry completes, and `StreamBodyReader.Close` returns (no goroutine left: check with `runtime.NumGoroutine` or a `decodedDone` assertion). Keep the existing corrupt-gzip test passing.
- [x] 1.2 In `newCaptureObservedStreamBodyReader` and `newCaptureWebSocketMessages`, recheck the tombstone after `LoadOrStore` and `CompareAndDelete`, abort, and delete the cache files for a deleted entry (design §8). Add a test that uses `storeTrafficPrecheckHook`-style synchronization to delete the entry between the check and the store, then verifies `trafficBodies`, `trafficWsMsgs`, and the body-cache files hold nothing for that ID.
- [x] 1.3 Run `go test ./internal/services/proxy_service` and `CGO_ENABLED=1 go test -race ./internal/services/proxy_service`, and verify both pass.

## 2. Streaming reads of in-progress bodies

- [x] 2.1 In `internal/pkg/body_cache/body_cache.go`, track the bytes written in `BodyCacheWriter` and add `SnapshotReader() (io.ReadCloser, int64, error)`: a read-only handle to the tmp file limited to that size, with no fsync, falling back to the committed file after `Close` (design §4). Add tests in `body_cache_test.go` for concurrent write-then-read, for reading after `Close` and `Preserve`, and for verifying that later writes never change bytes already returned. Verify with `go test ./internal/pkg/body_cache`.
- [x] 2.2 Switch `capturedBody.Reader()` in `captured_body.go` from `Snapshot()` to `SnapshotReader()` and remove `Snapshot()` once it has no callers. Add a proxy-service test that opens the body view of a 64 MiB response mid-stream and verifies the body matches the bytes received so far, with allocations measured by `testing.AllocsPerRun` or `runtime.MemStats` below 2× the body size. Verify with `go test ./internal/services/proxy_service`.

## 3. Capture storage lifecycle

- [x] 3.1 In `proxy_service.go`, remove the previous body-cache session directory in `finalizeCaptureRestartCaptureLocked` after a successful save or discard, and at the end of `shutdown` only when `flushOnShutdown` succeeded or had nothing to do (design §6). Add tests in `storage_cleanup_test.go` for Save and Restart, Discard and Restart, a successful shutdown, and a failed final flush (directory kept).
- [x] 3.2 In `ServiceStartup`, remove every UUID-named directory directly under `cache/` before the session cache is created, leaving `process-icons` and other names untouched. Add a test that seeds two UUID directories, `process-icons`, and a non-UUID directory, then verifies only the UUID directories are removed.
- [x] 3.3 Update `docs/user-guide.md` (Storage section) to state that cached bodies of a capture session are removed after restart, after a clean shutdown, and at the next start after a crash, while saved histories and process icons remain. Verify the text matches the behavior tested in 3.1 and 3.2.

## 4. Back-pressured UI event delivery

- [x] 4.1 In `frontend_event_batcher.go`, add `FrontendEventBatch.Claim() (release func())` and make `run` wait for the release of every claim or shutdown, while `publish` keeps applying the pending limits (design §9). Test unclaimed delivery, prolonged stalls, count and byte bounds, ordered delivery and drop reporting after release, and shutdown releasing pending payloads without enqueueing behind an abandoned claim. The review fix in 13.1 removes the original two-second timeout.
- [x] 4.2 In `internal/desktop/store/event_router.go`, claim in the bus callback and release in a `defer` after `Dispatch` on the main thread. Add a store test with a fake dispatcher that holds posted functions, verifying at most one batch is outstanding and drop counts trigger `requestRecovery`. Verify with `go test ./internal/desktop/store ./internal/services/proxy_service` and `CGO_ENABLED=1 go test -race ./internal/desktop/store ./internal/services/proxy_service`.

## 5. Live capture interface memory

- [x] 5.1 In `internal/desktop/store/traffic.go`, keep a live `strings.Builder` seeded in `initLiveCursors` and `prepareBinarySSESnapshot`, append only decoded new bytes in `applyLive`, publish `builder.String()`, and drop the builder in `resetLiveReconciliation` (design §10). Add a `traffic_test.go` case that applies 1000 chunks of 1 KiB after a 1 MiB snapshot, verifying the result equals the concatenation and total allocated bytes grow linearly (not quadratically) with chunk count. Keep the existing gap and binary-to-text tests passing.
- [x] 5.2 In `TrafficStore.applyBatch` (and wherever the eviction watermark advances), delete `Highlights` entries at or below the watermark. Add a test that highlights an entry, evicts it with 5001 newer entries, and verifies the highlight is gone.
- [x] 5.3 Run `go test ./internal/desktop/store ./internal/desktop/capture ./internal/desktop/logic` and `task vet:ui`, and verify they pass.

## 6. History memory

- [x] 6.1 In `internal/services/history_service`, replace the eager `indexMap` with a lazy LRU of at most 4 indexes loaded through `bufio` by `loadIndexLocked(key)`. Make `ListHistoryKeys`, `ClearHistories`, `DeleteHistory`, and startup maintenance use a directory scan plus metadata only, and route `GetHistory`, `GetHistoryTrafficBodyView`, `ResendRequest`, `ExportHAR`, and `ExportTraffic` through the loader (design §11). Update existing tests and add tests that listing 10 histories loads no index, that opening a fifth history evicts the least recently used one, and that delete and clear evict cached indexes. Verify with `go test ./internal/services/history_service`.
- [x] 6.2 In `internal/desktop/store`, add `HistoryStore.CloseOpened()` (cancel the load job, reset `Traffic` including `BodyView` and highlights) and `FilterStore.Release()` (drop `base`, `filtered`, and the match maps), and call `Release` from `clearSelection`. Add `history_test.go` and `filter_test.go` cases verifying no reference to the previous entries remains after close (`Entries`, `Filter.Base()`, the internal maps).
- [x] 6.3 In `internal/desktop/capture/workspace.go` `syncTabs`, when the history tab is gone, call `CloseOpened`, a new `Table.Reset()` that drops rows, sorter state, and table state, and the category view reset. Add a capture test that opens a history, closes its tab, and verifies the history table and category view hold no rows. Verify with `go test ./internal/desktop/capture ./internal/desktop/store` and `task vet:ui`. Check the change in light and dark themes with `FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./internal/desktop/... -run TestRender -count=1`.
- [x] 6.4 Update `docs/user-guide.md` (History and HAR) to note that closing the history tab frees the opened history, and verify the statement against 6.3's test.

## 7. WebSocket payload spooling

- [x] 7.1 In `internal/pkg/body_cache`, add an append-only per-entry handle (`Append(p) (offset int64, err error)`, `ReadAt`, `Close`, deletion with the entry's other files) for kind `ws`, with tests for concurrent append and read and for deletion. Verify with `go test ./internal/pkg/body_cache`.
- [x] 7.2 In `proxy_service.go`, store captured WebSocket payloads above the body-cache threshold through that handle, keeping metadata and small payloads inline, and build messages with a single copy from `frame.DataBuffer().Bytes()` (design §5). Materialize payloads in `getTrafficBodyViewInner`, the HBIN encoder input, and the HAR and traffic exports. Add tests verifying that a session with 8 MiB binary messages keeps them out of `TrafficWsMsgs` memory, and that the body view, HBIN round-trip, and HAR export are byte-identical to the sent payloads. Verify with `go test ./internal/services/proxy_service ./internal/services/history_service`.

## 8. Settled-entry tracking

- [x] 8.1 Add `pending` to `trafficEntryLifecycle`, and increment and decrement it exactly once per producer: the HTTP exchange (`capture_metrics.go`: complete, fail, bodyless), each captured body (`makeCaptureOnDoneFunc`), the WebSocket frame loop (`websocketInterceptor` exit), and the process-attribution binding (until the lookup is final, released on deactivate). Raw TCP entries settle when attribution settles. Add tests covering normal completion, upstream failure, client cancel, rewrite abort, WebSocket close, attribution timeout, delete during exchange, and clear during exchange, each ending with `pending == 0`. Verify with `CGO_ENABLED=1 go test -race ./internal/services/proxy_service`.

## 9. Session spill store

- [x] 9.1 Create `internal/services/proxy_service/capture_spill.go` with `captureSpillStore`: 1 GiB segment rotation in the session body-cache directory, `Append(entry, bodyView)` via `hbinWriteEntry`, an ID-sorted ~24-byte index, `Entry(id)`, `Body(id)`, `CopyRaw(dst, id) (headerLen, bodyRel, n)`, `Delete(id)` (zero the byte range), `Reset`, and `Close` (design §1). Add `capture_spill_test.go` covering round-trip equality with `hbinWriteEntry` and decode for HTTP, WebSocket, and raw TCP entries; segment rotation (forced with a small test limit); out-of-order IDs; deletion zeroing; and raw copy producing bytes decodable at rebased offsets. Verify with `go test ./internal/services/proxy_service`.

## 10. Spilling and spill-aware access

- [x] 10.1 Add the spiller goroutine (design §2): wake on insertion above 5000 + 256 and on final producer release above 5000, batches of 256 settled entries under `clearDataMu` and `captureLifecycleMu.RLock`, and a swap under `trafficAttributionMu` with generation, tombstone, and revision rechecks that releases the snapshot, bodies, body-cache files, WebSocket messages, and process binding. Stop it on cancel and before the final flush. Disable it with a single warning when the body cache is unavailable. Test that after 20000 settled entries memory holds 5000 plus unsettled entries, that an old open WebSocket stays in memory, and that clear, restart, and delete racing with a batch leave a consistent state (`-race`). Task 13.2 covers completion without subsequent traffic.
- [x] 10.2 Route `GetTrafficBodyView`, `ResendRequest`, `DeleteTraffic`, `harExportSnapshots`, `currentHARExportEntry`, `trafficExportBodies`, and `ExportHAR`/`ExportTraffic` through a single ID resolver that handles spill refs, with lazy per-entry decoding during export and statistics decrement by kind on spilled delete (design §3). Add tests that body view, resend, edit recovery (via the returned `TrafficBodyView`), delete, and HAR and CSV export of a spilled entry match the same entry before spilling, and that an export of all entries in a partly spilled session lists every ID in order.
- [x] 10.3 Update `docs/technical/traffic-metrics-and-har.md` with the spill store: location, encoding, lifetime, and the 5000-plus-in-progress memory window. Verify it matches the implementation constants.
- [x] 10.4 Run `go test ./internal/services/proxy_service ./internal/services/history_service ./internal/desktop/capture ./internal/desktop/logic` and `CGO_ENABLED=1 go test -race ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service`, and verify both pass. The full race run used `-timeout 30m`; capture completed in 730.83 seconds with no exclusions.

## 11. Memory-safe periodic save

- [x] 11.1 Rewrite `flushHistoryToDiskCaptureLocked` to merge spill refs (raw copy with rebased index offsets) and in-memory snapshots (encoded as today, with in-progress bodies read through `SnapshotReader`) in ID order, keeping the two-file atomic commit and generation checks (design §4). Add tests verifying that a partly spilled session's saved HBIN v3 pair loads in `history_service` with every entry, header, body, and WebSocket message identical to the unspilled baseline; that a save during a 256 MiB in-progress download allocates well below the body size; and that existing `history_file_pair` crash and recovery tests still pass.
- [x] 11.2 Make `syncHistoriesToDiskPeriodically` schedule the next periodic save at `max(60s, 5 × last duration)` while forced saves stay immediate. Add a test with an injectable clock or flush hook verifying the delay after a slow flush.
- [x] 11.3 Update `docs/technical/traffic-metrics-and-har.md` (HBIN History Format) with the save interval rule and the note that saved sessions remain subject to the 4 GiB HBIN v3 limit. Verify the documented numbers match the code.

## 12. Integration verification

- [x] 12.1 Run `task verify` and verify it passes with no skipped proxy, history, or desktop packages.
- [x] 12.2 Run a soak check in a test or benchmark (`internal/services/proxy_service`) that captures 200000 small HTTP exchanges and 20 long WebSocket connections through the real interceptors. Verify that heap in use after GC stays within a fixed bound independent of the entry count (compare the 50k and 200k checkpoints), that goroutines return to baseline after the proxy stops, and that the cache directory is removed after Restart. Record the numbers in the change's PR description.

## 13. Review regressions

- [x] 13.1 Keep a claimed frontend event batch outstanding until release or shutdown, without timeout-based delivery of another batch. Cover a stalled interface beyond the former timeout, bounded pending events and bytes, ordered recovery after release, and shutdown without waiting for an abandoned claim.
- [x] 13.2 Wake the background spiller when the last producer finishes, including when no new traffic arrives and only one old unfinished entry remains beyond the window. Exercise the real spiller and producer releases without calling `spillAll` after completion.
- [x] 13.3 Release the persistent history detail panel and its body, Raw, Hex, SSE, WebSocket, and image caches when its history closes or changes, rejecting pending viewer work. Cover closing, empty frames, reopening, and switching histories with the real detail panel.
- [x] 13.4 Apply SSE growth incrementally in the displayed body viewers as well as the store. Cover text-buffer content, reset/replacement and UTF-8 boundaries, and allocations on the actual viewer update path after a large snapshot.
- [x] 13.5 Reconcile the proposal, design, delta and main specs with the fixes, explicitly distinguish the bounded history index cache from open history data, and record verification results and remaining limitations.

## Workflow completion

- Follow-ups for Request Editor stream back-pressure, the WebSocket Client message limit, and an HBIN format with 64-bit offsets are recorded in [verification.md](verification.md).
- Delta specs were synchronized into `openspec/specs/` and compared requirement by requirement. The completed change was archived on 2026-11-18 after the review fixes and verification.
