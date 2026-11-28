# FlowLens User Guide

This guide covers day-to-day workflows in FlowLens. For installation and building from source, see the [README](../README.md).

Use the sidebar to switch workbench pages. Drag its divider to resize it, or click the active navigation icon to collapse it. Settings open in a separate window; **Settings > General** controls interface and code font sizes.

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

- **Start Proxy** begins forwarding and recording.
- **Pause** stops recording new traffic while forwarding continues. **Resume** records new traffic in the same session; skipped traffic is not recovered.
- Choose **Stop Proxy** from the button's dropdown to stop forwarding.

Previously admitted requests, downloads, SSE streams, and WebSocket connections continue recording to completion while paused. Requests and connections started while paused stay unrecorded; each HTTP request on a reused connection gets its own decision. Existing records remain available to inspect, save, and export.

Clear/restart-capture actions and configuration-driven proxy restarts preserve pause. An explicit Stop followed by Start enables recording, and pause is not saved across application restarts. **Toggle Proxy** shortcuts still start/stop forwarding; **Toggle Recording** is a separate command with no default binding. Configure both under **Settings > Shortcuts**.

### MITM Certificates

HTTPS decryption requires a CA trusted by the client. Configure it in **Settings > Proxy**, add it to the client's trust store, and restart the proxy when prompted.

- **Built-in CA:** Enabled on new installations. FlowLens generates a pair only when both managed files are absent and reuses valid files. Incomplete, invalid, expired, or unreadable files require manual repair with **Generate CA / Regenerate CA**; Settings remains accessible.
- **Custom CA:** Disable **Use built-in CA certificate** and select both a certificate and private key. FlowLens never generates or overwrites custom files. Switching modes preserves custom paths and does not change trust. Upgrades retain earlier paths in custom mode.

#### Current-User Trust on Windows and macOS

Save CA mode/path changes, then use the shield button to install or uninstall the configured CA. Installation validates the certificate and matching private key but installs only the public certificate. Applications with separate trust stores need their own setup.

| Platform | Trust location and behavior |
| --- | --- |
| Windows | Current user's **Trusted Root Certification Authorities**, without a confirmation dialog. Machine-wide certificates are neither shown nor removed. |
| macOS | Login keychain, trusted for `ssl` and `basic`. Authentication may be required. Canceling can leave an **installation incomplete** state; uninstall before retrying. No System keychain changes or `sudo`. |
| Linux | Manage trust manually. |

Uninstall removes only the exact certificate and user trust record, keeping files on disk. It works with an expired certificate or missing private key, and on macOS can remove orphaned trust records. Uninstall installed or incomplete trust before regenerating. FlowLens never replaces trust automatically or removes older CAs. If trust status cannot be read, reopen **Settings > Proxy** to retry.

### Managed System Proxy

The toolbar's system-proxy control temporarily points operating-system proxy settings at the active FlowLens listener:

- **Windows:** Supports HTTP/HTTPS. SOCKS5 is disabled because WinINet clients do not reliably support a SOCKS system proxy.
- **macOS:** Supports HTTP/HTTPS and SOCKS5.
- **Linux:** Not offered.
- **Bind address:** A listener bound to `ALL` (`0.0.0.0`) configures the system proxy as `127.0.0.1`.

FlowLens snapshots the original system proxy settings in memory and restores them on normal shutdown. If you or another application modifies the system proxy in the meantime, FlowLens leaves the newer configuration untouched. After a crash, forced termination, or power loss, restore the system proxy manually in OS network settings—restarting FlowLens cannot recover a lost in-memory snapshot.

### DNS Cache

Click the globe button beside the HTTP cache control, enable caching, set an expiration of `1`–`3600` seconds, and click **Apply**. Defaults are disabled and 60 seconds; settings persist across restarts.

Capture, Request Editor, resend, and WebSocket connections share up to 1,024 successful domain lookups. The duration is FlowLens's expiration period, not the DNS record TTL. An upstream proxy may resolve destinations itself.

**Clear cache**, disabling, or changing the duration evicts entries without interrupting active connections. Stopping capture keeps the cache; quitting releases it. These controls do not affect OS/upstream caches or update checks.

## Inspect Traffic

### Traffic Tables

