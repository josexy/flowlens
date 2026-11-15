# AGENTS.md

Engineering guidelines for FlowLens. Product usage belongs in [README.md](README.md) and [docs/](docs/user-guide.md).

## Workflow

FlowLens uses Go 1.27.3+, MyGo native UI, `nuxt-ui-go` components, Task, SQLite, `mitmproxy-go`, and `xhttp`. Python hooks use external CPython 3, without an embedded interpreter or Python cgo dependency.

- Before multi-step work, define the change, verification, and completion criteria. Check effects on persistence/files, networking/models, Python Workers, process attribution, events/shortcuts, windows, copy, and docs.
- If more than two plausible interpretations would materially change the result, explain them first; otherwise follow existing behavior and proceed.
- Keep changes within the owning module. Preserve unrelated workspace edits; avoid incidental formatting, parallel implementations, and one-off abstractions.
- Follow existing Go service/package boundaries. Reuse `internal/desktop/store` state and `nui` base controls.
- Views run on the main thread and rebuild every frame from Go state. Never store a `*ui.Context` or `Element`. Workers use `runtime.Run` and never touch view state.

## Module Ownership

| Owner | Responsibility |
| --- | --- |
| `main.go` | Delegation to `internal/app` |
| `internal/app` | Application assembly, database startup, windows/tray, single instance, events, update engines, and shutdown |
| `internal/serviceruntime` | Service startup and shutdown, lifecycle interfaces, and the in-process event bus |
| `internal/services/app_service` | App/window APIs, environment info, and authoritative update state |
| `internal/services/proxy_service` | Capture, synthetic requests, WebSocket, resend, metrics, HBIN, shared HAR/traffic export, and managed system-proxy integration |
| `internal/services/history_service` | History reads/deletion/resend and import/export entry points; reuse proxy codecs and writers |
| `internal/services/python_plugin_service` | Plugin registration/revisions/rules, Workers, protocol, SDK, request hooks, and execution output |
| `internal/services/api_collection_service` | SQLite repository, tree transactions, and managed request-body files |
| `internal/services/{setting_service,logging_service,shortcut_service}` | Settings and CA trust, logging, and system shortcuts respectively |
| `internal/pkg/{process_attribution,systemproxy}` | Platform process/icon lookup and system-proxy control |
| `internal/pkg/{body_cache,body_spool,database,logger}` | Body cache, Python body temp files, shared database, and logging |
| `internal/desktop/store` | Shared interface state, on the main thread; reuse its stores |
| `internal/desktop/logic` | Pure rules: formatting, headers, cookies, filters, hex, table state |
| `internal/desktop/runtime` | Main-thread dispatcher and cancelable background jobs |
| `internal/desktop/{shell,capture,inspect,editor,collection,rewrite,plugins,settings,update,memstats,resetstartup}` | Main window frame and each surface or window |
| `internal/desktop/widgets` | Shared controls, including `ProcessIcon` and its cache |
| `internal/desktop/shortcuts` | Command catalog, bindings/conflicts, recording, and per-window dispatch |
| `internal/desktop/theme`, `internal/desktop/locale`, `locales/` | Appearance, language, and message catalogs |

Take the single-instance lock before opening SQLite so a second process exits before database contention. Python contracts/examples live in `docs/technical/python-plugins*.md` and `docs/examples/python-plugins`.

## High-Risk Contracts

### Networking, History, and Export

