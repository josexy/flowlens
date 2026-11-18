# Native UI behavior and comparison

The MyGo interface preserves FlowLens's information hierarchy, business actions, data fidelity, and meaningful states. Native input, focus, selection, and accessibility behavior take priority. Layout, spacing, typography, animation, and control placement do not need to reproduce the Wails frontend pixel for pixel. A surface passes review when its information is readable and its actions remain discoverable and operable in both themes.

FlowLens 1.0.5 remains a source of product behavior and historical comparisons. The `align-native-ui-interactions` change supersedes the earlier blanket visual-parity rule; specific contracts such as ordered headers, export contents, error states, and destructive-action confirmation still apply. The earlier render and behavior reviews below are historical evidence, not a requirement to restore every visual difference.

## Source review, 2026-11-13

The review compared the original repository at `972cf419b60551bea63813b8c4dfe42b3a78c8c2` with the native repository at `78e6c2e2e04266b24b8f4e622a6a8911b5f49c6c`. The complete module inventory and code references are in [the change design](../../openspec/changes/align-native-ui-interactions/design.md).

| Area | Source-review conclusion |
| --- | --- |
| Shell, capture/history, editor, collection, rewrite, plugins, settings, update | The principal surfaces and business entry points have native implementations. This is not proof that every branch or platform behaves identically. |
| Traffic selection and details | The reviewed baseline overrode native selection to reproduce the original modifier-click model. The native-first change removes that override and gives details explicit open/close actions. |
| Navigation and editable grids | Several custom rows only handled clicks. Settings lost the original keyboard entry; some other mouse-only entry points were inherited from the old frontend. |
| Memory Statistics | The reviewed baseline kept fixed card/metric columns instead of adapting to available content width. Layout acceptance includes the minimum window with the widest sidebar. |
| Native replacements | Editors, Hex virtualization, menus, help placement, and chart interaction have deliberate differences, recorded below. |

Source review and headless tests do not establish real-window IME, DPI, platform-menu, drag/drop, or window-lifecycle compatibility. Keep unperformed platform checks in the migration verification matrix; do not infer completion from visual similarity or a successful build.

## Rendering fixes and scale baseline, 2026-11-13

A real-window comparison against original commit `972cf41` and native baseline `38a23db` found four rendering defects: Hex columns kept a stale measured width, the first imported HAR tab used its internal key, the Formatted body label was squeezed, and the traffic context menu clipped its last item at the toolkit's default maximum height. Hex now requests the frames needed to finish width-dependent layout, import completion uses its returned metadata, body tabs keep their label widths and update the displayed body in the same build, and context menus use the available window height while retaining native scrolling and positioning.

The comparison machine uses 150% monitor DPI and a separate 119% Windows accessibility text scale. MyGo already applies monitor DPI. `internal/desktop/theme.Begin` applies the accessibility factor once to the whole window and consumes it before resolving the component theme, keeping fixed geometry, custom text, code editors, and base controls at the same scale. Main, settings, update, and startup-progress windows share this entry point. Shared form details now include vertical number steppers, consistent settings selectors, palette swatches, required-field markers, and grouped shortcut rows with resolved keycaps.

Regression tests cover idle-frame Hex reflow at widths 320/525/900, complete body labels and immediate tab activation, imported tab naming, and the last traffic-menu action's visibility and hit target. Scale tests combine 100%/150%/200% rendering scales with 100%/119%/150% accessibility scales. `CGO_ENABLED=0 go test ./...` (with actual CPython 3 integration tests), `task vet:ui`, and a Windows GUI build passed. Windows lifecycle fixtures now isolate `APPDATA` as well as HOME/XDG paths.

The repaired Windows build was inspected with an isolated profile and a synthetic HAR: immediate import naming, body tabs, Hex at normal and narrower splitter widths, the complete traffic menu, and general/shortcut settings in light and dark themes. Local screenshots and the comparison report are under the ignored `bin/ui-audit/` directory. Other monitor configurations, cross-monitor transitions, macOS, and Linux still need real-window verification; the headless scale matrix is not a substitute for those checks.

