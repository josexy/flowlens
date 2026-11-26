# Tasks

## 1. Selective history decoding

- [x] 1.1 Implement the shared summary decoder with bounded skipping, integrate history-list reads, and replace decode-then-strip; verify v1-v3 content parity, truncation/flag/count errors, cancellation, and allocation bounds with `CGO_ENABLED=0 go test ./internal/services/proxy_service ./internal/services/history_service -run 'TestHistory(Summary|Summaries)|TestHBIN(Summary|Skip)' -count=1`; update both affected technical references.

## 2. Integration verification

- [x] 2.1 Verify the implementation against every scenario with `task fmt:check lint:go vet:ui i18n:check`, `CGO_ENABLED=0 go test ./... -skip '^TestClearLocalDataReportsPartialFailureWithResult$'`, a focused summary allocation benchmark, and `openspec validate skip-history-detail-fields --strict --no-interactive`. Record results and the previously reproduced Windows baseline permission-test limitation; review the final diff and behavior/docs alignment. No UI/Python/native changes require new runtime acceptance.

## Acceptance evidence

- Platform: Windows amd64, CGO disabled; no new UI or native platform behavior.
- Task 1's exact targeted command passed, covering v1-v3 HTTP/TCP records, optional and empty fields, first duplicate/empty Content-Type, all truncation points, invalid header flags/counts, a truncated 4 GiB declared length, reader errors, and actual history-reader cancellation during a 1 MiB Cookie.
- `task fmt:check lint:go vet:ui i18n:check` passed (0 lint issues; 1338 messages in both languages). `git diff --check` passed.
- `CGO_ENABLED=0 go test ./... -skip '^TestClearLocalDataReportsPartialFailureWithResult$'` passed across all packages, including capture/actions, editor/store, HBIN/HAR/export, and history tests. The skipped test's reliance on chmod removing Windows directory-delete permission was already reproduced on clean baseline `1f07797` in the preceding memory optimization; no new exclusion was introduced and that unrelated test was not changed.
- `CGO_ENABLED=0 go test ./internal/services/proxy_service -run '^$' -bench '^BenchmarkHistoryEntryDecode$' -benchmem -benchtime=300ms -count=1` passed. With 4 KiB repeated detail fields, full/summary reads allocated 175531/3295 B per record; with 64 KiB repeated fields, 2756024/3287 B. The benchmark uses a synthetic record with multiple headers, TLS fields, and certificate names, not one cookie alone.
- The 2000-row, 4 KiB Cookie history regression measured total allocations of about 10.20 MiB for complete reads and 2.42 MiB for list reads; retained heaps were about 9.70/1.80 MiB. A subsequent full detail still contained the complete Cookie.
- `openspec validate skip-history-detail-fields --strict --no-interactive` passed. Diff review confirmed only list decoding selects summary mode, and writer/body/action APIs keep their behavior. Both technical references describe bounded consumption and the distinction between allocations and process memory.
- The traffic-history delta was synchronized with the existing Purpose and requirements preserved. Exact delta/target comparison and duplicate/section checks passed; `openspec validate --specs --strict --no-interactive` passed for all 18 capabilities.
- Limits: skipped bytes still pass through bounded buffers; required row data remains proportional to history size. Measurements describe Go allocations/heap in synthetic tests, not an installed application's Task Manager working set. Manual native runs and optional race checks were not needed for this decoder-only change.

## Workflow follow-up

- Synchronize the traffic-history delta, run `openspec validate --specs --strict --no-interactive`, and archive the completed change.
