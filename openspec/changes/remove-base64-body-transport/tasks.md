# Tasks

**Build state between groups.** Group 1 changes in-process types that `internal/desktop` reads, so the module does not build again until group 2 is done (design: "Wide signature churn"). Verify group 1 with service package tests only. Commit once group 2 builds and passes, following the Conventional Commit rules in `AGENTS.md`.

## 1. Services pass raw payloads (`proxy_service`, `history_service`)

- [x] 1.1 Body views carry raw bytes (design decisions 1 and 6):
  - **Fields.** In `TrafficBodyView` and `trafficBodyViewInner`, replace `RequestBodyEncoding` and `ResponseBodyEncoding` with `RequestBodyBinary` and `ResponseBodyBinary`.
  - **Building the view.** `encodeTrafficBodyView` returns `bytes2String(body)` for every body.
  - **Setting the marker.** `getTrafficBodyViewInner` sets it from `shouldShowBodyAsBinary(...) || !UTF8Valid()`, renamed from `shouldEncodeBodyForTrafficView`.
  - **HBIN.** Flag byte 1 maps to the marker in both directions.
  - **Request body.** `DecodeTrafficRequestBody` returns `([]byte, bool, error)`.
  - **Recovery.** `decodeTrafficRequestBody` uses `string2Bytes`, and `shouldRecoverRequestSingleFile` checks `RequestBodyBinary`.

  Update the tests:
  - `traffic_codec_test.go`, `body_cache_integration_test.go`, and `request_body_recovery_test.go`.
  - Add a case where a non-UTF-8 `text/plain` body gives the marker and bytes identical to the capture.
  - Add a case where recovering an `application/octet-stream` request writes a file identical to the body.

  Run `go test ./internal/services/proxy_service`.
- [x] 1.2 Live chunks carry bytes (decision 2):
  - Replace `TrafficLiveUpdate.ChunkBase64` and `HTTPRequestStreamEvent.ChunkBase64` with `Chunk []byte`.
  - Publish `bytes.Clone(data)` from the SSE capture `onChunk`, and `bytes.Clone(buffer[:n])` from `runHTTPRequestStreamReadLoop`.

  Update `live_updates_test.go`, `frontend_events_test.go`, and `frontend_event_batcher_test.go`. For each publisher, add a test that overwrites the source buffer after publishing and asserts that the delivered chunk is unchanged. Run `go test ./internal/services/proxy_service`.
- [x] 1.3 Editor responses carry bytes (decision 6):
  - **Field.** Replace `SendRequestResponse.BodyEncoding` with `BodyBinary`.
  - **Materializing.** Drop `encodeBase64` from `materializeHTTPRequestPluginResponseBody` and `materializeHTTPRequestPluginResponseReader`.
  - **Marker.** Set `BodyBinary` under the existing conditions:
    - a binary content type,
    - an unsupported content encoding on a stream,
    - a non-empty binary plugin result.

  Update the tests:
  - `send_http_request_test.go`, `http_request_plugins_response_test.go`, and `http_request_plugin_body_store_test.go`.
  - Add a case where a binary plugin result larger than 4 MiB, file-backed, reaches `SendRequestResponse.Body` byte for byte.

  Run `go test ./internal/services/proxy_service ./internal/services/python_plugin_service`.
- [x] 1.4 Save and icons carry bytes:
  - Remove `SaveBodyToFileRequest.BodyEncoding`. `decodeBodyToFileBytes` returns `string2Bytes(req.Body)` and still rejects an empty body.
  - Replace `ProcessIconData.DataBase64` with `Data []byte`.

  Update `image_preview_save_test.go` and `process_attribution_test.go`, asserting that the written file and the icon bytes equal their sources. Run `go test ./internal/services/proxy_service`.
- [x] 1.5 Raw WebSocket payloads and HBIN v3 (decision 5):
  - **Payloads.** `normalizeWebSocketMessage` stores raw bytes for binary messages.
  - **Version.** Set `hbinVersionCurrent = 3`.
  - **Reading.** Thread the format version into `hbinReadEntryBody`, `DecodeTrafficHARBodies`, and `DecodeTrafficRequestBody`. Replace `DecodeTrafficBody` with `DecodeTrafficBodyWithVersion`.
  - **Legacy payloads.** For `version < 3`, Base64-decode binary payloads, and keep the stored bytes when the decode fails.
  - **Codec comment.** Rewrite the layout comment in `traffic_codec.go`: flag 1 means binary display, and v3 stores raw binary WebSocket payloads.

  Add tests to `traffic_codec_test.go`:
  - A v3 round trip of a binary message with every byte value from `0x00` to `0xff`.
  - A v2 fixture whose Base64 payload reads back as the original bytes.
  - A v2 payload with invalid Base64 that reads back as its stored bytes.
  - `isSupportedHistoryFormatVersion(4) == false`.
  - `har_import_test.go` asserting that an import writes v3.

  Run `go test ./internal/services/proxy_service`.
- [x] 1.6 History reads use the version. `HistoryService.GetHistoryTrafficBodyView` passes `fileIndex.formatVersion` to `DecodeTrafficBodyWithVersion`. Add a `history_service` test that writes a v2 and a v3 history, each with the same binary WebSocket message, and asserts that both body views return identical `Data`. Run `go test ./internal/services/history_service ./internal/services/proxy_service`.

## 2. The interface reads raw bytes (`internal/desktop`)