## Remaining visual checklist, 2026-11-13

The follow-up comparison reran both Windows applications with the same synthetic HAR and 150% display / 119% text scaling. The remaining visual items were addressed as follows:

| Checklist item | Result |
| --- | --- |
| Traffic column widths | The path column keeps a 300-DIP default and grows into spare space. Saved/custom widths retain the 120-DIP minimum. Later columns remain available by native horizontal scrolling. The 100-DIP method column remains intentional. |
| Numeric fields | Use `nui.InputNumber` for display, editing, stepping, and validation. The field shows ungrouped digits when focused or blurred. |
| WebSocket send action | The circular XL button sits at the message editor's lower right, with the existing shortcut, loading state, and send guards. The draft selector and surrounding spacing match the original proportions. |
| Code appearance | Native editors use VS-like light/dark token colors, an 18/13 line-height ratio, and consistent framing/left inset. The settings store now supplies the actual code font size to request, response, plugin, SSE, and rewrite editors, including preview/discard. Native token categories, folding and gutter mechanics remain in use. The initial audit's description of Python string colors was corrected: ordinary strings are red in the original light theme. |
| Dialogs | Plugin forms use the shared 512-DIP modal width, 16-DIP field spacing, and subdued header/footer borders. Descriptions grow from three to eight lines using native Textarea sizing rather than a WebView resize handle. Domain-filter controls use standard button sizing, readable count pills, and bounded scrollable groups. |
| Memory Statistics | Summary-card line heights now match the calibrated density. No sample still means unknown; an actual zero-valued sample displays zero and its absent GC time displays “No GC yet”. This intentionally avoids claiming that an unobserved runtime has no GC activity. |
| Already-correct behavior | Empty plugin names still fail validation, duplicate headers remain separate, and missing raw header order still produces its degradation warning. |

Regression coverage includes default/custom path widths, horizontally reached sort controls, numeric bounds and invalid input, code-font preview/discard, and the floating send button after repeated rebuilds and resizes. The full Go suite (using real CPython 3), MyGo UI vet, and the Windows GUI build pass. The new local screenshot report is `bin/ui-audit/round2/report.html`; it also records the retained native differences. The cross-platform and cross-monitor limits described above still apply.

## Font settings follow-up, 2026-11-13

Font selectors now write selections back in their change callbacks, so rebuilding the settings window no longer resets the chosen family. The native settings host enumerates MyGo's usable font families instead of Windows registry font-file labels such as `SimSun & NSimSun`. Returning a draft to the saved font size also refreshes the appearance preview, and unchanged previews no longer invalidate every window repeatedly.

Custom interface text uses the same size ratio as nui controls while retaining the existing heading/body/caption hierarchy. Code editors and explicit font samples retain independent sizes. Text rows, count pills, shared field grids, and rewrite-rule drag spacing accommodate larger interface fonts.

The full Go suite, MyGo UI vet, and Windows GUI build passed. Headless coverage includes font selection/save/reopen/discard, size reset, independent code sizing, 12/16/24 interface sizes across DPI/accessibility scales, large field rows, and rewrite dragging. Real-window checks confirmed Arial selection and the 24-size settings layout. The final manual pass was stopped before validating the corrected Chinese font list and saved-font restoration after a process restart; those checks remain outstanding, along with the platform limits above.

## Deliberate differences

