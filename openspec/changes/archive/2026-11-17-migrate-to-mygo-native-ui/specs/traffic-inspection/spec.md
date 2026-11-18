# Spec Delta

## Purpose

Defines how a selected traffic entry is inspected, including its metadata, ordered headers, cookies, bodies, hex data, SSE and WebSocket messages, certificates, timing, and process details.

## ADDED Requirements

### Requirement: Traffic detail panel
Selecting an entry SHALL open a detail panel with request tabs Overview, Raw, Headers, Query, Cookies, and Body, and response tabs Raw, Headers, Cookies, Trailers, and Body, showing only the tabs that apply to the entry.

#### Scenario: Failed exchange
- **WHEN** the selected exchange failed before a response arrived
- **THEN** the response side shows the error tab

#### Scenario: WebSocket entry
- **WHEN** the selected entry is a WebSocket exchange
- **THEN** a WebSocket tab lists its messages

#### Scenario: Live entry
- **WHEN** the selected entry receives new response data
- **THEN** the panel updates without losing the active tab

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
