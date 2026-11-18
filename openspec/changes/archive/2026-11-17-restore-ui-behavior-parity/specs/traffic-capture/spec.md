# Spec Delta

## MODIFIED Requirements

### Requirement: Row selection
The table SHALL select rows as FlowLens 1.0.5 did: a click selects one row, Shift-click adds or removes one row, Ctrl-click (Cmd-click on macOS) on a row that is not selected selects that row alone, and dragging selects the range from the pressed row to the row under the pointer with automatic scrolling near the edges. Selecting rows SHALL NOT open or close the detail panel, except where the traffic-inspection capability says so. A press whose release never reaches the table, as one released outside it or one the window loses the focus during, SHALL click no row and SHALL leave the keys free to change the selection. Opening another saved session SHALL clear the selection and the focus, as 1.0.5 did.

#### Scenario: Click selects without opening details
- **WHEN** the detail panel is closed and the user clicks a row
- **THEN** that row alone is selected and focused, and the detail panel stays closed

#### Scenario: Click while details are open
- **WHEN** the detail panel is open and the user clicks another row
- **THEN** that row alone is selected and the panel shows it

#### Scenario: Clicking the only selected row
- **WHEN** exactly one row is selected and the user clicks it
- **THEN** the selection is cleared, no entry is focused, and the detail panel closes

#### Scenario: Shift-click adds a row
- **WHEN** rows 2 and 5 are selected and the user Shift-clicks row 8
- **THEN** rows 2, 5, and 8 are selected, not the range between them, and row 8 is focused

#### Scenario: Shift-click removes the focused row
- **WHEN** rows 2, 5, and 8 are selected, row 8 is focused, and the user Shift-clicks row 8
- **THEN** rows 2 and 5 stay selected and the first of them in table order is focused

#### Scenario: Shift-click removes the last row
- **WHEN** one row is selected and the user Shift-clicks it
- **THEN** the selection is empty, no entry is focused, and the detail panel closes

#### Scenario: Ctrl-click keeps a multiple selection
- **WHEN** rows 2, 5, and 8 are selected and the user Ctrl-clicks (Cmd-clicks on macOS) row 5
- **THEN** rows 2, 5, and 8 stay selected

#### Scenario: Drag replaces the selection
- **WHEN** rows 2 and 9 are selected and the user drags from row 4 to row 6, with or without a modifier
- **THEN** rows 4 to 6 alone are selected, row 6 is focused, and the detail panel stays as it was

#### Scenario: Quick drag
- **WHEN** the user presses row 3 and moves past the drag threshold to row 6 before the table draws again
- **THEN** rows 3 to 6 are selected

#### Scenario: Release outside the table
- **WHEN** the user presses a row, releases the button just outside the table without dragging, and presses Down
- **THEN** the press clicked no row, and Down moves the selection

#### Scenario: Window loses the focus during a press
- **WHEN** the window loses the focus while the user presses a row, and the user later presses Down or Cmd+A (Ctrl+A on Windows and Linux) in the table
- **THEN** the press clicked no row, and the keys change the selection

#### Scenario: Drag selection near the edge
- **WHEN** the user drags a selection to the edge of the table
- **THEN** the table scrolls and extends the selection

#### Scenario: Keyboard selection
- **WHEN** the table has the keyboard focus and the user presses Down
- **THEN** the next row alone is selected and focused, and the detail panel stays as it was

#### Scenario: Selected entry evicted
- **WHEN** a selected entry is removed because of the retained limit
- **THEN** it is removed from the selection and the status bar count updates

#### Scenario: Focused entry removed
- **WHEN** the focused entry leaves the table while other rows stay selected
- **THEN** the first row still selected in table order is focused

#### Scenario: Opening another saved session
- **WHEN** rows of a saved session are selected and the user opens another saved session
- **THEN** no row is selected, no entry is focused, and the detail panel is closed

## ADDED Requirements

### Requirement: Selection export file name
Exporting selected rows SHALL use the base name `<alias>-selection` when the table shows a saved history that has an alias, and `flowlens-selection-<count>` otherwise. The format's usual suffix and extension SHALL follow the base name, as for other exports.

#### Scenario: History with an alias
- **WHEN** the table shows a saved history aliased "login-flow" and the user exports three selected rows as HAR
- **THEN** the save dialog suggests `login-flow-selection.har`

#### Scenario: Live capture
- **WHEN** the table shows live capture and the user exports three selected rows as CSV
- **THEN** the save dialog suggests `flowlens-selection-3-csv.csv`
