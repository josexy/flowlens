# Proposal

## Why

The native MyGo interface works end to end, and commit `aad9892` already brought the shell, toolbar, table, and sidebars close to the 1.0.5 Wails build. A side-by-side review of `/home/josexy/work/flowlens/frontend/src` against `desktop/` still finds differences in styling and interaction that users of 1.0.5 will notice. Some are regressions: a saved setting that does nothing, controls that are hard to read, input that no longer works. Others change familiar layouts. The maintainer chose 1.0.5 as the reference, so this change restores it wherever MyGo and nuxt-ui-go can express it.

## What Changes

The review compared the old Vue templates and styles with the Go views, rendered the new main window headlessly in both themes, compared it with `assets/screenshots/flowlens-{light,dark}.png` from the old repository, and listed locale keys that the old frontend used but no Go code uses. Differences found:

- **Cross-cutting interaction**
  - The saved Pointer Cursor setting has no effect.
  - Inputs that appear after an action, such as inline folder creation, rename, create, and alias dialogs, and cell editors, do not take focus. 1.0.5 focused them in 13 places.
  - Escape does not cancel inline folder creation.
  - A vertical wheel does not scroll the overflowing workspace and detail tab strips; MyGo scrolls them sideways only with Shift.
  - Notifications sit bottom-right in the main window and top-right in Settings and Update, and show a progress bar. 1.0.5 put them top-center in every window, without a progress bar, and kept HAR import results on screen for 5 or 8 seconds.
  - Some icon-only controls lost their tooltips.
  - The shutdown indicator lost its description and spinner.
- **Appearance**
  - Method badges are translucent, so on a selected row in the dark theme they nearly vanish.
  - Long methods are truncated (`OPTI…`).
  - The table header draws column dividers that 1.0.5 did not.
  - Highlighted and inspected rows lost their 1 px outline.
  - Duration and size columns no longer use tabular numbers.
- **Traffic detail**
  - Request and response tab strips draw their bottom border only under the tabs.
  - Header, query, and cookie tables now wrap every value to full height with a fixed 50/50 split. 1.0.5 used compact one-line rows, a resizable name column, and click to read the full value.
  - TLS and certificate labels are hard-coded in English, and supported lists are joined with commas.
  - Response cookies became cards that lack the attribute grid, the empty-value and flag markers, and the raw Set-Cookie section.
  - Image preview no longer fits the pane, has no Ctrl+wheel zoom, no drag to pan, and no hint.
  - Code viewers scroll past the last line.
- **Request workspace**
  - Key/value editors are always-on single-line inputs. 1.0.5 used a bordered grid whose cells open an auto-growing editor on click.
  - The active tab is not scrolled into view.
  - A dragged tab gives no visual feedback while it moves.
- **API Collections, Categories, and Settings**
  - Tree rows lost the end-aligned chevron, child counts, 20 px icons, method label style, and name tooltip. A dragged row is not dimmed.
  - Category section headers lost their tinted band.
  - Selected host and process filters show a background highlight and counts, where 1.0.5 showed a check mark.
  - Settings file pickers lost their messages and the private-key filter name.

Each item is restored to 1.0.5. Where MyGo replaces a WebView workaround, such as the hex viewer's segment paging and its large-body gate, which virtualization makes unnecessary, the native behavior stays and `design.md` records it as an exception.

nuxt-ui-go v0.0.2 has no theme-level cursor hook. With the maintainer's approval, a small upstream addition to `github.com/josexy/nuxt-ui-go` adds one, and FlowLens moves to the release that contains it.

## Capabilities

### New Capabilities

- `interaction-conventions`: Interaction rules shared by every FlowLens window: focus when an input appears, Enter and Escape in inline editors, wheel scrolling of horizontal strips, tooltips on icon-only controls, notification placement and duration, and drag feedback.

### Modified Capabilities

These capabilities are defined by the in-flight `migrate-to-mygo-native-ui` change and are not yet in `openspec/specs/`. This change adds requirements to them; it does not alter existing ones.

- `appearance`: 1.0.5 as the visual reference, the Pointer Cursor setting, and legible colored badges on any row state.
- `desktop-shell`: The shutdown indicator's content.
- `traffic-capture`: Traffic table header, badge, row marker, and numeric styling.
- `traffic-inspection`: Compact field tables, full-width detail tab strips, localized TLS and certificate details, response cookie attributes, image zoom and pan, and code viewer scroll limits.
- `traffic-history`: Category tree section headers and filter check marks.
- `request-workspace`: Grid key/value editors, keeping the active tab in view, and tab drag feedback.
- `api-collections`: Tree row presentation and drag feedback.
- `application-settings`: File picker messages and filters. Shortcut row tooltips fall under `interaction-conventions`.

## Impact

- **Code**: `desktop/{shell,capture,inspect,editor,collection,settings,update,widgets,theme}`. New shared widgets: an editable grid, a wheel-to-horizontal helper, and autofocus support. `backend/app/settings_window.go` gains dialog messages. Copy changes use existing locale keys; any new keys go into both catalogs.
- **Dependencies**: `github.com/josexy/nuxt-ui-go` moves from v0.0.2 to a release with a pointer-cursor theme option. That work happens in `/home/josexy/work/nuxt-ui-go`, outside this repository's edit roots, with the maintainer's approval.
- **Persistence**: No changes to schemas or settings. The header table's name column width stays in the view and is not saved, as in 1.0.5.
- **Ordering**: Archive `migrate-to-mygo-native-ui` first, so these added requirements merge into its capability specs.
- **Verification limits**: The headless Tester has no CJK font, and the old app cannot run here. Visual checks compare English headless renders against the two reference screenshots plus code-derived 1.0.5 values. A desktop pass on each platform still has to confirm the final look.
