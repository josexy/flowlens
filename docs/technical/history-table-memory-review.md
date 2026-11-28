# History Traffic Table Memory Review

This record summarizes the measurements behind `reduce-history-table-retained-memory`. Current ownership and profiling instructions live in [Memory Diagnostics](memory-diagnostics.md). Native acceptance remains open; the measurements below are historical headless results.

## Baseline and Measurement Scope

- **Baseline:** `d932ef5`, compared with the earlier `dd1853a` implementation.
- **Environment:** Windows amd64, Go 1.27.3, Intel i9-14900HX, 32 logical processors, `CGO_ENABLED=0`.
- **Baseline dependencies:** MyGo v0.10.0 and Nuxt UI Go `v0.1.2-0.20261119040559-42aa9b196036`.
- **Method:** Matched synthetic fixtures, three samples of 20 iterations for acceptance comparisons, two-GC retained-heap samples, and separate profiles with `-memprofilerate=1`.

The fixtures model history-sized tables without opening HBIN files. Storage excludes source records, selection, and rendered UI. UI deltas are sampled after bookkeeping exists. `B/op` measures cumulative allocations, not retained or peak memory. Whole-process profiles overlap these quantities; do not add them together or infer native process-memory savings.

At `d932ef5`, bounded labels, unfiltered match-map removal, and stable-ID update reuse were already implemented. The remaining costs were duplicated table identity storage, live-oriented history filters/sorters, and sidebar work. The review did not establish an unbounded leak or a fixed 2× history-switch peak.

| 100k-row workload | Baseline retained bookkeeping |
| --- | ---: |
| All, unsorted | 12,037,440 B (11.48 MiB) |
| Filtered, search `h` | 14,402,032 B (13.73 MiB) |
| Numeric status sort | 23,331,312 B (22.25 MiB) |
| Filtered and sorted | 25,695,896 B (24.51 MiB) |

At 1160 × 714 DIP, the table built 27 rows and 297 cells, adding about 6.08 MiB of UI heap. Stable-ID Patch1 used 22,448 B/op and 547 allocations at both 50k and 100k rows. At `dd1853a`, the same update used 2,259,840 B/op at 50k and 4,511,649 B/op at 100k; retained unsorted storage had barely changed.

## Implementation Acceptance Evidence

### Final Headless Acceptance (2026-11-27)

The intermediate implementation used a local native-table adapter, snapshot filters/sorters, a virtual history sidebar, and category cache invalidation. It passed `task verify`, render checks, lifecycle tests, and the matched storage/allocation gates.

Its four 100k-row storage medians were 806,408 / 1,764,872 / 1,609,224 / 2,567,688 B in the table order above. Mutable Patch1 used 15,864 B/op and 456 allocations. All 32 table renders matched the original baseline; the 16 sidebar renders covered both languages, both themes, and 16/24-pixel text.

This adapter was replaced by the shared Nuxt implementation below. These results describe that earlier stage, not the current renderer. Detailed intermediate tables and local diagnostic notes remain in Git history at `f67c07f`.

### Nuxt Borrowed-Row Reintegration (2026-11-27)

The measured Nuxt implementation was published as `ff3b88e73c27f4abdd2cea8b521732ac5f76e54c`, initially consumed as `v0.1.2-0.20261127051345-ff3b88e73c27`. FlowLens subsequently committed the integration at `f67c07f` with Nuxt UI Go v0.2.0. The numbers in this section belong to the November 27 implementation stage; they are not new measurements of every later dependency change.

FlowLens returned to `widgets.Table` / `nui.Table` with borrowed numeric rows. Nuxt owns the ordered-ID snapshot, selection compaction, layout, and styling. FlowLens retains explicit live/snapshot policies, validates history IDs before publication, and supplies traffic-specific cell decorations. The local renderer and duplicate identity/selection storage were removed. Cell wrappers remain, so their per-visible-row cost is unchanged.

| 100k snapshot workload | Baseline B | Borrowed Nuxt B | Acceptance ceiling |
| --- | ---: | ---: | ---: |
| All | 12,037,440 | 806,376 | 2.5 MiB |
| Filtered | 14,402,032 | 1,764,840 | 4.5 MiB |
| Numeric sorted | 23,331,312 | 1,609,192 | 3.5 MiB |
| Filtered and sorted | 25,695,896 | 2,567,656 | 5.5 MiB |

Select All added 2,364,592 B separately. Clearing returned storage to 806,376 B; shrinking from 100k to two rows and clearing retained 3,576 B. Original source records remained alive throughout these samples.

