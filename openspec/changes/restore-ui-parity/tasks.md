# Tasks

Each task is checked against the 1.0.5 source in `/home/josexy/work/flowlens/frontend/src`, which is read-only. View tasks are verified headlessly with `ui.NewTester`, and `task vet:ui` runs after each group that changes views. Render checks set `FLOWLENS_RENDER_DIR` and compare the PNGs, in English at 1600×860, with `/home/josexy/work/flowlens/assets/screenshots/flowlens-{light,dark}.png` and the values listed in `design.md`.

## 1. Render harness

- [ ] 1.1 Add test-only package `desktop/internal/rendertest`, with traffic fixtures resembling the reference screenshots (methods, statuses, process names, highlights) and `Shoot(t, tester, name)`, which writes `Tester.Image()` only when `FLOWLENS_RENDER_DIR` is set. Verify that `go test ./desktop/internal/rendertest` passes, that `go list -deps .` of the app binary does not include the package, and that one capture-workspace render in each theme writes two PNGs.

## 2. Shared interaction conventions

- [ ] 2.1 Add `widgets.AutoFocus` (an `InputOptions.Style` that focuses the editor) and `widgets.InlineInput` (Enter submits, Escape cancels, focused on appearance). Verify with Tester cases using `Tester.Focused` and `Tester.Key(0, ui.KeyEscape)`, then `go test ./desktop/widgets`.
- [ ] 2.2 Apply focus-on-open to the API Collection new-folder row and rename dialog, the save-to-collection dialog, the capture alias dialog, the domain filter add-row input, and the plugin create, rename, and file dialogs. Use `InlineInput` for inline folder creation. Verify a Tester case per surface that typing after opening enters text, and that Escape cancels inline creation. Run `go test ./desktop/collection ./desktop/capture ./desktop/plugins`.
- [ ] 2.3 Add `widgets.HorizontalWheel`, which maps the dominant wheel axis to horizontal scroll when the strip overflows, ignores Ctrl, and passes the event on otherwise. Apply it to the workspace tab strip and the detail request and response tab strips. Verify Tester cases with `Tester.Wheel`: a vertical wheel moves an overflowing strip; a strip that does not overflow does not consume the event. Run `go test ./desktop/widgets ./desktop/editor ./desktop/inspect`.
- [ ] 2.4 Make `widgets.Notifier` turn off toast progress and accept `WithTimeout`. Set `nui.ToastTopCenter` in the main, Settings, and Update windows. Give HAR import 5 s and 8 s results, and Restore Initial State success 5 s. Verify Tester cases: the toast text is centered horizontally near the top of each window, no progress element appears, and an 8 s toast is still visible after `Tester.Advance(7*time.Second)`. Run `go test ./desktop/widgets ./desktop/shell ./desktop/settings ./desktop/update ./desktop/capture`.
- [ ] 2.5 List every `UTooltip`/`AppTooltip` in the 1.0.5 templates with its text key. Add each missing tooltip on the matching icon-only control, including the shortcut row's clear and reset buttons and the API Collection toolbar. Record the list in `docs/technical/ui-parity.md`. Verify with a Tester case per surface that hovering shows the tooltip text, and with a script check that each listed key is referenced from a tooltip call site.

## 3. Traffic table

- [ ] 3.1 Extend `capture.tableTheme` and `ListState.HeaderCell` for the 1.0.5 header: elevated background, primary text titles, no resting dividers, accent resize edge on hover, and accent sort arrow. Verify with a render check in both themes, and with a Tester case that a hovered resize edge reports the accent color and a resting one draws no divider pixels.
- [ ] 3.2 Make method and status badges opaque, using the 1.0.5 tones, wide enough for `OPTIONS` and `CONNECT` at default column widths. Verify Tester cases: `OPTIONS` appears in full with no ellipsis; the badge pixel color on a selected dark-theme row matches the unselected badge. Add a render check.
- [ ] 3.3 Add the 1 px outline to highlighted and inspected rows (stronger in the dark theme), and tabular numerals to the duration and size cells. Verify with a render check, and with a Tester case on the inspected row's marker and outline. Run `go test ./desktop/capture ./desktop/logic`.

## 4. Traffic detail

- [ ] 4.1 Draw the request and response tab strip border across the full row, keeping the trailing response chips. Verify with a render check, and with a Tester case that the border spans the pane width when the tabs are narrower.
- [ ] 4.2 Add `widgets.FieldGrid`: 40 px one-line rows, row hover, a resizable name column kept in view state, and a read-only auto-growing overlay on click. Use it for headers, trailers, query, and request cookies. Verify Tester cases: row heights do not change for a long value; a click opens the overlay with the full value selectable; drag resizes within limits; order, duplicates, casing, and empty values are kept. Run `go test ./desktop/widgets ./desktop/inspect`.
- [ ] 4.3 Rebuild response cookies on `FieldGrid` with the 1.0.5 attributes popover: name and count, an attribute grid, "Empty value", a check mark for flags, "No attributes", and a Raw Set-Cookie section with a copy button. Verify Tester cases for flag, empty, and no-attribute cookies, and for the copied raw header.
- [ ] 4.4 Use the localized TLS and certificate labels, with list values one per line. Verify a Tester case under `zh-Hans` that no English TLS labels remain, and a case with three cipher suites on three lines.
- [ ] 4.5 Rework the image preview: fit scale at 100%, Ctrl+wheel and pinch zoom, drag to pan above 100% with the grab cursor, an info bar with scale, info, and the zoom hint, and the context menu kept; remove the migration-only zoom buttons. Verify Tester cases: a 4000×3000 fixture fits the pane; `Tester.Wheel` with Ctrl changes the scale by one step; a drag moves the offset; `Tester.Cursor` is grab while zoomed.
- [ ] 4.6 Set `NoScrollPastEnd`, and turn off whitespace rendering for read-only `CodeView`s. Verify a Tester case that scrolling to the end leaves the last line at the bottom. Run `go test ./desktop/widgets ./desktop/inspect ./desktop/editor`.

