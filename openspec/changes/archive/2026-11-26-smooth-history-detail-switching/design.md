# Design

## Context

See proposal.md for the visible issue. HistoryTraffic currently clears metadata, bodies, and Panel caches before every asynchronous row read. Spinner delay therefore cannot prevent the intermediate empty panes. The existing stable-loading and cache-release contracts need the explicitly requested bounded-retention exception.

## Goals / Non-Goals

Preserve a single previous rendered record while the current target loads, without changing the lightweight row list or making stale content actionable. Keep metadata/body stages independent and use the existing cancellation tokens, dispatcher, clock, and nui controls. Do not change service reads, persistent formats, network behavior, or live capture.

## Decisions

- HistoryTraffic owns a distinct immutable previous-detail snapshot and a cancelable 180 ms main-thread deadline. Repeated row changes preserve the same snapshot and deadline rather than accumulating snapshots. Normal selection fields remain exclusively the new target.
- Panel chooses when the new active panes are ready. It requests a body only for applicable body-dependent tabs, then releases the previous snapshot and derived viewers before building the replacement. A slow read expires through the store clock even while hidden. Failures and normal close/reset/delete paths release immediately.
- The retained snapshot implements the existing detail-data shape. Rendering uses it only for the temporary view; actions and service requests continue using the selected record. Disable retained content and tabs without opacity changes, retaining a usable close button and normal table focus/navigation. No additional interface copy is needed in either language.
- If the previous detail still owns keyboard focus, move focus to its close button during retention: the native code editor otherwise keeps its copy commands under a disabled ancestor. A table that already received focus keeps it. Persistent UI handles identify the detail scope and close action without retaining build values.
- Preserve the loading deadline across the retained-to-pending transition so slow reads do not incur a second spinner delay. Main-thread state owns all snapshots; runtime.Run still owns I/O and rejects canceled results. No Context or Element is retained.
- Immediate blanking is the current behavior and leaves the flash. Full-history prefetching would violate lightweight browsing, and unbounded previous-record caching would undermine memory release.

## Risks / Trade-offs

- A large old body temporarily overlaps the incoming body -> retain only one previous view, for at most 180 ms, without extending the deadline; all terminal paths release it.
- Stale content may appear current -> keep its source distinct, mark the view busy, and disable all content input, with close outside the disabled content.
- Body-dependent tabs could flash after metadata -> retain until the required payload settles or the common deadline expires; failures preserve new metadata.
- Layout or disabled opacity could still flash -> use stable existing control identities and test rendered frames in both layouts, themes, and languages.

## Migration Plan

No storage/API migration or new dependency is required. Reverting this change restores the previous loading presentation. Automated UI tests are platform-independent and run on Windows; native macOS/Linux interactive acceptance is outside this UI-only change's automated evidence.
