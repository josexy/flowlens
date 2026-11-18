# Design

## Context

See proposal.md (Why) for the audit findings. The current state that constrains the approach:

- **Capture state.** `ProxyService` holds capture state in `trafficEntries` (an ordered map of immutable snapshots), `trafficBodies` (`capturedBody`: in memory up to the cache threshold, otherwise a body-cache file under `cache/<session-uuid>/`), `trafficWsMsgs` (whole payloads in memory, count-limited), and `trafficProcessBindings`. Nothing is removed except by delete, clear, restart, or cache cleanup. `GetTraffic` already returns only the newest 5000 entries.
- **Locking.** The lock order is `lifecycleOpsMu → requestDraftCacheOpsMu → clearDataMu → captureLifecycleMu → bodyCacheMu → mu`, with `trafficAttributionMu` guarding snapshot publication and deletion tombstones (`trafficEntryLifecycle.deleted`). Flush, export, and delete serialize on `clearDataMu`.
- **History flush.** `flushHistoryToDiskCaptureLocked` rewrites the whole HBIN v3 pair through `writeAndCommitHistoryFilePair` (temp files, then an atomic rename). Each entry is encoded with `hbinWriteEntry`. The header and the zstd body block are position-independent, and the index stores `(id, headerOffset uint32, bodyOffset uint32)`.
- **Event delivery.** The frontend batcher emits on its own goroutine through the synchronous bus. `EventRouter.Connect` then posts each batch to MyGo's main-thread queue, which is an unbounded FIFO.
- **History service.** It builds `indexMap` for every saved history at startup and on every list refresh, and keeps it for the whole run. `HistoryStore` keeps the opened history until another one is opened or the history is deleted.
- **Constraints.** HBIN v3 stays the written format (AGENTS.md). Traffic and certificate timestamps stay in Unix microseconds. Unknown metric values stay at `-1`. Exports stay streamed and atomic.

## Goals / Non-Goals

**Goals:**
- Capture memory is O(5000 entries + in-progress exchanges + ~24 bytes per older entry), independent of session length.
- No full-body materialization during periodic saves, and no unbounded queue between the batcher and the UI thread.
- Every file a capture session creates is removed by a defined lifecycle event.

**Non-Goals:**
- Request Editor stream chunks and WebSocket Client message lists. These events still use `PublishUnbounded` and are not limited by this change.
- Lifting the HBIN v3 4 GiB offset limit, or changing any on-disk history format.
- Bounding the memory of a single body opened in the detail panel. The spec requires complete text viewing.
- Changing the 5000-entry interface window or the WebSocket message-count setting.

## Decisions

### 1. Session spill store using HBIN v3 entry encoding

A new `captureSpillStore` (`internal/services/proxy_service/capture_spill.go`) belongs to the body-cache session: it lives in the same `cache/<uuid>/` directory, is created and rotated with it under `bodyCacheMu`, and is used under `captureLifecycleMu`.

- **Storage.** Appends go to segment files `spill-NNNN.hbin`. A segment rotates before it would pass 1 GiB, which keeps `hbinWriteEntry`'s uint32 offsets valid inside a segment and keeps any single file well below the 4 GiB check.
- **Entry format.** Each entry is written with `hbinWriteEntry`, using the segment as the data file and a 16-byte scratch buffer as the index writer. The store records `{id, segment, header, body, end, kind}` in an ID-sorted slice (about 24 bytes per entry; `kind` drives the statistics decrement on delete). IDs arrive almost in order, so insertion is an append or a short tail shift. Lookups use binary search.
- **Reads.** Reads use `io.NewSectionReader` over a per-segment `*os.File` opened read-only once, decoded with the existing `DecodeTrafficEntryWithVersion` and `DecodeTrafficBodyWithVersion` at v3.
- **Deleting a spilled entry.** The bytes in `[header, end)` are overwritten with zeros and the index record is removed. This satisfies the storage-release requirement without compaction; space returns when the session directory is removed.

