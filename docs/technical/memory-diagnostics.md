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
| Selected body and editor response | Loaded views of the current response; released on replacement or closure (history row changes may retain one disabled previous detail for up to 180 ms until the replacement settles, times out, fails, or closes). Formatted text is unavailable at or above 512 KiB. |
| History indexes | Up to four recent histories, evicted least-recently-used first. |
| Codec pools | Reusable zstd/Brotli buffers; memory depends on concurrency and input windows. zstd uses a 1 MiB encoder window and rejects decoder windows above 8 MiB. |
| MyGo resources | Text layouts, element states, frame arenas, atlases, and drawing buffers. Some trimming requires subsequent frames. |

History scans skip unused headers, trailers, certificates, and TLS fields. Selected metadata is loaded separately; body bytes are requested only by views that need them. Closing or replacing a history/detail cancels reads and rejects late results. Export, resend, Copy as cURL, and Edit Request read full records when needed.

## Application-owned UI resources

`widgets.AppIcon` shares one bitmap decoded by `ui.DecodeBitmapScaled(resources.AppIcon, 256)`. Title bars and Software Update display it at 18 and 48 DIPs. Retained CPU pixels, including mipmaps, measured 0.332 MiB; per-window GPU textures are separate. Decoding briefly uses the full source, and failure leaves usable windows without the icon.

Traffic tables use `nui.Table` through `widgets.Table`, with
`nui.KeyedTableState[*logic.TrafficEntry, uint64]` and `SetBorrowedRows`. Nuxt keeps
one ordered-ID snapshot and numeric selection while borrowing the already
filtered/sorted rows; it does not retain copied row values, string IDs or a full
validation map in this mode. FlowLens validates source identities before binding.
Other surfaces retain Nuxt's copied string-keyed API. Nuxt also owns selection
compaction, theme mapping, layout and the header divider. Each built traffic
cell, including the fill cell, retains the existing decoration container;
`BuildCell` supplies that element only as a synchronous callback parameter, plus
the displayed index/value/selection metadata. Traffic-specific backgrounds,
markers and hit testing use that container. Draw callbacks retain visual scalars
only; MyGo remains authoritative for native selection, keyboard and accessibility.
Large selection maps are replaced after clear/shrink with stable bound addresses
and native cursor/anchor state.