| 1.0.5 behavior | Native behavior | Reason |
| --- | --- | --- |
| Hex view "Large hex view" gate and segment paging | One virtualized hex list for any size | Paging bounded DOM size in the WebView; the native list builds only visible rows. |
| 10 px WebKit scroll bars | Platform overlay scroll bars | The toolkit draws native scroll bars. |
| Monaco bracket colorization and find widget | `ui/editor` highlighting and find bar | Monaco is not available. |
| CSS color transitions of 150–200 ms | nuxt-ui-go motion where a component has it | Transitions do not change behavior. |
| "Use pointer cursor" setting | Removed | The native app does not need it. |
| Method column 80 px wide by default | 100 px | The native fonts are wider than the WebView's: at 80 px, `OPTIONS` and `CONNECT` badges did not fit. A saved width still applies. |
| API Collection name tooltip anchored on the name | Anchored on the whole row, shown to its right | A hover-tracking anchor inside the row would take the row's clicks in MyGo's hit testing. |
| Menu item icons and highlight swatches in every menu | Shown in context menus, which are `nui.ContextMenu` drawn in the window as 1.0.5's `UContextMenu` was, and in macOS menu-bar menus; Windows and Linux menu-bar menus show none | Those menu bars draw no item icons. |
| No API Collection menu item to move a nested folder to the root | Move to Root in a nested folder's menu | It gives a path without dragging to the drop zone's action. |
| Error toasts when writing to the clipboard fails | No such toasts | Native clipboard writes cannot fail. |
| Shift-click toggles a row; Ctrl/Cmd-click toggles details; no table keys | Native single/additive/range/drag selection and keyboard movement; Enter or unmodified double-click opens details; Close details preserves selection | The toolkit owns selection and activation. Modifier clicks only select. |
| Each Python console and SSE message row a tab stop, opened with Enter or Space | Each table one tab stop: the arrow keys move between rows, and Enter opens one | MyGo's table is one focusable list. |
| WebSocket messages can switch between list and conversation modes | WebSocket messages use a list | Both the client and the traffic detail panel use the same message list. |
| A press on a selected traffic row released on the next row, under the drag threshold, clicks the row pressed | It clicks no row | MyGo leaves such a press to the release, and the table cannot tell it from a press on the scroll bar. |
| Python API help docked beside the content in a workspace 1100 px wide or more, a Slideover in a narrower one | A Slideover at the window's right edge, 420 px wide, at any width | One placement at every size; the Slideover keeps the content's width and closes with Escape or a click beside it. |
| Dragging the secondary splitter to zero collapses it | Clicking the active primary section toggles the sidebar; dragging changes its width within bounds | Keep the explicit native collapse action. |
| Memory chart supports horizontal drag-to-zoom and double-click reset | Native chart offers sample inspection, keyboard movement, and series visibility; no drag zoom | Keep the toolkit's chart interaction without a second gesture implementation. |

The following replacements are accepted on code and headless-test evidence. Platform rendering and IME remain subject to the real-window checks above. Original paths below are relative to `frontend/src/components/`; native paths are relative to the repository root.

| Replacement | Original evidence | Native evidence |
| --- | --- | --- |
| Code editor | `common/MonacoBodyEditor.vue` | `internal/desktop/widgets/code_view.go` (`ui/editor`) |
| Virtual Hex | `common/HexDumpViewer.vue` | `internal/desktop/inspect/hex.go`, `internal/desktop/logic/hexdump.go` |
| Menus | `menu/TrafficContextMenu.vue`, `traffic-workspace/WorkspaceTabBar.vue` | `internal/desktop/widgets/context_menu.go`, `internal/desktop/capture/menu.go`, `internal/desktop/editor/tabbar.go` |
| Help Slideover | `workbench/ContentHost.vue` | `internal/desktop/plugins/help.go`, `internal/desktop/shell/app.go` |
| Sidebar collapse | `../views/MainView.vue`, `../stores/workbench.ts` | `internal/desktop/shell/app.go`, `internal/desktop/store/workbench.go` |
| Memory chart | `memstats/MemStatsChart.vue` | `internal/desktop/memstats/view.go`, pinned nuxt-ui-go `chart.go` |

## Native interaction implementation, 2026-11-13

Traffic tables now use MyGo selection directly. Enter, an unmodified native double-click, and **Show** share the detail-opening action; **Close details** returns focus without clearing selection. Settings and the custom navigation rows use native button activation with independent child checkboxes/switches. Request grids use focusable text cells and an anchored Textarea: native Tab order, Enter/blur commit, Escape restores the original value, and composition confirmation stays inside the editor. Row identity checks end editing when its data disappears.

