# Spec Delta

## MODIFIED Requirements

### Requirement: Traffic detail panel
The panel SHALL show the focused entry with applicable request tabs Overview, Raw, Headers, Query, Cookies, Body and response tabs Raw, Headers, Cookies, Trailers, Body. Enter, an unmodified double-click in a traffic table, or its Show action SHALL open details. Selection alone SHALL NOT open a closed panel. A visible, keyboard-operable close action SHALL hide details without clearing row selection and return focus to the originating table.

#### Scenario: Failed exchange
- **WHEN** the selected exchange failed before a response arrived
- **THEN** the response side shows the error tab

#### Scenario: WebSocket entry
- **WHEN** the selected entry is a WebSocket exchange
- **THEN** a WebSocket tab lists its messages

#### Scenario: Live entry
- **WHEN** the selected entry receives new response data
- **THEN** the panel updates without losing the active applicable tab

#### Scenario: Opening from the keyboard
- **WHEN** a traffic table has focus and the user presses Enter with a valid selected focus row
- **THEN** the panel opens for that row, including when other rows are selected

#### Scenario: Enter while editing text
- **WHEN** an input, editor, or modal owns keyboard focus
- **THEN** its Enter behavior does not open traffic details behind it

#### Scenario: Additive click does not toggle details
- **WHEN** the user Ctrl-clicks on Windows/Linux or Cmd-clicks on macOS to change selection
- **THEN** the modifier changes selection without acting as a panel visibility toggle; an empty selection still closes details

#### Scenario: Show item
- **WHEN** one row is selected and the user chooses Show from its context menu
- **THEN** the panel opens for that row; Show remains unavailable for multiple selection

#### Scenario: Closing explicitly
- **WHEN** the user activates the panel's close action
- **THEN** the panel hides, selected entry IDs and count remain intact, and focus returns to its table

#### Scenario: Selecting after close
- **WHEN** the panel was explicitly closed and the user selects another row
- **THEN** it stays closed until an explicit open action

#### Scenario: Opening another row
- **WHEN** details are open and the user selects a different row or opens its details
- **THEN** the panel shows the current focus row without retaining a late body result from the previous entry

#### Scenario: Empty selection
- **WHEN** all selected rows are removed from selection or leave the visible source
- **THEN** no stale entry remains displayed and the panel closes

#### Scenario: Native double-click opens details
- **WHEN** the user double-clicks a traffic row without modifiers
- **THEN** details open for that row; rapid repeated modifier clicks only change selection
