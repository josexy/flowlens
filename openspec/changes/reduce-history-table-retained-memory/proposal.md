# Proposal

## Why

The reviewed `d932ef5` implementation greatly reduces update allocations, but a 100,000-row history still retains about 11.48 MiB of unsorted bookkeeping or 22.25 MiB when numerically sorted, before records and rendered UI. The [memory review](../../../docs/technical/history-table-memory-review.md) identifies duplicated table identities, live-oriented history caches, and sidebar work that can be removed without limiting searchable history or changing stored traffic.

## What Changes

- Give history filtering and sorting an explicit snapshot processing mode inside the existing store/logic owners: borrow unchanged input views, allocate only needed result buffers, and release temporary sort values after producing the result. Keep the live-update cache policy for capture.
- Replace the full-list string/index adapter inside the shared `capture.Table` with numeric entry identities and borrowed rows through `nui.Table`, subject to the same measured compatibility gate. Extend NuxtUI-Go with typed keys and borrowing, retain its shared rendering, theme, and native interaction path, and remove the FlowLens-owned native-table adapter. Do not introduce another production table implementation.
- Eliminate repeated ID formatting in selection/background paths and the shallow history-load clone by making result-slice ownership explicit, including in backend test doubles.
- Virtualize the saved-session sidebar; cache category facets, filtered roots, and prepared list items by their actual inputs. Release hidden category-derived data through a sidebar lifecycle notification while preserving user selections, search, and expansion intent.
- Add matched retained-storage, scrolling, sidebar, and lifecycle acceptance evidence. Report native history-switch/idle measurements separately from Go allocation and collected-heap measurements.

### Scope and non-goals

The implementation scope is FlowLens's existing desktop store, logic, capture, and shell owners. Both live and history tables use the shared table integration; history-specific processing is explicitly selected and never enabled merely because a benchmark has no live controls.

This change does not reimplement the already completed 256-byte previews or live patch buffer reuse. It does not cap history rows, truncate stored values, change the live 5,000-row limit, change history loading/failure fallback, add background sorting, force GC, or promise a fixed process-memory peak. The user-requested follow-up authorizes changes to NuxtUI-Go and the resulting dependency integration, but does not change MyGo or remove the required per-cell decoration container: native cell-style support and shared text-cache idle eviction are documented follow-up designs, outside this implementation.

### Completion criteria

- Existing search, full-value sorting, native selection, row actions, column persistence, loading/error behavior, and release contracts pass the acceptance matrix in `design.md`.
- With the same 100,000-record Windows headless fixtures, zero selected rows, and no rendered table, retained history working storage meets the proposed gates: All <= 2.5 MiB, Filtered <= 4.5 MiB, numeric Sorted <= 3.5 MiB, and Filtered+Sorted <= 5.5 MiB. These are proposed implementation gates, not measured results or cross-platform promises.
- Stable live updates and long-path scrolling retain the allocation improvements from `d932ef5`; the migration does not claim the theoretical 11-cell-wrapper node saving.
- Saved-session row builds follow the viewport, repeated unchanged category frames reuse derived data, and hide/reset/replace paths release the intended references without clearing user choices.
- Scope-appropriate tests, UI vet, light/dark and English/Chinese interaction checks, measured comparisons, and strict OpenSpec validation are recorded. Unavailable native platform checks remain explicitly open.

## Capabilities

### New Capabilities

None; the work extends existing traffic capabilities.

### Modified Capabilities

- `traffic-capture`: distinguish viewport-bounded visual construction from data refresh cost and cover shared table behavior with large historical sources while preserving live interaction contracts.
- `traffic-history`: specify economical history working storage, virtual saved-session browsing, category cache lifecycle, and unchanged complete-data actions and switching failure behavior.

## Impact

- **Owners:** `internal/desktop/store` owns snapshot/filter publication and history slice ownership; `internal/desktop/logic` owns sorting; `internal/desktop/capture` owns table binding, selection, categories, and saved-session rows; `internal/desktop/shell` owns sidebar visibility notifications, wired by `internal/app`. The existing history service's fresh-slice behavior is documented, not replaced with another reader.
- **UI:** main-window capture/history table and sidebars only. Preserve focus and command dispatch, Ctrl/Command selection differences, DPI/text scaling, existing Nuxt colors, layouts, and both locales. Settings, update, and startup windows are unaffected.
- **Platforms/dependencies:** use the pinned MyGo API and the revised Nuxt table API and `CGO_ENABLED=0` on Windows, macOS, and Linux. Cross-compilation does not count as native runtime acceptance. Nuxt table changes live in the separate NuxtUI-Go source repository; never modify the module cache. Local integration uses an ignored Go workspace until an authorized library publication supplies a resolvable module version.
- **Persistence/protocols:** no settings schema, SQLite migration, HBIN codec, ordered header, timestamp, unknown metric, import/export format, or managed-file change. Export/action snapshots retain their existing ownership requirements. Network forwarding, Python Workers, process attribution/icon storage, CA trust, updates, and shutdown sequencing are unchanged.
- **Documentation:** update `docs/technical/memory-diagnostics.md`, the reviewed report, and task-oriented history/category memory guidance in `docs/user-guide.md` with measured behavior. No interface copy is expected; any necessary new copy must update both catalogs and generated accessors.
