# Traffic Metrics and HAR

This reference specifies how FlowLens measures timings and sizes, maps traffic to HAR 1.2, persists sessions in HBIN, enforces recording admission, and bounds capture memory. For UI import/export workflows, see the [User Guide](../user-guide.md#history-and-har).

## Timing Sources

FlowLens captures request-attempt start, request-write end, response first-byte, and response body-end timestamps at the upstream transport boundary. Responses modified by live rewrite rules record downstream send events instead. Live traffic and HBIN histories store all timestamps as Unix microseconds together with each exchange's terminal state.

- **Retries:** A retry replaces the timestamps of the stale attempt.
- **Unknown values:** Failed, canceled, pending, and incomplete exchanges keep `-1` rather than fabricating completion timings.
- **Connection phases:** DNS, TCP connect, and TLS handshake durations come from `mitmproxy-go` connection metadata. They belong to the underlying connection, are shared by every request reusing that connection, and are **not** added to an exchange's Total Duration.
- **DNS cache hits:** A shared DNS cache hit records a DNS duration of `0`. Initial lookups, expired entries, and coalesced in-flight queries keep their measured durations; cache-lookup overhead remains inside the connection setup interval.
- **Clock alignment on Windows:** Connection setup and HTTP exchange timestamps share a single high-resolution monotonic clock.

## Header and Body Sizes

- **Header size (`metrics.headerSize`):** Computed once as the UTF-8 byte length of the complete head rendered in the **Raw** panel: the start line plus `CRLF`, each displayed header line plus `CRLF`, and the trailing blank `CRLF`. Detail views, traffic totals, HAR `headersSize`, and HBIN all consume this value directly.
  - **HTTP/2:** Uses synthesized start lines (such as `GET /path HTTP/2.0` and `HTTP/2.0 200`). Pseudo-headers mirror the **Raw** panel: `:authority` maps to `host`, `:scheme` is omitted, and `:method`, `:path`, and `:status` move into the start line. This is a logical text size that excludes TCP/TLS framing, HPACK compression, and HTTP/2 frame overhead.
  - **HTTP/1:** Uses origin-form paths, authority-form targets for `CONNECT`, and `*` for `OPTIONS`.
  - **Unknown sentinel:** Missing metrics, truncated headers, or an unavailable start line produce `-1`. Normalized fallback headers without raw wire order can still produce a known size.
- **Body size (`BodySize`):** Counts entity bytes after transport encoding, matching the bytes actually sent or received on the wire.

Captured bodies are decoded for inspection while the original encoded bytes are forwarded unchanged. If a `gzip` or `zstd` checksum fails, capture keeps the bytes decoded before the error, including the final decoded `zstd` block, and drains the remaining encoded stream. HTTP Request Editor sends and body rewrite rules still report decoding failures.

## HAR Export

HAR exports stream to a temporary file in the destination directory and atomically rename into place upon completion. Entries whose cached body is missing still export without a body and increment the skipped/unavailable body count in the result.

### Timings

- Timestamps retain microsecond precision; elapsed durations export as fractional milliseconds.
- Each completed DNS, TCP connect, and TLS phase is attributed **once**, to the earliest request in the source capture or history carrying that connection's metadata—regardless of row selection or export order. Exporting only a later request on a reused connection leaves its connection phases at `-1`.
- When connection setup precedes the HTTP exchange (as with HTTPS `CONNECT`), `startedDateTime` shifts to setup start and `time` includes setup.
- `blocked` captures setup and forwarding gaps prior to the exchange. Only the portion of a connection phase overlapping the request interval is subtracted from `send`.
- HAR `connect` includes TLS (`ssl`), so `ssl` is not double-counted when summing `time`. For complete, non-overlapping exchanges, `time` equals the sum of known `blocked`, `dns`, `connect`, `send`, `wait`, and `receive` values. Remaining preparation and retry overhead stays in `send` (noted in `timings.comment`).
- Unavailable or incomplete phases stay `-1`. A failed HTTP send does not clear an already-completed connection phase.
- Overlapping upload and response phases cannot be represented as a serial waterfall, so `wait` and `time` remain `-1`.
- `_connectionTimings` preserves original connection timestamps; other FlowLens extension fields preserve original request/response timestamps and terminal states.

### Request Bodies

- **Structured forms:** Complete URL-encoded and multipart bodies up to `4 MiB` and `1,000` parameters export as `postData.params`, preserving parameter order, duplicates, empty values, filenames, and part content types. Binary multipart values use Base64 with a per-parameter `_encoding` extension. Per the HAR spec, structured forms omit `postData.text`, and multipart boundaries are represented structurally rather than as raw text.
- **Raw fallback:** Larger forms, incomplete or malformed bodies, and unsupported form encodings export as raw `postData.text`; binary raw bodies add `_encoding: "base64"`. HAR readers that ignore `_encoding` cannot reconstruct those binary payloads.

## HAR Import

- **Headers:** Header arrays preserve order, duplicates, casing, and empty values, with wire order marked unavailable.
- **Bodies:** Text and Base64 bodies are supported; decoded payloads are not decompressed again. URL-encoded `postData.params` can reconstruct a request body, whereas multipart parameters without raw text cannot reproduce original wire bytes and are marked unavailable. Missing bodies stay distinct from genuinely empty bodies.
- **Timings:** Valid FlowLens timestamp and state extensions take precedence. Standard HAR timings convert from milliseconds to microseconds while preserving total duration and known stages; pre-send waiting and connection costs join the request interval without double-counting TLS. Missing, inconsistent, or failed timings remain unknown (`-1`).
- **Sizes:** Header size is recomputed using FlowLens's **Raw** panel convention. Body sizes are taken from HAR `bodySize` rather than decoded text length.
- **Unsupported fields:** Pages, cache entries, standalone cookie attributes, WebSocket frames, and unknown extensions are dropped. Re-exporting an imported history therefore does not reproduce every original field, and `blocked` may merge into `send`.
- **Storage mapping:** Imported entries map directly into standard traffic fields with no special source label and without changing the HBIN format version.

## HBIN History Format

| Version | Read / Write | Notes |
| --- | --- | --- |
| **v3** | Read and write | Stores binary WebSocket messages as raw bytes instead of Base64 |
| **v2** | Read only | Added upstream connection timestamps; binary WebSocket payloads decoded from Base64 on read |
| **v1** | Read only | Connection timings display as unavailable (`—`); binary WebSocket payloads decoded from Base64 on read |

Earlier development-only layouts are unsupported, and files with unknown versions are skipped without being deleted. Body records use `zstd` frames written with a `1 MiB` window. Frames with windows up to `8 MiB`, including those written by older builds, are readable; a frame that declares a larger window is rejected rather than allocated.

For inspection, decoded HBIN request and response bytes are owned by their read operation and transferred directly into immutable body views. Reader cleanup and decoder-pool reuse cannot change those views. Live capture readers still copy mutable capture data. This does not change empty versus unavailable bodies, binary payloads, or the streaming HAR/traffic export paths.

History browsing retains lightweight rows with message protocols, metrics, first content types, connection addresses, process details, and TCP ALPN needed by existing columns, filters, categories, and action availability. Full ordered headers, trailers, TLS/certificate details, and bodies are reread for inspection and actions. Summary rows are never used as full request/export inputs and are not written back to HBIN. File reads are buffered and cancellation-aware; replacing or closing a selection prevents stale results from repopulating it.

For HBIN v1-v3 list reads, the shared decoder skips the encoded payloads of nonessential fields instead of decoding and then discarding them: header names/values other than the first case-insensitive Content-Type, all trailers, certificates (including subject/issuer and name arrays), and TLS fields other than selected ALPN for TCP rows. The first Content-Type keeps its original casing and value, including an empty value before a duplicate. Message presence, truncation/order flags, metrics, process data, and connection timings retain their normal semantics. Skipping uses bounded reads rather than seeking past data, preserving count/flag validation, truncation errors, and cancellation without allocations proportional to the skipped payload size. Complete decoding and the stored format are unchanged.

- **Periodic and terminal saves:** Live capture persists to its history pair every `60` seconds while modified, and immediately on shutdown, restart, and cache cleanup. After a periodic save completes, the next save waits `max(60s, 5 × saveDuration)` (for example, a 30-second save schedules the next save 150 seconds later). Each save writes to temporary files and swaps atomically. Spilled entries are copied as raw encoded bytes and re-indexed at their new offsets; in-memory entries are encoded during save; bodies still streaming are copied from their cache files up to bytes received so far without loading them into memory.
- **4 GiB limit:** HBIN v3 indexes entries using 32-bit offsets, capping any single history at `4 GiB` of encoded data. A save exceeding that limit fails and is logged, while the session's cached data stays exportable until the session ends.

## Recording Admission

**Pause Recording** controls capture admission without interrupting proxy forwarding:

- **One-time decision:** Admission is evaluated once when the HTTP interceptor registers a request, the WebSocket interceptor registers a successful handshake, or the raw TCP interceptor sees a new tunnel. HTTP requests on reused connections are admitted independently.
- **Skipped traffic:** Unadmitted exchanges create no traffic entry, cached body, metrics record, or process-attribution subscription, and are never retroactively added when recording resumes. Unrecorded WebSocket frames are still read, forwarded, and released normally.
- **In-flight traffic:** Exchanges admitted before pausing continue recording to completion—including downloads, SSE streams, and every message on existing WebSocket connections—so existing rows and totals can still update while paused.
- **Unchanged proxy behavior:** TLS handling, rewrite rules, upstream proxy settings, live request options, and system proxy settings continue operating without dropping connections. Connection-level process lookup stays available for subsequent admitted requests on reused connections.
- **State lifetime:** Pause state is process-local and never saved to settings or history. An explicit **Start Proxy** enables recording; configuration-driven listener restarts preserve pause before accepting connections. **Clear Traffic** and capture-session restarts preserve pause while keeping their standard generation and cleanup rules. Existing entries remain readable, periodically savable, and exportable while paused; HBIN and HAR formats are unchanged.

## Capture Memory

A live capture session retains in memory only its newest `5,000` settled entries plus exchanges still in progress (HTTP exchanges until response completion/failure, captured bodies until fully read/closed, WebSocket entries until connection close, and process lookups until final). These entries back the live UI table.

Older settled entries spill to disk in batches of up to `256`:

- **Spill trigger:** New entries wake the spiller once in-memory entries exceed `5,000 + 256`; the final producer completing an exchange also wakes the spiller whenever more than `5,000` entries remain, allowing older exchanges to spill after traffic subsides.
- **Transparency:** Status-bar totals, saved histories, capture-wide exports, and per-entry actions (details, resend, edit, copy, export, delete) seamlessly include spilled entries.
- **Location:** Spill segments are stored as `spill-NNNN.hbin` files inside the session body-cache directory (`cache/<session-uuid>/`), rolling to a new segment at `1 GiB`.
- **Encoding:** Spilled entries use the HBIN v3 entry layout (header followed by a `zstd` body record containing both bodies and WebSocket messages) tracked by a ~`24`-byte in-memory index entry. Deleting a spilled entry zeroes its bytes on disk.
- **Large WebSocket payloads:** Any captured WebSocket message exceeding the body-cache threshold writes immediately to `<id>_ws.body` in the session cache directory, keeping only metadata in memory.
- **Lifetime:** Deleting an entry removes its cached data; clearing capture removes all session data. The entire session body-cache directory is deleted when restarting capture (after Save or Discard), after a clean shutdown save succeeds, and on the next launch following a crash or failed final save. If the body cache directory is unavailable, capture keeps all entries in memory.
