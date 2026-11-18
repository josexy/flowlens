# Spec Delta

## ADDED Requirements

### Requirement: Bounded capture memory
A capture session SHALL hold in memory only its newest 5000 entries and entries whose exchange is still in progress. Older finished entries SHALL move to disk for the rest of the session, so capture memory does not grow with session length. Captured WebSocket message payloads larger than the body cache threshold SHALL be held on disk.

#### Scenario: Long capture
- **WHEN** a session has captured 200000 finished entries
- **THEN** entries older than the newest 5000 hold no headers, bodies, WebSocket messages, or process details in memory

#### Scenario: Status total
- **WHEN** older entries have moved to disk
- **THEN** the status bar total still counts every entry in the session

#### Scenario: Long-running exchange
- **WHEN** a WebSocket connection or streaming response opened early stays open while thousands of newer entries arrive
- **THEN** it stays in memory and keeps receiving updates until it finishes

#### Scenario: Exchanges finish after traffic stops
- **WHEN** exchanges older than the newest 5000 entries finish and no new entries arrive
- **THEN** their data moves to disk automatically, including when only one such entry remains, and their details remain readable

#### Scenario: Acting on an older entry
- **WHEN** the table holds its window while reading and the user opens, copies as cURL, resends, edits, exports, or deletes an entry that is no longer among the newest 5000
- **THEN** the action behaves as it does for the newest entries, with the same headers, bodies, and messages

#### Scenario: Exporting the whole capture
- **WHEN** the user exports all captured entries of a session that has entries on disk
- **THEN** every entry of the session is written in ID order, as for a session held entirely in memory

#### Scenario: Large WebSocket messages
- **WHEN** an open WebSocket connection has received 1000 messages of 8 MiB each
- **THEN** those payloads are held on disk, and the WebSocket view, saved history, and export show them byte for byte

### Requirement: Periodic capture saving
The current capture SHALL be saved to its history periodically and at shutdown. A save SHALL NOT read whole bodies still being received into memory. When a save takes long, the next periodic save SHALL wait at least five times that save's duration, and never less than 60 seconds.

#### Scenario: Large download in progress
- **WHEN** a periodic save runs while a 2 GiB response is still being received
- **THEN** the received part is saved without being held in memory, and the response keeps streaming to the client

#### Scenario: Slow save
- **WHEN** a periodic save of a large session takes 30 seconds
- **THEN** the next periodic save starts no sooner than 150 seconds later

#### Scenario: Session with entries on disk
- **WHEN** a session whose older entries are on disk is saved
- **THEN** the saved history contains every entry of the session in ID order

### Requirement: Capture storage release
On-disk data of a capture session SHALL be removed once it is no longer needed: when entries are deleted, when the capture is cleared, when capture restarts after Save or Discard, after a successful save at shutdown, and at startup for sessions left by earlier runs. Saved histories and process icons SHALL NOT be affected.

#### Scenario: Restarting capture
- **WHEN** the user chooses Save and Restart and the save succeeds
- **THEN** the previous session's cached bodies and on-disk entries are removed and its saved history remains

#### Scenario: Previous run ended abnormally
- **WHEN** FlowLens starts after a crash left an earlier session's cached bodies on disk
- **THEN** that data is removed while saved histories and cached process icons remain

#### Scenario: Deleting entries while bodies arrive
- **WHEN** the user deletes entries whose request or response bodies start arriving at the same moment
- **THEN** no body data of the deleted entries remains in memory or on disk

#### Scenario: Failed save at shutdown
- **WHEN** the final save at shutdown fails
- **THEN** the session's cached data is kept on disk until the next startup

### Requirement: Capture never stalls forwarding
Decoding a captured body for display SHALL NOT delay or block forwarding the original bytes between client and server, whatever the body contains.

#### Scenario: Bytes after the compressed stream
- **WHEN** a `deflate` response carries bytes after the end of its compressed stream
- **THEN** the client receives the complete response, the entry completes, and no background work remains for it

#### Scenario: Corrupt compressed body
- **WHEN** a `gzip` response has an invalid checksum
- **THEN** the client receives the original bytes and the entry completes with the bytes that could be decoded
