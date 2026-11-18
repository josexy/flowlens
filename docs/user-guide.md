# FlowLens User Guide

This guide covers day-to-day use of FlowLens. For installation, start with the [README](../README.md).

Capture, history, categories, API Collections, rewrite rules, Python plugins, and memory statistics share one workbench window. Settings open in a separate window.

## Set Up Capture

Start the proxy from the toolbar and configure your client to use it. Defaults:

| Setting | Default |
| --- | --- |
| Mode | HTTP |
| Host | `127.0.0.1` |
| Port | `8080` |
| Process attribution | Enabled |
| Upstream proxy | System Proxy |
| Use built-in CA certificate | Enabled for new installations |

Changing the listener, proxy mode, or CA may require restarting the running proxy. Listening on `ALL` (`0.0.0.0`) exposes the proxy to every device that can reach this machine.

Upstream proxy modes: `No Proxy` sends traffic directly, `System Proxy` uses the operating-system or environment proxy, and `Custom Proxy` uses the configured URL.

### MITM Certificates

HTTPS capture requires a CA certificate that the client application trusts. Inspect the selected CA under `Settings > Proxy`, add it to the trust store the client uses, then restart the proxy when prompted.

**Built-in CA.** New installations enable **Use built-in CA certificate**. FlowLens manages the certificate and private key internally without showing their paths. It checks them at startup and when you switch to built-in mode, generating a pair only if both files are absent. Existing certificates are reused. Incomplete, invalid, expired, or unreadable files are left intact for repair with **Generate CA / Regenerate CA**; these failures do not prevent opening Settings.

**Custom CA.** Turn the switch off to select your own certificate and private key; both are required to start the proxy. Generation and regeneration are disabled in this mode, so FlowLens never overwrites your files. Switching modes keeps your custom paths and does not install, uninstall, move, or replace certificates. Installations upgraded from earlier versions keep their previous paths in custom mode.

**Trust on Windows and macOS.** The shield button beside **Generate CA / Regenerate CA** installs or uninstalls the saved CA for the **current user**. Save changes to the CA mode or file paths first; other settings can stay unsaved. Installation requires a valid CA certificate with a matching private key, and installs only the public certificate. The status reflects the current user's store; some applications use their own trust stores.

- **Windows** installs into the current user's **Trusted Root Certification Authorities** store without a confirmation dialog. Certificates inherited from the local computer store are not shown or removed.
- **macOS** uses `/usr/bin/security` to add the certificate to the **login keychain** and trust it for the `ssl` and `basic` policies. macOS may ask for authentication; FlowLens does not use `sudo` or change the System keychain. Canceling authentication can leave the certificate imported without trust, shown as **installation incomplete**; uninstall it before retrying. An orphaned trust record can be removed even if its certificate is gone.

Uninstall removes only the exact configured certificate and its user trust settings, and leaves the certificate and key files in place. It stays available when the private key is missing or the certificate has expired. Uninstall an installed or incomplete CA before regenerating it. FlowLens does not replace trust automatically or clean up older CAs. Linux, machine-wide trust, and application-specific stores need manual management. If the store cannot be queried, the button stays disabled until you reopen Proxy settings.

### Managed System Proxy

The toolbar's system-proxy control temporarily points the operating-system proxy settings at the active FlowLens listener.

- Windows supports HTTP/HTTPS. SOCKS5 is disabled because WinINet clients do not reliably support a SOCKS system proxy.
- macOS supports HTTP/HTTPS and SOCKS5.
- Linux does not offer this control.
- A listener on `ALL` (`0.0.0.0`) is configured as `127.0.0.1`.

FlowLens keeps the original settings in memory and restores them on a normal shutdown. If another application or the user changes the proxy in the meantime, FlowLens leaves that newer configuration alone. A crash, forced termination, or power loss can leave the system proxy pointing at FlowLens; restore it manually in the operating-system network settings, because restarting FlowLens cannot recover the lost snapshot.

### DNS Cache

The globe control beside the HTTP cache control opens the DNS cache settings. Enable the cache, choose a duration from 1 to 3600 seconds, and select **Apply**. It is off by default with a 60-second duration, and the setting persists across restarts.

