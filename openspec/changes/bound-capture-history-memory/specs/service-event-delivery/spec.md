# Spec Delta

## MODIFIED Requirements

### Requirement: Bounded high-frequency events
Pending high-frequency events SHALL be bounded, with events beyond the bound dropped and counted per event name.

#### Scenario: Burst exceeds the bound
- **WHEN** traffic events are published faster than the interface consumes them and the pending limit is reached
- **THEN** further high-frequency events are dropped, the drop count is reported, and process memory stays within the limit

#### Scenario: Recovering after drops
- **WHEN** the interface receives a report of dropped traffic events
- **THEN** it reconciles its list against a backend snapshot and converges on the backend's entries

#### Scenario: Slow interface
- **WHEN** the interface has not finished applying the previous batch of events
- **THEN** no further batch is handed to it, and events published meanwhile stay within the pending limit or are dropped and counted
