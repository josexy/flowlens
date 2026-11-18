# Tasks

Each task is checked against the 1.0.5 source in `/home/josexy/work/flowlens/frontend/src`, which is read-only. View tasks are verified headlessly with `ui.NewTester`. `task vet:ui` runs after each group that changes views. Render checks set `FLOWLENS_RENDER_DIR` and use `desktop/internal/rendertest`.

## 1. Traffic table selection and the detail panel

- [x] 1.1 Apply the 1.0.5 click model in `capture.Table` (design decision 1):
  - Snapshot the selection and focus at the end of `Build`.
  - Record primary-button presses through `ObserveInput`, using the hovered row and the `Mods`.
  - Hold the snapshot while a press is down without a drag.
  - Rewrite plain, Shift, and `ui.Cmd` clicks after `ui.Table` returns.
  - Make a finished drag select only the pressed-to-`Selected` range.
  - Remove `detailOpenedAt`, `dragAfterPress`, and the auto-open from `syncChoice`.

  Replace `TestSingleSelectionOpensDetail` and `TestModifierSelection` with one Tester case per traffic-capture "Row selection" scenario, and keep `TestEvictedSelectionIsRemoved` passing. Add two more cases:
  - A Shift press: the frame between `Press` and `Release` still shows the earlier selection.
  - A click after the table scrolls under a still pointer: it selects the row under the pointer.

  Update `TestDragSelectsRangeOfRows` and `TestDragNearTheEdgeScrollsAndExtends` for the replace rule. Run `go test ./desktop/capture ./desktop/store`.
- [x] 1.2 Make primary-click open, close, or move the detail panel (decision 1). Delete `inspect.Panel.OnClose`, its button, and the `detailClosePanel` key from both catalogs, then run `task i18n:generate`. Verify Tester cases for the traffic-inspection scenarios "Opening with Ctrl-click", "Closing with Ctrl-click", "Ctrl-click on another row", "Show item", and "No close button". For "Show item", extend `TestRowMenuShowDetails` to check that the item is unavailable with two rows selected. Run `go test ./desktop/capture ./desktop/inspect`, `task i18n:check`, and `task vet:ui`.
- [x] 1.3 Update the docs:
  - Add one sentence to the capture part of `docs/user-guide.md`: a click selects a row, Ctrl-click (Cmd-click on macOS) or Show opens or closes its details, and Shift-click adds or removes a row.
  - In `docs/technical/ui-parity.md`, add a deliberate-difference row for table arrow keys, Home/End, and Select All.
  - Add a "Behavior review" section to the same file listing the surfaces design.md found to match.

  Verify that `go test ./desktop/internal/parity` passes and that the user-guide sentence names both modifiers.

## 2. Filter type tabs

- [x] 2.1 Wrap the filter bar's `ui.ScrollHorizontal` with `widgets.WheelScroll` (decision 3). Verify Tester cases for the interaction-conventions scenarios:
  - At a window width where the tabs overflow, `Tester.Wheel` with a vertical delta over the tabs scrolls them and leaves the table's first visible row unchanged.
  - At a width where they fit, the same wheel scrolls the table.

  Run `go test ./desktop/capture ./desktop/widgets`.

  Note: the filter bar is above the traffic table, not inside it, so a wheel the fitting strip leaves goes to the bar's ancestors, none of which scroll; it cannot scroll the table, as in 1.0.5. `TestWheelLeavesFittingFilterTabs` checks that neither the strip nor the table moves, and `widgets.TestWheelPassesOnWhenStripDoesNotOverflow` checks the hand-off to surrounding content.

## 3. Message detail dialogs

- [x] 3.1 Add `widgets.DetailDialog` and `widgets.DetailWrap` (decision 4): window-relative width and content height, a header slot, icon actions with tooltips, a pressed state, a disabled Save, and wrap reset on open. Verify Tester cases:
  - At 1600×1000 the dialog is 960 px wide with a 700 px content area.
  - At 900×600 it is at most 828 px wide with a 420 px content area.
  - Hovering each action shows its tooltip.
  - Wrap shows as pressed while on, and is off again after close and reopen.
  - Save is unavailable while a save is in progress.

  Run `go test ./desktop/widgets`.
- [x] 3.2 Move the SSE message detail onto `DetailDialog`:
  - `#n` and event-type badges, and `ID: …` or `ID: —`.
  - The Data, Formatted, and Raw Event tabs, with the `json` language on Formatted.
  - The 1.0.5 file names.

  Verify Tester cases for the "SSE message detail content" and "SSE message file names" scenarios, recording suggested names with a fake `PickSavePath` for each tab. Run `go test ./desktop/inspect`.
