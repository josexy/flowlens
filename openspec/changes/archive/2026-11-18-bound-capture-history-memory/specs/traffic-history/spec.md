# Spec Delta

## ADDED Requirements

### Requirement: History memory release
Decoded history entries and bodies SHALL be retained only for the open history. Closing or replacing it SHALL release its details, viewer caches, filters, table rows, and categories. The service MAY cache compact entry indexes for at most four recently accessed histories; listing, refreshing, and startup cleanup SHALL NOT load additional entry indexes.

#### Scenario: Closing the history tab
- **WHEN** the user closes the history tab of a history with 100000 entries
- **THEN** its entries, details, filter results, table rows, and category tree are released and the capture table is shown

#### Scenario: Many saved histories
- **WHEN** 100 saved histories of 50000 entries each exist and the user starts FlowLens or refreshes the history list
- **THEN** no history's entries or entry index are kept in memory until that history is opened or exported

#### Scenario: Reopening a closed history
- **WHEN** the user opens a history again after closing its tab
- **THEN** it shows the same entries and details as before

#### Scenario: Opening another history
- **WHEN** the user opens a second history while the first is open
- **THEN** the first history's decoded entries, bodies, and viewer data are released, while its compact index may remain in the bounded cache

#### Scenario: Closing after viewing a large body
- **WHEN** the user closes a history after viewing its body, Raw, Hex, SSE, WebSocket, or image details
- **THEN** the hidden detail viewers retain none of those payloads, and pending decoding or parsing results cannot repopulate them

#### Scenario: Accessing more than four saved histories
- **WHEN** opening or exporting another history would cache a fifth entry index
- **THEN** the least recently used index is evicted and the cache contains at most four indexes
