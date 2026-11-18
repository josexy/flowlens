# traffic-inspection Specification

## Purpose
Defines how a selected traffic entry is inspected, including its metadata, ordered headers, cookies, bodies, hex data, SSE and WebSocket messages, certificates, timing, and process details.

## Requirements

### Requirement: Detail layout
The detail panel SHALL support horizontal and vertical layouts with an adjustable split, both persisted.

#### Scenario: Restoring the layout
- **WHEN** the user switches to vertical layout, adjusts the split, and restarts FlowLens
- **THEN** the panel opens with the saved layout and split

### Requirement: Faithful header display
Headers and trailers SHALL be shown in wire order, retaining duplicates, casing, and empty values.

#### Scenario: Duplicate headers
- **WHEN** a response has two `Set-Cookie` headers with different casing
- **THEN** both are shown in their original order and casing

#### Scenario: Wire order unavailable
- **WHEN** raw header order was not recorded for an entry
- **THEN** normalized fields are shown with a notice that wire order is unavailable

#### Scenario: Truncated headers
- **WHEN** a header block was truncated during capture
- **THEN** the panel shows that the headers are truncated

### Requirement: Body viewing modes
The body viewer SHALL classify bodies as JSON, XML, HTML, JavaScript, CSS, text, image, SVG, or binary and offer the applicable Formatted, Image, Raw, SSE, and Hex views.

#### Scenario: JSON body
- **WHEN** a JSON response is viewed
- **THEN** the Formatted view shows indented JSON with syntax highlighting

#### Scenario: Image body
- **WHEN** an image response in PNG, JPEG, GIF, WebP, or BMP format, or an SVG, is viewed
- **THEN** the Image view shows the picture

#### Scenario: Undecodable image
- **WHEN** an image body cannot be decoded
- **THEN** a message is shown and the Raw and Hex views remain available

#### Scenario: HTML body
- **WHEN** an HTML response is viewed
- **THEN** its source is shown with syntax highlighting and is not executed or rendered as a page

### Requirement: Complete text viewing
Text bodies and Raw HTTP views SHALL show all available content without pagination, with search, selection, copy, and save covering the full text and word wrap toggleable.

#### Scenario: Searching a long body
- **WHEN** the user searches a body larger than the visible area
- **THEN** matches throughout the full content are found

#### Scenario: Very large text
- **WHEN** a document has at least 512K characters or a line of at least 128K characters
- **THEN** it is shown as plain text without syntax highlighting or bracket matching

### Requirement: Missing and empty bodies are distinct
The viewer SHALL distinguish an empty body from a body that is unavailable.

#### Scenario: Unavailable body
- **WHEN** a body's stored payload is missing
- **THEN** the viewer states that the body is unavailable and does not show it as empty

### Requirement: Hex view
The Hex view SHALL show offset, hexadecimal, and text columns with byte-based selection, copy, and export, and keep rendering bounded for large bodies.

#### Scenario: Selecting bytes
- **WHEN** the user selects a range in the hexadecimal column
- **THEN** the same bytes are highlighted in the text column and copy uses exactly those bytes

#### Scenario: Very large body
- **WHEN** a body of hundreds of megabytes is shown in Hex view
- **THEN** only the visible region is laid out and scrolling stays responsive

### Requirement: SSE message view
Responses of type `text/event-stream` SHALL open the SSE tab by default and list complete events in order with event type, effective ID, and a short data preview.

#### Scenario: Following new messages
- **WHEN** messages arrive while the list is at the latest message
- **THEN** the list follows new messages

#### Scenario: Pausing follow
- **WHEN** the user scrolls up
- **THEN** following pauses until the user activates Jump to latest message

#### Scenario: Message detail
- **WHEN** the user opens a message
- **THEN** its full data and original event block are shown, and small valid JSON also offers a formatted view

#### Scenario: Incomplete final event
- **WHEN** the final event has no terminating blank line
- **THEN** it is not counted as a message and a notice states that it is incomplete

#### Scenario: Encoded body
- **WHEN** an SSE body cannot be parsed because it is encoded binary data
- **THEN** the view explains this and the Hex view and save remain available

### Requirement: WebSocket message view
The WebSocket tab SHALL list sent and received messages in order and open a message's full content in a detail view.

#### Scenario: Message limit reached
- **WHEN** an entry's retained message limit is reached
- **THEN** the view states that some messages were truncated

### Requirement: Overview details
The Overview SHALL show connection metadata, timing, TLS and server certificate details, and process details, and SHALL display unknown numeric values as `—`.

#### Scenario: Incomplete timing
- **WHEN** an exchange failed before completing
- **THEN** unknown timing and size values show `—` and no completion value is invented

#### Scenario: Process details
- **WHEN** process attribution succeeded
- **THEN** process name, PID, app ID, executable path, and lookup status are shown

