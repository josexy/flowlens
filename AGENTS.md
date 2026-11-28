# AGENTS.md

Engineering rules for FlowLens: where code belongs, contracts that are easy to break, and how to verify changes. Usage lives in [README.md](README.md) and the [user guide](docs/user-guide.md), specified behavior in `openspec/specs/`, and protocol, limit, and format details in `docs/technical/`.

## Workflow

Stack: Go (version in `go.mod`), MyGo native UI, `nuxt-ui-go` (`nui`), Task, SQLite, `mitmproxy-go`, and `xhttp`. Python hooks use an external CPython 3, with no embedded interpreter or Python cgo.

- Before multi-step work, define the change, its verification, and completion criteria. Check effects on persistence/files, networking/models, Python Workers, process attribution, events/shortcuts, windows, copy, and docs.
- If more than two plausible interpretations would materially change the result, explain them first; otherwise follow existing behavior.
- Stay within the owning module and reuse `internal/desktop/store` state and `nui` controls. Preserve unrelated edits; no incidental formatting, parallel implementations, or one-off abstractions.
- Views run on the main thread and rebuild every frame from Go state: never store a `*ui.Context` or `Element`. Background work uses `runtime.Run` and never touches view state.

## Module Ownership

| Owner | Responsibility |
| --- | --- |
| `internal/app` (`main.go` only delegates) | Assembly, database startup, windows/tray, single instance, events, update engines, shutdown |
| `internal/serviceruntime` | Service lifecycle and typed event topics |
| `internal/services/app_service` | App/window APIs, environment info, authoritative update state |
| `internal/services/proxy_service` | Capture, synthetic requests, WebSocket, resend, metrics, HBIN, shared HAR/traffic export, managed system proxy |
| `internal/services/history_service` | History reads/deletion/resend and import/export entry points, reusing proxy codecs and writers |
| `internal/services/python_plugin_service` | Plugin registry/revisions/rules, Workers, protocol, SDK, hooks, execution output |
| `internal/services/api_collection_service` | SQLite repository, tree transactions, managed request-body files |
| `internal/services/rewrite_service` | Rewrite-rule persistence, validation, compiled matchers, published snapshots |
| `internal/services/{setting,logging,shortcut,mem_stats}_service` | Settings and CA trust, logging, system shortcuts, and memory sampling, respectively |
| `internal/pkg/` | `process_attribution`, `systemproxy`, `body_cache`, `body_spool` (Python body temp files), `database`, `logger` |
| `internal/desktop/store` | Shared interface state on the main thread |
| `internal/desktop/logic` | Pure rules: formatting, headers, cookies, filters, hex, table state |
| `internal/desktop/runtime` | Main-thread dispatcher and cancelable background jobs |
| `internal/desktop/widgets` | Shared controls, including `ProcessIcon` and its cache |
| `internal/desktop/shortcuts` | Command catalog, bindings/conflicts, recording, per-window dispatch |
| `internal/desktop/pythonapi` | Python API help, its copy, and copied system prompts |
| `internal/desktop/theme`, `internal/desktop/locale`, `locales/` | Appearance, language, message catalogs |
| Other `internal/desktop/*` | `shell` (main window frame) and one package per surface or window |
| `_website/` | Optional Astro/TypeScript homepage and download navigation; the leading underscore keeps it out of Go's `./...`, and it is outside `task verify` |

## High-Risk Contracts

### Networking, History, and Export

Timing, size, HAR, and HBIN semantics are defined in [traffic-metrics-and-har.md](docs/technical/traffic-metrics-and-har.md); keep it in step with the code.

