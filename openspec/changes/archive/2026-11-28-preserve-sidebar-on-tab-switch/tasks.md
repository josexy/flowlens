# Tasks

## 1. Sidebar-preserving tab synchronization

- [x] 1.1 Add UI regression coverage for pointer/shortcut tab switching, collapsed sidebars, active/inactive history closure, and explicit navigation; fix synchronization and update the user guide. Verify with `CGO_ENABLED=0 go test ./internal/desktop/capture -run 'TestWorkspaceTabsPreserveSidebarNavigation|TestSidebarAndWorkspaceTabsStayTogether' -count=1` in light/dark themes and both UI languages.

## 2. Integration verification

- [x] 2.1 Run `CGO_ENABLED=0 go test ./...`, `task vet:ui`, and `openspec validate preserve-sidebar-on-tab-switch --strict --no-interactive`; compare implementation, specification, and acceptance evidence, recording platform and limitations.

## Workflow follow-up

- Synchronize the desktop-shell delta and archive the completed change.
- Run `openspec validate --specs --strict --no-interactive` after synchronization.

## Acceptance evidence

Verified on Windows on 2026-11-28:

- Before the fix, the new Categories regression failed because switching to the live capture tab changed `ActiveSection` to Capture.
- After the fix, `CGO_ENABLED=0 go test ./internal/desktop/capture -run 'TestWorkspaceTabsPreserveSidebarNavigation|TestSidebarAndWorkspaceTabsStayTogether' -count=1` passed. The real shell/tab strip tests cover English and Simplified Chinese, light/dark themes, Capture/Categories/API Collections, pointer and Ctrl+Tab/Ctrl+Shift+Tab activation, expanded/collapsed sidebars, source labels, history cleanup, and explicit Capture navigation.
- `CGO_ENABLED=0 go test ./...` and `task vet:ui` passed.
- `openspec validate preserve-sidebar-on-tab-switch --strict --no-interactive` passed; `openspec validate --specs --strict --no-interactive` passed all 18 capabilities after synchronization.
- `git diff --check` passed. The implementation updates the traffic selection without invoking sidebar navigation, matching the delta and user guide.

Validation uses the headless MyGo tester; no manual native-window check or macOS/Linux runtime check was performed. No platform-specific APIs or visual layout changed.
