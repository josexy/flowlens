# Traffic Metrics and HAR

This reference defines how FlowLens measures timing and sizes, and how those values map to HAR and HBIN. For importing and exporting in the app, see the [user guide](../user-guide.md#history-and-har).

## Timing Sources

FlowLens records the request-attempt start, request-write end, and response first-byte and body-end events at the upstream transport boundary. Responses changed by rewrite rules use the actual downstream send events instead. Live traffic and HBIN history keep these as Unix microsecond timestamps, together with each exchange's terminal state.

- A retry replaces the data of the stale attempt.
- Failed, canceled, pending, or incomplete exchanges keep unknown values (`-1`) instead of synthetic completion data.
- DNS, TCP connect, and TLS handshake durations come from mitmproxy-go's connection metadata. They describe the connection, are shared by every request that reuses it, and are not added to an exchange's Total Duration.
- An application DNS cache hit reports a DNS duration of `0`. First lookups, expired entries, and shared in-flight queries keep their measured duration; cache lookup overhead stays in the connection setup interval.
- On Windows, connection setup and HTTP exchange timings share one high-resolution clock.

## Header and Body Sizes

`metrics.headerSize` is computed once, as the UTF-8 byte size of the complete head shown in the Raw panel: the start line and CRLF, each displayed field line and CRLF, and the final CRLF. Detail views, traffic totals, HAR `headersSize`, and HBIN all read this value directly.

HTTP/2 uses synthesized start lines such as `GET /path HTTP/2.0` and `HTTP/2.0 200`. Pseudo-headers follow the Raw panel: `:authority` becomes `host`, `:scheme` is omitted, and method, path, and status move into the start line. The result is a logical text size that excludes TCP/TLS bytes, HPACK compression, and frame overhead. HTTP/1 uses origin-form paths, authority-form targets for CONNECT, and `*` for OPTIONS.

Missing metrics, truncated headers, or an unavailable start line give `-1`. Normalized fallback fields without raw wire order can still have a known size.

`BodySize` counts entity bytes after transport encoding, as actually sent or received.

## HAR Export

HAR output is streamed to a temporary file in the destination directory and atomically replaces the target when complete. Entries whose cached body is missing are exported without the body and counted in the result.

### Timings

- Timestamps keep microsecond precision; elapsed times are milliseconds with fractional digits.
- Each complete DNS, TCP connect, and TLS phase is assigned once, to the earliest request in the source capture or history that carries that connection's metadata. This is independent of selection and export order, so exporting only a later request on a reused connection leaves its connection phases at `-1`.
- When connection setup precedes the HTTP exchange, as with HTTPS CONNECT, `startedDateTime` moves to the setup start and `time` includes setup.
- `blocked` covers setup and forwarding gaps before the exchange. Only the part of a connection phase that overlaps the request interval is subtracted from `send`.
- HAR `connect` includes TLS, so `ssl` is not added again when summing `time`. For complete, non-overlapping exchanges, `time` equals the sum of the known `blocked`, `dns`, `connect`, `send`, `wait`, and `receive` values. Remaining preparation and retry overhead stays in `send`, as `timings.comment` notes.
- Unavailable or incomplete phases stay `-1`. A failed HTTP send does not erase a completed connection phase.
- Overlapping upload and response phases cannot be expressed as a serial waterfall, so `wait` and `time` stay `-1`.
- `_connectionTimings` keeps the original connection timestamps; other extension fields keep the original request and response timestamps and terminal states.

### Request Bodies

Complete URL-encoded and multipart bodies up to 4 MiB and 1,000 parameters export as `postData.params`, keeping parameter order, duplicates, empty values, filenames, and part content types. Binary multipart values use Base64 with a per-parameter `_encoding` extension. Structured forms omit `postData.text`, as HAR requires, and multipart boundaries are represented structurally rather than as raw text.

Larger forms, incomplete or malformed bodies, and unsupported form representations keep the raw `postData.text`; binary raw bodies use `_encoding: "base64"`. Readers that ignore `_encoding` cannot reconstruct those binary values.

## HAR Import

- Header arrays keep their order, duplicates, case, and empty values; wire order is marked unavailable.
- Text and Base64 bodies are supported. Decoded content is not decompressed again.
- URL-encoded parameters can rebuild a request body. Multipart parameters without raw body text cannot reproduce the original bytes and are marked unavailable. A missing payload stays distinct from a genuinely empty body.
- Valid FlowLens timestamp and state extensions take precedence. Other HAR timings convert from milliseconds to microseconds, keeping the total duration and known stages. Pre-send waiting and connection costs join the request interval, and TLS is not counted twice. Missing, inconsistent, and failed timings stay unknown.
- Header size is recalculated with the Raw display convention. Body sizes come from HAR `bodySize`, not decoded text length.
- Pages, cache details, standalone cookie attributes, WebSocket frames, and unsupported extensions are dropped. Re-export therefore does not reproduce every original field, and `blocked` may become part of `send`.

Import maps values into the existing traffic fields without source labels and without changing the HBIN version.

## HBIN History Format

FlowLens writes HBIN v3 and reads v1, v2, and v3. v2 added upstream connection timestamps; v3 stores binary WebSocket messages as their original bytes instead of Base64. v1 and v2 binary WebSocket messages are decoded on read, and v1 histories show connection timings as unavailable. Earlier development-only layouts are unsupported, and files with unknown versions are skipped without being deleted.

**Saving the current capture.** The capture is saved to its history pair every 60 seconds while it changes, and at once on shutdown, restart, and cache cleanup. After a periodic save, the next one waits five times that save's duration, and never less than 60 seconds: a save that takes 30 seconds is followed by one 150 seconds later. Each save rewrites the pair through temporary files and an atomic rename. Spilled entries (see below) are copied as their encoded bytes and indexed at their new offsets; entries in memory are encoded as they are saved, and bodies still being received are read from their cache files up to the bytes received so far, without loading them into memory.

**Size limit.** HBIN v3 indexes entries with 32-bit offsets, so one history holds at most 4 GiB of encoded data. A save that would pass that limit fails and is logged; the capture's cached data stays available for export until the session ends.

## Capture Memory

A capture session keeps in memory only its newest 5000 entries and the entries whose exchange is still in progress: an HTTP exchange until its response ends or fails, a captured body until it is read to its end or closed, a WebSocket entry until its connection closes, and a process lookup until its result is final. These are the entries the interface shows.

Older entries that are finished move to the session's spill store in batches of up to 256, once the in-memory entries pass 5000 + 256. The status bar, saved history, capture-wide exports, and the actions on an entry (details, resend, edit, copy, export, delete) include spilled entries as before.

- **Location.** Spill segments are `spill-NNNN.hbin` files in the session's body-cache directory, `cache/<session-uuid>/`. A new segment starts once the current one reaches 1 GiB.
- **Encoding.** Each spilled entry is written with the HBIN v3 entry encoding: its header, then its zstd body record with both bodies and its WebSocket messages. An in-memory index of about 24 bytes per entry locates it. Deleting a spilled entry overwrites its bytes with zeros.
- **WebSocket payloads.** A captured WebSocket message larger than the body-cache threshold is written to `<id>_ws.body` in the same directory as soon as it is captured. Only its metadata stays in memory.
- **Lifetime.** A deleted entry's data is removed with it, and clearing the capture removes all of it. The whole body-cache directory is removed when capture restarts after Save or Discard, after the final save at shutdown succeeds, and at the next start after a crash or a failed final save. If the body cache is unavailable, the capture keeps every entry in memory.
