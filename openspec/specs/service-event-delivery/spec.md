# service-event-delivery Specification

## Purpose
Defines how backend services operate independently of the interface and how their live events reach it in order, within bounded memory, and without making windows unresponsive.

## Requirements

### Requirement: Headless backend operation
Backend services SHALL be constructible and testable without a desktop session or a graphical toolkit.

#### Scenario: Backend tests on a headless machine
- **WHEN** the backend test suite runs on a Linux machine with no display and with cgo disabled
- **THEN** services start, run, and shut down in tests without opening a window

### Requirement: Ordered live events
Live traffic events SHALL reach the interface in the order they were published.

#### Scenario: Entry followed by its patches
- **WHEN** a traffic entry and later patches to it are published in one flush interval
- **THEN** the interface applies the entry before its patches and in publication order

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

#### Scenario: Prolonged interface stall
- **WHEN** the interface leaves a batch unapplied for more than two seconds while capture continues
- **THEN** elapsed time does not allow another batch into the interface queue, and both the pending event count and pending byte limits still apply

#### Scenario: Shutdown with an unapplied batch
- **WHEN** event delivery stops while the interface still holds an unapplied batch
- **THEN** shutdown returns without waiting for that batch, releases pending payloads, and does not enqueue another batch behind it

### Requirement: Lossless lifecycle events
Events that complete or reset a stream SHALL NOT be dropped by the high-frequency bound.

#### Scenario: Stream end during a burst
- **WHEN** the pending limit is reached and a request stream's end event is published
- **THEN** the end event is delivered after the stream's earlier delivered events

### Requirement: Responsive windows during backend work
Service calls that can block SHALL run off the UI thread so that windows keep responding to input.

#### Scenario: Loading a large body
- **WHEN** the user opens a traffic entry whose body takes seconds to load
- **THEN** the window keeps redrawing and accepting input and shows a loading state until the body arrives

### Requirement: Stale results are discarded
A result for an operation that has been superseded SHALL NOT replace newer state.

#### Scenario: Selection changes during a load
- **WHEN** the user selects entry A and then entry B before A's detail has loaded
- **THEN** B's detail is shown even if A's result arrives later

#### Scenario: Window closes during a load
- **WHEN** a window closes while a backend result is pending for it
- **THEN** the result is discarded without error

### Requirement: Consistent state across windows
A change made in one window or by a backend service SHALL be reflected in every open window without a restart.

#### Scenario: Settings saved
- **WHEN** the user saves settings in the Settings window
- **THEN** the main window uses the saved values

#### Scenario: Local data cleared
- **WHEN** storage cleanup clears local data
- **THEN** every open window drops the cleared entries and cached icons

### Requirement: Clean event shutdown
Event delivery SHALL stop during shutdown without blocking or delivering to closed windows.

#### Scenario: Events during shutdown
- **WHEN** services publish events after shutdown has begun
- **THEN** publishing returns without blocking and no closed window is updated
