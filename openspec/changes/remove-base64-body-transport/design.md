# Design

## Context

See `proposal.md` (Why) for motivation. This design rests on the following facts about the code today.

- **One process.** `serviceruntime.Bus` delivers events synchronously as `any` values. `frontend_event_batcher.go` estimates queue size by reflection and already counts a `[]byte` as its length. Nothing between the services and the views is serialized.
- **HBIN HTTP bodies are already raw.** `hbinWriteEntryBody` stores raw bytes. Its per-side flag byte (0 text, 1 "base64", 2 unavailable) only records how the view should render the body. When `convertToTrafficBodyView` builds the view, `encodeTrafficBodyView` Base64-encodes flagged or non-UTF-8 bodies and hands the others over without a copy through `bytes2String`.
- **Binary WebSocket payloads are Base64.** `normalizeWebSocketMessage` stores them Base64-encoded in memory. `hbinWriteWebSocketMessage` writes that text as is, so v1 and v2 files hold Base64 for binary messages.
- **Every Base64 site is one of two kinds:**

  | Kind | Sites |
  | --- | --- |
  | Transport, to be removed | `TrafficBodyView.{Request,Response}BodyEncoding`; `TrafficLiveUpdate.ChunkBase64`; `HTTPRequestStreamEvent.ChunkBase64`; `SendRequestResponse.BodyEncoding` and `materializeHTTPRequestPluginResponse*(encodeBase64)`; `WebSocketMessage.Data` for binary; `ProcessIconData.DataBase64`; `SaveBodyToFileRequest.BodyEncoding`; `decodeTrafficRequestBody` in request recovery; `DecodeTrafficRequestBody`'s encoding result |
  | Format, to be kept | HAR export, import, and `postData` (`HARBody.Encoding`); the Python Worker wire (`requestBodyWire.Base64`) and `SendRequestBody{BodyType: binary}`, which carries that wire text through the hook chain; `Proxy-Authorization`; the macOS trust plist; Hex "Copy as Base64"; `BuildCurl` |

- **Desktop consumers.** Each one branches on `== "base64"`:
  - `logic.ClassifyBody`, `BodyTabs`, `EstimateDecodedByteLength`, and `DecodeHexInput`
  - `inspect` body, hex, image, SSE, messages, and raw
  - `store.TrafficStore.applyLive` and `prepareBase64SSESnapshot`
  - `store.WorkspaceStore.RegisterHTTPStream` and `flushStream`
  - `logic.ResponseViewOf`, `formatRawMessage`, and `BuildCurl`
  - `widgets.decodeProcessIcon`

## Goals / Non-Goals

**Goals:**
- No in-process Base64 encode or decode between the services and the views.
- Byte-for-byte equal results for everything the user can see, copy, save, or export. The only exceptions are the improvements stated in the specs: the Hex view shows bytes without a loading state, and streaming binary editor bodies are no longer quadratic.
- HBIN v3 that stores binary WebSocket payloads raw. Reading v1 and v2 stays lossless.

**Non-Goals:**
- **Python Worker JSON protocol, SDK, and plugin `binary` body.** JSON needs text. The Base64 string also passes between chained plugins without a re-encode, so decoding it in Go would add work, not remove it.
- **HAR formats and the generated cURL text.** Their Base64 is part of the output format.
- **UTF-8 handling of live text streams.** `utf8StreamDecoder`, with its BOM strip and U+FFFD replacement, stays as it is. Only its input changes from decoded Base64 to raw chunks.
- **Which bodies count as binary.** The rules stay as they are. Editor responses still use the content type or the plugin kind, and do not check UTF-8 validity.
- **The HBIN HTTP body layout and its flag byte values.** Only the WebSocket payload meaning changes in v3.

## Decisions

### 1. Bodies stay `string`, holding raw bytes; the encoding string becomes a `Binary bool`

