# Spec Delta

## ADDED Requirements

### Requirement: Shutdown indicator content
While FlowLens shuts down, the main window SHALL show a non-dismissible indicator with the shutdown title, the description "Saving data, please wait…" in the current language, and a spinner. It SHALL block input to the workbench until the process exits.

#### Scenario: Quitting from the tray
- **WHEN** the user quits FlowLens from the tray while data is being saved
- **THEN** the main window shows the shutdown title, the saving description, and a spinner, and ignores clicks on the workbench
