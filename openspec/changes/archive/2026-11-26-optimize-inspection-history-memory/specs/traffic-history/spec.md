## ADDED Requirements

### Requirement: Lightweight history browsing
An opened history SHALL retain only the fields needed for its complete table, filters, categories, and action availability. Full headers, trailers, TLS details, certificates, and bodies SHALL be loaded on demand for inspection or actions. All rows SHALL remain searchable and sortable, and HBIN v1-v3 reading, export ordering, and exact stored content SHALL remain unchanged.

#### Scenario: Opening a large history
- **WHEN** a saved history contains many records with large headers or certificates
- **THEN** all rows are available without retaining those full details for every record

#### Scenario: Inspecting and acting on a summarized row
- **WHEN** the user inspects a row, copies cURL, edits its request, resends it, or exports it
- **THEN** the operation uses the complete stored record with duplicate headers, casing, order, trailers, metrics, process information, and body availability preserved

#### Scenario: Replacing or closing an in-progress history read
- **WHEN** the user changes the selected history or detail, closes the history, or shuts down during a read
- **THEN** obsolete reads are canceled and their results cannot restore discarded data

#### Scenario: Detail read fails
- **WHEN** a selected record can no longer be read
- **THEN** no incomplete summary is presented as a complete record or used to construct a request, and the existing error-reporting path reports the failure