| Workload | Recorded result |
| --- | --- |
| Mutable 100k Patch1 | 21,296 B/op, 484 allocations; below the 32 KiB / 700 gate |
| 12 actual-live modes | All passed baseline +10% limits for bytes and allocation counts |
| 64-KiB path scrolling | 5,331,204 B/op; within baseline +10% |
| Viewport heights 714 / 1414 / 2114 DIP | 27 / 52 / 77 built rows, independent of 5k–100k source size |
| 100k viewport UI heap | 6,370,664 / 11,482,744 / 16,412,176 B; all within baseline +10% |
| History sidebar, 100 / 1000 sessions | 19 rows and 191 nodes/states in both; 1,888,488 / 1,888,504 B of UI heap |
| Unchanged 100k categories | 69,441,928 B of derived data plus UI; zero facet/root/item rebuilds; 21,568 B/op |
| Changing category search | About 6.03 MB/op; prepared items rebuild, full facets/roots do not |

The original 1000-session sidebar retained 42,192,968 B and built all 1000 rows. Category data still scales with the source while visible. Timing varied between matched runs, so these results do not establish a portable speedup.

Three lifecycle samples each ran a warm-up followed by four load/filter/sort/select/clear/shrink/hide/close cycles. Collected end-minus-warm heap deltas were 15,352 / -43,640 / -40,392 B, within the 2-MiB regression bound. The tests asserted release of table, selection, and category state. Dedicated sorter tests checked that snapshot sort keys and folded text became reclaimable.

The full-sampling 50k Idle profile retained 24,749.61 KiB / 211,994 objects across the entire test process, including fixture records and UI. Its numeric ID slice accounted for about 392 KiB; the borrowed path retained no copied records or validation map. This unsorted live-policy profile does not establish snapshot-sort peaks or native recovery.

Recorded verification:

- FlowLens `task verify`, targeted capture/widgets/shortcuts tests, and the desktop render suite passed. All 32 table images matched the original baseline; 16 sidebar images matched the preceding candidate.
- All 65 table and four sidebar workloads had three samples. Storage, allocation, viewport, and release-cycle gates passed.
- Nuxt tests covered copied/borrowed transitions, numeric identities, selection pruning, sorting, callbacks, and stable updates. Library verification and website checks/build passed; remote icon timeouts left browser/network rendering unverified.
- Published-module verification used `GOWORK=off`, with matching implementation hashes, valid checksums, and no local replace. OpenSpec strict validation and `git diff --check` passed for that stage.

### Selected-Row Update Follow-Up (2026-11-28)

Nuxt v0.2.0 also includes the collection optimizations measured between dependency commits `a07e537f3bed` and `41c7fbd746e0`. On the same Windows toolchain, three 20-iteration samples of `BenchmarkTrafficTableLive/Rows5000/AllSelectedPatch1` reduced median allocation from 200,280 to 56,565 B/op (1,023 to 1,006 allocations). Both built 297 cells/op.

The 10,000-row allocation regression failed against the older dependency at 295,584 bytes per patch and passed against the newer implementation with a 16 KiB ceiling. These are headless allocation results, not native frame-time or process-memory claims.

## Reproduction

Use the commands and fixture definitions in [Memory Diagnostics](memory-diagnostics.md#matched-history-table-acceptance-fixtures). The original baseline is `d932ef5`; expanded storage, viewport, and sidebar comparisons used a measurement-only harness on that revision. Keep the same machine, toolchain, fixture, and dependency stage when comparing results.

Regression checks include:

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/desktop/capture ./internal/desktop/store ./internal/desktop/logic -count=1
go test ./internal/desktop/capture -run '^TestHistoryMemoryCyclesSettleAfterCollection$' -count=3
task verify
```

## Outstanding Native Acceptance

OpenSpec tasks **8.3 and 8.4 remain unchecked** in [the active change](../../openspec/changes/reduce-history-table-retained-memory/tasks.md):

- **Windows:** Real-HBIN interaction, failed-load fallback, 100%/150% scale, and natural idle samples at 0/30/120/300 seconds, plus a separate forced-collection run.
- **macOS and Linux:** Targeted tests on native hosts and the native interaction matrices.

Headless tests, image comparisons, and cross-builds do not satisfy these tasks. The change remains unarchived. Native cell-decoration hooks and shared text-cache idle trimming are separate dependency work and are not counted as savings here.
