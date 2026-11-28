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

Use `curl.exe` instead of `curl` in Windows PowerShell.

```sh
# Measure what collection can reclaim.
curl -X POST http://127.0.0.1:6060/debug/flowlens/collect

# Capture a heap profile after GC; repeat after the workload as after.pprof.
curl -o before.pprof 'http://127.0.0.1:6060/debug/pprof/heap?gc=1'
go tool pprof -sample_index=inuse_space -top -base before.pprof after.pprof

# Inspect goroutines identified as leaked by the runtime.
curl 'http://127.0.0.1:6060/debug/pprof/goroutineleak?debug=1'
```

Measure natural recovery in a separate run at 0, 30, 120, and 300 seconds after the workload. Save `/debug/pprof/heap` without `gc=1` and do not call the collect endpoint. Record Process, object memory, and Held alongside each profile; diagnostic requests also allocate memory.

Keep the binary revision, data, window size, DPI, theme, columns, selected body, capture state, and accessibility-client activity matched between runs.

### Interpreting the Collect Report

| Field | Meaning |
| --- | --- |
| Process | OS process memory, including allocations outside Go |
| Heap in use | Go heap objects, including garbage awaiting collection |
| Kept for reuse | Free heap pages and unused space within heap spans |
| Held | Go memory not returned to the OS: objects, reusable heap, stacks, and overhead |

Falling object memory after collection indicates reclaimable garbage. If it stays high across repeated matched cycles, inspect profiles for retained objects. High **Kept for reuse** can reflect capacity or fragmentation; a persistent Process/Held gap calls for native profiling. Neither establishes a leak on its own.

Heap profiles can lag by two GC cycles. Use runtime counters for recovery curves and profiles for allocation-site attribution.

## Why Memory Stays High While Idle

Little allocation can delay GC, pools and cleanups can survive additional cycles, and Go returns free pages gradually. Partly occupied spans and renderer caches may remain after payloads are released. Some UI caches need subsequent frames to trim.

## What FlowLens Keeps on Purpose

| State | Retention and release |
| --- | --- |
| Live capture | Newest 5,000 settled entries plus in-progress exchanges. Bodies above the configured threshold (10 KiB by default) spill to disk. Clear or restart releases the live window. |
| Open history | Table/filter metadata for all rows. Closing or replacing the history releases it; switching workbench pages keeps it open. |
| Selected body and editor response | Viewer data for the current response. Replacement or closure releases it; a history row switch may keep one disabled previous detail for up to 180 ms. Formatted text is unavailable at or above 512 KiB. |
| History indexes | Up to four recent histories, evicted least-recently-used first. |
| Codec pools | Reusable zstd/Brotli buffers. zstd uses a 1 MiB encoder window and rejects decoder windows above 8 MiB. |
| MyGo resources | Text layouts, element states, frame arenas, atlases, and drawing buffers. Native memory is separate from Go heap measurements. |

History scans skip unused headers, trailers, certificates, and TLS fields. Details load metadata first and bodies only when needed. Closing or replacing a selection cancels reads and rejects late results. Export, resend, Copy as cURL, and Edit Request load full records when needed.

## Application-owned UI resources

- **App icon:** `widgets.AppIcon` shares one bitmap decoded at 256 pixels. Title bars and Software Update display it at 18 and 48 DIPs; GPU textures are per window.
- **Traffic table:** `widgets.Table` uses `nui.KeyedTableState[*logic.TrafficEntry, uint64]` and `SetBorrowedRows`. Nuxt borrows filtered/sorted rows, retains one ordered-ID snapshot and selection, and compacts selection after clear/shrink. FlowLens validates identities and rebinds published rows on the main thread. Other tables use Nuxt's copied string-keyed API.
- **Cells:** Each built traffic cell retains a decoration container for backgrounds, markers, and hit testing. `BuildCell` supplies it only during its synchronous callback. Drawing callbacks retain visual values, never elements. Native controls own selection, keyboard input, and accessibility.
- **Labels:** Table previews are bounded to 256 UTF-8 bytes, including the ellipsis, without splitting runes. Search, sorting, copy, inspection, and export use full values. Visual clipping alone does not bound text shaping.
- **Choices and menus:** State is per window and owns submitted items. `widgets.SelectState` resubmits on data/locale changes. Menu revisions must cover descriptors and captured callback targets; context menus prepare on opening, while dropdowns also prepare when closed. Selection callbacks copy value snapshots into application state. Tables use `OnChange` for selection, cursor, sort, and layout notices.

