# Memory Diagnostics

Use the diagnostic server to distinguish retained Go objects, reusable heap pages, and native process memory.

## Profiling a Running Instance

Set `FLOWLENS_PPROF_ADDR` to a literal loopback IP and port. The server is disabled by default and closes with the application.

```powershell
# Windows
$env:FLOWLENS_PPROF_ADDR = "127.0.0.1:6060"
& "$env:LOCALAPPDATA\Programs\FlowLens\FlowLens.exe"
```

```sh
# macOS
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 /Applications/FlowLens.app/Contents/MacOS/FlowLens

# Linux
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 flowlens
```

| Endpoint | Purpose |
| --- | --- |
| `/debug/pprof/` | Go heap, allocation, goroutine, CPU, and trace profiles |
| `POST /debug/flowlens/collect` | Force collection, wait for cleanups, return free pages to the OS, and report memory at each stage |

### Useful Profiling Commands

Use `curl.exe` instead of `curl` in Windows PowerShell.

```sh
# Measure what collection can reclaim.
curl -X POST http://127.0.0.1:6060/debug/flowlens/collect

# Capture a heap profile after GC.
curl -o before.pprof 'http://127.0.0.1:6060/debug/pprof/heap?gc=1'
# Repeat after the workload, saving after.pprof.
go tool pprof -sample_index=inuse_space -top -base before.pprof after.pprof

# Inspect goroutines identified as leaked by the runtime.
curl 'http://127.0.0.1:6060/debug/pprof/goroutineleak?debug=1'
```

For natural recovery, use a separate run and sample at 0, 30, 120, and 300 seconds after the workload. Save `/debug/pprof/heap` without `gc=1`; do not call the collect endpoint. Record Process, object memory, and Held alongside each profile. Diagnostic requests also allocate memory.

Keep comparisons matched: binary revision, data, window size, DPI, theme, columns, selected body, capture state, and accessibility (UIA) client activity.

### Interpreting the Collect Report

| Field | Meaning |
| --- | --- |
| Process | OS process-memory measurement; includes memory outside Go |
| Heap in use | Go heap objects, including garbage not yet collected |
| Kept for reuse | Free heap pages and unused space within heap spans |
| Held | Go runtime memory not returned to the OS: objects, reusable heap, stacks, and overhead |

If object memory falls after collection, some allocations were awaiting GC. If it stays high across repeated matched cycles, inspect heap profiles for retained objects. High Kept for reuse can reflect spare capacity or fragmentation; it does not alone establish a leak. A persistent Process/Held gap warrants native profiling because the counters cover different memory.

Heap profiles can lag by two GC cycles. Use runtime counters for the recovery curve and profiles for allocation-site attribution.

## Why Memory Stays High While Idle

- **Collection timing:** Little allocation can delay the next GC. The current Go runtime has a roughly two-minute periodic trigger; this is not a deadline for full memory recovery.
- **Pools and cleanups:** Idle codec buffers may survive another GC cycle. Weak-pointer cleanup can also defer release of request and TLS state.
- **Page reuse:** The runtime returns free pages gradually. Partly occupied spans and renderer caches can remain after payloads are released.

## What FlowLens Keeps on Purpose

| State | Retention and release |
| --- | --- |
| Live capture | Newest 5,000 settled entries plus in-progress exchanges. Bodies above the configured threshold (10 KiB by default) spill to disk. Clear or restart releases the live window. |
| Open history | Table/filter metadata for all rows; released when history closes or is replaced. Switching workbench tabs keeps it open. |
| Selected body and editor response | Loaded views of the current response; released on replacement or closure. Formatted text is unavailable at or above 512 KiB. |
| History indexes | Up to four recent histories, evicted least-recently-used first. |
| Codec pools | Reusable zstd/Brotli buffers; memory depends on concurrency and input windows. zstd uses a 1 MiB encoder window and rejects decoder windows above 8 MiB. |
| MyGo resources | Text layouts, element states, frame arenas, atlases, and drawing buffers. Some trimming requires subsequent frames. |

History scans skip unused headers, trailers, certificates, and TLS fields. Selected metadata is loaded separately; body bytes are requested only by views that need them. Closing or replacing a history/detail cancels reads and rejects late results. Export, resend, Copy as cURL, and Edit Request read full records when needed.

## Application-owned UI resources

`widgets.AppIcon` shares one bitmap decoded by `ui.DecodeBitmapScaled(resources.AppIcon, 256)`. Title bars and Software Update display it at 18 and 48 DIPs. Retained CPU pixels, including mipmaps, measured 0.332 MiB; per-window GPU textures are separate. Decoding briefly uses the full source, and failure leaves usable windows without the icon.

Traffic tables paint backgrounds and outlines on existing Nuxt cells, including the trailing fill cell. Drawing callbacks retain visual scalars only. The first displayed cell supplies committed vertical bounds for right-click hit testing; the native table owns selection, keyboard input, and accessibility.

`BenchmarkTrafficTableStress` measures warmed retained UI heap with its tester alive, separately from timed allocations. Set `FLOWLENS_TABLE_HEAP_DIR` to save heap profiles. Ordinary checks use `CGO_ENABLED=0`:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTable' -benchmem -count=5
```

Headless UI savings do not predict the same percentage reduction in native process memory.