The dependency is pinned to the released
[Nuxt UI Go v0.2.0](https://github.com/josexy/nuxt-ui-go/releases/tag/v0.2.0)
in `go.mod`, including the borrowed-row and collection optimizations from PRs
#18 and #20. Its `ui/` sources match the previously verified PR #20 commit
[`41c7fbd746e0efa7b302c7719e4ee000514dfb0d`](https://github.com/josexy/nuxt-ui-go/commit/41c7fbd746e0efa7b302c7719e4ee000514dfb0d).
Borrowed row updates avoid a temporary selection map when all selected IDs still
exist, including updates after Select All. Column styles are merged once per
build. FlowLens retains decorated traffic cells for backgrounds, markers and hit
testing; these custom cells do not use Nuxt's direct-text path for `Value` columns.
`TestTableAllSelectedPatchesHaveBoundedAllocations` checks allocation bytes,
selection/detail preservation and pruning after shrink;
`BenchmarkTrafficTableLive/Rows5000/AllSelectedPatch1` measures the full update.

On Windows amd64 (Go 1.27.3, i9-14900HX, `CGO_ENABLED=0`), three 20-iteration
samples on 2026-11-28 gave median allocations of 200,280 B/op and 1,023 allocs/op
at `a07e537f3bed`, versus 56,565 B/op and 1,006 allocs/op at `41c7fbd746e0`
(about 72% fewer allocated bytes). Both built 297 cells/op. The same 10,000-row
regression test fails against the older dependency at 295,584 bytes per patch
and passes against `41c7fbd746e0` with a 16 KiB ceiling. These are headless
allocation measurements, not native frame-time or process-memory claims.

Choice controls own persistent submitted options/items. `widgets.SelectState`
resubmits on data/locale changes, while menu wrappers refresh on opening, locale,
action or explicit revisions. Listbox and font selections use `SelectionState`
and take value snapshots in change callbacks. Table changes use native notices
instead of the removed `OnSelectionChange` callback. Keep these revisions and
state lifetimes intact when adding options or changing callback targets.

Cell text is bounded before shaping to 256 UTF-8 bytes, including a trailing ellipsis when shortened; valid runes are never split. This affects labels only: filtering, sorting, inspection, copy, and export still use complete values. Single-line clipping alone does not bound text-engine allocations for long paths.

Live filters and sorters reuse compact matching caches, ID-keyed sort values,
and pointer buffers on the main thread; history uses the snapshot policy described
below. Result slices are borrowed views until the next refresh/sort, so persistent
action snapshots must clone them. Obsolete entries and removed pointer tails are
released. Nuxt's borrowed binding detects membership/order changes from its ID
snapshot and preserves stable-patch gestures. All original history records remain
available; a borrowed source must be rebound on the main thread after publication.

`BenchmarkTrafficTableLongPathScroll` measures newly visible rows with 64-byte through 64-KiB paths; use it alongside `BenchmarkTrafficTableLive` for selected/sorted updates. Idle frames alone do not cover text shaping when scrolling.

`BenchmarkTrafficTableStress` measures warmed retained UI heap with its tester alive, separately from timed allocations. Set `FLOWLENS_TABLE_HEAP_DIR` to save heap profiles. Ordinary checks use `CGO_ENABLED=0`:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTable' -benchmem -count=5
```

Headless UI savings do not predict the same percentage reduction in native process memory.

## Matched history-table acceptance fixtures

Use `d932ef5` as the baseline for `reduce-history-table-retained-memory`. Keep a
detached checkout for the original storage/stress/live/long-path command. Run
the expanded harness before production changes, then use the same fixtures for
each candidate stage, on the same machine, toolchain and pinned dependencies:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll|TableViewport)$' -benchmem -benchtime=20x -count=3
go test ./internal/desktop/capture -run '^$' -bench '^Benchmark(HistorySidebar|CategorySidebar)$' -benchmem -benchtime=20x -count=3
```

Compare medians of three samples. The [memory review](history-table-memory-review.md#implementation-acceptance-evidence)
records the environment, baseline measurements and stage results. The expanded
baseline adds only fixtures and diagnostic counters; both explicitly named
Snapshot and Live workloads initially use the existing live-cache implementation.
The candidate Snapshot workload selects the implemented history policy. The
[Nuxt reintegration evidence](history-table-memory-review.md#nuxt-borrowed-row-reintegration-2026-11-27)
records both local measurements and published-module verification. The final
integration pins the published Nuxt commit and passes verification with
`GOWORK=off`; the temporary workspace override has been removed.
Mutable stress `Patch1` and actual live capture always exercise the live policy.

History's successful `ListHistoryEntries` transfers exclusive array ownership
to the store. Nil compaction clears its tail in place; records are immutable
after publication. Backends used in tests clone reusable fixture arrays. Duplicate
decoded IDs fail before publication without modifying files or the previous open
history. Numeric ID values retain their existing semantics.

History construction selects `logic.TrafficSnapshot` for its filter, and the
shared table uses that explicit policy for its sorter. Ordinary capture selects
`logic.TrafficLive`; absence of live controls does not imply immutability. Reset
retains the source's policy. Deletion and long-running actions still take their
own snapshots where required; ownership transfer does not remove those clones.

Snapshot filtering borrows the source in All mode, owns only search/tab matches
when those predicates are active, and owns a separate final result only when
category filters are active. Borrowed views are read-only and valid only until
the next refresh or release; consumers retaining an action snapshot must clone
it. Source revisions invalidate aliases even when IDs and lengths are unchanged.
`BaseVersion` independently invalidates facets when data outside the final subset
changes. Release clears only owned buffers. Empty results drop buffers, and
capacity above `max(64, 4 * result length)` is replaced with compact storage.

Snapshot sorting retains only the current ordered pointers. Dense comparison
records and full case-folded strings become reclaimable after the sort returns;
source/result revision and sort state gate recomputation. Unsorted/empty/reset
releases the ordered output. Full mixed-case paths still require temporary folded
strings, so retained numeric-sort savings do not imply a bound on text-sort peak
memory. Live capture continues to retain incremental ID-keyed comparison values.

- Storage preallocates and keeps all source records alive through two-GC samples,
  builds no table UI, and verifies zero selected rows. All, search `h`, numeric
  status sort, and search+sort are separate workloads. `selected-B` measures the
  incremental Select All storage; SelectionClear and Shrink report the retained
  remainder after clearing selection (Shrink keeps two rows). Source record bytes
  are excluded even after shrinking the active view.
- Viewport uses 1160-DIP width and heights 714/1414/2114 at 5k/50k/100k source
  rows. It reports settled row/cell builds, read-only pinned-Tester node/state
  counts, and collected UI heap after bookkeeping already exists.
- HistorySidebar uses 100/1000 metadata-only sessions at 240 × 714 DIP. No
  unopened history entries or indexes are loaded. Its UI delta excludes metadata.
- CategorySidebar uses an uncapped 100k-row history at 240 × 714 DIP, either
  unchanged or with a changing host search. Its heap delta includes derived
  filter/category data and sidebar UI, and excludes original source records.
  Facet/root/prepared-item counters distinguish recomputation from virtual rows.
- Timed B/op and allocs/op exclude fixture setup and forced collection. UI heap,
  bookkeeping, selected IDs, original records, and native memory are separate
  quantities; whole-process pprof totals overlap these samples and must not be
  added to them. Use actual HBIN/native idle runs for process-memory claims.

## Sidebar cache lifetimes

Categories cache facets by source, base revision, selected hosts/processes and
locale; structure roots follow the final-result revision. Sidebar search filters
those summaries/roots and prepares listbox items only when its normalized query
or the relevant source data changes. Live source rebuilds keep the 250-ms throttle;
source switches and user filter/query/locale changes update promptly. Unchanged
frames build virtual controls without rescanning all records.

The shell calls a surface's sidebar-hidden callback once when navigation or
collapse hides it. Categories drop derived entries, summaries, tree nodes,
listbox data, handles and menus, preserving scalar/key navigation intent. Tree
and listbox state are replaced by pointer: a reset can happen after they were
built but before that frame's layout. Reopening rebuilds once and prunes invalid
intent; source close/replacement clears source-specific intent even while hidden.
Empty/no-match states also drop obsolete tree nodes. No background job maintains
hidden caches.

Saved sessions use a native keyed list over the existing metadata. Pointer row
activation and keyboard Enter use native notices; index changes alone cannot
activate another session. One menu is bound to the target session key and visible
anchor, and closes on scroll/removal/hide. There is no all-session menu-state map.
At 240 × 714 DIP, both 100 and 1000 sessions build 19 rows and 191 nodes/states;
the reviewed 1000-session UI delta falls from about 40.24 MiB to 1.80 MiB.
Metadata remains O(saved sessions), and source record or native-process savings
are not part of that UI measurement.
