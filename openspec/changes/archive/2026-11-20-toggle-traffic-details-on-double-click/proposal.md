# Proposal

## Why

Traffic details can currently be opened by double-clicking a row, but closing them requires another action. The requested interaction makes an unmodified double-click toggle the detail panel. Current code already reserves Ctrl/Cmd-click for native additive selection rather than a visibility shortcut.

## What Changes

- Toggle details with an unmodified row double-click in the shared capture/history table.
- Preserve single-click selection, Ctrl/Cmd additive selection, Shift range selection, and Enter/context-menu Show as explicit open actions. Empty selection still clears and closes details.
- Update the user guide and the existing inspection specification.
- Complete when native input regression checks prove open/hide/reopen, selection and keyboard behavior remain intact, required Go/UI checks pass, and the change is synchronized and archived.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: Double-click toggles traffic detail visibility while retaining native selection and explicit open/close behavior.

## Impact

`internal/desktop/capture` owns the shared live/history table and its input tests. The behavior is the same on Windows, macOS, and Linux; the existing platform-specific additive modifier remains intact. Documentation changes are limited to `docs/user-guide.md` and the inspection spec. No interface strings or appearance changes are needed in either language or theme.

Persistence/files, networking/models/codecs, Python Workers, process attribution, event subscriptions, shortcut configuration, other windows, and dependencies are unaffected. No migrations or service changes are in scope.
