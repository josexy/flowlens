# python-plugins Specification

## Purpose
Defines the workbench for creating, editing, validating, and matching global Python request plugins, and the API help that supports them.

## Requirements

### Requirement: Plugin list
The Python Plugins section SHALL list plugins with search, enable switches, status, and drag ordering, and offer creating plugins and opening the plugins directory.

#### Scenario: Reordering plugins
- **WHEN** the user drags a plugin to a new position
- **THEN** the new order is saved and used for later requests

#### Scenario: No matching search
- **WHEN** a search matches no plugins
- **THEN** a no-results state is shown

#### Scenario: Plugin status
- **WHEN** a plugin has been validated or has a runtime error
- **THEN** its validation state, runtime state, and active revision are shown

### Requirement: Plugin file editing
The plugin workbench SHALL browse, create, rename, delete, and edit a plugin's files in a code editor, restricting paths to the plugin directory.

#### Scenario: Saving a file
- **WHEN** the user edits a file and presses Save
- **THEN** the file is written and the unsaved state clears

#### Scenario: Path outside the plugin
- **WHEN** the user enters a file path that escapes the plugin directory
- **THEN** the path is rejected with a message

#### Scenario: Unsaved content on delete
- **WHEN** the user deletes a file with unsaved content
- **THEN** the confirmation states that unsaved content will be lost

### Requirement: Python code assistance
The Python editors SHALL provide syntax highlighting and completion and hover help for the FlowLens plugin API.

#### Scenario: Completing the plugin API
- **WHEN** the user types an API prefix in a plugin or request script
- **THEN** matching FlowLens API items are offered with their descriptions

### Requirement: Validation and reload
The workbench SHALL save and validate a plugin on request and reload it from saved files.

#### Scenario: Invalid plugin
- **WHEN** validation fails
- **THEN** the error is shown and the previously active revision stays in use

### Requirement: Plugin metadata and deletion
The user SHALL be able to edit a plugin's name and description and delete a plugin with its files after confirmation.

#### Scenario: Deleting a plugin
- **WHEN** the user confirms deletion
- **THEN** the plugin and its files are removed

### Requirement: Plugin parameters
The workbench SHALL edit plugin parameters as a JSON object, with format and save actions.

#### Scenario: Non-object JSON
- **WHEN** the user saves parameters that are not a JSON object
- **THEN** an error is shown and the saved parameters are unchanged

### Requirement: Plugin rules
The workbench SHALL manage ordered rules of methods and URL patterns that decide which requests a plugin runs on.

#### Scenario: Adding a rule
- **WHEN** the user adds a rule with a method and URL pattern and saves
- **THEN** the plugin runs on matching requests in the HTTP Request Editor

#### Scenario: Reordering rules
- **WHEN** the user drags a rule to a new position
- **THEN** the new rule order is saved

### Requirement: API help
API help SHALL be available from the Python Plugins sidebar and the request script toolbar, with search and a copyable complete system prompt for either scope.

#### Scenario: Copying the system prompt
- **WHEN** the user copies the system prompt in the request script scope
- **THEN** the clipboard contains the complete prompt for that scope in the current language

### Requirement: Trusted code confirmation
FlowLens SHALL require the user to acknowledge that plugins run as trusted local code before enabling Python plugins.

#### Scenario: Declining the confirmation
- **WHEN** the user declines the trusted code confirmation
- **THEN** Python plugins remain disabled
