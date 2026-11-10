# Spec Delta

## ADDED Requirements

### Requirement: FlowLens 1.0.5 as the visual reference
Layout, spacing, type sizes, colors, icons, and control placement SHALL match the FlowLens 1.0.5 interface wherever the native toolkit can draw them. A deliberate difference SHALL be listed in the change that introduces it, with its reason.

#### Scenario: Comparing a surface with 1.0.5
- **WHEN** a surface is rendered at the same window size, theme, and language as a 1.0.5 reference
- **THEN** its regions, control order, icons, and colors match the reference, apart from listed differences and platform font rendering

### Requirement: Pointer cursor setting
When Pointer Cursor is enabled in Settings, the pointer SHALL become a hand over every enabled clickable control in every FlowLens window. When it is disabled, clickable controls SHALL show the default arrow. Disabled controls SHALL never show the hand.

#### Scenario: Enabling the setting
- **WHEN** the user enables Pointer Cursor and saves
- **THEN** buttons, tabs, menu triggers, list rows that open or select items, and links show a hand cursor in the main, Settings, and Update windows without a restart

#### Scenario: Disabled control
- **WHEN** the setting is enabled and the pointer rests on a disabled button
- **THEN** the default arrow cursor is shown

### Requirement: Colored badges stay legible
Method, status, and summary badges SHALL keep their tint and text color on every row state, including selected, inspected, highlighted, and striped rows, in light and dark themes.

#### Scenario: Selected row in the dark theme
- **WHEN** an OPTIONS request with a 404 status is selected in the dark theme
- **THEN** both badges keep a tinted background that is opaque against the row and readable text
