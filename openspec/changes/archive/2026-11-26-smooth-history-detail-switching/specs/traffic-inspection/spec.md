# Spec Delta

## MODIFIED Requirements

### Requirement: Inspection cache release
Capture and history inspection SHALL release obsolete payloads and derived viewer caches when their selection changes, is removed, or details close. History row changes MAY retain one disabled previous view for at most the 180 ms transition window. Clearing or restarting capture SHALL also release hidden derived lists. Late results SHALL NOT restore discarded caches. Reopening details SHALL load the selected record normally and preserve viewing preferences.

#### Scenario: Clearing after inspecting a large response
- **WHEN** the user clears or restarts a capture after inspecting a large response
- **THEN** its body, Raw, formatted, Hex, image, SSE, and WebSocket viewer data and derived lists are released without requiring another frame of that view

#### Scenario: Closing or changing details
- **WHEN** the user closes details, selects another entry, or deletes the selected entry in capture or history
- **THEN** caches of the previous detail are released, except for the bounded history transition; closing keeps row selection, and reopening shows the selected record
- **AND** closing, deselecting, deleting the selected entry, or replacing the history releases any retained transition immediately, without requiring another frame

#### Scenario: Obsolete background result
- **WHEN** an image or SSE parsing result arrives after its detail was discarded
- **THEN** it cannot restore the discarded viewer data

#### Scenario: Replacing a body or closing a message dialog
- **WHEN** a body is replaced or an SSE or WebSocket message dialog closes
- **THEN** obsolete derived body or dialog payload caches are released while the current body and message list remain available

#### Scenario: Hidden transition expires
- **WHEN** a history transition remains pending for 180 ms while its panel is not rendering
- **THEN** its retained record and viewer caches are released on the main thread without waiting for another panel frame

### Requirement: Stable detail loading transitions
History row changes SHALL retain at most one previous view for up to 180 ms, with content and tabs disabled without dimming and close available. Ready details SHALL replace it immediately; slow reads SHALL release it and show loading feedback in stable panes. Retained data SHALL remain separate from the selected record and its actions. Summary fields SHALL NOT be treated as complete details.

#### Scenario: Fast history selection
- **WHEN** a history detail and any body needed by its active panes finish loading before 180 ms
- **THEN** the old view is replaced in one step without an intermediate blank pane, changed tab counts, missing response chips, or spinner

#### Scenario: Slow history selection
- **WHEN** the selected detail remains pending after 180 ms
- **THEN** the previous view is released, loading indicators appear inside stable panes, pending metadata tabs are noninteractive, and close still works

#### Scenario: Waiting for a body
- **WHEN** Raw, Body, or WebSocket requires an unfinished payload read
- **THEN** the previous view may remain only until the transition deadline; afterwards available new metadata remains visible with loading feedback and incomplete Raw copying disabled
- **AND** Overview and Headers do not request or wait for bodies

#### Scenario: Changing or closing a pending selection
- **WHEN** the user selects another record or closes details before the loading deadline
- **THEN** obsolete reads and deadlines cannot publish stale data or indicators
- **AND** further row changes do not extend the retained view's deadline, and closing immediately releases records and body caches

#### Scenario: Retained content actions
- **WHEN** a previous view is retained during a row change
- **THEN** its tabs, selection/copy, save, dialogs, and other content actions are disabled for pointer and keyboard input, while close remains usable

#### Scenario: Failed replacement
- **WHEN** replacement metadata or its required body fails to load
- **THEN** the retained view is released and existing error handling applies; a body failure leaves the new complete metadata available

#### Scenario: First selection
- **WHEN** details open without a previous displayed record
- **THEN** the existing stable pending layout is shown, its spinner waits 180 ms, and ready content is displayed immediately
