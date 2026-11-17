# Spec Delta

## ADDED Requirements

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
