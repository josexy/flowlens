## ADDED Requirements

### Requirement: Stable detail loading transitions
Pending history metadata SHALL preserve the detail split and tab-bar layout without showing previous record content as current or treating summary fields as complete. Loading indicators SHALL appear only after a 180 ms wait and disappear immediately when content is ready. Closing SHALL remain available throughout loading.

#### Scenario: Fast history selection
- **WHEN** a history detail read completes before 180 ms
- **THEN** the panel keeps its layout and shows the result without an intervening spinner

#### Scenario: Slow history selection
- **WHEN** metadata remains pending after 180 ms
- **THEN** loading indicators appear inside the stable panes, pending tabs are noninteractive, and close still works

#### Scenario: Waiting for a body
- **WHEN** Raw, Body, or WebSocket requires an unfinished payload read
- **THEN** the outer detail layout and available metadata remain visible, with delayed loading feedback and incomplete Raw copying disabled

#### Scenario: Changing or closing a pending selection
- **WHEN** the user selects another record or closes details before the loading deadline
- **THEN** the old deadline cannot show a stale indicator and old records and body caches are released
