# Design

## Context

See `proposal.md` for the differences and why they matter. The facts below shape the approach. Each was checked against mygo v0.8.3, nuxt-ui-go v0.0.2, and the 1.0.5 sources in `/home/josexy/work/flowlens/frontend/src`.

| Fact | Consequence |
| --- | --- |
| mygo resolves the cursor from the innermost element that sets one, stopping at the innermost clickable (`ui/runtime.go` `updateCursor`). A cursor set on a container never reaches the controls inside it. nuxt-ui-go controls set no cursor, and `nui.Theme` has no cursor field. | The Pointer Cursor setting needs a nuxt-ui-go change for library controls, plus a FlowLens helper for its own `ButtonBase` and clickable rows. |
| mygo scrolls a horizontal-only container with the wheel only when Shift is held (`ui/input.go` `scroll`), but `Element.HandleInput` sees `InputScroll` before scroll containers do. | Restoring wheel scrolling of tab strips is a FlowLens helper. No library change is needed. |
| `Element.AutoFocus` focuses an element in the frame it appears. `nui.InputOptions.Style` exposes the inner editor element. `Element.Shortcut` reports an element-scoped key. | Autofocus and Escape-to-cancel can be done in FlowLens without new APIs. |
| `ui.Table` draws a resize edge with `Theme.Border` after every header cell, and titles in `Theme.TextMuted` at weight 600. `ListState.HeaderCell` decorates header cells. | The traffic table's header can be restyled through its own `ui.Theme` (already built by `capture.tableTheme`) and `HeaderCell`, without forking the table. |
| `ui/editor.State` has `NoScrollPastEnd` and a `Whitespace` mode. `CodeView` sets neither. | The scroll limit is a `widgets.CodeView` default. |
| `mygo.OpenDialogOptions` has `Message`, used on macOS. | Picker messages need only wiring in `backend/app/settings_window.go`. |
| `nui.ToastViewportOptions.Position` defaults to bottom-right, and `nui.ToastOptions.Progress` defaults to on. `widgets.Notifier` has no per-call timeout. | Placement, progress, and duration are fixed in `widgets` and the three window views. |
| `ui.Tester.Image()` renders a frame, and `Tester.Cursor`, `Tester.Focused`, and `Tester.Wheel` observe the remaining behavior. The Tester has no CJK font, and the 1.0.5 app cannot run on this machine. | Behavior is verified with Tester assertions. Visuals are checked against English renders, two reference screenshots, and values taken from the 1.0.5 Tailwind classes. |

The 1.0.5 values are read from the old templates: `h-8` is 32 px, `h-10` 40 px, `size-5` 20 px, `text-sm` 14 px at the default 16 px root, `px-2.5` 10 px, `w-30` 120 px. Colors come from `style.css` tokens: `--app-accent-selected`, `--app-control-bg`, and the method and status maps in `TrafficTable.vue`.

## Goals / Non-Goals

**Goals:**

- Close every difference listed in the proposal, with a test per spec scenario. Visual items get a render check.
- Put each new behavior in one shared place, such as `desktop/widgets` or `desktop/theme`, rather than repeating it per view.
- Keep the nuxt-ui-go change small, generic, and useful outside FlowLens.

**Non-Goals:**

- Pixel-identical rendering. Font rasterization, native scroll bars, and nuxt-ui-go component internals may differ.
- New features, changes to persisted data, or saving the header table's name column width, which 1.0.5 did not save.
- Changes to mygo. Its owner is outside this project.
- Reworking surfaces that already match, such as Python Plugins, Rewrite Rules, Update, and Memory Statistics, beyond the shared conventions.

## Decisions

### 1. 1.0.5 is the reference; exceptions are listed here

Each item is restored from the 1.0.5 source unless it appears below. An exception stays native only when 1.0.5 behavior worked around a WebView limit, or the platform draws the item itself.

| 1.0.5 behavior | Kept native behavior | Reason |
| --- | --- | --- |
| Hex view "Large hex view" gate and segment paging (`detailHexLargeBodyTitle`, `detailHexPage`, and related keys) | One virtualized hex list for any size | Paging bounded DOM size in the WebView. The native list already builds only the visible rows of a 500 MB body (migration task 7.8). |
| 10 px WebKit scroll bars | Platform overlay scroll bars | The toolkit draws native scroll bars. |
| Monaco bracket colorization and find widget | `ui/editor` highlighting and find bar | Monaco is not available. Highlighting, folding, and find are equivalent. |
| CSS color transitions of 150–200 ms | nuxt-ui-go motion tokens where a component has them | Transitions do not change behavior. |

