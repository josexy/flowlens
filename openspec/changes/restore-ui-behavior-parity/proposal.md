# Proposal

## Why

`restore-ui-parity` brought the native interface's styling and most interaction in line with FlowLens 1.0.5. A second side-by-side review of `/home/josexy/work/flowlens/frontend/src` against `desktop/` looked for behavior rather than appearance: what a click, a modifier key, a menu, a dialog, or a saved file does. Most surfaces now match. The differences that remain change how users work: the traffic table's clicks follow a different model, and three detail dialogs and the Python console lost controls that 1.0.5 had. 1.0.5 stays the reference, so this change restores it.

## What Changes

The review went through every context menu, toolbar, pane, and dialog. It compared the actions, modifier handling, enabled states, default file names, and copied text of each. It also checked which locale keys each 1.0.5 component used that the Go view does not. Surfaces that already match are listed in `design.md`. The differences found:

- **Traffic table clicks.** In 1.0.5, a click only selects the row and moves the focus. The detail panel stays as it was. Ctrl-click (Cmd-click on macOS) opens or closes the panel for that row. Shift-click adds or removes one row. Clicking the only selected row clears the selection and closes the panel. The native table instead opens the panel on any click, uses Ctrl/Cmd-click to add or remove rows, and uses Shift-click to select a range. Once the panel is open, the native build can close it only by emptying the selection: `inspect.Panel` has a close button, but nothing wires it up. All of this is restored to 1.0.5, and the unused close button is deleted. Arrow-key and Cmd/Ctrl+A selection from the native list stay, since 1.0.5 had no table keys and they do not conflict.
- **Filter type tabs.** A vertical wheel does not scroll the overflowing All…Binary and 1xx…5xx tab strip. 1.0.5 scrolled it, as it did the other tab strips that `restore-ui-parity` already fixed.
- **SSE and WebSocket message detail dialogs.**
  - Both are narrower (720 px instead of up to 960 px) and shorter (320 px instead of up to 720 px).
  - They have no word-wrap toggle.
  - They put Copy and Save as text buttons in the footer, not as icon buttons with tooltips in the header.
  - The SSE dialog lost its sequence and event-type badges, and its formatted view shows no JSON highlighting.
  - The WebSocket dialog lost its direction and Text/Binary badges.
  - An empty message stream says "No content" instead of "No messages".
  - Saved files use different default names: `sse-message-<n>.txt` instead of `sse-message.txt`, `sse-message.json` or `sse-event.txt`, and `hex-selection.bin` instead of `websocket-selection.bin`.
- **Python request console.**
  - The list has lost its Time, Type, Source, and Output header and its resizable columns.
  - Rows are 28 px instead of 40 px.
  - The stream icon and the level pill are gone.
  - The running indicator dot is gone.
  - Word wrap moved from the entry dialog to the list.
  - The entry dialog lost its 1.0.5 header and size.
  - Copy all and Save all write a different text layout and default file names.
  - Clear no longer closes an open entry dialog.
- **Menus.**
  - 1.0.5 highlight items carried color swatches.
  - Capture and history menus had Export, Set Alias, and Delete icons, the column menu had a Show All Columns icon, and the Settings Root CA Add menu had Select Files and Add Path Manually icons.
  - The tab context menu and the New Tab menu showed the bound shortcuts for Save API, Close, New HTTP, and New WebSocket.
  - The native menus show none of these.
- **Small differences.**
  - Exporting selected rows from a saved history with an alias suggests `<alias>-selection` in 1.0.5. The native build always suggests `flowlens-selection-<n>`.
  - The request body type menu lists None, JSON, Text, XML, File, Form Data, URL-encoded in 1.0.5. The native order differs.

Two native additions are kept and recorded as deliberate differences: the arrow-key and Select All behavior of the traffic table, and the API Collection folder menu's Move to Root item. Error toasts for clipboard writes that cannot fail natively are not restored.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

These capabilities are defined by the in-flight `migrate-to-mygo-native-ui` and `restore-ui-parity` changes and are not yet in `openspec/specs/`.

- `traffic-capture`: Row selection follows the 1.0.5 click and modifier model. Selection export suggests the history alias.
- `traffic-inspection`: The detail panel opens and closes by Ctrl/Cmd-click or the row menu's Show item, not by selection, and has no close button. SSE and WebSocket message details regain their 1.0.5 content and file names, and the empty message stream regains its text.
- `request-workspace`: The Python console regains its column table, entry detail, copy/save format, and file names. The body type menu returns to the 1.0.5 order.
- `interaction-conventions`: The filter type tabs join the strips a vertical wheel scrolls. Message detail dialogs share the 1.0.5 frame. Menus show the 1.0.5 icons, color swatches, and shortcut hints.

## Impact

- **Code**: `desktop/capture` (table selection, filter bar, menus), `desktop/inspect` (panel, SSE, messages, hex export name), `desktop/editor` (console, body type options, tab menus), `desktop/settings` (Root CA Add menu icons), `desktop/widgets` (shared detail dialog frame and menu icons), `desktop/shortcuts` (an exported helper that gives a command's resolved binding as a menu shortcut), and `docs/technical/ui-parity.md`. Copy uses existing locale keys, including `workspace.http_request.console_column_*` and `workspace.websocket_client.no_messages`. No new keys are expected. The native-only `detailClosePanel` key is removed from both catalogs.
- **Dependencies**: None. MyGo stays at v0.8.3 and nuxt-ui-go at v0.0.2. Menu icons use `mygo.NewMenuIconSVG`, which `desktop/locale` already depends on through the `mygo` package.
- **Persistence**: None. Console column widths and dialog wrap states stay in view state, as in 1.0.5.
- **Ordering**: Archive `migrate-to-mygo-native-ui`, then `restore-ui-parity`, then this change. The deltas modify requirements that those changes add.
- **Verification limits**: Behavior is verified with `ui.NewTester`. Native menus draw icons differently on each platform (Windows and Linux menu bars show none), so menu icons and shortcut hints are checked through the menu model in tests and still need a desktop pass.
