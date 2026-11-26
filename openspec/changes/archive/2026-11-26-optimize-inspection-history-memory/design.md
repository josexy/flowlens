# Design

## Context

See proposal.md. MyGo v0.10.0 Buffer.Reset keeps a bounded version log containing old complete text snapshots. BodyViewer currently fills both text buffers even when a mode is unavailable. HBIN body decoding first builds byte-backed readers and then copies them into a view. History rows currently hold full TrafficEntry values.

## Decisions

- Widgets replace read-only buffers and immediately synchronize editor state to release hidden snapshots; editable buffers and incremental streaming edits keep their existing semantics. Inspect prepares Raw/Formatted lazily and exposes viewer reset preserving preferences. The workspace store notifies the editor of response replacement on the main thread, releasing previous derived content even for an inactive request tab without waiting for that editor to render.
- Proxy service owns decoded byte buffers. HBIN readers expose ownership explicitly so body views can take those bytes; ordinary capture readers still copy their mutable data. Do not change HBIN v1-v3 or streamed export writers.
- History service provides explicit summary-list and complete-detail APIs. Summaries retain existing TrafficEntry table fields, message protocols/metrics/content types, addresses, process information, and TCP ALPN; they omit complete headers/trailers/certificates. Reuse the existing decoder one record at a time and buffer disk reads. Keep full GetHistory for existing service callers. A separate complete selected detail avoids modifying immutable list entries or retaining loaded details in the list.
- Store background jobs pass contexts into history reads, cancel a superseded detail job, and reject stale results. Table actions resolve complete historical entries before producing cURL or request drafts. Export and resend continue using the service's complete records. Views and published state stay on the main thread. Closing history drops summaries, selected detail, bodies, filters, and table caches.

## Risks / Trade-offs

- Partial records accidentally reaching details/actions: distinct APIs, separate detail state, and regression checks for every table field/filter plus complete action headers.
- Editor identity and hidden-state retention: use documented State synchronization; verify extra frames, light/dark themes, read-only selection, wrap, copy/save, and SSE boundaries. No layout, shortcut, localization, or window changes.
- Cancellation while reading compressed bodies: context-aware reader and stale-result checks, with file/decoder cleanup on all returns.
- Summaries still scale with row count and decode full headers transiently: this preserves global search/sort and existing decoder validation without introducing a second codec. Memory tests quantify retained bytes separately from total allocations and process memory.

## Migration Plan

No persisted data or schema changes. Reverting the implementation restores previous loading behavior without migration. Windows headless tests and task verify provide acceptance for the platform-independent changes; no native macOS/Linux UI acceptance is claimed.
