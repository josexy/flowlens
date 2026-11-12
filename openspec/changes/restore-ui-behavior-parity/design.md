# Design

## Context

See `proposal.md` for the differences and why they matter. The facts below shape the approach. Each was checked against mygo v0.8.3, nuxt-ui-go v0.0.2, and the 1.0.5 sources in `/home/josexy/work/flowlens/frontend/src`.

| Fact | Consequence |
| --- | --- |
| `ui.ListState` with `Selection` hard-codes the selection model. Click chooses one row, Cmd/Ctrl-click toggles one, Shift-click extends a range, and a press on an unselected row selects it on pointer-down. There is no public per-row click hook (`rowBuild.clicked` is internal). | The 1.0.5 model cannot be configured. It has to be applied over MyGo's result. |
| `Element.ObserveInput` sees pointer presses and releases on an element and its descendants, with `Mods`, before the target handles them, without taking them. `Element.HandleInput` sees only input aimed at the element itself, not at its rows. | The table can learn which button and modifiers a row press had while MyGo still runs its own selection, drag-select, keyboard, and context-menu logic. |
| `capture.Table.syncChoice` opens the detail panel when one row is chosen (`detailOpenedAt`), and `inspect.Panel.OnClose` would draw an X in the response tab strip, but no caller sets it, so the panel can be closed only by emptying the selection. In 1.0.5 the panel opened only by Ctrl/Cmd-click and Show, and closed by Ctrl/Cmd-click or an empty selection (`traffic.selectEntry(null)`). | The auto-open logic and the unused close button are removed. `TrafficStore.SelectEntry(nil)` already hides the panel. |
| `ui.MenuItem.Icon` takes a `MenuIcon` made once by `mygo.NewMenuIconSVG(*ui.SVG, template)`. `ui.MenuItem.Shortcut(mods, key)` shows an accelerator. Windows and Linux menu bars draw no icons, but their context menus do. `nui.MenuItem` has `Icon` and `Shortcut` fields. | Icons and hints need no library change. `desktop/locale` already imports the root `mygo` package. |
| `ui.Tester.Menu` returns item labels only, while `Tester.Image` renders menus a window draws itself (`Context.SetMenuPresentation`). | Menu icons and accelerators are checked by render, plus unit tests of the helpers that produce them. |
| `ui.Table` columns accept `Width`, `MinWidth`, and `MaxWidth`, and `ListState.Columns` keeps the user's widths. | The console table can enforce the 1.0.5 bounds with the stock table. |
| `widgets.WheelScroll` already maps the dominant wheel axis to sideways scrolling, for `widgets.TabStrip`. `capture.BuildFilterBar` wraps its tabs in a plain `ui.ScrollHorizontal`. | The filter tabs need one call. |
| SSE, WebSocket, and console details each build their own `nui.ModalWithOptions`, 720 px wide, with footer buttons and fixed 320 or 420 px bodies. | A shared frame removes three copies and fixes all three. |

### Surfaces found to match

The review also compared these surfaces with 1.0.5 and found no functional difference, so this change leaves them alone. Each was checked at the level of actions, enabled states, copy keys, and file names.

- Proxy toolbar, quick settings, DNS cache, host filter, and the resend dialog.
- Shortcuts catalog and handlers.
- Filter logic.
- Category sidebar and its leaf menu.
- Capture and history sidebar, HAR import (including dropped files), and restart capture.
- Overview, process, TLS, certificate, Raw, Raw TCP, and body and hex panes.
- WebSocket Client.
- HTTP editor request, settings, script, and response panes.
- Workspace tab close semantics.
- API Collection.
- Rewrite Rules and Python Plugins, including reordering.
- Settings, with its save, reset, and quit guard.
- Update, Memory Statistics, and Reset Startup.
- Shell, status bar, title bar, and Python help docking.

## Goals / Non-Goals

**Goals:**

- Restore each listed behavior with a Tester case per spec scenario, and a render check where only pixels show it.
- Keep the 1.0.5 selection model inside `capture.Table`, so `inspect`, `store`, and the context menu see only the resulting selection and focus.

