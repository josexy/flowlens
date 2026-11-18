# Spec Delta

## MODIFIED Requirements

### Requirement: Live body growth
A live SSE body in the detail panel SHALL grow by the data received, in offset order, without gaps or repeated data. A body first loaded as binary SHALL switch to text when SSE data arrives, as before.

#### Scenario: Streaming SSE response
- **WHEN** an SSE response receives chunks while its entry is shown
- **THEN** the body and the SSE list extend with each chunk in order, and the Hex view shows the bytes received so far

#### Scenario: Out-of-order delivery
- **WHEN** a live chunk arrives whose offset is past the end of the body shown
- **THEN** the panel reloads the body snapshot instead of showing a body with a gap

#### Scenario: Long-running stream
- **WHEN** an SSE response shown in the detail panel has already received 50 MiB and another chunk arrives
- **THEN** applying the chunk costs time and memory in proportion to the chunk, without copying the data already shown
