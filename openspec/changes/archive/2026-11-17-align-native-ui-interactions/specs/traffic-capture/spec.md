# Spec Delta

## REMOVED Requirements

### Requirement: Row selection
**Reason**: The 1.0.5 Shift-toggle and Ctrl/Cmd-click model overrode the toolkit's native selection; this change returns selection to MyGo.
**Migration**: Use Native row selection. Shift-click now selects a range and Ctrl/Cmd-click toggles a row.

## ADDED Requirements

### Requirement: Native row selection
Traffic tables SHALL use conventional native single, additive, range, drag, and keyboard selection. Selecting rows SHALL NOT itself open a closed detail panel. Selection SHALL track entry identity through table changes, exclude removed entries, and report its current count. An interrupted pointer gesture SHALL NOT leave selection or keyboard navigation stuck. Opening another saved session SHALL clear its previous selection, focus, and details.

#### Scenario: Click selects without opening details
- **WHEN** the detail panel is closed and the user clicks a row
- **THEN** that row alone is selected and focused, and the panel stays closed

#### Scenario: Click while details are open
- **WHEN** the detail panel is open and the user clicks another row
- **THEN** that row alone is selected and the panel shows it

#### Scenario: Clicking the only selected row
- **WHEN** exactly one row is selected and the user clicks it without a modifier
- **THEN** it remains selected and the detail panel's visibility does not change

#### Scenario: Additive selection
- **WHEN** row 2 is selected and the user Ctrl-clicks row 5 on Windows/Linux or Cmd-clicks it on macOS
- **THEN** rows 2 and 5 are selected, row 5 is focused, and a closed panel stays closed

#### Scenario: Removing a selected row
- **WHEN** rows 2 and 5 are selected and the user applies the additive modifier to row 5 again
- **THEN** row 5 leaves the selection and row 2 remains available for detail inspection

#### Scenario: Removing the last selected row
- **WHEN** the user removes the last selected row with the additive modifier
- **THEN** the count becomes zero and no entry remains displayed in the detail panel

#### Scenario: Shift selects a range
- **WHEN** row 2 is the selection anchor and the user Shift-clicks row 5
- **THEN** the inclusive range from row 2 through row 5 in current table order is selected

#### Scenario: Drag replaces the selection
- **WHEN** the user drags from row 4 to row 6 without modifiers
- **THEN** rows 4 through 6 are selected according to the native drag gesture and detail visibility does not change

#### Scenario: Quick drag
- **WHEN** a press and movement across several rows occur before the next drawn frame
- **THEN** the selection extends to the latest drag position and does not revert to the earlier selection

#### Scenario: Release outside the table
- **WHEN** a pointer gesture ends outside the table and the user returns to keyboard navigation
- **THEN** the next keyboard selection change succeeds without a delayed pointer action reverting it

#### Scenario: Window loses focus during a press
- **WHEN** the window loses focus during a press and the user subsequently returns to the table
- **THEN** keyboard movement and Select All continue to operate

#### Scenario: Drag selection near the edge
- **WHEN** the user drags a selection to the table's edge
- **THEN** the table scrolls and extends the selection

#### Scenario: Keyboard selection
- **WHEN** the table has focus and the user presses Down, Shift+Down, or the platform Select All binding
- **THEN** native row movement, range extension, or Select All occurs and a closed detail panel stays closed

#### Scenario: Selected entry evicted
- **WHEN** a selected entry leaves the retained traffic set
- **THEN** it leaves the selection and the displayed selection count updates

#### Scenario: Focused entry removed
- **WHEN** the inspected entry is filtered out, deleted, evicted, or deselected while other selected rows remain
- **THEN** inspection moves to the first remaining selected row in table order; if none remain, details close

#### Scenario: Sorting preserves identity
- **WHEN** the user sorts the table with selected rows
- **THEN** those entry IDs remain selected and a subsequent range gesture uses the displayed order

#### Scenario: Opening another saved session
- **WHEN** the user switches from a selected saved session to another session
- **THEN** no row from the previous session remains selected or focused and its details are closed

#### Scenario: Context menu on a selected group
- **WHEN** the user opens a row context menu within a multiple selection
- **THEN** applicable bulk actions retain that selection; header context menus continue to target columns
