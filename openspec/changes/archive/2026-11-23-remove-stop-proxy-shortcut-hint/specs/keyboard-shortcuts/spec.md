# Spec Delta

## MODIFIED Requirements

### Requirement: Customizing bindings
Settings SHALL let the user search commands, record or clear a single-step binding per command, and restore one or all commands to defaults. The Stop Proxy menu SHALL omit shortcut hints even when Toggle Proxy has a binding, without changing shortcut behavior.

#### Scenario: Conflicting binding
- **WHEN** the user records a binding already used by another command
- **THEN** FlowLens asks whether to replace it, confirming clears the binding from the other command, and canceling changes neither command

#### Scenario: Cleared binding
- **WHEN** the user clears a binding and saves
- **THEN** that command has no binding, including after restart

#### Scenario: Labels follow customization
- **WHEN** a binding is changed
- **THEN** menus and tooltips that display shortcut hints show the new binding

#### Scenario: Stop Proxy menu omits shortcut hints
- **WHEN** the user opens the proxy dropdown while recording is active or paused, with or without a Toggle Proxy binding
- **THEN** Stop Proxy shows its power icon and localized label without shortcut keycaps
- **AND** selecting it still stops the proxy, and configured application or global Toggle Proxy bindings retain their start/stop behavior
