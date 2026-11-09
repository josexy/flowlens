# Spec Delta

## Purpose

Defines how saved traffic is browsed, categorized, imported, and exported, and how existing history files remain usable.

## ADDED Requirements

### Requirement: Saved history browsing
The Capture section SHALL list saved histories by alias or creation date, and selecting one SHALL show its traffic in the same table and detail panel as live capture.

#### Scenario: Opening a history
- **WHEN** the user selects a saved history
- **THEN** its entries are shown with the same filters, columns, and detail views as live capture

#### Scenario: Refreshing
- **WHEN** the user activates refresh
- **THEN** the list reflects the histories currently on disk

### Requirement: History deletion
The user SHALL be able to delete one history or clear all histories after confirmation.

#### Scenario: Clear all
- **WHEN** the user confirms Clear All
- **THEN** readable saved histories are removed and the list updates in every window

#### Scenario: Unsupported history file
- **WHEN** a history file has an unsupported or unknown version
- **THEN** it is skipped without being deleted

### Requirement: Existing data compatibility
FlowLens SHALL read existing history files, settings, API Collections, and plugins from the existing `FlowLens` data directory without conversion.

#### Scenario: Upgrading from the previous build
- **WHEN** FlowLens starts on a machine with data written by the previous Wails build
- **THEN** existing HBIN v1 and v2 histories, settings, API Collections, rewrite rules, and plugins are available

#### Scenario: New history format
- **WHEN** FlowLens saves a new history
- **THEN** it is written as HBIN v2

### Requirement: Traffic categories
The Categories section SHALL group the current capture or selected history by host and by process with a structure tree, with search, and filter the traffic table by the selected node.

#### Scenario: Selecting a host
- **WHEN** the user selects a host node
- **THEN** the table shows only that host's entries and indicates the capture or history source

#### Scenario: Searching categories
- **WHEN** the user searches and nothing matches
- **THEN** an empty state for no matching domains or processes is shown

### Requirement: HAR import
The user SHALL be able to import HAR files through a file picker or by dropping `.har` files onto the capture or history traffic table, including its empty state.

#### Scenario: Importing several files
- **WHEN** the user imports three HAR files
- **THEN** they are processed sequentially with progress shown, each successful file becomes a separate history named after it, and the first successful result opens

#### Scenario: Partial failure
- **WHEN** one of several files is invalid
- **THEN** the other files are imported and the result reports imported files and entries, skipped entries, unavailable bodies, and failed files

#### Scenario: Canceled picker
- **WHEN** the user cancels the file picker
- **THEN** nothing happens

#### Scenario: Drop indication
- **WHEN** `.har` files are dragged over the traffic table
- **THEN** the table indicates that dropping imports them

### Requirement: Traffic export
Capture and history menus SHALL export entries as CSV and HAR, and row menus SHALL also export Request, Request Headers, Request Body, Response, Response Headers, Response Body, and Request + Response.

#### Scenario: Exporting selected entries as HAR
- **WHEN** the user exports selected entries as HAR
- **THEN** a destination is requested, entries are written in ID order, and the result reports exported, skipped, and missing-body counts

#### Scenario: Interrupted export
- **WHEN** an export fails while writing
- **THEN** any existing file at the destination remains intact

#### Scenario: Format unavailable for the selection
- **WHEN** no selected entry can provide the chosen format, such as a response body on a failed exchange
- **THEN** that format is shown as unavailable

#### Scenario: Export scope is stable during capture
- **WHEN** new traffic arrives during an export
- **THEN** only the entries chosen when the export began are written

### Requirement: Export safety notice
Exports SHALL NOT redact content automatically.

#### Scenario: Sensitive headers
- **WHEN** an entry with an `Authorization` header is exported
- **THEN** the exported file contains the header unchanged