## 5. Request workspace

- [ ] 5.1 Add `widgets.EditableGrid`: 1.0.5 layout, placeholders, row hover, key-column resize, and a click-to-edit `Textarea` overlay that grows, opens focused with its text selected, and commits on Enter, Shift+Enter, or blur. Keep invalid-name icons and file-cell slots. Verify Tester cases for opening, committing, blur, Enter without a line break, the resize bounds, and 1,000 rows building only visible rows, then `go test ./desktop/widgets`.
- [ ] 5.2 Replace `editor.rowsView` and the URL-encoded and form-data row builders with `EditableGrid` in the HTTP Request Editor and the WebSocket Client. Give URL-encoded its own placeholders, actions, and tooltips. Verify that existing editor tests pass unchanged in behavior (query and URL sync, cookie and header sync, dirty state), and add cases for the URL-encoded copy. Run `go test ./desktop/editor ./desktop/store ./desktop/logic`.
- [ ] 5.3 Scroll the active workspace tab into view whenever the active key changes. Verify Tester cases: opening a collection request with 20 tabs open, and switching by shortcut to a hidden tab, both bring the tab fully into view.
- [ ] 5.4 Rework tab dragging with `Element.Draggable` along the horizontal axis: neighbors slide to open the slot, the strip auto-scrolls at its edges, Capture stays first, and a press that does not move still activates the tab. Verify Tester cases for reordering past hidden tabs, Capture staying fixed, and click-without-drag. Run `go test ./desktop/editor ./desktop/store`.

## 6. API Collections and Categories

- [ ] 6.1 Restyle collection rows: 32 px rounded rows with hover, 20 px icons, child-count pills, an end-aligned rotating chevron, a 14 px semibold method label, and a name tooltip. Verify with a render check, and with Tester cases for the count pill and the tooltip on a truncated name.
- [ ] 6.2 Restrict dragging to a hover grip handle, dim the source row to half opacity during a drag, and keep the folder ring and Move to Root zone. Verify Tester cases: a press on the name opens without dragging; a grip drag dims the source and moves the item; dropping on the root zone moves a nested folder. Run `go test ./desktop/collection ./desktop/store`.
- [ ] 6.3 Restyle the category tree: tinted section bands with borders and end-aligned chevrons, accent check marks for selected host and process filters, 20 px icons, rounded hover rows, and no counts. Verify with a render check, and with Tester cases that selecting two hosts shows two check marks and filters to either host. Run `go test ./desktop/capture ./desktop/logic`.

## 7. Shell and Settings

- [ ] 7.1 Show the shutdown indicator as a non-dismissible card with the title, `AppShutdownDescription`, and a spinner, blocking workbench input. Verify Tester cases that the description is shown, that clicks on the workbench do nothing, and that `ShutdownUIReady` is still called once. Run `go test ./desktop/shell ./backend/app`.
- [ ] 7.2 Pass picker messages and the Private Key Files filter name from Settings to `mygo.OpenDialogOptions`. Verify with a fake `PickFile` that records the title, message, and filter for each certificate and interpreter picker. Run `go test ./desktop/settings ./backend/app`.

## 8. Remove the Pointer Cursor setting

- [ ] 8.1 Remove the General settings row and the Restore Initial State reset of `PointerCursor`, and remove `CommonConfig.PointerCursor` with its default and round-trip tests. Point the `desktop/settings` tests that toggle `PointerCursor` to dirty the draft at another common boolean. Verify that `grep -rn PointerCursor backend desktop` finds nothing; that a Tester case shows General settings without a pointer cursor row; and that a `setting_service` test storing a common payload with `"pointerCursor":true` loads, keeps its other values, and saves without the key. Run `go test ./backend/services/setting_service ./desktop/settings ./desktop/store`.
- [ ] 8.2 Remove `settingsPointerCursorLabel` and `settingsPointerCursorHint` from `locales/en.json` and `locales/zh-Hans.json`, run `task i18n:generate`, and add a line to the `CHANGELOG.md` 1.1.0 section saying the pointer cursor setting was removed. Verify that `task i18n:check` passes, that the generated catalog has no pointer cursor accessors, and that the changelog line sits under `## 1.1.0`.

## 9. Integration checks

- [ ] 9.1 Run `task verify`, `task vet:ui`, and `CGO_ENABLED=1 go test -race ./desktop/...`. Record the results in this file.
- [ ] 9.2 Render every surface in both themes with `FLOWLENS_RENDER_DIR`. Review the PNGs against the reference screenshots and the `design.md` values, and record the remaining differences (expected: only the listed exceptions and font rendering) in `docs/technical/ui-parity.md`.
- [ ] 9.3 On a Linux desktop session, compare FlowLens with 1.0.5 side by side for focus, wheel scrolling of strips, grid editing with Chinese IME input, drag feedback, and notifications. Record results. Windows and macOS checks join migration tasks 17.4 and 17.5.

## Workflow follow-up

- Archive `migrate-to-mygo-native-ui` before archiving this change. Its capability specs must exist for the added requirements to merge, and for the `application-settings` General settings modification, which `openspec` refuses to archive without a base spec.
- Archive this change after implementation and review are complete.
