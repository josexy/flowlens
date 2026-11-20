# Spec Delta

## MODIFIED Requirements

### Requirement: Development toolchain
Compiling FlowLens and running ordinary Go tests SHALL require no C or Objective-C compiler on supported desktop platforms. The project SHALL use Go, Task entry points, and the pinned MyGo CLI, without Node.js, npm, or generated web bindings. macOS packaging and signing tools and optional race-detector prerequisites SHALL be documented separately from application compilation.

#### Scenario: Fresh checkout
- **WHEN** a developer with Go and Task runs the build task in a fresh checkout
- **THEN** the application compiles without installing a JavaScript or C/Objective-C toolchain

#### Scenario: macOS packaging tools
- **WHEN** a developer creates a macOS application bundle or disk image
- **THEN** the documented platform packaging and signing tools remain usable with cgo disabled
- **AND** any packaging-tool failure is distinguishable from a requirement to compile application C/Objective-C sources

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

### Requirement: Continuous integration
CI SHALL verify pushes and pull requests with cgo disabled, including native macOS regression tests and executable compile checks for supported targets. Release verification and packaging SHALL use the same no-cgo policy. Platform feature coverage SHALL distinguish native execution from cross-compilation and SHALL NOT pass solely by testing unavailable stubs.

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
