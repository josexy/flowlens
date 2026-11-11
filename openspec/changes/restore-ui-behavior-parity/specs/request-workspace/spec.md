# Spec Delta

## ADDED Requirements

### Requirement: Python console columns
The Python request console SHALL list entries in a bordered table with a header of Time, Type, Source, and Output. The user SHALL be able to resize each column within bounds: Time 120–360 px, Type 80–260 px, Source 96–420 px, and Output 240–900 px, starting at 152, 96, 120, and 260 px. Output SHALL fill the remaining width. Widths SHALL last while the editor tab is open and SHALL NOT be saved.

#### Scenario: Resizing past a bound
- **WHEN** the user drags the Source column edge to 600 px
- **THEN** the Source column stops at 420 px

#### Scenario: Narrow console
- **WHEN** the columns are wider than the console
- **THEN** the table scrolls horizontally and the header moves with the rows

### Requirement: Python console rows
Each console row SHALL be 40 px high and show on one line its local time, a Type cell with a stream icon (terminal for stdout, warning triangle for stderr) and a level pill in the entry's tone, its source, and the first line of its output. Multi-line output SHALL show its line count. The list SHALL NOT wrap output.

#### Scenario: Multi-line stderr entry
- **WHEN** a plugin writes three lines to stderr with no level
- **THEN** the row shows the warning triangle, an Info pill in the error tone, the first line, and "3 lines", at 40 px

#### Scenario: Opening an entry
- **WHEN** the user clicks a row
- **THEN** the entry detail opens for that entry

### Requirement: Python console status
The console header SHALL show a dot and status text: a pulsing accent dot with "Script running" while a send is in progress, and a muted dot with the entry count otherwise.

#### Scenario: During a send
- **WHEN** a request with hooks is being sent
- **THEN** the header shows the pulsing dot and "Script running"

#### Scenario: After a send
- **WHEN** the send finished with five entries
- **THEN** the header shows the muted dot and "5 output entries"

### Requirement: Python console entry detail
The entry detail SHALL show the entry's time, a pill with its stream icon, stream, and level in its tone, and its source, with Wrap, Copy, and Save icon actions. Wrap SHALL start off and SHALL apply to the detail text only.

#### Scenario: Turning on wrap
- **WHEN** the user opens a long single-line entry and turns on Wrap
- **THEN** the detail text wraps and the console rows stay on one line

### Requirement: Python console copy and save format
Copy All and Save All SHALL write one line per entry, `[<local time>] [<source>] [<stream>] <message>`, joined by line breaks, where stream is the entry's raw stream name, else its level, else `log`. Save All SHALL suggest `flowlens-python-console.log`. Saving one entry SHALL write its message and suggest `flowlens-python-console-entry.log`.

#### Scenario: Copy all
- **WHEN** the console holds a stdout entry from the current script and the user chooses Copy All
- **THEN** the clipboard holds `[<time>] [Current script] [stdout] <message>` for it, one line per entry

#### Scenario: Save one entry
- **WHEN** the user chooses Save in an entry's detail
- **THEN** the save dialog suggests `flowlens-python-console-entry.log` and the file holds only the message

### Requirement: Clearing the console closes its detail
Clearing the console, or a new send that replaces its entries, SHALL close an open entry detail.

#### Scenario: Clear with the detail open
- **WHEN** an entry detail is open and the user chooses Clear
- **THEN** the detail closes and the console is empty

### Requirement: Body type menu order
The HTTP Request Editor's body type menu SHALL list None, JSON, Text, XML, File, Form Data, and URL-encoded, in that order.

#### Scenario: Opening the body type menu
- **WHEN** the user opens the body type menu
- **THEN** File comes after XML and URL-encoded comes last
