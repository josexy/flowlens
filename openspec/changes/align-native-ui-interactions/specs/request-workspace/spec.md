# Spec Delta

## MODIFIED Requirements

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
