# Spec Delta

## ADDED Requirements

### Requirement: Collection tree row presentation
Collection rows SHALL be 32 px tall and rounded, with a hover background. Folders SHALL show a 20 px folder icon, a pill with their child count, and, when they have children, an end-aligned chevron that rotates when open. Requests SHALL show the HTTP method or `WS` in 14 px semibold in the method's color before the name. Hovering a name SHALL show the full name in a tooltip to its right.

#### Scenario: Folder with children
- **WHEN** a folder holds three requests
- **THEN** its row shows a folder icon, the name, a pill reading 3, and an end-aligned chevron

#### Scenario: Truncated name
- **WHEN** a request name is wider than the sidebar
- **THEN** the name is cut with an ellipsis, and hovering shows the full name

### Requirement: Collection drag handle and feedback
Each row SHALL show a grip handle on hover, and dragging SHALL start only from that handle. While dragging, the source row SHALL be dimmed to half opacity. A valid target folder SHALL show a primary tint and ring. Move to Root SHALL appear as a dashed drop zone at the top of the tree.

#### Scenario: Clicking a request
- **WHEN** the user presses and releases on a request name without using the grip
- **THEN** the request opens and no drag begins

#### Scenario: Moving to the root
- **WHEN** the user drags a nested folder onto the Move to Root zone
- **THEN** the zone is highlighted, and releasing moves the folder to the top level
