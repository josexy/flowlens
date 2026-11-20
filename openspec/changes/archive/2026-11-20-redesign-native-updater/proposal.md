# Proposal

## Why

The update window invents verification and preparation phases from a timer even though MyGo exposes Check and Install, and every successful update requires a separate restart action. A compact native updater should report observable progress and support an explicitly selected automatic, guarded restart.

## What Changes

- Replace the four-step display with a single status, release notes, download progress, and a stable action area.
- Represent MyGo Install as one operation, without timed verification/preparation phases.
- Default the per-session restart-after-install choice on; capture it when the user starts installing and request a guarded restart exactly once on success.
- Allow hiding the window throughout an operation; keep cancel separate and disable it once the reported download completes.
- Keep manual installation, signature verification, delta fallback, unsaved-work guards, and normal shutdown.
- Make failed restart attempts retryable without reinstalling the update.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `application-updates`: truthful progress, simpler window interactions, and automatic guarded restart after successful installation.

## Impact

Changes are confined to app update integration, app_service, shared updater state, update window, shell status copy, locales, tests, and update documentation. No dependency upgrade, persistent setting, database change, transport change, Python Worker change, attribution change, or shortcut change is needed. Existing lifecycle guards and shutdown services remain authoritative.
