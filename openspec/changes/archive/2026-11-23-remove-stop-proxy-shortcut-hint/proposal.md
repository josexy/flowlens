# Proposal

## Why

The Stop Proxy dropdown renders keycaps when Toggle Proxy has a custom binding. The user wants this single-action menu to show only its power icon and label.

## What Changes

- Omit shortcut hints from the Stop Proxy menu regardless of the configured binding.
- Preserve the stop action, focus restoration, and application/global shortcut behavior.
- Update the existing menu render test and user guide. Completion requires successful UI checks and English/Chinese light/dark renders with a configured binding.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `keyboard-shortcuts`: Exempt the Stop Proxy menu from binding hints while retaining shortcut customization and dispatch.

## Impact

The owner is `internal/desktop/capture`; the shared toolbar behaves identically on Windows, macOS, and Linux. The existing shortcut catalog and settings remain authoritative. Persistence, files, networking/models, Python Workers, process attribution, event dispatch, other windows, and localized copy are unchanged. No API, dependency, or data migration is needed. Documentation changes are limited to the user guide and shortcut specification.
