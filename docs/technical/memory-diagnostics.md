# Memory Diagnostics

This guide explains how to profile a running FlowLens instance, why memory can remain elevated while idle after heavy capture bursts, and what state FlowLens retains intentionally.

## Profiling a Running Instance

The diagnostic HTTP server is disabled by default. To enable it, start FlowLens with `FLOWLENS_PPROF_ADDR` set to a loopback IP and port. Any non-loopback address is rejected and logged, and the server shuts down automatically when FlowLens exits.

```sh
# macOS
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 /Applications/FlowLens.app/Contents/MacOS/FlowLens

# Linux
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 flowlens

# Windows (PowerShell)
$env:FLOWLENS_PPROF_ADDR = "127.0.0.1:6060"; & "$env:LOCALAPPDATA\Programs\FlowLens\FlowLens.exe"
```

### Diagnostic Endpoints

| Endpoint | Purpose |
| --- | --- |
| `/debug/pprof/` | Standard Go runtime profiles: `heap`, `allocs`, `goroutine`, `goroutineleak`, `threadcreate`, `profile` (CPU), and `trace` |
| `POST /debug/flowlens/collect` | Runs two GC cycles with cleanups between them, returns free heap pages to the OS, and reports process memory, heap in use, kept-for-reuse memory, and held memory at each step |

### Useful Profiling Commands

```sh
# Trigger forced GC and OS memory return, and print step-by-step memory stats
curl -X POST http://127.0.0.1:6060/debug/flowlens/collect

# Inspect live heap usage
go tool pprof -sample_index=inuse_space http://127.0.0.1:6060/debug/pprof/heap?gc=1

# Capture before/after heap snapshots and diff them
curl -o before.pprof 'http://127.0.0.1:6060/debug/pprof/heap?gc=1'
# ... perform the action, then capture after.pprof ...
go tool pprof -sample_index=inuse_space -top -base before.pprof after.pprof

# Check for leaked goroutines
curl 'http://127.0.0.1:6060/debug/pprof/goroutineleak?debug=1'
```

### Interpreting the Collect Report

Read the last line of the `POST /debug/flowlens/collect` output:

- **Process memory drops close to held memory:** The extra memory was uncollected garbage or idle heap awaiting runtime reclamation, not a leak.
- **Heap in use stays high:** Live Go objects are still retaining memory. Compare `before.pprof` and `after.pprof` heap profiles around the suspect action to identify the retainer.
- **Kept for reuse stays high while heap in use is low:** The Go heap is fragmented—surviving objects are scattered across partially used pages that cannot yet be returned to the OS.
- **Process memory stays well above held memory:** The difference lives outside the Go heap (such as GPU textures, font caches, and native libraries). Go profiles cannot inspect it; use platform-native profiling tools instead.

## Why Memory Stays High While Idle

Three runtime mechanisms can keep process memory elevated for several minutes after a traffic burst stops:

- **Forced GC interval (up to ~2 minutes):** Go normally triggers garbage collection when allocations reach the heap goal (twice the live heap from the previous cycle). An idle FlowLens allocates very little, so garbage from a capture burst can remain until the runtime's periodic forced collection runs every 2 minutes.
- **Pooled codecs and weak-pointer cleanups (up to ~4 minutes):** `sync.Pool` retains idle objects across the first GC cycle and drops them on the second. The largest pooled objects are compression codecs, and a burst can leave one per concurrent body in the pool. A `zstd` encoder used for spill segments, saved histories, and `zstd` request bodies retains a history buffer twice its 1 MiB window (~5 MiB total) after a body larger than 128 KiB. A `zstd` decoder retains history for the largest window it decoded: ~1.5 MiB after the 1 MiB-window frames FlowLens writes, and at most ~8.5 MiB, because a frame that declares a window over 8 MiB is rejected rather than allocated. A Brotli reader retains a ring buffer sized by the stream's window, up to ~16 MiB. Likewise, `mitmproxy-go` stores each proxied response's request context (and its connection TLS state) in a weak-pointer-keyed table until cleanup runs after the response is collected. Together, these release roughly 4 minutes after traffic stops.
- **Background page scavenging and fragmentation:** After each collection, the Go runtime returns free spans to the OS in the background while retaining roughly 10% headroom above current heap demand. Pages that still hold even one live object stay mapped, so **Kept for reuse** can exceed the size of retained burst entries until those entries are evicted or cleared.

## What FlowLens Keeps on Purpose

| Retained State | Bound | Released When |
| --- | --- | --- |
| Live capture | Newest 5,000 settled entries plus in-progress exchanges; bodies above the cache threshold (10 KiB default) spill to disk | Older entries spill to disk; **Clear** or restarting capture releases the rest |
| Opened history | Lightweight rows of the active history: table/filter fields, message metrics/content types, addresses, and process information | The history tab closes or another history opens |
| Selected entry | Decoded bodies and viewer state (formatted, Raw, Hex, and image caches) | Selection changes, details close, or capture is cleared |
| Request-editor response text | Current response only; read-only replacements discard previous editor versions, including hidden viewers | Response replacement or tab close |
| History indexes | Up to 4 most recently accessed histories | Evicted least-recently-used first |
| Pooled `zstd` encoders | ~5 MiB each after writing a body over 128 KiB, scaled by peak concurrent writers | Second GC cycle after last use |
| Pooled `zstd` decoders | ~1.5 MiB each after FlowLens's own 1 MiB-window frames, up to ~8.5 MiB at the 8 MiB window cap, scaled by peak concurrent readers | Second GC cycle after last use |
| Pooled Brotli readers | Up to ~16 MiB each, sized by the largest window read, scaled by peak concurrent readers | Second GC cycle after last use |
| MyGo text layouts | ~8 MiB steady state, up to 64 MiB while actively used by recent frames | Trimmed only during frame rendering, not while all windows are idle |
| MyGo element state and frame arena | Current frame's arena plus spare capacity and recycled element states | Excess arena chunks and stale references are trimmed during subsequent frames |

Body text editors are created on demand for the selected mode. Formatted text is never generated for bodies at or above 512 KiB, where the Formatted tab is unavailable. Switching modes can retain both representations of the current body; replacement releases them. SSE Raw text, once opened, appends new chunks without rebuilding the preceding text or retaining undo steps. Copy and Save continue using the selected mode, with original raw bytes and line endings preserved outside the formatted editor.

History browsing selectively decodes one record at a time. It retains the first Content-Type in each message and skips other header names/values (including Cookie and Set-Cookie), all trailers, certificates, and unneeded TLS fields using their encoded lengths. Skipped strings and collections are not materialized; a bounded reusable buffer consumes their bytes while still detecting truncation and cancellation. This reduces initial scan allocations and GC work, but does not necessarily reduce disk reads or immediately lower Task Manager process memory. TCP rows retain selected ALPN for the protocol column.

Complete selected metadata is held separately, never inserted back into the row list. Opening Overview, Headers, Cookies, or certificate details does not decode bodies; Raw, Body, and WebSocket panes request the selected payload once, independently of metadata. Pending selection preserves only small tab identifiers for layout, not previous records or bodies. Copy as cURL and Edit Request load the complete record for the action; export and resend continue reading their full source records. Switching or closing a history/detail cancels both read stages and rejects late results. Switching to another workspace tab keeps an open history's rows; closing the history tab releases them. The list still scales with the number of records and the fields it needs so search, categories, and sorting cover the whole history.
