# traffic-history Specification

## Purpose
Defines how saved traffic is browsed, categorized, imported, and exported, and how existing history files remain usable.

## Requirements

### Requirement: Saved history browsing
The Capture section SHALL list saved histories by alias or creation date, and selecting one SHALL show its traffic in the same table and detail panel as live capture. Saved-session visual construction SHALL be limited to visible rows and bounded overscan, while every listed session remains reachable by scrolling and keyboard navigation.

#### Scenario: Opening a history
- **WHEN** the user selects a saved history
- **THEN** its entries are shown with the same filters, columns, and detail views as live capture

#### Scenario: Refreshing
- **WHEN** the user activates refresh
- **THEN** the list reflects the histories currently on disk

#### Scenario: Many saved sessions
- **WHEN** the Capture sidebar contains 1000 saved histories
- **THEN** it builds only visible session rows and bounded overscan without loading entries or entry indexes for unopened histories
- **AND** pointer and keyboard navigation can reach and activate a session near the end of the list

#### Scenario: Refresh changes row positions
- **WHEN** a refresh, deletion, or import changes session positions while the list is scrolled or focused
- **THEN** the opened session and valid focus target continue to refer to their original session identities
- **AND** index changes alone do not open another session or redirect a pending action

#### Scenario: A menu anchor leaves view
- **WHEN** an open saved-session row menu loses its anchor because that session scrolls out of view or is removed
- **THEN** the menu closes and cannot act on a different row at the same position

#### Scenario: Empty or resized session list
- **WHEN** the list becomes empty, its viewport is resized, or interface text is enlarged
- **THEN** loading and empty states remain usable and all nonempty rows, including the last one, remain reachable with vertically readable text and controls in either theme and language

#### Scenario: Replacement history fails to load
- **WHEN** history A is open and loading history B fails
- **THEN** A remains available with its previous data rather than being replaced with partial B results
- **AND** the existing failure-reporting and loading-indicator behavior is preserved

#### Scenario: Candidate history has duplicate row identities
- **WHEN** a candidate history returns multiple records with the same row identity
- **THEN** loading reports a failure and keeps the previous history available instead of publishing ambiguous selections
- **AND** the saved file is not rewritten

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
The Categories section SHALL group the current capture or selected history by host and by process with a structure tree, with search, and filter the traffic table by the selected node. Category results SHALL update whenever their source, applicable filters, search, or language changes, including changes outside the currently selected subset. Unchanged redraws SHALL reuse previously derived category results.

#### Scenario: Selecting a host
- **WHEN** the user selects a host node
- **THEN** the table shows only that host's entries and indicates the capture or history source

#### Scenario: Searching categories
- **WHEN** the user searches and nothing matches
- **THEN** an empty state for no matching domains or processes is shown

#### Scenario: Base data changes outside the selected subset
- **WHEN** source data changes the available host or process categories without changing the table's currently selected subset
- **THEN** the category choices reflect the new data after the normal refresh interval
- **AND** existing valid selections remain applied

#### Scenario: Repeated unchanged redraws
- **WHEN** a large history's category sidebar redraws without relevant source, filter, search, or language changes
- **THEN** it reuses the current category summaries and prepared tree/list data while constructing only the controls needed for the viewport

#### Scenario: A table filter temporarily hides structure nodes
- **WHEN** a table search or tab hides expanded structure nodes while Categories is visible, and the filter is then cleared
- **THEN** the nodes return with their previous expansion

#### Scenario: Search or language changes
- **WHEN** the user changes category search or the interface language with active host or process filters
- **THEN** visible labels and matching choices update without discarding selected filters hidden by search
- **AND** returning to an empty search restores the complete applicable category choices

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

### Requirement: Staged history inspection
History inspection SHALL display complete metadata without waiting for body decoding. Bodies SHALL load when Raw, Body, or WebSocket inspection requires them. Complete request/action inputs, HBIN v1-v3 compatibility, exact content, cancellation, and selection cleanup SHALL remain intact.

