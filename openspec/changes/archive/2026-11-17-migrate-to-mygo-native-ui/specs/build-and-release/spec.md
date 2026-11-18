# Spec Delta

## Purpose

Defines how developers build, verify, and release FlowLens without a JavaScript toolchain, and which artifacts each release provides.

## ADDED Requirements

### Requirement: Development toolchain
Building and testing FlowLens SHALL require only Go, Task, and the pinned MyGo CLI, plus Xcode Command Line Tools on macOS, and SHALL NOT require Node.js, npm, or generated web bindings.

#### Scenario: Fresh checkout
- **WHEN** a developer with Go and Task runs the build task in a fresh checkout
- **THEN** the application builds without installing a JavaScript toolchain

### Requirement: Task entry points
The Taskfile SHALL provide tasks for development run, build, packaging, tests, UI lifecycle checks, Go lint and fixes, modernization checks, and catalog generation and checking.

#### Scenario: Listing tasks
- **WHEN** a developer runs `task --list`
- **THEN** `dev`, `build`, `package`, `run`, `test`, `lint:go`, `lint:go:fix`, `modernize`, `modernize:fix`, `vet:ui`, `i18n:generate`, and `i18n:check` are listed with descriptions

#### Scenario: Full verification
- **WHEN** a developer runs the verification task on a machine with no display
- **THEN** formatting, lint, UI lifecycle checks, catalog checks, and tests run and report failures with a non-zero exit code

### Requirement: Cgo build modes
FlowLens SHALL build with cgo disabled for Windows and Linux targets and with cgo enabled for macOS targets.

#### Scenario: Cross-compiling from Linux
- **WHEN** the application is built from Linux with `CGO_ENABLED=0` for Windows amd64, Linux amd64, and Linux arm64
- **THEN** every build succeeds

#### Scenario: Building for macOS
- **WHEN** the application is built on a Mac with `CGO_ENABLED=1` for macOS arm64 and universal
- **THEN** every build succeeds and the universal application contains both architectures

### Requirement: macOS native features in release builds
macOS release builds SHALL include process attribution with icons and the managed system proxy.

#### Scenario: Release build with cgo disabled
- **WHEN** a macOS release build runs with cgo disabled
- **THEN** the build fails rather than producing an application without these features

#### Scenario: Process attribution on macOS
- **WHEN** a macOS build attributes a local connection
- **THEN** the process identity and icon are available and the provider does not report that it is unavailable

#### Scenario: System proxy on macOS
- **WHEN** a macOS build enables the managed system proxy
- **THEN** the system proxy is applied and later restored under the same rules as other platforms

### Requirement: Release artifacts
Releases SHALL provide a Windows x64 NSIS installer, macOS Apple Silicon and universal disk images, and Linux x64 Debian and tar archives with a checksum file.

#### Scenario: Building a release
- **WHEN** the release workflow runs for a version tag
- **THEN** those artifacts are published to a draft GitHub Release

### Requirement: Signed update manifests
Release builds SHALL publish update archives and per-platform manifests signed with the project's update key.

#### Scenario: Missing signing key
- **WHEN** the release workflow runs without the update signing key
- **THEN** the release fails before uploading rather than publishing a release that installed applications cannot verify

### Requirement: Version metadata
The application version SHALL be maintained only in `mygo.json`, embedded by MyGo, and read at runtime through `mygo.App.Version()`. Release preparation SHALL verify that the requested version matches this configuration before building or uploading.

#### Scenario: Setting the version
- **WHEN** a developer sets `version` in `mygo.json` to `1.2.3` and builds with MyGo
- **THEN** the application environment details, updater, HAR creator metadata, and packaged artifacts use `1.2.3`

#### Scenario: Checking the version
- **WHEN** a release tag or manually supplied version differs from `mygo.json`
- **THEN** release preparation fails without changing files, building, or uploading

### Requirement: Continuous integration
CI SHALL run the verification task on every push and pull request and SHALL build for all supported platforms.

#### Scenario: Failing test
- **WHEN** a pull request introduces a failing test or a lifecycle check failure
- **THEN** CI fails

### Requirement: Developer documentation
The repository documentation SHALL describe the MyGo stack, development commands, and module ownership, with no remaining Wails or frontend instructions.

#### Scenario: Following the setup instructions
- **WHEN** a developer follows the README from a fresh checkout
- **THEN** the commands succeed as documented
