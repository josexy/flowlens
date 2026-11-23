# Design

## Context

The menu's `Shortcut` field currently receives the resolved Toggle Proxy binding. `nui` renders a nonempty value as keycaps; Toggle Proxy has no default binding. See proposal.md for the requested change.

## Goals / Non-Goals

Remove the presentation hint without removing the command or clearing the user's binding.

## Decisions

Omit `Shortcut` only on this menu item in `internal/desktop/capture/toolbar.go`. Changing the shared binding helper or menu component would affect unrelated hints. Keep the existing power icon, localized label, busy guard, callback, and focus restoration.

Adapt the existing menu test using its configured Ctrl+Shift+Alt+F12 binding, English/Chinese locales, both themes, and normal/large fonts. Verify label geometry and the absence of keycaps; retain the existing action and keyboard-focus tests. No new localization strings are needed.

## Risks / Trade-offs

The binding is less visible in this menu; Settings > Shortcuts remains the place to inspect and edit it. Other toolbar surfaces reuse the same capture component. This adds no state, background work, storage, or platform-specific behavior. Rollback restores the presentation field and its former test expectations; no migration or cleanup is needed.
