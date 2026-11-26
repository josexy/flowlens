# Design

## Context

See proposal.md for motivation. History rows are summaries; inspect must never interpret them as complete headers. Complete action reads already use GetHistoryDetail. MyGo v0.10.0 exposes frame-local Now/After and a deterministic Tester clock.

## Goals / Non-Goals

**Goals:** Separate metadata and body readiness, keep stable layout while loading, and preserve existing ownership/cancellation and exact data semantics.

**Non-Goals:** Keep old bodies visible, cache multiple full records, block the UI on disk reads, or delay ready content to prolong a spinner.

## Decisions

- The history service exposes context-aware metadata-only and body-only reads, reusing existing indexed HBIN paths. Full GetHistoryDetail remains the action API. No migration or writer changes.
- HistoryTraffic owns two cancelable jobs and one selection generation on the main thread. A guarded request records body demand and starts one read once metadata is available. Completed/failed attempts are not retried by every frame. Closing/replacing resets both stages and invokes existing viewer cleanup.
- An optional deferred-data interface supplies the pending row identity and a body request callback to inspect. Existing live capture continues its eager streaming subscription behavior. Only body-dependent panes invoke the guarded request.
- Pending metadata uses the same splitter and tab-strip keys. Retain only small tab IDs for layout, with localized labels and disabled tabs, never previous header values or counts. Keep the close button usable. TCP keeps its single-pane form.
- Frame-clock deadlines delay spinners by 180 ms and use Context.After for a future redraw. Store no Context or element and create no background UI timer. Selection cleanup resets deadlines; ready content is never delayed.
- Raw can render complete headers while its body loads, but copy stays disabled until the full payload is available. Body/WebSocket panes use delayed loading feedback. Existing failure logging and cancellation remain; metadata survives body failure.

## Risks / Trade-offs

- Very slow metadata reads still need an empty content area; preserving chrome and delaying feedback removes unnecessary whole-panel flashes without retaining old data.
- Separate file reads can fail independently; keep metadata available and validate/cancel each stage, with normal reopen retry.
- Changes affect both layouts and shared loading indicators; use deterministic clock tests, lifecycle/action regressions, and light/dark renders.

## Migration Plan

No persistent changes. Reverting the staged service/store calls restores combined loading. No new strings, focus bindings, or shortcuts; preserve tab choices and the existing close action.
