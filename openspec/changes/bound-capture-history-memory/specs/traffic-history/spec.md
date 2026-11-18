# Spec Delta

## ADDED Requirements

### Requirement: History memory release
Saved histories SHALL use memory for their entries only while one is open. Closing the history tab SHALL release the open history's entries, details, filters, table rows, and categories. Saved histories that are not open SHALL hold no per-entry data in memory, whether listed, refreshed, or cleaned up at startup.

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
- **THEN** the first history's entries and per-entry data are released
