# UI Parity with FlowLens 1.0.5

FlowLens 1.0.5, the last Wails build, is the visual and interaction reference for the native MyGo interface. This page records what was compared, the deliberate differences, and checks that keep the two aligned. The `restore-ui-parity` change in `openspec/changes/` explains the decisions.

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
| Menu item icons and highlight swatches in every menu | Shown in context menus on every platform and in macOS menu-bar menus; Windows and Linux menu-bar menus show none | Those menu bars draw no item icons. |
| No API Collection menu item to move a nested folder to the root | Move to Root in a nested folder's menu | It gives a path without dragging to the drop zone's action. |
| Error toasts when writing to the clipboard fails | No such toasts | Native clipboard writes cannot fail. |
| No keys in the traffic table | Up, Down, Home, and End move the selection, and Cmd+A (Ctrl+A on Windows and Linux) selects all rows, while the table has the keyboard focus; the detail panel stays as it was | MyGo's list provides them, and they do not conflict with the 1.0.5 click model. |
| Each Python console and SSE message row a tab stop, opened with Enter or Space | Each table one tab stop: the arrow keys move between rows, and Enter opens one | MyGo's table is one focusable list. |
| A WebSocket text message in the conversation opens nothing when clicked | It opens its details, as in the list | It gives the conversation a path to a text message's actions, and conflicts with nothing in 1.0.5. |
| A press on a selected traffic row released on the next row, under the drag threshold, clicks the row pressed | It clicks no row | MyGo leaves such a press to the release, and the table cannot tell it from a press on the scroll bar. |
| Python API help docked beside the content in a workspace 1100 px wide or more, a Slideover in a narrower one | A Slideover at the window's right edge, 420 px wide, at any width | One placement at every size; the Slideover keeps the content's width and closes with Escape or a click beside it. |

## Render review

Each surface has a `TestRender…` test that writes light and dark PNGs at 1600×860 when `FLOWLENS_RENDER_DIR` is set (`desktop/internal/rendertest`):

```sh
FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./desktop/... -run TestRender
```

The renders were compared with `assets/screenshots/flowlens-{light,dark}.png` of the 1.0.5 repository and the values in the `restore-ui-parity` design. Beyond the deliberate differences above, the remaining differences are:

- Font rendering. The headless tester draws with its bundled font, which is wider than Segoe UI, SF Pro, or Noto Sans, and has no CJK glyphs; Chinese layouts were checked by tests rather than by eye.
- Process icons are blank in renders, because the tests have no icon cache. Rows otherwise match.
- The capture render shows the table without the detail panel; `capture-detail` shows it open beside the inspected row, as Ctrl-click leaves it. The test backend serves no bodies, so its body tab is empty. The detail panel has its own renders (`detail-headers`).
- Native scroll bars, as listed above.

The `restore-ui-behavior-parity` renders (`capture-detail`, `sse-detail-formatted`, `ws-detail-receive-text`, `ws-detail-send-binary`, `python-console`, `python-console-detail`, `menu-highlight`, `menu-history`, `tab-menu`, `settings-root-ca-add`, and, after its review, `sse-list`, `ws-list`, and `ws-conversation`) match the 1.0.5 templates and the `restore-ui-behavior-parity` design, except:

- The Python console and SSE message tables' headers have no elevated background, and their rows no line between them: MyGo's `ui.Table` draws its own header and rows.
- The console's Output column fills the room the others leave, at least 240 px rather than 1.0.5's 260 px. Once the user resizes it, it keeps that width rather than filling the rest.
- WebSocket text messages and console entries show in a read-only code view with line numbers, where 1.0.5 had a plain text area.
- Menu renders draw the menus in the window (`SetMenuPresentation`); the app shows the system's menus, which still need the desktop pass.

Interaction that renders cannot show (focus, wheel scrolling of strips, grid editing with an input method, drag feedback, and notifications) is covered by Tester cases, and still needs a desktop pass on each platform.

## Behavior review

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
- Update, Memory Statistics, and Reset Startup.
- Shell, status bar, and title bar.

A review of the change found that two of these still differed and restored them: the SSE message list of the body pane, and the WebSocket message rows and bubbles of the detail panel and the WebSocket Client.

## Tooltips

Every `UTooltip` and `AppTooltip` in the 1.0.5 templates (`frontend/src`), with the locale keys its text uses. Where the text is chosen at run time, every key it can show is listed. Rows marked *dynamic* show data rather than copy.

`TestTooltipInventoryKeysHaveTooltipCallSites` in `desktop/internal/parity` reads the keys in this table and fails unless each one's accessor is referenced from Go code that builds a tooltip. Keep the table in step when a tooltip is added or removed.

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
| `common/websocket/WebSocketMessageStream.vue` | Direction filter | `workspace.websocket_client.filter_send`, `workspace.websocket_client.filter_receive` |
| `common/websocket/WebSocketMessageStream.vue` | Clear | `workspace.websocket_client.clear_messages` |
| `common/websocket/WebSocketMessageStream.vue` | View mode | `workspace.websocket_client.view_mode_list`, `workspace.websocket_client.view_mode_conversation` |
| `modal/WebSocketMessageDetailModal.vue` | Wrap, copy, save | `detail.wrap_body`, `detail.copy_body`, `detail.save_body` |
| `python-plugins/PythonPluginRulesPane.vue` | Delete rule | `python_plugins.delete_rule` |
| `python-plugins/PythonPluginSurface.vue` | Save file (with binding) | `python_plugins.save_file` |
| `python-plugins/PythonPluginSurface.vue` | Validate, reload | `python_plugins.validate_hint`, `python_plugins.reload_hint` |
| `python-plugins/PythonPluginSurface.vue` | Plugin actions | `python_plugins.rename_plugin`, `python_plugins.open_directory`, `python_plugins.delete_plugin` |
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
| `settings/CACertificateInfoPanel.vue` | Trust action | `settings.ca_trust_loading`, `settings.ca_trust_save_first`, `settings.ca_trust_status_unavailable` |
| `settings/CACertificateInfoPanel.vue` | Generate | `settings.ca_save_first`, `settings.ca_trust_uninstall_first` |
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
