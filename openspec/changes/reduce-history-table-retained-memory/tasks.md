# Tasks

All items below are implementation work and remain unchecked during proposal creation. Use the policies, ownership rules, adoption gate, and workload budgets in [design.md](design.md). Run commands from the FlowLens root; ordinary checks use `CGO_ENABLED=0` (PowerShell: `$env:CGO_ENABLED = "0"`). Record revision, Go/dependency versions, platform, fixture, and command with each measurement. Do not treat a proposed target as an achieved result.

## 1. Establish matched acceptance evidence

- [x] 1.1 Record `d932ef5` baseline storage, stress, live-update, and long-path measurements in an isolated checkout using `go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll)$' -benchmem -benchtime=20x -count=3`; verify reproducibility and distinguish storage, UI retention, and B/op.
- [x] 1.2 Extend `BenchmarkTrafficTableStorage` with explicit snapshot/live policy, Filtered+Sorted, and selection-clear/shrink variants; add `BenchmarkTrafficTableViewport`, `BenchmarkHistorySidebar`, and `BenchmarkCategorySidebar` fixtures for design Decision 9. Verify zero selected rows and preallocated source records in storage samples, stable source counts, viewport-bound row counts, and current-policy baseline runs without changing production behavior.
- [x] 1.3 Document the exact fixture/baseline procedure and exclusions in `docs/technical/memory-diagnostics.md`; verify the documented benchmark commands run and initial evidence contains no inferred native-process savings.

## 2. Make history input ownership and source policy explicit

- [x] 2.1 Specify exclusive result-array ownership for successful `HistoryBackend.ListHistoryEntries` calls, update store/capture/controlled-load test doubles to clone fixture arrays, compact nils in place in the successful load path, and clear removed tail pointers. Verify empty/nil/partially nil results, repeated backend reads, and unmodified fixture arrays with `go test ./internal/desktop/store ./internal/desktop/capture ./internal/services/history_service -count=1`.
- [x] 2.2 Validate decoded history row identity uniqueness once before publishing a replacement, using temporary numeric storage and the existing error path, without imposing new numeric-ID validity rules. Verify duplicate-ID failure preserves the previous history and does not write its files; cover canceled, failed, and rapid A/B/A loads and shutdown with the same targeted package command.
- [x] 2.3 Add explicit history-versus-live processing configuration in the existing filter/sorter/table construction paths and benchmark fixtures. Verify history selects snapshot behavior while mutable `Patch1` and actual live tests retain live behavior; document this ownership/policy contract in `docs/technical/memory-diagnostics.md` and run `go test ./internal/desktop/store ./internal/desktop/logic ./internal/desktop/capture -count=1`.

## 3. Reduce history filter storage

- [x] 3.1 Implement snapshot filtering inside `FilterStore` with borrowed source/base views, separately owned result buffers, no per-entry history predicate map, and result-sized growth/shrink. Add ownership tests proving `Release`, All-mode transitions, and shrink never clear or reorder source entries; verify all four predicate combinations, empty/one-row cases, and 100k-to-two-row shrink with `go test ./internal/desktop/store -count=1`.
- [x] 3.2 Publish independent base and final-result revisions, including equal-length replacements, reused IDs, source changes hidden by category selection, and aliased All results. Verify table/categorical consumers observe new data, live patches keep their existing behavior, and old pointers/tails are released with `go test ./internal/desktop/store ./internal/desktop/capture -count=1`.
- [x] 3.3 Update memory documentation with the borrowed-view validity boundary and measured filter storage; verify with `go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableStorage$' -benchmem -benchtime=20x -count=3`, reporting this stage separately from the still-existing table adapter cost.

## 4. Release history sort intermediates