- Proxy/editor HTTP uses `github.com/josexy/xhttp`, including its HTTP/2 support; do not mix standard-library HTTP types or another HTTP/2 implementation.
- External headers/trailers remain `[]HTTPHeaderField`. Preserve raw HeaderBlock order, duplicates, casing, empty values, and truncation. Without raw data, use normalized fields and mark wire order unavailable; never substitute a map.
- Keep request normalization in `synthetic_request_headers.go`: URL controls routing; the proxy service owns pseudo-headers, `Host`, framing, generated content type, and fallback UA. Reject HeaderOrder the selected transport cannot represent losslessly.
- Keep synthetic transport/shared TLS dialing in `synthetic_transport.go`. Explicit HTTP/1.1 must not apply HTTP/2 fingerprints; redirects retain fingerprint semantics. Protocol, proxy, and fingerprint settings must round-trip through API Collection.
- Live `HTTPMessageMetrics` comes only from transport-boundary events, with microsecond precision and retry/capture-generation isolation. HAR import may map valid file statistics. Unknown numbers use `-1`; failed, canceled, or incomplete bodies must not gain fabricated completion values.
- `HeaderSize` is the UTF-8 size of the complete Raw-panel head, including start line, field lines, and terminating blank line. HTTP/2 uses synthetic start lines and displayed pseudo-header conversion, excluding TCP/TLS, HPACK, and frame overhead. The interface and HAR read it directly. `BodySize` counts entity bytes after transport encoding; traffic and certificate timestamps use Unix microseconds.
- Write HBIN v2 and read v1/v2; earlier development layouts remain unsupported. Skip unknown versions without deleting them. Traffic/model changes require codec, history/HAR tests, and `internal/desktop/logic`/view updates together.
- Reuse proxy-service `HARFileWriter` and shared traffic-export writers with streamed atomic writes. Distinguish empty bodies from missing payloads, retain other exportable HAR entries, and report skipped/missing-body counts. Exports do not automatically redact credentials, cookies, bodies, or process paths.
- Interface export goes through `internal/desktop/capture/export.go`, preserving source, ID order, capture generation, extensions, concurrency guards, and result statistics. Capture/history menus expose CSV/HAR; traffic-row menus expose individual message formats too.
- API Collection transactions and managed body files must remain consistent through rollback, orphan cleanup, and startup validation.

### Python Plugins

- Hooks run only in HTTP Request Editor, excluding ordinary MITM capture, resend, and WebSocket Client. Plugins are trusted code; `-I` isolates imports, not execution.
- Workers use versioned length-prefixed JSON with bounded frames, source, bodies, parameters, shared state, output, and hook time. Timeout, cancellation, crash, or corruption terminates/replaces the Worker; Windows also cleans up its child process tree.
- Snapshot matching order, revisions, and parameters before send. Request order is global plugins → current script → network; responses reverse it. Requests fail closed; responses fail open. `context.shared` is isolated per plugin and shared only between its request/response hooks.
- Separate user-facing body semantics from storage. Edited requests/ordinary responses over 4 MiB use `body_spool`; never put file bytes in Worker JSON or expose/persist temp paths.
- Clean managed temp files after every outcome and Worker exit. SSE hooks may change only state/headers before response start; no body/trailer edits or spool files.
- Persist global plugin metadata/rules/parameters/effective revisions in SQLite plus the managed directory. API Collection may save current script source; bypass/enabled switches belong to the current tab and default off when reopened.
- Complete output belongs only in the request-editor console with its execution ID; do not add global log rings or plugin-workbench log history.

### Process Attribution and Icons

- Lookup is asynchronous and must not block proxy connections. Only direct local connections participate; mark and skip remote clients.
- Identify processes by PID + `StartToken`; bound queue, TTL, capacity, and query timeout. Late results must revalidate the connection and record.
- Icon cleanup coordinates Manager rotation/shutdown, pending-task cancellation, stale `IconKey` invalidation, disk deletion, and the interface's cache. Missing disk files must remain recoverable on demand.
- Reuse `widgets.ProcessIcon`; views must not call `GetProcessIcon` directly or create another cache.
- `ProcessInfo` changes require proxy models, HBIN codec/tests, history, and interface updates together.

### Settings, Certificates, and Lifecycle

- Settings defaults, validation, partitioned serialization, and SQLite persistence belong to `setting_service`; logs go through `internal/pkg/logger`/`logging_service`.
- Ordinary settings use `UpdatePreservingShortcuts`. Preserve newer shortcut, Python-runtime, and traffic-detail state from narrow writes; layout/table preferences use `SaveTrafficDetailConfig`/`SaveTrafficTableConfig`, serialized with full saves and published only after persistence succeeds.
- CA trust stays in `setting_service`, serialized with generation and guarded by the expected certificate fingerprint. Install only the public certificate into the current user's Windows root store or macOS login keychain/user trust; validate CA, validity, and key pair first.
- CA status must distinguish unknown/incomplete/installed states. Uninstall only the exact certificate/trust record, including expired certificates or missing keys. Remove existing/incomplete trust before regeneration; do not add automatic replacement, historical-CA cleanup, or machine-wide trust.
- Managed system proxy uses `proxy_service` and `internal/pkg/systemproxy`. Preserve the original snapshot and restore on normal shutdown only if the managed configuration still matches; never overwrite newer external changes.
- Updates use `app_service` snapshots/revisions and `store.UpdaterStore` across windows. Keep one operation active, ignore stale events, and cancel/wait during shutdown. Install only updates signed with the built-in key, and retain the manual path through the release page. Restart must use the quit guards and normal shutdown even after the update window closes.
- Save every function `EventBus.On` returns and call it when the listener goes away.

