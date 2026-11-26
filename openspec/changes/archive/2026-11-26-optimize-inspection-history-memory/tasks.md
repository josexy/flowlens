# Tasks

## 1. Response viewers

- [x] 1.1 Release obsolete read-only text and hidden response state; lazily prepare body modes. Add retention, mode-switch, copy/save, and SSE regressions; update memory diagnostics. Verify with CGO_ENABLED=0 go test ./internal/desktop/widgets ./internal/desktop/inspect ./internal/desktop/editor.

## 2. HBIN bodies

- [x] 2.1 Transfer owned decoded bytes without another full copy; preserve legacy binary, empty/missing, and HAR behavior. Add allocation/ownership regressions and document the read path. Verify with CGO_ENABLED=0 go test ./internal/services/proxy_service ./internal/services/history_service.

## 3. History loading

- [x] 3.1 Load lightweight history summaries and complete details/action inputs on demand with cancellation and stale-result rejection. Add service/store/table compatibility and retained-memory regressions; update diagnostics and technical documentation. Verify with CGO_ENABLED=0 go test ./internal/services/history_service ./internal/desktop/store ./internal/desktop/capture ./internal/desktop/inspect ./internal/desktop/logic.

## 4. Integration

- [x] 4.1 Run task verify and openspec validate optimize-inspection-history-memory --strict --no-interactive; compare implementation and spec scenarios, record measured results and remaining platform limitations.

## Verification evidence

Validated on Windows amd64, Go 1.27.3, with CGO_ENABLED=0.

- The targeted widget/inspect/editor and history/store/capture/logic package suites passed. Added regressions cover replacement while a request tab is inactive, hidden body tabs, mode-specific copy/save and CRLF, incremental SSE, cancellation and discarded results, closing a loading panel in both themes, complete cURL/edit inputs, every table sort/filter/export-availability rule, TCP ALPN, and HBIN v1-v3 details including legacy binary WebSocket payloads.
- Body ownership tests prove that history views reuse their exclusively owned decoded allocation, survive reader cleanup, and cannot be changed by a mutable live-capture buffer.
- `task verify` passed formatting, lint, UI lifetime checks, and both locale catalogs. Its Go test stage failed only at the pre-existing `TestClearLocalDataReportsPartialFailureWithResult`: Windows allowed removal after the test's POSIX-style directory chmod. The same test failed identically in a clean detached worktree at baseline `1f07797`; that worktree was removed. No production or test code was changed to hide this unrelated failure.
- On the final implementation, `task fmt:check lint:go vet:ui i18n:check` passed (zero lint issues), and `go test ./... -skip '^TestClearLocalDataReportsPartialFailureWithResult$'` passed across the repository. The explicit exclusion is solely the reproduced baseline failure; unfiltered `task verify` is not claimed to pass.
- `openspec validate optimize-inspection-history-memory --strict --no-interactive` passed. Implementation and scenarios were compared before synchronization/archive.
- Synchronized `traffic-history` and `traffic-inspection` while preserving their existing content and Purpose sections. `openspec validate --specs --strict --no-interactive` passed all 18 main specifications.

Measured retained Go heap after collection (synthetic data, not whole-process working set):

| Case | Before | After |
| --- | --- | --- |
| 20 distinct 1 MiB responses in one viewer | 42.57 MiB | 2.05 MiB |
| Replace those responses with 4 bytes | 41.57 MiB | 0.01 MiB |
| 2,000 history rows with a 4 KiB Cookie per row | 9.71 MiB full records | 1.80 MiB summaries |
| New response while Headers hides a previously rendered large body | 12.16 MiB loaded | 0.20 MiB after replacement |

Remaining limits: full history headers are decoded transiently one record at a time, summary memory still scales with row count, and selected payloads can still be large. OS memory return and native caches are independent of retained-heap reductions. No native macOS/Linux runtime or manual installed-app acceptance was performed. The unrelated Windows permissions test remains a known full-check failure.

## Workflow follow-up

- Synchronize deltas, run openspec validate --specs --strict --no-interactive, and archive this completed change.
