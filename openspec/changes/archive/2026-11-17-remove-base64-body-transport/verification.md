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

## Streaming append allocation follow-up

Measured on 2026-11-17 with Go 1.27.3, Windows amd64, Intel Core i9-14900HX, `CGO_ENABLED=0`. The baseline uses the same new benchmark against the original `response.Body += appended.String()` implementation.

```text
go test ./internal/desktop/store -run '^$' -bench '^BenchmarkHTTPStreamAppend$' -benchmem -benchtime=1x -count=3
```

Each operation appends a complete stream through `flushStream`, flushing every 64 KiB chunk separately. Values below are median allocated MiB per operation across three runs, not peak or retained heap size. Binary chunks contain NUL and invalid UTF-8; text chunks contain four-byte UTF-8 characters.

| Body size | Binary before | Binary after | Text before | Text after |
| --- | ---: | ---: | ---: | ---: |
| 4 MiB | 133.96 | 19.26 | 138.02 | 23.32 |
| 8 MiB | 523.97 | 48.26 | 532.03 | 56.33 |
| 16 MiB | 2072.00 | 95.16 | 2088.06 | 111.22 |
| 32 MiB | 8240.06 | 186.79 | 8272.12 | 218.85 |

Doubling the stream previously approximately quadrupled allocation. With the persistent builder, the 8/16/32 MiB runs approximately double allocation instead; growth-capacity rounding accounts for deviations. The 32 MiB binary run allocates 97.7% fewer bytes. These measurements cover body accumulation, not the cost of formatting or rendering the response.

- `TestHTTPStreamBodyAllocationBudget` limits an 8 MiB stream to 96 MiB of cumulative allocation, allowing buffer growth, decoding, and event bookkeeping. Both binary and text cases fail on the original implementation (about 524/532 MiB) and pass after the fix (about 48/56 MiB).
- `TestStreamBodySnapshotsSurviveAppendAndNextResponse` covers an existing response prefix, multiple growth steps, completion, a new response in the same tab, and retention of every earlier snapshot for binary and text bodies.
- `TestStreamTextBodyUsesWireOffsetsAfterDecoding` covers a split BOM and UTF-8 character, invalid UTF-8 replacement, overlapping chunks, and duplicates across separate flushes. Existing ordering, gap, stop, and close tests also pass.
- `task verify` passes on this Windows host: formatting, golangci-lint (0 issues), UI vet, i18n (1387 messages), and all Go test packages, including the theme package that failed on the earlier Linux host.
- `CGO_ENABLED=1 go test -race ./internal/desktop/store ./internal/desktop/editor ./internal/desktop/inspect` passes, including the new snapshot and allocation-budget tests.
- The broader `CGO_ENABLED=1 go test -race ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service` run passes every package except `desktop/capture`, which reaches the default 10-minute package timeout. At the deadline, `TestCategoryGroupsShareOuterScroll` had been running for 2 seconds, with the stack in MyGo layout. No data race was reported; the complete capture race suite remains unverified. Its ordinary tests pass in `task verify`.
