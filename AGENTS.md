# AGENTS.md

Engineering guidelines for FlowLens. Product usage belongs in [README.md](README.md) and [docs/](docs/user-guide.md).

## Workflow

FlowLens uses Wails v3, Go 1.27+, SQLite, `mitmproxy-go`, `xhttp`, Vue 3, TypeScript, Pinia, Nuxt UI v4, and Tailwind CSS v4. Python hooks use external CPython 3.11+, without an embedded interpreter or Python CGO dependency.

- Before multi-step work, define the change, verification, and completion criteria. Check effects on persistence/files, networking/models, Python Workers, process attribution, events/shortcuts, windows, bindings, copy, and docs.
- If more than two plausible interpretations would materially change the result, explain them first; otherwise follow existing behavior and proceed.
- Keep changes within the owning module. Preserve unrelated workspace edits; avoid incidental formatting, parallel implementations, and one-off abstractions.
- Follow existing Go service/package boundaries and Vue `script setup` conventions. Reuse Pinia stores and Nuxt UI base controls.
- Regenerate exported Wails APIs/models with `wails3 generate bindings -ts -i`; include generated changes, consume them through `#bindings/*`, and never edit `frontend/bindings` by hand.

## Module Ownership

| Owner | Responsibility |
| --- | --- |
| `main.go` | Embedded assets and delegation to `backend/app` |
| `backend/app` | Wails assembly, database startup, windows/tray, single instance, events, updater wiring, and shutdown |
| `backend/services/app_service` | App/window APIs, environment info, authoritative update state, and self-update eligibility |
| `backend/services/proxy_service` | Capture, synthetic requests, WebSocket, resend, metrics, HBIN, shared HAR/traffic export, and managed system-proxy integration |
| `backend/services/history_service` | History reads/deletion/resend and import/export entry points; reuse proxy codecs and writers |
| `backend/services/python_plugin_service` | Plugin registration/revisions/rules, Workers, protocol, SDK, request hooks, and execution output |
| `backend/services/api_collection_service` | SQLite repository, tree transactions, and managed request-body files |
| `backend/services/{setting_service,logging_service,shortcut_service}` | Settings and CA trust, logging, and system shortcuts respectively |
| `backend/pkg/{process_attribution,systemproxy}` | Platform process/icon lookup and system-proxy control |
| `backend/pkg/{body_cache,body_spool,database,logger}` | Body cache, Python body temp files, shared database, and logging |
| `frontend/src/stores` | Shared state; reuse `trafficWorkspace`, `apiCollection`, `workbench`, `setting`, `theme`, and `updater` |
| `frontend/src/components/traffic-workspace` | Capture/history/classification, request editor, and WebSocket client |
| `frontend/src/shortcuts` | Command catalog, bindings/conflicts, and single-window dispatch |

Create the Wails application before opening SQLite so the single-instance guard runs before database contention. Python contracts/examples live in `docs/technical/python-plugins*.md` and `docs/examples/python-plugins`.

## High-Risk Contracts

### Networking, History, and Export

