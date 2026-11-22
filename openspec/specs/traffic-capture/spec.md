# traffic-capture Specification

## Purpose
Defines the capture workspace: controlling the proxy from the toolbar and viewing, filtering, and acting on live traffic in the traffic table.

## Requirements

### Requirement: Proxy control
The capture toolbar SHALL start and stop the proxy and show whether it is running and on which address. A compact split control SHALL fit its localized labels and keep a stable width across states: Start Proxy when stopped, Pause while recording, and Resume while paused. When running, its dropdown SHALL offer Stop Proxy. Recording state SHALL be expressed by the split control, without an extra label beside proxy status; pending operations SHALL disable conflicting actions.

#### Scenario: Starting the proxy
- **WHEN** the user starts the proxy with a valid configuration
- **THEN** the toolbar shows the running state and listening address, and recording is enabled

#### Scenario: Start failure
- **WHEN** the proxy cannot start, such as when the port is in use
- **THEN** the error is shown and the toolbar stays in the stopped state

#### Scenario: Stop from the split menu
- **WHEN** the user chooses Stop Proxy while recording is active or paused
- **THEN** the proxy stops and the control returns to Start Proxy without clearing existing traffic

#### Scenario: Accessible split control
- **WHEN** the user operates the control by keyboard in either theme and interface language
- **THEN** its primary action and dropdown are separately focusable, clearly named, and perform the same operations as pointer activation

### Requirement: Quick proxy settings
The toolbar SHALL let the user change proxy mode, listening host, port, upstream proxy disabling, HTTP/2 disabling, anti-cache, anti-compression, TLS verification skipping, and domain include and exclude filters.

#### Scenario: Invalid port
- **WHEN** the user enters a port outside 1 to 65535
- **THEN** the value is rejected with a message and the saved port is unchanged

#### Scenario: Domain filters
- **WHEN** the user saves domain filters
- **THEN** the toolbar shows the active filter count and new traffic follows the filters

### Requirement: DNS cache control
The toolbar SHALL let the user enable the shared DNS cache, set a duration from 1 to 3600 seconds, apply it, and clear it.

#### Scenario: Invalid duration
- **WHEN** the user enters a duration outside 1 to 3600
- **THEN** the value is rejected and the applied configuration is unchanged

#### Scenario: Clearing the cache
- **WHEN** the user clears the DNS cache
- **THEN** cached records are removed without interrupting existing connections

### Requirement: Managed system proxy control
On Windows and macOS the toolbar SHALL toggle the system proxy to the active listener, and on Linux the control SHALL NOT be shown.

#### Scenario: Unsupported mode on Windows
- **WHEN** the proxy mode is SOCKS5 on Windows
- **THEN** the system proxy control is disabled with an explanation

#### Scenario: Listener on all interfaces
- **WHEN** the listener host is `ALL`
- **THEN** the system proxy points to `127.0.0.1`

### Requirement: Live traffic table
The traffic table SHALL show new entries as they are captured and update existing rows as their exchanges progress, retaining at most the newest 5000 entries.

#### Scenario: Entries arrive
- **WHEN** traffic is captured while the table shows the latest entries
- **THEN** new rows appear in ID order

#### Scenario: Exceeding the retained limit
- **WHEN** more than 5000 entries have been captured
- **THEN** the lowest IDs are removed and evicted entries do not reappear from late events

### Requirement: Reading without losing position
While the user reads away from the newest entries, the table SHALL hold its displayed rows and report how many new requests are waiting.

#### Scenario: Scrolled away during capture
- **WHEN** the user has scrolled away from the newest edge and new traffic arrives
- **THEN** the visible rows stay in place and a Show new requests control displays the count

#### Scenario: Showing new requests
- **WHEN** the user activates Show new requests
- **THEN** the queued entries are applied in ID order and the table returns to the newest edge

#### Scenario: Patches while holding
- **WHEN** rows on screen receive status or timing updates while new entries are held
- **THEN** the visible rows update in place

