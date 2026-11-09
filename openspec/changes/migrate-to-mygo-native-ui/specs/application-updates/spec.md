# Spec Delta

## Purpose

Defines how FlowLens checks for, verifies, and applies new versions, and how users are directed to manual installation when self-update is not possible.

## ADDED Requirements

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
FlowLens SHALL install an update only after its signature has been verified against the public key built into the application.

#### Scenario: Valid update
- **WHEN** a signed update is downloaded and verified
- **THEN** the update is staged and the window offers Restart now and Restart later

#### Scenario: Tampered update
- **WHEN** a downloaded update fails signature verification
- **THEN** it is not installed, the error is shown, and the running application is unaffected

### Requirement: Update progress
While updating, the window SHALL show the Check, Download, Verify, and Prepare phases and download progress, and SHALL allow cancellation during checking and downloading.

#### Scenario: Canceling a download
- **WHEN** the user cancels during download
- **THEN** the download stops and no update is staged

#### Scenario: Continuing in the background
- **WHEN** the user chooses to continue in the background during download
- **THEN** the window closes and the status bar continues to show update state

#### Scenario: Closing during verification
- **WHEN** the user closes the window during verification or preparation
- **THEN** the window stays open and explains that the step must finish

#### Scenario: Closing during a download
- **WHEN** the user closes the window while checking or downloading
- **THEN** the window offers to continue in the background or cancel

### Requirement: One update operation at a time
FlowLens SHALL run at most one update operation and SHALL ignore results from superseded operations.

#### Scenario: Repeated check
- **WHEN** the user requests a check while one is running
- **THEN** no second operation starts

### Requirement: Guarded restart
Restarting to apply an update SHALL pass the same unsaved-work guards and normal shutdown as quitting.

#### Scenario: Restart with unsaved settings
- **WHEN** the user chooses Restart now with unsaved settings
- **THEN** the user is asked to resolve them and the restart proceeds only after confirmation

#### Scenario: Restart failure
- **WHEN** the restart cannot be performed
- **THEN** the error is shown and the application keeps running

#### Scenario: Update window closed before restart
- **WHEN** the update is ready and the Update window has been closed
- **THEN** the restart remains available from the status bar

### Requirement: Status bar update state
The status bar SHALL reflect checking, available, ready, and error update states and open the Software Update window.

#### Scenario: Update ready
- **WHEN** an update has been staged
- **THEN** the status bar offers Restart and update

### Requirement: Shutdown during update
Quitting during an update operation SHALL cancel it and wait for it to stop before exit.

#### Scenario: Quit during download
- **WHEN** the user quits while an update is downloading
- **THEN** the download is canceled and shutdown completes without leaving a partial install
