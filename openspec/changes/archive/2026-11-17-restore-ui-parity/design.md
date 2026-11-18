# Design

## Context

See `proposal.md` for the differences and why they matter. The facts below shape the approach. Each was checked against mygo v0.8.3, nuxt-ui-go v0.0.2, and the 1.0.5 sources in `/home/josexy/work/flowlens/frontend/src`.

| Fact | Consequence |
| --- | --- |
| `CommonConfig.PointerCursor` is shown in General settings and saved, but nothing reads it. `decodeSettingSection` decodes the common section with plain `json.Unmarshal`. | Removing the field ignores stored values on load and drops them at the next save, with no migration. |
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

**Non-Goals:**

- Pixel-identical rendering. Font rasterization, native scroll bars, and nuxt-ui-go component internals may differ.
- New features, persisted data beyond dropping `pointerCursor`, or saving the header table's name column width, which 1.0.5 did not save.
- Changes to mygo or nuxt-ui-go. FlowLens stays on mygo v0.8.3 and nuxt-ui-go v0.0.2.
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
| "Use pointer cursor" setting | Removed; controls show the platform's default cursor | The maintainer decided the native app does not need it (decision 2). |

Unused locale keys for the first four exceptions stay in the catalogs, so a later change can restore them without new translations.

### 2. Remove the Pointer Cursor setting

The maintainer decided the setting is not needed after the move to MyGo, so FlowLens drops it rather than restoring it:

- Remove the General settings row in `desktop/settings/general.go`, and the `PointerCursor` reset in Restore Initial State defaults in `desktop/store/settings.go`.
- Remove `CommonConfig.PointerCursor` from `backend/services/setting_service/setting_types.go`, along with its default and round-trip tests. A test instead stores a common payload containing `"pointerCursor":true` and checks that it loads and saves without error.
- Several `desktop/settings` tests toggle `PointerCursor` only to make the draft dirty. Point them at another common boolean, such as `LogDisabled`.
- Remove `settingsPointerCursorLabel` and `settingsPointerCursorHint` from both catalogs, and regenerate with `task i18n:generate`.
- Note the removal in the `CHANGELOG.md` 1.1.0 section.

Alternatives considered:

- *Implement it*: needs a `Theme.PointerCursor` flag in every interactive nuxt-ui-go part, a release, a `go.mod` bump, and a FlowLens helper at about 230 clickable call sites. mygo resolves the cursor from the innermost clickable, so a container cannot set it for its children. The maintainer declined this cost.
- *Hide the row but keep the field*: leaves a setting nothing reads, which `setting_service` would still default, validate, and persist.

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

- [Users who enabled the pointer cursor in 1.0.5 find the option gone.] → It never worked in the native build. The 1.1.0 release notes state that it was removed, and their other settings load unchanged.
- [The editable grid regresses performance or IME input in large tables.] → Keep `ui.List` virtualization, open only one overlay at a time, and add Tester cases for 1,000 rows and for IME composition in the overlay. Chinese input still needs a check on each desktop platform.
- [A transparent table `Border` hides lines the table still needs.] → Render checks in both themes. If any line disappears, draw it explicitly in `cellBackground` or the header row.
- [A wheel handler on strips captures scrolling that should go to the page.] → The handler returns false unless the strip overflows and can still move in that direction. A Tester case covers the non-overflowing strip.
- [Visual parity is judged by eye from English renders.] → Use the two reference screenshots and the Tailwind-derived values. Record a desktop comparison per platform in the integration tasks.

## Migration Plan

No data migration is needed. A stored `pointerCursor` value is ignored on load and dropped at the next save of the common section. Archive `migrate-to-mygo-native-ui` before this change. Its General settings requirement is the base that the `application-settings` delta modifies, and `openspec` refuses to archive the MODIFIED block until that spec exists. Rolling back means reverting the commits. A rollback that restores the field reads it as false for users whose settings were saved in between.
