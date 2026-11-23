# Spec Delta

## MODIFIED Requirements

### Requirement: Bounded high-frequency events
Pending high-frequency events SHALL be bounded, with events beyond the bound dropped and counted per event kind.

#### Scenario: Burst exceeds the bound
- **WHEN** traffic events are published faster than the interface consumes them and the pending limit is reached
- **THEN** further high-frequency events are dropped, the drop count is reported, and process memory stays within the limit

#### Scenario: Large payloads reach the byte limit first
- **WHEN** pending live body chunks reach the 4 MiB pending byte limit before the 512-event limit
- **THEN** further high-frequency events are dropped and counted for their kind, while a single event larger than the byte limit is dropped rather than delivered

#### Scenario: Recovering after drops
- **WHEN** the interface receives a report of dropped traffic events
- **THEN** it reconciles its list against a backend snapshot and converges on the backend's entries

#### Scenario: Slow interface
- **WHEN** the interface has not finished applying the previous batch of events
- **THEN** no further batch is handed to it, and events published meanwhile stay within the pending limit or are dropped and counted

#### Scenario: Prolonged interface stall
- **WHEN** the interface leaves a batch unapplied for more than two seconds while capture continues
- **THEN** elapsed time does not allow another batch into the interface queue, and both the pending event count and pending byte limits still apply

#### Scenario: Shutdown with an unapplied batch
- **WHEN** event delivery stops while the interface still holds an unapplied batch
- **THEN** shutdown returns without waiting for that batch, releases pending payloads, and does not enqueue another batch behind it

## ADDED Requirements

### Requirement: Operation-scoped request streams
Events of an HTTP Request Editor stream or a WebSocket Client session SHALL reach only the send or connection that started them, in publication order. Events of an operation that was stopped, replaced, or whose tab closed SHALL be discarded.

#### Scenario: Data before the send returns
- **WHEN** a streaming response publishes chunks before the call that started the send has returned to the interface
- **THEN** the chunks appear in the tab that started the send, in order, and none are lost

#### Scenario: Concurrent streams in two tabs
- **WHEN** two editor tabs stream responses at the same time
- **THEN** each tab shows only its own response body, and the end of one stream does not end the other

#### Scenario: Stopped stream
- **WHEN** the user stops a stream while its chunks are still pending delivery
- **THEN** the body keeps what was shown when Stop took effect, later chunks of that stream are discarded, and the tab is no longer streaming

#### Scenario: Sending again after Stop
- **WHEN** the user stops a stream and sends again from the same tab before the stopped stream's remaining events have been delivered
- **THEN** those events are discarded, and the tab shows only the new response

#### Scenario: Tab closed during a stream or session
- **WHEN** the user closes a tab while its stream or WebSocket session is active
- **THEN** the stream or session is disconnected, its later events are discarded without error, and no other tab changes

#### Scenario: WebSocket messages before the connection returns
- **WHEN** a WebSocket Client session receives messages before the connect call has returned to the interface
- **THEN** the session shows as connected and lists those messages in order

#### Scenario: WebSocket session closed by the server
- **WHEN** the server closes a WebSocket Client session
- **THEN** the tab shows the session as closed, further sends are refused, and late events of that session are discarded

### Requirement: Batched plugin console output
Python plugin output SHALL reach the request editor console in ordered batches applied in one interface update per 16 ms flush interval, not one update per output line, and SHALL NOT be dropped before it reaches the console.

#### Scenario: Burst of plugin output
- **WHEN** a plugin writes several thousand lines within one send
- **THEN** the console receives every line in order, subject only to its existing entry limit, and the window applies them in batches rather than redrawing once per line

#### Scenario: Output for another send
- **WHEN** plugin output arrives for an execution that no open tab is running
- **THEN** the output is discarded without changing any console

#### Scenario: Output during shutdown
- **WHEN** plugin output is still pending as event delivery stops
- **THEN** shutdown does not wait for it and no closed window is updated
