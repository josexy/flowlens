## ADDED Requirements

### Requirement: Bounded traffic cell previews
Capture and history tables SHALL render text values using previews of at most 256 UTF-8 bytes, including a trailing ellipsis when shortened, without splitting a valid UTF-8 rune. Filtering, sorting, inspection, copying, and exports SHALL use original complete values. Empty and unknown-value presentations SHALL remain unchanged.

#### Scenario: Long path enters the viewport
- **WHEN** a row with a path longer than 256 bytes enters view in capture or history
- **THEN** the table shows a bounded prefix ending in an ellipsis in either theme and interface language
- **AND** the original path remains available through Copy Path and inspection

#### Scenario: Unicode boundary
- **WHEN** shortening a valid Unicode cell value would cut through a multibyte rune
- **THEN** the preview ends at the previous rune boundary before the ellipsis

#### Scenario: Suffix distinguishes records
- **WHEN** two full paths share their preview prefix but have different suffixes
- **THEN** searching, sorting, copying, and exporting distinguish the complete paths

## MODIFIED Requirements

### Requirement: Full method and status badges
Method and status badges SHALL show their full text within the traffic-cell preview limit, such as `OPTIONS` or `CONNECT`, at the default column widths. They SHALL use the 1.0.5 tones: each method's color on a 13% tint over the panel background, and the success, info, warning, error, and default status tones.

#### Scenario: Long method name
- **WHEN** an OPTIONS request is shown with default column widths
- **THEN** its badge reads `OPTIONS` with no ellipsis
