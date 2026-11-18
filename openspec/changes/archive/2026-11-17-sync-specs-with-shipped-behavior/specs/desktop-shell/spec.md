# Spec Delta

## REMOVED Requirements

### Requirement: Restore Initial State
**Reason**: Settings > Storage no longer offers Restore Initial State; commit `52dae83` removed the feature.
**Migration**: Use Settings > Storage cleanup to remove caches and history, or delete the application data directory manually.

### Requirement: Restore Initial State startup progress
**Reason**: Without Restore Initial State there is no pending reset to clean up at startup.
**Migration**: None.

## MODIFIED Requirements

### Requirement: Startup failure reporting
When startup fails after the single-instance lock, FlowLens SHALL keep running and show a startup failure window with the failed step and its error instead of exiting. The window SHALL offer Retry, which runs the whole startup sequence again, Open Log Folder, and Quit. The failure SHALL always be written to the log file, even when saved settings turn logging off.

#### Scenario: Database cannot be opened
- **WHEN** the application database cannot be opened at startup
- **THEN** the error is logged and the startup failure window shows the failed step and its error

#### Scenario: Retry succeeds
- **WHEN** the user selects Retry and startup now succeeds
- **THEN** the main window opens and replaces the startup failure window