### Requirement: Raw TCP tunnel details
Raw TCP entries SHALL show tunnel target, source type, observation time, and any outer CONNECT protocol and headers.

#### Scenario: Tunnel through HTTP CONNECT
- **WHEN** a raw TCP entry originated from HTTP CONNECT
- **THEN** the source type and CONNECT headers are shown

### Requirement: Cookie display
Cookie tabs SHALL list parsed cookies and their attributes and provide the raw header text.

#### Scenario: Response cookie attributes
- **WHEN** a response sets a cookie with attributes
- **THEN** the user can view each attribute and the raw `Set-Cookie` header

### Requirement: Full-width detail tab strips
The request and response tab strips SHALL span the full width of their pane. The active tab SHALL show a primary underline, and a bottom border SHALL run under the whole strip. Trailing response chips SHALL stay right-aligned in the same row.

#### Scenario: Few tabs in a wide pane
- **WHEN** the request pane is wider than its tabs
- **THEN** the strip's bottom border reaches both edges of the pane

### Requirement: Compact field tables
Header, trailer, query, and cookie tables in the detail panel SHALL use one-line rows 40 px tall in a bordered grid, with row hover and a name column the user can resize. A long value SHALL be cut to one line. Clicking a cell SHALL open a read-only editor over it that grows to show the whole value, can be selected and copied, and closes when focus leaves.

#### Scenario: Reading a long header value
- **WHEN** the user clicks the value of a long `user-agent` header
- **THEN** a read-only editor opens over the cell showing the full value, and the row heights of the table do not change

#### Scenario: Resizing the name column
- **WHEN** the user drags the boundary between the name and value columns
- **THEN** the name column width changes within limits for that table, and every row follows

### Requirement: Localized TLS and certificate details
TLS and certificate rows SHALL use localized labels: ALPN, Cipher Suite, Supported TLS Versions, Supported ALPNs, Supported Cipher Suites, SHA1 Fingerprint, and SHA256 Fingerprint. List-valued rows SHALL show one item per line.

#### Scenario: Chinese interface
- **WHEN** the interface language is Chinese and an HTTPS entry is inspected
- **THEN** every TLS and certificate label is shown in Chinese

#### Scenario: Supported cipher suites
- **WHEN** the client offered several cipher suites
- **THEN** each suite appears on its own line

### Requirement: Response cookie attributes
Response cookies SHALL appear in the compact field table, with an attributes button on each value showing the attribute count. Its popover SHALL show the cookie name and count and a name/value grid of attributes. An attribute with an empty value SHALL show "Empty value". A flag attribute without `=` SHALL show a check mark. A cookie without attributes SHALL show "No attributes". A Raw Set-Cookie section SHALL show the header text with a copy button.

#### Scenario: Flag and empty attributes
- **WHEN** a cookie has `Secure` and `Domain=`
- **THEN** the popover shows a check mark for Secure and "Empty value" for Domain

#### Scenario: Raw header
- **WHEN** the user opens a cookie's attributes
- **THEN** the popover shows the original Set-Cookie line and copies it on request

### Requirement: Image preview zoom and pan
At 100% an image SHALL fit inside the pane without upscaling. Ctrl+wheel SHALL zoom in steps. When the image is zoomed beyond 100%, dragging SHALL pan it with a grab cursor. A bar above the image SHALL show the scale percentage, the format, the dimensions and size, and the "Ctrl + wheel to zoom" hint. The context menu SHALL keep zoom in, zoom out, reset, and save.

#### Scenario: Large image
- **WHEN** a 4000 x 3000 image is previewed in a smaller pane
- **THEN** it is shown whole, scaled to fit, at 100%

#### Scenario: Zoom and pan
- **WHEN** the user holds Ctrl and scrolls up, then drags the image
- **THEN** the scale increases by one step and the image moves with the pointer

### Requirement: Code viewer scroll limit
Read-only body, raw, and console viewers SHALL stop scrolling at the last line, and SHALL NOT draw whitespace markers.

#### Scenario: Scrolling to the end
- **WHEN** the user scrolls to the bottom of a response body
- **THEN** the last line rests at the bottom of the viewer

### Requirement: SSE message list
The SSE view SHALL list messages in a bordered table with a header of ID, Event type, and Data preview, as FlowLens 1.0.5 did, in 44 px rows that show the message's effective ID or `—`, its event type, and the first line of its data or `(empty)`. A click on a row, or Enter on the row the arrow keys moved to, SHALL open its detail; rows SHALL NOT show as selected. Once parsing ends, an incomplete last event SHALL be noted under the list.

#### Scenario: Message before any ID
- **WHEN** the first message of a stream has no ID and the second has ID `7`
- **THEN** the first row's ID cell shows `—` and the second's shows `7`

