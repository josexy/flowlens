# Design

## Context

See proposal.md. The stores discard bodies but the long-lived inspection panel keeps separate Raw, formatted, Hex, image, SSE, and WebSocket state. History cleanup currently depends on rendering the capture workspace.

## Goals / Non-Goals

Release obsolete references on the main thread at their owning state transition. Preserve current capture data when merely stopping the proxy, and keep the selected record when details close. Do not change GC policy, export semantics, body storage, or normal background capture.

## Decisions

- Add explicit store reset callbacks wired by the capture workspace to its derived lists and panel Release methods. Rendering-only cleanup would miss inactive surfaces; GC cannot reclaim reachable data.
- Reuse Panel.Release and cancelable runtime jobs. Preserve lightweight viewing preferences while replacing payload-bearing viewer state.
- Reset derived body/message views when their content is replaced or their dialog closes, without discarding valid streaming state on append.
- Test state transitions and obsolete async completions directly, with a focused heap reproduction as supporting evidence rather than a fragile global memory threshold.

## Risks / Trade-offs

- Releasing a viewer requires rebuilding it on reopening. Preserve tab/wrap choices and test reopened content.
- Late image/SSE results could recreate caches. Cancel their job keys before dropping the viewer and exercise queued completions.
- Callback wiring may miss hidden surfaces. Exercise reset before another frame and verify source-specific cleanup.

## Migration Plan

No data migration. Deploy with the next application build; rollback is a code revert.

## Verification

- `CGO_ENABLED=0 go test ./...`: passed on Windows.
- `task vet:ui` and `task lint:go`: passed (zero lint issues).
- Capture/history reset, deletion, detail close/replacement/reopening, queued image/SSE results, message-dialog cleanup, streaming append, and Hex selection regression tests passed, including targeted `CGO_ENABLED=1 go test -race` runs for capture, inspection, and stores.
- The approximately 8 MiB body reproduction measured 2.16 MiB baseline, 35.04 MiB loaded, and 2.00 MiB after capture reset and test-only GC. Production does not force GC.
- Closing-history renders were inspected in both light and dark themes. Replacing the category tree pointer keeps the current frame's list state valid while discarding the obsolete view state.
- The broad desktop/process-attribution/proxy race run passed every package except capture, which reached the default 10-minute package timeout during `TestHistoryInitialLoadingKeepsFiltersAndHeader`. That test passed alone under race in 6.3 seconds. A supplementary full inspect race run with an explicit 90-second cap also timed out; inspect passed in the broader run (257 seconds). The latest changed paths passed targeted race checks. Complete capture-suite race coverage remains limited by the runtime budget, with no race report observed.
