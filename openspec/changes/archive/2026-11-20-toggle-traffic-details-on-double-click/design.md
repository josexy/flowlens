# Design

## Context

See proposal.md for motivation. Capture and history share `capture.Table`, whose native MyGo submission handles both double-click and Enter. The current submission always opens details. MyGo exposes `RowDoubleClicked` on cells without taking input away from native selection.

## Goals / Non-Goals

Preserve native selection, focus, and submission while distinguishing pointer activation from Enter. Keep all work in the existing capture owner; do not add gesture timers, custom key handlers, persisted state, or dependency changes.

## Decisions

- Observe `RowDoubleClicked` in the existing nui table cell style callback and capture a build-local boolean. On unmodified submission, close a visible panel only for a row double-click; otherwise use the existing focused-row open action. This avoids durable gesture state and keeps Enter and Show idempotent.
- Reuse `CloseDetails` so selection and focus restoration stay intact. Keep the existing modifier guard: Ctrl/Cmd-click changes selection and never directly toggles details. Empty selection still closes the panel.
- Use the same behavior for live and historical traffic, in both light/dark themes and languages. No rendered copy or layout changes are needed; update only the user-guide instructions.
- State remains on the main thread. Existing store close/reopen paths own body cleanup and stale-result rejection; no changes to asynchronous jobs, persistence, service models, or codecs are needed.

## Risks / Trade-offs

- Native submission includes keyboard input: exercise repeated Enter to ensure it still only opens details.
- A double-click includes a first selection click: changing rows updates an open panel, then the double-click hides it; test this explicitly.
- Cell padding and badges must also recognize the gesture: use the full nui cell element and native input tests, preserving table input ownership.
- Headless tests cover interaction on the host platform. Native macOS/Linux desktop acceptance is not implied by Windows checks.

## Migration Plan

No migration. Reverting the table callback and corresponding docs/spec restores the prior open-only double-click behavior.
