# Implementation and verification

Date: 2026-11-17. Go 1.27.3, Linux arm64, Python 3.13.5. Branch `remove-base64-body-transport`: `991dc8a` (groups 1 and 2) and `f884e06` (group 3).

## Deviations from the task text

- **Invalid UTF-8 marker on view reads.** `encodeTrafficBodyView` returns the raw bytes for every body, as specified, and still sets the binary marker when the bytes are not valid UTF-8. Without it, history reads and cache-backed bodies without tracked capture state would lose the marker for invalid UTF-8, which the design's "rules stay as they are" non-goal and the "Body with invalid UTF-8" scenario require.
- **History traffic export accepted only v1/v2.** `history_service/traffic_export_service.go` rejected v3 with "unsupported version". It now accepts v1 to v3, as the "Exporting a v3 history" scenario requires. `TestHistoryTrafficExportSupportedVersions` covers v3, and `TestExportHARStreamsV2ConnectionTimings` asserts that v2 and v3 export identical HAR.
- **`detailHexLoading` removed too.** Deleting the Hex view's loading path left this message with no user, alongside `detailHexDecodeFailed`. Both are removed from both catalogs and the accessors regenerated.
- **SSE capture publisher extracted.** The `onChunk` closure became `capturedSSEChunkPublisher`, so the buffer-ownership test exercises the real publisher.
- **Legacy WebSocket read avoids a copy.** `hbinReadWebSocketMessage` hands its freshly read buffer to the message with `bytes2String`.

## 4.1 Base64 audit

`grep -rn "base64" internal --include='*.go' | grep -v _test.go` lists only format sites:

| Site | Kind |
| --- | --- |
| `proxy_service/har_export.go`, `har_import_entry.go`, `har_post_data.go` | HAR export, import, and `postData` |
| `python_plugin_service/http_request_runner.go` | Python Worker wire |
| `proxy_service/http_request_plugins.go:291`, `proxy_service.go:3414` | Plugin `binary` body (`SendRequestBody{BodyType: binary}`) |
| `proxy_service/synthetic_transport.go` | `Proxy-Authorization` |
| `setting_service/ca_trust_security_plist.go` | macOS trust plist |
| `desktop/logic/hexdump.go`, `desktop/inspect/hex.go` (menu item) | Hex "Copy as Base64" |
| `desktop/logic/traffic_filter.go` | `BuildCurl` |
| `proxy_service/traffic_codec.go` | Legacy v1/v2 WebSocket decode in the HBIN reader |

## 4.2 Checks

- `task verify`: `fmt:check`, `lint:go` (0 issues), `vet:ui`, and `i18n:check` (1387 messages in en and zh-Hans) pass. `go test ./...` passes in every package except `internal/desktop/theme`, where `TestCustomTextMatchesControlsAcrossSizesAndDPI` fails ("text did not scale proportionally"). The same test fails identically on `813e94b`, before this change, in a clean worktree; the package does not depend on any changed code.
- `go test -v ./internal/services/python_plugin_service`: 147 tests ran against Python 3.13.5, 0 skipped, 0 failed, including the binary body and response-file cases in `worker_integration_test.go`.
- `CGO_ENABLED=1 go test -race -timeout 30m ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service`: no data race reported. `proxy_service`, `process_attribution`, `desktop/store`, `desktop/inspect`, `desktop/editor`, and every other desktop package pass, except:
  - `desktop/theme` fails on the same typography test as above.
  - `desktop/capture` reached the 30-minute timeout. That package takes about 2 minutes without `-race`, and the test running at the deadline had started 2 seconds earlier, so the whole package is slow under `-race` rather than stuck on one test. Without `-race`, 12 layout, scrolling, and pixel tests take 70% of its time and run no background work.
  - At the user's request, the capture race run was narrowed to the 21 tests that cover code this change touched or background work in the package (`-race -v`, 483 seconds): 21 passed, no data race reported. Selected:
    - Changed code: `TestCopyCurlAndUnavailableBody` (`BuildCurl` with `RequestBodyBinary`), `TestRowMenuCopyValues`, `TestHistoryMenuBlocksExportOfUnreadableFormat` and `TestHistoryDeleteAndUnsupportedFormatStaysListed` (history format checks).
    - Body views loaded in jobs: `TestEditLimit` (the only Edit test; 50 chained body loads into the editor), `TestResendRefusesUnavailableBody`, `TestRowMenuShowDetails`, `TestDetailPanelOpensBesideTheTable`, `TestLateDetailBodyCannotReplaceTheCurrentView`, `TestHistoryLoadingKeepsRowsAndDetail`, `TestHistoryInitialLoadingKeepsFiltersAndHeader`, `TestHistoryListOpensAndShowsTraffic`, `TestHistoryClearAll`.
    - Export and import jobs: `TestExportSelection`, `TestExportSingleFileUsesFileTarget`, `TestExportManyEntriesUsesDirectory`, `TestExportDialogCancelDoesNothing`, `TestHistoryExportUsesHistoryService`, `TestHARImportFromDropOpensResult`, `TestHARImportPartialFailureAndCancel`.
    - Process icon cache jobs: `TestProcessColumnStates`.
  - The remaining 114 capture tests (layout, scrolling, pixels, keyboard, selection gestures, category lists, toast timing) pass without `-race` in `task verify`.
- `openspec validate remove-base64-body-transport --strict`: valid. It notes that archive would refuse the `traffic-history` delta until `migrate-to-mygo-native-ui` is archived, as the proposal records.
- `git diff --check`: clean.

## Not run

- The desktop pass in "Workflow follow-up" (open and save a captured PNG and a binary WebSocket message, open a v2 history) was not run; this session had no display. The headless `ui.NewTester` cases cover those surfaces.
