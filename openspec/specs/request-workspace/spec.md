# request-workspace Specification

## Purpose
Defines the tabbed workspace for composing and sending HTTP requests and WebSocket sessions, including edits started from captured traffic and per-request Python hooks.

## Requirements

### Requirement: Workspace tabs
The workspace SHALL open HTTP and WebSocket editors in tabs that can be reordered by dragging and closed individually or as Close Others, Close Right, and Close All.

#### Scenario: Closing a tab with unsaved changes
- **WHEN** the user closes a tab with unsaved changes
- **THEN** FlowLens asks for confirmation, and Force Close skips the confirmation

#### Scenario: Reordering tabs
- **WHEN** the user drags a tab to a new position
- **THEN** the tab order changes and the active tab stays active

#### Scenario: Unsaved marker
- **WHEN** a tab's content differs from its saved state
- **THEN** the tab shows an unsaved indicator

### Requirement: HTTP request composition
The HTTP Request Editor SHALL edit method, URL, query parameters, ordered headers, cookies, and a body of type None, JSON, Text, XML, Form Data, URL-encoded, or File.

#### Scenario: URL and query stay synchronized
- **WHEN** the user edits a query parameter row
- **THEN** the URL updates, and editing the URL updates the query rows

#### Scenario: Header order is sent as displayed
- **WHEN** the user arranges header rows and sends the request
- **THEN** the headers are sent in the displayed order

#### Scenario: Invalid header layout
- **WHEN** the transport cannot represent the requested header layout
- **THEN** the request is rejected with a visible error and is not silently reordered

#### Scenario: Invalid input before send
- **WHEN** the URL, method, or a required file is invalid
- **THEN** a validation message is shown and nothing is sent

### Requirement: Request settings
Each HTTP request SHALL support No Proxy, System Proxy, MITM Proxy, and Custom Proxy modes, automatic, HTTP/1.1, or HTTP/2 protocol, a maximum redirect count defaulting to 0, and a per-tab Skip TLS Verification switch defaulting to off.

#### Scenario: Redirect limit reached
- **WHEN** a response chain reaches the configured redirect limit
- **THEN** the last response's status, headers, and body are shown without an error

### Requirement: TLS fingerprint settings
HTTPS and WSS requests SHALL offer Go, Chrome, Firefox, Safari, Edge, iOS, Android 11 OkHttp, and randomized-ALPN ClientHello profiles, and HTTP requests SHALL accept a four-part HTTP/2 fingerprint.

#### Scenario: Fingerprint with explicit HTTP/1.1
- **WHEN** a request uses HTTP/1.1 and has an HTTP/2 fingerprint
- **THEN** the fingerprint is not applied

### Requirement: Sending and viewing responses
The editor SHALL send the active request, allow stopping it, and show response status, timing, headers, trailers, and body using the traffic body viewers.

#### Scenario: Streaming response
- **WHEN** a response streams
- **THEN** the body updates as data arrives and Stop ends the stream

#### Scenario: Canceled request
- **WHEN** the user stops a request before it completes
- **THEN** the response area states that the request was canceled

#### Scenario: Transport error
- **WHEN** the request fails
- **THEN** the error details are shown and can be copied

### Requirement: WebSocket Client
The WebSocket Client SHALL connect with ordered headers and the same proxy and TLS settings, send messages, and show the ordered message stream with message details.

#### Scenario: Connect and exchange messages
- **WHEN** the user connects and sends a message
- **THEN** sent and received messages appear in order

#### Scenario: Connection closes
- **WHEN** the connection closes or fails
- **THEN** the state is shown and the user can reconnect

### Requirement: Opening captured traffic in the editor
Edit on a traffic entry SHALL open an editor tab populated from that entry, retaining header order and body.

#### Scenario: Editing a captured request
- **WHEN** the user chooses Edit on a captured HTTP entry
- **THEN** a tab opens with its method, URL, ordered headers, and body

#### Scenario: Editing a large captured body
- **WHEN** the captured request body is available only from the body cache
- **THEN** the complete body is recovered into the editor

### Requirement: Request file bodies
The editor SHALL accept files for File and Form Data bodies through a picker or by dropping files onto the file control.

#### Scenario: Dropping a file
- **WHEN** the user drops a file onto a form-data file row
- **THEN** that row uses the dropped file and dropped files elsewhere do not change it

### Requirement: Python hooks in requests
HTTP requests SHALL be able to use global Python plugins and a current-request script, and the script's enabled switch and the global-plugins bypass SHALL be per tab and off by default when a request is reopened.

#### Scenario: Plugins not configured
- **WHEN** Python is not configured
- **THEN** the controls explain that Python must be enabled in Settings first

#### Scenario: Empty enabled script
- **WHEN** the script is enabled and empty
- **THEN** the request is not sent and a message explains why

### Requirement: Python request console
The console SHALL show complete plugin output and execution status for each send and SHALL be the only place that output is shown.