**Non-Goals:**

- Changes to mygo or nuxt-ui-go.
- Restoring error toasts for clipboard writes, which cannot fail natively, or the environment-info retry, whose loader cannot fail.
- Removing native additions that do not conflict with 1.0.5: list keyboard navigation and Select All, and the API Collection Move to Root menu item. Decision 9 records them.

## Decisions

### 1. Apply the 1.0.5 click model over MyGo's selection

`capture.Table` keeps MyGo's `Selection`, `Selected`, and `DragSelect` for drag selection, keyboard movement, the right-click choice, and edge scrolling. It then rewrites the outcome of plain row clicks:

- **Snapshot.** At the end of each `Build`, the table records the selected IDs and the focused entry ID.
- **Observe.** `ObserveInput` on the table element records a primary-button `InputPointerDown` with its `Mods` and the row under the pointer. The row is the one whose cell reported `Hovered()` in the last frame; `cellBackground` already builds a fill per cell. A matching `InputPointerUp` that is not `Canceled` and was not part of a drag (`ListState.DragSelecting()` never became true) becomes a pending click.
- **Restore.** In the build that first sees a press, the table puts the snapshot back after `ui.Table` returns. MyGo makes its pointer-down choice only in that build (`pressPending` lasts one build pass), so the choice never shows, for example a range during a Shift press, and nothing is held until the release. A release may never reach the table: observers see a release only over the table, and a press the window loses the focus during ends without one. Holding the snapshot until the release would then keep reverting the keys, Select All, and the right-click choice.
- **Stale press.** A press elsewhere (`PressedOutside`) after a press whose release the table never saw ends that press, so a later release over the table clicks no row.
- **Rewrite.** In the next `Build`, after `ui.Table` returns, the table restores the snapshot and applies the 1.0.5 rule for the pending click. It sets `Selection`, `Selected` (so the keys move from the clicked row), the focused entry, and `ShowDetail`:
  - **Plain click.** If the snapshot is exactly that row, clear the selection and the focus. Otherwise select that row alone and focus it.
  - **Shift-click.** Toggle the row. An added row is focused. Removing the focused row focuses the first remaining selected row in table order. Removing the last row clears the focus.
  - **Primary-click** (`ui.Cmd`: Command on macOS, Control elsewhere). If the row is not selected, select it alone. If it is the focused row, toggle `ShowDetail`. Otherwise focus it and open the panel.
- **Drag.** When a drag that started with any modifier ends, the selection becomes the range from the pressed row to `Selected`, as `replaceSelectionWithRange` did. The endpoint is focused and `ShowDetail` is left alone. When the press and a move past the drag threshold reach the app before the next frame, the build that sees the press also starts the drag, and `Selected` is already the drag's end: the pressed row is then the hovered one, not `Selected`.
- **Sessions.** When `HistoryKey` changes, the table clears the selection, the focus, and any gesture, as 1.0.5's `dataContextKey` watcher did. Sessions number their entries alike, so the IDs chosen in one would otherwise choose rows of the next.
- **Removed rows.** When the focused row leaves the table, the first row still selected in table order is focused, as 1.0.5's `remainingEntries[0]`.

`syncChoice` loses `detailOpenedAt`, `dragAfterPress`, and the auto-open. It still focuses `Selected` after keyboard moves and drags, and clears the focus when the selection empties. A keyboard focus change never changes `ShowDetail`.

Alternatives considered:

- A click-policy hook in MyGo. This is out of scope: FlowLens stays on v0.8.3.
- Turning off `Selection` and implementing selection in FlowLens with clickable cell overlays. Overlays take the row's hit-testing (as the API Collection tooltip anchor did), losing drag-select, keyboard selection, and the right-click choice.
- `HandleInput` on the table. It does not receive presses aimed at rows.

### 2. The detail panel follows the selection and the click rules

