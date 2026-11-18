# Spec Delta

## Purpose

Defines the memory statistics view that shows live Go runtime memory, garbage collection, and environment information for the running application.

## ADDED Requirements

### Requirement: Monitoring control
The Memory Statistics section SHALL start, pause, and refresh monitoring at a user-selected interval and clear collected data.

#### Scenario: Changing the interval while running
- **WHEN** the user selects another interval during monitoring
- **THEN** monitoring continues at the new interval

#### Scenario: No data
- **WHEN** no samples have been collected
- **THEN** a no-data state is shown

#### Scenario: Clearing data
- **WHEN** the user clears data
- **THEN** the chart and history of samples are emptied

### Requirement: Memory usage chart
The view SHALL chart memory usage over time, updating as samples arrive.

#### Scenario: New sample
- **WHEN** a sample arrives during monitoring
- **THEN** the chart extends with the new values

#### Scenario: Long monitoring session
- **WHEN** monitoring runs long enough to accumulate many samples
- **THEN** the chart remains responsive and memory used by retained samples stays bounded

### Requirement: Runtime metrics
The view SHALL show heap, stack, overall allocation, garbage collection, and runtime metrics, including Go version, OS and architecture, and CPU core count.

#### Scenario: No garbage collection yet
- **WHEN** no garbage collection has run
- **THEN** the last GC time shows a no-GC-yet state

### Requirement: Monitoring lifecycle
Monitoring SHALL stop when the application shuts down.

#### Scenario: Shutdown while monitoring
- **WHEN** the application quits during monitoring
- **THEN** monitoring stops before exit
