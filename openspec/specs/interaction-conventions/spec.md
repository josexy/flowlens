# interaction-conventions Specification

## Purpose
Defines interaction rules shared by every FlowLens window, so focus, keys, wheel scrolling, tooltips, notifications, and drag feedback behave the same on every surface, as they did in FlowLens 1.0.5.

## Requirements

### Requirement: Focus follows a newly shown input
When an action reveals a text input, the input SHALL receive keyboard focus. This covers inline folder creation, rename and create dialogs, the capture alias dialog, the save-to-collection dialog, plugin file dialogs, and grid cell editors. Opening a grid cell editor SHALL also select its text.

#### Scenario: Creating a folder inline
- **WHEN** the user chooses New Folder in API Collections
- **THEN** the folder name input appears with keyboard focus, and typing enters the name without a click

#### Scenario: Opening a rename dialog
- **WHEN** the user chooses Rename on a collection item or plugin
- **THEN** the dialog opens with focus in its name field

#### Scenario: Editing a grid cell
- **WHEN** the user clicks a cell of an editable key/value grid
- **THEN** the cell's editor takes focus with its existing text selected

### Requirement: Enter and Escape in inline editors
In an inline single-field editor, Enter SHALL commit, and Escape SHALL cancel without committing. In a dialog with one name field, Enter SHALL submit the dialog when its primary action is enabled.

#### Scenario: Cancelling inline creation
- **WHEN** the user presses Escape in the inline new-folder input
- **THEN** the input closes and no folder is created

#### Scenario: Submitting with Enter
- **WHEN** the user presses Enter in the rename dialog with a non-empty name
- **THEN** the rename is submitted, as if the primary button were clicked

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

### Requirement: Tooltips on icon-only controls
Every icon-only control that had a tooltip in FlowLens 1.0.5 SHALL show a tooltip with the same localized text. Where 1.0.5 showed the command's key binding, the tooltip SHALL show the resolved binding.

#### Scenario: Shortcut row actions
- **WHEN** the user hovers the clear or reset button of a shortcut row in Settings
- **THEN** a tooltip names the action and the command

#### Scenario: Toolbar control with a binding
- **WHEN** the user hovers Start or Stop in the proxy toolbar
- **THEN** the tooltip shows the label and the resolved Toggle Proxy binding

### Requirement: Notification placement and duration
Notifications SHALL appear top-center in every FlowLens window, without a progress bar. Success and info notifications SHALL stay 1.8 seconds, warnings 3 seconds, and errors 4 seconds, unless an operation sets its own duration. HAR import results SHALL stay 5 seconds, or 8 seconds when any file failed.

#### Scenario: Notification in the Settings window
- **WHEN** an operation in the Settings window reports success
- **THEN** the notification appears top-center without a progress bar

#### Scenario: HAR import with a failed file
- **WHEN** a HAR import finishes with at least one failed file
- **THEN** the result notification stays visible for 8 seconds

### Requirement: Drag feedback
While the user drags an item to reorder or move it, FlowLens SHALL show the dragged item as in motion, by dimming its source or by drawing it under the pointer. It SHALL mark the valid drop target, and SHALL scroll the container when the pointer nears its edge.

#### Scenario: Dragging a collection item
- **WHEN** the user drags a saved request over a folder
- **THEN** the dragged row is dimmed, the folder is marked as the drop target, and releasing moves the request into it

#### Scenario: Dragging a workspace tab
- **WHEN** the user drags a workspace tab along the strip
- **THEN** the tab follows the pointer until it is released at its new position

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