`inspect.Panel` drops the never-set `OnClose` field, its button, and the native-only `detailClosePanel` locale key. The row menu's Show item keeps calling `SelectEntry` and `SetShowDetail(true)`. Store behavior is unchanged: `SelectEntry(nil)` already hides the panel. This also makes the click on the only selected row close it.

### 3. Filter tabs use `widgets.WheelScroll`

`BuildFilterBar` wraps its `ui.ScrollHorizontal` with `widgets.WheelScroll`, which keeps its own `ScrollState` and gives up the event when the strip does not overflow, as the interaction-conventions spec requires.

### 4. One detail dialog frame

`widgets.DetailDialog` builds a `nui.ModalWithOptions` with:

- A width of `min(960, 0.92 × window width)`, from `c.Size()`.
- A header row: a caller slot on the left and icon actions on the right.
- A bordered, elevated content box `min(0.7 × window height, 720)` tall.

Actions are described as `{Icon, Label, Pressed, Disabled, Run}` and drawn as ghost icon buttons with tooltips, the pressed one in the primary color.

`widgets.DetailWrap` keeps one wrap flag per view and resets it when the dialog opens. 1.0.5's SSE detail reset `wordWrap` on open; its console and WebSocket dialogs kept the flag of their still-mounted component between openings. The spec resets all three, so a dialog never opens wrapped.

The three callers fill the slots:

- **SSE.** `#n` and event-type badges, `ID: …` text, the Data, Formatted, and Raw Event tabs above the content, and a `CodeView` set to `json` for Formatted.
- **WebSocket.** Direction and type badges and the size. Text uses the `CodeView`; binary uses the hex view.
- **Console.** The time, a stream and level pill, and the source. The message uses a read-only `CodeView`.

Save names move into these callers. `HexView` gains a `SelectionFilename` field, defaulting to `hex-selection.bin`, which the WebSocket dialog sets to `websocket-selection.bin`, and an `Exporting` flag the dialog shares with its Save, as 1.0.5's dialog passed its `exporting` to `HexDumpViewer`: neither saves while the other does.

Alternative considered: patching each modal in place. That keeps three diverging copies of the sizing and action rules.

### 5. The console becomes a `ui.Table`

`editor.consoleView` replaces its `ui.List` with a `ui.Table`:

- The columns are Time, Type, Source, and Output, with the 1.0.5 default, minimum, and maximum widths. Output is the flexible column, so it fills the remaining width.
- `ListState.Columns` lives in the view, so widths last while the tab is open and are not saved.
- `FollowEnd` keeps following new output, as the 1.0.5 80 px near-bottom check did.
- Rows are 40 px.
- The Type cell draws the `Terminal` or `TriangleAlert` icon and a `nui.Badge` in the tone from `logic.PythonLogTone`.
- The header shows a dot, which pulses through a looping opacity animation while running, and the status text.
- The list's wrap button goes away; wrap belongs to the detail dialog (decision 4).

Copy All and Save All use a new `logic.PythonConsoleText(entries, sourceLabel)`. It writes `[time] [source] [stream] message` per line, with the 1.0.5 stream fallback (`stream || level || "log"`). The file names become `flowlens-python-console.log` and `flowlens-python-console-entry.log`. The detail closes when its entry leaves `ScriptConsoleEntries`, which covers Clear and a new send.

### 6. Menu icons and shortcut hints

`widgets/menu_icons.go` makes each `*mygo.MenuIcon` once, lazily, behind a `sync.Once`:

- Template icons from `lucide.FileDown`, `Tag`, `Trash`, and `ListChecks`.
- Non-template swatches: a 12 px circle `ui.SVG` per highlight color, filled with the 1.0.5 hex and outlined at 20% black.

A constructor error leaves that item without an icon. The capture, history, row, and column menus attach them. The Settings Root CA Add dropdown is a `nui.DropdownMenu`, so it sets `nui.MenuItem.Icon` to `lucide.FolderOpen` and `lucide.Pencil` directly.