### Requirement: Traffic filtering
The table SHALL filter by a search query over URL, target, port, source, and path, and by type tabs All, HTTP, HTTPS, WebSocket, SSE, TCP, HTTP1, HTTP2, JSON, XML, Text, HTML, JS, Image, Media, and Binary.

#### Scenario: Search query
- **WHEN** the user types a search query
- **THEN** only matching rows are shown and the status bar total still counts all entries in the session

#### Scenario: Type tab with live traffic
- **WHEN** the JSON tab is active and a non-JSON response completes
- **THEN** that entry is not shown

### Requirement: Table columns
The table SHALL offer ID, Process, Method, Host, Path, Status, Type, Duration, Size, Server IP, and Protocol columns, which the user can sort, resize, reorder, and hide, with these preferences persisted.

#### Scenario: Persisted layout
- **WHEN** the user reorders, resizes, or hides columns and restarts FlowLens
- **THEN** the table opens with the saved column layout

#### Scenario: Incomplete values while sorting
- **WHEN** the table is sorted by duration or size and some exchanges are incomplete
- **THEN** rows without values stay last in either sort direction

#### Scenario: Sort cycle
- **WHEN** the user clicks a sortable column header repeatedly
- **THEN** the sort cycles through ascending, descending, and unsorted

### Requirement: Rendering at capacity
Scrolling the traffic table SHALL stay responsive at the retained entry limit, including while traffic is arriving.

#### Scenario: Scrolling a full table
- **WHEN** the table holds 5000 entries and traffic is arriving
- **THEN** only rows in view are built and each frame's build time does not grow with the total entry count

### Requirement: Row actions
The row context menu SHALL offer Show, Copy as cURL, Copy (Target, URL, Domain, Path, Server IP, Client IP, Server Address, Client Address, Row), Export, Edit, Resend, Resend N Times, Highlight (Red, Orange, Yellow, Green, Blue, Purple, Clear), and Delete.

#### Scenario: Resend N times
- **WHEN** the user resends an entry N times with N from 1 to 1000
- **THEN** N requests are sent and appear according to the chosen proxy option

#### Scenario: Edit limit
- **WHEN** the user chooses Edit with more than 50 entries selected
- **THEN** a message states the limit and no tabs are opened

#### Scenario: Unavailable request body
- **WHEN** an entry's request body is unavailable and the user chooses Edit, Resend, or Copy as cURL
- **THEN** the action states that the body is unavailable and does not send incomplete data

#### Scenario: Deleting multiple entries
- **WHEN** the user deletes multiple selected entries and confirms
- **THEN** the entries are removed and the result is reported

### Requirement: Process column
The Process column SHALL show the application icon and name when available, updating as attribution completes, and a placeholder otherwise.

#### Scenario: Metadata before the icon
- **WHEN** the process name is known but its icon has not loaded
- **THEN** the name is shown with a placeholder icon and the icon appears once loaded

#### Scenario: Remote client
- **WHEN** a connection comes from a remote client
- **THEN** the row is marked remote and no local process lookup is shown

### Requirement: Capture session management
The capture workspace SHALL let the user clear traffic, set a session alias, and restart capture with Save and Restart or Discard and Restart when records exist.

#### Scenario: Restart with records
- **WHEN** the user restarts capture while the session has records
- **THEN** FlowLens asks whether to save or discard them before starting a new session

### Requirement: Traffic table header style
The traffic table header SHALL have a 32 px band on the elevated background with semibold column titles and no vertical dividers. A hovered title SHALL get a hover background. The sorted column SHALL show an up or down arrow in the accent color. A resize handle SHALL appear in the accent color only while hovered.

#### Scenario: Sorted column
- **WHEN** the user sorts by ID descending
- **THEN** the ID title shows a down arrow in the accent color, and no divider lines separate the titles

### Requirement: Full method and status badges
Method and status badges SHALL show their full text, such as `OPTIONS` or `CONNECT`, at the default column widths. They SHALL use the 1.0.5 tones: each method's color on a 13% tint over the panel background, and the success, info, warning, error, and default status tones.