Memory Statistics now wraps its toolbar and summary cards and stacks metric cards according to the content area's available width. This preserves the native chart's inspection and series toggles, without drag zoom. Tester checks cover content widths 558/770/820/1360 DIPs (including a 1024-wide window with a 42% sidebar), paused/running resize cycles, retained samples and hidden series, both languages/themes, the maximum configured interface font, and 150% rendering scale. Narrow light/dark renders were inspected. These checks do not replace the real-window platform matrix.

## MyGo 0.9.0 and NuxtUI-Go 0.1.0, 2026-11-14

Component imports now use `github.com/josexy/nuxt-ui-go/ui`. Menu separators and the shared traffic/SSE/console table header divider use NuxtUI-Go's pixel-aligned rendering; FlowLens no longer overlays those components with its own divider drawing. The shared table still extends cell content across native padding, including vertical stretching, so row backgrounds, outlines, and hit targets cover the full cell.

The custom title/status bars retain their one-device-pixel drawing: MyGo's ordinary borders use rounded DIP widths, and these bars are not NuxtUI-Go components. `internal/desktop/theme.Begin` still applies Windows accessibility text scaling once to the whole window. Neither behavior is replaced by the upstream divider fixes.

Ordinary pane, grid, code-view, card, tab, and window-frame borders use the page's `nui.Theme.Border`. MyGo's native `Theme.Border` maps to Nuxt's stronger `BorderAccented`; do not use it for page borders. The shared table adapts its local theme snapshot so both native column dividers and Nuxt's header rule match the page border. Control outlines and selection, focus, error, and drop indicators keep their semantic colors.

The migration audit also checked table column-change notifications, programmatic focus scrolling, tab-panel names, and landmark roles. FlowLens does not use TableOptions.OnChange as a selection-only callback, its tab strips build their content separately, and it does not nest the new landmark components. Existing selection, column-layout persistence, menu, tab, and focus tests cover the affected interactions.

The full Go suite (with CPython 3.12), Go lint, MyGo UI vet, formatting/catalog checks, and Windows build passed. Existing pixel tests cover light/dark themes, 100%/125%/150%/200% display scales, and 100%/119% zoom. Traffic/menu/SSE/console renders were inspected under `bin/ui-upgrade-v090/`; cross-monitor transitions and macOS/Linux real windows remain unverified.

## MyGo 0.9.2 and NuxtUI-Go 0.1.1, 2026-11-16

FlowLens needs no source change; NuxtUI-Go 0.1.1 only moves to MyGo 0.9.2 and documents it. The audit checked each MyGo change its migration notes name:

- `ui.SVG` is no longer comparable. FlowLens holds icons only as `*ui.SVG` and never compares SVG values or uses them as map keys.
- `Element.PopoverMenu` takes variadic options. FlowLens does not call it; its context menus are `nui.ContextMenu`.
- `ui.Tester` gives keys pressed without Ctrl or Super during `Compose` to the input method, as each platform's filter does. FlowLens has no `Composing()` guards of its own and relies on that filter, as NuxtUI-Go does; `TestEditableGridCompositionStaysInEditor` now exercises it.
- `RequestSingleInstanceLock` holds the lock until the process exits, so the update restart's `Relaunch` waits for the old process instead of starting beside it. Startup still takes the lock before opening SQLite.

`ListState.MoveAnimation` is not adopted, because NuxtUI-Go's motion levels and `AppConfig.DisableAnimations` do not govern it. Windows builds do not use the `mygo_nowebview` tag, because no build with that tag has been run on Windows yet.

On Linux, formatting, Go lint, MyGo UI vet, catalog checks, the Go suite, and the Windows and Linux builds passed. The exception is `TestCustomTextMatchesControlsAcrossSizesAndDPI`, which fails the same way under MyGo 0.9.0 on hosts without Arial. The race detector passed for `internal/app`, process attribution, the proxy service, and the desktop packages except `capture`, which exceeds the default test timeout under `-race`. Its non-race run takes the same time under both MyGo versions. Real windows remain unverified.

