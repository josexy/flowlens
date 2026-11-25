# Spec Delta

## MODIFIED Requirements

### Requirement: Development toolchain
Compiling FlowLens and running ordinary Go tests SHALL require no C or Objective-C compiler on supported desktop platforms. Building, verifying, and packaging the application SHALL use Go, Task, and the pinned MyGo CLI, without Node.js, npm, or generated web bindings; only the optional project website uses Node.js. macOS packaging and signing tools and optional race-detector prerequisites SHALL be documented separately from application compilation.

#### Scenario: Fresh checkout
- **WHEN** a developer with Go and Task runs the build task in a fresh checkout
- **THEN** the application compiles without installing a JavaScript or C/Objective-C toolchain

#### Scenario: macOS packaging tools
- **WHEN** a developer creates a macOS application bundle or disk image
- **THEN** the documented platform packaging and signing tools remain usable with cgo disabled
- **AND** any packaging-tool failure is distinguishable from a requirement to compile application C/Objective-C sources

#### Scenario: Full verification without Node.js
- **WHEN** a developer without Node.js runs the full verification task, or CI verifies the application
- **THEN** no website task runs and no JavaScript toolchain is needed

### Requirement: Developer documentation
The repository documentation SHALL describe the MyGo stack, development commands, and module ownership, with no remaining Wails or application frontend instructions. Instructions for the optional project website SHALL be kept separate from the application's setup and build instructions.

#### Scenario: Following the setup instructions
- **WHEN** a developer follows the README from a fresh checkout
- **THEN** the commands succeed as documented

#### Scenario: Website instructions
- **WHEN** a developer looks for how to build or deploy the project website
- **THEN** the website's own README and the building guide describe it, and the application setup steps do not depend on it
