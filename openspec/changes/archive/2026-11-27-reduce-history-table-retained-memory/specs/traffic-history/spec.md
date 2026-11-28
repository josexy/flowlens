# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