- **Columns:** Drag dividers to resize. Widths, order, and visibility save automatically. Path initially fills available space; manually resized columns keep their widths, with horizontal scrolling when needed.
- **Selection:** Click selects a row; `Ctrl`-click (`Cmd` on macOS) toggles it; `Shift`-click selects a range. With table focus, use arrows to move, `Shift`+arrows to extend, and `Primary+A` to select all.
- **Details:** Double-click without modifiers to toggle details, or use `Enter` / **Show** to open them. Changing selection updates an open panel. **Close details** preserves selection and returns focus to the table; deselecting the last row closes details.
- **Long values:** Shortened cells end in `…`. Copy or inspect the record for full values; search, sorting, and exports use the complete record.
- **Cleanup:** Clearing/restarting capture releases its view caches. Closing details or changing records releases the previous viewer; reopening reloads it. Stopping the proxy keeps captured records. See [memory use](#check-flowlens-memory-use) for delayed OS memory recovery.
- **Exports:** Capture/history menus export all or selected entries as CSV/HAR. Row menus also save individual requests, responses, headers, bodies, or combined exchanges.

### Categories

Filter traffic by domain, process, or URL structure. Multiple filters can remain active, including filters hidden by sidebar search.

Use `Tab` to enter lists, `Up`/`Down` to move, and `Enter`/`Space` to toggle filters. In the URL tree, `Left`/`Right` collapses or expands branches. Right-click or use `Menu` / `Shift+F10` for request actions.

Leaving or collapsing Categories releases its derived lists and tree. Search, filters, and valid expansion/scroll choices survive for the same source. Closing or replacing a history clears its source-specific navigation.

### Bodies

- **Text and Raw:** Show the full body with search, selection, copy, save, and optional wrapping. At 512 Ki characters total or 128 Ki characters on one line, syntax highlighting and bracket matching are disabled.
- **Binary:** Raw and Fmt are hidden. Images open in Image; other binary content opens in Hex. Unavailable tabs switch automatically; text-based formats such as SVG retain Raw.
- **Images:** Preview PNG, JPEG, GIF, WebP, BMP, SVG, AVIF, and ICO. Animation shows a still frame; ICO previews one stored image up to 256×256 px. Undecodable images remain available in Hex (and Raw for text). Saving preserves original bytes.
- **Hex:** Renders visible rows only, supporting large bodies without pagination.

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

The session list builds visible rows as you scroll, so large lists do not render
every session at once. Use `Tab` to reach the list, arrow keys or `Home`/`End` to
navigate, and `Enter` to open a session. Right-click or `Menu`/`Shift+F10` opens
the target session's actions; a menu closes when its row leaves view.

All rows in an open history remain searchable and sortable using their complete
values. Clearing filters or sorting releases unused working buffers, and clearing
a large selection releases its extra identity storage. If a replacement history
cannot load, the previous history remains available. These releases make memory
reclaimable; they do not force an immediate return of process memory to the OS.

History details show complete headers and metadata before loading bodies. Open **Raw**, **Body**, or **WebSocket** when you need payload content; other detail tabs do not wait for it. When switching rows, the previous view stays briefly (up to 180 ms) until the new details are ready, so quick reads do not flash blank content or a spinner. Its content actions are disabled during the switch, while close stays available. Slower reads show loading feedback in the same layout. Raw copying becomes available after the payload loads.

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

Use a request tab’s menu to save it into an API Collection folder. The dialog remembers the last folder; saving a linked tab updates its existing entry. Saved requests preserve transport settings, ordered headers, body, and Python script source. Script enable switches default to off when reopened.

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

- **Patterns:** Match the full URL. Scheme/host are case-insensitive; path/query preserve case and encoding. Each `*` captures text, `\*` matches a literal star, and `?` is literal. Redirect targets use `${1}`, `${2}`, etc., `$$` for `$`, and an absolute HTTP/HTTPS URL with the complete query. Redirects change routing without a `302`; **Keep current Host** preserves Host.
- **Header/query edits:** Run in row order. **Add** appends; **Set** replaces the first match and removes duplicates, or appends if absent; **Delete** removes all matches. Header names are case-insensitive; query names are case-sensitive.
- **Order:** Clear the search filter and drag rules to reorder; release saves, `Esc` cancels. Rules match the original request once and run top to bottom in each phase. Saved changes affect new requests; in-flight requests keep their snapshot.
- **Drafts:** **Save** (`Primary+S`) commits; **Revert** restores saved content. Switching rules preserves drafts. Enable switches and ordering save immediately. Resolve conflicts before overwriting. **Delete** permanently removes the rule and unsaved edits; failed operations preserve drafts. Quit/restart prompts offer save, discard, or cancel; forced exits lose drafts.
- **Bodies:** Literal or Go regex replacement supports UTF-8 text (`$1`, `$$`). Each stage is limited to 8 MiB and each body phase to 10 seconds, with additional regex bounds. Streaming, binary, non-UTF-8, and bodyless responses reject edits. **Any rewrite failure aborts the exchange.**
- **Preview:** Choose **Regex replace**, enter a sample, and click **Preview**. Edits refresh the match count and inline diff; the pencil returns to editing. Copy uses the sample or result, and is disabled while pending/failed. Large inputs show plain results. Preview sends no traffic and saves neither rules nor samples.
- **Startup:** Editing and proxy startup wait for rules to load. A load failure requires fixing the error and restarting. Unsupported rules remain visible but do not run.

Capture, history, and exports record rewritten traffic. See [HTTP Rewrite Rules](technical/http-rewrite-rules.md) for exact matching, preview, and processing limits.

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
- History file pairs (`.hidx` index and `.hbin` data) and `api_collections/files`: Saved histories and managed request-body files.
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
- Automatic restart triggers only after installation succeeds and still honors unsaved settings, editor tabs, and rewrite-rule drafts. Canceling a quit guard keeps FlowLens running—reopen **Software Update** from the status bar to restart later. Failed restarts can be retried without re-downloading. **Restart later** keeps the current version running until the next launch; it does not postpone installation.
- Development builds and non-writable installations show **Open release page** for manual installation.

## Security

- Use FlowLens only on traffic and systems you own or are authorized to inspect.
- Listening on `ALL` (`0.0.0.0`) exposes the proxy to reachable network devices; apply appropriate firewall and network controls.
- Anyone possessing your MITM CA private key can impersonate sites that trust that CA. Protect it carefully and remove or regenerate it when no longer needed.
- Captures, history, exports, API Collections, logs, cached bodies, and process metadata can contain sensitive secrets.
- Python plugins run as trusted local code with your user permissions; interpreter isolation (`-I`) is not a security sandbox.

Report security vulnerabilities through [SECURITY.md](../SECURITY.md), never in a public issue.
