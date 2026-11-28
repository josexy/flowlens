# Design

## Context

See [proposal.md](proposal.md) for the motivation and `d932ef5` baseline measurements. This design changes application-owned working storage, not the stored traffic model.

The pinned Nuxt table always copies rows into string-keyed records and retains validation storage. It has no borrowed-row/numeric-key path. The pinned MyGo table already owns native selection, column layout, virtualization, and accessibility, but its body-cell callback does not expose the cell element for decoration. These are separate constraints: removing the full-list adapter is possible without pretending that its per-cell decoration container can also disappear.

History summaries are read into a fresh slice by the production service. `HistoryTraffic.Load` and `Delete` publish replacement snapshots; highlight/detail state is separate. In contrast, live capture patches its entry slice in place. `FilterStore.Version` currently tracks the final filtered result, so it alone cannot invalidate category facets when the base data changes outside that result.

Existing specifications preserve complete search/sort/action values, all history rows, native table interaction, and workbench selection across sidebar switches. The current `Rendering at capacity` scenario loosely states that frame build time is independent of row count; source refresh and sorting are in fact O(N) or O(N log N). The delta limits that guarantee to visual row construction, and adds history coverage without claiming constant-time data processing. Releasing category-derived data on hide must preserve navigation choices to satisfy `desktop-shell`.

## Goals / Non-Goals

**Goals:**

- Remove persistent duplicated table identity records and history-only live-update caches; keep one authoritative record snapshot and compact current result/order views.
- Give every borrowed slice a clear validity boundary, and every retained result a source generation. Release cannot clear someone else's backing array.
- Preserve the one shared capture/history table, native interactions, complete record semantics, and the previous history as failure fallback during loading.
- Bound saved-session visual construction by the viewport and category recomputation by changed inputs.
- Separate retained bytes, allocation throughput, visual node counts, and native process recovery in acceptance evidence.

**Non-Goals:**

- No service-model, codec, persistent settings, header-order, metric-unit, body-availability, or file-format changes.
- No generic table framework, second production renderer, copied MyGo internals, `unsafe` renderer access, or dependency fork/update.
- No forced GC/scavenging, automatic global text-cache flush, paging/capping of history, asynchronous filtering/sorting, or removal of the existing loading/detail transition policy.
- No per-cell node-reduction target in this change. That requires an upstream API described under Decision 8.

## Decisions

### 1. Keep processing policy explicit and domain-owned

History construction selects snapshot behavior for the existing filter and sorter; live construction retains incremental behavior. Express this through small explicit constructor/options in the existing owners, not through `Live() == nil`, interface type assertions, or a second filtering implementation in the view.

| Owner | Responsibility |
| --- | --- |
| `store.HistoryStore` / `HistoryTraffic` | Published summary slice, source version, cancellation, successful-load ownership transfer, reset/delete |
| `store.FilterStore` | Live versus snapshot policy, borrowed/owned results, base and final-result revisions |
| `logic.TrafficTableSorter` | Existing comparison rules, live cache, snapshot sort scratch and current result |
| `capture.Table` | Numeric selection and ordered identity snapshot, native table binding, decoration and business actions |
| `capture.categoryView` / `Workspace` | Derived category caches, durable navigation choices, session-list state |
| `shell.App` / `Surface` | Notify a sidebar owner when its visible lifetime ends |
| `internal/app` | Wire the capture-owned sidebar lifecycle callback during assembly |

The uncapped benchmarks must explicitly select the policy they intend to exercise. Preserve a mutable uncapped `Patch1` benchmark and the real live-capture benchmarks; do not relabel their allocation regression as an immutable-history test.

**Alternative rejected:** treating every uncapped/non-live-control source as immutable would silently alter benchmark and future-source contracts. Applying snapshot processing to live capture would also give up the recently implemented incremental allocation improvements.

### 2. Borrow history filter views, and keep owning buffers separate

Snapshot filtering scans only after source version, search/tab, or host/process inputs change. It retains no per-entry predicate map.

| Predicate state | Base view | Final filtered view |
| --- | --- | --- |
| No search/tab predicate and no category selection | Borrow source | Borrow base/source |
| Search/tab only | Own matching-pointer buffer | Borrow base |
| Categories only | Borrow source | Own matching-pointer buffer |
| Search/tab and categories | Own base matches | Own final matches |

