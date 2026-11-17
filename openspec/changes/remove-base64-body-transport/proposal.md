# Proposal

## Why

The Wails build passed bodies to a web view as JSON, so binary payloads were Base64 text. The MyGo views live in the same Go process as the services, yet every binary payload still makes that round trip. The service encodes it, the in-process event bus carries a string a third larger, and a view decodes it again on the main thread or in a background job. That costs CPU, and memory for two extra copies: a Base64 string a third larger than the payload, and a decoded copy in each view that needs the bytes. For a streaming binary editor response, it also costs quadratic time: the store decodes each chunk, then re-encodes the whole accumulated body on every flush. Binary WebSocket payloads are worse still: they stay Base64 in memory and in HBIN history files.

## What Changes

Base64 stays only where a format or an external process needs text. It leaves every in-process path between the services and the views.

- **Traffic and history bodies.** `TrafficBodyView` carries every request and response body as its exact bytes. The `"base64"` encoding values become a boolean marker for binary display, set when the content type, an unsupported content encoding, or invalid UTF-8 makes the body binary. The views no longer decode anything. Hex view reads binary bodies in place, as it already does for text, so it no longer waits on an asynchronous load. The image preview decodes the bytes directly.
- **Live updates.** `TrafficLiveUpdate` SSE chunks and `HTTPRequestStreamEvent` chunks carry raw byte slices. The traffic store and the workspace store append bytes. A binary streaming editor response no longer re-encodes its whole body on each flush.
- **Request editor responses.** `SendRequestResponse.Body` holds raw bytes with a binary marker, including binary Python-plugin results and file-backed plugin results.
- **WebSocket messages.** `WebSocketMessage.Data` holds raw bytes for binary messages, both in capture and in WebSocket Client.
- **Process icons.** `ProcessIconData` carries PNG bytes.
- **Saving bodies.** `SaveBodyToFileRequest` takes raw bytes. Body save, WebSocket message save, and hex selection export no longer encode only for the service to decode.
- **History format.** **BREAKING (file format):** New histories are written as HBIN v3, which stores binary WebSocket payloads as raw bytes. v1 and v2 files stay readable, with their Base64 payloads decoded on read. Builds that only know v2 skip v3 files without deleting them. HTTP bodies are already raw in every version, so only the meaning of the WebSocket payload bytes changes. History-format checks in the interface (HAR exportability) accept v3.
- **Unchanged outputs.** Users see the same output as before in each of these places. They still encode Base64 themselves, because Base64 is part of their format:
  - Hex "Copy as Base64".
  - The `base64 -d` pipeline that "Copy as cURL" generates for binary request bodies.
  - HAR export and import, including `_encoding`.
  - The Python Worker JSON protocol and the plugin `binary` body passed through the hook chain.
  - The `Proxy-Authorization` header.
  - macOS trust plist parsing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

These capabilities are defined by the in-flight `migrate-to-mygo-native-ui` change and are not yet in `openspec/specs/`.

- `traffic-history`: New histories are HBIN v3. v1, v2, and v3 are read, and binary WebSocket payloads from older files are restored to their original bytes.
- `traffic-inspection`: Bodies, live chunks, and binary WebSocket messages reach the viewers as their exact bytes. Hex, image, save, and copy operate on those bytes without a decode step. Base64 appears only in outputs where the user asks for it.
- `request-workspace`: Editor responses, including streaming and plugin-produced binary bodies, are shown and saved as their exact bytes. A binary captured request opened with Edit is recovered byte for byte.

## Impact

- **Code**:
  - `internal/services/proxy_service`: types, `traffic_codec.go`, `proxy_service.go` (body view, SSE capture, editor send and stream, WebSocket normalization, body save), `process_attribution.go`, and `http_request_plugin_body_store.go`.
  - `internal/services/history_service`: body read with format version.
  - `internal/desktop/store`: `traffic.go` and `workspace.go`.
  - `internal/desktop/logic`: `body.go`, `hexdump.go`, `http_raw.go`, `request_send.go`, `request_state.go`, `traffic_filter.go`, and `traffic.go`.
  - `internal/desktop/inspect`: `body.go`, `hex.go`, `image.go`, `sse.go`, `messages.go`, and `raw.go`.
  - `internal/desktop/editor/response.go`, `internal/desktop/capture/menu.go`, and `internal/desktop/widgets/process_icon.go`.
  - Tests in all of these packages.
- **APIs**: In-process Go structs change field names and types. `*Encoding string` becomes a binary `bool`. `ChunkBase64` becomes `Chunk []byte`. `DataBase64` becomes `Data []byte`. `DecodeTrafficBody` takes a format version. No external API, Worker protocol, or Python SDK changes.
- **Persistence**: HBIN v3 as described above. SQLite, settings, and API Collection are untouched.
- **Docs**:
  - `AGENTS.md` HBIN contract ("write v3, read v1/v2/v3").
  - `docs/user-guide.md` HBIN mentions.
  - `docs/technical/http-rewrite-rules.md` HBIN line.
  - A `CHANGELOG.md` 1.1.0 note that history saved by this version needs 1.1.0 or later.
- **Ordering**: Archive `migrate-to-mygo-native-ui` before this change, since the `traffic-history` delta modifies its "Existing data compatibility" requirement. The "New history format" scenario there still says HBIN v2.
- **Dependencies**: None.