- [x] 4.1 Add snapshot sorting to the existing sorter using dense temporary key records and a retained ordered-pointer result, with recomputation gated by snapshot/result revision and sort state. Verify ascending/descending/unsorted transitions, unknown values last, ID tie ordering, full mixed-case text, and source immutability with `go test ./internal/desktop/logic ./internal/desktop/capture -count=1`.
- [x] 4.2 Add retention/invalidation tests for same-ID replacement, equal-size membership changes, selection-preserving filter/delete/sort changes, and release of long folded keys after a completed sort, reset, or empty result. Run `go test ./internal/desktop/logic ./internal/desktop/store ./internal/desktop/capture -count=1` and the storage benchmark; document retained savings and remaining transient long-text sort costs without claiming a peak bound.

## 5. Move borrowed numeric table binding into NuxtUI-Go

- [x] 5.1 Extend NuxtUI-Go's existing Table renderer/state with typed string/integer IDs and a borrowed-row API, preserving the copied string-keyed API. Verify source ownership, local/manual sorting, numeric zero/high IDs, same-ID replacement, copied-mode validation, stable updates and selection capacity release with focused library tests.
- [x] 5.2 Return FlowLens traffic tables to `nui.Table` through `widgets.Table`; remove local native rendering/theme/header-divider, ID snapshot and selection-compaction adapters. Verify high IDs, selection/filter/sort/delete, history replacement, interrupted drag, outside release, focus loss and native keyboard gestures with `go test ./internal/desktop/capture ./internal/desktop/widgets ./internal/desktop/shortcuts -count=1` and UI vet.
- [x] 5.3 Preserve all table themes/fonts, row heights, columns/fill/empty states, markers, process cells, menus, detail focus, layout persistence and HAR drop. Re-render the complete light/dark, en/zh-Hans, normal/enlarged-text matrix and compare it with the previous candidate and original baseline.
- [x] 5.4 Repeat three-sample storage/stress/live/scroll/viewport/sidebar gates and release profiling after Nuxt reintegration. Retain all Decision 9 budgets, with no retained borrowed-row copies, all-row membership maps or full-history display-label cache. Update memory guidance and identify the local library revision/workspace used.
- [x] 5.5 Update NuxtUI-Go component docs, bilingual showcase/copyable examples and generated references for borrowed rows and typed callbacks. Run library lint, verify, website check/build and skill-reference checks; record unavailable native/browser interaction without substituting headless results.
- [x] 5.6 After explicit authorization for library commit/publication, publish the verified Nuxt change, pin a resolvable module version in FlowLens, and run the integration checks with `GOWORK=off`. Keep this task open while integration depends on an ignored local workspace; do not fabricate a version or commit a personal replace path.

## 6. Cache and release category-derived data

- [x] 6.1 Cache facets, filtered roots, and prepared listbox data using base/final revisions, source, selections, sidebar query, and locale as specified. Preserve live throttling and existing virtual list/tree controls. Verify unchanged-frame recomputation counters, changes outside the final subset, source/locale changes, hidden selections, and outer-scroll keyboard behavior with `go test ./internal/desktop/capture ./internal/desktop/store -count=1` and `BenchmarkCategorySidebar`.
- [x] 6.2 Add the sidebar-hidden notification to `shell.Surface` and wire capture-owned cleanup in `internal/app`; separate category derived release from source reset. Verify transitions to every workbench section and sidebar collapse, one notification per transition, preserved same-source choices, cleared source-specific intent on replacement, closed menus, and no stale callbacks with `go test ./internal/desktop/shell ./internal/desktop/capture ./internal/app -count=1` plus `task vet:ui`.
- [x] 6.3 Add weak-reference/counter regressions for hide/reopen and hidden source close/replacement, and update category memory guidance in `docs/user-guide.md` and `docs/technical/memory-diagnostics.md`. Verify a 100k-row source releases its category-derived tree while hidden, rebuilds once on reopen, and retains valid filters/expansion/scroll intent using `go test ./internal/desktop/capture -count=1` and the sidebar benchmark command below; inspect both themes and locales.

## 7. Virtualize the saved-session sidebar