- Proxy/editor HTTP uses `github.com/josexy/xhttp`, including its HTTP/2 support; do not mix standard-library HTTP types or another HTTP/2 implementation.
- External headers/trailers remain `[]HTTPHeaderField`. Preserve raw HeaderBlock order, duplicates, casing, empty values, and truncation. Without raw data, use normalized fields and mark wire order unavailable; never substitute a map.
- Keep request normalization in `synthetic_request_headers.go`: URL controls routing; the backend owns pseudo-headers, `Host`, framing, generated content type, and fallback UA. Reject HeaderOrder the selected transport cannot represent losslessly.
- Keep synthetic transport/shared TLS dialing in `synthetic_transport.go`. Explicit HTTP/1.1 must not apply HTTP/2 fingerprints; redirects retain fingerprint semantics. Protocol, proxy, and fingerprint settings must round-trip through API Collection.
- Live `HTTPMessageMetrics` comes only from transport-boundary events, with microsecond precision and retry/capture-generation isolation. HAR import may map valid file statistics. Unknown numbers use `-1`; failed, canceled, or incomplete bodies must not gain fabricated completion values.
- `HeaderSize` is the UTF-8 size of the complete Raw-panel head, including start line, field lines, and terminating blank line. HTTP/2 uses synthetic start lines and displayed pseudo-header conversion, excluding TCP/TLS, HPACK, and frame overhead. Frontend/HAR read it directly. `BodySize` counts entity bytes after transport encoding; traffic and certificate timestamps use Unix microseconds.
- Write HBIN v2 and read v1/v2; earlier development layouts remain unsupported. Skip unknown versions without deleting them. Traffic/model changes require codec, history/HAR tests, bindings, and frontend updates together.
- Reuse proxy-service `HARFileWriter` and shared traffic-export writers with streamed atomic writes. Distinguish empty bodies from missing payloads, retain other exportable HAR entries, and report skipped/missing-body counts. Exports do not automatically redact credentials, cookies, bodies, or process paths.
- Frontend export goes through `useTrafficExport.ts`/`useHARExport.ts`, preserving source, ID order, capture generation, extensions, concurrency guards, and result statistics. Capture/history menus expose CSV/HAR; traffic-row menus expose individual message formats too.
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
- Icon cleanup coordinates Manager rotation/shutdown, pending-task cancellation, stale `IconKey` invalidation, disk deletion, and each frontend window's cache. Missing disk files must remain recoverable on demand.
- Reuse `AppProcessIcon.vue` and `processIconCache.ts`; business components must not call `GetProcessIcon` directly or create another cache.
- `ProcessInfo` changes require proxy models, HBIN codec/tests, history, bindings, and frontend display updates together.

### Settings, Certificates, and Lifecycle

- Settings defaults, validation, partitioned serialization, and SQLite persistence belong to `setting_service`; logs go through `backend/pkg/logger`/`logging_service`.
- Ordinary settings use `UpdatePreservingShortcuts`. Preserve newer shortcut, Python-runtime, and traffic-detail state from narrow writes; layout/table preferences use `SaveTrafficDetailConfig`/`SaveTrafficTableConfig`, serialized with full saves and published only after persistence succeeds.
- CA trust stays in `setting_service`, serialized with generation and guarded by the expected certificate fingerprint. Install only the public certificate into the current user's Windows root store or macOS login keychain/user trust; validate CA, validity, and key pair first.
- CA status must distinguish unknown/incomplete/installed states. Uninstall only the exact certificate/trust record, including expired certificates or missing keys. Remove existing/incomplete trust before regeneration; do not add automatic replacement, historical-CA cleanup, or machine-wide trust.
- Managed system proxy uses `proxy_service` and `backend/pkg/systemproxy`. Preserve the original snapshot and restore on normal shutdown only if the managed configuration still matches; never overwrite newer external changes.
- Updates use `app_service` backend snapshots/revisions and the `updater` store across windows. Keep one operation active, ignore stale events, and cancel/wait during shutdown. Match artifacts to platform/install eligibility, verify checksums, and retain manual fallback. Restart must use the app's unsaved-settings guard and normal cleanup even after the update window closes.
- Save every `Events.On()` off function and call it on unmount/store `cleanup()`; never clean up by `Events.Off(eventName)`.

### Shortcuts

- System shortcuts belong only to `shortcut_service`: allowlist, register before persist, rollback on failure, and cleanup on shutdown. Changes use `ApplyShortcutConfig`.
- In-app commands go through `frontend/src/shortcuts` and `AppShortcutHost`; buttons/menus/shortcuts share business functions. Do not add parallel component `keydown` handlers.
- `when` checks the real active page/tab. Inputs and Monaco retain their keys unless `editablePolicy: 'allow'` explicitly permits a command.
- Display resolved bindings; `primary` means Command on macOS and Ctrl elsewhere. Missing override uses the default; `binding: null` explicitly disables it.

### Frontend and Copy

