# Spec Delta

## ADDED Requirements

### Requirement: Traffic table header style
The traffic table header SHALL have a 32 px band on the elevated background with semibold column titles and no vertical dividers. A hovered title SHALL get a hover background. The sorted column SHALL show an up or down arrow in the accent color. A resize handle SHALL appear in the accent color only while hovered.

#### Scenario: Sorted column
- **WHEN** the user sorts by ID descending
- **THEN** the ID title shows a down arrow in the accent color, and no divider lines separate the titles

### Requirement: Full method and status badges
Method and status badges SHALL show their full text, such as `OPTIONS` or `CONNECT`, at the default column widths. They SHALL use the 1.0.5 tones: each method's color on a 13% tint over the panel background, and the success, info, warning, error, and default status tones.

#### Scenario: Long method name
- **WHEN** an OPTIONS request is shown with default column widths
- **THEN** its badge reads `OPTIONS` with no ellipsis

### Requirement: Inspected and highlighted row markers
The row shown in the detail panel SHALL have a 3 px accent bar at its start and a 1 px accent outline. A highlighted row that is not selected SHALL have its highlight color as a background tint, a 3 px start bar, and a 1 px outline. The tint and outline SHALL be stronger in the dark theme.

#### Scenario: Inspecting a highlighted row
- **WHEN** the user opens a highlighted row in the detail panel
- **THEN** the row shows the selection tint with the accent bar and outline in place of the highlight marker

### Requirement: Aligned numeric columns
Duration and size cells SHALL use tabular numerals, so values line up across rows.

#### Scenario: Comparing durations
- **WHEN** rows show durations of 11 ms and 496 ms
- **THEN** digits of equal place value line up vertically