`TrafficBodyView` uses `RequestBody`/`ResponseBody string` plus `RequestBodyBinary`/`ResponseBodyBinary bool`. The same pattern applies to `SendRequestResponse` (`Body`, `BodyBinary`), `logic.HTTPResponseView` (`BodyBinary`), `inspect.BodyContent` (`Binary`), `logic.RawHTTP*Input` (`BodyBinary`), and `trafficBodyViewInner`.

- **Why `string`.** Strings are immutable. A body view built on a service goroutine is read on the main thread and then captured by background jobs for hex, image, SSE parsing, and save, so sharing it needs no defensive copy. The views already compare and slice bodies as strings. `CodeView.SetText` takes a string, and `logic.HexString` reads one in place. `encodeTrafficBodyView` can wrap freshly read bytes in a string with no copy, as it already does for text.
- **Alternative: `[]byte`.** Rejected. Any holder could mutate the shared bytes, so each handoff to a job would need a copy. It would also force conversions at every text view and change roughly twice as many signatures.
- **Why a `bool`.** The old string had exactly two values, `""` and `"base64"`. A named boolean says what it means: render as binary.

### 2. Chunks and icon data are `[]byte`, owned by the event

`TrafficLiveUpdate.Chunk` and `HTTPRequestStreamEvent.Chunk` are `[]byte`, and so is `ProcessIconData.Data`.

- **Ownership.** The publisher passes a fresh copy, using `bytes.Clone`. The SSE capture `onChunk` receives `p[:n]` of the caller's `Read` buffer, and `runHTTPRequestStreamReadLoop` reuses its own `buffer`. Base64 encoding used to copy implicitly; without it, sharing these slices would corrupt queued events. Subscribers treat the slices as read-only.
- **Why not `string`.** `[]byte` avoids a conversion before `utf8StreamDecoder.decode([]byte)` and before `append`. The batcher already sizes `[]byte` by its length.

### 3. Desktop consumers read bodies in place

- **`logic`.** `ClassifyBody(contentType string, binary bool)` and `BodyTabs(..., binary bool, ...)` use `len(body)` for the formatted-size threshold. `DecodeHexInput` and `EstimateDecodedByteLength` are deleted. `HexBytes` is deleted too if nothing else uses it; today only the Base64 load path does. `HexString` remains the single hex source. `SerializeHexSelection`'s `utf8Text` is `!binary`.
- **`inspect.HexView`.** `load` sets `logic.HexString(body)` at once for every body. The asynchronous decode, the `loading` and `failed` states, and the `detailHexDecodeFailed` locale key, which has no other use, are deleted. A body that grows keeps its reading position and selection, as before.
- **`inspect` image.** `decodeImage` takes the body string and converts it to `[]byte` inside its background job, so the main thread never copies a large body.
- **Raw view and cURL.** `formatRawMessage` writes `RawHTTPBinaryBody` when the body is binary. `BuildCurl(entry, body, binary)` Base64-encodes the body itself, giving the exact command text it produced before.
- **Saving.** `SaveBodyToFileRequest` keeps `Body string`, with raw bytes, and loses `BodyEncoding`. The service writes `string2Bytes(req.Body)`. Hex export passes the joined selection without encoding it.
- **WebSocket messages.** `WebSocketMessage.Data` holds raw bytes for every message type. `MessageStream` builds the hex view with `binary = true`, and Save sends the bytes as they are.

### 4. Live stores append bytes

- **Traffic store.** `prepareBase64SSESnapshot` becomes `prepareBinarySSESnapshot`. When a binary-marked snapshot receives SSE chunks, it decodes the snapshot through `utf8StreamDecoder` and clears the marker, the same switch as today with no Base64 step. `applyLive` passes `update.Chunk` directly. Offset and gap checks are unchanged.
- **Workspace store.** `httpStreamRuntime.binary` and the per-flush `EncodeToString` of the whole body are deleted. A binary stream appends `string(fresh)` to `response.Body` and a text stream appends `decoder.decode(fresh)`, so both paths share one append. The initial offset is `len(response.Body)` in both cases. Each flush still copies the body once when it concatenates, as text does today, but encoding no longer grows with the stream.
  - **Alternative: a chunked byte store.** Rejected for this change. It would change `HTTPResponseView` and every reader of its body for a gain that only shows on very long binary streams.

