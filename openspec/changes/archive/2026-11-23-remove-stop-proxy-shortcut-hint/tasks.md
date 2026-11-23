# Tasks

## 1. Stop Proxy menu presentation

- [x] 1.1 Remove the Stop Proxy shortcut hint, adapt the existing menu render test, and update the user guide; verify with `CGO_ENABLED=0 go test ./internal/desktop/capture` and inspect English/Chinese light/dark menu renders produced with `FLOWLENS_RENDER_DIR` set.

## 2. Integration verification

- [x] 2.1 Run `task vet:ui`, `CGO_ENABLED=0 go test ./...`, and `openspec validate remove-stop-proxy-shortcut-hint --strict --no-interactive`; compare the final implementation, specification, and acceptance evidence, recording platform and remaining limitations.

## Acceptance evidence

- Windows, `CGO_ENABLED=0`: `go test ./internal/desktop/capture -count=1`, `go test ./...`, and `task vet:ui` passed.
- `openspec validate remove-stop-proxy-shortcut-hint --strict --no-interactive`, `git diff --check`, and formatting checks passed.
- After synchronization, `openspec validate --specs --strict --no-interactive` passed all 17 specifications; the updated requirement matches the delta and preserves the main spec's title, purpose, and unrelated requirements.
- Inspected all eight Stop Proxy menu renders: English/Chinese, light/dark, font sizes 16/24. The power icon and label fit, and Ctrl+Shift+Alt+F12 keycaps are absent despite the configured binding.
- Reviewed the final implementation against the delta and user guide: only the menu hint was removed; stop dispatch, busy guards, focus restoration, and shortcut settings are unchanged. Existing interaction tests passed.
- Verification used the headless MyGo renderer on Windows. Native macOS/Linux sessions were not exercised; the change has no platform-specific branch.

## Workflow follow-up

- Synchronize the keyboard-shortcuts delta and archive the completed change.
- Run `openspec validate --specs --strict --no-interactive` after synchronization.
