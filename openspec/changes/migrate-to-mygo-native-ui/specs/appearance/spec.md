# Spec Delta

## Purpose

Defines the user-controlled look of FlowLens: theme mode, color palettes, and interface and code fonts, including live preview.

## ADDED Requirements

### Requirement: Theme mode
FlowLens SHALL support light, dark, and automatic theme modes, persist the choice, and apply it to every window.

#### Scenario: Automatic mode follows the system
- **WHEN** the theme mode is automatic and the operating system appearance changes
- **THEN** every open window switches appearance without a restart

#### Scenario: Persisted mode
- **WHEN** the user selects dark mode and restarts FlowLens
- **THEN** every window opens in dark mode

### Requirement: Color palettes
FlowLens SHALL offer primary colors red, orange, amber, yellow, lime, green, emerald, teal, cyan, sky, blue, indigo, violet, purple, fuchsia, pink, and rose, and neutral colors slate, gray, zinc, neutral, stone, taupe, mauve, mist, and olive, defaulting to blue and slate.

#### Scenario: Selecting a palette
- **WHEN** the user selects the emerald primary color and the zinc neutral color and saves
- **THEN** every window uses those palettes in both light and dark modes

#### Scenario: Unknown saved color
- **WHEN** the saved palette names a color FlowLens does not offer
- **THEN** the default for that palette is used

### Requirement: Theme preview
Palette changes in the Settings window SHALL preview immediately in every window and SHALL be discarded if the user leaves without saving.

#### Scenario: Preview discarded
- **WHEN** the user picks a primary color and closes Settings without saving
- **THEN** every window returns to the saved palette

### Requirement: Interface and code fonts
FlowLens SHALL let the user choose interface and code fonts from the fonts available on the system, or use the bundled defaults.

#### Scenario: Default fonts
- **WHEN** no font is selected
- **THEN** interface text uses the bundled interface font and code uses the bundled monospaced font

#### Scenario: Chinese text with the bundled fonts
- **WHEN** interface text contains Chinese characters not present in the selected font
- **THEN** those characters render with a system fallback font rather than placeholder glyphs

### Requirement: Font sizes
FlowLens SHALL let the user set the interface font size from 12 to 24 with a default of 16, and the code font size from 10 to 32 with a default of 13.

#### Scenario: Out-of-range saved value
- **WHEN** a saved font size is outside the allowed range
- **THEN** the default size is used

#### Scenario: Changing the size
- **WHEN** the user changes the code font size and saves
- **THEN** code editors and viewers use the new size without a restart

### Requirement: Component visual style
Controls SHALL follow one consistent Nuxt UI v4-style visual language across light and dark themes.

#### Scenario: Same control in both themes
- **WHEN** a screen is viewed in light and dark themes
- **THEN** text and control states remain legible and use the same component styles
