# Design

## Context

See proposal.md. MyGo v0.10.0 shapes whole strings even for single-line cells. Nuxt already reuses row storage and validates selection in SetRows. The application currently rebuilds its inputs and three sorter maps per refresh.

## Goals / Non-Goals

Bound text sent to the renderer and eliminate whole-list allocation on stable patches. Preserve sorting, selection, row actions, and cleanup. Do not change service snapshots, HBIN/HAR, full traffic values, asynchronous jobs, native controls, or dependencies.

## Decisions

1. `logic.TrafficCellPreview` supplies a prefix capped at 256 UTF-8 bytes including an ellipsis, cutting only at a rune boundary. Capture's shared text and badge renderers use it. Original strings continue into search/sort/copy/inspection/export. A rendering-only cap fixes the allocation before shaping; width clipping alone does not. No new translated copy or theme-specific styling is needed. The limit was tightened from the initial 512-byte implementation after review.
2. `FilterStore` keeps one pointer-keyed compact match cache and reusable base/filtered pointer slices. The default unfiltered All view needs no matching cache. Changed entry pointers or filter keys recompute predicates; each refresh toggles a mark and removes unmarked records, including old revisions of the same ID. Returned slices are main-thread borrowed views valid until the next refresh, which matches the synchronous table/category consumers. Change detection compares old pointers before replacing them, including removals and empty results. This avoids per-refresh maps and slices while retaining current versions and avoids retaining an ID/entry pair for every filter match.
3. `TrafficTableSorter` keeps one ID-keyed entry/value cache, a reusable sorted slice, and a generation for pruning. Cache case-folded text once per changed value, preserving original-text and ID tie breaks and unknown values last. A stable order replaces entry pointers in place. Callers treat sorted slices as borrowed views until the next sort.
4. The table owns a numeric ordered-ID snapshot and reusable index input. Unchanged ordered IDs skip SetRows entirely. For changed order, SetRows validates membership before focus reconciliation, removing the duplicate selection map. Unchanged positions reuse existing native string IDs.
5. Reusable slices clear removed pointer slots. Cache/storage capacity is released on empty/reset and shrunk when it exceeds four times source size (minimum 64). No old revisions or removed records are retained in caches. Existing history/capture reset hooks remain authoritative.

## Risks / Trade-offs

- Borrowed slices can silently change under a consumer: keep their use on the main thread, compare IDs using the independent snapshot, clone when a consumer needs a persistent snapshot, and exercise table/category/selection integration tests.
- Reusing storage can retain deleted data: test replacements, churn, large-to-small sources, and weak references after release.
- Text previews can hide a distinguishing suffix: keep original values in sorting/search and all actions; show the ellipsis and document how to copy full values.
- Warm caches do not measure scrolling: benchmark both idle and scrolling long paths, plus selected/sorted patches. Headless Go heap results exclude GPU and native process memory.

## Migration Plan

No data migration. Revert the desktop changes to roll back. Validate on Windows with Go tests, UI vet, and light/dark renders; record other-platform native runtime limitations without presenting headless tests as native acceptance.