- Capture, Request Editor, resend, and WebSocket connections share up to 1024 cached domains. Only successful lookups are cached.
- The duration is FlowLens's own expiration period, not the DNS record's TTL. When an upstream proxy resolves the destination, its resolver is used instead.
- **Clear cache** removes records without interrupting existing HTTP, HTTP/2, or WebSocket connections; new connections resolve again. Disabling the cache or changing its duration also clears it.
- Stopping capture keeps the cache for the Request Editor; quitting FlowLens releases it.
- These controls do not clear operating-system or upstream DNS caches, and do not affect update checks.

## Inspect Traffic

### Traffic Tables

In capture and history tables, a click selects one row, Ctrl-click (Cmd-click on macOS) adds or removes a row, and Shift-click selects a range. With the table focused, the arrow keys move the selection, Shift+arrows extend it, and `Primary+A` selects all.

Enter, a double-click, or the row menu's **Show** opens the detail panel. **Close details** hides it, keeps the selection, and returns focus to the table. Changing the selection updates an open panel but does not reopen a closed one.

Clearing or restarting a capture releases its detail-view, table, filter, and category caches, including hidden views. Closing details or selecting another record releases the previous record's viewer caches; opening details again reloads the selected record. Stopping the proxy keeps the captured records available. Memory can decrease after garbage collection; the Go `Sys` statistic also includes address space whose physical memory has already been returned to the operating system.

Capture, history, and row context menus export all or selected entries as CSV or HAR. Row menus can also export a request, response, headers, body, or the combined exchange.

### Categories

Categories filter traffic by domain, process, and URL structure; the three groups share one scrollbar. Tab reaches each group and its list. Up/Down moves between entries and Enter or Space toggles the highlighted filter. Several filters can stay selected, including ones hidden by the sidebar search. In the structure tree, Left/Right collapses or expands branches, and Enter or Space toggles a branch or selects a request. Right-click a request, or press the Menu key or Shift+F10, for its context menu.

### Bodies

- **Text and Raw** viewers show the complete content without pagination; search, selection, copy, and save cover all of it. Wrapping can be toggled at any size. Documents of 512K characters or more, or with a line of 128K characters or more, are shown as plain text without syntax highlighting or bracket matching.
- **Images**: PNG, JPEG, GIF, WebP, BMP, SVG, and AVIF (including transparency) are previewed. Animated images show a still frame. Unsupported or undecodable images stay available in Raw and Hex, and saving keeps the original bytes.
- **Hex** renders only the visible rows, so large bodies are not paged.

**SSE** responses (`text/event-stream`) open on the **SSE** tab in the Request Editor and in capture and history details. The list shows complete events in order with their type, effective ID, and a data preview; drag a column divider to resize it. Click a row to see its full data or original event block, with a formatted view for small JSON messages. Scrolling up pauses following new messages; **Jump to latest message** resumes it.

- Heartbeats and control-only blocks appear only in the **Raw** tab.
- An event without a terminating blank line is incomplete and is not listed, even after the stream stops.
- Encoded binary bodies cannot be parsed as SSE; save them or use the Hex view.
- The response toolbar copies or saves the entire response; the message dialog copies or saves only its selected view. The list is rebuilt from the body and adds no per-message timestamps.

### Timing and Sizes

The Timing panel shows the request and response timeline measured at the upstream connection, plus the DNS, TCP connect, and TLS handshake durations of the connection that carried it. Connection phases are shared by requests that reuse the connection and are not added to the request's Total Duration. Unavailable or incomplete values show `—`. A DNS cache hit shows a DNS duration of `0`.

Header sizes count the text shown in the Raw panel; for HTTP/2 they exclude HPACK compression and frame overhead. Body sizes count bytes as transferred, before decompression. See [Traffic Metrics and HAR](technical/traffic-metrics-and-har.md) for the exact definitions.

### Process Attribution

Process attribution is enabled by default under `Settings > Proxy`. It applies only to direct local connections; remote clients are marked as remote and skipped.

- The traffic list shows the app icon and name when available. Details include process name, PID, app ID, executable path, and lookup status.
- Lookup never delays a connection. Text details may appear before the icon.
- Process details are saved with history; icons are loaded separately and cached under `cache/process-icons`.
- Missing permissions or metadata show a placeholder without interrupting capture.

## History and HAR

