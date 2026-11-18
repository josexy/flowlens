# Spec Delta

## ADDED Requirements

### Requirement: Adaptive memory statistics layout
Memory Statistics SHALL adapt its toolbar, summary cards, and metric groups to the available content width. Monitoring controls SHALL remain reachable and values readable at supported window sizes, sidebar widths, languages, and configured font sizes. Reducing the content width SHALL wrap or stack content without requiring hidden horizontal actions.

#### Scenario: Minimum main window with expanded sidebar
- **WHEN** the main window is 1024 by 768 and its secondary sidebar occupies 42 percent of the workbench area
- **THEN** start/pause, interval, refresh, and clear remain reachable, and metric groups use enough rows to keep labels and values readable

#### Scenario: Wider content
- **WHEN** the user reduces the sidebar width or expands the window to 1600 by 860
- **THEN** summary and metric groups use additional columns when they fit without changing their information or order

#### Scenario: Theme, language, and font variation
- **WHEN** the user switches between English and Chinese, light and dark themes, or supported interface font sizes
- **THEN** controls stay operable and text is not obscured by adjacent columns

### Requirement: Layout changes preserve monitoring state
Resizing the Memory Statistics surface or changing its sidebar visibility SHALL preserve monitoring status, interval, retained samples, and chart series visibility. Layout changes SHALL NOT implicitly restart monitoring, clear samples, or request an extra sample.

#### Scenario: Resize during monitoring
- **WHEN** the user resizes the page while monitoring is active
- **THEN** monitoring continues at the existing interval with its retained samples

#### Scenario: Resize while paused
- **WHEN** the user changes sidebar width while monitoring is paused and a chart series is hidden
- **THEN** monitoring remains paused, the samples remain, and the series stays hidden
