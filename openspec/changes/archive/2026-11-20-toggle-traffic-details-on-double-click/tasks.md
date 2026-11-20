# Tasks

## 1. Traffic detail interaction

- [x] 1.1 Implement native double-click toggling in the shared table and add input regression coverage for live/history, light/dark, open/hide/reopen, ordinary/modifier selection, and repeated Enter; verify with `CGO_ENABLED=0 go test ./internal/desktop/capture`.
- [x] 1.2 Update the user guide and compare its double-click, Ctrl/Cmd selection, Enter, and close instructions with the implementation; confirm no locale keys need changes.

## 2. Integration verification

- [x] 2.1 Run `task vet:ui` and `CGO_ENABLED=0 go test ./...`; record results, host platform, and native platform limitations.
- [x] 2.2 Run `openspec validate toggle-traffic-details-on-double-click --strict --no-interactive` and compare implementation, specs, and acceptance evidence.

## Acceptance evidence

- Windows host, `CGO_ENABLED=0`: `go test ./...` passed (capture suite 29.068s; other packages passed from cache).
- Final regression check with real detail panels: `go test ./internal/desktop/capture -run 'TestNativeDoubleClickTogglesDetails|TestClickingTheOnlySelectedRowKeepsIt' -v -timeout 60s` passed, including live/history and light/dark subtests.
- The existing single-click test now advances the tester clock before clicking, so it does not accidentally submit a double-click. The new test restores the native cursor after deselecting a row before testing Enter.
- `task vet:ui`, strict change validation, strict validation of all 17 main specs, and `git diff --check` passed.
- User-guide instructions and the inspection requirement agree with the native submission callback; no localized strings changed. Existing store close/open paths preserve body cleanup and selection semantics.
- Windows native desktop acceptance completed on 2026-11-20 with `CGO_ENABLED=0 MYGO_LDFLAGS=-macos=13.0 go tool mygo build -o .mygo/local-validation -skip-installer -skip-dmg`. The built executable used an isolated APPDATA profile and three loopback HTTP requests through its proxy; the system proxy remained disabled.
- Actual pointer/keyboard checks passed: single-click selects without opening, double-click opens/hides/reopens, method badges and cell padding accept double-clicks, an open panel follows a single-click on another row, and Enter opens a closed panel while repeated Enter keeps it open. Both light and dark appearances were exercised.
- Fresh targeted native-input tests passed with `-count=1`, covering Ctrl additive selection, last-row deselection, live/history toggling in both themes, keyboard focus isolation, and late-body rejection. Ctrl-click coverage used MyGo Tester; the desktop automation API does not expose modifier-click injection.
- The temporary proxy, HTTP fixture, and application were stopped after testing. macOS/Linux native acceptance remains untested. No layout, persistence, service, or protocol changes were made.
- Before PR submission, `task lint:go` passed with zero issues; `task i18n:check`, changed-file formatting, `git diff --check`, and strict validation of all 17 main specs also passed. The archived delta matches the synchronized inspection requirement.

## Workflow follow-up

- Synchronized the traffic-inspection delta, passed `openspec validate --specs --strict --no-interactive`, and archived the completed change on 2026-11-20.
