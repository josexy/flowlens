# Spec Delta

## ADDED Requirements

### Requirement: Byte-exact editor responses
The HTTP Request Editor SHALL show, copy, and save a response body as the exact bytes it received or a Python response hook produced. A body SHALL be shown as binary, as before, when its content type is binary, a streaming body has an unsupported content encoding, or a hook returned a binary body.

#### Scenario: Binary response
- **WHEN** a request returns an `image/png` response
- **THEN** the Image and Hex views show the received bytes, and saving the body writes a file identical to them

#### Scenario: Binary result from a Python hook
- **WHEN** an `onResponse` hook replaces the body with binary data, either inline or larger than 4 MiB
- **THEN** the response body holds exactly the bytes the hook produced and is shown as binary

#### Scenario: Text response
- **WHEN** a request returns a JSON response
- **THEN** the Formatted and Raw views show its text as before, and no Base64 appears

### Requirement: Byte-exact request recovery
Edit on a captured or historical entry with a binary request body SHALL recover a file body that holds exactly the captured request bytes.

#### Scenario: Editing a binary captured request
- **WHEN** the user chooses Edit on an entry whose request body is `application/octet-stream`
- **THEN** the editor tab uses a File body whose file is identical to the captured request body

#### Scenario: Editing a request with invalid UTF-8
- **WHEN** the user chooses Edit on an entry whose `text/plain` request body contains invalid UTF-8
- **THEN** the editor tab uses a File body with the original bytes, not text with replacement characters

### Requirement: Streaming binary editor responses
A streaming editor response shown as binary SHALL grow by each received chunk, in offset order. Each chunk SHALL extend the body without re-processing the data already shown, so a long stream stays responsive.

#### Scenario: Streaming with an unsupported content encoding
- **WHEN** a `text/event-stream` response with an unsupported `Content-Encoding` streams into the editor
- **THEN** the Hex view grows with the received bytes, in order, until the stream completes or is stopped

#### Scenario: Chunk ahead of the body
- **WHEN** a chunk arrives whose offset is past the end of the body shown
- **THEN** it is held until the missing data arrives, and no gap appears in the body
