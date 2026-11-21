# Spec Delta

## MODIFIED Requirements

### Requirement: Body viewing modes
The body viewer SHALL classify bodies as JSON, XML, HTML, JavaScript, CSS, text, image, SVG, or binary and offer the applicable Formatted, Image, Raw, SSE, and Hex views. Raw and Formatted SHALL be offered only for bodies available as text. An unavailable active mode SHALL fall back to an applicable mode, preferring Image for image bodies and Hex for ordinary binary bodies.

#### Scenario: JSON body
- **WHEN** a JSON response is viewed
- **THEN** the Formatted view shows indented JSON with syntax highlighting

#### Scenario: Image body
- **WHEN** an image response in PNG, JPEG, GIF, WebP, or BMP format, or an SVG, is viewed
- **THEN** the Image view shows the picture
- **AND** binary image bodies offer Image and Hex without Raw or Formatted, while textual SVG retains Raw and applicable Formatted access

#### Scenario: Undecodable image
- **WHEN** an image body cannot be decoded
- **THEN** a message is shown and Hex remains available; Raw remains available only when the body is available as text

#### Scenario: HTML body
- **WHEN** an HTML response is viewed
- **THEN** its source is shown with syntax highlighting and is not executed or rendered as a page

#### Scenario: Ordinary binary body
- **WHEN** a binary body without an image or SSE view is opened
- **THEN** only Hex is offered and selected, with saving still available for nonempty content

#### Scenario: Switching from text to binary
- **WHEN** Raw or Formatted is selected and the viewer receives a binary body
- **THEN** the unavailable text modes disappear and the viewer selects an applicable mode without rendering a binary placeholder in a text editor

#### Scenario: Returning to text
- **WHEN** a binary body is replaced by text, including a live SSE snapshot becoming text
- **THEN** Raw becomes available again, applicable Formatted access returns, and a still-applicable selected mode is retained unless a newly available image or SSE view takes priority

#### Scenario: Text availability differs from the declared type
- **WHEN** a body is marked unavailable as text despite declaring a text or structured-text content type
- **THEN** Raw and Formatted are hidden; a body available as text retains Raw even if its declared type normally describes binary content

### Requirement: Byte-exact body viewing
The body viewer SHALL show, copy, and save a captured or historical body as its exact stored bytes. As before, a body SHALL be treated as binary when it has a binary or multipart content type, an unsupported content encoding, or invalid UTF-8: its body Raw and Formatted views are unavailable, and its Hex view and saved file hold its bytes.

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
- **THEN** its body Raw view is hidden, and Hex opens with the original bytes, not replacement characters
