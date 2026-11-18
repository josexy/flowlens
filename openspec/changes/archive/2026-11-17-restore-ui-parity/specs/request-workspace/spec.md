# Spec Delta

## ADDED Requirements

### Requirement: Grid key/value editors
The header, query, cookie, URL-encoded, and form-data editors of the HTTP Request Editor, and the header editor of the WebSocket Client, SHALL be bordered grids. Rows are 40 px tall, with a 46 px enable column, key and value columns, and a 46 px remove column. Rows SHALL show a hover background. Each cell SHALL show its text on one line, with a muted placeholder when empty.

#### Scenario: Empty table
- **WHEN** a new HTTP request tab opens its Headers pane
- **THEN** one row is shown, with a checked enable box and muted key and value placeholders inside a bordered grid

### Requirement: Click-to-edit grid cells
Clicking a key or value cell SHALL open an editor over the cell that wraps long text and grows with its content. Enter, with or without Shift, or a click outside SHALL commit the edit without inserting a line break. The key column SHALL be resizable within limits.

#### Scenario: Editing a long value
- **WHEN** the user clicks a value cell holding a 300-character cookie and edits it
- **THEN** the editor grows to show the whole value, and Enter closes it with the new value shown on one line

#### Scenario: Invalid header name
- **WHEN** a header key contains a space and the editor is closed
- **THEN** the key cell shows an error icon whose tooltip explains that the name is invalid

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
