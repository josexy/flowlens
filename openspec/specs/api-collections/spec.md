# api-collections Specification

## Purpose
Defines how saved HTTP and WebSocket requests are organized in folders, saved from editor tabs, and reopened with their settings intact.

## Requirements

### Requirement: Collection tree
The API Collections section SHALL show folders and saved requests in a tree that can be refreshed and expanded or collapsed entirely, and opening a saved request SHALL open it in a workspace tab.

#### Scenario: Empty collection
- **WHEN** no requests have been saved
- **THEN** an empty state is shown

#### Scenario: Load failure
- **WHEN** the collection cannot be loaded
- **THEN** an error message is shown and no partial tree is presented as complete

### Requirement: Tree management
The user SHALL be able to create folders and child folders, rename items, copy requests, move items by dragging or to the root, and delete items after confirmation.

#### Scenario: Deleting a folder
- **WHEN** the user confirms deleting a folder
- **THEN** the folder and everything inside it are removed

#### Scenario: Moving a folder into itself
- **WHEN** the user drags a folder into one of its own descendants
- **THEN** the move is rejected and the tree is unchanged

#### Scenario: Multi-select delete
- **WHEN** the user enables multi-select, selects several items, and confirms delete
- **THEN** the selected items are removed in one operation

### Requirement: Saving requests
Editor tabs SHALL be savable into a chosen folder from the tab context menu, and saving a tab linked to a collection entry SHALL update that entry.

#### Scenario: Saving a new request
- **WHEN** the user saves an unsaved tab with a name and destination folder
- **THEN** the request appears in the tree and the tab becomes linked to it

#### Scenario: Saving a linked tab
- **WHEN** the user presses Save on a linked tab with changes
- **THEN** the existing entry is updated without a save dialog

#### Scenario: No folders exist
- **WHEN** the user saves a request and a folder is required
- **THEN** the user is asked to create a folder first

### Requirement: Request round trip
Saved requests SHALL retain protocol, redirect limit, TLS verification, TLS profile, HTTP/2 fingerprint, proxy selection, ordered headers, body, and current-request script source.

#### Scenario: Reopening a saved request
- **WHEN** the user opens a saved request
- **THEN** all saved fields are restored and the script enable switch is off

#### Scenario: Older saved request
- **WHEN** a request saved before a setting existed is opened
- **THEN** that setting uses its default value

### Requirement: Managed body files
Bodies saved with a collection entry SHALL be stored in managed files that stay consistent with the database.

#### Scenario: Failed save
- **WHEN** saving a request fails after its body file was written
- **THEN** the database is unchanged and the unreferenced file is removed

#### Scenario: Deleting a request
- **WHEN** a saved request with a managed body file is deleted
- **THEN** its managed file is removed and files of other requests remain

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