#### Scenario: Long method name
- **WHEN** an OPTIONS request is shown with default column widths
- **THEN** its badge reads `OPTIONS` with no ellipsis

### Requirement: Inspected and highlighted row markers
The row shown in the detail panel SHALL have a 3 px accent bar at its start and a 1 px accent outline. A highlighted row that is not selected SHALL have its highlight color as a background tint, a 3 px start bar, and a 1 px outline. The tint and outline SHALL be stronger in the dark theme.

#### Scenario: Inspecting a highlighted row
- **WHEN** the user opens a highlighted row in the detail panel
- **THEN** the row shows the selection tint with the accent bar and outline in place of the highlight marker

### Requirement: Aligned numeric columns
Duration and size cells SHALL use tabular numerals, so values line up across rows.

#### Scenario: Comparing durations
- **WHEN** rows show durations of 11 ms and 496 ms
- **THEN** digits of equal place value line up vertically

### Requirement: Selection export file name
Exporting selected rows SHALL use the base name `<alias>-selection` when the table shows a saved history that has an alias, and `flowlens-selection-<count>` otherwise. The format's usual suffix and extension SHALL follow the base name, as for other exports.

#### Scenario: History with an alias
- **WHEN** the table shows a saved history aliased "login-flow" and the user exports three selected rows as HAR
- **THEN** the save dialog suggests `login-flow-selection.har`

#### Scenario: Live capture
- **WHEN** the table shows live capture and the user exports three selected rows as CSV
- **THEN** the save dialog suggests `flowlens-selection-3-csv.csv`

### Requirement: Native row selection
Traffic tables SHALL use conventional native single, additive, range, drag, and keyboard selection. Selecting rows SHALL NOT itself open a closed detail panel. Selection SHALL track entry identity through table changes, exclude removed entries, and report its current count. An interrupted pointer gesture SHALL NOT leave selection or keyboard navigation stuck. Opening another saved session SHALL clear its previous selection, focus, and details.

#### Scenario: Click selects without opening details
- **WHEN** the detail panel is closed and the user clicks a row
- **THEN** that row alone is selected and focused, and the panel stays closed

#### Scenario: Click while details are open
- **WHEN** the detail panel is open and the user clicks another row
- **THEN** that row alone is selected and the panel shows it

#### Scenario: Clicking the only selected row
- **WHEN** exactly one row is selected and the user clicks it without a modifier
- **THEN** it remains selected and the detail panel's visibility does not change

#### Scenario: Additive selection
- **WHEN** row 2 is selected and the user Ctrl-clicks row 5 on Windows/Linux or Cmd-clicks it on macOS
- **THEN** rows 2 and 5 are selected, row 5 is focused, and a closed panel stays closed

#### Scenario: Removing a selected row
- **WHEN** rows 2 and 5 are selected and the user applies the additive modifier to row 5 again
- **THEN** row 5 leaves the selection and row 2 remains available for detail inspection

#### Scenario: Removing the last selected row
- **WHEN** the user removes the last selected row with the additive modifier
- **THEN** the count becomes zero and no entry remains displayed in the detail panel

#### Scenario: Shift selects a range
- **WHEN** row 2 is the selection anchor and the user Shift-clicks row 5
- **THEN** the inclusive range from row 2 through row 5 in current table order is selected

#### Scenario: Drag replaces the selection
- **WHEN** the user drags from row 4 to row 6 without modifiers
- **THEN** rows 4 through 6 are selected according to the native drag gesture and detail visibility does not change

#### Scenario: Quick drag
- **WHEN** a press and movement across several rows occur before the next drawn frame
- **THEN** the selection extends to the latest drag position and does not revert to the earlier selection

#### Scenario: Release outside the table
- **WHEN** a pointer gesture ends outside the table and the user returns to keyboard navigation
- **THEN** the next keyboard selection change succeeds without a delayed pointer action reverting it

