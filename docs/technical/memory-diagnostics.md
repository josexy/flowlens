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

## Application-Owned UI Resources

- **App icon:** `widgets.AppIcon` shares one bitmap decoded at 256 pixels; title bars and Software Update draw it at 18 and 48 DIPs. GPU textures are per window.
- **Traffic table:** `widgets.Table` binds `nui.KeyedTableState[*logic.TrafficEntry, uint64]` with `SetBorrowedRows`. Nuxt keeps the borrowed rows, one ordered-ID snapshot, and the selection, and compacts selection storage after a clear or shrink. Other tables use Nuxt's copied, string-keyed API.
- **Cells:** Each built traffic cell keeps one decoration container for backgrounds, markers, and hit testing. `BuildCell` passes it only during the callback, and drawing callbacks keep visual values, never elements.
- **Labels:** Cell text is cut to 256 UTF-8 bytes, including the ellipsis, without splitting runes. Search, sorting, copy, inspection, and export use full values. Visual clipping alone does not bound text shaping.
- **Choices and menus:** `widgets.SelectState` resubmits options only when its key, usually the locale, changes. Menu revisions must cover the items and the targets their callbacks capture. Context menus build items when opened; dropdowns also build them while closed.

### Live and Snapshot Policies

Live capture uses `logic.TrafficLive`: the filter caches per-entry matches and reuses its buffers, and the sorter caches comparison values by ID. History uses `logic.TrafficSnapshot`:

- A successful `ListHistoryEntries` hands its result array to the store, which compacts nil entries in place. Duplicate IDs fail the load and keep the previous history. Published records are immutable, so test backends return clones of their fixtures.
- Filtering borrows the source when no search, tab, or category filter applies and owns a result buffer only for the filters in use. A category change reuses the current search result. Buffers are compacted when their capacity exceeds `max(64, 4 * length)` and released when empty or on release.
- `Version` changes with every snapshot result, even when IDs and lengths match. `BaseVersion` changes only with the source or search/tab, so category facets can follow data outside the selected subset.
- Sorting keeps only the ordered pointers. Sort keys, including lowercase copies of long text, are temporary, so text sorts still have a transient peak.

Every filter and sort result is borrowed until the next refresh or release. Background actions and deletion must copy what they keep.

## Sidebar Cache Lifetimes

- **Categories:** Facets are rebuilt when the source, `BaseVersion`, selected hosts/processes, or locale changes; the structure tree follows `Version`; sidebar search only re-filters prepared items. Live capture rebuilds at most every 250 ms, while user actions refresh at once.
- **Hiding Categories:** Leaving the page or collapsing the sidebar releases entries, summaries, tree and listbox state, handles, and menus. Search, filters, expansion, and scroll survive for the same source. Reopening rebuilds once and drops expansion for nodes that no longer exist; a table filter that hides nodes while the sidebar is visible keeps their expansion. Closing or replacing the source clears its navigation, even while hidden.
- **History list:** A native keyed list builds only visible session rows, so UI cost does not grow with the number of saved sessions (metadata still does). Activation uses native notices, so reordering cannot open a session. The row menu follows its session key and closes when the row scrolls out, is removed, or the sidebar hides.

## Table and Sidebar Benchmarks

Run allocation benchmarks separately from profiling. In PowerShell, set `$env:CGO_ENABLED = "0"` and `$env:FLOWLENS_TABLE_HEAP_DIR` instead of the inline variables.

```sh
export CGO_ENABLED=0
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll|TableViewport)$' -benchmem -benchtime=20x -count=3
go test ./internal/desktop/capture -run '^$' -bench '^Benchmark(HistorySidebar|CategorySidebar)$' -benchmem -benchtime=20x -count=3

# Heap profile of one warmed workload, written to FLOWLENS_TABLE_HEAP_DIR.
FLOWLENS_TABLE_HEAP_DIR=/tmp/flowlens-profiles go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableStress$/^Rows50000$/^Idle$' -benchtime=1x -memprofilerate=1 -count=1
go tool pprof -sample_index=inuse_space -top /tmp/flowlens-profiles/BenchmarkTrafficTableStress-Rows50000-Idle.pprof
```

| Benchmark | Measures |
| --- | --- |
| `TableStorage` | Table bookkeeping after two GCs, with no UI and the source records kept alive. All, filtered, sorted, and filtered+sorted run separately; selection and shrink-to-two-rows are separate cases. |
| `TableStress` | Warmed UI heap; the mutable `Patch1` case uses the live policy. |
| `TableLive` | Live updates with selection, sorting, and churn. |
| `TableLongPathScroll` | Newly visible rows with 64-byte to 64-KiB paths. |
| `TableViewport` | Built rows/cells, node and state counts, and UI heap at 1160 DIP wide, three heights, and 5k/50k/100k rows. |
| `HistorySidebar` | UI for 100 and 1000 sessions at 240 × 714 DIP, excluding metadata. |
| `CategorySidebar` | Derived data and UI for 100k rows, with and without search changes, excluding records. |

Compare medians of matched runs. `B/op` is cumulative allocation, not retained or peak memory. Table bookkeeping, selection, UI heap, and source records are separate measurements; do not add whole-process profiles to them. Process-memory claims need real HBIN files and native idle/recovery runs; native acceptance for this work is tracked in the `reduce-history-table-retained-memory` OpenSpec change.
