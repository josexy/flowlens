## ADDED Requirements

### Requirement: Workspace tabs preserve sidebar navigation
Activating a workspace tab by pointer or shortcut, or closing a history tab, SHALL preserve the selected sidebar section, secondary sidebar visibility, and width. Capture and history tabs SHALL still update the selected traffic source, including the Capture sidebar selection and Categories content. Explicit sidebar navigation SHALL retain its existing behavior.

#### Scenario: Switching traffic tabs with Categories open
- **WHEN** the user switches between history and live capture tabs while Categories is selected
- **THEN** Categories stays selected and displays the active traffic source
- **AND** the Capture sidebar selection matches that source when the user returns to it

#### Scenario: Switching tabs with another sidebar selected
- **WHEN** the user switches workspace tabs with API Collections selected
- **THEN** API Collections stays selected

#### Scenario: Switching tabs with the sidebar collapsed
- **WHEN** the user switches workspace tabs while the secondary sidebar is collapsed
- **THEN** the sidebar stays collapsed and retains its width

#### Scenario: Closing history
- **WHEN** the history tab is closed, whether active or inactive
- **THEN** its data is released, any history traffic selection falls back to live capture, and sidebar navigation stays unchanged

#### Scenario: Explicit capture navigation
- **WHEN** the user opens Capture through the primary sidebar and selects its live capture item
- **THEN** the Capture sidebar is shown and the live capture tab becomes active