Unused locale keys for these exceptions stay in the catalogs, so a later change can restore them without new translations.

### 2. Pointer cursor: nuxt-ui-go theme flag plus a FlowLens helper

Upstream, in `/home/josexy/work/nuxt-ui-go`, add `Theme.PointerCursor bool` (default false). Every interactive part the library builds then applies `ui.CursorPointer` when the flag is set and the part is enabled. Parts in scope: buttons, links, tab triggers, menu and listbox items, select and menu triggers, switches, checkboxes, radios, accordion and disclosure triggers, pagination, and tree items. Text inputs keep the I-beam. Release the change as v0.0.3 with tests, and move `go.mod` to it.

In FlowLens, `theme.Appearance` gains `PointerCursor` from `CommonConfig.PointerCursor`, and `theme.Resolve` sets it on both themes. A helper `widgets.Clickable(element, theme)` sets the hand on FlowLens' own clickable elements: `ui.ButtonBase` call sites, clickable rows, tabs, and the title bar controls. Each call site that attaches `OnClick` to a custom element goes through the helper.

Alternatives considered:

- *FlowLens wrappers only*: cannot reach the clickable parts inside library components such as tabs, menu items, and switches.
- *Hide the setting*: drops a 1.0.5 feature, against the chosen reference.
- *Change mygo so containers set a default cursor for clickables*: mygo is outside the maintainer's control.

### 3. Focus, Enter, and Escape live in `desktop/widgets`

- `widgets.AutoFocus() func(root, editor, leading, trailing ui.Element)` returns an `InputOptions.Style` that calls `editor.AutoFocus()`. Dialogs and inline inputs pass it. A grid cell editor also selects all of its text on open.
- `widgets.InlineInput(...)` wraps `nui.Input` for inline creation and rename. Enter submits, and Escape cancels, detected with `Shortcut(0, ui.KeyEscape)` on the editor element. Escape here is a control behavior, like `OnSubmit`, not an application command, so it stays out of the `desktop/shortcuts` catalog. Dispatcher bindings are unaffected, because the editor consumes the key only while focused.
- Name dialogs already submit on Enter through `OnSubmit`; tests confirm each one.

Alternative considered: a dispatcher command for Escape. Commands are user-rebindable and appear in Settings, and 1.0.5 never offered Escape as a command.

### 4. Wheel scrolling of strips: `widgets.HorizontalWheel`

`widgets.HorizontalWheel(strip ui.Element, state *ui.ScrollState)` installs `HandleInput`. For an `InputScroll` without Ctrl, it takes the axis with the larger magnitude, as `handleTabsWheel` in 1.0.5 did, and scrolls the strip when it overflows. When the strip does not overflow or is at its end, it returns false, so the event reaches the content around it. The workspace tab strip and the detail panel's two tab strips use it. The editor's request sub-tabs use it too, since they share the tab strip code.

### 5. Full-width tab strips

`inspect.Panel.tabBar` and the editor's request and response tab bars draw the strip's bottom border on the row itself, across its full width, and keep `nui.Tabs` for the triggers and the active underline. The response chips stay as a trailing child. Making the tabs element fill the scroll container's width was rejected: the tabs then do not scroll once they overflow.

### 6. One editable grid widget for every key/value editor

A new `widgets.EditableGrid` replaces `editor.rowsView` and the URL-encoded and form-data row builders. Its layout matches 1.0.5: an outer border, 40 px rows with a top border between rows, and columns of 46 px enable, key, value, and 46 px actions. Rows have a hover background, cells show text on one line, and empty cells show a muted placeholder. A key-column resize handle highlights in the accent color while hovered. Clicking a cell opens an overlay `nui.Textarea` (`AutoResize`) above it, with focus and its text selected. Enter, with or without Shift, commits, as does losing focus. Invalid-name and form-data file cells keep their current validation and file picking, as trailing slots of the cell.

The read-only variant, `widgets.FieldGrid`, replaces `widgets.KeyValueTable` in the detail panel. It has two columns with a resizable name column whose width lives in view state only. Clicking a cell opens a read-only overlay for selecting and copying the value. Response cookies use `FieldGrid` with a trailing attributes popover laid out as in 1.0.5.

Rows are built through a `ui.List`, so long tables stay virtualized as they are today. An open overlay belongs to one row key. It closes when that row leaves the list or when the data changes under it.

