# Tasks

## 1. Staged reads

- [x] 1.1 Add context-aware metadata/body reads and guarded staged history state; verify v1-v3 parity, no body reads for metadata, cancellation, failure, full actions, and demand deduplication with `CGO_ENABLED=0 go test ./internal/services/history_service ./internal/desktop/store ./internal/desktop/capture`; update memory and traffic technical references.

## 2. Loading transitions

- [x] 2.1 Preserve pending detail chrome, request bodies from dependent panes, and delay indicators using the frame clock; verify fast/slow transitions, disabled pending actions, Raw copy readiness, selection cleanup, and closing with `CGO_ENABLED=0 go test ./internal/desktop/inspect ./internal/desktop/capture`; inspect generated light/dark renders for both layouts and update the user guide.

## 3. Integration

- [x] 3.1 Run `task fmt:check lint:go vet:ui i18n:check`, `CGO_ENABLED=0 go test ./... -skip '^TestClearLocalDataReportsPartialFailureWithResult$'`, and `openspec validate smooth-history-detail-loading --strict --no-interactive`; compare final behavior to all scenarios and record evidence and the known Windows baseline permission-test limitation.

## Acceptance evidence

- Windows amd64, CGO disabled. Focused service/store/inspect tests passed, followed by the full repository test command above (covering all package scopes in tasks 1 and 2). The pre-existing close-history regression now explicitly requests a body before verifying its release.
- Service tests compare staged and complete reads for HBIN v1-v3, retain metadata even with the body record removed, reject missing entries, propagate cancellation, and preserve legacy binary WebSocket payloads. Full action APIs remain unchanged.
- Store tests verify metadata without body requests, one payload read despite repeated demands, demand queued before metadata, cancellation/discarded results on replacement/close, metadata survival after body failure, no redraw retries, reopen retry, and TCP/invalid-metadata guards.
- Headless UI tests use MyGo's deterministic clock to check 179/180 ms boundaries, fast completion without a spinner, fresh deadlines on rapid selection, no stale indicator after close/completion, unchanged splitter bounds, disabled pending tabs, and complete Raw copy readiness. Existing cache-release, capture/action, selection/focus, body-mode, SSE, and layout regressions passed.
- `FLOWLENS_RENDER_DIR=<local render directory> CGO_ENABLED=0 go test ./internal/desktop/inspect -run '^TestRenderHistoryLoadingTransitions$' -count=1` passed and produced 24 renders across English/Chinese, light/dark themes, horizontal/vertical layouts, and fast/slow/ready states. Visual inspection confirmed stable panes, visible close controls, and appropriate loading feedback in light horizontal and dark vertical samples.
- `task fmt:check lint:go vet:ui i18n:check` passed with zero lint issues and 1338 messages in each locale. `git diff --check` and strict change validation passed.
- Both deltas were synchronized without changing existing Purpose/requirements; exact content and duplicate/section checks passed. `openspec validate --specs --strict --no-interactive` passed all 18 specifications. Formatting and Go lint were rechecked after the final release-test adjustment.
- Only `TestClearLocalDataReportsPartialFailureWithResult` remains excluded from the full Go run: its Windows chmod failure was previously reproduced on clean baseline `1f07797`. No new exclusions or changes to that unrelated test.
- Implementation, scenarios, user guide, and technical references were compared. No format, network model, action payload, or live-stream subscription changes. No manual installed-app profiling, native macOS/Linux runs, or optional race run was performed; rendering acceptance used the real headless MyGo view implementation. Very slow metadata reads still leave a pending content area beneath stable chrome, without retaining previous payloads.

## Workflow follow-up

- Synchronize both deltas, validate all main specs strictly, and archive the completed change.
