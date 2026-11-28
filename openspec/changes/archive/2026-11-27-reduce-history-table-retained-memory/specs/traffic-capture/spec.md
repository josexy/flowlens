# Spec Delta

## MODIFIED Requirements

### Requirement: Rendering at capacity
Scrolling the traffic table SHALL stay responsive at the retained entry limit, including while traffic is arriving. In capture and history, visual row construction SHALL be limited to the viewport and bounded overscan. Increasing source size SHALL NOT require constructing every row during a redraw. Data refresh, filtering, and sorting SHALL preserve their complete results and are distinct from viewport construction.

#### Scenario: Scrolling a full table
- **WHEN** the table holds 5000 entries and traffic is arriving
- **THEN** only rows in view and bounded overscan are built, and visual construction follows the visible rows and columns rather than the total entry count
- **AND** incoming updates, reading-position retention, and Show new requests continue to follow their existing contracts

#### Scenario: Large history with the same viewport
- **WHEN** the user views histories containing 5000, 50000, and 100000 entries with the same row content, columns, and viewport dimensions
- **THEN** settled redraws construct comparable numbers of cells without capping the available history rows
- **AND** all rows remain available to search, full-value sorting, native selection, and actions

#### Scenario: A selected record is updated in place
- **WHEN** an existing row receives a new revision under the same identity without changing displayed order
- **THEN** its displayed values and applicable details update while selection, focus, and the reading position remain correct
- **AND** a closed detail panel remains closed

#### Scenario: Sorting or filtering changes displayed order
- **WHEN** a filter or sort changes the visible membership or order while multiple rows are selected
- **THEN** surviving entries remain selected by identity, removed entries leave the selection, and range gestures use the resulting order
- **AND** inspection follows the existing remaining-selection rule and row actions use complete original records

#### Scenario: Enlarged or rearranged table
- **WHEN** the user changes text size or viewport dimensions, horizontally scrolls, or resizes, reorders, or hides columns
- **THEN** visible cells retain complete row backgrounds and markers, correct header and context-menu targeting, and native keyboard and accessibility behavior
- **AND** this behavior is preserved in both themes and interface languages, including an empty table or a table with no visible data columns
