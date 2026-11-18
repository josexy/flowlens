# Spec Delta

## MODIFIED Requirements

### Requirement: Traffic detail panel
The detail panel SHALL show the focused entry with request tabs Overview, Raw, Headers, Query, Cookies, and Body, and response tabs Raw, Headers, Cookies, Trailers, and Body, showing only the tabs that apply to the entry. As in FlowLens 1.0.5, Ctrl-click (Cmd-click on macOS) on a row and the row menu's Show item SHALL open the panel, and selecting rows alone SHALL NOT. The panel SHALL NOT have its own close button.

#### Scenario: Failed exchange
- **WHEN** the selected exchange failed before a response arrived
- **THEN** the response side shows the error tab

#### Scenario: WebSocket entry
- **WHEN** the selected entry is a WebSocket exchange
- **THEN** a WebSocket tab lists its messages

#### Scenario: Live entry
- **WHEN** the selected entry receives new response data
- **THEN** the panel updates without losing the active tab

#### Scenario: Opening with Ctrl-click
- **WHEN** the panel is closed and the user Ctrl-clicks (Cmd-clicks on macOS) a row
- **THEN** that row is focused and the panel opens showing it

#### Scenario: Closing with Ctrl-click
- **WHEN** the panel shows row 5 and the user Ctrl-clicks row 5
- **THEN** the panel closes and row 5 stays selected

#### Scenario: Ctrl-click on another row
- **WHEN** the panel shows row 5 and the user Ctrl-clicks row 7
- **THEN** the panel stays open and shows row 7

#### Scenario: Show item
- **WHEN** one row is selected and the user chooses Show from its context menu
- **THEN** the row is focused and the panel opens showing it, and the item is unavailable while several rows are selected

#### Scenario: No close button
- **WHEN** the panel is open
- **THEN** the response tab strip ends with the response summary chips and offers no close button

## ADDED Requirements

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

### Requirement: WebSocket conversation bubbles
The WebSocket conversation SHALL show each message as FlowLens 1.0.5 did: a bubble tinted in its direction's color, with its tag above its text, or above a button with the size of a binary message that opens it.

#### Scenario: Binary message in the conversation
- **WHEN** the user clicks the size button of a binary message in the conversation
- **THEN** its detail opens

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