#### Scenario: Opening a message from the keyboard
- **WHEN** the list has the keyboard focus and the user presses Down, then Enter
- **THEN** the detail of the next message opens

#### Scenario: Incomplete last event
- **WHEN** the body ends with an incomplete event
- **THEN** a note under the list says so

### Requirement: WebSocket message rows
The WebSocket message list, in the detail panel and the WebSocket Client, SHALL show each message as FlowLens 1.0.5 did: a 44 px row inside a bordered box, with a direction arrow, a Send or Receive tag, and the text, or the size of a binary message. Send SHALL be amber and Receive blue, and a tag SHALL draw its text on a 12% tint of its direction's color.

#### Scenario: Received text message in the list
- **WHEN** the stream holds a received text message
- **THEN** its row shows a blue down arrow, a Receive tag, and the text, and no size

### Requirement: SSE message detail content
The SSE message detail SHALL show the message's sequence number and event type as badges and its effective ID, then the Data, Formatted, and Raw Event views. The Formatted view SHALL highlight JSON.

#### Scenario: Opening a message
- **WHEN** the user opens SSE message 4 of event type `update` with ID `abc`
- **THEN** the dialog shows a `#4` badge, an `update` badge, and `ID: abc`

#### Scenario: Message without an ID
- **WHEN** the opened message has no effective ID
- **THEN** the dialog shows `ID: —`

### Requirement: SSE message file names
Saving an SSE message SHALL suggest `sse-message.txt` from the Data view, `sse-message.json` from the Formatted view, and `sse-event.txt` from the Raw Event view.

#### Scenario: Saving the raw event
- **WHEN** the Raw Event view is shown and the user chooses Save
- **THEN** the save dialog suggests `sse-event.txt`

### Requirement: WebSocket message detail content
The WebSocket message detail SHALL show a direction badge (Send in the warning color, Receive in the info color), a Text or Binary File badge, and the message size. Text messages SHALL show their text, and binary messages a hex view.

#### Scenario: Received text message
- **WHEN** the user opens a received text message of 12 bytes
- **THEN** the dialog shows an info Receive badge, a Text badge, and the size, with the text below

#### Scenario: Sent binary message
- **WHEN** the user opens a sent binary message
- **THEN** the dialog shows a warning Send badge and a Binary File badge, with a hex view below

### Requirement: WebSocket message file names
Saving a WebSocket message SHALL suggest `websocket-message.txt` for a text message and `websocket-message.bin` for a binary message. Exporting a hex selection from the dialog SHALL suggest `websocket-selection.bin`.

#### Scenario: Exporting a hex selection
- **WHEN** the user selects bytes in a binary message's hex view and chooses Export
- **THEN** the save dialog suggests `websocket-selection.bin`

### Requirement: Empty WebSocket message stream
A WebSocket message stream with no messages to show, in the detail panel or the WebSocket Client, SHALL say "No messages".

#### Scenario: Direction filter hides every message
- **WHEN** the stream holds only received messages and the filter shows Send only
- **THEN** the stream shows the "No messages" empty state

#### Scenario: No body loaded
- **WHEN** the detail panel shows a WebSocket entry whose body is not loading and was not loaded
- **THEN** the WebSocket tab shows the stream's toolbar and "No messages"

### Requirement: Explicit traffic detail panel
The panel SHALL show the focused entry with applicable request tabs Overview, Raw, Headers, Query, Cookies, Body and response tabs Raw, Headers, Cookies, Trailers, Body. Enter, an unmodified double-click in a traffic table, or its Show action SHALL open details. Selection alone SHALL NOT open a closed panel. A visible, keyboard-operable close action SHALL hide details without clearing row selection and return focus to the originating table.

#### Scenario: Failed exchange
- **WHEN** the selected exchange failed before a response arrived
- **THEN** the response side shows the error tab

#### Scenario: WebSocket entry
- **WHEN** the selected entry is a WebSocket exchange
- **THEN** a WebSocket tab lists its messages

#### Scenario: Live entry
- **WHEN** the selected entry receives new response data
- **THEN** the panel updates without losing the active applicable tab

#### Scenario: Opening from the keyboard
- **WHEN** a traffic table has focus and the user presses Enter with a valid selected focus row
- **THEN** the panel opens for that row, including when other rows are selected

#### Scenario: Enter while editing text
- **WHEN** an input, editor, or modal owns keyboard focus
- **THEN** its Enter behavior does not open traffic details behind it

#### Scenario: Additive click does not toggle details
- **WHEN** the user Ctrl-clicks on Windows/Linux or Cmd-clicks on macOS to change selection
- **THEN** the modifier changes selection without acting as a panel visibility toggle; an empty selection still closes details

#### Scenario: Show item
- **WHEN** one row is selected and the user chooses Show from its context menu
- **THEN** the panel opens for that row; Show remains unavailable for multiple selection

