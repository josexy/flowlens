## ADDED Requirements

### Requirement: Bounded history scan allocation
Opening a history SHALL skip details not needed by its rows without allocations proportional to those fields' encoded payload sizes. The list SHALL preserve its existing fields and actions, and complete stored details SHALL remain available on demand. Selective reading SHALL preserve HBIN v1-v3 compatibility, validation, cancellation, and file contents.

#### Scenario: Large cookies and certificates
- **WHEN** a history contains large Cookie, Set-Cookie, custom header, trailer, certificate, or unused TLS fields
- **THEN** loading its list does not materialize those payloads as strings, byte arrays, or object collections
- **AND** opening a detail or performing an action still reads the complete stored content

#### Scenario: List fields and older histories
- **WHEN** a valid HBIN v1, v2, or v3 history is opened
- **THEN** all existing rows, first Content-Type values, protocols including TCP selected ALPN, flags, metrics, process information, sorting, filtering, and action availability are preserved
- **AND** full reads preserve header order, duplicates, casing, empty versus missing data, and unknown metrics

#### Scenario: Invalid skipped field
- **WHEN** a skipped field is truncated or a header block has an invalid flag or excessive field count
- **THEN** loading reports the existing decoding failure instead of accepting an incomplete record

#### Scenario: Cancellation while skipping
- **WHEN** a history read is canceled during a large skipped field
- **THEN** the read stops through the existing cancellation path and obsolete results cannot repopulate the list