### 5. HBIN v3: raw binary WebSocket payloads

`hbinVersionCurrent` becomes 3, and the layout stays identical to v2.

- **Write.** v3 writes `msg.Data` for every message.
- **Read.** The body decode path takes the file's format version. `DecodeTrafficBody(r)` becomes `DecodeTrafficBodyWithVersion(r, version)`, mirroring `DecodeTrafficEntryWithVersion`, and `hbinReadEntryBody` takes the version too. For `version < 3` and `msgType == binary`, the payload is decoded with `base64.StdEncoding`. If that decode fails, the stored bytes are kept, so a damaged message still opens instead of failing the whole entry.
- **Callers.**
  - `history_service.GetHistoryTrafficBodyView` passes `fileIndex.formatVersion`.
  - `DecodeTrafficHARBodies` and `DecodeTrafficRequestBody` do not read WebSocket payloads, so they pass the version only to keep parsing in one shape.
  - `logic.IsHARExportableHistoryFormat` accepts 3.
  - HAR import already writes `hbinVersionCurrent`.
- **Why not keep v2 and convert in the codec.** The user chose v3. It also removes Base64 from disk and avoids an encode on every history save.

### 6. Service-side binary decisions keep their rules, with new names

- **Renames.** `shouldEncodeBodyForTrafficView` becomes `shouldShowBodyAsBinary`, and `encodeBodyAsBase64` becomes `bodyBinary`. The conditions do not change.
- **Plugin responses.** `materializeHTTPRequestPluginResponseBody` and `materializeHTTPRequestPluginResponseReader` lose `encodeBase64`. They return the inline bytes as a string, or read a file-backed result into one. The response sets `BodyBinary` when the plugin kind is binary and the body is not empty, the same condition as today.
- **Request recovery.** `decodeTrafficRequestBody(bodyView)` becomes `string2Bytes(bodyView.RequestBody)`. `shouldRecoverRequestSingleFile` checks `RequestBodyBinary`.

## Risks / Trade-offs

- **Reused read buffers.** If a chunk is published without copying it, a queued event can change after it is sent. → Copy in both publishers. Add a test that publishes, overwrites the source buffer, and asserts that the delivered chunk is unchanged.
- **Invalid UTF-8 in text views.** A non-UTF-8 body could reach a text view if a binary marker is missed. → The service sets the marker from the same tracked `UTF8Valid` as today, and `BodyViewer.set` still sends binary bodies only to the hint and Hex view. The existing classification tests are updated rather than removed.
- **Older builds and v3 histories.** A build that only knows v2 skips v3 files and does not delete them. Downgrading hides histories saved after the upgrade. → Note this in the 1.1.0 changelog section. 1.0.x already cannot update to 1.1.0 by itself.
- **Damaged Base64 in old files.** A v1 or v2 binary message may not decode. → Keep its stored bytes, as described in Decision 5, and cover this with a codec test.
- **Spec ordering.** The `traffic-history` delta modifies a requirement that the unarchived `migrate-to-mygo-native-ui` change adds. → Archive that change first, as the proposal records.
- **Wide signature churn.** About 20 production files and 25 test files change. The views share `BodyContent` and `HexView` across traffic, editor, and WebSocket payloads, so no producer can switch alone without a temporary dual path. → Switch the service layer first, verified by service package tests alone, since those packages do not import `internal/desktop`. Then switch the interface, after which the whole module builds again. Commit only after both are done.
  - **Alternative: a temporary dual path.** The views would accept both the Base64 marker and raw bytes, and each producer would move over in turn. Rejected: it adds and then deletes a second decode path in every view.

## Migration Plan

1. **Deploy.** Data needs no migration. The first history saved after the upgrade is v3, and existing v1 and v2 files are read in place.
2. **Roll back.** Revert the commits. v3 files written in the meantime are skipped by the reverted build but stay on disk, and are readable again after a re-upgrade.
