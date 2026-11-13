# Implementation and verification

Date: 2026-11-13. Go 1.27.3, Windows amd64, `CGO_ENABLED=0`, pinned MyGo v0.8.3 / nuxt-ui-go v0.0.2. The original frontend repository was read only.

## Delivered behavior

- F1: removed the legacy left-click selection override. MyGo owns single, additive, range, drag and keyboard selection. Sorting/filtering/history changes retain the existing identity and export contracts. Enter, unmodified native double-click (explicitly confirmed by the user), and Show share detail activation. Close details preserves selection and returns focus; live/history callbacks are wired separately.
- F2: settings, capture/history/category, collection, rewrite, plugin/file and memory navigation use native focus/activation. Child switches, checkboxes, file controls and menus remain independent. Hidden navigation no longer has an active keyboard entry. Business operations still use existing stores and functions.
- F3: request grid cells use ButtonBase plus an anchored Textarea. Tab/Shift+Tab follow native focus order; Enter/blur commit, Escape restores the original value, and IME confirmation stays inside editing. Stable field identity prevents writes into a replacement row. HTTP headers/query/cookies/urlencoded/form-data and WebSocket headers are covered. The editor is hidden in the same build that observes blur, fixing a residual overlay found during real-window testing.
- F4: toolbar and summary cards wrap; metric cards stack using available content width. Existing samples, monitoring state, interval and chart series visibility survive resizing. Native chart gestures remain unchanged.
- Native-first comparison rules, bilingual close-details copy, keyboard usage and the interaction compatibility note are updated. No version bump.

## Automated checks

Passed:

- Targeted package suites for capture/inspect/store/logic, settings/memstats, collection, plugins/rewrite, editor/widgets, shortcuts/shell/theme, and isolated `backend/app`.
- Final `go test ./desktop/...`, including `desktop/plugins` with real CPython 3.12.10 and no skipped plugin UI tests.
- `task vet:ui`, `task lint:go` (0 issues), `git diff --check`, `task i18n:generate`, `task i18n:check` (1381 matching messages), `task build`, and `openspec validate align-native-ui-interactions --strict`.
- Narrow memory layout tests at 558/770/820/1360 DIPs, both languages/themes, maximum interface font, 150% rendering scale, and paused/running resize cycles. Light English and dark Chinese narrow renders were inspected.
- Grid regression checks include activation without clicking, original-value restoration, native forward/reverse Tab order, hidden/removed rows, ordered/disabled fields, separate file controls, composition confirmation, bounded virtualization, and closing before the window becomes idle.

Environment setup: `python3` on the normal PATH resolves to the WindowsApps alias and fails to start a Worker. Verification used a temporary `python3.exe` launcher copied from the installed `D:/Python/Python312/python.exe`, with PATH overridden only for test processes. APPDATA was also redirected to temporary directories for tests. No global PATH or Python installation was changed.

Additional full-repository run: `go test -json ./...` passed all tested packages except the unchanged `backend/pkg/logger`. `TestOpenDirUsesInjectedOpener` reproducibly fails Windows temporary-directory cleanup because `flowlens.log` is still open. The test registers `manager.Close` before calling `t.TempDir`, so cleanup attempts directory removal first. This is outside this UI change; the same isolated test also fails. Optional Unix permission/symlink and Windows Store/Python Manager discovery checks skipped where unsupported; Python Worker integration tests ran using real CPython.

An initial `backend/app` test run without isolated APPDATA exposed existing Windows fixture isolation gaps. Its identified reset manifest and marker were moved to a temporary quarantine; subsequent app/full-suite runs used isolated profiles. No product changes were made to those fixtures.

No concurrency or backend lifecycle implementation changed, so race checks and unrelated transport/Worker protocol work were not added.

## Windows real-window acceptance

Ran the built application against a copy of the existing FlowLens data directory, with separate APPDATA. Only the copy's appearance/window settings were changed; plugin execution and history retention were disabled in the copy. Proxy capture/system proxy stayed off. Both verification launches were closed normally.

| Check | Observed result |
| --- | --- |
| History single click, Shift+Down, drag selection | Selection counts and displayed range matched; selecting did not open details. |
| Ctrl+mouse click | User manually confirmed additive/removal selection and no detail opening. The automation API cannot hold a modifier during a mouse click. Headless modifier-click tests also passed. |
| Right-click over a selected row | Five-row selection retained and batch menu shown. |
| Enter, native double-click, Close details | Details opened, closing retained selection, Down continued from the table without reopening it. |
| Settings Tab/Shift+Tab, Enter/Space | Visible focus moved between General and Proxy; switching pages retained the font-size draft. |
| Grid activation and Escape | Keyboard reopened the focused cell; typed text wrapped; Escape restored its original empty value. |
| Microsoft Chinese IME | Candidate popup appeared; Enter confirmed composition without closing the editor; Space selected a Chinese character and editing continued. |
| Grid Tab/Shift+Tab and idle | Shift+Tab committed and returned to the preceding cell. An initial stale overlay was fixed; rebuilt app retest showed no overlay after idle without extra input. |
| Memory layout, language/theme/font | Minimum-size window, widest sidebar, Chinese/English, light/dark, 24 px configured interface font at the current 144 DPI (150%) kept memory toolbar actions visible. |
| Running chart resized narrow to wide | Sampling continued, sample history remained, and the hidden HeapAlloc series stayed hidden. |

Limits: this was one Windows display at 150%, not a cross-monitor DPI migration test. macOS Cmd/native menus, Linux platform behavior, cross-display DPI, and the broader pre-existing window/drag/drop/release matrix remain unperformed. Their predecessor tasks remain unchanged and incomplete. Windows UI Automation sometimes reported an old focused text field; visible caret/focus and actual keyboard outcomes were used to confirm operation, without claiming toolkit accessibility-provider parity.

## Scope and archival

Product edits are confined to the owning desktop surfaces/widgets, two detail callbacks in `backend/app`, locales, and documentation. New tests reuse existing fixtures and a shared test-only Tab traversal helper. No database schema, persistence representation, network model/transport, ordered-header normalization, Python Worker/SDK, process attribution/cache, CA/system proxy, update protocol or shortcut catalog changed. The logger test failure is recorded rather than folded into this change.

Do not archive before `migrate-to-mygo-native-ui` → `restore-ui-parity` → `restore-ui-behavior-parity`. Main specs are currently absent; strict validation succeeds, but the CLI correctly notes that the MODIFIED deltas cannot yet be archived. Revalidate their targets after the predecessor deltas are merged. No predecessor checkbox or archive was changed.