- HTTP goes through `github.com/josexy/xhttp`, including its HTTP/2; never mix in standard-library HTTP types or another HTTP/2 implementation.
- Headers/trailers are ordered `[]HTTPHeaderField`, never maps. Preserve raw order, duplicates, casing, empty values, and truncation; with only normalized fields, mark wire order unavailable.
- `synthetic_request_headers.go` owns request normalization: the URL controls routing, and the proxy service owns pseudo-headers, `Host`, framing, generated content type, and fallback UA. Reject a HeaderOrder the transport cannot represent losslessly.
- `synthetic_transport.go` owns synthetic transport and TLS dialing: no HTTP/2 fingerprints on explicit HTTP/1.1, fingerprints survive redirects, and protocol/proxy/fingerprint settings round-trip through API Collection.
- Metrics come only from transport-boundary events, isolated per retry and capture generation. Unknown values are `-1`; never fabricate completion values. Traffic and certificate timestamps are Unix microseconds.
- Write HBIN v3, read v1–v3, and skip unknown versions without deleting them. Traffic-model changes update the codec, history/HAR tests, and `internal/desktop/logic`/views together.
- Exports reuse the proxy service's `HARFileWriter` and traffic-export writers (streamed, atomic), keep empty and missing bodies distinct, report skipped counts, and never redact. Interface export goes through `internal/desktop/capture/export.go`, which owns source, ordering, capture generation, and concurrency guards.
- API Collection transactions and managed body files stay consistent through rollback, orphan cleanup, and startup validation.

### Python Plugins

Scopes, ordering, failure handling, limits, and security are specified in [python-plugins.md](docs/technical/python-plugins.md), with examples in `docs/examples/python-plugins`.

- Hooks run only in HTTP Request Editor, never in MITM capture, resend, or WebSocket Client. Plugins are trusted code; `-I` isolates imports, not execution.
- Workers speak versioned, length-prefixed, bounded JSON. Timeout, cancellation, crash, or corruption terminates and replaces the Worker, including its child process tree on Windows.
- Snapshot matches, revisions, and parameters before send. Requests run global plugins → current script → network and fail closed; responses run in reverse and fail open.
- Edited requests and ordinary responses over 4 MiB use `body_spool`: no file bytes in Worker JSON, no exposed or persisted temp paths, and cleanup after every outcome and Worker exit. SSE hooks change only status and headers.
- Global plugins persist in SQLite plus the managed directory. Current-script enable/bypass switches belong to the tab and default off.
- Output goes only to the request-editor console under its execution ID; no global or workbench log history.

### Process Attribution and Icons

- Lookup is asynchronous, never blocks proxy connections, and covers only direct local connections; mark and skip remote clients.
- Identify processes by PID + `StartToken` with a bounded queue, TTL, capacity, and timeout. Late results revalidate the connection and record.
- Icon cleanup coordinates Manager rotation/shutdown, pending-task cancellation, stale `IconKey` invalidation, disk deletion, and the interface cache; missing files stay recoverable. Views use only `widgets.ProcessIcon`.
- `ProcessInfo` changes update proxy models, the HBIN codec and tests, history, and the interface together.

### Settings, Certificates, and Lifecycle

- `setting_service` owns settings defaults, validation, serialization, and persistence. Ordinary writes use `UpdatePreservingShortcuts`, which keeps newer shortcut, Python-runtime, and traffic-detail state. Layout/table preferences use `SaveTrafficDetailConfig`/`SaveTrafficTableConfig`, serialized with full saves and published only after persisting. Logs go through `internal/pkg/logger`.
- CA trust is serialized by generation and guarded by the expected certificate fingerprint. Validate the CA, validity, and key pair, then install only the public certificate into the current user's trust store. Status distinguishes unknown, incomplete, and installed. Uninstall only the exact certificate/trust record, even when expired or keyless, and remove existing trust before regenerating. No automatic replacement, historical-CA cleanup, or machine-wide trust.
- The managed system proxy restores the original snapshot on normal shutdown only if its configuration still matches.
- Updates keep one active operation through `app_service` revisions and `store.UpdaterStore`, ignore stale events, cancel/wait on shutdown, install only updates signed with the built-in key, and keep the manual release-page path. Restart uses the quit guards and normal shutdown, even after the update window closes.
- Take the single-instance lock before opening SQLite.
- Call every function `Topic.Subscribe` returns when its listener goes away; subscribers never block.