Alternatives considered:
- **Appending to the live HBIN history file itself.** This needs in-place header and count updates and a variable-length alias rewrite, and it breaks the two-file atomic commit. Rejected.
- **A new spill format.** It would duplicate the codec and its tests. Rejected.
- **SQLite.** It adds write amplification and lock contention on the capture path. Rejected.

### 2. Settled tracking and a background spiller

`trafficEntryLifecycle` gains a `pending atomic.Int32` count of producers that can still change the entry:
- the HTTP exchange, until it completes or fails;
- each captured body, until its `onDone` fires;
- the WebSocket frame loop, until it exits;
- the process-attribution binding, until its lookup is final.

Each producer increments the count when it is attached and decrements it exactly once at its terminal point. An entry is **settled** when the count reaches zero. A settled entry cannot be updated again.

A single spiller goroutine starts with the proxy service. `storeTrafficSnapshot` wakes it, without blocking, when the in-memory count exceeds 5000 + 256. The last producer's release also wakes it whenever more than 5000 entries remain; this includes a single old exchange finishing after new traffic stops. The lifecycle's settlement callback is fixed before publication, uses the same coalesced wake channel, and performs no disk I/O. The spiller works in batches of up to 256 entries:
1. Under `clearDataMu` and `captureLifecycleMu.RLock`, it walks `trafficEntries` from the oldest entry and takes settled entries until fewer than 5000 newer entries remain.
2. It encodes them into the spill store using `getTrafficBodyViewInner` readers.
3. Under `trafficAttributionMu`, it rechecks generation, tombstone, and `Revision`, then swaps each entry. The swap removes the snapshot from `trafficEntries`, adds the spill index record, deactivates and releases the process binding, aborts and drops `TrafficBodies`, deletes that ID's body-cache files, and drops `TrafficWsMsgs`.

An entry that fails the recheck is skipped. Its appended bytes are zeroed.

The spiller releases its locks between batches, so it never holds them long enough to stall traffic registration behind a waiting writer. It stops on `baseCtx` cancellation and during shutdown before the final flush. When the body cache is unavailable (initialization failed), spilling is disabled for the session and a warning is logged once. Retention then falls back to today's behavior.

Alternatives considered:
- **Spilling inline on the transport goroutine.** This violates the no-I/O rule in `registerTrafficEntry`. Rejected.
- **Inferring "settled" from metric states.** Trailers, attribution, and body `onDone` arrive on independent goroutines, and a counter states the contract explicitly. Rejected.

### 3. Spill-aware access paths

A single helper resolves an ID to either the in-memory snapshot or a spill reference. The following use it:
- `GetTrafficBodyView`
- `ResendRequest`
- `DeleteTraffic` / `deleteTrafficEntryCaptureLocked`
- `harExportSnapshots` / `currentHARExportEntry` / `trafficExportBodies`
- the flush

`RecoverRequestBodyForEditing` already receives a `TrafficBodyView`, so it inherits the behavior. `GetTraffic` stays tail-only.

`harExportSnapshots` returns an ID-ordered list of refs captured under `clearDataMu`, so the export scope stays stable. Spilled entries are decoded one at a time while writing, not materialized up front. HAR connection-ID observation does a first header-only pass over the refs.

### 4. Periodic save: raw copy, streamed in-progress bodies, adaptive interval

The flush merges spill refs and in-memory snapshots in ID order:
- **Spilled entries** are copied as raw bytes `[header, end)` from their segment with `io.CopyN`. The flush writes their index record itself, with rebased offsets: `newBody = newHeader + (body − header)`. This needs no decoding, re-compression, or full-body buffer.
- **In-memory entries** are encoded as today, except for bodies still being received. `BodyCacheWriter.Snapshot()` (fsync plus `os.ReadFile`) is replaced by `SnapshotReader()`, which opens the tmp file read-only, wrapped in an `io.LimitReader` of the bytes written so far. The writer tracks that count under its mutex. Written bytes are visible to readers without fsync, and later appends never change bytes before the limit.

