# Verification

Reviewed and verified on Windows amd64 with Go 1.27.3 on 2026-11-18.

## Review fixes

| Issue | Regression coverage | Result |
| --- | --- | --- |
| A stalled UI received another batch after two seconds | `TestFrontendEventBatcherKeepsClaimsBoundedDuringLongStalls` exercises event-count and byte limits over three three-second stalls; `TestConnectKeepsAtMostOneBatchWaitingForTheMainThread` holds the real router for ten simulated seconds and verifies snapshot recovery; the close test verifies abandoned claims do not block shutdown or retain pending payloads. | Passed. Time-based release is removed. |
| Completed exchanges remained in memory without new traffic | `TestSpillerWakesWhenResponsesFinishWithoutNewTraffic` completes 6000 real intercepted HTTP responses after insertion wakeups have drained; `TestSpillerWakesForOneOldBodyBelowTheBatchThreshold` finishes the last old body with only 5001 entries remaining. Neither manually runs the spiller after completion. | Both failed before the fix and passed afterwards. |
| Closed history retained hidden detail caches | `TestHistoryDetailReleaseDropsRenderedCachesAndReopens`, `TestPanelReleaseDiscardsPendingViewerResults`, and capture workspace tests cover viewer data, queued results, closing, reopening, replacement and store clearing before tab closure. | Passed. Both light and dark history-closed renders were inspected. |
| SSE updated the store incrementally but reset the text viewers | `TestSSEBodyViewerAppendsWithoutCopyingTheSnapshot`, `TestSSEBodyViewerBuildKeepsRawAndHexCurrent`, and `TestSSEBodyViewerHandlesReplacementAndTextBoundaries` cover allocations, real Raw/Hex/SSE frames in both themes, replacement, binary-to-text transitions, Chinese/emoji and split CRLF. Existing store UTF-8/gap tests also pass. | A 1 MiB snapshot followed by 32 small chunks allocated 87,754,560 bytes before the fix and 77,040 bytes after it. Read-only streamed text keeps no undo history. |

## Checks

- `task verify`: passed, including formatting, golangci-lint (zero issues), MyGo UI vet, catalog validation and `CGO_ENABLED=0 go test ./...`.
- `openspec validate bound-capture-history-memory --strict`: passed.
- `openspec validate --specs --strict`: all 17 main specs passed after synchronization.
- `CGO_ENABLED=1 go test -race -timeout 30m ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service ./internal/pkg/body_cache ./internal/services/history_service`: all packages passed without a race report. The capture package took 730.83 seconds, exceeding the default ten-minute timeout that interrupted the earlier review run; no tests were excluded.
- `FLOWLENS_SOAK=1 CGO_ENABLED=0 go test ./internal/services/proxy_service -run '^TestCaptureSoak$' -count=1 -v`: passed on rerun. It captured 200000 HTTP exchanges and 20 WebSocket connections in 33.865 seconds (36.26 seconds for the complete test). Live heap was 15.1 MiB at 50000 entries and 23.2 MiB at 200000. At the final sample, memory held 5076 entries and disk held 194944. Goroutines returned to 2, matching the baseline, and Restart removed the session directory.

The first soak attempt, run alongside the full race suite, exceeded its 30-second spiller catch-up deadline with 35971 entries still in memory. An overlay of the original PR backend passed in 38.06 seconds; the current code then passed in 36.26 seconds. No production code, memory assertion or test deadline was relaxed to obtain the pass. The catch-up deadline is sensitive to host load and filesystem throughput; the first failure remains part of the verification record.

## Follow-ups outside this change

- Request Editor streams: define and implement back-pressure for `PublishUnbounded` stream chunks while preserving terminal-event order and complete stream data.
- WebSocket Client: define a message count/byte retention policy and its visible truncation behavior; this change only spools captured WebSocket payloads.
- History format: design a version with 64-bit offsets and compatibility readers to remove HBIN v3's 4 GiB indexing constraint. This change continues writing v3.
