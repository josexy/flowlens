# Spec Delta

## ADDED Requirements

### Requirement: Keyboard-operable navigation entries
Settings sections, capture/history entries, category items, collection nodes, rewrite rules, plugin/file entries, and the Memory Statistics sidebar entry SHALL be reachable and activatable without a pointer. Focus SHALL be visible. Activation SHALL perform the same business action as clicking the corresponding entry, preserving existing selection, expansion, and draft behavior.

#### Scenario: Selecting a settings section
- **WHEN** a user navigates to a settings section with the keyboard and activates it with Enter or Space
- **THEN** that page appears and the current settings draft is preserved

#### Scenario: Opening history or selecting a category
- **WHEN** a user reaches a history or category entry through Tab and, where applicable, list navigation keys, then activates it
- **THEN** the same history opens or category filter applies as for pointer activation

#### Scenario: Activating collection nodes
- **WHEN** the user activates a focused collection folder or request with the keyboard
- **THEN** the folder expands/collapses or the request opens using the same workflow as pointer activation

#### Scenario: Switching rules, plugins, and files
- **WHEN** the user navigates to and activates a rewrite rule, plugin, or plugin file with the keyboard
- **THEN** the corresponding content appears with the same draft retention and guard behavior as clicking it

#### Scenario: Collapsed sidebar
- **WHEN** a sidebar is collapsed or another page is active
- **THEN** its hidden entries do not retain keyboard activation routes that operate behind the visible page

### Requirement: Independent row controls
A navigation row's primary activation SHALL remain distinct from its checkbox, enable switch, menu, delete action, and drag handle. Disabled actions SHALL remain disabled for keyboard activation. Focus movement or primary row activation SHALL NOT silently trigger a destructive action or an enable-state change.

#### Scenario: Toggling a plugin switch
- **WHEN** the user activates a plugin row's enable switch from the keyboard
- **THEN** only that switch action runs, without also opening or reordering a different plugin

#### Scenario: Multi-select collection row
- **WHEN** the user activates a collection row's selection checkbox
- **THEN** the checkbox changes the checked set once without also opening the request

#### Scenario: Dragging a navigation row
- **WHEN** the user completes a pointer drag using a row's drag handle
- **THEN** the existing move workflow runs without an additional unintended row activation
