# Proposal

## Why

A code audit of live capture and history display found several places where memory and disk use grow without bound or are never released:

- **The backend keeps the whole capture in memory.** The proxy service keeps every entry of a capture session until Clear or Restart: headers, TLS/certificate metadata, bodies up to the 10 KiB cache threshold per side, process-attribution bindings, and up to 1000 WebSocket messages per connection with no byte limit. The table only shows the newest 5000 entries.
- **The periodic save re-reads everything.** Every 60 seconds the history save re-encodes the entire session. Bodies still being received are read completely into memory through `BodyCacheWriter.Snapshot()`, so a long download is allocated again at every save.
- **Previous body caches stay on disk.** Each run's `cache/<uuid>` body-cache directory is never removed after a restart, shutdown, or crash unless the user clears the cache manually.
- **Closed and unopened histories stay in memory.** A closed history tab keeps all of its entries, filter caches, and table rows. The history service keeps the entry index of every saved history in memory for the whole run.
- **Live SSE details copy the whole body on every update.** Each update in the capture detail panel copies the full body received so far.
- **The event bound does not limit memory.** Event batches are handed to the UI thread's unbounded queue whether or not the previous batch has been applied.
- **Capture can stall a proxied response.** A `deflate` response with bytes after its compressed stream leaves the decoding goroutine without a reader, so the response hangs and the goroutine leaks.
- **Deleted entries can leave data behind.** Deleting an entry while its body is starting can leave an orphan body buffer.

Long captures are FlowLens's main use, so memory must stay flat over time.

## What Changes

- **Spill finished entries to disk.** The proxy service keeps only the newest 5000 entries plus entries whose exchange is still in progress. Older entries that have finished move to a session spill store in the body-cache session directory. Saved history, capture-wide export, status totals, and per-entry actions (detail, resend, edit, copy, delete, export) on older entries stay complete.
- **Spool large WebSocket messages.** Payloads of captured WebSocket messages above the body-cache threshold go to disk. Payloads are no longer copied twice.
- **Make the periodic save memory-safe.** The save streams in-progress bodies from their cache files instead of reading them into memory. Spilled entries are copied as encoded bytes without decoding. The save interval grows for sessions whose save takes long.
- **Release capture storage.** A session's body-cache directory, including spill segments, is removed after Save/Discard and Restart and after a successful final save at shutdown. Directories from earlier runs are removed at startup. Deleting entries while their bodies start leaves no data behind.
- **Fix the decoding stall.** Capture decoding always drains the encoded stream, so trailing bytes cannot stall forwarding or leak a goroutine.
- **Back-pressure UI event delivery.** At most one event batch waits for the UI thread. Events published meanwhile stay within the existing pending bound, and excess events are dropped and counted so the UI recovers through a snapshot, as today.
- **Make live SSE updates incremental.** Both the retained body and its displayed Raw text append only new data. The viewer keeps no unused Formatted copy of SSE and no undo history for received chunks.
- **Hold history data only while open.** Closing the history tab releases the opened history's entries, details, filter caches, table rows, and category data. The history service loads an entry index only when a history is opened or exported, and caches a small number of indexes instead of every saved history's.
- **Prune row highlights.** Highlights of entries that leave the live window are removed.

Out of scope, recorded as follow-ups: Request Editor SSE streams and WebSocket Client message lists, which also grow without bound; and the HBIN v3 4 GiB offset limit, which longer captures will reach sooner.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-capture`: adds requirements for bounded capture memory, periodic capture saving, release of capture storage, and forwarding that capture decoding cannot stall.
- `traffic-history`: adds a requirement that history data is held in memory only while a history is open.
- `traffic-inspection`: "Live body growth" adds a scenario requiring each live chunk to cost only its own size.
- `service-event-delivery`: "Bounded high-frequency events" adds a slow-interface scenario so the pending bound actually limits process memory.

## Impact

- **Code:**
  - `internal/services/proxy_service`: `proxy_service.go` (retention, delete, clear, restart, flush, body views, resend, edit recovery), `body_stream.go`, `captured_body.go`, `har_service.go`, `traffic_export_service.go`, `traffic_codec.go` (raw entry copy), `capture_metrics.go` (settled tracking), `process_attribution.go`, `frontend_event_batcher.go`, and a new spill store.
  - `internal/pkg/body_cache`: snapshot reader, WebSocket payload file, session-directory removal.
  - `internal/services/history_service`: index loading, delete, clear, exports.
  - `internal/desktop/store`: `traffic.go`, `history.go`, `filter.go`, `event_router.go`.
  - `internal/desktop/capture`: workspace and history-tab close handling, table and category reset.
  - `internal/desktop/inspect`, `internal/desktop/widgets`, and `internal/app`: release hidden detail viewers and their pending work, wire the history lifecycle, and append SSE text without resetting the editor.
- **Formats:** HBIN v3 is unchanged. Spill segments use the HBIN v3 entry encoding and live only inside the session's body-cache directory.
- **Disk:** a session's on-disk cache now also holds spilled entries until it is saved and the session ends. Cache directories from previous runs are no longer kept.
- **Docs:** `docs/user-guide.md` (storage and history notes) and `docs/technical/traffic-metrics-and-har.md` (spill and save behavior).
- **Tests:** packages listed in AGENTS.md for metrics/body/HBIN/HAR/export and request-editor scopes, plus `-race` for concurrency changes.
