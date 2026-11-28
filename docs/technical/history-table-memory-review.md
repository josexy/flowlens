# History Traffic Table Rendering Memory Review

- **Reviewed revision**: `d932ef5` (`perf(capture): bound cell text and reuse traffic table caches`)
- **Historical baseline**: `dd1853a` (`perf(desktop): reduce UI memory and smooth history detail switching (#35)`)
- **Review date / environment**: 2026-11-27, Windows `amd64`, Go 1.27.3, Intel i9-14900HX, `CGO_ENABLED=0`
- **Dependencies at the reviewed revision**: `github.com/egoist/mygo v0.10.0`, `github.com/josexy/nuxt-ui-go v0.1.2-0.20261119040559-42aa9b196036`
- **Evidence**: source inspection, matched headless benchmarks, an allocation profile with `-memprofilerate=1`, a temporary viewport/struct-size probe, and the capture/store/logic tests.

FlowLens now pins the released Nuxt UI Go v0.2.0. The versions and measurements
below describe their historical stages; see
[Application-owned UI resources](memory-diagnostics.md#application-owned-ui-resources)
for the current integration and choice-control migration.

> [!IMPORTANT]
> **Assessment**: The original report correctly identified working row virtualization, substantial full-history bookkeeping, and significant UI cost per built row. Its findings must, however, be tied to their revision.
>
> At `d932ef5`, unfiltered match maps, repeated full-list allocations for stable-ID patches, and unbounded table-label shaping have already been addressed. These are no longer pending fixes. Retained storage is still substantial: the 100,000-row fixture retains approximately **11.48 MiB unsorted**, **13.73 MiB with search enabled**, or **22.25 MiB sorted**, excluding the records themselves and rendered UI. The 27-row table UI adds approximately **6.08 MiB** in the headless fixture.
>
> The remaining evidence supports a memory-efficiency review, not a demonstrated unbounded leak or a P0 incident. Passing release tests does not prove that every callback/cache/native allocation is released, and allocation totals do not establish a fixed 2× switching peak.

## 1. Measurement Scope and Limits

The benchmarks are defined in [table_benchmark_test.go](../../internal/desktop/capture/table_benchmark_test.go). They use synthetic records and an uncapped source to model history-sized tables; they do not open real HBIN histories.

| Metric | What it measures | What it does not establish |
| --- | --- | --- |
| `cells/op` | Cell callbacks across the builds performed by one operation | Total retained data, or work outside the table |
| `ui-retained-B` | Change in process-wide `HeapAlloc` after warming a second table Tester, with two GCs before each sample | Total history memory, native/GPU memory, or a peak |
| `storage-B` | Collected heap added by filter/sorter/table bookkeeping, after fixture records already exist and before building this table's UI | Record/string payloads, history indexes, sidebars, details, or disk decoding |
| `B/op`, `allocs/op` | Cumulative Go allocations during the timed operation | Live retained memory or maximum simultaneous memory |
| `inuse_space`, `inuse_objects` | Allocation-site attribution for the whole profiled process | Ownership by one window, or exact struct counts at every site |

The stress benchmark takes its baseline **after** `table.refreshRows()`. Its ~6.08 MiB therefore excludes the full-list storage; it is predominantly the newly created Tester's nodes, states, layouts, atlas, and painter/scene buffers. Small unrelated process allocations can also affect the difference.

Heap-profile entries marked cumulative include their callees. Do not sum overlapping cumulative entries or add a whole-process profile to the heap differences. The default 512-KiB sampling rate is too coarse for confident attribution of many sub-MiB allocations; this review uses `-memprofilerate=1` for attribution, separately from normal-speed benchmark runs. Tiny-allocation packing, GC timing, capacity growth, and allocator rounding still matter.

The original report's Linux `arm64` measurements and unpublished probes were not independently reproduced in this review. The current numbers below are matched Windows measurements. Struct sizes, size classes, fonts, and renderer behavior must be checked for the target build; equality across every 64-bit platform/toolchain is not guaranteed.

## 2. Reproduced Measurements

### 2.1 Warmed Table UI and Stable-ID Patches

1160 × 714 DIP, light theme, 11 data columns, short labels, no resolved process/status badge in the visible fixture rows. One settled frame builds 27 rows and 297 cells. Measurements use `-benchtime=20x -count=1`.

| Mode | Rows | `cells/op` | `ui-retained-B` | `B/op` | `allocs/op` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Idle | 5,000 | 297 | 6,370,104 | 22,192 | 544 |
| Idle | 50,000 | 297 | 6,369,880 | 22,192 | 544 |
| Idle | 100,000 | 297 | 6,371,416 | 22,192 | 544 |
| Marked | 5,000 | 297 | 6,930,120 | 22,576 | 548 |
| Marked | 50,000 | 297 | 6,933,208 | 22,576 | 548 |
| Patch1 | 50,000 | 297 | 6,371,432 | 22,448 | 547 |
| Patch1 | 100,000 | 297 | 6,371,432 | 22,448 | 547 |

Matched baseline runs reproduce the old patch cost:

| Patch1 workload | `dd1853a` B/op / allocs/op | `d932ef5` B/op / allocs/op |
| --- | ---: | ---: |
| 50,000 rows | 2,259,840 / 50,582 | 22,448 / 547 |
| 100,000 rows | 4,511,649 / 100,710 | 22,448 / 547 |

These patches replace the last record without changing its ID or order in an unsorted table. They demonstrate reduced allocation for this path, not constant-time refresh: filtering and ordered-ID comparisons still scan the list. Sorting changes, filtering membership changes, and scrolling are different workloads.

### 2.2 Full-List Retained Storage

The same `BenchmarkTrafficTableStorage` harness from `d932ef5` was run against both revisions; the baseline sources were extracted into a temporary directory. This benchmark did not exist at the older revision, so the harness was copied there without changing production code.

| 100,000-row mode | `dd1853a` storage-B | `d932ef5` storage-B | Interpretation |
| --- | ---: | ---: | --- |
| All, unsorted | 12,003,888 (11.45 MiB) | 12,037,440 (11.48 MiB) | Essentially unchanged |
| Filtered | 11,993,696 (11.44 MiB) | 14,402,032 (13.73 MiB) | About 2.30 MiB more retained |
| Sorted | 25,389,920 (24.21 MiB) | 23,331,312 (22.25 MiB) | About 1.96 MiB less retained |

`Filtered` sets search text to `"h"`, which matches these fixture rows. `Sorted` sorts by status code and has no active filter. These are separate modes, not cumulative measurements of combined filtering and sorting. At 5,000 rows, current All / Filtered / Sorted storage is 650,464 / 798,288 / 1,347,152 B.

The newer implementation deliberately retains reusable buffers and identity snapshots to reduce update allocations. The increase in filtered retained memory is real and was already documented in the [archived optimization evidence](../../openspec/changes/archive/2026-11-27-optimize-traffic-table-memory/tasks.md). Allocation improvements must not be presented as equal reductions in retained heap.

### 2.3 What the Adapter Microbenchmark Means

`BenchmarkTrafficSetRows/FullAdapter` still produces approximately:

| Rows | B/op | allocs/op |
| --- | ---: | ---: |
| 5,000 | 59,845 | 4,902 |
| 50,000 | 653,632 | 49,902 |
| 100,000 | 1,321,707 | 99,902 |

It deliberately allocates a fresh `[]int` and formats every ID on every iteration. It demonstrates that cost in isolation; **it no longer models every current `Table.refreshRows` call**. Current refreshes reuse buffers, bypass `SetRows` when ordered IDs are unchanged, and reuse existing strings for IDs that remain in place.

The fixture's IDs start at 1. `strconv` reuses static strings below 100, so the top-of-table idle benchmark misses most ID-formatting allocations. A separate settled-frame probe measured **544 allocations at the top versus 951 after scrolling to row 1,000**. This includes all frame work; the 407-allocation increase must not be assigned solely to `cellBackground`.

### 2.4 Allocation-Site Attribution

Selected entries from the current 50,000-row Idle profile, sampled with `-memprofilerate=1`:

| Allocation site | Retained size, rounded | Scope |
| --- | ---: | --- |
| `nui.TableState[int].SetRows` (cumulative) | 4,305 KiB | ID map ~1,707 KiB, records ~1,568 KiB, pending IDs ~784 KiB, ID strings ~246 KiB |
| `mygo/ui.(*engine).lookState` | 2,899 KiB | States plus allocations made at this site, across both Testers |
| `mygo/ui.(*context).alloc` | 2,881 KiB | Node arena chunks and their slice storage, across both Testers |
| `mygo/internal/scene.(*Atlas).Grow` | 512 KiB | Atlas pages in this Windows fixture |
| `mygo/ui.(*Painter).textForeground` (cumulative) | 434 KiB | Text painting allocations |
| `mygo/internal/scene.(*Differ).Remember` | 388 KiB | Scene diff state |
| `mygo/ui.(*Painter).add` | 328 KiB | Painter operation buffers |
| `mygo/internal/text.(*System).layout` (cumulative) | 308 KiB | Allocations under this layout function, not all text-system memory |

This is a whole-process profile, including synthetic records and the benchmark's helper fixture window. The table above omits the fixture record allocations; that omission does not make the remaining entries a delta for the stress window.

### 2.5 Viewport Height and Long-Path Scrolling

A temporary probe with the same setup and 50,000 records measured the following. Node/state counts were read from the Tester's engine after warmup.

| Height (DIP) | Built rows | Nodes / active states | Retained UI bytes | MiB |
| --- | ---: | ---: | ---: | ---: |
| 714 | 27 | 1,037 / 1,037 | 6,371,400 | 6.08 |
| 1,414 | 52 | 1,937 / 1,937 | 11,477,880 | 10.95 |
| 2,114 | 77 | 2,837 / 2,837 | 16,411,616 | 15.65 |

The increments are about **193–199 KiB per additional built row** in this sample, not a universal constant. Use the table's DIP viewport height, not monitor resolution alone; window chrome, side panels, scaling, and row content affect the result.

The current [long-path scrolling benchmark](../../internal/desktop/capture/table_memory_test.go#L108) moves through 1,000 records in 27-row steps, with 20 timed page changes:

| Path-size fixture | B/op |
| --- | ---: |
| 64 bytes | 4,527,981 |
| 4 KiB | 5,337,926 |
| 16 KiB | 5,336,966 |
| 64 KiB | 5,336,047 |

The near-flat 4–64 KiB results confirm bounded display shaping on this workload. Scrolling still allocates ~5.09 MiB per operation; this is allocation throughput, not ~5.09 MiB of permanent growth per page. The original full paths remain in the records and are excluded from the timed setup.

## 3. Findings at the Reviewed Revision

### 3.1 Nuxt Table Retains Full-List Identity and Record Storage

**Still applicable.** `nui.TableState[int]` owns `rows []tableRecord[int]`, `ids map[string]bool`, `pendingIDs []string`, and formatted ID bytes. The records store an index into FlowLens's rows, not another copy of each `TrafficEntry`.

At 100,000 sequential IDs, these structures account for approximately **8.4 MiB**. The record struct is 32 B, and each pending string slot is 16 B on this build. Clearing pending IDs releases their string references but retains the backing array. ID bytes depend on digit lengths and tiny-allocation packing; “100,000 string allocations” does not mean 100,000 separately retained heap objects.

Current [refreshRows](../../internal/desktop/capture/table.go#L225) also retains `indices []int` and `rowIDs []uint64`: approximately 1.53 MiB combined at 100,000 rows. The identity snapshot is needed because filter/sorter slices are reused. Membership/order changes can still cause ID formatting, but stable-order payload patches no longer rebuild the entire adapter.

The old `pruneSelection` full membership map has been removed. [cellBackground](../../internal/desktop/capture/table.go) still formats IDs per built cell, and ID labels / some selection paths also format them. Reusing `TableState.RowID` or the `chosen` value already supplied by Nuxt is a smaller optimization to investigate before replacing the table.

### 3.2 Filtering No Longer Always Retains Match Maps

**Original finding superseded in part.** [FilterStore.Refresh](../../internal/desktop/store/filter.go#L58) now:

- Uses no match map when search is empty, the tab is All, and both host/process selections are empty.
- Uses **one** `map[*TrafficEntry]trafficFilterMatch` for active filtering, combining base/category results and pruning removed pointers.
- Retains **two reusable pointer buffers**, `base` and `filtered`, even in the unfiltered All view.
- Reserves these buffers using source length, so a highly selective filter does not imply small backing arrays.
- Releases storage on empty/reset and rebuilds oversized storage after substantial shrink.

At 100,000 entries, the two pointer buffers occupy ~1.53 MiB. The active-filter map adds about 2.26 MiB in the measured fixture; category selection does not add another independent full map. Pointer keys and pointer buffers remain GC-scanned.

A future no-copy path could reduce retention, but borrowed-slice ownership and version propagation must remain correct. Live capture replaces entries in its slice in place, so aliasing the source can hide changes from content comparison. The current implementation intentionally maintains separate snapshots.

### 3.3 Sorting Still Adds Full-List Storage, but Reuses It

**Original two-map/reallocation description is obsolete.** [TrafficTableSorter](../../internal/desktop/logic/traffic_table.go#L131) now retains one `map[uint64]trafficSortRecord` and a reusable sorted pointer slice. On this build, `sortValue` is **48 B**, and `trafficSortRecord` is **64 B**; the old 32-B figure applied to the baseline's `sortValue`.

The numeric-sort fixture adds approximately **10.77 MiB** over the current unsorted storage. Sort-order changes reuse the map/slice; the claim that every re-sort allocates three fresh full-size maps no longer applies. Capacity growth/shrink can allocate new storage, and changing a text sort key can allocate new sort values.

Text values now cache `strings.ToLower(text)`. If case folding requires a new string, full-length string bytes remain retained in addition to map storage. Therefore numeric-sort results are not an upper bound for sorting long or mixed-case paths. The 256-byte display limit deliberately does not apply to sorting, filtering, copy, or export.

[Table.Reset](../../internal/desktop/capture/table.go#L131) still preserves sort order, so another history can immediately repopulate the sorter. A history-specific sorter that retains only sorted pointers is a possible design, but must invalidate on source/filter/membership/sort changes and be measured against the present reusable implementation.

### 3.4 Per-Built-Row UI Cost Is Real; Nodes and States Are Reused

**Still applicable, with corrected accounting.** The short-label fixture adds 36 nodes per built row: two row containers, 11 native cells, 11 Nuxt cell wrappers, and 12 content nodes. Status badges and resolved process content increase this count; a trailing fill column adds more cells/wrappers.

The probe measured `ui.node` at 2,176 B and `ui.state` at 2,216 B. Nodes are allocated in 32-node chunks: 69,632 B of payload rounds to a 73,728-B heap allocation on this build. State allocations use the 2,304-B class. This yields approximately **4.5 KiB per node/state pair**, before maps, text, and paint buffers, or ~162 KiB for a 36-node row.

These are retained/reused structures, **not newly heap-allocated for every element every idle frame**. MyGo reuses node arenas and stable states, prunes obsolete states after builds, and retains up to 256 free states. Exit transitions and kept-alive views can extend lifetimes.

Nuxt's extra wrapper contributes 11 nodes per ordinary built row. Removing it could avoid roughly **1.3 MiB at 27 rows / 2.5 MiB at 52 rows** of node/state storage. These are structural estimates, not measured savings from an implemented alternative.

### 3.5 Unbounded Table-Label Shaping Has Been Fixed

**Resolved for the traffic table.** [TrafficCellPreview](../../internal/desktop/logic/traffic_preview.go) bounds labels to **256 UTF-8 bytes including the ellipsis**, without splitting valid runes. The text helpers in [table.go](../../internal/desktop/capture/table.go) and badge helper in [table_style.go](../../internal/desktop/capture/table_style.go) apply it before constructing `ui.Text`.

The original mechanism was valid: MyGo's `textWidths` measures unconstrained text, so `SingleLine`/visual clipping alone does not bound shaping work. But recommending a new 512–1024-rune path limit would overlook the existing stricter limit and the regression coverage.

This fix bounds displayed-label shaping, not record payload size, all surrounding UI labels, or full-value search/sort work.

### 3.6 Text Cache Retention Is Frame-Based, with a Larger Live Budget

**Applicable, but the old “up to ~8 MiB while idle” statement is too strong.** In the pinned MyGo `internal/text/layout.go`:

- `maxLayouts = 4096`, ordinary estimated layout budget `maxLayoutBytes = 8 MiB`.
- Recently used layouts can remain above that budget within `maxLiveLayoutBytes = 64 MiB`; layouts over 8 MiB have additional admission rules, with `maxLargeLayoutBytes = 32 MiB`.
- Over-budget eviction becomes possible when the oldest layout is at least eight global text-system frames old.
- Age-only eviction occurs when a layout has not been used for **more than 240 frames**.
- No layout-cache timer advances these counters while all windows are idle. Stopping during a recently used burst can therefore leave **more than 8 MiB** cached until subsequent frames.

These are accounting budgets for the layout cache, not strict bounds for all text memory: fonts, glyph atlases, node-held layouts, and other buffers are separate. Native windows share a text system, whereas each Tester owns one. Native window idle handling also calls `ReleaseShaping`, which releases shaping-engine scratch rather than emptying the layout cache. A headless snapshot cannot substitute for native idle-recovery measurement.

### 3.7 Surrounding Sidebars Need Separate Attribution

**History list:** [historySidebar](../../internal/desktop/capture/history.go) still constructs every saved-session row under `ui.Scroll`. Each closed-menu row has approximately seven nodes plus menu state, so 100 sessions imply roughly 3 MiB of node/state storage before text and paint buffers. This is a structural estimate, scales with the number of **saved sessions**, and is not included in the stress-table measurement.

**Categories:** [category.go](../../internal/desktop/capture/category.go) mixes cached retained data with per-build work:

- `BuildCategoryFacets` and listbox item preparation run when the sidebar is built.
- `FilterStructureByHost` returns the existing roots without copying for an empty query; a nonempty query clones only the root slice.
- `BuildStructureTree` is version/context-driven, and `updateTree` is gated by dirty/search state. Tree nodes and closures are **not all reconstructed every frame**.
- The category lists/tree use [categoryViewport](../../internal/desktop/capture/category_scroll.go) to virtualize UI rows, while derived tree/model data can still scale with the full source.
- Hiding the sidebar can leave derived data retained; reset/replacement releases it. Releasing on hide trades lower retention for rebuild cost on return.

### 3.8 History Switching Can Overlap Data, but Has No Proven Fixed Multiplier

[HistoryStore.Select](../../internal/desktop/store/history.go#L387) keeps history A available while B is read, so A's state and B's newly decoded summaries can coexist. On success, filter release and `HistoryTraffic.Load` → `Reset` drop old application references before the new table is populated.

The peak depends on the sizes of A and B, decoder/index storage, details, frame callbacks, and GC timing. It is **not necessarily two complete table states**, and neither `B/op` nor steady-state profiles establish a fixed 2× process-memory peak.

The `slices.Clone(entries)` before removing nils is a shallow copy of pointers, not a deep copy of all records: approximately **0.77 MiB for 100,000 rows** on this build. The production [ListHistoryEntries](../../internal/services/history_service/history_service.go#L287) returns a fresh slice, making in-place filtering a candidate. Before changing it, make exclusive ownership explicit for the `HistoryBackend` contract and implementations/test doubles.

## 4. What Existing Verification Supports

| Area | Supported conclusion | Boundary |
| --- | --- | --- |
| Row virtualization | The tested table builds 297 cells regardless of 5k–100k source rows | Full-list data/storage remains; other columns, interactions, and viewports change build counts |
| History closure/replacement | Reset drops application-owned table/filter/category state; repeated-close tests show selected row/body objects become unreachable | Tests wait for transitions, build more frames, and force GC; they do not prove universal callback/native-cache release |
| Row-height/state management | MyGo stores measured heights in sparse 64-row blocks and reuses/prunes states | Height blocks can accumulate as more rows are visited; this is not a guarantee that every native-list structure is viewport-bounded |
| Process icons | UI LRU is capped at 256 bitmaps; stored PNGs are normalized to 64×64 | Bounded caches still consume memory; empty-process fixtures do not measure actual icon-heavy history |
| Extra arena for transitions | The table probe found zero spare chunks; the Nuxt tabs indicator does not set `Fly` | Other application transitions can still use spare arenas |

The [release tests](../../internal/desktop/capture/release_test.go), [filter memory tests](../../internal/desktop/store/filter_memory_test.go), [sorter memory tests](../../internal/desktop/logic/traffic_table_memory_test.go), and [table memory tests](../../internal/desktop/capture/table_memory_test.go) passed in this review. This supports the exercised lifetimes and cache behavior, not the blanket statement “no closure leaks.”

## 5. Revised Optimization Roadmap

1. **Take the remaining inexpensive work first.** Investigate reusing stored row IDs / Nuxt's `chosen` argument in per-cell selection checks. Consider removing the one shallow history-load clone after specifying ownership. Cache category facets by the source/filter/search inputs that actually affect them. The unfiltered match-map removal and bounded previews are already implemented.
2. **Measure history-specific storage reductions.** Candidate changes include fewer filter pointer snapshots and a static-history sorter without the live-update value cache. Preserve same-ID replacement, filtering, deletion, sort invalidation, selection, and reset/shrink behavior. Compare retained bytes as well as allocation throughput; neither metric alone is sufficient.
3. **Prototype a table integration with less duplicated storage.** Prefer extending/reusing `nui` where feasible, consistent with the repository's UI conventions. A borrowed-row adapter or numeric identity support could avoid much of the ~8.4 MiB Nuxt bookkeeping at 100k rows. This is a gross opportunity, not guaranteed net savings: the replacement may still need identity/order/selection storage.
4. **Treat wrapper removal as a separate UI problem.** MyGo's cell callback takes `func(row, col int)`, without a native cell `Element`; `HeaderCell` only handles headers and `CellsStyle` handles block selection. Current Nuxt wrappers carry backgrounds, stripes, outlines/markers, row-span hit testing, and double-click handling. Stretching a badge/text element is not automatically equivalent to styling the full cell. A native decoration hook may be needed; adding another wrapper preserves the node cost. Validate light/dark styling, column sizing/reordering/hiding, selection, context menus, shortcuts, and accessibility.
5. **Optimize surrounding views when their own profiles justify it.** Virtualize the saved-session list for large session counts, and consider releasing hidden category-derived data. Capture real history load/switch/scroll/idle/close curves before setting a memory target or claiming a peak reduction.

A direct replacement of `nui.Table` is therefore a candidate requiring a prototype and behavioral validation, not an already-proven highest-ROI fix.

## 6. Reproduction and Remaining Verification

Run ordinary allocation measurements separately from allocation-site profiling:

```powershell
$env:CGO_ENABLED = "0"

# Warm UI, full-list storage, and the intentionally allocating adapter microbenchmark.
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStress|TableStorage|SetRows)$/^Rows(5000|50000|100000)$/^(Idle|Patch1|Marked|All|Filtered|Sorted|FullAdapter)$' -benchmem -benchtime=20x -count=1

# Newly visible text and rows.
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableLongPathScroll$' -benchmem -benchtime=20x -count=1

# Existing lifecycle, preview, filter, sorter, and selection coverage.
go test ./internal/desktop/capture ./internal/desktop/store ./internal/desktop/logic -count=1

# Whole-process profile captured with the warmed fixture still alive.
$env:FLOWLENS_TABLE_HEAP_DIR = Join-Path $env:TEMP "flowlens-table-profiles"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableStress$/^Rows50000$/^Idle$' -benchtime=1x -memprofilerate=1 -count=1
go tool pprof -sample_index=inuse_space -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
go tool pprof -sample_index=inuse_objects -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
Remove-Item Env:FLOWLENS_TABLE_HEAP_DIR
```

The height/count probe reused the stress fixture setup, varied the second Tester's height, and read `rt.c.used`, `rt.states`, `rt.c.chunks`, and field types through reflection. It ran as a temporary Go test overlay; no production code or permanent test was changed. It is additional diagnostic evidence, not an existing named repository benchmark.

All three test packages passed. This review changes only this report. Native process/GPU memory, real-file switching peaks, native idle recovery, macOS/Linux behavior, and combined large-history/filter/text-sort/category workloads were not measured here. Follow [Memory Diagnostics](memory-diagnostics.md) for those measurements; none can be inferred solely from the headless table numbers.

## Implementation acceptance evidence

### Re-established baseline (2026-11-27)

The apply run checked out `d932ef5` in the detached worktree
`C:/Users/asdzx/AppData/Local/Temp/flowlens-history-memory-d932ef5` and ran:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll)$' -benchmem -benchtime=20x -count=3
```

All benchmarks passed. Environment: Windows amd64, Go 1.27.3, Intel i9-14900HX,
32 logical processors, MyGo v0.10.0 and Nuxt v0.1.2-0.20261119040559-42aa9b196036.
The complete local log is `C:/Users/asdzx/AppData/Local/Temp/flowlens-history-memory-baseline.log`.
Selected medians of three matched samples reproduce the reviewed scale:

| Fixture | Retained bookkeeping B | Retained UI B | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Storage / 100k / All | 12,037,440 | — | 16 | 2 |
| Storage / 100k / Filtered | 14,402,032 | — | 16 | 2 |
| Storage / 100k / Sorted | 23,331,312 | — | 16 | 2 |
| Stress / 50k / Idle | — | 6,371,400 | 22,192 | 544 |
| Stress / 100k / Idle | — | 6,371,528 | 22,192 | 544 |
| Stress / 100k / Patch1 | — | 6,371,432 | 22,448 | 547 |
| Live / 5k / SelectedPatch1 | — | — | 54,836 | 1,312 |
| Live / 5k / SortedPatch100 | — | — | 81,846 | 1,375 |
| LongPathScroll / 64 KiB | — | — | 5,336,298 | 13,853 |

Stress rows build 297 cells per operation at 1160 × 714 DIP. Storage keeps the
synthetic source alive before and through the two-GC samples and builds no table
UI. The stress baseline already includes table bookkeeping; its UI delta is a
separate measurement. Timed B/op excludes setup and collection. These results
measure neither native process memory nor HBIN load/switch peaks or idle recovery.

### Expanded harness before production changes

The current-policy run at `d932ef5` plus the measurement-only harness passed
`BenchmarkTrafficTableStorage`, `BenchmarkTrafficTableViewport`,
`BenchmarkHistorySidebar`, and `BenchmarkCategorySidebar` with
`-run '^$' -benchmem -benchtime=20x -count=3`. The full local log is
`C:/Users/asdzx/AppData/Local/Temp/flowlens-history-memory-expanded-baseline.log`.
Both explicitly named Snapshot and Live policies still use the baseline caches
at this stage. Environment and dependencies match the baseline above.

| Fixture | Median retained bytes | Settled work |
| --- | ---: | --- |
| Snapshot / 100k / All | 12,037,432 | zero selection, no UI |
| Snapshot / 100k / Filtered | 14,402,024 | zero selection, no UI |
| Snapshot / 100k / Sorted | 23,331,304 | zero selection, no UI |
| Snapshot / 100k / FilteredSorted | 25,695,896 | zero selection, no UI |
| Snapshot / 100k / SelectionClear | 15,532,520 | incremental Select All: 4,013,984 B |
| Snapshot / 100k to 2 / Shrink | 3,498,984 | zero selection after shrink/clear |
| Viewport / 100k / 714 DIP | 6,371,944 | 27 rows, 297 cells, 1,037 nodes/states |
| Viewport / 100k / 1414 DIP | 11,484,536 | 52 rows, 572 cells, 1,937 nodes/states |
| Viewport / 100k / 2114 DIP | 16,412,944 | 77 rows, 847 cells, 2,837 nodes/states |
| History sidebar / 100 sessions | 4,642,800 | 100 rows, 820 nodes/states |
| History sidebar / 1000 sessions | 42,192,968 | 1000 rows, 8,020 nodes/states |
| Categories / 100k / unchanged | 71,036,040 | 1 facet + 2 item rebuilds/frame, 0 root rebuilds |

Viewport counts are the same at 5k, 50k and 100k. History-sidebar row counts are
currently proportional to sessions, establishing the behavior to change. Category
heap includes its derived tree/filter state and UI; unchanged frames allocate
32,752 B/op and 252 allocs/op. These are separate fixtures, not additive savings.

### Snapshot-filter stage (table adapter and live sorter still present)

The same Windows/toolchain fixture and three-sample storage command passed after
introducing snapshot filter ownership. Local log:
`C:/Users/asdzx/AppData/Local/Temp/flowlens-history-filter-storage.log`.

| Snapshot / 100k workload | Median storage B | Reduction from expanded baseline B |
| --- | ---: | ---: |
| All | 10,431,864 | 1,605,568 |
| Filtered (`h`) | 11,390,328 | 3,011,696 |
| Sorted (status) | 21,725,736 | 1,605,568 |
| FilteredSorted | 22,684,200 | 3,011,696 |

All now borrows its source without pointer buffers. Filtered retains one
result-sized pointer buffer and no history predicate map. These totals still
include the old Nuxt adapter and cached live sort keys; they are intermediate
measurements and do not meet or claim the final table-binding budgets. Source
records, selected IDs, rendered UI and native memory remain excluded.

### Snapshot-sort stage (old table adapter still present)

The three-sample storage command passed with the same environment and fixtures;
log: `C:/Users/asdzx/AppData/Local/Temp/flowlens-history-sort-storage.log`.
100k numeric Sorted retains a median **11,234,696 B**, and FilteredSorted
**12,193,160 B**. Each is 10,491,040 B below the filter-only stage. The filter
and adapter still account for the rest; final binding budgets are not yet met.

The logic/store/capture suites pass snapshot ascending/descending/unsorted order,
unknown-last metrics, ascending-ID ties, full mixed-case text, source immutability,
same-ID replacement and same-size membership changes. A 512-row, approximately
64-KiB mixed-case path test collects after sorting and verifies that retained
incremental heap stays below 1 MiB while source records remain alive. It exercises
release of roughly 32 MiB of temporary folded keys; it is a retention check,
not a peak-memory bound. Reset and empty-result tests release previous pointers.

### Numeric native table adoption gate

The candidate on `codex/reduce-history-table-retained-memory` passed the full
storage/stress/live/long-path/viewport command with three samples, plus
`go test ./internal/desktop/capture ./internal/desktop/widgets ./internal/desktop/shortcuts ./internal/services/setting_service -count=1`
and `task vet:ui`. Local benchmark log:
`C:/Users/asdzx/AppData/Local/Temp/flowlens-native-table-candidate.log`.
No dependency or other production table renderer changed.

| 100k snapshot workload | Median storage B | MiB | Gate MiB |
| --- | ---: | ---: | ---: |
| All | 806,408 | 0.77 | 2.5 |
| Filtered | 1,764,872 | 1.68 | 4.5 |
| Sorted | 1,609,224 | 1.53 | 3.5 |
| FilteredSorted | 2,567,688 | 2.45 | 5.5 |
| Select All then clear | 806,408 | 0.77 | measured separately |
| Shrink from 100k to two, then clear | 3,608 | 0.0034 | measured separately |

Select All adds 2,364,592 B for numeric selection; clear releases that map. There
is one ordered-ID slice, no permanent all-row membership map, string/index adapter
or display-label cache. Mutable 100k Patch1 uses **15,864 B/op, 456 allocs/op**
(baseline 22,448/547), below 32 KiB/700. All actual-live modes reduce B/op and
allocs/op; their median elapsed times improve 2.5–11.6% in this matched run:

| Live mode | Rows | Baseline → candidate B/op | Baseline → candidate allocs/op |
| --- | ---: | ---: | ---: |
| Idle | 500 | 49,200 → 41,768 | 1,155 → 823 |
| Patch1 | 500 | 54,415 → 46,988 | 1,305 → 973 |
| Patch100 | 500 | 81,406 → 74,183 | 1,371 → 1,039 |
| SortedPatch100 | 500 | 81,656 → 74,416 | 1,375 → 1,043 |
| Idle | 5,000 | 49,201 → 41,769 | 1,155 → 823 |
| Patch1 | 5,000 | 54,420 → 46,988 | 1,305 → 973 |
| SelectedPatch1 | 5,000 | 54,836 → 47,388 | 1,312 → 979 |
| Patch100 | 5,000 | 81,616 → 74,185 | 1,371 → 1,039 |
| Patch1000 | 5,000 | 351,247 → 343,816 | 2,275 → 1,943 |
| SortedPatch100 | 5,000 | 81,846 → 74,415 | 1,375 → 1,043 |
| Churn100 | 5,000 | 2,047,283 → 2,019,066 | 11,474 → 6,142 |
| Churn1000 | 5,000 | 2,461,766 → 2,420,348 | 17,772 → 12,441 |

64-KiB path scrolling uses 5,320,252 B/op versus 5,336,298 baseline; the existing
bounded-scroll regression passes. At 714/1414/2114 DIP, the 100k fixture builds
27/52/77 rows and retains median UI heap of 6,367,760 / 11,475,344 / 16,407,960 B.
All 5k–100k viewport samples pass the baseline +10% gate. Counts remain exactly
1,037 / 1,937 / 2,837 nodes and states: **the per-cell wrapper cost is unchanged**.

The render fixture covers en/zh-Hans, light/dark, 16/24-pixel interface text, and
default, fixed/reordered, empty and fill-only tables. All **32 images are pixel
identical** to the same test-only harness run in the isolated `d932ef5` checkout.
The eight default combinations and representative other states were visually
inspected. Native keyboard/drag/menu, resizing, detail, persistence, HAR drop,
high uint64 IDs and selection clear/shrink regressions pass headlessly. This
accepts the repository integration; real-file native Windows recovery and
macOS/Linux acceptance remain separate tasks, not inferred from these results.

### Sidebar acceptance

Windows amd64, the same toolchain/dependencies and 20 iterations × 3 samples:
`go test ./internal/desktop/capture -run '^$' -bench '^Benchmark(HistorySidebar|CategorySidebar)$' -benchmem -benchtime=20x -count=3`.
Local log: `C:/Users/asdzx/AppData/Local/Temp/flowlens-sidebars-final-bench.log`.

| Fixture | Baseline → candidate retained B | Baseline → candidate work |
| --- | ---: | --- |
| 100 saved sessions | 4,642,800 → 1,894,176 | 100 → 19 rows/frame; 820 → 191 nodes/states |
| 1000 saved sessions | 42,192,968 → 1,889,144 | 1000 → 19 rows/frame; 8,020 → 191 nodes/states |
| 100k categories, unchanged | 71,036,040 → 69,441,896 | facets/items/roots: 1/2/0 → 0/0/0 per frame |

History-sidebar allocation is 6,040 B/op, 134 allocs/op for both session counts.
Unchanged categories use 21,568 B/op, 239 allocs/op, with median time 0.278 ms
versus 3.562 ms baseline. A changing search still rebuilds filtered tree/list
data and costs about 6.03 MB/op; it does not rebuild facets or full roots. The
category heap total includes the large visible-source derived tree, not just UI.

The capture/store/shell/widgets/history-service/app suites pass, as does UI vet.
Tests cover base-only changes outside selection, locale/query changes, hidden
selections, outer-scroll keyboard reveal, a 100k tree becoming weakly unreachable
after hide, one rebuild on return, preserved expansion/search/filter/scroll intent,
and source replacement/close while hidden. Shell tests cover one hide notification
for every workbench transition and collapse. Session tests cover 100/1000 rows,
last-row reachability after resize and 150% text scaling, native keyboard Enter,
stable key identity across reorder/removal, pointer/keyboard menu targeting and
menu dismissal on scroll/hide. Existing import/delete/export and loading tests pass.
Both sidebar surfaces were rendered in both languages/themes at 16/24 text sizes
and inspected. Settings/update/startup windows and service persistence are unchanged.

### Final headless acceptance (2026-11-27)

This section records the earlier direct-native candidate. The subsequent
user-requested Nuxt reintegration is recorded separately below; use that later
section for the current architecture and dependency-delivery status.

This is the final headless evidence for OpenSpec tasks 8.1, 8.2 and 8.5. The
candidate is the uncommitted `codex/reduce-history-table-retained-memory` working
tree based on `d932ef5`, including the narrow-sidebar fix and lifecycle regression.
Windows amd64, Go 1.27.3, i9-14900HX (32 logical processors), `CGO_ENABLED=0`,
MyGo v0.10.0 and Nuxt v0.1.2-0.20261119040559-42aa9b196036 match the baseline.
The continuation used command-line tests and saved render artifacts only; native
acceptance remains open as described below.

#### Repository and render checks

The final `task verify` log passes formatting, lint (0 issues), `task vet:ui`,
catalog validation (1,338 messages in each of en and zh-Hans), and `go test ./...`.
No interface strings changed, so catalog generation was unnecessary. After the
last production edit, both documented render commands passed:

```powershell
$env:CGO_ENABLED = "0"
$env:FLOWLENS_RENDER_DIR = Join-Path $env:TEMP "flowlens-history-memory-final-renders"
go test ./internal/desktop/... -run TestRender -count=1
go test ./internal/desktop/capture ./internal/desktop/shell -run 'Test(Category|HistorySidebar|Render)' -count=1
```

The render run produced 212 PNGs. All 32 table matrix images still match the
isolated `d932ef5` measurement harness pixel for pixel: en/zh-Hans, light/dark,
16/24-pixel text, default/fixed-reordered/empty/fill-only. The fixture asserts the
native table accessibility role in each combination. All 16 final history and
category sidebar images were visually inspected across the same language, theme
and text-size combinations. The narrow 170-DIP/24-pixel regression also passes;
the heading and timestamps remain single-line. Existing interaction tests cover
horizontal scrolling, columns, selection and focus, menus, process cells, loading,
details, persistence and HAR dropping. These are headless native-control tests,
not operating-system interaction acceptance.

The scope audit found no changes to settings/update/startup windows, dependencies,
locale catalogs, settings schema, database, HBIN codecs, network models, Python
Workers, process attribution, update or shutdown sequencing. `internal/app`
only wires sidebar visibility callbacks; the history-service edit documents the
existing fresh-array ownership contract. The final follow-up changes only
documentation and the dependency-microbenchmark comment, not runtime behavior.

#### Final matched benchmark gates

The original storage/stress/live/long-path command was repeated in the detached
`d932ef5` checkout, then the candidate table command and sidebar command were run
sequentially. Each used `-benchmem -benchtime=20x -count=3`. The comparison checked
exactly three samples for all 38 original-baseline, 37 expanded-baseline, 65 final
table and 4 final sidebar workloads. FilteredSorted, viewport and sidebar
comparisons use the measurement-only expanded baseline recorded above.

| 100k snapshot, no UI or selection | Baseline B | Final median B | Gate MiB | Result |
| --- | ---: | ---: | ---: | --- |
| All | 12,037,440 | 806,408 | 2.5 | pass (0.77 MiB) |
| Filtered, search `h` | 14,402,032 | 1,764,872 | 4.5 | pass (1.68 MiB) |
| Sorted, numeric status | 23,331,312 | 1,609,224 | 3.5 | pass (1.53 MiB) |
| FilteredSorted | 25,695,896 | 2,567,688 | 5.5 | pass (2.45 MiB) |

Select All adds **2,364,592 B** separately. Clearing it returns the All workload
to **806,408 B**; shrinking from 100k to two rows and clearing retains **3,608 B**.
The preallocated original records remain alive and excluded in all these samples.
Mutable 100k Patch1 remains **15,864 B/op and 456 allocs/op**, below 32 KiB/700.
Every one of the 12 actual-live modes has lower B/op and allocs/op than the matched
baseline, passing the +10% allocation limits.

The slower elapsed times in `flowlens-history-final-table-bench.log` prompted this
sequential repeat. They did not recur at that scale: live medians now range from
9.96% faster to 4.67% slower than the rerun baseline (500-row Idle: +3.77%;
5k SortedPatch100: +4.67%; the other ten modes are faster). This is timing
variability, not evidence of a universal speedup; its external cause was not
established. The 64-KiB path median is 6.883 ms versus 7.036 ms baseline and uses
**5,320,274 B/op** versus 5,336,306. The 4/16/64-KiB variants remain approximately
5.32 MB/op, and the bounded-scroll regression passes.

All nine viewport workloads pass the expanded-baseline +10% retained-UI limit.
At 5k, 50k and 100k rows, the 714/1414/2114-DIP heights each build the same
27/52/77 rows, 297/572/847 cells and 1,037/1,937/2,837 nodes and states.
The final 100k UI medians are **6,367,760 / 11,478,416 / 16,409,384 B**, measured
after bookkeeping exists. Per-cell decoration wrappers remain unchanged.

| Final sidebar workload, 240 × 714 DIP | Median retained B | Work per frame | B/op / allocs/op |
| --- | ---: | --- | ---: |
| 100 saved sessions | 1,891,176 UI | 19 rows, 191 nodes/states | 6,040 / 134 |
| 1000 saved sessions | 1,888,616 UI | 19 rows, 191 nodes/states | 6,040 / 134 |
| 100k categories, unchanged | 69,472,504 derived + UI | 0 facets/items/roots rebuilt | 21,568 / 239 |
| 100k categories, changing search | 69,441,896 derived + UI | 0 facets, 2 prepared item sets, 0 roots rebuilt | 6,032,274 / 18,608 |

Metadata and original record bytes are excluded from the respective sidebar
deltas. Unchanged category median time is 0.308 ms versus 3.562 ms baseline.
Changing search still allocates approximately 6.03 MB/op (baseline 5.51 MB/op);
it is not an unchanged frame or a claim of zero filtering work.

#### Release cycles and allocation-site attribution

`go test ./internal/desktop/capture -run '^TestHistoryMemoryCyclesSettleAfterCollection$' -v -count=3`
passes. Each sample warms once, then runs four 100k-row load/filter/numeric-sort/
Select-All/clear/shrink/hide/close cycles. Each cycle asserts that table rows,
ordered IDs, selection capacity tracking and category tree references are released.
After two GCs, the three end-minus-warm heap deltas are **50,424 / -16,960 / 22,760 B**
(object deltas **69 / 17 / 28**), below the 2-MiB regression bound. These small
residual runtime/UI differences do not show accumulating history working state;
they do not assert an exactly zero heap delta or establish a native leak bound.
The separately measured original 100k synthetic records occupy
**30,414,152 / 30,398,352 / 30,398,528 B** and are kept alive throughout.

Snapshot sorter inspection and the passing full-text retention tests confirm
that history retains ordered pointers, not the former ID-keyed comparison map
or folded long-text keys. Live sorting retains its intentional incremental cache.
The production traffic table has no Nuxt string/index adapter, additional permanent
membership map or full-history display-label cache. `BenchmarkTrafficSetRows`
is explicitly labeled as a dependency microbenchmark of the former adapter.

The documented 50k Idle profile command was rerun with `-memprofilerate=1`, followed
by both `go tool pprof -sample_index=inuse_space -top` and `inuse_objects -top`.
The whole test process retains **24,766.84 KiB / 211,953 objects**. Major allocation
sites include fixture records (`entry`, 11,725.66 KiB flat), MyGo state (2,899.12 KiB),
element allocation (2,881.06 KiB), live pointer buffers (784.02 KiB), and the ordered
numeric-ID slice (392.19 KiB). This unsorted live-policy fixture attributes process
heap; it does not independently prove snapshot sort-key release. Its record and
UI totals overlap the other measurements and must not be added to them. Native
process memory, Held/Process gaps and history-switch peaks remain unmeasured here.

#### Evidence locations and open native acceptance

The command results and medians are recorded above so the acceptance decision
does not depend on keeping temporary files. Detailed local artifacts are under
`C:/Users/asdzx/AppData/Local/Temp/`:

- Verification: `flowlens-history-final-verify.log`, `flowlens-history-final-renders.log`,
  and `flowlens-history-headless-acceptance.log`.
- Comparisons: `flowlens-headless-baseline-recheck.log`,
  `flowlens-headless-final-table.log`, `flowlens-headless-final-sidebars.log`,
  and `flowlens-headless-final-medians.json`; the earlier expanded baseline is
  `flowlens-history-memory-expanded-baseline.log`.
- Cycles: `flowlens-headless-final-cycles.log`.
- Profile: `flowlens-headless-final-profiles/BenchmarkTrafficTableStress-Rows50000-Idle.pprof`,
  with `flowlens-headless-final-profile{,-space,-objects}.log`.
- Images: `flowlens-history-memory-final-renders/`, compared with
  `flowlens-history-memory-baseline-renders/` for the 32 table images.

The implementation, both capability deltas and user/technical documentation agree
on full-value behavior, explicit source policy, ownership, cache invalidation,
native-control delegation and release boundaries. Strict change validation and
`git diff --check` pass. Native body-cell decoration support and shared text-cache
idle trimming remain separate dependency work; neither is implemented or counted
as a saving in this change.

OpenSpec **8.3 remains unchecked**. The interrupted earlier Windows session and
successful executable build do not establish the required real-HBIN interaction,
failed-load fallback, 100%/150% scale, or natural idle 0/30/120/300-second and separate
forced-collection curves. The requested headless continuation did not resume
Computer Use or collect those native results. **8.4 remains unchecked** because
no macOS or Linux native test host was available; their targeted Go tests and
native interaction matrices were not run. Headless 150% scaling and Windows
results do not substitute for these checks. The change remains unarchived with
native acceptance outstanding.

### Nuxt borrowed-row reintegration (2026-11-27)

The user requested moving the generic table optimization into NuxtUI-Go and
returning FlowLens to `nui.Table`. This section supersedes the direct-native
architecture above while retaining its measurements as historical stages.

The measurement sources are the FlowLens working tree based on `d932ef5` and the
NuxtUI-Go working tree based on `478b6eb`, on branches
`codex/reduce-history-table-retained-memory` and `codex/table-borrowed-rows`.
Nuxt was subsequently committed and pushed as
[`ff3b88e73c27f4abdd2cea8b521732ac5f76e54c`](https://github.com/josexy/nuxt-ui-go/commit/ff3b88e73c27f4abdd2cea8b521732ac5f76e54c),
with [PR #18](https://github.com/josexy/nuxt-ui-go/pull/18) targeting main.
The PR is now merged; its library and website CI checks both succeeded.
At that stage, FlowLens pinned `v0.1.2-0.20261127051345-ff3b88e73c27` in go.mod/go.sum.
Initial measurements used an ignored local workspace pointing at
`E:/Codes/Github/NuxtUI-Go`; that stage's final verification used the downloaded published
module with `GOWORK=off`. The task-created workspace files were backed up and
removed, and default resolution selected that same version without a replace.
No module-cache source files were manually edited and MyGo was not upgraded.
Environment remains Windows amd64, Go 1.27.3, i9-14900HX, 32 logical processors,
`CGO_ENABLED=0`.

#### Ownership and API changes

Nuxt now provides `KeyedTableState[T, K]` and `KeyedTableOptions[K]` with string or
integer identities, retaining `TableState[T]` and `TableOptions` as string-keyed
aliases. Existing `SetRows` still copies and validates atomically. The explicit
`SetBorrowedRows` path trusts caller-validated unique stable keys, borrows row
values, keeps one ID snapshot, and owns selection pruning/compaction. Local sorting
uses a separate permutation; manual sorting adds no such array. Numeric zero and
full-width uint64 IDs work in selection, focus and typed callbacks.

FlowLens still validates history IDs before publication and keeps its snapshot/live
filter/sorter policies. Its shared traffic table now calls `widgets.Table` with
borrowed numeric state. The local `ui.Table` renderer, theme/row-height/header-rule
copy, ID snapshot and selection-capacity implementation are removed. Nuxt's
`BuildCell` exposes row index/value/selection plus its existing styled element as
a synchronous parameter, so traffic backgrounds, markers and hit testing remain
business code. No element is stored in the cell metadata struct; both projects'
lifecycle vet passes. There is one production renderer and one existing content
container per built cell.

#### Verification and measurements

Library focused regressions cover copied/borrowed transitions, source immutability,
local/manual/cleared sort, same-ID value updates, reused arrays, numeric zero/high
IDs, typed callbacks, focus, selection cleanup, stable updates with zero row-storage
allocations, and collected 100k-row storage. The existing copied-string tests also
pass. `task lint:go` reports zero issues; `task verify` passes tests, Go/UI vet,
generated-source consistency and Windows amd64/Linux amd64/macOS arm64 cross-builds.
The component API, bilingual showcase, borrowed-data toggle, copyable examples and
generated table skill reference are updated; generic aliases retain their existing
option-editor IDs. `go run ./internal/cmd/genskill -check` passes.

FlowLens's final targeted capture/widgets/shortcuts suite and `task verify` pass,
including lint, UI vet, both catalogs and repository-wide tests. The expanded
desktop render suite passes. All **32 table images** are pixel-identical to the
isolated original baseline; all **16 sidebar images** match the previous verified
candidate. The matrix includes both languages/themes, 16/24-pixel text, and
default/fixed-reordered/empty/fill-only tables. Representative final table images
were inspected again. No new interface strings or unrelated owner changes were
introduced by reintegration.

The documented table and sidebar benchmark commands were rerun sequentially with
`-benchmem -benchtime=20x -count=3`: all 65 table and 4 sidebar workloads contain
three samples. Decision 9's storage/allocation/viewport gates pass against the
matched original/expanded baseline. Final 100k snapshot medians, excluding source
records, selection and rendered UI:

| Workload | Original baseline B | Borrowed Nuxt B | Gate MiB |
| --- | ---: | ---: | ---: |
| All | 12,037,440 | 806,376 | 2.5 |
| Filtered | 14,402,032 | 1,764,840 | 4.5 |
| Numeric Sorted | 23,331,312 | 1,609,192 | 3.5 |
| FilteredSorted | 25,695,896 | 2,567,656 | 5.5 |

These remain approximately **0.77 / 1.68 / 1.53 / 2.45 MiB**. Select All adds
2,364,592 B separately; clear returns to 806,376 B and 100k-to-two-row shrink/clear
retains 3,576 B. Mutable 100k Patch1 uses **21,296 B/op / 484 allocs/op**, within
32 KiB/700 and below the original 22,448/547. It exceeds the direct-native stage's
15,864/456 by 5,432 B/op and 28 allocations, reflecting the restored common Nuxt
path; retained all-row copies have not returned. All 12 actual-live modes pass
their baseline +10% B/op/allocs limits. Median times in this run are 8.1–20.5% below
the earlier matched baseline, but timing varies and is not a portable speed claim.
64-KiB path scrolling uses **5,331,204 B/op**, within its +10% gate.

At 714/1414/2114-DIP heights, 5k/50k/100k fixtures still build 27/52/77 rows,
297/572/847 cells and 1,037/1,937/2,837 nodes/states. The 100k UI medians are
**6,370,664 / 11,482,744 / 16,412,176 B**, all within baseline +10%. The wrapper
node cost is unchanged. At 240 × 714 DIP, 100/1000 saved sessions both build
19 rows and 191 nodes/states, retaining 1,888,488/1,888,504 B of UI heap.
Unchanged 100k categories retain 69,441,928 B of derived data plus UI, with
zero facet/root/item rebuilds and 21,568 B/op / 239 allocs/op. Changing search
still costs approximately 6.03 MB/op; it rebuilds prepared items, not full facets
or roots.

Three lifecycle samples each ran a warm-up plus four load/filter/numeric-sort/
select/clear/shrink/hide/close cycles. Collected end-minus-warm heap deltas are
**15,352 / -43,640 / -40,392 B** (object deltas 51/4/-13), within the 2-MiB
regression bound. Original 100k synthetic records were separately measured at
30,398,752 / 30,398,416 / 30,398,432 B and kept alive. The tests verify released
table/selection/category state; these small residuals do not imply exact-zero
heap growth or native process recovery.

The full-sampling 50k Idle profile and both pprof top commands passed. The whole
test process retains **24,749.61 KiB / 211,994 objects**, including source records,
fixture state and UI. The numeric ID slice accounts for approximately 392 KiB;
the borrowed path has no retained copied records or ID membership map. Snapshot
sort-key release is established by its dedicated tests and source inspection;
this unsorted live-policy profile is not that test. These quantities overlap
other heap measurements and are not added together or equated to native memory.

Final local artifacts are under `C:/Users/asdzx/AppData/Local/Temp/`:

- `flowlens-nui-borrowed-{targeted,verify,ui-vet,renders,table-bench,sidebars,cycles,profile,profile-space,profile-objects}.log`
- `flowlens-nui-borrowed-medians.json`, `flowlens-nui-borrowed-renders/`, and
  `flowlens-nui-borrowed-profiles/BenchmarkTrafficTableStress-Rows50000-Idle.pprof`
- `nui-borrowed-{lint,verify,regressions,website-check,website-build}.log`;
  the final full verification supersedes the earlier regression log's resolved
  generator/option-editor failures.

Nuxt's `task website:check` and `task website:build` pass with Node 24.22.2. The
check includes formatting, type checking, 73 standalone example builds, every
public option configuration and preview tests; the build generated 302 routes.
The initial missing Shiki dependency was resolved by restoring the locked website
dependencies without changing package.json or the lockfile. Installation reported
skipped dependency build scripts; the subsequent checks/build succeeded without
enabling them. Static prerendering emitted remote icon timeout warnings, so it
does not establish browser rendering or network availability. Browser and native
interaction are not run under the user's headless-only instruction.
OpenSpec 8.3 and 8.4 remain unchecked; cross-builds and headless input tests do not
satisfy those native tasks. Task 5.6 is complete after the user's explicit commit,
push and PR authorization. Go resolved the published commit via Git/SSH after
HTTP transport failures, with checksum validation retained. `go mod tidy` changes
only the Nuxt pin/checksums, `go mod verify` reports all modules verified, and the
downloaded table implementation files match the committed source hashes.
`GOWORK=off CGO_ENABLED=0 task verify` passes formatting, lint (zero issues), UI vet,
both catalogs and all repository tests; its local log is
`C:/Users/asdzx/AppData/Local/Temp/flowlens-nui-published-verify.log`. No additional
benchmark repetition was needed because the published implementation matches the
measured source. FlowLens changes remain uncommitted, and the OpenSpec change is
not archived while native acceptance is outstanding.
