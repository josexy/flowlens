# Spec Delta

## ADDED Requirements

### Requirement: FlowLens 1.0.5 as the visual reference
Layout, spacing, type sizes, colors, icons, and control placement SHALL match the FlowLens 1.0.5 interface wherever the native toolkit can draw them. A deliberate difference SHALL be listed in the change that introduces it, with its reason.

#### Scenario: Comparing a surface with 1.0.5
- **WHEN** a surface is rendered at the same window size, theme, and language as a 1.0.5 reference
- **THEN** its regions, control order, icons, and colors match the reference, apart from listed differences and platform font rendering

### Requirement: Colored badges stay legible
Method, status, and summary badges SHALL keep their tint and text color on every row state, including selected, inspected, highlighted, and striped rows, in light and dark themes.

#### Scenario: Selected row in the dark theme
- **WHEN** an OPTIONS request with a 404 status is selected in the dark theme
- **THEN** both badges keep a tinted background that is opaque against the row and readable text