Do not reserve source-length capacity for a highly selective result. Reuse owned buffers for repeated filtering, clear removed pointer slots, and discard capacity when empty or greater than `max(64, 4 * resultLength)`. If a result aliases the source or the other result, keep the owned scratch separately identified; release clears only owned storage, then drops borrowed references. Clearing an aliased source would corrupt the still-open history.

Publish two revisions after computing results: base-data revision and final-result revision. For snapshot aliases, a changed source version must invalidate consumers even when length and IDs remain equal; relying on a comparison of the same backing array is incorrect. Base revision also invalidates on search/tab changes and source updates that affect base consumers while the final selected subset is unchanged. Conservative invalidation is acceptable; stale categories are not. Returning to All releases unused history result buffers.

Keep the existing live predicate cache and stable-patch behavior. Add the base revision there as well so category caching has a correct input. Result slices remain read-only borrowed views valid until the next refresh; long-running actions must continue capturing independent snapshots.

**Alternatives rejected:** aliasing live input without version handling can conceal patches; retaining the existing two full-size buffers and map for every historical query preserves the main filtered-memory cost.

### 3. Retain history sort output, not its intermediate keys

Use the existing `trafficSortValue`/comparison semantics for both modes. When a snapshot needs sorting, build a temporary dense sequence containing each entry pointer and its computed sort value, sort that sequence, and write the ordered pointers into the sorter's reusable output buffer. Do not create a full membership/value map for snapshot sorting. Compute each key once per sort, then let scratch and case-folded strings become unreachable when the call returns; do not put them in a pool or persistent field.

The table's source/result revision and sort state gate recomputation. A source replacement, equal-length membership change, search/tab/category change, deletion, sort-column change, or direction change rebuilds the result. Unsorted returns the filter view without sorting it in place and releases the previous owned order buffer. Reset/empty/shrink clears pointers and releases excess capacity. Same-ID new records must replace the old pointers.

Preserve unknown duration/size last in both directions, case-folded full-text comparison followed by original text, and ascending ID tie breaking. Do not implement descending by blindly reversing a previous result: that reverses tie ordering and moves unknown values to the front. Numeric ID keys preserve the source's existing sort semantics; changing numeric comparison semantics is outside this change.

The 256-byte preview limit does not apply to sort values. Mixed-case long paths can create substantial **temporary** folded strings; this change reduces their retained lifetime rather than claiming a universal sort-peak bound. Measure that workload separately. There is no new worker or background state publication.

**Alternatives rejected:** keeping only a sorted slice but leaving the live value map reachable would not achieve the goal; recomputing lowercase strings inside every comparison would restore allocation pressure proportional to comparisons.

### 4. Bind borrowed rows and numeric identities through Nuxt

The user-requested follow-up moves the generic table optimization into NuxtUI-Go. Keep `TableState[T]` and `TableOptions` as backward-compatible string-keyed aliases; add `KeyedTableState[T, K]`, `KeyedTableOptions[K]`, and a string/integer `TableKey` constraint. Both modes use the same `nui.Table` renderer, styling, native input, callbacks and accessibility. Numeric zero and high uint64 IDs are valid.

`SetRows` continues copying values and validating IDs atomically. `SetBorrowedRows` is a distinct trusted-data contract: the caller guarantees unique stable IDs, a pure key function, and main-thread publication before rendering. It borrows values and retains one ordered-ID snapshot so in-place source-array reuse cannot hide identity changes. It has no retained all-row validation map or copied row records. It reports input identity/order changes, prunes removed selections using temporary selected-ID storage, and releases excess identity capacity after shrink/reset. Local sorting uses an optional index permutation without modifying caller data; manual sorting has no permutation. Nil binding releases borrowed values and identities. Copying and borrowing modes can replace one another.

History still validates decoded IDs once before publishing a successful load, with its existing failure fallback; live sources already own identity validation. Thus moving table validation out of borrowed binding does not weaken FlowLens's source identity contract. Filtering, sorting and complete-value actions retain their existing owners and semantics.

`capture.Table` uses `nui.KeyedTableState[*logic.TrafficEntry, uint64]`, calls `SetBorrowedRows` on result revisions, and passes it through the existing `widgets.Table` wrapper. Remove the local native state binding, ordered-ID array, selection compaction and duplicated native rendering/theme/header-divider implementation. Keep only traffic-specific cells, decorations, focus/detail business rules and menus. Nuxt owns generic selection pruning/capacity release while preserving MyGo's bound field address, cursor and gesture state. Native keyboard/range/drag behavior remains authoritative.

