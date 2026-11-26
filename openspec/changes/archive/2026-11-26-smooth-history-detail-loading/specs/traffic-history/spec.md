## ADDED Requirements

### Requirement: Staged history inspection
History inspection SHALL display complete metadata without waiting for body decoding. Bodies SHALL load when Raw, Body, or WebSocket inspection requires them. Complete request/action inputs, HBIN v1-v3 compatibility, exact content, cancellation, and selection cleanup SHALL remain intact.

#### Scenario: Inspecting headers with a large body
- **WHEN** the user opens a history entry in Overview or Headers
- **THEN** complete metadata is available without loading its body
- **AND** switching to a body-dependent view starts at most one payload read for the selection

#### Scenario: Body read fails
- **WHEN** body decoding fails after metadata is available
- **THEN** the readable metadata remains visible and the existing error path reports the failure
- **AND** redraws do not repeatedly restart the failed read; closing and reopening permits another attempt

#### Scenario: Superseded read
- **WHEN** the selected record changes, details close, or the history closes during either read stage
- **THEN** obsolete work is canceled and late results cannot repopulate the detail or its viewers

#### Scenario: Full record actions
- **WHEN** the user copies cURL, edits, resends, or exports without viewing a body first
- **THEN** the action still receives the complete stored input independently of the inspection load state