- Prefer Nuxt UI, canonical Tailwind v4 classes, semantic colors, and `UIcon`/`i-lucide-*`; avoid new base-control wrappers and `@vicons/*`. Select values cannot be empty strings; use a local sentinel.
- Reuse `utils/{headers,cookies}.ts` for ordered editing/copying and degradation hints, and `utils/format.ts` for dates, microsecond times, durations, and sizes.
- Reuse setting/theme stores, appearance variables, and theme-color helpers for fonts/colors/layout. Check light/dark themes and affected windows; tray/title-bar/visibility changes must cover `backend/app`, `App.vue`, settings, and locales together.
- Keep hex viewing in the shared viewer and `utils/hexdump*.ts`; preserve byte-based selection/copy/export and bounded rendering for large bodies.
- Put copy in both `frontend/src/locales/{zh,en}.json` with matching leaf keys, placeholders, and state conditions. Keep persistent copy short, preserve distinct lifecycles, and retain delete/overwrite/cancel/restart/truncation/order-degradation semantics.

## Verification

Use Go and Node.js versions from `go.mod`/`.node-version` (Node 24 LTS, 24.21+), local npm, and a matching Wails v3 CLI. Task is recommended; Python 3.11+ is optional outside hook tests.

Run the smallest checks matching the risk. Default backend verification is `go test ./...`; use `task lint:go` for Go lint/format checks. Root tasks: `task dev`, `task build`, `task package`, `task run`.

Without Task, run `wails3 dev -config ./build/config.yml`. The frontend defaults to port `9245`, overridden by `WAILS_VITE_PORT`; debug through the Wails desktop window.

| Backend scope | Relevant Go test packages |
| --- | --- |
| SQLite/settings/API Collection | `./backend/pkg/database ./backend/services/setting_service ./backend/services/api_collection_service` |
| Shortcuts | `./backend/services/setting_service ./backend/services/shortcut_service` |
| Metrics/body/HBIN/HAR/traffic export | `./backend/services/proxy_service ./backend/services/history_service` |
| Process attribution | `./backend/pkg/process_attribution ./backend/services/proxy_service ./backend/services/history_service ./backend/services/setting_service` |
| Python hooks | `./backend/services/python_plugin_service ./backend/services/proxy_service ./backend/services/setting_service` |
| CA trust/appearance/layout | `./backend/services/setting_service` |
| Updates/windows/shutdown | `./backend/app ./backend/services/app_service` |

Prefix package lists with `go test`. For concurrency/lifecycle changes, also run `go test -race ./backend/pkg/process_attribution ./backend/services/proxy_service` when supported. Worker/SDK/interpreter/package changes must actually run Python 3.11+ integration tests, without skips.

Frontend checks run inside `frontend`:

```shell
npm run type-check
npm run lint
npm run lint:tailwind
npm run build
```

| Frontend scope | Relevant npm scripts |
| --- | --- |
| Process icons | `test:process-icon-cache` |
| Request editor/Python console | `test:request-editor-state`, `test:python-console` |
| Traffic/export/table | `test:traffic-utils`, `test:traffic-table` |
| Detail layout/hex viewer | `test:traffic-detail-layout`, `test:hexdump` |
| Updates/CA trust/theme colors | `test:updater`, `test:ca-trust`, `test:theme-colors` |

Prefix scripts with `npm run`. Component/theming/packaging changes require `npm run build`. Full Tailwind diagnostics use `npm run lint:tailwind -- --all`; fixes only through explicit `lint:fix` or `lint:tailwind -- --fix`. In PowerShell, use `npm` when forwarding script arguments.

## Docs, Releases, and Completion

- Python entry-point/scope/hook/order/limit/security/console changes update both technical guides, examples, and README.
- Use English Conventional Commits: `<type>(<scope>): <summary>`, one class of change per commit. Include only verified in-scope files, synchronized bindings/interfaces, and consistent bilingual copy.
- Release metadata uses `task version VERSION=1.2.3`; add `CHECK=true` to verify without writing. Wails upgrades must align `go.mod`, frontend runtime, and `WAILS_VERSION` in `.github/workflows/release.yml`.
- After changing `build/config.yml` metadata/file associations, run `wails3 task common:update:build-assets`.
- Report the completed result, checks run, and unresolved risks. If verification is unavailable, state why; feasibility alone is not completion.
