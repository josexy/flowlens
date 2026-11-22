# Tasks

## 1. Recording admission and lifecycle

- [x] 1.1 Implement service-owned pause/resume and restart preservation, gate HTTP/WebSocket/TCP admission without changing forwarding or rewrite behavior, document the boundary in docs/technical/traffic-metrics-and-har.md, and verify admission, in-flight completion, forwarding, reset, and concurrency regressions with `CGO_ENABLED=0 go test ./internal/services/proxy_service`.

## 2. Native split control

- [x] 2.1 Wire guarded store operations and the independent recording command; implement the compact three-state nui split button with stable width, English/Chinese copy, and user-guide instructions; run `task i18n:generate` and verify store, capture, shortcuts, and native render/interaction tests with `CGO_ENABLED=0 go test ./internal/desktop/store ./internal/desktop/capture ./internal/desktop/shortcuts` and `task vet:ui`.

## 3. Integration acceptance

- [x] 3.1 Run `task verify`, inspect light/dark and English/Chinese native renders, run `openspec validate pause-traffic-recording --strict --no-interactive`, compare implementation to every changed requirement, and record commands, platform, outcomes, and any unavailable native-platform checks here.

## Acceptance evidence

Verified on Windows amd64, 2026-11-22. Ordinary checks used CGO_ENABLED=0.

- `go test ./internal/services/proxy_service`: passed. Real HTTP/HTTPS traffic still forwards on the same downstream connection while paused; skipped bodies and records are absent; recording resumes with contiguous IDs. Rewrite request/response bodies, in-flight HTTP/SSE completion, WebSocket frame forwarding/release and connection admission, TCP admission, pause-preserving restart/reset, and concurrent admission are covered.
- `CGO_ENABLED=1 go test -race ./internal/services/proxy_service -run 'TestRecordingPause|TestStartPublishesCompleteStateAndSerializesStop' -count=1`: passed with the installed MinGW toolchain.
- `go test ./internal/desktop/capture -run 'TestRecording|TestRenderRecording' -count=1`: passed. Native tests cover all three states, stable segment geometry, keyboard open/close/stop and focus restoration, busy guards, failures, independent commands, and rejection of stale status events.
- `FLOWLENS_RENDER_DIR=E:/Codes/Golang/src/flowlens/.mygo/recording-renders go test ./internal/desktop/capture -run TestRenderRecording -count=1`: passed. Inspected English/Chinese, light/dark, three states and open menus. At a 740-DIP content width the quick settings and split control remain separated and visible. Renders are local verification outputs, not repository artifacts.
- `task i18n:generate`, `task verify`, `openspec validate pause-traffic-recording --strict --no-interactive`, and `git diff --check`: passed on the final implementation. The full verify includes formatting, Go lint (zero issues), UI lifetime vet, both catalogs, and all Go package tests.
- Requirement comparison: the pause gate changes admission only; admitted exchanges keep recording; rejected exchanges never acquire records later; pause is transient; explicit start enables recording; internal restart/reset preserves it; the split control and separate shortcut follow the approved behavior. Storage formats and networking configuration remain unchanged.

Native rendering and interaction verification used MyGo's headless tester. macOS/Linux application runtime acceptance was not performed on this Windows host.

Follow-up UI refinement, 2026-11-22: removed the three explanatory locale entries and both split-control tooltip calls at the user's request. Existing recording/start/render tests, catalog checks, UI vet, and whitespace checks passed. Button accessible names and the stop-menu shortcut remain available. This removes the split control's tooltip path; it does not repair the dependency's unrelated invalid tooltip-owner restoration during development hot reload.

Final UI refinement: English actions use Pause and Resume; control width is measured from the localized labels and current font instead of reserving 200 DIP. The extra recording/paused text beside the proxy address is removed. Existing recording/start/render tests, catalog checks, UI vet, spec validation, and whitespace checks passed after these refinements.

## Workflow follow-up

- Archive the verified change and synchronize delta specs.
- Run `openspec validate --specs --strict --no-interactive` after synchronization.