`capturedBody.Reader()` uses the same reader, so opening the detail view of a large in-progress body copies it once (in `convertToTrafficBodyView`) instead of twice.

`syncHistoriesToDiskPeriodically` measures each periodic flush and sets the next delay to `max(60s, 5 × duration)`. Shutdown, restart, and cache-cleanup flushes stay forced and immediate.

Alternatives considered:
- **Incremental HBIN append.** See Decision 1. Rejected.
- **A fixed longer interval.** This weakens durability for small sessions. Rejected.

### 5. WebSocket payloads above the cache threshold

`TrafficWsMsgs` keeps each message's metadata, and keeps its payload inline when the payload is at or below the body-cache threshold. Larger payloads are appended to a per-entry `<id>_ws.body` file in the session body-cache directory, created through a small append-only handle in `internal/pkg/body_cache` (`Append(p) (offset, error)`, `ReadAt`). The message records `{offset, size}`.

`getTrafficBodyViewInner`, the HBIN encoder, and the HAR and traffic exports materialize payloads only while building their output. Live updates still carry the payload, bounded by the batcher's byte limit. The frame loop builds the message from `frame.DataBuffer().Bytes()` with a single copy instead of `Clone()` followed by `string()`.

Alternatives considered:
- **A per-connection byte budget with truncation.** It changes visible behavior and loses data. Rejected.

### 6. Session storage lifecycle

The body-cache session directory, which now includes spill segments and WebSocket payload files, is removed with `os.RemoveAll`:
- **Restart.** In `finalizeCaptureRestartCaptureLocked`, after the save or discard succeeds, the old directory is removed before the new one is created.
- **Clear.** In `ClearTraffic`, the spill store is reset and its segments deleted. The directory is kept for the same session.
- **Shutdown.** At the end of `shutdown`, the directory is removed only after `flushOnShutdown` succeeds or had nothing to flush.
- **Startup.** In `ServiceStartup`, every directory directly under `cache/` whose name parses as a UUID is removed. The current session's directory has not been created yet. `process-icons` and other non-UUID names are never touched.

Startup cleanup is safe because no code reads another session's directory, and the single-instance lock is taken before services start.

### 7. Decoder pipe always drained

In `newObservedStreamBodyReader`, the decoding goroutine runs `io.Copy(io.Discard, pr)` after both the success and the failure paths, before closing. Bytes after the end of the compressed stream are discarded from capture and still reach the client through the `TeeReader`.

### 8. Delete versus body-capture race

`newCaptureObservedStreamBodyReader` and `newCaptureWebSocketMessages` recheck the tombstone after `LoadOrStore`. If the entry was deleted in between, they `CompareAndDelete` their value, abort the captured body or messages, and delete that ID's cache files. `deleteTrafficEntryCaptureLocked` sets the tombstone before `LoadAndDelete`, so one of the two sides always sees the other.

### 9. One UI batch in flight

`FrontendEventBatch` gains a nil-safe `Claim() (release func())`.
- `EventRouter.Connect` claims each batch synchronously in the bus callback and defers `release` around `Dispatch` on the main thread. A dispatcher that has closed may discard the posted function; shutdown handles this abandoned claim.
- After emitting a claimed batch, the batcher's `run` loop waits for every claim's release or `stop`. There is no timeout: a stalled interface still owns the previously queued payload. Meanwhile `publish` keeps applying `MaxPendingEvents` and `MaxPendingBytes`, and excess events are dropped and counted, which triggers the existing snapshot recovery.
- Unclaimed batches (headless runs, tests, no UI listener) do not wait.

`Close` signals `stop`, so an abandoned claim cannot block shutdown. When stopping with an outstanding claim, the batcher discards and releases its pending payloads instead of enqueueing another batch behind the abandoned one. Closing with no outstanding claim can still flush the final pending batch.

Alternatives considered:
- **Bounding the queue inside `Dispatcher`.** It cannot tell lossless events from droppable ones. Rejected.
- **Blocking `Publish`.** This would stall transport goroutines. Rejected.

