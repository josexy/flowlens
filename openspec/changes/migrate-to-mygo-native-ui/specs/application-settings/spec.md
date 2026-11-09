# Spec Delta

## Purpose

Defines the Settings window, covering general, proxy, Python, shortcuts, logging, and storage configuration, and how unsaved changes are handled.

## ADDED Requirements

### Requirement: Settings sections
The Settings window SHALL provide General, Proxy, Python Plugins, Shortcuts, Logs, and Storage sections.

#### Scenario: Navigating sections
- **WHEN** the user switches sections with unsaved changes in the current section
- **THEN** the changes remain and can be saved together

### Requirement: Unsaved changes guard
The Settings window SHALL track unsaved changes and confirm before they are discarded by closing the window.

#### Scenario: Closing with changes
- **WHEN** the user closes Settings with unsaved changes
- **THEN** FlowLens asks for confirmation before discarding them

#### Scenario: Saving
- **WHEN** the user saves valid changes
- **THEN** they persist, take effect in every window, and the unsaved state clears

### Requirement: Concurrent preference preservation
Saving settings SHALL NOT overwrite shortcut, Python runtime, traffic table, or traffic detail preferences that were updated elsewhere after the Settings window opened.

#### Scenario: Layout changed while Settings is open
- **WHEN** the user changes the traffic detail layout in the main window and then saves Settings
- **THEN** the newer detail layout is retained

### Requirement: General settings
General settings SHALL configure fonts, font sizes, theme colors, title bar mode, main window close behavior, and pointer cursor behavior.

#### Scenario: Font search
- **WHEN** the user searches the font list
- **THEN** matching system fonts are offered with a preview

#### Scenario: Title bar change requires restart
- **WHEN** the user changes the title bar mode
- **THEN** a notice states that a restart is required

### Requirement: Proxy connection settings
Proxy settings SHALL configure process attribution, upstream proxy mode (None, System, Custom), and connection options.

#### Scenario: Custom upstream without a URL
- **WHEN** the upstream proxy mode is Custom and the URL is empty or invalid
- **THEN** saving is blocked with a message

#### Scenario: Restart prompt
- **WHEN** a saved change requires the running proxy to restart
- **THEN** the user is told and can restart the proxy

### Requirement: Certificate authority configuration
Proxy settings SHALL let the user use the built-in CA or select custom CA certificate and private key files, and inspect the selected CA.

#### Scenario: Custom CA mode
- **WHEN** built-in CA is turned off
- **THEN** both custom paths are required and generate and regenerate actions are disabled

#### Scenario: Invalid built-in CA files
- **WHEN** the built-in CA files are incomplete, invalid, or expired
- **THEN** Settings still opens and offers Generate or Regenerate CA without overwriting files automatically

#### Scenario: Trusted roots and client certificates
- **WHEN** the user adds extra trusted root paths or per-host client certificates and saves
- **THEN** they are persisted and used for upstream connections

### Requirement: Current-user CA trust
On Windows and macOS, Settings SHALL install or uninstall the saved CA for the current user and show whether trust is installed, incomplete, or unknown.

#### Scenario: Installation
- **WHEN** the user installs a valid saved CA
- **THEN** only the public certificate is added for the current user and the status updates

#### Scenario: Incomplete installation
- **WHEN** macOS authentication is canceled during install
- **THEN** the status shows installation incomplete and uninstall remains available

#### Scenario: Status unknown
- **WHEN** the trust store query fails
- **THEN** the action is disabled until the status can be refreshed

#### Scenario: Regenerating an installed CA
- **WHEN** the installed CA has not been uninstalled and the user tries to regenerate it
- **THEN** the user is told to uninstall it first

#### Scenario: Linux
- **WHEN** FlowLens runs on Linux
- **THEN** the install and uninstall control is not offered

### Requirement: Python runtime settings
Settings SHALL configure Python plugin enablement, interpreter path by browsing or detection, connection test, and hook timeout, and show runtime status.

#### Scenario: Detecting interpreters
- **WHEN** the user runs detection
- **THEN** discovered interpreters are listed with versions and can be selected

#### Scenario: Testing an interpreter
- **WHEN** the interpreter test fails
- **THEN** the error is shown and the previous runtime stays in use

### Requirement: Log settings
Log settings SHALL configure enablement and level, and offer clearing logs and opening the log directory.

#### Scenario: Changing the level
- **WHEN** the user changes the log level
- **THEN** the level applies immediately

### Requirement: Storage settings
Storage settings SHALL configure the body cache threshold, WebSocket message retention, and history retention, and show disk usage for cache, history, and total.

#### Scenario: Clearing cache only
- **WHEN** the user confirms clearing cache files with a non-empty capture
- **THEN** the current capture is archived first and then the live list is cleared

#### Scenario: Clearing all data
- **WHEN** the user confirms clearing all data
- **THEN** caches, the current capture, and readable saved history are removed while the database, API Collections, preferences, certificates, and logs remain

### Requirement: Shortcut settings
The Shortcuts section SHALL manage in-app and system-wide bindings as defined by the keyboard shortcut capability.

#### Scenario: Unsaved shortcut change
- **WHEN** the user records a binding and closes Settings without saving
- **THEN** the binding is discarded after confirmation
