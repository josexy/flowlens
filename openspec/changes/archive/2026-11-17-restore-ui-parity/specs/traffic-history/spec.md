# Spec Delta

## ADDED Requirements

### Requirement: Category section headers
Each category section header, such as Hosts, Processes, and Structure, SHALL be a full-width 32 px band on the sidebar header tint, with semibold muted text, a bottom border, and a top border after the first section. Its chevron SHALL sit at the end of the row.

#### Scenario: Collapsing a section
- **WHEN** the user clicks the Hosts section header
- **THEN** the section collapses, and its end-aligned chevron points right

### Requirement: Category filter check marks
A selected host or process filter SHALL show an accent check mark at the end of its row, beside its 20 px globe or process icon. Rows SHALL NOT show entry counts.

#### Scenario: Selecting two hosts
- **WHEN** the user selects two hosts in the category tree
- **THEN** both rows show an accent check mark, and the traffic list shows entries for either host

### Requirement: Structure tree rows
Structure folders SHALL show a 20 px folder icon and an end-aligned chevron that rotates when the folder opens. Rows SHALL be rounded and SHALL show a hover background.

#### Scenario: Expanding a folder
- **WHEN** the user expands a path segment in the structure tree
- **THEN** its chevron rotates, its icon becomes an open folder, and its children appear below it