Nuxt's `BuildCell` callback supplies the existing styled content element as a build-only parameter and a `TableCell[T]` containing displayed index, value and selection. This gives traffic decorations the row context they need without an index array, a per-cell row search or another container. The metadata struct contains no `ui.Element` or Context; nothing build-scoped is stored across frames. Existing Cell/Value callbacks retain their behavior when BuildCell is absent.

#### Compatibility and adoption gate

The original direct-native candidate is a historical measurement stage, not the final architecture. Re-run the same render/interaction and Decision 9 storage/allocation/viewport gates after restoring the shared Nuxt path. Preserve one cell decoration container, all columns/fill/empty states, Nuxt-derived row height/colors/borders, header overlays, context menus, HAR drop and persisted layouts. Do not retain two production renderers or a display-label cache.

The library addition must preserve existing copied string-table behavior and typed callbacks. Add borrowed numeric/string ownership, identity, sorting, invalidation, selection-release and retained-memory regressions in NuxtUI-Go, update its component docs and bilingual showcase, and run its required headless checks. Native/browser interaction remains unverified under the user's headless-only instruction.

Use an ignored local Go workspace to compile both source repositories while developing. Do not fabricate a pseudo-version, edit the module cache, or commit a personal replace/workspace path. Library commits/publication require explicit authorization under its AGENTS.md; after publication, pin the resolvable dependency and verify FlowLens with `GOWORK=off` before completing the dependency-delivery task.

### 5. Transfer history-load slice ownership once

Document that a successful `HistoryBackend.ListHistoryEntries` returns a backing array owned exclusively by the caller; the backend retains no mutable access to that array. Records remain immutable summaries after publication. The production service already creates this array. Update store/capture fake backends to return a shallow clone of their fixture arrays, including controlled-load variants, before changing the consumer.

Compact nil entries in place after a successful read, clear removed tail pointers, and publish through the existing `Load` path. A nil/empty success opens an empty history; an error or cancellation never publishes partial output. Keep A usable while B loads; only a valid B result releases A's filter/table/detail references. Existing job cancellation and stale-result rejection remain authoritative, including close, rapid A/B/A switching, and shutdown. Do not remove clones from deletion, export, or other actions whose snapshot ownership differs.

**Alternative rejected:** removing the clone without updating the contract corrupts fake/shared backend slices and makes later reuse unsafe. The saving is one pointer array (~0.77 MiB at 100k), not a deep record-copy reduction or a fixed peak multiplier.

### 6. Cache category work by the input it actually uses

Retain the existing `categoryView`, `nui.Listbox`, `nui.Tree`, and `categoryViewport`. Separate cache keys and owners:

| Derived data | Invalidation inputs |
| --- | --- |
| Host/process facets | Source key, base revision, host/process selections, locale-dependent fallback label |
| Structure roots | Source key and final filtered revision |
| Search-filtered facets/root list and prepared listbox items | Respective derived revision and normalized sidebar search; locale where labels differ |
| Tree nodes/handles | Structure/search revision; expansion/selection continues through the existing tree state |

Store string/scalar key snapshots, not borrowed result slices as equality evidence. A row outside the selected category can change available facets while `Filtered()` stays identical; use the new base revision. Preserve the live 250-ms throttle for derived rebuilds, while source changes and user filter/search actions invalidate promptly. Do not delay publication of table filter results. Empty search reuses existing roots. Repeated unchanged frames construct only visible UI and do not rerun facets or rebuild full item arrays.

Add a narrow sidebar-hidden callback to the existing shell `Surface` contract. The shell tracks the previously visible sidebar and calls its callback once when the active section changes or the secondary sidebar collapses, before subsequent builds. Wire Categories to `Workspace` cleanup through existing app assembly; call from the main thread, with no subscriptions or goroutines. Tests that mount capture directly exercise the same cleanup method.

Split category derived-data release from source reset. Hide releases entry references, facets, roots, prepared items, tree nodes, handles, and menu state; it preserves search text, selected host/process keys in the store, expanded node keys, active-leaf intent, and scroll intent for the same source. User expansion keys may legitimately scale with user actions. Replace tree pointers rather than mutating an already-built tree. Reopening reconstructs once from current data, prunes no-longer-existing intent keys, restores valid selection/expansion, and clamps scroll. Source close/replacement clears source-specific intent as existing reset does. Hidden menus cannot retain or act on old source entries.