- [x] 3.3 Move the WebSocket message detail onto `DetailDialog`:
  - Direction badge (Send in warning, Receive in info), a Text or Binary File badge, and the size.
  - Only Save for binary messages.
  - Add `HexView.SelectionFilename`, set to `websocket-selection.bin` in this dialog.

  Switch the `inspect.MessageStream` empty state to `WorkspaceWebsocketClientNoMessages`. Verify:
  - Tester cases for the "WebSocket message detail content", "WebSocket message file names", and "Empty WebSocket message stream" scenarios, in both the detail panel and the WebSocket Client.
  - A light and dark render of a received text message and a sent binary message.

  Run `go test ./desktop/inspect ./desktop/editor` and `task vet:ui`.

## 4. Python console

- [x] 4.1 Add `logic.PythonConsoleText(entries, sourceLabel)`, which writes `[time] [source] [stream] message` per line with the `stream || level || "log"` fallback. Verify table tests for stdout, stderr, a level-only entry, an empty stream and level, and multi-line messages, then run `go test ./desktop/logic`.
- [x] 4.2 Rebuild the console list as a `ui.Table` (decision 5):
  - The Time, Type, Source, and Output columns with the 1.0.5 default and bounded widths, Output flexible, and widths kept in the view.
  - 40 px rows, with the stream icon and the toned level pill in the Type cell, and the line count.
  - The status dot and text.
  - Remove the list wrap button.

  Verify Tester cases for the "Python console columns", "Python console rows", and "Python console status" scenarios, including dragging the Source edge to 600 px, which stops at 420. Run `go test ./desktop/editor`.
- [x] 4.3 Move the entry detail onto `DetailDialog`: time, the stream and level pill, and the source, with Wrap, Copy, and Save. Use `PythonConsoleText` and the 1.0.5 file names for Copy All and Save All. Close the detail when its entry leaves the console. Verify Tester cases for the "Python console entry detail", "Python console copy and save format", and "Clearing the console closes its detail" scenarios, checking `Tester.Clipboard` and fake `PickSavePath` names. Run `go test ./desktop/editor ./desktop/logic` and `task vet:ui`.

## 5. Menus

- [x] 5.1 Add `widgets/menu_icons.go` (decision 6): lazily made template icons for FileDown, Tag, Trash, and ListChecks, and one swatch per highlight color. Attach them to:
  - The Highlight submenu.
  - The capture menu (Export, Set Alias).
  - The history menu (Export, Delete).
  - The column menu (Show All Columns).

  Set `lucide.FolderOpen` and `lucide.Pencil` on the Settings Root CA Add dropdown. Verify:
  - A unit test that every icon is made and each swatch uses its 1.0.5 color.
  - `Tester.Menu` labels unchanged for each menu.
  - Light and dark renders of the Highlight submenu and the history menu in a window with drawn menus (`SetMenuPresentation`).
  - A Tester render of the Root CA Add dropdown.

  Run `go test ./desktop/widgets ./desktop/capture ./desktop/settings`.
- [x] 5.2 Add `shortcuts.MenuShortcut(commandID, config)`. Show it on the workspace tab menu's Save API and Close items, and as `nui.MenuItem.Shortcut` on New HTTP and New WebSocket. Verify:
  - Unit tests for a default binding on Windows (Ctrl+S), on macOS (Command+S), an override, and `binding: null`, which returns no shortcut.
  - A drawn-menu render of the tab menu.
  - A Tester case that the open New Tab dropdown shows the New HTTP binding text.
  - A Tester case that disabling Close Tab removes its hint.

  Run `go test ./desktop/shortcuts ./desktop/editor`.
- [x] 5.3 Add deliberate-difference rows to `docs/technical/ui-parity.md`: no icons on Windows and Linux menu-bar menus, the API Collection Move to Root menu item, and no clipboard error toasts. Verify the rows are present and `go test ./desktop/internal/parity` passes, then run `task vet:ui`.

## 6. Export name and body type order

- [x] 6.1 Add `Table.HistoryAlias`, wire it from the open history's metadata in `capture.Workspace`, and use `<alias>-selection` in `exportSelection` (decision 7). Extend `TestExportSelection` with the "Selection export file name" scenarios, checking that the fake `PickSavePath` receives `login-flow-selection.har` and `flowlens-selection-3-csv.csv`. Run `go test ./desktop/capture`.
- [x] 6.2 Reorder the body type options to None, JSON, Text, XML, File, Form Data, and URL-encoded. Verify a Tester case that opens the body type select and reads the options in that order. Run `go test ./desktop/editor` and `task vet:ui`.

## 7. Integration checks