Alternative considered: keep always-on inputs and only restyle them. That gives no auto-growing editor for long values, and the layout differs from 1.0.5.

### 7. Traffic table styling through its own theme

`capture.tableTheme` gains the header values: elevated header background, `Text` instead of `TextMuted` for titles, and a transparent `Border` so that resize edges show only while hovered. The edge already switches to `Accent` on hover. Borders the table itself still needs are drawn by FlowLens. `HeaderCell` adds the accent sort arrow color if the built-in arrow cannot take it.

- Badges become opaque: the method color mixed 13% over the panel background, and the status tones from `badgeToneClass`. They keep a minimum width that fits `OPTIONS` and `CONNECT`, and the Method column's default width grows only if the text still does not fit.
- `cellBackground` adds the 1 px outline for highlighted and inspected rows.
- Duration and size cells use `FontFeatures("tnum")`.

### 8. Notifications

`widgets.Notifier` gains `WithTimeout(time.Duration)`, used by HAR import and Restore Initial State. It also sets `Progress: nui.Set(false)` on every toast. The main, Settings, and Update windows build `nui.ToastViewport` with `Position: nui.ToastTopCenter`.

### 9. Image preview

`inspect.imagePreview` computes a fit scale: the image's natural size limited to the pane bounds, never above 1. Its 100% means "fitted", as `max-h-full max-w-full object-contain` did in 1.0.5. Ctrl with `InputScroll` (and `InputZoom`) zooms in `logic.ImageScaleStep` steps. A primary-button drag pans an offset while the scale is above 1, using a grab cursor. The bar shows the scale, the info, and the `detailImageZoomHint` text. The zoom buttons added during the migration are removed, since the context menu already offers zoom.

### 10. Render harness for visual checks

Add a test-only package, `desktop/internal/rendertest`, with fixture traffic and a `Shoot(t, tester, name)` helper. When `FLOWLENS_RENDER_DIR` is set, it writes `Tester.Image()` as a PNG; otherwise it does nothing. Each surface's tests call it at 1600×860 in light and dark themes, mirroring the reference screenshots. No production code imports the package. Render checks are reviewed by eye against the references. They are not pixel assertions, because the font engine and platform rendering differ.

### 11. Remaining small items

- `widgets.CodeView` sets `NoScrollPastEnd` and turns whitespace rendering off for read-only views.
- The TLS and certificate rows use the existing `locales.Detail*` accessors and render list values one item per line.
- The category tree follows 1.0.5: tinted section bands, end-aligned chevrons, check marks for selected filters, and 20 px icons. The counts go, as 1.0.5 had none.
- API Collection rows follow 1.0.5: a hover grip handle as the only drag source, child-count pills, and name tooltips. The source row is dimmed to half opacity during a drag.
- Workspace tabs use `ScrollIntoView` when the active key changes. Drag uses `Element.Draggable` along the horizontal axis, with neighbors moving into the target slot. The strip auto-scrolls near its edges.
- The shutdown overlay becomes a non-dismissible modal-style card with the title, `AppShutdownDescription`, and a spinner.
- The settings pickers pass `Message`, and key pickers use the `SettingsPrivateKeyFileFilter` name.

## Risks / Trade-offs

- [The nuxt-ui-go release is delayed or rejected.] → Items in decisions 2–11 do not depend on it. The pointer-cursor tasks are last and can ship separately. Until they land, the setting stays a no-op, as it is today.
- [The editable grid regresses performance or IME input in large tables.] → Keep `ui.List` virtualization, open only one overlay at a time, and add Tester cases for 1,000 rows and for IME composition in the overlay. Chinese input still needs a check on each desktop platform.
- [A transparent table `Border` hides lines the table still needs.] → Render checks in both themes. If any line disappears, draw it explicitly in `cellBackground` or the header row.
- [A wheel handler on strips captures scrolling that should go to the page.] → The handler returns false unless the strip overflows and can still move in that direction. A Tester case covers the non-overflowing strip.
- [Visual parity is judged by eye from English renders.] → Use the two reference screenshots and the Tailwind-derived values. Record a desktop comparison per platform in the integration tasks.
- [nuxt-ui-go work sits outside this repository's allowed edit roots.] → It is done in its own repository with the maintainer's approval. This change consumes only the tagged release through `go.mod`.

## Migration Plan

No data migration is needed. Merge `restore-ui-parity` after `migrate-to-mygo-native-ui` is archived, so its added requirements merge into the capability specs. Rolling back means reverting the commits. Settings and stored data are untouched, and the `go.mod` bump can be reverted on its own.