No work runs merely to keep a hidden sidebar cache fresh. This callback covers workbench navigation/collapse, not native hide-to-tray idle policy or a new shell-window lifecycle.

### 7. Virtualize saved-session rows without changing their actions

Replace the `ui.Scroll` loop in `historySidebar` with a keyed `ui.List` backed by existing `History.Metadata`. Keep a persistent list state in `Workspace`; use metadata key, not row index, as identity. Build the existing two-line label/time/icon/count presentation only for visible rows and bounded overscan. Use native list focus/activation and existing `openHistory`/menu business functions, without nested competing selection handlers or custom key dispatch.

Account for row spacing and both text lines when calculating height; test normal and enlarged text, 100%/150% scaling, and the final row after resize. On metadata refresh/delete/reorder, reconcile cursor and scroll by key, preserve the opened history if it remains present, and never open a different history merely because an index changed. Keyboard navigation can reach and activate offscreen sessions. Keep loading/empty states, refresh/import/clear controls, per-row menus, and alias/date/count semantics.

Sweep unbuilt closed menu state. If its anchor leaves the viewport, dismiss an open row menu and release its row reference; do not let it silently retarget the row now occupying that position. Metadata remains O(saved sessions); the goal is viewport-bounded UI nodes and menu residency, not zero metadata storage.

### 8. Record the remaining framework work without assuming unavailable APIs

| Review finding | This change | Follow-up design / prerequisite |
| --- | --- | --- |
| Nuxt full-list storage and string selection | Borrowed rows and typed identities in the shared Nuxt table | Implemented in the separately authorized library follow-up |
| History filter/sort working sets | Explicit snapshot processing and transient keys | Further peak optimization only after long-text sort measurements |
| Per-built-cell wrapper nodes | Retain one decoration container for parity | Add an upstream MyGo body-cell style callback receiving the native cell and row/column identity, before content construction; move decoration there and remove exactly one wrapper |
| Frame-aged text layouts after scrolling | Measure native idle recovery, preserve bounded previews | Upstream text system needs an idle-trim operation serialized with rendering and aware of all windows, recently used layouts, and node-held layouts |
| History switching overlap | Remove owned shallow clone; retain fallback | Measure actual A/B/decoder/detail overlap before considering a user-visible loading-policy change |

For a future cell hook, require native padding/full-width background, tail-cell, clipping, selection/accessibility, and callback-lifetime tests, then demonstrate reduced node counts and retained UI at 27/52/77 rows. The ~1.3/~2.5 MiB node/state estimates are hypotheses, not included in this change's storage budget.

For idle trimming, do not call a shared cache purge when one history closes: other windows can still use it, and it can cause reshaping churn. A framework change must separately measure logical layout budgets, retained Go objects, native allocations, and redraw cost after idle. The authorized Nuxt table follow-up is limited to borrowed rows and typed identities; native cell-decoration hooks and shared text-cache changes remain deferred.

### 9. Use explicit acceptance workloads and gates

All proposed byte gates below use Windows amd64, Go 1.27.3, the pinned dependencies, `CGO_ENABLED=0`, and the report's fixtures. Re-establish matched baseline/candidate runs on the same machine, use three runs and compare medians, and keep fixture records alive through two-GC measurements. These gates are not asserted measurements or portable struct-size assumptions.

The budget estimate starts with roughly 0.77 MiB per 100,000-slot pointer/uint64 array on this build. The proposed All path needs an ordered-ID snapshot; active filters add only the needed result arrays, and sorting adds one ordered-pointer result. The gates allow capacity rounding and other small state beyond those arrays. They exclude the source records, UI, selected-ID sets, and transient sort keys, all of which are measured separately; the numeric gates do not establish a maximum for arbitrary text-sort peaks.