- [x] 2.1 `logic` (decision 3):
  - **Body helpers.**
    - `ClassifyBody(contentType string, binary bool)`.
    - `BodyTabs(contentType string, binary bool, body string, enableSSE bool)`, using `len(body)` for the formatted threshold.
    - Delete `DecodeHexInput`, `EstimateDecodedByteLength`, `base64Encoding`, and `HexBytes` if it has no other user.
  - **Raw HTTP.** `RawHTTPRequestInput` and `RawHTTPResponseInput` get `BodyBinary bool`.
  - **Response view.** `HTTPResponseView.BodyBinary` replaces `BodyEncoding`, set in `ResponseViewOf`.
  - **cURL.** `BuildCurl(entry, body string, binary bool)` encodes Base64 itself.
  - **History formats.** `IsHARExportableHistoryFormat` accepts 3.

  Update `body_test.go`, `hexdump_test.go`, `traffic_test.go`, `text_utilities_test.go`, and `category_test.go`. The `BuildCurl` tests assert that binary and text output are byte-identical to the current output for the same bytes. Run `go test ./internal/desktop/logic`.
- [x] 2.2 `inspect` (decision 3):
  - **Body content.** `BodyContent.Binary` replaces `Encoding`, in `bodyPane`, `raw.go`, and `editor/response.go`.
  - **Hex view.**
    - `HexView.Build(c, theme, identity, body string, binary bool)` loads `logic.HexString` synchronously.
    - Delete the `loading` and `failed` paths.
    - Delete `detailHexDecodeFailed` from `locales/en.json` and `locales/zh-Hans.json`, then run `task i18n:generate`.
  - **Image.** `decodeImage` converts the string inside its job, and the size label uses `len(body)`.
  - **SSE.** `sse.go` checks `Binary`.
  - **WebSocket messages.** `messages.go` builds the hex view with `binary = true` and saves `Data` unencoded.
  - **Saving.** Body save and hex export send raw bytes.

  Update `inspect_test.go`, `image_test.go`, and `render_test.go` with `ui.NewTester` cases for the traffic-inspection scenarios:
  - "Binary body in Hex view": the first frame shows bytes with no spinner.
  - "Saving a binary body", "Saving a binary message", and "Exporting a hex selection": the `SaveBody` request holds the exact bytes.
  - "Copying a selection as Base64".
  - "Body with invalid UTF-8".

  Run `go test ./internal/desktop/inspect ./internal/desktop/editor`, `task i18n:check`, and `task vet:ui`.
- [x] 2.3 Stores (decision 4):
  - **Traffic store.** `prepareBase64SSESnapshot` becomes `prepareBinarySSESnapshot`, and `applyLive` reads `update.Chunk`.
  - **Workspace store.** `RegisterHTTPStream` starts at `len(response.Body)`. `flushStream` has one append path. Delete `httpStreamRuntime.binary` and the per-flush re-encoding.

  Update `traffic_test.go` and `workspace_test.go` with cases for:
  - A binary snapshot that switches to text when an SSE chunk arrives.
  - A gap that triggers a snapshot reload.
  - A binary editor stream that appends three chunks in order, delivered out of order, with the middle chunk deferred.
  - A text stream that is unchanged, including a UTF-8 rune split across two chunks.

  Run `go test ./internal/desktop/store`.
- [x] 2.4 Remaining callers:
  - `capture/menu.go` passes `RequestBodyBinary` to `BuildCurl`.
  - `widgets/process_icon.go` decodes `data.Data` directly.

  Run `go build ./...` and `go test ./internal/desktop/... ./internal/app`. The module now builds and passes. Commit groups 1 and 2.

## 3. History format documentation

- [ ] 3.1 Update the HBIN references:
  - The `AGENTS.md` networking bullet: write HBIN v3, and read v1, v2, and v3.
  - `docs/user-guide.md` lines on HAR import ("HBIN v2 history") and on history versions: v3 is written, and v1 and v2 stay readable.
  - The `docs/technical/http-rewrite-rules.md` HBIN sentence.
  - A bullet in the `CHANGELOG.md` 1.1.0 "Installing this version" list: history saved by 1.1.0 needs 1.1.0 or later to open.

  Verify that `grep -rn "HBIN v2\|writes v2" AGENTS.md docs` finds only descriptions of older files, then commit as `docs(history): document HBIN v3`.

## 4. Integration verification

- [ ] 4.1 Audit the remaining Base64 uses. Run `grep -rn "base64" internal --include='*.go' | grep -v _test.go`. It must list only the format sites in the design's Context table:
  - HAR,
  - the Python Worker wire and the plugin binary body,
  - `Proxy-Authorization`,
  - the macOS trust plist,
  - hex "Copy as Base64",
  - `BuildCurl`,
  - the legacy WebSocket decode in the HBIN reader.

  Record the result in the apply notes.
- [ ] 4.2 Run the full checks:
  - `task verify`.
  - `CGO_ENABLED=1 go test -race ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service`, because live chunk ownership is concurrency-sensitive.
  - `go test ./internal/services/python_plugin_service` with Python 3 available, so the binary response-hook integration tests in `worker_integration_test.go` run without skips.

  Report any check that cannot run, and why.

## Workflow follow-up

- Archive `migrate-to-mygo-native-ui` before archiving this change, since the `traffic-history` delta modifies its "Existing data compatibility" requirement.
- A desktop pass on one platform is worth doing, though the headless tests cover these surfaces:
  - Open a captured PNG and a binary WebSocket message.
  - Save each one.
  - Open a v2 history recorded before the upgrade.
