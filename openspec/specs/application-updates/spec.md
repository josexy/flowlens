# application-updates Specification

## Purpose
Defines how FlowLens checks for, verifies, and applies new versions, and how users are directed to manual installation when self-update is not possible.

## Requirements

### Requirement: Update checks
The Software Update window SHALL let the user check GitHub Releases and show whether the installation is up to date or a newer version is available with its version, date, and release notes.

#### Scenario: Newer version available
- **WHEN** a newer release exists
- **THEN** the version, publish date, and release notes formatted from Markdown are shown

#### Scenario: Up to date
- **WHEN** no newer release exists
- **THEN** the window states that FlowLens is up to date

#### Scenario: Check failure
- **WHEN** the check fails because of a network error
- **THEN** the error is shown and the user can try again

### Requirement: Update eligibility
FlowLens SHALL determine whether the current installation can update itself and otherwise offer manual installation.

#### Scenario: Install location is not writable
- **WHEN** FlowLens is installed by a package manager or in a location it cannot write to
- **THEN** the window explains that manual installation is required and offers to open the release page

#### Scenario: Development build
- **WHEN** FlowLens is a development build
- **THEN** in-app update is unavailable and no update is applied

### Requirement: Signed update installation
FlowLens SHALL install an update only after its signature has been verified against the public key built into the application, and SHALL report success only when installation returns successfully.

#### Scenario: Valid update
- **WHEN** a signed update is downloaded, verified, and installed
- **THEN** the update is ready for the next launch and the window offers Restart now and Restart later if the application remains open

#### Scenario: Tampered update
- **WHEN** a downloaded update fails signature verification
- **THEN** it is not installed, the error is shown, and the running application is unaffected

### Requirement: Update progress
The update window SHALL show checking or one installation operation with measured download progress. It SHALL NOT simulate verification or preparation phases with timers. Closing the window SHALL leave the operation running; cancellation SHALL be a separate action available during checks and incomplete downloads.

#### Scenario: Canceling a download
- **WHEN** the user cancels while download cancellation is available
- **THEN** cancellation is requested and awaited, and no automatic restart occurs
- **AND** an installation that nevertheless finishes successfully remains ready for a later restart

#### Scenario: Continuing in the background
- **WHEN** the user hides the window during installation
- **THEN** the window closes, the operation continues, and the status bar continues to show update state

#### Scenario: Closing during verification
- **WHEN** the user closes the window after all reported bytes have downloaded but installation is still running
- **THEN** the window closes without interrupting installation and cancellation is unavailable

#### Scenario: Closing during a download
- **WHEN** the user closes the window while checking or downloading
- **THEN** the window closes without canceling the operation or requiring a confirmation dialog

#### Scenario: Delta fallback
- **WHEN** installation falls back from a complete delta download to downloading the full archive
- **THEN** measured progress restarts and cancellation becomes available again

#### Scenario: Unknown download size
- **WHEN** no positive download total is available
- **THEN** progress is indeterminate and no completion percentage is fabricated

### Requirement: One update operation at a time
FlowLens SHALL run at most one update operation and SHALL ignore results from superseded operations.

#### Scenario: Repeated check
- **WHEN** the user requests a check while one is running
- **THEN** no second operation starts

### Requirement: Guarded restart
Manual and automatic update restarts SHALL pass the same unsaved-work guards and normal shutdown as quitting. A declined restart SHALL leave the installed update ready without repeatedly prompting; a failed restart SHALL be retryable without reinstalling.

#### Scenario: Restart with unsaved settings
- **WHEN** a manual or automatic restart encounters unsaved settings
- **THEN** the user is asked to resolve them and the restart proceeds only after confirmation

#### Scenario: Restart failure
- **WHEN** the restart cannot be performed
- **THEN** the error is shown and the application keeps running
- **AND** retry requests another guarded restart without downloading again

#### Scenario: Update window closed before restart
- **WHEN** the update is ready and the Update window has been closed
- **THEN** the restart remains available from the status bar

### Requirement: Status bar update state
The status bar SHALL reflect checking, available, installing, ready, and error update states and open the Software Update window.

#### Scenario: Update ready
- **WHEN** an update has been installed for the next launch
- **THEN** the status bar offers Restart now

### Requirement: Shutdown during update
Quitting during an update operation SHALL cancel it and wait for it to stop before exit.

#### Scenario: Quit during download
- **WHEN** the user quits while an update is downloading
- **THEN** the download is canceled and shutdown completes without leaving a partial install

### Requirement: Restart after installation choice
Before installation, users SHALL be able to choose whether to restart automatically when it completes. This session-only choice SHALL default on, survive closing the update window, and be captured when installation starts. Successful completion SHALL request one guarded restart when selected, even with the update window closed.

#### Scenario: Automatic restart selected
- **WHEN** the user installs with automatic restart selected and installation succeeds
- **THEN** one guarded restart is requested without requiring another button click

#### Scenario: Automatic restart disabled
- **WHEN** installation succeeds with automatic restart disabled
- **THEN** the application keeps running and offers Restart now or Restart later

#### Scenario: Failed or canceled installation
- **WHEN** installation fails, is canceled, or completes while application shutdown is underway
- **THEN** no automatic restart is requested

#### Scenario: Restart confirmation declined
- **WHEN** the user declines an unsaved-work confirmation after successful automatic installation
- **THEN** the application keeps running, the update remains ready, and reopening the window does not trigger another automatic restart