- [x] 7.1 Run `task verify`, `task vet:ui`, and `CGO_ENABLED=1 go test -race ./desktop/...`. Record the results in this file.

  Results, on Linux arm64, 2026-11-11:
  - `task verify`: passed. `fmt:check`, `lint:go` (0 issues), `vet:ui`, `i18n:check` (1380 messages in en and zh-Hans), and `go test ./...`.
  - `task vet:ui`: passed.
  - `CGO_ENABLED=1 go test -race ./desktop/...`: passed, with no races reported.
  - After those runs, the SSE detail tabs moved to `widgets.PillTabs` and two renders were added. `fmt:check`, `lint:go`, `vet:ui`, and the `desktop/capture`, `desktop/inspect`, and `desktop/widgets` tests were run again and passed.
- [x] 7.2 Render the capture table with a detail panel, all three detail dialogs, the console, and the decorated menus in both themes with `FLOWLENS_RENDER_DIR`. Compare them with the 1.0.5 templates and the values in design.md, and record any remaining differences in `docs/technical/ui-parity.md`.
- [ ] 7.3 On a Linux desktop session, compare FlowLens with 1.0.5 side by side for row clicks with each modifier, drag selection, platform menu icons and shortcut hints, and the three detail dialogs. Record the results. Windows and macOS checks join migration tasks 17.4 and 17.5.

## 8. Review fixes

A review of the implementation found the gaps below. Each fix has a Tester case that fails without it.

- [x] 8.1 Restore the snapshot only in the build that sees a press, not until the release (decision 1), so a release outside the table or a lost window focus no longer leaves the keys, Select All, and the right-click choice reverted. End a press whose release never came when a press lands elsewhere. Verify `TestReleaseOutsideTheTableLeavesTheKeysWorking`, `TestLosingTheWindowFocusDuringAPressLeavesTheKeysWorking`, `TestAReleaseAfterAPressElsewhereClicksNoRow`, and that `TestShiftPressShowsTheEarlierSelectionUntilRelease` still passes.
- [x] 8.2 Take the pressed row from the hovered row when the drag starts in the build that sees the press. Verify `TestDragStartingInThePressBuildAnchorsAtThePressedRow`.
- [x] 8.3 Clear the selection, the focus, and any gesture when the table shows another saved session, and focus the first row still selected in table order when the focused row leaves. Verify `TestSwitchingHistoriesClearsTheSelection` and `TestEvictionFocusesTheFirstRowLeftInTableOrder`.
- [x] 8.4 Add `widgets.RowActivator` (decision 8) and open console entries with it. Verify `TestPythonConsoleRowsOpenByKeyboard`: Enter opens the row the keys reached, a click reopens the row opened before, and a header click opens nothing.
- [x] 8.5 Rebuild the SSE list as a `ui.Table` with 1.0.5's columns, rows, and incomplete note, opened with `RowActivator`. Verify `TestSSEMessageListColumns`, `TestSSEMessagesOpenByKeyboard`, and the existing SSE cases.
- [x] 8.6 Restore 1.0.5's WebSocket list rows and conversation bubbles with `theme.WebSocketPalette`, show the stream when no body view is loaded, and share the dialog's busy flag with the hex Export. Verify `TestWebSocketMessageRows`, `TestWebSocketConversationOpensBinaryMessagesByTheirButton`, `TestWebSocketPaneWithoutABodyView`, `TestWebSocketHexExportWaitsForTheDialogsSave`, `TestWebSocketPalette`, and light and dark renders of `ws-list`, `ws-conversation`, and `sse-list`.
- [x] 8.7 Record in `docs/technical/ui-parity.md` the differences these fixes leave: one tab stop for console and SSE rows, the click that opens a WebSocket text bubble, and a press on a selected traffic row that drifts onto another row.
- [x] 8.8 Run `task verify` and `CGO_ENABLED=1 go test -race` over the changed packages, and record the results here.

  Results, on Linux arm64, 2026-11-11:
  - `task verify`: passed. `fmt:check`, `lint:go` (0 issues), `vet:ui`, `i18n:check` (1380 messages in en and zh-Hans, no new keys), and `go test ./...`.
  - `CGO_ENABLED=1 go test -race ./desktop/capture ./desktop/inspect ./desktop/editor ./desktop/widgets ./desktop/theme`: passed, with no races reported.
  - `openspec validate restore-ui-behavior-parity --strict`: valid, with the archive-order notes listed under Workflow follow-up.
  - After those runs, the SSE row label gained a bounds check; `fmt:check`, `lint:go`, `vet:ui`, and the `desktop/inspect` tests were run again and passed.

## Workflow follow-up

- Archive `migrate-to-mygo-native-ui`, then `restore-ui-parity`, before this change. This change modifies "Row selection", "Traffic detail panel", and "Wheel scrolls horizontal strips", which those changes add, and `openspec` refuses a MODIFIED delta without a base spec.
- Archive this change after implementation and review are complete.