### Shortcuts

- System shortcuts belong to `shortcut_service` (allowlist, register before persisting, roll back on failure, clean up on shutdown) and change through `ApplyShortcutConfig`.
- In-app commands go through `internal/desktop/shortcuts` and share business functions with buttons and menus; no key handlers in views. `When` checks the real active page/tab; text fields and code editors keep their keys unless a command opts in.
- Show resolved bindings (`primary` is Command on macOS, Ctrl elsewhere). No override means the default; `binding: null` disables the command.

### Interface and Copy

- Prefer `nui` components, theme colors, and `lucide` icons. Select values cannot be empty strings; use a local sentinel.
- Reuse `internal/desktop/logic` for header editing/copying, dates, times, durations, and sizes, and `internal/desktop/theme` plus the settings store for fonts, colors, and layout. Check light and dark themes in every affected window.
- Window roots call `theme.Begin`, which applies Windows text scaling once; never scale text again.
- Page, pane, grid, card, tab, and window-frame borders use `nui.Theme.Border`, not MyGo's `Theme.Border`; controls and selection/focus/error/drop indicators keep semantic colors.
- Tray, title-bar, and visibility changes update `internal/app`, `internal/desktop/shell`, settings, and locales together.
- Hex viewing stays in `internal/desktop/inspect/hex.go` and `internal/desktop/logic/hexdump.go`: byte-based and bounded for large bodies.
- Add copy to both `locales/{en,zh-Hans}.json` with matching keys and placeholders, then run `task i18n:generate`; accessor arguments follow alphabetical placeholder order. Keep copy short and preserve delete/overwrite/cancel/restart/truncation/order-degradation semantics.

## Verification

Run the smallest checks that match the risk; `task verify` is the full check and needs no display. The Taskfile sets `CGO_ENABLED=0` for ordinary checks on every platform. Optional race checks have separate toolchain requirements.

| Scope | `go test` packages |
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

- Add tests to the existing file for the same feature; create a test file only for a distinct feature, shared fixture or benchmark set, or platform build constraint.
- Worker/SDK/interpreter changes run the Python 3 integration tests without skips.
- View changes run `task vet:ui`. To inspect renders, run `FLOWLENS_RENDER_DIR=/tmp/flowlens-renders go test ./internal/desktop/... -run TestRender -count=1`.
- Website changes run `task website:check website:build` (Node.js 24). The application's checks never need Node.js.

## Docs, Releases, and Completion

- Update docs with behavior: the user guide stays task-oriented, and details go in `docs/technical/` (such as `traffic-metrics-and-har.md` and `http-rewrite-rules.md`). Python hook/scope/order/limit/security/console changes also update both plugin guides, the examples, README, and `internal/desktop/pythonapi/data`.
- Change specified behavior through an OpenSpec change, archived once implemented.
- Commits are English Conventional Commits, `<type>(<scope>): <summary>`, one class of change each, with only verified in-scope files.
- Every `git commit` message should describe the actual changes as concretely as possible. For fixes, explain the problem and how the change resolves it; use the commit body when the summary cannot capture the relevant details.
- The version lives only in `mygo.json`; releases need a matching tag and a `## <version>` section in `CHANGELOG.md`. All ordinary builds keep `CGO_ENABLED=0`. macOS packaging keeps the 13.0 bundle and Mach-O deployment metadata (`MYGO_LDFLAGS=-macos=13.0`); runtime acceptance still requires native tests on both architectures. See [Building and Packaging](docs/technical/building.md).
- `_website/src/lib/release.ts` and its tests match release asset names exactly as the `check-assets` job requires them; change both together.
- Report the result, checks run, and unresolved risks. If verification was unavailable, say why; feasibility alone is not completion.