### Live and Snapshot Policies

Live capture uses `logic.TrafficLive`: filters reuse matching caches and pointer buffers, while sorters cache ID-keyed comparison values. History uses `logic.TrafficSnapshot`, selected explicitly and retained across resets.

A successful `ListHistoryEntries` transfers exclusive ownership of its result array to the store. Nil compaction clears its tail in place; duplicate IDs fail before publication, preserving the files and previous history. Published records are immutable, and test backends clone reusable fixture arrays.

Snapshot filtering borrows the source in All mode, owns search/tab results when needed, and adds a final result only for category filters. Source revisions invalidate aliases even when IDs and lengths match. `BaseVersion` invalidates facets independently of the final subset. Release clears owned buffers; empty results release them, and capacity above `max(64, 4 * result length)` is compacted.

Snapshot sorting retains ordered pointers only. Comparison records and folded text become reclaimable after sorting; source/result revisions and sort state control recomputation. Unsorted, empty, and reset states release output. Long mixed-case paths still need temporary folded strings, so numeric-sort retention does not bound text-sort peaks.

All filter/sort results are borrowed until the next refresh or release. Long-running actions and deletion must clone any snapshot they retain.

## Sidebar Cache Lifetimes

Categories cache facets by source, base revision, selected hosts/processes, and locale; structure roots follow the final-result revision. Search prepares filtered items only when inputs change. Live rebuilds retain the 250-ms throttle; source switches and user changes refresh promptly.

Hiding Categories releases derived entries, summaries, tree/listbox state, handles, and menus. Search, filters, and valid expansion/scroll intent survive for the same source. State is replaced by pointer because hiding can occur between build and layout. Reopening rebuilds once; source replacement or closure clears source-specific intent, including while hidden. Empty results also release obsolete nodes.

History uses a native keyed list over session metadata. Only visible rows build controls. Pointer activation and keyboard Enter use native notices; reordering alone cannot open a session. One context menu follows its session key and closes on scroll, removal, or hide. Metadata still scales with the number of saved sessions.

## Matched History-Table Acceptance Fixtures

Run allocation measurements separately from profiling:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll|TableViewport)$' -benchmem -benchtime=20x -count=3
go test ./internal/desktop/capture -run '^$' -bench '^Benchmark(HistorySidebar|CategorySidebar)$' -benchmem -benchtime=20x -count=3

$env:FLOWLENS_TABLE_HEAP_DIR = Join-Path $env:TEMP "flowlens-table-profiles"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableStress$/^Rows50000$/^Idle$' -benchtime=1x -memprofilerate=1 -count=1
go tool pprof -sample_index=inuse_space -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
go tool pprof -sample_index=inuse_objects -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
Remove-Item Env:FLOWLENS_TABLE_HEAP_DIR
```

| Fixture | Measures |
| --- | --- |
| Storage | Bookkeeping after two GCs, with source records kept alive and no UI. All, filtered, sorted, and filtered+sorted are separate workloads. Selection is measured separately; shrink keeps two rows. |
| Stress | Warmed UI heap after bookkeeping exists; mutable `Patch1` exercises live policy. |
| Live | Actual live updates, including selection, sorting, and churn. |
| LongPathScroll | Newly visible rows with 64-byte through 64-KiB paths. |
| Viewport | Rows/cells, node/state counts, and UI heap at 1160-DIP width, three heights, and 5k/50k/100k rows. |
| HistorySidebar | UI for 100/1000 sessions at 240 × 714 DIP, excluding metadata. |
| CategorySidebar | Derived data and UI for 100k rows, with unchanged or changing search, excluding original records. |

Compare medians from matched runs. `B/op` is cumulative allocation, not retained heap or peak memory. UI, bookkeeping, selection, and original records are separate measurements; whole-process profiles overlap them and must not be added to them.

Use real HBIN files and native idle/recovery runs for process-memory claims. Outstanding native acceptance is tracked in the [change tasks](../../openspec/changes/reduce-history-table-retained-memory/tasks.md).
