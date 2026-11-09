# Spec Delta

## Purpose

Defines the capture workspace: controlling the proxy from the toolbar and viewing, filtering, and acting on live traffic in the traffic table.

## ADDED Requirements

### Requirement: Proxy control
The capture toolbar SHALL start and stop the proxy and show whether it is running and on which address.

#### Scenario: Starting the proxy
- **WHEN** the user starts the proxy with a valid configuration
- **THEN** the toolbar shows the running state and listening address

#### Scenario: Start failure
- **WHEN** the proxy cannot start, such as when the port is in use
- **THEN** the error is shown and the toolbar stays in the stopped state

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
The table SHALL offer ID, Process, Method, Host, Path, Status, Type, Time, Duration, Size, Server IP, and Protocol columns, which the user can sort, resize, reorder, and hide, with these preferences persisted.

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

### Requirement: Row selection
The table SHALL support single selection, multiple selection with modifier keys, and range selection by dragging with automatic scrolling near the edges.

#### Scenario: Drag selection near the edge
- **WHEN** the user drags a selection to the edge of the table
- **THEN** the table scrolls and extends the selection

#### Scenario: Selected entry evicted
- **WHEN** a selected entry is removed because of the retained limit
- **THEN** it is removed from the selection and the status bar count updates

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