#### Scenario: Plugin error on a response hook
- **WHEN** a response hook fails
- **THEN** the response is still shown and the console shows the error

#### Scenario: Plugin error on a request hook
- **WHEN** a request hook fails
- **THEN** the request is not sent and the console shows the error

### Requirement: Grid key/value editors
The HTTP header, query, cookie, URL-encoded, and form-data editors and WebSocket header editor SHALL present ordered, compact grids with enable, key, value, and row-action areas. Closed text cells SHALL show one-line content or a muted placeholder. Hover and keyboard focus SHALL remain distinguishable. Row dimensions SHALL accommodate native fonts without requiring the original frontend's exact pixels.

#### Scenario: Empty table
- **WHEN** a new HTTP request tab opens its Headers pane
- **THEN** one row is shown with a checked enable box and muted key and value placeholders inside a bordered grid

#### Scenario: Keyboard reaches text cells
- **WHEN** a user navigates the grid without a pointer
- **THEN** each editable name and value cell can receive visible focus and can be activated without first clicking it

### Requirement: Click-to-edit grid cells
Clicking a text cell or activating it with Enter or Space SHALL open a focused editor that wraps and grows with its content. Enter, including Shift+Enter, and leaving the editor SHALL commit without adding an Enter-generated line break. Escape SHALL cancel that edit and restore its original value. Ending editing with Enter or Escape SHALL return focus to the cell or a valid fallback. The key column SHALL remain resizable within limits.

#### Scenario: Editing a long value
- **WHEN** the user opens a value cell holding a 300-character cookie and edits it
- **THEN** the editor grows to show the value and Enter closes it with the new value shown on one line

#### Scenario: Invalid header name
- **WHEN** a header key contains a space and editing ends
- **THEN** the key cell shows an error icon whose tooltip explains that the name is invalid

#### Scenario: Tab continues through fields
- **WHEN** the user presses Tab or Shift+Tab while editing
- **THEN** the current edit commits and focus proceeds to the next or previous operable control without reopening the editor

#### Scenario: Input method confirmation
- **WHEN** Enter confirms an active input method composition
- **THEN** it confirms the composition without prematurely closing the cell editor

#### Scenario: Escape cancels editing
- **WHEN** the user presses Escape after changing a cell value outside active composition
- **THEN** the editor closes, the value from when editing began is restored, and focus returns to its cell

#### Scenario: Edited row disappears
- **WHEN** a row being edited is removed or the source tab disappears
- **THEN** editing ends without writing into another row and focus moves only to a control that still exists in the active surface

#### Scenario: Ordered fields survive keyboard editing
- **WHEN** the user edits duplicated or differently cased headers through the keyboard
- **THEN** row order, casing, duplicates, empty values, and enable flags are preserved under the same validation rules as pointer editing

### Requirement: URL-encoded editor copy
The URL-encoded body editor SHALL use its own placeholders, "Field name" and "Field value", and its own Add Row and Clear All actions and tooltips, as in FlowLens 1.0.5.

#### Scenario: URL-encoded placeholders
- **WHEN** the user selects the URL-encoded body type
- **THEN** empty cells show "Field name" and "Field value"

### Requirement: Active tab stays in view
When a workspace tab becomes active by selection, creation, opening from traffic or a collection, or a shortcut, the tab strip SHALL scroll smoothly by the least amount that shows the whole tab.

#### Scenario: Opening a tab past the visible strip
- **WHEN** many tabs are open and the user opens a saved request from API Collections
- **THEN** the new active tab is scrolled fully into view

### Requirement: Tab drag presentation
A dragged workspace tab SHALL follow the pointer horizontally, and its neighbors SHALL slide to open the target slot. The strip SHALL scroll when the pointer nears its edge. The Capture tab SHALL stay first and not move. Releasing SHALL keep the dragged tab active if it was active.

#### Scenario: Dragging past the visible strip
- **WHEN** the user drags a tab to the right edge of an overflowing strip
- **THEN** the strip scrolls, and the tab can be dropped past tabs that were hidden

### Requirement: Python console columns
The Python request console SHALL list entries in a bordered table with a header of Time, Type, Source, and Output. The user SHALL be able to resize each column within bounds: Time 120–360 px, Type 80–260 px, Source 96–420 px, and Output 240–900 px, starting at 152, 96, 120, and 260 px. Output SHALL fill the remaining width. Widths SHALL last while the editor tab is open and SHALL NOT be saved.

#### Scenario: Resizing past a bound
- **WHEN** the user drags the Source column edge to 600 px
- **THEN** the Source column stops at 420 px

#### Scenario: Narrow console
- **WHEN** the columns are wider than the console
- **THEN** the table scrolls horizontally and the header moves with the rows

### Requirement: Python console rows
Each console row SHALL be 40 px high and show on one line its local time, a Type cell with a stream icon (terminal for stdout, warning triangle for stderr) and a level pill in the entry's tone, its source, and the first line of its output. Multi-line output SHALL show its line count. The list SHALL NOT wrap output.