#### Scenario: Window loses focus during a press
- **WHEN** the window loses focus during a press and the user subsequently returns to the table
- **THEN** keyboard movement and Select All continue to operate

#### Scenario: Drag selection near the edge
- **WHEN** the user drags a selection to the table's edge
- **THEN** the table scrolls and extends the selection

#### Scenario: Keyboard selection
- **WHEN** the table has focus and the user presses Down, Shift+Down, or the platform Select All binding
- **THEN** native row movement, range extension, or Select All occurs and a closed detail panel stays closed

#### Scenario: Selected entry evicted
- **WHEN** a selected entry leaves the retained traffic set
- **THEN** it leaves the selection and the displayed selection count updates

#### Scenario: Focused entry removed
- **WHEN** the inspected entry is filtered out, deleted, evicted, or deselected while other selected rows remain
- **THEN** inspection moves to the first remaining selected row in table order; if none remain, details close

#### Scenario: Sorting preserves identity
- **WHEN** the user sorts the table with selected rows
- **THEN** those entry IDs remain selected and a subsequent range gesture uses the displayed order

#### Scenario: Opening another saved session
- **WHEN** the user switches from a selected saved session to another session
- **THEN** no row from the previous session remains selected or focused and its details are closed

#### Scenario: Context menu on a selected group
- **WHEN** the user opens a row context menu within a multiple selection
- **THEN** applicable bulk actions retain that selection; header context menus continue to target columns

### Requirement: Bounded capture memory
A capture session SHALL hold in memory only its newest 5000 entries and entries whose exchange is still in progress. Older finished entries SHALL move to disk for the rest of the session, so capture memory does not grow with session length. Captured WebSocket message payloads larger than the body cache threshold SHALL be held on disk.

#### Scenario: Long capture
- **WHEN** a session has captured 200000 finished entries
- **THEN** entries older than the newest 5000 hold no headers, bodies, WebSocket messages, or process details in memory

#### Scenario: Status total
- **WHEN** older entries have moved to disk
- **THEN** the status bar total still counts every entry in the session

#### Scenario: Long-running exchange
- **WHEN** a WebSocket connection or streaming response opened early stays open while thousands of newer entries arrive
- **THEN** it stays in memory and keeps receiving updates until it finishes

#### Scenario: Exchanges finish after traffic stops
- **WHEN** exchanges older than the newest 5000 entries finish and no new entries arrive
- **THEN** their data moves to disk automatically, including when only one such entry remains, and their details remain readable

#### Scenario: Acting on an older entry
- **WHEN** the table holds its window while reading and the user opens, copies as cURL, resends, edits, exports, or deletes an entry that is no longer among the newest 5000
- **THEN** the action behaves as it does for the newest entries, with the same headers, bodies, and messages

#### Scenario: Exporting the whole capture
- **WHEN** the user exports all captured entries of a session that has entries on disk
- **THEN** every entry of the session is written in ID order, as for a session held entirely in memory

#### Scenario: Large WebSocket messages
- **WHEN** an open WebSocket connection has received 1000 messages of 8 MiB each
- **THEN** those payloads are held on disk, and the WebSocket view, saved history, and export show them byte for byte

### Requirement: Periodic capture saving
The current capture SHALL be saved to its history periodically and at shutdown. A save SHALL NOT read whole bodies still being received into memory. When a save takes long, the next periodic save SHALL wait at least five times that save's duration, and never less than 60 seconds.

#### Scenario: Large download in progress
- **WHEN** a periodic save runs while a 2 GiB response is still being received
- **THEN** the received part is saved without being held in memory, and the response keeps streaming to the client

#### Scenario: Slow save
- **WHEN** a periodic save of a large session takes 30 seconds
- **THEN** the next periodic save starts no sooner than 150 seconds later

#### Scenario: Session with entries on disk
- **WHEN** a session whose older entries are on disk is saved
- **THEN** the saved history contains every entry of the session in ID order

