# Proposal

## Why

Switching from an open history tab to live capture currently selects and expands the Capture sidebar, displacing Categories or API Collections. Tab selection should update the traffic source without overriding the user's sidebar navigation.

## What Changes

- Preserve the selected sidebar section, its visibility, and its width when activating workspace tabs or closing history tabs.
- Continue synchronizing the Capture sidebar's selected item and the Categories data source with the active traffic tab.
- Retain explicit sidebar navigation and history cleanup behavior.
- Complete when regression tests cover pointer and shortcut activation, collapsed sidebars, and history closure, and the relevant Go and UI checks pass.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `desktop-shell`: Specify that workspace tab changes preserve sidebar navigation.

## Impact

The change belongs to `internal/desktop/capture`, reusing `internal/desktop/store` state and the existing editor and shell. Update the user guide and desktop-shell specification. Behavior is shared by Windows, macOS, and Linux; no platform API or layout changes are needed. Persistence, files, networking/models, Python Workers, process attribution, service events, secondary windows, and localized copy are unaffected. Shortcut bindings remain unchanged. No unrelated navigation redesign or new state abstraction is in scope.
