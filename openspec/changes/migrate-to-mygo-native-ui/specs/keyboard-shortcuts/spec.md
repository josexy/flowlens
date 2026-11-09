# Spec Delta

## Purpose

Defines the in-application keyboard commands, how they are scoped and customized, and the optional system-wide shortcuts.

## ADDED Requirements

### Requirement: Default command bindings
FlowLens SHALL provide these default bindings: Save `Primary+S`; Open Settings `Primary+,`; the six workbench sections `Primary+1` through `Primary+6`; New HTTP `Primary+N`; New WebSocket `Primary+Shift+N`; Close Tab `Primary+W`; Next and Previous Tab `Control+Tab` and `Control+Shift+Tab`; Send `Primary+Enter`; Focus Filter `Primary+F`.

#### Scenario: Primary modifier on macOS
- **WHEN** FlowLens runs on macOS
- **THEN** `Primary` means Command in behavior and in displayed labels

#### Scenario: Primary modifier elsewhere
- **WHEN** FlowLens runs on Windows or Linux
- **THEN** `Primary` means Control in behavior and in displayed labels

### Requirement: Command scoping
A shortcut SHALL run only while a FlowLens window has focus and an enabled handler exists for the current page, tab, and modal state.

#### Scenario: Blocked by a modal
- **WHEN** a modal dialog is open and the user presses the binding of a command that modals block
- **THEN** the command does not run

#### Scenario: No handler on the page
- **WHEN** the user presses the Send binding on a page with no active request
- **THEN** nothing is sent and the key press retains its normal behavior

### Requirement: Editable control precedence
Text inputs and code editors SHALL keep their own key behavior unless the command explicitly allows running while editing.

#### Scenario: Blocked in a text field
- **WHEN** focus is in a text input and the user presses the New HTTP binding
- **THEN** no tab is created

#### Scenario: Allowed while editing
- **WHEN** focus is in the request body editor and the user presses the Send binding
- **THEN** the active request is sent

### Requirement: Shared command behavior
A shortcut SHALL perform the same operation as the matching button or menu item.

#### Scenario: Saving through the shortcut
- **WHEN** the user presses the Save binding on a dirty rewrite rule
- **THEN** the rule is saved exactly as with the Save button

### Requirement: Customizing bindings
Settings SHALL let the user search commands, record or clear a single-step binding per command, and restore one or all commands to defaults.

#### Scenario: Conflicting binding
- **WHEN** the user records a binding already used by another command
- **THEN** FlowLens asks whether to replace it, confirming clears the binding from the other command, and canceling changes neither command

#### Scenario: Cleared binding
- **WHEN** the user clears a binding and saves
- **THEN** that command has no binding, including after restart

#### Scenario: Labels follow customization
- **WHEN** a binding is changed
- **THEN** menus and tooltips show the new binding

### Requirement: System-wide shortcuts
FlowLens SHALL offer optional system-wide shortcuts for showing the main window and toggling the proxy, disabled by default.

#### Scenario: Global shortcut while unfocused
- **WHEN** a system-wide shortcut for showing the main window is configured and pressed while another application is focused
- **THEN** the main window is shown and focused

#### Scenario: Rapid toggle
- **WHEN** the toggle-proxy shortcut is pressed repeatedly within 250 milliseconds
- **THEN** the proxy toggles once

### Requirement: System shortcut registration safety
A system-wide shortcut SHALL be registered with the operating system before it is persisted, and failures SHALL be reported without changing the saved configuration.

#### Scenario: Registration failure
- **WHEN** the operating system rejects a system-wide shortcut
- **THEN** the failure is shown, previously working registrations remain active, and the saved configuration is unchanged

#### Scenario: Unsupported session
- **WHEN** the desktop session offers no system-wide shortcut support
- **THEN** the unsupported state is shown in Settings and the rest of FlowLens works normally
