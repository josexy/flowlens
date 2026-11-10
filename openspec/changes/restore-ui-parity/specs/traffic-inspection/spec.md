# Spec Delta

## ADDED Requirements

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
