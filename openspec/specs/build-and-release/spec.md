# build-and-release Specification

## Purpose
Defines how developers build, verify, and release FlowLens without a JavaScript toolchain, and which artifacts each release provides.

## Requirements

### Requirement: Development toolchain
Compiling FlowLens and running ordinary Go tests SHALL require no C or Objective-C compiler on supported desktop platforms. The project SHALL use Go, Task entry points, and the pinned MyGo CLI, without Node.js, npm, or generated web bindings. macOS packaging and signing tools and optional race-detector prerequisites SHALL be documented separately from application compilation.

#### Scenario: Fresh checkout
- **WHEN** a developer with Go and Task runs the build task in a fresh checkout
- **THEN** the application compiles without installing a JavaScript or C/Objective-C toolchain

#### Scenario: macOS packaging tools
- **WHEN** a developer creates a macOS application bundle or disk image
- **THEN** the documented platform packaging and signing tools remain usable with cgo disabled
- **AND** any packaging-tool failure is distinguishable from a requirement to compile application C/Objective-C sources

### Requirement: Task entry points
The Taskfile SHALL provide tasks for development run, build, packaging, tests, UI lifecycle checks, Go lint and fixes, modernization checks, and catalog generation and checking.

#### Scenario: Listing tasks
- **WHEN** a developer runs `task --list`
- **THEN** `dev`, `build`, `package`, `run`, `test`, `lint:go`, `lint:go:fix`, `modernize`, `modernize:fix`, `vet:ui`, `i18n:generate`, and `i18n:check` are listed with descriptions

#### Scenario: Full verification
- **WHEN** a developer runs the verification task on a machine with no display
- **THEN** formatting, lint, UI lifecycle checks, catalog checks, and tests run and report failures with a non-zero exit code

### Requirement: Cgo build modes
FlowLens SHALL build with `CGO_ENABLED=0` for all supported Windows, Linux, and macOS targets. Ordinary development, verification, and release entry points SHALL select this mode. macOS builds SHALL retain compatibility with macOS 13.0 and support arm64 and amd64, including a universal application. Optional race checks SHALL declare their own toolchain prerequisites separately.

#### Scenario: Cross-compiling from Linux
- **WHEN** the application is built from Linux with `CGO_ENABLED=0` for Windows amd64, Linux amd64, and Linux arm64
- **THEN** every build succeeds

#### Scenario: Building for macOS
- **WHEN** the application is built on a Mac with `CGO_ENABLED=0` for macOS arm64 and universal
- **THEN** every build succeeds and the universal application contains both arm64 and amd64 architectures

#### Scenario: Cross-compiling macOS executables
- **WHEN** a non-macOS host compiles the application executable for darwin/arm64 and darwin/amd64 with cgo disabled
- **THEN** both executable builds succeed without a macOS C compiler or SDK
- **AND** this result does not substitute for native runtime or package verification

#### Scenario: Default build configuration
- **WHEN** a developer or workflow runs ordinary development, test, verification, or release commands through the repository entry points
- **THEN** cgo is disabled on macOS as well as Windows and Linux

#### Scenario: Minimum supported macOS version
- **WHEN** a packaged application built with cgo disabled runs on macOS 13.0 on a supported architecture
- **THEN** it starts and provides the required native features without raising the minimum OS version

### Requirement: macOS native features in release builds
macOS release and development builds with cgo disabled SHALL provide process attribution with application metadata and icons, system upstream-proxy discovery, managed system proxy control, and process physical-footprint sampling. Disabling cgo SHALL NOT substitute unsupported implementations, omit these features, or change their existing user-visible semantics.

#### Scenario: Release build with cgo disabled
- **WHEN** a macOS release build runs with cgo disabled
- **THEN** it succeeds with all required native features included

#### Scenario: Process attribution on macOS
- **WHEN** a macOS build with cgo disabled attributes a direct local connection and the operating system permits access
- **THEN** it resolves the process identity and available application metadata and icon asynchronously
- **AND** it preserves process-instance identity across PID reuse and never blocks proxy forwarding

#### Scenario: Process metadata or icon unavailable
- **WHEN** a process exits, access is denied, or its metadata or icon cannot be read
- **THEN** existing unavailable or partial-result behavior is preserved without fabricating identity or attaching a stale icon to another process
- **AND** when an attributed process exits before its icon is ready, the provider can load the icon from the recorded bundle or executable path

#### Scenario: Attribution canceled during shutdown
- **WHEN** the application shuts down with attribution or icon work queued
- **THEN** cancellation completes without waiting indefinitely for the UI thread and late results do not update closed or replaced state

#### Scenario: System upstream proxy on macOS
- **WHEN** no valid environment proxy overrides readable macOS proxy settings in a build with cgo disabled
- **THEN** upstream selection retains the existing HTTP, HTTPS, then SOCKS preference and URL normalization

#### Scenario: System proxy on macOS
- **WHEN** a macOS build with cgo disabled enables the managed system proxy in HTTP/HTTPS or SOCKS5 mode
- **THEN** the selected network service's settings are snapshotted and updated through the existing authorization flow
- **AND** disabling management or normal shutdown restores the snapshot only while FlowLens still owns the current configuration

#### Scenario: Proxy configuration changes externally
- **WHEN** another program changes the managed service's proxy configuration before an apply or restore transaction
- **THEN** FlowLens reports the conflict and leaves the external configuration intact, including when only an auxiliary setting changed

#### Scenario: Proxy update fails
- **WHEN** authorization, preference locking, committing, or applying a proxy update fails
- **THEN** FlowLens reports failure without falsely claiming that management is active
- **AND** recovery does not overwrite settings changed by another program

#### Scenario: Physical footprint on macOS
- **WHEN** a macOS build with cgo disabled samples its own process memory successfully
- **THEN** Memory uses the physical-footprint measure corresponding to Activity Monitor's Memory column
- **AND** it excludes Python Workers and does not substitute resident memory or Go heap usage

#### Scenario: Native memory sample unavailable
- **WHEN** the operating system cannot supply a valid physical-footprint sample
- **THEN** Memory remains unavailable and the chart shows the existing gap while Go runtime statistics remain usable

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
CI SHALL run `go test ./...` with cgo disabled on Linux and macOS for pull requests and pushes to the main branch. The macOS suite SHALL include the AppKit main-loop test. Release verification and packaging SHALL use the same no-cgo policy.

#### Scenario: Failing test
- **WHEN** a pull request introduces a failing test or a lifecycle check failure
- **THEN** CI fails

#### Scenario: macOS regression coverage
- **WHEN** macOS verification runs with cgo disabled
- **THEN** process-identity, loopback-attribution, proxy-binding, and physical-footprint checks exercise functional implementations
- **AND** interactive authorization and AppKit checks have explicit native validation evidence before the migration is accepted

#### Scenario: Supported architecture coverage
- **WHEN** this migration is accepted for release
- **THEN** both macOS architectures have native ABI and feature-test evidence, and the universal package has been checked
- **AND** cross-compilation alone is not reported as successful runtime verification

#### Scenario: Required feature loses no-cgo support
- **WHEN** a change makes a required native feature unavailable solely because cgo is disabled
- **THEN** verification fails even if the executable still compiles

### Requirement: Developer documentation
The repository documentation SHALL describe the MyGo stack, development commands, and module ownership, with no remaining Wails or frontend instructions.

#### Scenario: Following the setup instructions
- **WHEN** a developer follows the README from a fresh checkout
- **THEN** the commands succeed as documented
