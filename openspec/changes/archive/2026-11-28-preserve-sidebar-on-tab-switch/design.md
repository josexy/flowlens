# Design

## Context

`capture.Workspace.syncTabs` synchronizes workspace tabs with `WorkbenchStore.CaptureSelection`. Its calls to `SelectCaptureItem` also select the Capture section and expand its sidebar. This conflates a tab's traffic source with an explicit sidebar navigation action. See proposal.md for motivation.

## Goals / Non-Goals

Keep tab synchronization within the capture workspace and preserve the store's explicit navigation API. Do not introduce another state owner, change tab focus, or change history loading and cleanup.

## Decisions

Set only `CaptureSelection` when synchronizing a changed tab or removing a stale history selection. Continue using `selectItem` and `SelectCaptureItem` for explicit item selection. Changing `SelectCaptureItem` globally would alter actual sidebar navigation; adding another store abstraction is unnecessary for a field assignment. Keep `lastSelection` updated to avoid treating synchronization as a new sidebar action on the next frame.

All changes remain on the main thread in the existing MyGo build flow. No asynchronous jobs, service events, persistence, codecs, migrations, or cleanup ownership change. History data continues to close through `syncHistoryData`.

The main window is the only affected window. Exercise the real shell and editor tab strip with `ui.NewTester`, pointer actions and existing tab shortcuts, repeated frames, Categories and API Collections, collapsed sidebars, active/inactive history closure, and explicit Capture navigation. Run the interaction test in light/dark themes and English/Simplified Chinese. No copy keys or visual styles change.

## Risks / Trade-offs

- Stale source state after closing an inactive history tab while an editor is active: cover both the changed-active-tab and unchanged-active-tab paths.
- Categories could display the previous source after a tab change: assert its source label after settled frames.
- Cross-platform native-window behavior is not exercised by headless tests; this change uses no platform-specific APIs or layout changes.

## Migration Plan

No migration is required. Reverting the synchronization change restores the previous interaction without affecting saved data.
