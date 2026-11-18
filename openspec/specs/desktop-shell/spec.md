# desktop-shell Specification

## Purpose
Defines the FlowLens application frame: its windows, workbench layout, tray presence, single-instance behavior, startup failure recovery, and how it closes and quits.

## Requirements

### Requirement: Self-contained native interface
FlowLens SHALL render every window with native UI and SHALL NOT require a web view, a Node.js runtime, or a local web server.

#### Scenario: Running without web runtimes
- **WHEN** FlowLens starts on a machine with no Node.js and no web view runtime
- **THEN** the main window and every secondary window open and work

### Requirement: Workbench layout
The main window SHALL show a primary sidebar with Capture, Categories, API Collections, Rewrite Rules, Python Plugins, and Memory Statistics sections, a resizable secondary sidebar, a content area, and a status bar.

#### Scenario: Switching sections
- **WHEN** the user selects a section in the primary sidebar
- **THEN** the secondary sidebar and content area show that section and the previously open section's selection is retained when the user returns

#### Scenario: Collapsing the secondary sidebar
- **WHEN** the user collapses the secondary sidebar
- **THEN** the content area takes the freed space and the sidebar can be expanded again to its previous width

### Requirement: Status bar
The status bar SHALL show the current traffic total and selection count and provide the detail layout toggle, environment information, the GitHub project link, and the update control.

#### Scenario: Selection count
- **WHEN** the user selects rows in the traffic table
- **THEN** the status bar shows the total entry count and the selected count

#### Scenario: Environment information
- **WHEN** the user opens environment information
- **THEN** the app version, Go version, build commit, and platform are shown

### Requirement: Single running instance
FlowLens SHALL run one instance per build mode, and launching it again SHALL restore and focus the existing main window.

#### Scenario: Second launch
- **WHEN** FlowLens is running with its main window hidden and the user launches it again
- **THEN** no second instance remains and the existing main window is shown and focused

### Requirement: Database opens after the instance guard
FlowLens SHALL acquire the single-instance guard before opening its database.

#### Scenario: Second launch does not touch the database
- **WHEN** a second instance starts while the first is running
- **THEN** the second instance exits without opening the database

### Requirement: Main window close behavior
Closing the main window SHALL follow the user's setting: hide to the tray by default, or quit the application.

#### Scenario: Hide to tray
- **WHEN** close behavior is hide to tray and the user closes the main window
- **THEN** settings are saved, the window hides, and capture keeps running

#### Scenario: Quit on close
- **WHEN** close behavior is quit and the user closes the main window
- **THEN** the application begins its normal shutdown

#### Scenario: Tray unavailable
- **WHEN** the tray icon could not be created and the user closes the main window with hide to tray selected
- **THEN** the application begins its normal shutdown instead of leaving a hidden process with no way to reopen it

### Requirement: System tray
FlowLens SHALL provide a tray icon that restores the main window on click and offers a menu with Open Main Window and Close actions in the current language.

#### Scenario: Restoring from the tray
- **WHEN** the user clicks the tray icon while the main window is hidden or minimized
- **THEN** the main window is shown and focused

#### Scenario: Quitting from the tray
- **WHEN** the user chooses Close from the tray menu
- **THEN** the application begins its normal shutdown

### Requirement: Graceful shutdown
On quit, FlowLens SHALL show a shutdown indicator in the main window, save settings, and stop updates, rule loading, global shortcuts, memory monitoring, history maintenance, proxy services, and Python workers before exiting.

#### Scenario: Quit while capturing
- **WHEN** the user quits while the proxy is running with a managed system proxy
- **THEN** capture stops, the managed system proxy is restored unless it was changed externally, and the process exits

#### Scenario: Repeated quit requests
- **WHEN** a quit request arrives while shutdown is already in progress
- **THEN** shutdown continues once and no additional confirmation is shown

### Requirement: Quit guards for unsaved work
FlowLens SHALL NOT quit or restart for an update while unsaved Settings or rewrite-rule drafts exist until the user resolves them.

#### Scenario: Dirty settings at quit
- **WHEN** the user quits with unsaved changes in the Settings window
- **THEN** the Settings window is focused and asks for confirmation, and quitting proceeds only if the user confirms

#### Scenario: Dirty rewrite drafts at quit
- **WHEN** the user quits with unsaved rewrite-rule drafts
- **THEN** FlowLens offers save, discard, and cancel, and cancel keeps the application running

### Requirement: Window geometry persistence
FlowLens SHALL persist the main window's size, position, and maximized state and restore them at launch, keeping the window reachable on the current displays.

#### Scenario: Restoring geometry
- **WHEN** the user resizes the main window and restarts FlowLens
- **THEN** the window opens at the saved size and position

#### Scenario: Saved position on a removed display
- **WHEN** the saved position lies outside every connected display
- **THEN** the window opens at a visible position

### Requirement: Title bar modes
FlowLens SHALL support a custom title bar and the system title bar, with the choice taking effect after restart.

#### Scenario: Custom title bar
- **WHEN** custom title bar mode is active
- **THEN** the window draws its own title bar with minimize, maximize, and close controls on Windows and Linux, keeps native window controls on macOS, and can be dragged by the title bar

#### Scenario: Changing the mode
- **WHEN** the user changes the title bar mode and saves
- **THEN** the current window keeps its mode until FlowLens restarts

### Requirement: Secondary windows
Settings and Software Update SHALL each open in a dedicated window, with at most one of each open.

#### Scenario: Reopening an open window
- **WHEN** the user opens Settings while the Settings window already exists
- **THEN** the existing window is shown and focused

### Requirement: Startup failure reporting
When startup fails after the single-instance lock, FlowLens SHALL keep running and show a startup failure window with the failed step and its error instead of exiting. The window SHALL offer Retry, which runs the whole startup sequence again, Open Log Folder, and Quit. The failure SHALL always be written to the log file, even when saved settings turn logging off.

#### Scenario: Database cannot be opened
- **WHEN** the application database cannot be opened at startup
- **THEN** the error is logged and the startup failure window shows the failed step and its error

#### Scenario: Retry succeeds
- **WHEN** the user selects Retry and startup now succeeds
- **THEN** the main window opens and replaces the startup failure window

### Requirement: Shutdown indicator content
While FlowLens shuts down, the main window SHALL show a non-dismissible indicator with the shutdown title, the description "Saving data, please wait…" in the current language, and a spinner. It SHALL block input to the workbench until the process exits.

#### Scenario: Quitting from the tray
- **WHEN** the user quits FlowLens from the tray while data is being saved
- **THEN** the main window shows the shutdown title, the saving description, and a spinner, and ignores clicks on the workbench
