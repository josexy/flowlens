# Tasks

## 1. Bounded history transitions

- [x] 1.1 Keep one previous history detail with a non-extending 180 ms deadline and immediate terminal cleanup; add store tests for timeout without rendering, rapid selection, failures, stale callbacks, and close/reset/delete. Update the technical reference. Verify with `CGO_ENABLED=0 go test ./internal/desktop/store`.
- [x] 1.2 Render the previous view with disabled content and an active close action until new metadata and required bodies settle; add MyGo transition tests for fast/slow reads, body demand, input safety, both layouts, themes, and languages. Update the user guide. Verify with `CGO_ENABLED=0 go test ./internal/desktop/inspect ./internal/desktop/capture ./internal/desktop/widgets` and inspect transition renders.

## 2. Integration verification

- [x] 2.1 Run `task vet:ui`, `CGO_ENABLED=0 go test ./...`, and `openspec validate smooth-history-detail-switching --strict --no-interactive`; compare implementation, specs, documentation, and acceptance evidence, recording remaining platform limitations.

## Acceptance evidence

- Windows, Go 1.27.3, ordinary checks with `CGO_ENABLED=0`: `go test ./...` passed. Final scoped reruns of `./internal/desktop/store`, `./internal/desktop/inspect`, `./internal/desktop/capture`, and `./internal/desktop/widgets` passed after the transition and focus changes.
- `task vet:ui` passed; `task lint:go` reported zero issues. Changed Go files are gofmt-clean and `git diff --check` passed.
- Render tests cover English and Simplified Chinese, light/dark themes, and horizontal/vertical layouts. Retained frames match the preceding frame pixel-for-pixel. Fast metadata/body replacement, a shared timeout, rapid selection, failed reads, obsolete callbacks, closed/reset/deleted selections, and timeout without rendering are covered.
- Native MyGo tester input covers disabled tabs/copy/popovers, keyboard copy from a previously focused body editor, retained close, and table focus after selecting a different row. The editor-focus regression required moving focus only when the previous detail still owned it.
- Rendered pending/ready/slow states were inspected from the temporary `flowlens-history-transitions` directory. No live application window was launched; interactive native Windows/macOS/Linux acceptance was not performed.
- Change strict validation and all 18 main-spec strict validations passed. The two modified traffic-inspection requirements match the synchronized delta; user and technical guides describe the implemented behavior.

## Workflow follow-up

- Synchronize the traffic-inspection delta, run `openspec validate --specs --strict --no-interactive`, and archive the completed change.
