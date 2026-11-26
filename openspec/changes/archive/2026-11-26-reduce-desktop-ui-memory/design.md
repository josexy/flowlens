# Design

## Context

See [proposal.md](proposal.md) for scope and acceptance criteria. The baseline is `20e2d64`, with MyGo `v0.10.0` and Nuxt UI Go `42aa9b196036`.

Nuxt applies a column's `Decoration.Style` to its content row immediately before calling `Cell`. `widgets.Table` stretches that row across native padding. This permits decorating existing cells without changing table ownership.

## Goals / Non-Goals

**Goals:** reuse native cells and one bounded bitmap while preserving layout, input, accessibility, and payload release.

**Non-Goals:** replace the table, change header rendering or runtime GC policy, or modify dependencies.

## Decisions

### 1. Decorate the existing cell

`capture.Table.decorateColumn` pairs synchronous `Decoration.Style` and `Cell` callbacks through a build-local `ui.Element`. The reference never enters persistent state or an input/drawing callback, and each build pass supplies a fresh element.

Apply `.Background(...)` to the existing cell and use `.DrawOver(...)` for the outline and start marker. This removes decorative nodes without adding layout borders or changing row height. Keep opaque badge colors and selection/inspection/highlight precedence.

A build-local cache shares drawing method values for equal `rowOutlineStyle` values. They contain only colors, zoom, and first/last flags, never records, bodies, elements, or contexts. Clip drawing per cell and derive edge flags from displayed column order, including `row-fill`.

For `C` displayed cells, this removes `C + 1` nodes per ordinary row and `2C + 2` per outlined/marked row. With 11 business columns, savings are 12/24 without fill or 13/26 with fill.

### 2. Keep the native table as the menu target

The first displayed cell supplies committed vertical bounds to `noteRowSpan`, replacing the standalone row-span element. The table panel retains the input observer and context-menu anchor.

Right-clicks preserve selected groups, select unselected rows, open column controls on the header, and do nothing below the rows. Keyboard menus retain the table-corner anchor. Cover scrolling, hidden/reordered columns, resized Path, fill cells, and empty tables.

### 3. Share the scaled icon

`widgets.AppIcon` uses `sync.OnceValue` and `ui.DecodeBitmapScaled(resources.AppIcon, 256)`. Custom title bars and Update reuse its immutable pointer on the UI thread at 18 and 48 DIPs.

The base image and mipmaps retain about 0.33 MiB of CPU pixels. The 1024-pixel packaging resource stays unchanged; the full decode occurs transiently before shrinking. Each window's GPU textures remain owned by MyGo. Decode failure returns nil and leaves the window controls usable. Keep the scaled pixels available for rendering and device recovery.

### 4. Preserve cleanup ownership

Existing stores and services still own traffic and body data. Closing or replacing a history/detail uses existing cancellation, stale-result rejection, `HistoryStore.CloseOpened`, `Table.Reset`, and panel release hooks. No new asynchronous work or subscriptions are introduced.

Release tests keep the window alive, render subsequent frames, and verify old rows/bodies are collected while preferences survive. The test runtime clears dequeued callback slots so its queue does not falsely retain payloads. The Windows storage-failure test holds a file open without delete sharing; Unix retains its permission fixture.

### 5. Compare matched workloads

Keep binary revision, data, window size, DPI, theme, columns, body view, capture state, and UIA activity matched. Table benchmarks measure warmed retained heap outside timed work, with records and tester alive; frame time, allocation bytes/counts, and cells remain separate.

Use separate runs for collected and natural-idle measurements. After ten identical history/body/close cycles, compare collected cycles 6-10 against the 5 MiB band. For natural idle, sample Process, objects, and Held at 0/30/120/300 seconds without collection requests.

The 20% table reduction is a target, not an assumed result. Record shortfalls; keep the change open if new allocations erase node savings. Native checks cover main/settings/update icons and table interactions in both themes and languages. Headless renders and cross-compilation do not substitute for native runtime checks.

## Risks / Trade-offs

| Risk | Mitigation |
| --- | --- |
| Stale frame elements or retained payloads | Synchronous local cell reference, scalar drawing state, `task vet:ui`, and repeated release tests. |
| Border seams or incorrect row hits | Pixel/bounds tests plus native scrolling, selection, column, and fill-cell checks. |
| Soft icons at high DPI | Inspect 100/150/200/400% where supported; retain the 0.5 MiB CPU-pixel budget. |
| Heap savings obscured by pools or native resources | Separate warmed table, collected-process, and natural-idle measurements. |
| Shared UI behaves differently across platforms | Record Windows runtime results and macOS/Linux compilation/runtime coverage separately. |

## Migration Plan

No data migration is required. The icon and table changes can be reverted independently without changing stored histories or settings. The remaining native icon acceptance is recorded in [tasks.md](tasks.md).