### Shortcuts

- System shortcuts belong only to `shortcut_service`: allowlist, register before persist, rollback on failure, and cleanup on shutdown. Changes use `ApplyShortcutConfig`.
- In-app commands go through `internal/desktop/shortcuts`; buttons/menus/shortcuts share business functions. Do not add parallel key handlers in views.
- `When` checks the real active page/tab. Text fields and code editors retain their keys unless a command explicitly allows them.
- Display resolved bindings; `primary` means Command on macOS and Ctrl elsewhere. Missing override uses the default; `binding: null` explicitly disables it.

### Interface and Copy

- Prefer `nui` components, theme colors, and `lucide` icons. Select values cannot be empty strings; use a local sentinel.
- Reuse `internal/desktop/logic` for ordered header editing/copying, degradation hints, dates, microsecond times, durations, and sizes.
- Reuse `internal/desktop/theme` and the settings store for fonts/colors/layout. Check light/dark themes and affected windows; tray/title-bar/visibility changes must cover `internal/app`, `internal/desktop/shell`, settings, and locales together.
- Keep hex viewing in `internal/desktop/inspect/hex.go` and `internal/desktop/logic/hexdump.go`; preserve byte-based selection/copy/export and bounded rendering for large bodies.
- Put copy in both `locales/{en,zh-Hans}.json` with matching keys and placeholders, then run `task i18n:generate`. Generated accessors take arguments in alphabetical order of placeholder name. Keep persistent copy short, preserve distinct lifecycles, and retain delete/overwrite/cancel/restart/truncation/order-degradation semantics.

## Verification

Use the Go version from `go.mod`. Task is recommended; Python 3 is optional outside hook tests. Windows and Linux use `CGO_ENABLED=0`, macOS `CGO_ENABLED=1`; the Taskfile sets this.

Run the smallest checks matching the risk. Full verification is `task verify`, which needs no display. Root tasks: `task dev`, `task package`, `task run`. Without Task, run `go tool mygo dev`.

| Scope | Relevant Go test packages |
| --- | --- |
| SQLite/settings/API Collection | `./internal/pkg/database ./internal/services/setting_service ./internal/services/api_collection_service` |
| Shortcuts | `./internal/services/setting_service ./internal/services/shortcut_service ./internal/desktop/shortcuts` |
| Metrics/body/HBIN/HAR/traffic export | `./internal/services/proxy_service ./internal/services/history_service ./internal/desktop/capture ./internal/desktop/logic` |
| Process attribution | `./internal/pkg/process_attribution ./internal/services/proxy_service ./internal/services/history_service ./internal/services/setting_service` |
| Python hooks | `./internal/services/python_plugin_service ./internal/services/proxy_service ./internal/services/setting_service ./internal/desktop/plugins` |
| CA trust/appearance/layout | `./internal/services/setting_service ./internal/desktop/settings ./internal/desktop/theme` |
| Updates/windows/shutdown | `./internal/app ./internal/services/app_service ./internal/desktop/update ./internal/desktop/shell` |
| Request editor | `./internal/desktop/editor ./internal/desktop/store` |
| Rewrite rules | `./internal/services/rewrite_service ./internal/desktop/rewrite ./internal/desktop/store` |

Prefix package lists with `go test`. For concurrency/lifecycle changes, also run `CGO_ENABLED=1 go test -race ./internal/desktop/... ./internal/pkg/process_attribution ./internal/services/proxy_service` when supported. Worker/SDK/interpreter/package changes must actually run Python 3 integration tests, without skips.

View changes run under `ui.NewTester` with no display. After changing views, run `task vet:ui`.

## Docs, Releases, and Completion

- Python entry-point/scope/hook/order/limit/security/console changes update both technical guides, examples, README, and `internal/desktop/pythonapi/data`.
- Use English Conventional Commits: `<type>(<scope>): <summary>`, one class of change per commit. Include only verified in-scope files and consistent bilingual copy.
- Release metadata uses `task version VERSION=1.2.3`; add `CHECK=true` to verify without writing. Each release needs a `## <version>` section in `CHANGELOG.md`, which becomes its update notes. See [Building and Packaging](docs/technical/building.md).
- macOS must keep `CGO_ENABLED=1`; `cgo_guard_darwin.go` fails a production build without it. MyGo upgrades go through `go.mod`, which pins the CLI.
- Report the completed result, checks run, and unresolved risks. If verification is unavailable, state why; feasibility alone is not completion.