- [x] 7.1 Replace the all-session Scroll loop with a keyed native list using existing metadata and row presentation. Verify viewport-bound build counts for 100/1000 sessions, empty/loading states, last-row visibility after resize/text scaling, keyboard activation, and stable session identity on refresh/reorder/delete with `go test ./internal/desktop/capture ./internal/desktop/shell -count=1` and `BenchmarkHistorySidebar`.
- [x] 7.2 Close row menus whose anchors leave view, prune obsolete menu state, and preserve existing import/refresh/delete/export actions. Verify pointer and keyboard menu targeting, no accidental session load after index changes, and hidden menu cleanup with `go test ./internal/desktop/capture ./internal/services/history_service -count=1`; inspect en/zh-Hans light/dark session rows and update history memory guidance and measured sidebar evidence.

## 8. Complete integration acceptance

- [x] 8.1 Run `task verify` and the expanded render command below after all changes. Verify render output and accessible roles for the complete main-window matrix; record that settings/update/startup windows and unrelated owners were not altered. If new UI copy was necessary, update both catalogs, run `task i18n:generate`, and verify `task i18n:check` before this gate.
- [x] 8.2 Run the final three-sample storage/allocation/viewport/sidebar comparisons and full-allocation profiling commands below. Verify every design Decision 9 gate, absence of old history sort-key retention, and no post-collection growth across repeated load/filter/sort/hide/close cycles. Record original record bytes, selected-ID state, UI heap, and native memory as separate categories.
- [ ] 8.3 On Windows, run the native app with actual HBIN histories and record load/scroll/sort/switch/close behavior, Process/Held/object measurements, natural idle samples at 0/30/120/300 seconds, and separate forced-collection recovery. Verify loading failure fallback, native keyboard/selection/menu interactions, 100%/150% scale, and no stale history or category state; record data and environment rather than asserting an unmeasured fixed peak ratio.
- [ ] 8.4 Run native interaction acceptance on macOS and Linux with both themes/locales and platform selection/shortcut behavior, recording OS/architecture and text scaling. Run the targeted Go tests on each host and distinguish these results from cross-compilation. If a required platform is unavailable, leave this item unchecked and document the limitation; do not mark native acceptance complete based on Windows headless results.
- [x] 8.5 Compare final implementation, both capability deltas, documentation, and measured evidence against each other, then run `openspec validate reduce-history-table-retained-memory --strict --no-interactive` and `git diff --check`. Verify all in-scope tasks have their stated evidence, all byte claims identify their workload, and remaining framework work is explicitly deferred without being presented as implemented.

### Integration commands

Run the following after the referenced benchmark fixtures exist. Use the same commands and data for baseline/candidate comparisons where applicable; port only measurement harnesses needed for a baseline run and record their revision.

```powershell
$env:CGO_ENABLED = "0"

# Relevant full package suites; task verify also runs the repository-wide suite.
go test ./internal/desktop/store ./internal/desktop/logic ./internal/desktop/capture ./internal/desktop/widgets ./internal/desktop/shell ./internal/desktop/shortcuts ./internal/services/history_service ./internal/services/setting_service ./internal/app -count=1
task verify

# Storage and update/scroll throughput use normal allocation sampling.
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTraffic(TableStorage|TableStress|TableLive|TableLongPathScroll|TableViewport)$' -benchmem -benchtime=20x -count=3
go test ./internal/desktop/capture -run '^$' -bench '^Benchmark(HistorySidebar|CategorySidebar)$' -benchmem -benchtime=20x -count=3

# Visual QA: extend the existing fixtures for the new states/locales.
$env:FLOWLENS_RENDER_DIR = Join-Path $env:TEMP "flowlens-history-memory-renders"
go test ./internal/desktop/... -run TestRender -count=1
go test ./internal/desktop/capture ./internal/desktop/shell -run 'Test(Category|HistorySidebar|Render)' -count=1
task vet:ui
Remove-Item Env:FLOWLENS_RENDER_DIR

# Whole-process attribution, distinct from benchmark B/op and UI heap deltas.
$env:FLOWLENS_TABLE_HEAP_DIR = Join-Path $env:TEMP "flowlens-history-memory-profiles"
go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTableStress$/^Rows50000$/^Idle$' -benchtime=1x -memprofilerate=1 -count=1
go tool pprof -sample_index=inuse_space -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
go tool pprof -sample_index=inuse_objects -top (Join-Path $env:FLOWLENS_TABLE_HEAP_DIR "BenchmarkTrafficTableStress-Rows50000-Idle.pprof")
Remove-Item Env:FLOWLENS_TABLE_HEAP_DIR

openspec validate reduce-history-table-retained-memory --strict --no-interactive
git diff --check
```