## Historical render review

The original review used a dedicated `TestRender…` case for each surface. Redundant screenshot-only cases were removed after behavior tests covered those surfaces. Remaining render helpers and behavior tests can still write PNGs through `internal/desktop/internal/rendertest` when `FLOWLENS_RENDER_DIR` is set. To run the remaining `TestRender…` cases explicitly:

```sh
FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./internal/desktop/... -run TestRender -count=1
```

The historical renders below were compared with `assets/screenshots/flowlens-{light,dark}.png` of the 1.0.5 repository and the values in the `restore-ui-parity` design. Some of their dedicated generators no longer exist. Beyond the deliberate differences above, the remaining differences are:

- Font rendering. The headless tester draws with its bundled font, which is wider than Segoe UI, SF Pro, or Noto Sans, and has no CJK glyphs; Chinese layouts were checked by tests rather than by eye.
- Process icons are blank in renders, because the tests have no icon cache. Rows otherwise match.
- The capture render shows the table without the detail panel; `capture-detail` shows it open beside the inspected row, after an explicit open action. The test backend serves no bodies, so its body tab is empty. The detail panel has its own renders (`detail-headers`).
- Native scroll bars, as listed above.

The `restore-ui-behavior-parity` renders (`capture-detail`, `sse-detail-formatted`, `ws-detail-receive-text`, `ws-detail-send-binary`, `python-console`, `python-console-detail`, `menu-highlight`, `menu-history`, `tab-menu`, `settings-root-ca-add`, and, after its review, `sse-list`, `ws-list`, and `ws-conversation`) match the 1.0.5 templates and the `restore-ui-behavior-parity` design, except:

- The Python console and SSE message tables' headers have no elevated background, and their rows no line between them: MyGo's `ui.Table` draws its own header and rows.
- The console's Output column fills the room the others leave, at least 240 px rather than 1.0.5's 260 px. Once the user resizes it, it keeps that width rather than filling the rest.
- WebSocket text messages and console entries show in a read-only code view with line numbers, where 1.0.5 had a plain text area.
- Context menus are `nui.ContextMenu`, drawn in the window as in the app; the menu bar's menus are the system's, which still need the desktop pass.

Interaction that renders cannot show (focus, wheel scrolling of strips, grid editing with an input method, drag feedback, and notifications) is covered by Tester cases, and still needs a desktop pass on each platform.

## Historical behavior review

The `restore-ui-behavior-parity` change compared what each surface does with 1.0.5: its actions, modifier keys, enabled states, copied text, and default file names. These surfaces were found to match and were left as they are:

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
- Update and Memory Statistics.
- Shell, status bar, and title bar.

A review of the change found that two of these still differed and restored them: the SSE message list of the body pane, and the WebSocket message rows and bubbles of the detail panel and the WebSocket Client.

## Tooltips

Tooltips carried over from the 1.0.5 templates (`frontend/src`) and their locale keys. Rows marked *dynamic* show data.

This table is a historical migration inventory. Tooltip behavior is checked by the surface-level `tooltip_test.go` cases and `rendertest.RequireTooltip`; catalog keys and placeholders are checked by `locales/locales_test.go` and `task i18n:check`. The old AST scan was removed because finding a locale accessor anywhere in a tooltip-building call graph did not establish that the corresponding control actually showed that tooltip.