### Requirement: Capture storage release
On-disk data of a capture session SHALL be removed once it is no longer needed: when entries are deleted, when the capture is cleared, when capture restarts after Save or Discard, after a successful save at shutdown, and at startup for sessions left by earlier runs. Saved histories and process icons SHALL NOT be affected.

#### Scenario: Restarting capture
- **WHEN** the user chooses Save and Restart and the save succeeds
- **THEN** the previous session's cached bodies and on-disk entries are removed and its saved history remains

#### Scenario: Previous run ended abnormally
- **WHEN** FlowLens starts after a crash left an earlier session's cached bodies on disk
- **THEN** that data is removed while saved histories and cached process icons remain

#### Scenario: Deleting entries while bodies arrive
- **WHEN** the user deletes entries whose request or response bodies start arriving at the same moment
- **THEN** no body data of the deleted entries remains in memory or on disk

#### Scenario: Failed save at shutdown
- **WHEN** the final save at shutdown fails
- **THEN** the session's cached data is kept on disk until the next startup

### Requirement: Capture never stalls forwarding
Decoding a captured body for display SHALL NOT delay or block forwarding the original bytes between client and server, whatever the body contains.

#### Scenario: Bytes after the compressed stream
- **WHEN** a `deflate` response carries bytes after the end of its compressed stream
- **THEN** the client receives the complete response, the entry completes, and no background work remains for it

#### Scenario: Corrupt compressed body
- **WHEN** a `gzip` response has an invalid checksum
- **THEN** the client receives the original bytes and the entry completes with the bytes that could be decoded

### Requirement: Pause recording independently of forwarding
Pausing recording SHALL stop admission of new captured exchanges without stopping or reconfiguring the proxy, changing system proxy settings, or interrupting client forwarding. Existing records SHALL remain readable, savable, and exportable. Resuming SHALL admit new exchanges in the same capture session without recovering traffic skipped during the pause.

#### Scenario: HTTP and HTTPS during a pause
- **WHEN** new HTTP or HTTPS requests use the paused proxy, including requests on an existing keep-alive connection
- **THEN** requests and responses are forwarded using the existing TLS, upstream, rewrite, and live-option settings without creating traffic records, captured bodies, capture metrics, or record attribution

#### Scenario: New WebSocket or TCP connection during a pause
- **WHEN** a new WebSocket or raw TCP tunnel is admitted while paused
- **THEN** traffic continues to forward without a capture record, and resuming does not start recording that already-admitted connection

#### Scenario: Exchanges admitted before pausing
- **WHEN** recording is paused with already-captured HTTP requests, SSE responses, or WebSocket connections in progress
- **THEN** those exchanges continue forwarding and recording through completion with their original metrics and body semantics

#### Scenario: Concurrent pause and admission
- **WHEN** pause or resume overlaps admission of a new exchange
- **THEN** that exchange receives exactly one admission decision and cannot appear later as a partial or resurrected record

### Requirement: Recording pause lifecycle
Recording pause SHALL be transient. Explicitly starting a stopped proxy SHALL enable recording. A configuration-driven listener restart SHALL preserve its previous recording state before accepting traffic. Clearing or restarting the capture session SHALL preserve the recording state. Pause and resume SHALL be unavailable when the proxy is stopped, and failed operations SHALL report the authoritative state.

#### Scenario: Internal restart while paused
- **WHEN** a configuration change restarts a paused proxy
- **THEN** its new listener starts paused with no interval in which new exchanges are recorded

#### Scenario: New capture while paused
- **WHEN** the user clears traffic or restarts the capture session while paused
- **THEN** the normal cleanup and late-callback protections apply and recording remains paused

#### Scenario: Explicit stop and start
- **WHEN** the user stops a paused proxy and explicitly starts it again
- **THEN** recording is enabled

#### Scenario: Application restart
- **WHEN** FlowLens starts again after a previous run was paused
- **THEN** the pause is not restored from settings or history
