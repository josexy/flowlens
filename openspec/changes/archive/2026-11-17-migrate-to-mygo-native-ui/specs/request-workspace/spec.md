# Spec Delta

## Purpose

Defines the tabbed workspace for composing and sending HTTP requests and WebSocket sessions, including edits started from captured traffic and per-request Python hooks.

## ADDED Requirements

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
