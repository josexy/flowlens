# Design

## Context

See proposal.md for motivation. MyGo v0.10.0 exposes Check and Install, with byte progress and a final error. Install replaces files on Windows/Linux and stages a bundle/helper on macOS. FlowLens already has an authoritative controller, revisioned shared store, and guarded relaunch path.

## Goals / Non-Goals

Use observable framework outcomes and retain one operation at a time. Keep update lifetime independent from window lifetime. Do not change framework files, signing, downloads, persistent settings, or process cleanup.

## Decisions

- Keep `checking`, `available`, `installing`, `ready`, and `error` with idle/up-to-date results. Install covers the complete framework call; bytes measure download only. Remove the timer and phase-reporting adapter. Mark cancellation unavailable at a positive total's completion, restore it if delta fallback reports an incomplete download, and always await the framework call on shutdown.
- Publish measured progress as complete revisioned snapshots, so receiving progress before the initial state cannot leave controls in an older phase.
- Pass the restart choice into the install command. Keep the editable default in the shared UI store and the captured choice in the operation snapshot. Emit the existing guarded restart event once after successful completion, outside the controller lock, and suppress it on cancellation/shutdown. A view callback would lose automatic restart when its window closes, so the controller owns completion.
- A restart error retains an installed release and retry restores ready before requesting the same guarded restart. No new restart loop or timer. Declining existing unsaved-work dialogs keeps ready and does not re-arm automatic restart.
- Closing the secondary window always hides progress without canceling the work. Cancel remains an explicit button when allowed. Main-window quit behavior and shutdown waiting remain unchanged.
- Visual plan: a quiet native utility window with the FlowLens icon, left-aligned status and running version, measured progress below it, scrollable release notes, and a fixed footer containing the restart checkbox and actions. Remove decorative cards, shadows, uppercase eyebrow, repeated metadata, and four-step tracker. Reuse app theme fonts and semantic colors; reference neutral light/dark bases are white #FFFFFF / slate #0F172A, primary blue #3B82F6, muted slate #64748B, borders #E2E8F0, and error red #EF4444, resolved through the configured nui theme rather than hardcoded. Use 20px status, 14px body, 12px metadata; 24px outer inset and 8/12/16px gaps. Native controls and app theme overrides take precedence. Review light/dark, both languages, smallest window, and long notes/errors.

```text
[FlowLens icon] Current update status
                Running version / short explanation
                [measured download progress when active]
------------------------------------------------------
Release notes                         New version/date
Scrollable Markdown
------------------------------------------------------
[x] Restart automatically when finished
    Unsaved changes will be checked before restarting.
[Hide / Close]                 [Cancel / Install & restart]
```

The initial dashboard-like card-and-stepper approach would repeat the old design's artificial stages; the revised layout puts the release decision and action together without a decorative workflow diagram.

## Risks / Trade-offs

- MyGo has no separate verification callback: keep one install operation and use a neutral finishing message after the last byte.
- Cancellation can race a successful install: preserve ready on success, suppress automatic restart, and never claim files were rolled back.
- macOS replacement and actual process relaunch are platform behavior: exercise adapters and guards in tests, but do not download or replace the developer's running app.
- Existing guards may require attention in another window: reuse their current presentation and preserve manual restart after decline.

## Migration Plan

No data migration. Generate both catalogs, run service/app/store/window/shell tests and UI vet, render both themes and languages, update documentation, validate and archive the change with its main spec synchronized.
