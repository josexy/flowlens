# Tasks

## 1. Bounded previews

- [x] 1.1 Implement bounded UTF-8 cell previews and document full-value actions; verify Unicode/limit/action tests and long-path scrolling benchmarks with `CGO_ENABLED=0 go test ./internal/desktop/logic ./internal/desktop/capture`.

## 2. Reusable row processing

- [x] 2.1 Reuse filtering, sorting, and native identity inputs; test same-ID patches, membership/order changes, selection, category switches, and cache release with `CGO_ENABLED=0 go test ./internal/desktop/store ./internal/desktop/logic ./internal/desktop/capture`; document ownership and allocation behavior.

## 3. Integration acceptance

- [x] 3.1 Run `task verify`, light/dark `TestRender` checks, matched table benchmarks, and `openspec validate optimize-traffic-table-memory --strict --no-interactive`; compare implementation/specs and record Windows evidence and native platform limitations.

## Initial acceptance evidence (512-byte preview)

2026-11-27, Windows amd64, Intel i9-14900HX, MyGo v0.10.0, CGO_ENABLED=0. Baseline: dd1853a. All checks passed:

- `task verify`: formatting, golangci-lint (0 issues), MyGo UI vet, both catalogs, and `go test ./...`.
- `go test ./internal/desktop/logic ./internal/desktop/store -count=1` and capture allocation/preview/release regressions; full capture tests also passed in verify.
- `FLOWLENS_RENDER_DIR=<temp>/renders go test ./internal/desktop/capture -run 'Test(TableStablePatches|LongTrafficPaths|RenderTrafficCell|ReplacingAndClearingHistory)' -v -count=1`: light/dark capture/history previews and full Copy Path; rendered images inspected.
- `go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableLive$' -benchmem -benchtime=300ms -count=1` and long-path scrolling with `-bench '^BenchmarkTrafficTableLongPathScroll$' -benchtime=20x`.
- `openspec validate optimize-traffic-table-memory --strict --no-interactive`.
- Delta synchronized to `traffic-capture`; `openspec validate --specs --strict --no-interactive`: 18 passed, 0 failed. The existing badge scenario and unrelated requirements were preserved.

| Workload | Before B/op | After B/op |
| --- | ---: | ---: |
| 5,000 rows, one patch | 301,044 | 52,197 |
| 5,000 rows, selected, one patch | 538,905 | 52,621 |
| 5,000 rows, sorted, 100 patches | 1,306,066 | 81,274 |
| Scroll 27 rows with 4-KiB paths | 84,253,977 | 6,624,544 |
| Scroll 27 rows with 16-KiB paths | 321,427,071 | 6,624,053 |
| Scroll 27 rows with 64-KiB paths | 1,042,727,462 | 6,623,288 |

Long-path scrolling uses 1,000 records, a 1160 x 714 headless table, and 20 page changes after warmup. 64-KiB scrolling dropped from about 1.45 seconds to 9 ms per page in these samples. These are cumulative allocations per operation, not retained process memory.

`BenchmarkTrafficTableStorage` compares collected retained table/filter/sort storage with records already allocated, using the same benchmark against baseline sources through a temporary Go overlay. At 100,000 rows: All 11,993,696 -> 12,047,632 bytes (essentially unchanged); Filtered 11,993,696 -> 14,402,032 bytes (+2.30 MiB); Sorted 25,389,920 -> 23,331,312 bytes (-1.96 MiB). The extra filtered storage is the explicit cost of reusable pointer/index snapshots; it is bounded and released on shrink/reset. At 5,000 rows the filtered increase is about 120 KiB. This trade-off is accepted in exchange for avoiding whole-list allocations on every live update.

No service models, persisted records, full-value actions, localization keys, or dependency APIs changed. Original-value filtering/sorting/copy, Unicode boundaries, bounded allocations, same-ID replacement, removed cache entries, slice tails, shrink/reset, and native selection behavior are covered. Native process RSS/GPU profiling and macOS/Linux runtime checks were not performed; headless Windows results do not claim those outcomes.

## 256-byte follow-up (2026-11-27)

Tightened the preview limit from 512 to 256 UTF-8 bytes, including the ellipsis, at the user's request. Updated the exact-limit and multibyte-boundary cases, used the shared limit for the preview-sized allocation fixture, and synchronized code, documentation, design, and delta/main specs. The initial measurements above remain the 512-byte results.

Passed targeted logic and capture tests for preview boundaries, original-value operations, bounded scrolling allocation, light/dark capture/history rendering, and complete Copy Path. `task vet:ui` passed on a sequential rerun after the first concurrent invocation encountered a transient missing generated `.syso` file. Main specs: 18 passed, 0 failed; `git diff --check` passed.

The same 20-page scrolling benchmark now allocates 5,336,917 / 5,336,554 / 5,336,022 bytes per page for 4 / 16 / 64 KiB paths. For 64-KiB paths, that is 5.09 MiB versus the 512-byte preview's 6.32 MiB (about 19% less); about 7.3 ms per page in this sample. These are Windows headless Go allocations, not native process RSS.

## Workflow follow-up

- Synchronize the delta, validate main specs with `openspec validate --specs --strict --no-interactive`, and archive the completed change.