Saved capture sessions appear under History and survive restarts. Only the open history's decoded entries and bodies are kept in memory: opening another history replaces them, and closing the history tab frees them, including detail-view caches, and returns to the capture. FlowLens may retain compact indexes for up to four recently accessed histories. Reopening a history shows the same entries.

**Import.** Use **Import HAR** beside the History refresh button, or drop `.har` files onto the capture or history table. Files are imported one at a time, each into a new history named after the file; importing the same file again creates another history. The first imported history opens in the history tab. The result reports imported files and entries, skipped entries, unavailable bodies, and failed files. Canceling the file picker does nothing.

- HAR 1.2 and the common fields of 1.1 are supported, including files with a UTF-8 BOM.
- Limits: 512 MiB per file, 32 MiB per entry, and 100,000 entries per file.
- Invalid JSON, cancellation, or a write failure rolls back that file; invalid entries are skipped.
- An imported history's creation date is the import date; requests keep their original timestamps. The source file is not modified or copied.
- A request whose body is missing from the archive cannot be resent, edited, or copied as a complete request.

**Export.** HTTP/HTTPS exchanges and WebSocket handshakes export as HAR 1.2; raw TCP tunnels and WebSocket frames are skipped. Entries whose cached body is missing are still exported and counted in the result. Re-exporting an imported history does not reproduce every original HAR field.

**Format.** New histories use HBIN v3, which needs FlowLens 2.0.0 or later. HBIN v1 and v2 histories still open; v1 histories show connection timings as `—`. Files in unknown formats are skipped without being deleted.

HAR files and other exports are not redacted. They can contain authorization headers, cookies, bodies, and process paths; review them before sharing.

## Request Editor and WebSocket Client

Open a new HTTP Request Editor or WebSocket Client tab with the **+** button in the tab bar, edit a captured request from its row menu, or open a saved request from API Collections.

- **Proxy**: `No Proxy`, `System Proxy`, `MITM Proxy`, or `Custom Proxy`.
- **Protocol**: automatic negotiation, or force `HTTP/1.1` or `HTTP/2`.
- **Max Redirects** (`Settings` tab): defaults to `0`, which follows no redirects. The initial request does not count: `1` allows A → B and returns B's response even if it redirects again. Reaching the limit returns the last response without an error. The request timeout covers the whole chain.
- **TLS profile**: HTTPS and WSS requests can use a Go, Chrome, Firefox, Safari, Edge, iOS, Android 11 OkHttp, or randomized-ALPN ClientHello.
- **Skip TLS Verification** (`Settings` tab): per tab and off by default. It covers the client's own TLS connections, including HTTPS proxies; a MITM proxy still verifies upstream certificates according to its own settings.
- **HTTP/2 fingerprint**: a four-part `SETTINGS|WINDOW_UPDATE|PRIORITY|PSEUDO_HEADER_ORDER` value. Requests forced to HTTP/1.1 ignore it.

Header rows are sent in the order shown. The URL decides where the request goes; FlowLens sets pseudo-headers, `Host`, body framing, generated content types, and the fallback `User-Agent`. A header layout the selected protocol cannot send exactly is rejected rather than reordered.

In key/value grids, Tab reaches editable cells and Enter or Space opens one with its text selected. Enter commits without a line break; Tab or Shift+Tab commits and moves on; Escape restores the original value; clicking elsewhere also commits. Confirming input-method composition keeps the editor open.

The WebSocket Client lists messages; the arrow button cycles between all, sent, and received messages. **Clear Messages** empties the list without closing the connection.