#### Scenario: Closing explicitly
- **WHEN** the user activates the panel's close action
- **THEN** the panel hides, selected entry IDs and count remain intact, and focus returns to its table

#### Scenario: Selecting after close
- **WHEN** the panel was explicitly closed and the user selects another row
- **THEN** it stays closed until an explicit open action

#### Scenario: Opening another row
- **WHEN** details are open and the user selects a different row or opens its details
- **THEN** the panel shows the current focus row without retaining a late body result from the previous entry

#### Scenario: Empty selection
- **WHEN** all selected rows are removed from selection or leave the visible source
- **THEN** no stale entry remains displayed and the panel closes

#### Scenario: Native double-click opens details
- **WHEN** the user double-clicks a traffic row without modifiers
- **THEN** details open for that row; rapid repeated modifier clicks only change selection

### Requirement: Byte-exact body viewing
The body viewer SHALL show, copy, and save a captured or historical body as its exact stored bytes. As before, a body SHALL be treated as binary when it has a binary or multipart content type, an unsupported content encoding, or invalid UTF-8: its Raw view shows the binary hint, and its Hex view and saved file hold its bytes.

#### Scenario: Binary body in Hex view
- **WHEN** the user opens the Hex view of a binary response body
- **THEN** the bytes are shown at once, without a loading state, and match the captured payload byte for byte

#### Scenario: Saving a binary body
- **WHEN** the user saves a binary request or response body
- **THEN** the written file is identical to the captured payload

#### Scenario: Image preview
- **WHEN** an image response is shown in the Image view
- **THEN** the picture is decoded from the body's bytes, and its displayed size is the byte count of the body

#### Scenario: Body with invalid UTF-8
- **WHEN** a `text/plain` body contains bytes that are not valid UTF-8
- **THEN** its Raw view shows the binary hint, and its Hex view shows the original bytes, not replacement characters

### Requirement: Live body growth
A live SSE body in the detail panel SHALL grow by the data received, in offset order, without gaps or repeated data. A body first loaded as binary SHALL switch to text when SSE data arrives, as before.

#### Scenario: Streaming SSE response
- **WHEN** an SSE response receives chunks while its entry is shown
- **THEN** the body and the SSE list extend with each chunk in order, and the Hex view shows the bytes received so far

#### Scenario: Out-of-order delivery
- **WHEN** a live chunk arrives whose offset is past the end of the body shown
- **THEN** the panel reloads the body snapshot instead of showing a body with a gap

#### Scenario: Long-running stream
- **WHEN** an SSE response shown in the detail panel has already received 50 MiB and another chunk arrives
- **THEN** both the retained body and the displayed text append the new data without rebuilding or copying the already displayed body on each update, and read-only streaming updates do not accumulate undo history

#### Scenario: Text and line-ending boundaries
- **WHEN** SSE chunks cross UTF-8 or CRLF boundaries
- **THEN** the text and SSE views show the same characters and line breaks as a complete snapshot, and Hex retains the exact received bytes

#### Scenario: Replacing the stream snapshot
- **WHEN** the selected entry, body encoding, or previously displayed stream content changes
- **THEN** the viewers replace their previous content and subsequent chunks extend the replacement without retaining or appending the previous stream

### Requirement: Byte-exact WebSocket messages
Binary WebSocket messages, in captured traffic and in WebSocket Client, SHALL be shown, sized, and saved as their exact payload bytes.

#### Scenario: Opening a binary message
- **WHEN** the user opens a received binary WebSocket message
- **THEN** its hex view shows the payload bytes and its size badge shows their count

#### Scenario: Saving a binary message
- **WHEN** the user saves a binary WebSocket message
- **THEN** `websocket-message.bin` is written with the payload bytes

#### Scenario: Exporting a hex selection
- **WHEN** the user exports a selected range from a binary message's hex view
- **THEN** the file holds exactly the selected bytes

### Requirement: Base64 only in requested outputs
Base64 SHALL appear only where the user asks for an output format that needs it. Hex "Copy as Base64" SHALL copy the standard Base64 of the selected bytes. "Copy as cURL" SHALL render a binary request body as a `base64 -d` pipeline with `--data-binary @-`.

#### Scenario: Copying a selection as Base64
- **WHEN** the user selects bytes in Hex view and chooses Copy as Base64
- **THEN** the clipboard holds the padded standard Base64 encoding of exactly those bytes

#### Scenario: Copying a binary request as cURL
- **WHEN** the user copies an entry with a binary request body as cURL
- **THEN** the command pipes the Base64 of the body through `base64 -d` into `curl ... --data-binary @-`, and running it sends the original bytes

#### Scenario: Copying a text request as cURL
- **WHEN** the user copies an entry with a text request body as cURL
- **THEN** the command passes the body with `--data-raw` and contains no Base64