On each native test host, `CGO_ENABLED=0 go test ./internal/desktop/capture ./internal/desktop/shell ./internal/desktop/store ./internal/desktop/logic -count=1` is the Unix shell equivalent of the relevant targeted check. Use `task run` to build/run the native app and the established diagnostic-server procedure for Process/Held/object and idle measurements. Tests and visual checks, not a successful build alone, establish runtime behavior.

No Python Worker/SDK, network/codec, or website implementation changes are planned. Their dedicated integration/build workflows are not added to this change; `task verify` retains the existing full application checks.

## Acceptance evidence

Measurements below ran on Windows amd64 with Go 1.27.3 during the Nuxt reintegration, before the v0.2.0 pin and the review fixes; they were not repeated afterwards. Headless results do not establish native process-memory recovery.

- **5.1–5.3:** NuxtUI-Go's typed, copied, and borrowed table tests pass, as do FlowLens's capture/widgets/shortcuts suites and UI vet. The local native-table adapter is removed. All 32 table renders match the original baseline and 16 sidebar renders match the preceding candidate.
- **5.4 / 8.2:** All 65 table and 4 sidebar workloads have three samples. Snapshot storage is 0.77/1.68/1.53/2.45 MiB for All/Filtered/Sorted/Filtered+Sorted, within the Decision 9 gates; stable 100k `Patch1` is 21,296 B/op and 484 allocs/op. Every Decision 9 gate passes. Lifecycle tests and a full-sampling profile separate record bytes, selected IDs, working storage, and UI heap.
- **5.5:** NuxtUI-Go `task lint:go`, `task verify`, `task website:check`, `task website:build`, and the generated-skill check pass. Browser and native interaction were not run.
- **5.6:** NuxtUI-Go [PR #18](https://github.com/josexy/nuxt-ui-go/pull/18) is merged and released as v0.2.0 (`d7d6a6c`), which `go.mod` pins without a `replace` directive. `go mod verify`, `go mod tidy -diff`, and `GOWORK=off CGO_ENABLED=0 task verify` pass.
- **8.1:** `task verify` and the complete render suite pass. Settings, update, and startup windows, interface catalogs, and unrelated service owners are unchanged.
- **8.5:** Implementation, both deltas, docs, and evidence agree. Cell-wrapper removal and shared text-cache trimming remain deferred. `openspec validate reduce-history-table-retained-memory --strict --no-interactive` and `git diff --check` pass.
- **Review fixes:** Category expansion is pruned only when the sidebar reopens, so a temporary table filter no longer discards it, and a snapshot category change reuses the current search result instead of rescanning the source. Both have regression tests, and `task verify` passes on Linux arm64 with Go 1.27.4.
- **8.3 — open:** Windows native acceptance with real HBIN histories, failure fallback, 100%/150% scaling, natural idle samples, and forced-collection recovery has not run.
- **8.4 — open:** macOS and Linux native acceptance has not run. Cross-builds, headless tests, and image comparisons do not substitute for it.

Do not archive this change until 8.3 and 8.4 pass.

## Workflow follow-up

- After implementation and acceptance, synchronize the intended deltas and archive using the corresponding OpenSpec workflow; do not synchronize or archive during proposal creation.
- After synchronization, run `openspec validate --specs --strict --no-interactive` and record the archive path and any unresolved native-platform limits.
- Track the native body-cell decoration hook and shared text-cache idle-trim design as separate dependency work when that scope is requested; they are not unchecked implementation tasks hidden from this change's completion criteria.
