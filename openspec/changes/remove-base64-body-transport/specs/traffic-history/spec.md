# Spec Delta

## MODIFIED Requirements

### Requirement: Existing data compatibility
FlowLens SHALL read existing history files, settings, API Collections, and plugins from the existing `FlowLens` data directory without conversion.

#### Scenario: Upgrading from the previous build
- **WHEN** FlowLens starts on a machine with data written by the previous Wails build
- **THEN** existing HBIN v1 and v2 histories, settings, API Collections, rewrite rules, and plugins are available

#### Scenario: New history format
- **WHEN** FlowLens saves a new history, from a capture or from a HAR import
- **THEN** it is written as HBIN v3

#### Scenario: Binary WebSocket message from an older history
- **WHEN** the user opens a binary WebSocket message in a history written as HBIN v1 or v2
- **THEN** its hex view, size, and saved file hold the message's original bytes, exactly as for the same message in a v3 history

#### Scenario: Unknown history version
- **WHEN** the history directory holds a file with a version newer than v3
- **THEN** the file is skipped and is not deleted

#### Scenario: Exporting a v3 history
- **WHEN** the user exports a v3 history as HAR or as traffic files
- **THEN** export is offered and produces the same content it does for a v2 history