The HTTP Request Editor can run Python hooks; see [Python Plugins](#python-plugins).

## API Collections

Save an HTTP Request Editor or WebSocket Client tab into a folder from its tab menu. Pick the destination in the dialog's folder tree, which opens at the last selected folder and shows the chosen folder's path below it. Saving a tab that is already linked to a collection entry updates that entry. Saved requests keep their protocol, redirect limit, TLS verification setting, TLS profile, HTTP/2 fingerprint, ordered headers, body, and Python script source. Script switches are off when a request is reopened.

**Organize.** **New Folder** in the toolbar and **New Child Folder** in a folder's menu open a name dialog; press Enter or **Create** to save. Drag a row or its grip onto a folder to move it; hovering over a collapsed folder expands it, and holding near the edge scrolls. Nested folders can also move to the top level with **Move to Root**, by menu or by dropping them on the drop zone; requests must stay inside a folder. Press Escape to cancel a drag.

**Keyboard.** Tab enters the tree. Up/Down moves the highlight, Left/Right collapses or expands folders, and Enter or Space selects a folder or opens a request. Clicking a folder toggles it; its arrow only expands or collapses. In multi-select mode, a click or Space toggles a row's selection and Shift selects a range; selecting a folder does not select its children. Right-click a row, or press the Menu key or Shift+F10, for its actions.

## Rewrite Rules

**Rewrite Rules** in the sidebar redirect URLs and edit headers, query parameters, and UTF-8 bodies. They apply only to live HTTP/HTTPS proxy traffic; the Request Editor, resend, imported history, WebSocket, CONNECT tunnels, and raw TCP are not affected.

To redirect an endpoint:

1. Create and name a rule; select `GET` or `ALL`.
2. Set the URL pattern to `https://api.example.com/users/*?token=*` and the **Redirect** target to `https://test.example.com/accounts/${1}?token=${2}`.
3. Use **Match test** to check the result without sending a request.
4. Save, enable the rule, and turn on the master switch. New rules start disabled.

**Patterns** match the complete URL: scheme and host ignore case; path and query keep case and encoding. Each `*` captures text, `\*` matches a literal star, and `?` is literal. Targets use `${1}`, `${2}`, and so on for captures and `$$` for `$`, and must be absolute HTTP/HTTPS URLs including the full query. A redirect changes where the request goes without returning a 302; **Keep current Host** keeps the original Host header.

**Header and query operations** run in row order: **Add** appends; **Set** replaces the first match and removes duplicates, or adds if absent; **Delete** removes all matches. Header names ignore case; query names do not.

- **Order**: drag saved rules to reorder them; the new order saves on release. Clear the search before reordering; Escape cancels. All rules match the original request, and each phase runs them from top to bottom. Saved changes affect new requests; requests in flight keep the rules they started with.
- **Drafts**: **Save** or `Primary+S` saves and **Revert** restores the saved rule. Switching rules keeps drafts; switches and ordering save immediately. Resolve external conflicts before overwriting. Deleting a rule offers **Cancel** or **Delete**, and Delete permanently removes the rule and its unsaved edits. Quitting or restarting for an update offers save, discard, or cancel. A failed save or delete keeps the draft; a forced exit loses it.
- **Bodies**: replace UTF-8 text directly or with Go regular expressions (`$1`, `$$`). Each stage is limited to **8 MiB** and each body phase to **10 seconds**, with additional regex limits. Streaming, binary, non-UTF-8, and bodyless responses (HEAD, 204, 304) reject body edits. **A rewrite failure interrupts the exchange.**
- **Regex preview**: select **Regex replace**, paste a sample under **Match test**, and click **Preview** for the match count and an inline diff. Edits refresh the preview; the pencil returns to the sample. Copy uses the sample while editing and the result while previewing, and is disabled for pending or failed results. Large text shows the full result without a diff or wrapping. Previews send no requests, save no rules, and do not keep the sample.
- **Startup**: the rule page and the proxy wait for rules to load. A loading error blocks editing and starting the proxy; fix the error and restart. Unsupported saved rules stay visible but do not run.

Capture, history, and exports record the rewritten content. See [HTTP Rewrite Rules](technical/http-rewrite-rules.md) for protocol details and resource limits.

## Python Plugins

The HTTP Request Editor can run global Python plugins and a script for the current request. Configure an interpreter under `Settings > Python`, then see the [Python plugin guide](technical/python-plugins.md) for setup, the API, limits, and examples.

In **Python Plugins**, the file toolbar creates, renames, and deletes a plugin's files. New files never replace an existing one, renaming keeps unsaved edits, and deleting discards them after confirmation. `main.py` and `manifest.json` can be edited but not renamed or deleted. Dialogs close on success and stay open on failure so you can retry. Drag plugins to reorder them, as with rewrite rules. Open the plugins directory from the sidebar toolbar.

## Keyboard Shortcuts

`Settings > Shortcuts` lets you search commands, record or clear a binding, and restore one or all defaults. `Primary` means Command on macOS and Ctrl on Windows and Linux. Menus and tooltips show your current bindings.

| Command | Default binding |
| --- | --- |
| Save the active request, settings, Python file or parameters, or rewrite rule | `Primary+S` |
| Open settings | `Primary+,` |
| Capture / categories / API Collections / rewrite rules / Python plugins / memory statistics | `Primary+1` through `Primary+6` |
| New HTTP / WebSocket request | `Primary+N` / `Primary+Shift+N` |
| Close the active tab | `Primary+W` |
| Next / previous tab | `Control+Tab` / `Control+Shift+Tab` |
| Send the active request | `Primary+Enter` |
| Focus the capture or history filter | `Primary+F` |

Toggling the proxy or the system proxy, clearing traffic, and showing the main window have no default binding.

- Shortcuts work only while a FlowLens window has focus and the command applies to the current page, tab, or dialog. Text fields and code editors keep their normal keys when no command applies.
- Each command has one single-step binding; chords and alternate bindings are not supported.
- Optional system-wide shortcuts can show the main window and toggle the proxy. They are off by default. The operating system may ask for approval or report a conflict, and Wayland portals may choose the final key.

Tab and Shift+Tab move between sidebar entries, settings sections, and their controls; Enter or Space activates the focused entry. Row checkboxes and switches are separate stops. Hidden sidebars and inactive pages ignore these keys, and code editors keep Tab for indentation.

## Windows and Tray

- Closing the main window hides FlowLens to the system tray; use the tray menu to restore it or quit.
- Only one FlowLens runs per build mode. Starting it again focuses the existing window.
- Window size, position, and maximized state are restored when possible.
- Settings open in their own window and ask before discarding unsaved changes.
- Switching between the custom and native title bar takes effect after a restart. The native title bar follows the FlowLens theme; switch between light, dark, and system appearance from the sidebar.

## Settings, Logs, and Storage

| Page | Contents |
| --- | --- |
| General | Theme colors, interface and code fonts, animations, title bar |
| Proxy | Process attribution, upstream proxy, built-in or custom CA, extra trusted roots, per-host client certificates |
| Python | Interpreter and global Python plugin behavior |
| Shortcuts | Application bindings and optional system-wide shortcuts |
| Logs | Logging state, level, and file actions |
| Storage | Body-cache threshold, WebSocket and history retention, cache cleanup, and storage statistics |

Appearance changes preview in every window immediately; save to keep them or discard to restore the saved look. Animations are on by default and follow the system's reduced-motion preference; turning them off makes transitions and loading indicators static.

### Logs

Logging is enabled at `info` level. The current file is `flowlens.log` in the `logs` directory of the configuration folder; it rotates at 10 MB and keeps five backups. Development builds also log to the console. `Settings > Logs` controls enablement and level, and can clear, refresh, or open the log folder.

If startup fails, FlowLens stays open and shows the failed step and its error. **Retry** starts again, **Open Log Folder** shows the logs, and **Quit** exits. The failure is always logged, even when logging is turned off.

Logs can contain hosts, paths, API Collection names, and local file paths. Review them before sharing.

### Local Storage

FlowLens keeps `flowlens.db`, history, API Collection files (`api_collections/files`), logs, and caches (including `cache/process-icons`) in the operating system's user configuration directory. On Unix-like systems, its directories are owner-only (`0700`) and its database, history, log, and body-cache files are `0600`. Windows relies on the permissions of the user's configuration directory.

Bodies cached during a capture session are removed once they are no longer needed: after **Save and Restart** or **Discard and Restart**, after the final save when FlowLens quits, and at the next start if FlowLens did not quit cleanly or the final save failed. Saved histories and cached process icons stay.

Storage cleanup removes either caches alone, or caches together with the current capture and readable saved history. Cache-only cleanup first saves a non-empty current capture to history. History files in unsupported or unknown formats are kept. Cleanup never deletes `flowlens.db`, API Collections, preferences, certificates, or logs.

## Security

- Use FlowLens only for traffic you own or are authorized to inspect.
- Listening on `ALL` exposes the proxy to reachable devices; apply suitable firewall and network controls.
- Anyone with the CA private key can impersonate sites that trust the CA. Keep it private, and remove or regenerate it when no longer needed.
- Captures, history, exports, API Collections, logs, cached bodies, and process details may contain sensitive data.
- Python plugins run as trusted local code. Interpreter isolation is not a security sandbox.

Report vulnerabilities as described in [SECURITY.md](../SECURITY.md), not in a public issue.