| 1.0.5 template | Control | Keys |
| --- | --- | --- |
| `common/AppSidebar.vue` | Section buttons | `menu.capture`, `menu.category`, `menu.api_collection`, `menu.rewrite_rules`, `menu.python_plugins`, `menu.memstats` |
| `common/AppSidebar.vue` | Theme | `theme.tooltip` |
| `common/AppSidebar.vue` | Language | `language.switch` |
| `common/AppSidebar.vue` | Settings | `menu.settings` |
| `common/StatusBar.vue` | Environment | `status.action_environment` |
| `common/StatusBar.vue` | GitHub | `status.action_github` |
| `common/StatusBar.vue` | Update | `status.update_ready`, `status.update_error` |
| `common/StatusBar.vue` | Detail layout | `status.switch_horizontal_layout`, `status.switch_vertical_layout` |
| `common/websocket/WebSocketMessageStream.vue` | Direction filter | `workspace.websocket_client.filter_all`, `workspace.websocket_client.filter_send`, `workspace.websocket_client.filter_receive` |
| `common/websocket/WebSocketMessageStream.vue` | Clear | `workspace.websocket_client.clear_messages` |
| `modal/WebSocketMessageDetailModal.vue` | Wrap, copy, save | `detail.wrap_body`, `detail.copy_body`, `detail.save_body` |
| `python-plugins/PythonPluginRulesPane.vue` | Delete rule | `python_plugins.delete_rule` |
| `python-plugins/PythonPluginSurface.vue` | Save file (with binding) | `python_plugins.save_file` |
| `python-plugins/PythonPluginSurface.vue` | Validate, reload | `python_plugins.validate_hint`, `python_plugins.reload_hint` |
| `python-plugins/PythonPluginSurface.vue` | Plugin actions (directory access is in the sidebar) | `python_plugins.rename_plugin`, `python_plugins.delete_plugin` |
| `python-plugins/PythonPluginSurface.vue` | File actions | `python_plugins.new_file`, `python_plugins.rename_file`, `python_plugins.delete_file` |
| `python-plugins/PythonPluginSurface.vue` | Active revision | *dynamic*, falls back to `python_plugins.no_revision` |
| `python-plugins/PythonPluginSurface.vue` | Validation error | *dynamic* |
| `python-plugins/PythonPluginWorkspaceSidebar.vue` | Help, root directory, create | `python_plugin_help.title`, `python_plugins.open_root_directory`, `python_plugins.create` |
| `python-plugins/PythonPluginWorkspaceSidebar.vue` | Validation dot | `python_plugins.validation_valid`, `python_plugins.validation_invalid`, `python_plugins.validation_unavailable` |
| `rewrite-rules/RewriteBodyPreview.vue` | Copy input or output | `rewrite_rules.regex_test_copy_input`, `rewrite_rules.regex_test_copy_output` |
| `rewrite-rules/RewriteBodyPreview.vue` | Wrap, preview, edit | `rewrite_rules.regex_test_wrap`, `rewrite_rules.regex_test_preview`, `rewrite_rules.regex_test_edit` |
| `rewrite-rules/RewriteFieldOperations.vue` | Operations help | `rewrite_rules.operations_hint` |
| `rewrite-rules/RewriteRulesSidebar.vue` | Refresh, new rule | `rewrite_rules.refresh`, `rewrite_rules.new_rule` |
| `rewrite-rules/RewriteRulesSurface.vue` | Save (with binding) | `rewrite_rules.save` |
| `settings/CACertificateInfoPanel.vue` | Trust action | `settings.ca_trust_loading`, `settings.ca_actions_save_first`, `settings.ca_trust_status_unavailable` |
| `settings/CACertificateInfoPanel.vue` | Generate | `settings.ca_actions_save_first`, `settings.ca_trust_uninstall_first` |
| `settings/CertificateFileInput.vue` | Browse | `settings.select_file` |
| `settings/ClientCertificateList.vue` | Remove | `settings.remove_client_cert` |
| `settings/PythonPluginSettings.vue` | Browse interpreter | `settings.python_interpreter_browse` |
| `settings/RootCAPathList.vue` | Remove | `settings.remove_root_ca` |
| `settings/ShortcutSettings.vue` | Clear binding | `shortcuts.clear_for` |
| `settings/ShortcutSettings.vue` | Reset binding | `shortcuts.reset_for` |
| `traffic/BodyViewer.vue` | Wrap, copy, save | `detail.wrap_body`, `detail.copy_body`, `detail.save_body` |
| `traffic/CookieTablePane.vue` | Copy | `detail.copy_cookie_header`, `detail.copy_set_cookie_header` |
| `traffic/detail/DetailHeadersPane.vue` | Copy JSON, sort, copy text | `detail.copy_request_headers_json`, `detail.sort_request_headers`, `detail.copy_request_headers_text`, `detail.copy_response_headers_json`, `detail.sort_response_headers`, `detail.copy_response_headers_text`, `detail.copy_response_trailers_json`, `detail.sort_response_trailers`, `detail.copy_response_trailers_text` |
| `traffic/detail/DetailProcessSection.vue` | Copy path | `detail.copy_process_path` |
| `traffic/detail/DetailRawHTTPPane.vue` | Wrap, copy | `detail.wrap_body`, `detail.copy_raw_http` |
| `traffic/DetailPanel.vue` | Status chip | *dynamic* (the status text) |
| `traffic/DNSCacheControl.vue` | DNS cache | `toolbar.dns_cache_on`, `toolbar.dns_cache_off` |
| `traffic/PortConfigPopover.vue` | Port | `toolbar.quick_port` |
| `traffic/ProxyToolbar.vue` | Mode, host | `toolbar.quick_mode`, `toolbar.quick_host` |
| `traffic/ProxyToolbar.vue` | System proxy (with binding) | `toolbar.system_proxy_on`, `toolbar.system_proxy_off`, `toolbar.system_proxy_mode_unsupported` |
| `traffic/ProxyToolbar.vue` | Toggles | `toolbar.disable_proxy_on`, `toolbar.disable_proxy_off`, `toolbar.disable_http2_on`, `toolbar.disable_http2_off`, `toolbar.anti_cache_on`, `toolbar.anti_cache_off`, `toolbar.anti_comp_on`, `toolbar.anti_comp_off`, `toolbar.skip_verify_tls_on`, `toolbar.skip_verify_tls_off` |
| `traffic/ProxyToolbar.vue` | Domain filters | `toolbar.host_filter_tooltip` |
| `traffic/ProxyToolbar.vue` | Start or Stop (with binding) | `toolbar.start`, `toolbar.stop` |
| `traffic/ProxyToolbar.vue` | Clear (with binding) | `toolbar.clear` |
| `traffic/QueryTablePane.vue` | Copy query | `detail.copy_query` |
| `traffic/ResponseCookieTable.vue` | Attributes, copy raw | `detail.cookie_attributes`, `detail.copy_set_cookie_header` |
| `traffic/SseMessageViewer.vue` | Jump to latest | `detail.sse.jump_to_latest` |
| `traffic/SseMessageViewer.vue` | Wrap, copy, save | `detail.wrap_body`, `detail.sse.copy_message`, `detail.sse.save_message` |
| `traffic-workspace/EditableKeyValueTable.vue` | Invalid name icon | `workspace.http_request.header_invalid_name` |
| `traffic-workspace/EditableKeyValueTable.vue` | Duplicate icon | *never shown*: every 1.0.5 caller passed `show-duplicate-warning="false"` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Python plugins switch | `workspace.http_request.plugins_enabled`, `workspace.http_request.plugins_bypassed`, `workspace.http_request.plugins_global_disabled` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Send or Stop (with binding) | `workspace.http_request.send_request`, `workspace.http_request.stop_request`, `workspace.http_request.stop_stream` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Header actions | `workspace.http_request.copy_request_headers_json`, `workspace.http_request.copy_request_headers_text`, `workspace.http_request.clear_request_headers`, `workspace.http_request.add_request_header` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Word wrap | `workspace.word_wrap` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Python help | `python_plugin_help.title` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Response chip | *dynamic* |
| `traffic-workspace/HttpRequestEditorPane.vue` | Response header actions | `workspace.http_request.copy_response_headers_json`, `workspace.http_request.copy_response_headers_text` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Trailer actions | `workspace.http_request.copy_response_trailers_json`, `workspace.http_request.copy_response_trailers_text` |
| `traffic-workspace/HttpRequestEditorPane.vue` | Copy error | `workspace.http_request.copy_response_error_details` |
| `traffic-workspace/HttpRequestFormDataBody.vue` | Add row, clear all | `workspace.http_request.form_data_add_row`, `workspace.http_request.form_data_clear_all` |
| `traffic-workspace/HttpRequestFormDataBody.vue` | File path | *dynamic* |
| `traffic-workspace/HttpRequestPythonConsole.vue` | Console actions | `workspace.http_request.console_copy_all`, `workspace.http_request.console_save`, `workspace.http_request.console_clear` |
| `traffic-workspace/HttpRequestPythonConsole.vue` | Entry actions | `workspace.http_request.console_wrap`, `workspace.http_request.console_copy_entry`, `workspace.http_request.console_save_entry` |
| `traffic-workspace/HttpRequestUrlEncodedBody.vue` | Add row, clear all | `workspace.http_request.urlencoded_add_row`, `workspace.http_request.urlencoded_clear_all` |
| `traffic-workspace/RequestCookiePane.vue` | Cookie actions | `detail.copy_cookie_header`, `workspace.http_request.clear_cookies`, `workspace.http_request.add_cookie` |
| `traffic-workspace/RequestFileBody.vue` | Choose file | `workspace.http_request.file_choose` |
| `traffic-workspace/RequestFileBody.vue` | File name and path | *dynamic* |
| `traffic-workspace/RequestMitmProxyButton.vue` | MITM proxy | `workspace.http_request.enable_mitm_proxy`, `workspace.http_request.disable_mitm_proxy` |
| `traffic-workspace/RequestQueryPane.vue` | Query actions | `workspace.http_request.copy_query`, `workspace.http_request.clear_query_parameters`, `workspace.http_request.add_query_parameter` |
| `traffic-workspace/WebSocketClientPane.vue` | Connect or Disconnect | `workspace.websocket_client.connect`, `workspace.websocket_client.disconnect`, `workspace.websocket_client.stop_connecting` |
| `traffic-workspace/WebSocketClientPane.vue` | Word wrap | `workspace.word_wrap` |
| `traffic-workspace/WebSocketClientPane.vue` | Send (with binding) | `workspace.websocket_client.send_message` |
| `traffic-workspace/WebSocketClientPane.vue` | Header actions | `workspace.http_request.copy_request_headers_json`, `workspace.http_request.copy_request_headers_text`, `workspace.http_request.clear_request_headers`, `workspace.http_request.add_request_header` |
| `traffic-workspace/WebSocketClientPane.vue` | Response header actions | `workspace.http_request.copy_response_headers_json`, `workspace.http_request.copy_response_headers_text` |
| `traffic-workspace/WebSocketClientPane.vue` | Copy error | `workspace.websocket_client.copy_response_error_summary` |
| `traffic-workspace/WorkspaceSidebar.vue` | Restart capture | `capture.restart` |
| `traffic-workspace/WorkspaceSidebar.vue` | Import HAR | `har_import.title`, `har_import.progress` |
| `traffic-workspace/WorkspaceSidebar.vue` | History refresh, clear all | `history.refresh`, `history.clearAll` |
| `traffic-workspace/WorkspaceTabBar.vue` | Unsaved marker, close | `workspace.tab_bar.unsaved_changes`, `workspace.close` |
| `api-collection/ApiCollectionTree.vue` | Row name | *dynamic* (the full name) |
| `workbench/ApiCollectionWorkspaceSidebar.vue` | Refresh, new folder | `api_collection.refresh`, `api_collection.new_folder` |
| `workbench/ApiCollectionWorkspaceSidebar.vue` | Expand or collapse all | `api_collection.expand_all`, `api_collection.collapse_all` |
| `workbench/ApiCollectionWorkspaceSidebar.vue` | Multi-select | `api_collection.multi_select`, `api_collection.exit_multi_select` |
| `workbench/ApiCollectionWorkspaceSidebar.vue` | Delete | `api_collection.delete` |
