# traffic-history Specification

## Purpose
Defines how saved traffic is browsed, categorized, imported, and exported, and how existing history files remain usable.

## Requirements

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

### Requirement: Category section headers
Each category section header, such as Hosts, Processes, and Structure, SHALL be a full-width 32 px band on the sidebar header tint, with semibold muted text, a bottom border, and a top border after the first section. Its chevron SHALL sit at the end of the row.

#### Scenario: Collapsing a section
- **WHEN** the user clicks the Hosts section header
- **THEN** the section collapses, and its end-aligned chevron points right

### Requirement: Category filter check marks
A selected host or process filter SHALL show an accent check mark at the end of its row, beside its 20 px globe or process icon. Rows SHALL NOT show entry counts.

#### Scenario: Selecting two hosts
- **WHEN** the user selects two hosts in the category tree
- **THEN** both rows show an accent check mark, and the traffic list shows entries for either host

### Requirement: Structure tree rows
Structure folders SHALL show a 20 px folder icon and an end-aligned chevron that rotates when the folder opens. Rows SHALL be rounded and SHALL show a hover background.

#### Scenario: Expanding a folder
- **WHEN** the user expands a path segment in the structure tree
- **THEN** its chevron rotates, its icon becomes an open folder, and its children appear below it

### Requirement: History memory release
Decoded history entries and bodies SHALL be retained only for the open history. Closing or replacing it SHALL release its details, viewer caches, filters, table rows, and categories. The service MAY cache compact entry indexes for at most four recently accessed histories; listing, refreshing, and startup cleanup SHALL NOT load additional entry indexes.

#### Scenario: Closing the history tab
- **WHEN** the user closes the history tab of a history with 100000 entries
- **THEN** its entries, details, filter results, table rows, and category tree are released and the capture table is shown

#### Scenario: Many saved histories
- **WHEN** 100 saved histories of 50000 entries each exist and the user starts FlowLens or refreshes the history list
- **THEN** no history's entries or entry index are kept in memory until that history is opened or exported

#### Scenario: Reopening a closed history
- **WHEN** the user opens a history again after closing its tab
- **THEN** it shows the same entries and details as before

#### Scenario: Opening another history
- **WHEN** the user opens a second history while the first is open
- **THEN** the first history's decoded entries, bodies, and viewer data are released, while its compact index may remain in the bounded cache

#### Scenario: Closing after viewing a large body
- **WHEN** the user closes a history after viewing its body, Raw, Hex, SSE, WebSocket, or image details
- **THEN** the hidden detail viewers retain none of those payloads, and pending decoding or parsing results cannot repopulate them

#### Scenario: Accessing more than four saved histories
- **WHEN** opening or exporting another history would cache a fifth entry index
- **THEN** the least recently used index is evicted and the cache contains at most four indexes

### Requirement: Lightweight history browsing
An opened history SHALL retain only the fields needed for its complete table, filters, categories, and action availability. Full headers, trailers, TLS details, certificates, and bodies SHALL be loaded on demand for inspection or actions. All rows SHALL remain searchable and sortable, and HBIN v1-v3 reading, export ordering, and exact stored content SHALL remain unchanged.

#### Scenario: Opening a large history
- **WHEN** a saved history contains many records with large headers or certificates
- **THEN** all rows are available without retaining those full details for every record

#### Scenario: Inspecting and acting on a summarized row
- **WHEN** the user inspects a row, copies cURL, edits its request, resends it, or exports it
- **THEN** the operation uses the complete stored record with duplicate headers, casing, order, trailers, metrics, process information, and body availability preserved

#### Scenario: Replacing or closing an in-progress history read
- **WHEN** the user changes the selected history or detail, closes the history, or shuts down during a read
- **THEN** obsolete reads are canceled and their results cannot restore discarded data

#### Scenario: Detail read fails
- **WHEN** a selected record can no longer be read
- **THEN** no incomplete summary is presented as a complete record or used to construct a request, and the existing error-reporting path reports the failure

### Requirement: Bounded history scan allocation
Opening a history SHALL skip details not needed by its rows without allocations proportional to those fields' encoded payload sizes. The list SHALL preserve its existing fields and actions, and complete stored details SHALL remain available on demand. Selective reading SHALL preserve HBIN v1-v3 compatibility, validation, cancellation, and file contents.

#### Scenario: Large cookies and certificates
- **WHEN** a history contains large Cookie, Set-Cookie, custom header, trailer, certificate, or unused TLS fields
- **THEN** loading its list does not materialize those payloads as strings, byte arrays, or object collections
- **AND** opening a detail or performing an action still reads the complete stored content

#### Scenario: List fields and older histories
- **WHEN** a valid HBIN v1, v2, or v3 history is opened
- **THEN** all existing rows, first Content-Type values, protocols including TCP selected ALPN, flags, metrics, process information, sorting, filtering, and action availability are preserved
- **AND** full reads preserve header order, duplicates, casing, empty versus missing data, and unknown metrics

#### Scenario: Invalid skipped field
- **WHEN** a skipped field is truncated or a header block has an invalid flag or excessive field count
- **THEN** loading reports the existing decoding failure instead of accepting an incomplete record

#### Scenario: Cancellation while skipping
- **WHEN** a history read is canceled during a large skipped field
- **THEN** the read stops through the existing cancellation path and obsolete results cannot repopulate the list
