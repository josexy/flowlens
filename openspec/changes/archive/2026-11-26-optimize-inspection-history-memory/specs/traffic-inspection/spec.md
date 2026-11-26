## ADDED Requirements

### Requirement: Bounded response text retention
Read-only viewers SHALL release obsolete complete text versions when their content is replaced, including when the viewer is hidden. Request-editor response replacement SHALL invalidate derived viewers of the previous response. Incremental SSE updates SHALL preserve the current stream without accumulating undo history.

#### Scenario: Repeated responses in one editor
- **WHEN** a request tab displays many distinct large responses and then a small response
- **THEN** its viewers retain only the current response's text and derived state, without requiring the tab to close or another frame of a hidden viewer

#### Scenario: Response replaced while another tab is visible
- **WHEN** a previous response body was viewed and a new response arrives while Headers is selected
- **THEN** the previous body's derived viewers are released and selecting Body shows the new response

### Requirement: On-demand text body views
The body viewer SHALL materialize Raw and Formatted text only when needed. Bodies at or above the existing 512 KiB formatted-view threshold SHALL NOT allocate formatted content. Viewing, copying, saving, and switching modes SHALL preserve their existing output, including binary bytes and empty versus unavailable bodies.

#### Scenario: Large JSON displayed as Raw or Hex
- **WHEN** a JSON body of at least 512 KiB is inspected
- **THEN** no formatted text is generated, and Raw, Hex, copy, and save retain their existing behavior

#### Scenario: Switching modes on a small JSON body
- **WHEN** the user switches between Raw and Formatted and copies or saves the result
- **THEN** each mode provides its expected content and retains word-wrap preferences

#### Scenario: SSE grows before Raw is opened
- **WHEN** an SSE stream grows while SSE or Hex is visible and Raw is opened later
- **THEN** Raw shows the complete current stream and subsequent chunks append without rebuilding the preceding text