### 10. Incremental live SSE body

`TrafficStore` keeps a `strings.Builder` for the live cursor, seeded when the first SSE chunk uses a text snapshot or in `prepareBinarySSESnapshot` for a binary snapshot. `applyLive` appends only decoded new bytes and publishes `builder.String()`, which is zero-copy. The builder is dropped in `resetLiveReconciliation`.

`inspect.BodyViewer` recognizes SSE growth with the same entry, content type and text encoding, then appends only the suffix to its existing Raw `CodeView`. It does not retain a Formatted buffer for SSE, which has no Formatted tab. Read-only `CodeView.AppendText` uses the pinned editor's `EditRemote` and `MarkSaved` to preserve selection without accumulating undo history. A CRLF split across updates contributes one visible newline while the source body and Hex keep the original bytes. Replacement, a new entry, or an encoding/content-type change reloads the text. Tests exercise the viewer's allocation path as well as actual Raw/Hex/SSE frames in both themes.

### 11. History memory

- **History service.** `indexMap` becomes a lazily filled cache of at most 4 indexes (LRU by last access). It is loaded by `loadIndexLocked(key)` from the index file through a `bufio.Reader`.
  - `ListHistoryKeys`, `ClearHistories`, `DeleteHistory`, and startup maintenance work from a directory scan plus metadata only.
  - `GetHistory`, `GetHistoryTrafficBodyView`, `ResendRequest`, `ExportHAR`, and `ExportTraffic` call the loader.
  - Deleting or clearing a history evicts its cache entry.
- **Interface.**
  - `HistoryStore` gains `CloseOpened()`, which cancels the load job and resets `Traffic`.
  - `FilterStore` gains `Release()`, which drops `base`, `filtered`, and the match maps. `clearSelection` calls it as well.
  - `capture.Workspace.syncTabs` closes a history whose tab is gone and tracks the loaded history key. A changed key, including a store cleared by deletion, resets the history table and categories and invokes the detail release callback wired in `internal/app`.
  - `inspect.Panel.Release` cancels image-decode and SSE-parse jobs before replacing its viewers, dropping body, text-editor, Raw, Hex, SSE, WebSocket and image caches even while the panel is hidden. Posted results are discarded by the job dispatcher; tab choices remain available when a history reopens.
  - `TrafficStore.applyBatch` deletes highlights of IDs at or below the eviction watermark.

## Risks / Trade-offs

- **[Long sessions now reach the HBIN 4 GiB limit]** A save of more than 4 GiB of compressed data fails as it does today, but long sessions are now viable and reach the limit more often. → The failure is logged and the spill store keeps the data for export. A follow-up is recorded for a 64-bit-offset format.
- **[Disk use grows with session length]** Spill segments duplicate the in-memory data that was evicted, and the cache files of spilled bodies are deleted after copying. → Storage statistics already count `cache/`. Clear cache and Restart release the data.
- **[An entry never settles]** A producer that never reaches its terminal point keeps its entry in memory. → Producers decrement in `defer`/`once` paths. Tests cover the failure, cancel, delete, and clear paths. The count of held-back entries is logged when it exceeds 1000.
- **[Spilling pauses during long exports and flushes]** These hold `clearDataMu`, so memory grows with traffic during that time. → This is bounded by the operation's duration. The adaptive flush interval reduces how often it happens.
- **[Batch wait adds latency]** A slow UI frame delays the next batch. → Events are coalesced into the next batch rather than lost, and the drop-and-recover path already exists.
- **[Startup cleanup removes data from a crashed session]** That data was already unreachable, since no recovery path reads it. → The behavior is documented in the user guide.

## Migration Plan

No data migration is needed. Existing `cache/<uuid>/` directories from earlier versions are removed at the first startup after upgrading, and saved histories are unaffected. To roll back, revert the change. Older builds ignore spill segments, and the directories holding them are removed by the user's Clear cache action or by this build's startup cleanup.