#### Scenario: Multi-line stderr entry
- **WHEN** a plugin writes three lines to stderr with no level
- **THEN** the row shows the warning triangle, an Info pill in the error tone, the first line, and "3 lines", at 40 px

#### Scenario: Opening an entry
- **WHEN** the user clicks a row
- **THEN** the entry detail opens for that entry

#### Scenario: Opening an entry from the keyboard
- **WHEN** the console table has the keyboard focus and the user presses Down, then Enter
- **THEN** the entry detail opens for the next entry, and no row shows as selected

### Requirement: Python console status
The console header SHALL show a dot and status text: a pulsing accent dot with "Script running" while a send is in progress, and a muted dot with the entry count otherwise.

#### Scenario: During a send
- **WHEN** a request with hooks is being sent
- **THEN** the header shows the pulsing dot and "Script running"

#### Scenario: After a send
- **WHEN** the send finished with five entries
- **THEN** the header shows the muted dot and "5 output entries"

### Requirement: Python console entry detail
The entry detail SHALL show the entry's time, a pill with its stream icon, stream, and level in its tone, and its source, with Wrap, Copy, and Save icon actions. Wrap SHALL start off and SHALL apply to the detail text only.

#### Scenario: Turning on wrap
- **WHEN** the user opens a long single-line entry and turns on Wrap
- **THEN** the detail text wraps and the console rows stay on one line

### Requirement: Python console copy and save format
Copy All and Save All SHALL write one line per entry, `[<local time>] [<source>] [<stream>] <message>`, joined by line breaks, where stream is the entry's raw stream name, else its level, else `log`. Save All SHALL suggest `flowlens-python-console.log`. Saving one entry SHALL write its message and suggest `flowlens-python-console-entry.log`.

#### Scenario: Copy all
- **WHEN** the console holds a stdout entry from the current script and the user chooses Copy All
- **THEN** the clipboard holds `[<time>] [Current script] [stdout] <message>` for it, one line per entry

#### Scenario: Save one entry
- **WHEN** the user chooses Save in an entry's detail
- **THEN** the save dialog suggests `flowlens-python-console-entry.log` and the file holds only the message

### Requirement: Clearing the console closes its detail
Clearing the console, or a new send that replaces its entries, SHALL close an open entry detail.

#### Scenario: Clear with the detail open
- **WHEN** an entry detail is open and the user chooses Clear
- **THEN** the detail closes and the console is empty

### Requirement: Body type menu order
The HTTP Request Editor's body type menu SHALL list None, JSON, Text, XML, File, Form Data, and URL-encoded, in that order.

#### Scenario: Opening the body type menu
- **WHEN** the user opens the body type menu
- **THEN** File comes after XML and URL-encoded comes last

### Requirement: Byte-exact editor responses
The HTTP Request Editor SHALL show, copy, and save a response body as the exact bytes it received or a Python response hook produced. A body SHALL be shown as binary, as before, when its content type is binary, a streaming body has an unsupported content encoding, or a hook returned a binary body.

#### Scenario: Binary response
- **WHEN** a request returns an `image/png` response
- **THEN** the Image and Hex views show the received bytes, and saving the body writes a file identical to them

#### Scenario: Binary result from a Python hook
- **WHEN** an `onResponse` hook replaces the body with binary data, either inline or larger than 4 MiB
- **THEN** the response body holds exactly the bytes the hook produced and is shown as binary

#### Scenario: Text response
- **WHEN** a request returns a JSON response
- **THEN** the Formatted and Raw views show its text as before, and no Base64 appears

### Requirement: Byte-exact request recovery
Edit on a captured or historical entry with a binary request body SHALL recover a file body that holds exactly the captured request bytes.

#### Scenario: Editing a binary captured request
- **WHEN** the user chooses Edit on an entry whose request body is `application/octet-stream`
- **THEN** the editor tab uses a File body whose file is identical to the captured request body

#### Scenario: Editing a request with invalid UTF-8
- **WHEN** the user chooses Edit on an entry whose `text/plain` request body contains invalid UTF-8
- **THEN** the editor tab uses a File body with the original bytes, not text with replacement characters

### Requirement: Streaming binary editor responses
A streaming editor response shown as binary SHALL grow by each received chunk, in offset order. Each chunk SHALL extend the body without re-processing the data already shown, so a long stream stays responsive.

#### Scenario: Streaming with an unsupported content encoding
- **WHEN** a `text/event-stream` response with an unsupported `Content-Encoding` streams into the editor
- **THEN** the Hex view grows with the received bytes, in order, until the stream completes or is stopped

#### Scenario: Chunk ahead of the body
- **WHEN** a chunk arrives whose offset is past the end of the body shown
- **THEN** it is held until the missing data arrives, and no gap appears in the body