`shortcuts.MenuShortcut(commandID, config) (ui.Modifiers, ui.Key, bool)` resolves a command's binding into the platform modifiers and key, reusing `uiKey` and `uiModifiers`. A missing or disabled binding returns false. The workspace tab menu calls `MenuItem.Shortcut` for `app.save` and `workspace.closeTab`. The New Tab `nui.DropdownMenu` sets `MenuItem.Shortcut` from the same display strings its tooltips use.

### 7. Small items

- `capture.Table` gains `HistoryAlias func() string`, wired by `Workspace` from the open history's metadata, and `exportSelection` uses `<alias>-selection` when it is set.
- The body type options are reordered.
- The empty state of `inspect.MessageStream`, shared by the detail panel and the WebSocket Client, uses `WorkspaceWebsocketClientNoMessages`.

### 8. Rows that open as buttons

1.0.5's console and SSE rows were buttons: a click or Enter opened one, and none stayed selected. MyGo's table reports no click on a row, and choosing the row already chosen changes nothing. `widgets.RowActivator` gives the table a `Selection` it empties after each build and a `Selected` cursor for the keys. A click therefore always chooses anew, a release seen through `ObserveInput` tells it from the keys, and Enter comes as the table's `Submitted`. Rows never show as chosen, the keys' row shows the focus ring, and MyGo's overlays give the focus back to the table as a detail closes.

The SSE list becomes a `ui.Table` with 1.0.5's ID, Event type, and Data preview columns, fixed, in 44 px rows. Its incomplete-event note moves under the list and shows once parsing ends, as 1.0.5's did; this also keeps the main thread from reading the parser while a background job appends to it. The WebSocket list and conversation take 1.0.5's direction colors from `theme.WebSocketPalette`, with the Send or Receive tag, and the list drops the native size column. The detail panel's WebSocket tab shows the stream, with its toolbar and "No messages", when no body view is loaded.

### 9. Deliberate differences, recorded

`docs/technical/ui-parity.md` gains rows for:

- Traffic table arrow keys, Home/End, and Select All. 1.0.5 had no table keys.
- The API Collection Move to Root menu item. It gives a non-drag path to the drop zone's action.
- Menu icons not drawn by Windows and Linux menu bars.
- Clipboard error toasts not shown.

It also gains a "Behavior review" section listing the surfaces in Context that were found to match.

## Risks / Trade-offs

- **Hover-based row lookup misses a press without a preceding move.** Example: a click right after the list scrolls under a still pointer. In that case the table falls back to `Selected` after MyGo's pointer-down choice, which is the pressed row for plain and Shift presses. A Tester case scrolls the table under a fixed pointer and clicks.
- **Restoring the snapshot could fight MyGo's drag start.** The snapshot is restored only in the build that sees the press, and a drag's result replaces MyGo's in every build of the drag. A Tester case drags with Cmd held and checks that the earlier selection is replaced; another starts the drag in the build that sees the press.
- **A press on a selected row that drifts under the drag threshold onto another row clicks nothing.** MyGo leaves such a press to the release, which lands on another row, and the table cannot tell it from a press on the scroll bar without coupling to MyGo's scroll bar geometry. 1.0.5 clicked the pressed row. `ui-parity.md` records it.
- **Testers of the unreleased native build lose click-to-open details.** The maintainer chose 1.0.5. Show and Ctrl/Cmd-click still open the panel. 1.1.0 is not released, so users coming from 1.0.x see no change and `CHANGELOG.md` needs no line. The user guide gains one sentence on opening details, since neither version documented it.
- **Platform menus differ.** Icons and accelerators are verified by render with drawn menus and by helper unit tests. Platform menus still need the desktop pass recorded in `restore-ui-parity` task 9.3.
- **`mygo.NewMenuIconSVG` rasterizes at 64 px once per icon.** Ten icons are made once, lazily, so the cost is negligible.

## Migration Plan

No data changes. The change ships with 1.1.0. Rollback is a revert. The archive order is `migrate-to-mygo-native-ui`, `restore-ui-parity`, then this change.