| Workload | Acceptance |
| --- | --- |
| 100k history, unselected, no rendered table: All / search `h` / status-sorted / search+status-sorted | Retained bookkeeping <= 2.5 / 4.5 / 3.5 / 5.5 MiB respectively; All/Filtered/Sorted also lower than matched baseline |
| Select All, then clear; delete/shrink/reset | Selected-ID storage is measured separately; clear/reset releases identity storage, obsolete pointers, and historical sort keys; no growth across repeated cycles after collection |
| Mutable 100k `Patch1`, stable ID/order | <= 32 KiB/op and <= 700 allocs/op, preserving the current ~22.4 KiB/547 reference behavior |
| Actual live selected/sorted/patch/churn benchmarks | B/op and allocs/op no more than 10% above matched `d932ef5` medians for each mode; investigate time regressions before acceptance |
| 64-byte to 64-KiB path scrolling, 20 page changes | 4–64 KiB variants remain flat within the existing bounded-allocation regression; 64-KiB B/op <= matched baseline + 10% |
| 1160-DIP table width, heights 714/1414/2114, 5k–100k rows | Visual builds scale with built rows/columns; retained UI <= matched baseline + 10%; record node counts without claiming wrapper removal |
| 100 versus 1,000 saved sessions at the same viewport | Similar visible row-build counts; list/menu node retention independent of total sessions apart from metadata and native sparse bookkeeping |
| 100k category rows, unchanged frames | Facet/root/item recomputation counters do not advance; source/filter/search/locale changes update the appropriate cached result |
| Categories hidden, history closed/replaced, late load result | Derived references are released at the defined boundary; choices survive hide; stale data cannot return |

Keep the old `BenchmarkTrafficSetRows/FullAdapter` only as a clearly labeled dependency microbenchmark, not the candidate's production adapter measurement. Extend storage benchmarks to explicitly exercise snapshot and live policy. Include empty/one-row/100k-to-two-row shrink, equal-size replacement with reused IDs, high numeric IDs, all columns hidden/fill-only layout, filtered selections, unknown values, and long mixed-case paths in correctness/retention coverage.

The UI matrix covers light/dark, English/Chinese, increased interface text size, horizontal scrolling, fixed/flex/reordered/hidden columns, selected/highlighted/inspected rows, process icons, native selection gestures, keyboard focus, menus, loading transitions, HAR dropping, and accessibility roles. Existing capture regression tests are the primary compatibility gate, with focused tests for new ownership/revision boundaries.

For native recovery, use the diagnostic procedure in `memory-diagnostics.md`: actual HBIN histories, load/scroll/sort/switch/close cycles, separate natural-idle samples at 0/30/120/300 seconds and forced-collection runs, and Process/Held/Go-object counters. Record dataset, dimensions, scale, themes, body selection, accessibility activity, and platform. There is no fixed 2x-peak or instant RSS-return criterion. Native Windows/macOS/Linux interaction checks are separate from compilation and headless measurements; unavailable checks stay open with a reason.

## Risks / Trade-offs

- **Borrowed result corruption** → Track owners separately, invalidate on source revisions, and test that filter release/sort never mutates history input or async action snapshots.
- **Nuxt reintegration regresses behavior or memory** → Reuse its existing renderer and decoration path, preserve the original allocation/storage gates, and test both old copied-string and new borrowed-numeric modes.
- **Cache invalidation misses unchanged final subsets** → Key facets from base revision plus category inputs and locale, not only final-result version.
- **Snapshot sort temporarily allocates folded strings** → Compute once per entry, release scratch after sorting, measure long mixed-case workloads separately, and do not claim a peak bound.
- **Saved-session virtualization changes focus or menu targeting** → Use stable keys, native list navigation, key-based reconciliation, and close menus whose anchor leaves view.
- **Sidebar cleanup erases user choices or runs too late** → Separate durable intent from derived models; notify on actual shell visibility transitions and test all sections/collapse paths.
- **Benchmark gains conceal process-memory costs** → Preserve separate storage/UI/allocation/native measurements and the report's exclusions; do not add overlapping profile totals.

## Migration Plan

1. Add matched acceptance fixtures and the explicit history processing policy, then implement filter/sorter ownership changes and validate them without altering live policy.
2. Exercise borrowed numeric Nuxt integration against the complete parity/storage gate; remove the local native adapter only after it passes. Preserve the copied-string API used by other Nuxt table users.
3. Apply the successful-load ownership contract and sidebar lifecycle/cache/virtualization changes with their specific regression tests.
4. Run the commands in `tasks.md`, update documentation with actual evidence and remaining framework work, and strictly validate the final implementation/spec agreement before synchronization/archive in the appropriate workflow.

No persisted data migration or new runtime setting is needed. Rollback is an implementation revert with unchanged history/settings compatibility. If an implementation gate fails, retain or restore the affected existing path and leave its task incomplete; revise the proposal before accepting a reduced scope.