#### Scenario: Inspecting headers with a large body
- **WHEN** the user opens a history entry in Overview or Headers
- **THEN** complete metadata is available without loading its body
- **AND** switching to a body-dependent view starts at most one payload read for the selection

#### Scenario: Body read fails
- **WHEN** body decoding fails after metadata is available
- **THEN** the readable metadata remains visible and the existing error path reports the failure
- **AND** redraws do not repeatedly restart the failed read; closing and reopening permits another attempt

#### Scenario: Superseded read
- **WHEN** the selected record changes, details close, or the history closes during either read stage
- **THEN** obsolete work is canceled and late results cannot repopulate the detail or its viewers

#### Scenario: Full record actions
- **WHEN** the user copies cURL, edits, resends, or exports without viewing a body first
- **THEN** the action still receives the complete stored input independently of the inspection load state

### Requirement: Economical history view working storage
Derived record-processing state for an open history SHALL retain only its current result views and selection. Temporary filter and sort intermediates SHALL become reclaimable after the operation. Clearing filters or sorting, substantially reducing the result, and closing or replacing the history SHALL release working storage no longer needed. Every original row and complete value SHALL remain available to the existing browse and action contracts.

#### Scenario: Large history without active transforms
- **WHEN** the user opens a valid 100000-entry history without filters or sorting
- **THEN** all entries remain available without retaining additional complete copies of row records solely for table binding or live-update processing

#### Scenario: Repeated filter and sort operations
- **WHEN** the user repeatedly applies and clears search, category filters, and sorting on the same history
- **THEN** retained working storage settles to what the current result requires rather than accumulating previous results or temporary comparison values
- **AND** an unsorted result returns to source order without modifying the original history

#### Scenario: Complete-value sorting and unknown values
- **WHEN** records differ only after the displayed preview, contain mixed-case text, or have unknown duration or size
- **THEN** sorting uses complete values, preserves existing text and tie ordering, and places unknown numeric values last in both directions
- **AND** filtering, inspection, copying, and export preserve exact stored content and empty-versus-missing distinctions

#### Scenario: Result shrinks or becomes empty
- **WHEN** a large result shrinks to a few entries or no entries through filtering or view deletion
- **THEN** unnecessary derived storage and obsolete references become reclaimable, removed selections are reconciled, and the empty state remains usable
- **AND** clearing the filter or reopening the history restores rows according to the existing rules without modifying the saved file

#### Scenario: Replacement reuses IDs
- **WHEN** the current result is replaced with different records that reuse IDs or have the same row count
- **THEN** filter, order, category, and displayed values reflect the new records rather than stale cached values
- **AND** opening a different saved session clears its predecessor's selection, focus, and details

#### Scenario: Read completes after cancellation
- **WHEN** an obsolete history load finishes after a different selection, history closure, or shutdown canceled it
- **THEN** it cannot publish rows, rebuild released derived state, or change the active selection

### Requirement: Category view memory lifecycle
Leaving Categories or collapsing its secondary sidebar SHALL release its derived record references, summaries, tree data, and menu state. For the same source, search, filter selections, and valid expansion and navigation choices SHALL survive this release. Reopening SHALL rebuild from current data. Closing or replacing the source SHALL discard its source-specific derived state and choices under the existing reset behavior.

#### Scenario: Hide and reopen the same source
- **WHEN** the user hides Categories with active filters and expanded structure nodes, then returns to the same source
- **THEN** the hidden view releases derived data without clearing the applied filters
- **AND** reopening reconstructs current data once and restores still-valid search, expansion, selection, and scroll intent

#### Scenario: Data changes while Categories is hidden
- **WHEN** rows are replaced or removed while Categories is hidden and the user reopens it
- **THEN** the rebuilt view reflects current rows, discards obsolete navigation targets, and clamps its scroll position
- **AND** no hidden menu or callback can act on the previous source

#### Scenario: Source closes while Categories is hidden
- **WHEN** the history closes or is replaced before Categories is shown again
- **THEN** its derived data cannot be restored by a later redraw, and the next category view uses only the active source
