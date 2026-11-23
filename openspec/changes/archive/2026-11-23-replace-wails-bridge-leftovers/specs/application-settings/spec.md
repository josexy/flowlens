# Spec Delta

## MODIFIED Requirements

### Requirement: Proxy connection settings
Proxy settings SHALL configure process attribution, upstream proxy mode (None, System, Custom), and connection options.

#### Scenario: Custom upstream without a URL
- **WHEN** the upstream proxy mode is Custom and the URL is empty or invalid
- **THEN** saving is blocked with a message

#### Scenario: Restart prompt
- **WHEN** a saved change requires the running proxy to restart
- **THEN** the user is told and can restart the proxy

#### Scenario: Restart reasons in the current language
- **WHEN** the user saves a change to the proxy mode and the listen port while the proxy is running
- **THEN** the saved notice names both reasons as translated text in the current language, in the order the proxy reported them, and never shows a message key

#### Scenario: Unrecognized restart reason
- **WHEN** the proxy reports a restart reason that has no translated text
- **THEN** the notice shows the reason as the proxy reported it, alongside any translated reasons

### Requirement: Storage settings
Storage settings SHALL configure the body cache threshold, WebSocket message retention, and history retention, and show disk usage for cache, history, and total.

#### Scenario: Clearing cache only
- **WHEN** the user confirms clearing cache files with a non-empty capture
- **THEN** the current capture is archived first and then the live list is cleared

#### Scenario: Clearing all data
- **WHEN** the user confirms clearing all data
- **THEN** caches, the current capture, and readable saved history are removed while the database, API Collections, preferences, certificates, and logs remain

#### Scenario: History list after clearing cache only
- **WHEN** clearing cache files archives the current capture
- **THEN** the history list in the main window shows the archived capture without a restart, and an open saved history that still exists stays open

#### Scenario: History list after clearing all data
- **WHEN** clearing all data removes saved history
- **THEN** the history list no longer shows the removed histories; an open saved history that was removed closes and the live capture is shown; and a history list load that was already in progress does not bring back removed entries

#### Scenario: Request files in the cleared draft cache
- **WHEN** clearing cache files or all data removes the request draft cache while request editor tabs use files from it as a body, form-data file, or WebSocket draft file
- **THEN** those tabs no longer reference the removed files; file references outside the draft cache, the rest of each draft, and its unsaved state are kept

#### Scenario: Process icons after clearing
- **WHEN** clearing cache files or all data removes cached process icons
- **THEN** every window discards the icons it holds, and process icons shown afterwards are loaded again

#### Scenario: Clearing partly fails
- **WHEN** a cleanup removes some data but fails to remove a directory
- **THEN** the error is shown, and windows still refresh the history list, request file references, and process icons for what was removed
