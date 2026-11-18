# Spec Delta

## Purpose

Defines interaction rules shared by every FlowLens window, so focus, keys, wheel scrolling, tooltips, notifications, and drag feedback behave the same on every surface, as they did in FlowLens 1.0.5.

## ADDED Requirements

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
A horizontal strip whose content overflows, such as the workspace tab strip or the detail request and response tab strips, SHALL scroll sideways with a vertical wheel, using the larger of the two wheel axes. Ctrl+wheel SHALL NOT scroll a strip.

#### Scenario: Scrolling overflowing workspace tabs
- **WHEN** the workspace tabs overflow and the user turns a vertical mouse wheel over the strip
- **THEN** the strip scrolls horizontally, and the scroll buttons update

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
