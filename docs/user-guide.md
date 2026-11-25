# FlowLens User Guide

This guide covers day-to-day workflows in FlowLens. For installation and building from source, see the [README](../README.md).

Capture, history, categories, API Collections, rewrite rules, Python plugins, and memory statistics share a single workbench window; settings open in a separate window. The interface uses compact controls, toolbars, and table rows to maximize visible content. Drag the sidebar divider to resize the sidebar, or click the active navigation icon to collapse it. Interface and code font sizes can be adjusted independently in **Settings > General**.

## Contents

- [Set Up Capture](#set-up-capture)
- [Inspect Traffic](#inspect-traffic)
- [History and HAR](#history-and-har)
- [Request Editor and WebSocket Client](#request-editor-and-websocket-client)
- [API Collections](#api-collections)
- [Rewrite Rules](#rewrite-rules)
- [Python Plugins](#python-plugins)
- [Keyboard Shortcuts](#keyboard-shortcuts)
- [Windows and Tray](#windows-and-tray)
- [Settings, Logs, and Storage](#settings-logs-and-storage)
- [Update FlowLens](#update-flowlens)
- [Security](#security)

## Set Up Capture

Start the proxy from the toolbar and point your client to its listener address.

| Setting | Default |
| --- | --- |
| Mode | `HTTP` |
| Host | `127.0.0.1` |
| Port | `8080` |
| Process attribution | Enabled |
| Upstream proxy | `System Proxy` |
| Use built-in CA certificate | Enabled on new installations |

Changing the listener address, proxy mode, or CA certificate may require restarting a running proxy. Listening on `ALL` (`0.0.0.0`) exposes the proxy to every device that can reach this machine.

Upstream proxy modes:

- **`No Proxy`:** Sends traffic directly to upstream targets.
- **`System Proxy`:** Uses the operating-system or environment proxy configuration.
- **`Custom Proxy`:** Routes upstream traffic through the configured proxy URL.

### Pause Recording Without Disconnecting Clients

The toolbar provides a split proxy button sized to its active labels:

- **Start Proxy** begins forwarding and recording traffic.
- While running, **Pause** stops recording new traffic without stopping the listener or changing the system proxy. The status bar keeps showing the listener address, and the button switches to **Resume**.
- **Resume** collects new traffic in the same capture session; traffic skipped while paused is not recovered.
- To stop forwarding completely, open the button's dropdown menu and choose **Stop Proxy**.

Recording behavior while paused:

- Requests and connections admitted before pausing continue recording to completion (including downloads, SSE streams, and active WebSocket connections), so existing rows can still update while paused.
- Requests and WebSocket/TCP connections started while paused stay unrecorded even if they finish after resuming. Each new HTTP request on a reused connection receives an independent recording decision.
- Existing records remain inspectable, savable, and exportable.
- Clearing traffic, restarting the capture session, and configuration-driven proxy restarts preserve the paused state. An explicit **Stop Proxy** followed by **Start Proxy** re-enables recording, and pause state is not persisted across application restarts.
- The **Stop Proxy** menu item intentionally displays no shortcut hint (view or customize bindings in **Settings > Shortcuts**). Existing **Toggle Proxy** shortcuts keep their start/stop behavior, and **Settings > Shortcuts** provides a separate **Toggle Recording** command with no default binding.

### MITM Certificates

Decrypting HTTPS traffic requires a CA certificate trusted by the client application. Inspect or configure the CA under **Settings > Proxy**, add it to your client's trust store, and restart the proxy when prompted.

- **Built-in CA:** Enabled by default on new installations via **Use built-in CA certificate**. FlowLens manages the certificate and private key internally without exposing file paths. It checks them on startup and when switching to built-in mode, generating a new pair only if both files are absent. Existing valid certificates are reused; incomplete, invalid, expired, or unreadable files are left intact for manual repair via **Generate CA / Regenerate CA** without blocking access to Settings.
- **Custom CA:** Disable **Use built-in CA certificate** to select your own certificate and private key (both are required to start the proxy). Generation and regeneration are disabled in custom mode, so FlowLens never overwrites your files. Switching modes preserves custom paths and never installs, uninstalls, moves, or replaces certificates. Upgrades from earlier versions retain their previous paths in custom mode.

#### Current-User Trust on Windows and macOS

The shield button beside **Generate CA / Regenerate CA** installs or uninstalls the saved CA for the **current user**. Save any changes to the CA mode or file paths first (other settings may remain unsaved). Installation requires a valid CA certificate paired with its matching private key and installs only the public certificate. The status reflects the current user's trust store; some applications maintain separate trust stores.

- **Windows:** Installs into the current user's **Trusted Root Certification Authorities** store without a confirmation dialog. Certificates inherited from the local computer store are neither shown nor removed.
- **macOS:** Uses `/usr/bin/security` to add the certificate to the **login keychain** and trust it for the `ssl` and `basic` policies. macOS may prompt for authentication; FlowLens never uses `sudo` or modifies the System keychain. Canceling authentication can leave the certificate imported without trust (**installation incomplete**)—uninstall it before retrying. Orphaned trust records can still be removed even if the certificate is gone.
- **Uninstall and limits:** Uninstalling removes only the exact configured certificate and its user trust record while leaving the certificate and key files on disk, and remains available even if the private key is missing or the certificate is expired. Uninstall an installed or incomplete CA before regenerating it. FlowLens never automatically replaces trust or cleans up older CAs. Linux, machine-wide trust, and application-specific stores require manual management. If the trust store cannot be queried, the button stays disabled until you reopen **Settings > Proxy**.

### Managed System Proxy

The toolbar's system-proxy control temporarily points operating-system proxy settings at the active FlowLens listener:

- **Windows:** Supports HTTP/HTTPS. SOCKS5 is disabled because WinINet clients do not reliably support a SOCKS system proxy.
- **macOS:** Supports HTTP/HTTPS and SOCKS5.
- **Linux:** Not offered.
- **Bind address:** A listener bound to `ALL` (`0.0.0.0`) configures the system proxy as `127.0.0.1`.

FlowLens snapshots the original system proxy settings in memory and restores them on normal shutdown. If you or another application modifies the system proxy in the meantime, FlowLens leaves the newer configuration untouched. After a crash, forced termination, or power loss, restore the system proxy manually in OS network settings—restarting FlowLens cannot recover a lost in-memory snapshot.

### DNS Cache

Click the globe button beside the HTTP cache control to configure the shared DNS cache. Enable it, set an expiration duration between `1` and `3600` seconds, and click **Apply**. The cache defaults to disabled with a 60-second duration and persists across restarts.

- Capture, Request Editor, resend, and WebSocket connections share up to `1,024` cached domains. Only successful lookups are cached.
- The duration is FlowLens's own expiration period rather than the DNS record TTL. When an upstream proxy resolves the destination host, the upstream proxy's resolver is used instead.
- **Clear cache** evicts cached entries without interrupting active HTTP, HTTP/2, or WebSocket connections; subsequent new connections resolve freshly. Disabling the cache or changing its duration also clears it.
- Stopping capture retains the cache for the Request Editor; quitting FlowLens releases it.
- These controls do not clear OS or upstream DNS caches and do not affect update checks.

## Inspect Traffic

### Traffic Tables

- **Columns:** Drag column dividers to resize columns; widths, column order, and hidden columns persist automatically. **Path** initially fills remaining space, while manually dragged widths stay fixed when the sidebar or window resizes. Extra space after fixed columns keeps the row background, and narrow tables scroll horizontally.
- **Selection:** Click selects one row, `Ctrl`-click (`Cmd`-click on macOS) toggles a row, and `Shift`-click selects a range. When the table has focus, arrow keys move selection, `Shift` + arrow keys extend it, and `Primary+A` selects all rows.
- **Details panel:** Double-click a row without modifiers to toggle the detail panel, or press `Enter` / choose **Show** from the row menu to open it. **Close details** hides the panel, keeps the current selection, and returns focus to the table. `Ctrl`/`Cmd`-click only modifies selection (deselecting the last row closes details). Changing selection updates an open panel without reopening a closed one.
- **Memory cleanup:** Clearing or restarting a capture releases its detail-view, table, filter, and category caches (including hidden views). Closing details or selecting another record releases the previous record's viewer caches; reopening details reloads the selected record. Stopping the proxy preserves captured records. Freed memory returns to the OS gradually; see [Check FlowLens Memory Use](#check-flowlens-memory-use).
- **Exports:** Capture, history, and row context menus export all or selected entries to CSV or HAR. Row menus also export individual requests, responses, headers, bodies, or combined exchanges.

### Categories

The **Categories** sidebar filters traffic by domain, process, and URL structure within a single shared scroll area:

- Press `Tab` to navigate between groups and lists, `Up`/`Down` to highlight entries, and `Enter` or `Space` to toggle filters. Multiple filters can stay active simultaneously, including ones hidden by the sidebar search.
- In the URL structure tree, `Left`/`Right` collapses or expands branches, and `Enter` or `Space` toggles a branch or selects a request.
- Right-click a request (or press the `Menu` key / `Shift+F10`) to open its context menu.

### Bodies

- **Text and Raw:** Render the full body without pagination so search, selection, copy, and save cover the entire content. Line wrapping can be toggled at any size. Bodies of `524,288` characters (`512 Ki`) or more—or containing any line of `131,072` characters (`128 Ki`) or more—render as plain text without syntax highlighting or bracket matching.
- **Binary bodies:** Hide **Raw** and **Fmt** because binary bytes cannot be displayed as text. Images open in **Image**; other binary bodies without a specialized viewer open in **Hex**. Switching between bodies automatically switches away from an unavailable tab. Text bodies (including SVG) retain **Raw**.
- **Images:** Preview PNG, JPEG, GIF, WebP, BMP, SVG, AVIF, and ICO (with transparency). Animated images show a still frame; ICO files preview one stored image (PNG or DIB representations up to 256×256 px). Unsupported or undecodable images remain viewable in **Hex** (and **Raw** if text-based). Saving always preserves the original bytes.
- **Hex:** Virtualizes rendering to visible rows so large bodies load without pagination.

#### Server-Sent Events (SSE)

`text/event-stream` responses open on the **SSE** tab in the Request Editor and in capture/history details:

- Complete events appear in arrival order with their event type, effective ID, and a data preview (drag column dividers to resize). Click any row to inspect the full data or original event block, with formatted JSON for small JSON payloads.
- Scrolling up pauses auto-scrolling; click **Jump to latest message** to resume following new events.
- Heartbeats and control-only blocks appear only in the **Raw** tab.
- Incomplete events lacking a terminating blank line are omitted, even after the stream closes.
- Encoded binary bodies cannot be parsed as SSE; save them or inspect them in **Hex**.
- The response toolbar copies or saves the full response, whereas the message dialog copies or saves only its selected view. The event list is reconstructed from the response body and adds no per-message timestamps.

### Timing and Sizes

The **Timing** panel reports the request and response timeline measured at the upstream transport boundary, along with DNS, TCP connect, and TLS handshake durations for the underlying connection:

- Connection-setup phases are shared across requests that reuse a connection and are not added to an individual request's **Total Duration**.
- Unavailable or incomplete values display `—`.
- A shared DNS cache hit reports a DNS duration of `0`.
- Header sizes measure the UTF-8 text displayed in the **Raw** panel (excluding HPACK compression and HTTP/2 frame overhead). Body sizes measure bytes as transferred on the wire before decompression.

See [Traffic Metrics and HAR](technical/traffic-metrics-and-har.md) for exact definitions.

### Process Attribution

Process attribution is enabled by default under **Settings > Proxy** and applies only to direct local connections (remote clients are marked as remote and skipped).

- The traffic list shows each process's icon and name when available; the detail panel includes process name, PID, app ID, executable path, and lookup status.
- Lookups run asynchronously and never delay proxy connections, so text metadata may appear before the icon finishes loading.
- Process metadata is saved with history; icons are loaded separately and cached under `cache/process-icons`.
- Missing permissions or unavailable metadata display a placeholder without interrupting capture.

## History and HAR

Saved capture sessions appear under **History** and persist across restarts. Only the currently open history's decoded entries and loaded bodies stay in memory: opening another history replaces them, and closing the history tab releases them (including detail-view caches) and returns to live capture. FlowLens retains compact indexes for up to four recently accessed histories so reopening them stays fast.

### Importing HAR Files

Click **Import HAR** beside the History refresh button, or drag and drop `.har` files onto the capture or history table:

- Files import one by one, each into a new history named after the file (re-importing the same file creates another history). The first imported history opens automatically.
- The import summary reports imported files and entries, skipped entries, unavailable bodies, and failed files. Canceling the file picker makes no changes.
- Supports HAR 1.2 and common HAR 1.1 fields, including UTF-8 BOM files.
- Limits: `512 MiB` per file, `32 MiB` per entry, and `100,000` entries per file.
- Invalid JSON, cancellation, or write errors roll back that file; invalid individual entries are skipped.
- The history's creation date reflects the import date while individual requests preserve their original timestamps. Source files are never modified or copied.
- Requests whose bodies were absent from the HAR archive cannot be resent, edited, or copied as complete requests.

### Exporting and HBIN Format

- **HAR export:** HTTP/HTTPS exchanges and WebSocket handshakes export as HAR 1.2; raw TCP tunnels and WebSocket frames are skipped. Entries with missing cached bodies still export and are counted in the summary. Re-exporting an imported history does not reproduce every original HAR field.
- **HBIN format:** New histories write HBIN v3 (requiring FlowLens 2.0.0+). HBIN v1 and v2 histories still open (v1 histories show `—` for connection timings). Files with unknown format versions are skipped without being deleted.
- **Privacy warning:** HAR files and other exports are never redacted—they can include authorization headers, cookies, payloads, and local process paths. Review them before sharing.

## Request Editor and WebSocket Client

Open a new **HTTP Request Editor** or **WebSocket Client** tab using the **+** button in the tab bar, edit a captured request from its row menu, or open a saved request from **API Collections**.

### Request Transport Settings

- **Proxy:** `No Proxy`, `System Proxy`, `MITM Proxy`, or `Custom Proxy`.
- **Protocol:** Automatic negotiation, or forced `HTTP/1.1` or `HTTP/2`.
- **Max Redirects** (**Settings** tab): Defaults to `0` (do not follow redirects). The initial request does not count—`1` follows `A → B` and returns `B`'s response even if `B` redirects again. Reaching the limit returns the last response without an error, and the request timeout covers the entire redirect chain.
- **TLS profile:** HTTPS and WSS requests support Go, Chrome, Firefox, Safari, Edge, iOS, Android 11 OkHttp, and randomized-ALPN ClientHello profiles.
- **Skip TLS Verification** (**Settings** tab): Per-tab and off by default. Applies to the client's own TLS connections (including HTTPS proxies); a MITM proxy still verifies upstream certificates according to its own settings.
- **HTTP/2 fingerprint:** Four-part `SETTINGS|WINDOW_UPDATE|PRIORITY|PSEUDO_HEADER_ORDER` string (ignored when `HTTP/1.1` is forced).

### Headers, Key/Value Grids, and WebSocket Controls

- **Header order:** Headers are sent in the exact order shown. The URL controls routing, while FlowLens manages pseudo-headers, `Host`, body framing, generated `Content-Type`, and fallback `User-Agent`. A header layout that the selected protocol cannot emit losslessly is rejected rather than silently reordered.
- **Key/value grid editing:** Press `Tab` to move between editable cells and `Enter` or `Space` to start editing with text selected. `Enter` commits without inserting a newline, `Tab`/`Shift+Tab` commits and advances, `Esc` reverts, and clicking outside commits. Confirming an IME composition keeps the cell editor open.
- **WebSocket Client:** Lists sent and received frames; click the direction button to cycle between all, sent, and received messages. **Clear Messages** empties the list without closing the connection.
- **Python scripting:** The HTTP Request Editor supports global plugins and per-request scripts; see [Python Plugins](#python-plugins).

## API Collections

Save an HTTP Request Editor or WebSocket Client tab into a folder from its tab menu. Choose the destination in the folder tree dialog (which opens at the last selected folder and displays its full path below). Saving a tab already linked to an entry updates that entry. Saved requests preserve protocol, redirect limit, TLS verification, TLS profile, HTTP/2 fingerprint, ordered headers, body, and Python script source—script enable switches always default to off when reopened.

- **Request types:** HTTP entries show a braces (`{}`) icon and their method (such as `GET` or `POST`). WebSocket entries show a chat icon and `WS`.
- **Organize:** **New Folder** (toolbar) and **New Child Folder** (folder menu) open a name dialog (`Enter` or **Create** saves). Drag a row or its grip onto a folder to move it (hovering over a collapsed folder expands it; dragging near the edges scrolls). Move nested folders to the top level with **Move to Root** via the menu or root drop zone (requests must live inside a folder). Press `Esc` to cancel a drag.
- **Keyboard navigation:** Press `Tab` to enter the tree, `Up`/`Down` to move focus, `Left`/`Right` to collapse/expand folders, and `Enter`/`Space` to select a folder or open a request. Clicking a folder toggles it, while its chevron only expands/collapses. In multi-select mode, click or `Space` toggles a row and `Shift` selects a range (selecting a folder does not select its children). Right-click or press `Menu` / `Shift+F10` for row actions.

## Rewrite Rules

**Rewrite Rules** in the sidebar redirect URLs and modify headers, query parameters, and UTF-8 bodies on **live HTTP/HTTPS proxy traffic only**. The Request Editor, resend, imported history, WebSocket, `CONNECT` tunnels, and raw TCP bypass rewrite rules.

### Example: Redirect an Endpoint

1. Create and name a new rule, and choose method `GET` or `ALL`.
2. Set the URL pattern to `https://api.example.com/users/*?token=*` and the **Redirect** target to `https://test.example.com/accounts/${1}?token=${2}`.
3. Use **Match test** to verify the expanded target without sending network traffic.
4. Save the rule, enable it, and turn on the master switch (new rules start disabled).

### Matching, Actions, and Lifecycle

- **URL patterns and redirect targets:** Patterns match the full URL—scheme and host are case-insensitive, while path and query preserve case and encoding. Each `*` captures text, `\*` matches a literal `*`, and `?` is literal. Targets substitute `${1}`, `${2}`, etc. and use `$$` for a literal `$`; targets must be absolute HTTP/HTTPS URLs including the complete query. Redirects change upstream routing without issuing a `302`; **Keep current Host** preserves the existing `Host` header.
- **Header and query operations:** Execute in row order: **Add** appends, **Set** replaces the first match and removes duplicates (or appends if absent), and **Delete** removes all matches. Header names are case-insensitive; query parameter names are case-sensitive.
- **Rule order:** Drag saved rules to reorder them (saved automatically on release; clear the search filter first, and press `Esc` to cancel). All rules match once against the original request and run top to bottom in each phase. Saved edits apply to new requests; in-flight requests retain the rules active when they started.
- **Drafts and saving:** **Save** (`Primary+S`) commits edits and **Revert** restores the saved rule. Switching between rules preserves unsaved drafts; enable switches and reordering save immediately. Resolve external conflicts before overwriting. Deleting a rule offers **Cancel** or **Delete** and permanently removes the rule and its unsaved edits. Quitting or restarting for an update prompts to save, discard, or cancel. Failed saves or deletes retain drafts; forced exits lose unsaved drafts.
- **Body edits:** Replace UTF-8 text literally or with Go regular expressions (`$1`, `$$`). Each stage is capped at `8 MiB` and each body phase at `10` seconds, with additional regex safety bounds. Streaming, binary, non-UTF-8, and bodyless responses (`HEAD`, `204`, `304`) reject body edits. **Any rewrite failure aborts the exchange.**
- **Regex preview:** Select **Regex replace**, paste a sample under **Match test**, and click **Preview** for the match count and an inline diff. Edits refresh the preview automatically; click the edit (pencil) icon to return to the sample. Copy copies the sample while editing or the result while previewing (disabled when pending or failed). Large inputs show the complete plain result without diffing or wrapping. Previews never send network requests, save rules, or persist samples.
- **Startup:** Both the rule page and the proxy wait for rules to load. A load error blocks rule editing and starting the proxy—fix the error and restart. Unsupported saved rules remain visible but do not run.

Capture, history, and exports record rewritten traffic. See [HTTP Rewrite Rules](technical/http-rewrite-rules.md) for protocol semantics and limits.

## Python Plugins

The HTTP Request Editor can run global Python plugins and a per-request script. Configure a Python 3 interpreter under **Settings > Python**, then see the [Python Plugin Guide](technical/python-plugins.md) for setup, the Hook/Body API, limits, and examples.

In the **Python Plugins** workbench, the file toolbar creates, renames, and deletes plugin files:

- New files never overwrite existing ones; renaming keeps unsaved edits; deleting discards them after confirmation.
- `main.py` and `manifest.json` can be edited but cannot be renamed or deleted.
- File dialogs close on success and stay open on failure so you can retry.
- Drag plugins to reorder them (as with rewrite rules), and use the sidebar toolbar to open the managed plugins directory.

## Keyboard Shortcuts

Open **Settings > Shortcuts** to search commands, record or clear bindings, and restore individual or all defaults. `Primary` maps to `Cmd` on macOS and `Ctrl` on Windows and Linux. Menus and tooltips display your active bindings.

| Command | Default Binding |
| --- | --- |
| Save active request, settings, Python file/params, or rewrite rule | `Primary+S` |
| Open Settings | `Primary+,` |
| Switch workbench: Capture / Categories / API Collections / Rewrite Rules / Python Plugins / Memory | `Primary+1` – `Primary+6` |
| New HTTP / WebSocket request | `Primary+N` / `Primary+Shift+N` |
| Close active tab | `Primary+W` |
| Next / previous tab | `Control+Tab` / `Control+Shift+Tab` |
| Send active request | `Primary+Enter` |
| Focus capture or history filter | `Primary+F` |

**Toggle Proxy**, **Toggle Recording**, **Toggle System Proxy**, **Clear Traffic**, and **Show Main Window** have no default binding.

- **Scope:** Application shortcuts trigger only while a FlowLens window is focused and the command applies to the active page, tab, or dialog. Text inputs and code editors retain normal keystrokes unless a command explicitly applies.
- **Single-step bindings:** Each command takes one binding; multi-key chords and alternate bindings are not supported.
- **System-wide shortcuts:** Optional global shortcuts for **Show Main Window** and **Toggle Proxy** are off by default. The OS may request approval or report conflicts, and Wayland portals may select the final key combination.
- **Keyboard navigation:** `Tab` and `Shift+Tab` move across sidebar entries, settings sections, and controls; `Enter` or `Space` activates focused entries. Row checkboxes and switches are separate tab stops. Collapsed sidebars and inactive pages ignore navigation keys, and code editors keep `Tab` for indentation.

## Windows and Tray

- Closing the main window hides FlowLens to the system tray; use the tray menu to restore or quit.
- Only one instance runs per build mode; launching FlowLens again focuses the existing window.
- Window size, position, and maximized state persist across launches when possible.
- Settings open in a dedicated window and prompt before discarding unsaved edits.
- Switching between the custom and native title bar takes effect after restart. The native title bar follows the active theme; switch between light, dark, and system appearance from the sidebar.

## Settings, Logs, and Storage

| Section | Contents |
| --- | --- |
| **General** | Theme colors, UI and code fonts, animations, and title-bar mode |
| **Proxy** | Process attribution, upstream proxy, built-in or custom CA, extra trusted roots, and per-host client certificates |
| **Python** | Interpreter selection, hook timeout, and global plugin master switch |
| **Shortcuts** | In-app bindings and optional system-wide shortcuts |
| **Logs** | Log enablement, level, and log file actions |
| **Storage** | Body-cache threshold, WebSocket and history retention, cache cleanup, and storage usage |

Appearance edits preview across every open window immediately—save to keep them or discard to restore the saved theme. Animations default to on and respect the OS reduced-motion preference; turning them off makes transitions and loading indicators static.

### Logs

Logging defaults to enabled at `info` level. Logs write to `flowlens.log` inside the `logs` directory of your FlowLens configuration folder, rotating at `10 MiB` with up to five backups. Development builds also log to stdout/stderr. Use **Settings > Logs** to toggle logging, change the level, clear logs, refresh, or open the log folder.

If startup fails, FlowLens remains open and displays the failed step and error with **Retry**, **Open Log Folder**, and **Quit**. Startup failures are always logged even when logging is disabled.

Logs can contain hostnames, paths, API Collection names, and local file paths—review them before sharing.

### Local Storage

FlowLens stores all data in the operating system's user configuration directory:

- `flowlens.db`: Settings, rewrite rules, API Collection metadata, and plugin registry.
- History files (`.hbin` pairs) and `api_collections/files`: Saved histories and managed request-body files.
- `logs/` and `cache/` (including `cache/process-icons`): Application logs, session body caches, and process icons.

On Unix-like systems, FlowLens creates directories with mode `0700` and database, history, log, and body-cache files with mode `0600`. Windows relies on the user's configuration directory permissions.

#### Cache Lifecycle and Cleanup

- Capture session body caches are deleted automatically once no longer needed: after **Save and Restart** or **Discard and Restart**, after the final shutdown save succeeds, and on the next launch if a previous exit was unclean or the final save failed. Saved histories and cached process icons remain intact.
- **Storage cleanup** (**Settings > Storage**) removes either caches only, or caches together with the current capture and readable saved histories. Cache-only cleanup archives a non-empty current capture to history first. History files in unsupported/unknown formats are preserved, and cleanup never deletes `flowlens.db`, API Collections, preferences, certificates, or logs.
- Open windows react to cleanup immediately without restarting, even if Settings is closed first: the history list reflects archived or cleared histories, a removed open history closes back to live capture, Request Editor tabs clear references to deleted draft-cache files while keeping the rest of each draft, and process icons reload. If cleanup partially fails, the error appears after windows refresh what succeeded.

### Check FlowLens Memory Use

Open **Memory** from the sidebar (`Primary+6`). Opening it for the first time in a run records one sample. **Start** polls at the selected interval until paused, **Refresh** records one sample immediately, and **Clear** resets the chart until you refresh or start again.

**Memory** reports the FlowLens process's own memory using your operating system's primary process metric (figures can differ from OS tools by one sampling interval and rounding):

| Platform | Equivalent OS Metric |
| --- | --- |
| **Windows** | The **Memory** column on the FlowLens process row in Task Manager (expand the group—the group total also counts Python Workers and other child processes) |
| **macOS** | The **Memory** column in Activity Monitor |
| **Linux** | `RES` in `top`/`htop` (`RSS` in `ps`) |

- **Memory** never includes Python Workers, which run as separate processes. If the OS metric cannot be read, it shows `-` as unavailable.
- **Heap in use** tracks live Go objects (such as captured traffic and caches), rising between garbage collections and falling after each cycle.
- Process memory can stay elevated after clearing traffic or closing a history because Go retains freed pages for reuse and returns them to the OS gradually. Expand **Go runtime details** to inspect total runtime memory, **Kept for reuse**, and **Returned to the OS**. Process memory also includes the native renderer, fonts, and system libraries outside Go's heap. For deep profiling, see [Memory Diagnostics](technical/memory-diagnostics.md).

## Update FlowLens

Open **Software Update** from the status bar and click **Check for updates**. Review the new version and release notes, then click **Install and restart**.

- **Restart automatically when finished** defaults to on for the current session (persisting if you close and reopen the update window); clear it to install now and restart manually later.
- **Hide** or the window close button lets an active update continue in the background. **Cancel** stops a check or an active download; once downloading finishes, installation runs to completion without cancellation. If installation completes before a concurrent cancel takes effect, the update remains ready for restart.
- Automatic restart triggers only after installation succeeds and still honors unsaved settings, editor tabs, and rewrite-rule drafts. Canceling a quit guard keeps FlowLens running—reopen **Software Update** from the status bar to restart later. Failed restarts can be retried without re-downloading, and **Restart later** applies the update on next launch.
- Development builds and non-writable installations show **Open release page** for manual installation.

## Security

- Use FlowLens only on traffic and systems you own or are authorized to inspect.
- Listening on `ALL` (`0.0.0.0`) exposes the proxy to reachable network devices; apply appropriate firewall and network controls.
- Anyone possessing your MITM CA private key can impersonate sites that trust that CA. Protect it carefully and remove or regenerate it when no longer needed.
- Captures, history, exports, API Collections, logs, cached bodies, and process metadata can contain sensitive secrets.
- Python plugins run as trusted local code with your user permissions; interpreter isolation (`-I`) is not a security sandbox.

Report security vulnerabilities through [SECURITY.md](../SECURITY.md), never in a public issue.
