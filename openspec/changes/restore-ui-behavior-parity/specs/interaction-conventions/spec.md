# Spec Delta

## MODIFIED Requirements

### Requirement: Wheel scrolls horizontal strips
A horizontal strip whose content overflows, such as the workspace tab strip, the detail request and response tab strips, or the traffic filter type tabs, SHALL scroll sideways with a vertical wheel, using the larger of the two wheel axes. Ctrl+wheel SHALL NOT scroll a strip.

#### Scenario: Scrolling overflowing workspace tabs
- **WHEN** the workspace tabs overflow and the user turns a vertical mouse wheel over the strip
- **THEN** the strip scrolls horizontally, and the scroll buttons update

#### Scenario: Scrolling overflowing filter type tabs
- **WHEN** the window is narrow enough that the filter type tabs overflow and the user turns a vertical mouse wheel over them
- **THEN** the tabs scroll horizontally and the traffic table does not scroll

#### Scenario: No overflow
- **WHEN** the strip does not overflow
- **THEN** the wheel event passes to the surrounding content

## ADDED Requirements

### Requirement: Message detail dialogs
The SSE message, WebSocket message, and Python console entry details SHALL open in a dialog up to 960 px wide and at most 92% of the window width. A header row SHALL show the item's details on the left and icon actions with tooltips on the right. Below it, a bordered content area SHALL be as tall as 70% of the window, up to 720 px.

#### Scenario: Large window
- **WHEN** the user opens a message detail in a 1600×1000 window
- **THEN** the dialog is 960 px wide and its content area is 700 px tall

#### Scenario: Small window
- **WHEN** the user opens a message detail in a 900×600 window
- **THEN** the dialog is at most 828 px wide and its content area is 420 px tall

### Requirement: Detail dialog actions
A message detail dialog SHALL offer Wrap when its content is text, Copy when its content is text, and Save, as icon buttons with tooltips. Wrap SHALL start off each time the view opens, and SHALL be shown as pressed while on. Save SHALL be unavailable while a save from that dialog is in progress, and so SHALL the export of a hex selection in it.

#### Scenario: Binary WebSocket message
- **WHEN** the user opens a binary WebSocket message
- **THEN** only Save is offered

#### Scenario: Turning on wrap
- **WHEN** the user turns on Wrap in an SSE message detail
- **THEN** long lines wrap and the Wrap button shows as pressed

#### Scenario: Saving a binary message
- **WHEN** a save of a binary WebSocket message is in progress
- **THEN** Save and the hex view's Export are unavailable until it ends

### Requirement: Menu item icons
Menus SHALL show the icons and color swatches of FlowLens 1.0.5 where the platform's menus show item icons:
- Traffic row Highlight colors: a swatch of their color.
- Capture menu: Export a file-download icon, Set Alias a tag icon.
- History menu: Export a file-download icon, Delete a trash icon.
- Column menu: Show All Columns a list-check icon.
- Settings Root CA Add menu: Select Files a folder-open icon, Add Path Manually a pencil icon.

Other items SHALL show no icon.

#### Scenario: Highlight submenu
- **WHEN** the user opens a traffic row's Highlight submenu
- **THEN** Red, Orange, Yellow, Green, Blue, and Purple each show a round swatch of their color, and Clear shows none

#### Scenario: History menu
- **WHEN** the user right-clicks a saved history
- **THEN** Export shows the file-download icon and Delete shows the trash icon

#### Scenario: Traffic row Delete
- **WHEN** the user right-clicks a traffic row
- **THEN** its Delete item shows no icon

### Requirement: Shortcut hints in menus
A menu item that runs a command with a keyboard binding SHALL show the binding in use. This covers Save API and Close in the workspace tab menu, and New HTTP and New WebSocket in the New Tab menu. An item whose command has no binding SHALL show none.

#### Scenario: Default bindings on Windows
- **WHEN** the user right-clicks an HTTP request tab with default shortcuts on Windows
- **THEN** Save API shows Ctrl+S and Close shows Ctrl+W

#### Scenario: Disabled binding
- **WHEN** the user disables the Close Tab shortcut and right-clicks a tab
- **THEN** Close shows no shortcut
