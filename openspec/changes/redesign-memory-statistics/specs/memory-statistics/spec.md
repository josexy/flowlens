# Spec Delta

## ADDED Requirements

### Requirement: Process memory
The first summary figure, Memory, SHALL show the FlowLens process's own memory, measured the way the platform's standard process tool measures it: private working set on Windows, physical footprint on macOS, and resident memory on Linux. A caption SHALL name the matching tool and column. Memory SHALL NOT include other processes, including Python Workers.

#### Scenario: Comparing with the OS tool
- **WHEN** the user compares Memory with FlowLens's own process in Task Manager's Memory column (Windows), Activity Monitor's Memory column (macOS), or the `RES` column of `top`/`htop` (Linux)
- **THEN** both show the same measure, differing only by sampling time and rounding

#### Scenario: Python Workers running
- **WHEN** Python Workers are running while a sample is taken
- **THEN** their memory is not part of Memory

#### Scenario: Value unavailable
- **WHEN** the platform value cannot be read for a sample
- **THEN** Memory shows the unknown mark with a caption saying the value is unavailable, and no Go runtime value is shown in its place

### Requirement: Heap in use
The second summary figure SHALL show heap in use: the memory held by Go objects, such as captured traffic, caches, and interface state, including objects not yet collected. The summary SHALL show no figures other than Memory and Heap in use.

#### Scenario: Summary contents
- **WHEN** a sample is shown
- **THEN** the summary shows Memory first and Heap in use second, and nothing else

#### Scenario: Before the first sample
- **WHEN** no sample exists
- **THEN** both figures show the unknown mark

### Requirement: Go runtime details
The section SHALL group Go runtime values in a collapsible "Go runtime details" group that starts collapsed. The group SHALL show exactly these values: held by Go runtime, heap in use, kept for reuse, stacks, runtime overhead, returned to the OS, next GC target, GC cycles, last GC with its pause, and goroutines.

#### Scenario: Default state
- **WHEN** the user opens the section for the first time in a run
- **THEN** Go runtime details are collapsed, and the summary and chart are visible without expanding them

#### Scenario: Memory breakdown adds up
- **WHEN** the details are expanded for a sample
- **THEN** heap in use, kept for reuse, stacks, and runtime overhead add up to held by Go runtime in bytes, any difference in the displayed values comes only from rounding, and returned to the OS is shown apart from that total

#### Scenario: No garbage collection yet
- **WHEN** no garbage collection has run
- **THEN** the last GC shows a no-GC-yet state

#### Scenario: Environment information
- **WHEN** the user looks for the Go version or platform
- **THEN** the status bar's Environment dialog shows them, and the Memory Statistics section does not repeat them

## MODIFIED Requirements

### Requirement: Monitoring control
The Memory Statistics section SHALL start, pause, and refresh monitoring at a user-selected interval and clear collected data. The first time the section is shown in a run, it SHALL take one sample if none exists, without starting monitoring.

#### Scenario: Changing the interval while running
- **WHEN** the user selects another interval during monitoring
- **THEN** monitoring continues at the new interval

#### Scenario: First view
- **WHEN** the user shows the section for the first time in a run and no sample exists
- **THEN** one sample is taken and its figures are shown, and monitoring stays paused

#### Scenario: No data
- **WHEN** no samples have been collected
- **THEN** a no-data state is shown

#### Scenario: Clearing data
- **WHEN** the user clears data
- **THEN** the chart and history of samples are emptied, and no sample is taken until the user starts monitoring or refreshes

### Requirement: Memory usage chart
The view SHALL chart Memory and Heap in use over time, updating as samples arrive. Series names SHALL follow the interface language. The view SHALL retain at most 600 samples and drop the oldest first.

#### Scenario: New sample
- **WHEN** a sample arrives during monitoring
- **THEN** the chart extends with the new values

#### Scenario: Long monitoring session
- **WHEN** monitoring runs long enough to accumulate more than 600 samples
- **THEN** the chart shows the newest 600 samples, remains responsive, and memory used by retained samples stays bounded

#### Scenario: Process memory unavailable
- **WHEN** a retained sample has no Memory value
- **THEN** the Memory series has a gap at that time instead of a zero, and Heap in use is still plotted

### Requirement: Layout changes preserve monitoring state
Resizing the Memory Statistics surface or changing its sidebar visibility SHALL preserve monitoring status, interval, retained samples, chart series visibility, and whether Go runtime details are expanded. Layout changes SHALL NOT implicitly restart monitoring, clear samples, or request an extra sample.

#### Scenario: Resize during monitoring
- **WHEN** the user resizes the page while monitoring is active
- **THEN** monitoring continues at the existing interval with its retained samples

#### Scenario: Resize while paused
- **WHEN** the user changes sidebar width while monitoring is paused and a chart series is hidden
- **THEN** monitoring remains paused, the samples remain, and the series stays hidden

#### Scenario: Resize with details expanded
- **WHEN** the user resizes the page or changes sidebar width while Go runtime details are expanded
- **THEN** the details stay expanded and no sample is requested

## REMOVED Requirements

### Requirement: Runtime metrics
**Reason**: The roughly 25 Go runtime values, including duplicates, lifetime counters, and environment details, measure nothing users can compare with their OS tools, and "Total Sys" looked like the process's memory without being it.
**Migration**: The Process memory and Heap in use figures replace the headline values. The trimmed Go values move to the collapsed Go runtime details, and the no-GC-yet scenario moves there too. Go version and platform stay in the status bar's Environment dialog. Mallocs, frees, object counts, cumulative allocation, forced GC count, GC CPU fraction, total GC pause, and CPU core count are no longer shown.
