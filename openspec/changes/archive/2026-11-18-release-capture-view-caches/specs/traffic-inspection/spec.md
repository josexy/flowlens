## ADDED Requirements

### Requirement: Inspection cache release
Capture and history inspection SHALL release obsolete payloads and derived viewer caches when their selection changes, is removed, or details close. Clearing or restarting a capture SHALL also release its derived lists, including hidden views. Late background results SHALL NOT restore discarded caches. Reopening details SHALL load the selected record normally and preserve viewing preferences.

#### Scenario: Clearing after inspecting a large response
- **WHEN** the user clears or restarts a capture after inspecting a large response
- **THEN** its body, Raw, formatted, Hex, image, SSE, and WebSocket viewer data and derived lists are released without requiring another frame of that view

#### Scenario: Closing or changing details
- **WHEN** the user closes details, selects another entry, or deletes the selected entry in capture or history
- **THEN** caches of the previous detail are released, closing keeps row selection, and reopening shows the selected record

#### Scenario: Obsolete background result
- **WHEN** an image or SSE parsing result arrives after its detail was discarded
- **THEN** it cannot restore the discarded viewer data

#### Scenario: Replacing a body or closing a message dialog
- **WHEN** a body is replaced or an SSE or WebSocket message dialog closes
- **THEN** obsolete derived body or dialog payload caches are released while the current body and message list remain available
