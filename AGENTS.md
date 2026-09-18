# AGENTS.md

Collaboration guidelines for the FlowLens project, for developers and coding agents. They focus on module boundaries, high-risk contracts, and verification requirements; product capabilities and usage instructions are defined by the README and `docs/`.

## 1. Project and Tech Stack

FlowLens is a cross-platform desktop MITM traffic capture tool built with `Wails v3 + Go 1.27+ + Vue 3 + TypeScript`. Core capabilities include HTTP/HTTPS and SOCKS5 proxying, WebSocket, request editing and resending, API Collection, history and HAR, process attribution, Python plugins, certificates, shortcuts, and local settings.

- Backend: Go, Wails v3, SQLite (`modernc.org/sqlite`), `mitmproxy-go`, `xhttp`, `websocket`, uTLS, `logx`
- Frontend: Vue 3, Vite, Vue Router, Pinia, Nuxt UI v4, Tailwind CSS v4, Monaco, Lucide
- Optional runtime: external CPython 3.11+; no embedded CPython, no CGO dependency
- Toolchain: npm, ESLint, Tailwind CSS language server, Go test, TypeScript utility tests

## 2. Module Boundaries

- `main.go`: only embeds resources and starts `backend/app`.
- `backend/app`: Wails wiring, SQLite initialization, window/tray/single-instance lifecycle, event bridging, and safe shutdown.
- `backend/services/proxy_service`: proxy core, request editing and synthetic transport, WebSocket, resend, timing/size collection, HBIN/HAR, and real-time frontend events.
- `backend/services/history_service`: entry points for history reads, deletion, resend, and HAR import/export; shared encoding and the HAR implementation are still provided by `proxy_service`.
- `backend/services/python_plugin_service`: plugin registration, revisions, rules, Workers, frame protocol, SDK, request hooks, and real-time logs.
- `backend/services/api_collection_service`: API Collection SQLite repository, tree operations, transactions, and managed request body files.
- `backend/pkg/process_attribution`: cross-platform process queries, asynchronous Manager, identity/icon caches; `proxy_service` only handles integration and lifecycle.
- `backend/services/{setting_service,logging_service,shortcut_service}`: the single business entry point for settings, logging, and system-level shortcuts.
- `backend/pkg/{body_cache,body_spool,database,logger}`: Body cache, Python Body temp files, shared database, and logging infrastructure.
- `frontend/src/stores`: cross-component state; prefer reusing existing stores such as `trafficWorkspace`, `apiCollection`, `workbench`, and `setting`.
- `frontend/src/components/traffic-workspace`: capture, history, classification, request editing, and WebSocket client UI.
- `frontend/src/shortcuts`: in-app command catalog, binding resolution, conflict detection, and single-window dispatch.
- `frontend/bindings`: Wails generated artifacts, used via `#bindings/*`, never maintained by hand.
- `docs/technical/python-plugins*.md` and `docs/examples/python-plugins`: Python plugin user contract and examples.

Changes belong inside the existing business owner; do not add parallel implementations or unnecessary abstractions for a one-off requirement.

## 3. General Collaboration Rules

- When a requirement has more than two reasonable interpretations that would significantly change the outcome, state the differences first; otherwise make reasonable assumptions based on the existing code and proceed.
- Preserve workspace changes unrelated to the task. When committing, include only the scope of the current task and do not mix in incidental formatting or unrelated fixes.
- For Go, follow the existing service/package split; for Vue, follow the existing `script setup`, Pinia, Nuxt UI, and Tailwind v4 organization.
- Use Nuxt UI directly for base controls; add shared components only for clearly app-level composite behavior.
- User-visible copy goes into `frontend/src/locales/{zh,en}.json`, not hardcoded in components.
- After adding or changing Wails exported interfaces/models, run `wails3 generate bindings -ts -i` and commit the generated artifacts with the change.
- Code changes must come with the minimal verification matching their risk; when verification cannot be run, state the reason and the remaining risk explicitly.

## 4. High-Risk Contracts

### 4.1 Networking, Headers, History, and HAR

- The proxy and request editing uniformly use `github.com/josexy/xhttp` with built-in HTTP/2 support; do not mix in standard library HTTP types or other HTTP/2 implementations.
- The external Header/Trailer model stays `[]HTTPHeaderField`. When the raw HeaderBlock is available, preserve line order, duplicates, casing, empty values, and truncation state; only when it is unavailable degrade to normalized fields and mark wire order as unavailable — never fall back to a map.
- HTTP request normalization is centralized in `synthetic_request_headers.go`: the URL determines routing; pseudo-headers, `Host`, framing, generated content type, and fallback UA are maintained by the backend. When the target transport cannot losslessly express HeaderOrder, it must error.
- Synthetic transport and the shared TLS dialer are centralized in `synthetic_transport.go`. Explicit HTTP/1.1 must not apply an HTTP/2 fingerprint; redirects preserve fingerprint semantics; protocol, proxy, and fingerprint configuration round-trip fully through API Collection.
- For live capture, `HTTPMessageMetrics` may only come from transport boundary events, and must keep microsecond precision plus retry isolation and capture generation isolation. HAR import may map valid statistics from the file onto existing fields. Failed, canceled, or incomplete Bodies must not fabricate completion values; unknown numeric values use `-1`.
- `HeaderSize` is the UTF-8 logical size of the complete textual header section in the Raw panel, including the start line, field lines, and the terminating empty line; HTTP/2 uses a synthetic start line and the display conversion of pseudo-headers, excluding TCP/TLS, HPACK, or frame overhead. The frontend and HAR read this metric directly; `BodySize` is the entity Body byte count after transport-layer encoding. Backend timestamps are uniformly stored as Unix microseconds.
- The current write format is HBIN v2 (which adds upstream connection timing); continue reading v1, and remain incompatible with earlier development-state layouts. Unknown versions should be skipped and must not be deleted; model changes require updating the codec, history tests, bindings, and frontend in sync.
- HAR generation and streaming atomic writes uniformly reuse `HARFileWriter` from `proxy_service`. Distinguish an empty Body from a missing cached Body, preserve the remaining exportable items, and count skipped/missingBodies. HAR does not automatically redact credentials, cookies, Bodies, or process paths.
- API Collection write operations must keep the SQLite transaction and managed request body files consistent, covering failure rollback, orphan cleanup, and startup validation.

### 4.2 Python Plugins

- Plugins only enter the HTTP request editor; they do not enter ordinary MITM capture, resend, or the WebSocket client.
- External CPython Workers use a versioned length-prefixed JSON protocol with bounds on frames, source code, Body, parameters, shared state, output, and hook time. On timeout, cancellation, crash, or protocol corruption, terminate and replace the Worker; on Windows also clean up the child process tree.
- `-I` only isolates the import environment; it is not a security sandbox. Plugins are always treated as trusted code.
- Before each send, snapshot the matching plugin order, revisions, and parameters. The request chain is "global plugins -> current request script -> network", and the response chain executes in reverse; the request phase is fail-closed and the response phase is fail-open.
- `context.shared` is isolated between different plugins; the request/response hooks of the same plugin may share JSON state.
- The user-facing Body semantics are separated from the internal storage representation. Edited requests and ordinary responses over 4 MiB use `body_spool`; file bytes must not be embedded in Worker JSON frames, and temp paths must not be exposed or persisted.
- Temp files managed by FlowLens must be cleaned up after success, blocking, failure, timeout, cancellation, and Worker exit. SSE may only modify state and Headers before the response starts, does not accept Body/Trailer modifications, and does not create spool files.
- Global plugin information, rules, parameters, and effective revisions are persisted by SQLite plus the managed directory. The current request script source may be saved alongside an API Collection HTTP request; the global plugin bypass switch and the current script enabled state belong only to the current tab and default to off when reopened.
- Complete run output goes only to the request editing console carrying the execution ID; do not restore a separate global log ring or plugin workbench log history.

### 4.3 Process Attribution and Icons

- Process queries must be asynchronous and must not block proxy connections; only local direct connections participate, and remote clients are explicitly skipped.
- Process identity uses PID + `StartToken`, and the queue, TTL, capacity, and query timeout stay bounded. Late results must verify that the connection and record are still valid.
- When clearing the icon cache, coordinate Manager rotation/shutdown, background task cancellation, invalidation of old `IconKey`s, disk file deletion, and frontend window cache invalidation; after disk files are lost, they should be recoverable on demand.
- The frontend uniformly uses `AppProcessIcon.vue` and `processIconCache.ts`; do not call `GetProcessIcon` directly in business components or build a separate cache.
- When `ProcessInfo` changes, update the proxy model, HBIN codec/tests, history, bindings, and frontend display in sync.

### 4.4 Settings, Logs, Events, and Shortcuts

- Settings maintain defaults, validation, partitioned serialization, and SQLite persistence through `setting_service`; logging uniformly goes through `backend/pkg/logger` and `logging_service`.
- Frontend `Events.On()` must save the returned off function and call it on component unmount or store `cleanup()`; do not use `Events.Off(eventName)` to clean up listeners with the same name.
- System-level shortcuts may only be registered by `shortcut_service`, keeping the whitelist, register-before-persist, rollback on failure, and shutdown cleanup.
- In-app shortcuts go through `frontend/src/shortcuts` and `AppShortcutHost`; buttons, menus, and shortcuts reuse the same business function, and no component-level parallel `keydown` handlers are added.
- A handler's `when` must check the real active page and tab state. Input controls and Monaco keep their own keys by default; only commands with `editablePolicy: 'allow'` may execute while editing.
- Shortcut display uniformly reads the resolved binding. `primary` is Command on macOS and Ctrl on Windows/Linux; do not hand-assemble platform copy.
- A missing override means use the default binding, and `binding: null` means explicitly disabled. Ordinary settings saves use `UpdatePreservingShortcuts`; shortcut changes go only through `ApplyShortcutConfig`.

### 4.5 Frontend and Copy

- Prefer Nuxt UI native components, Tailwind v4 canonical classes, semantic color classes, and `UIcon`/`i-lucide-*`; do not introduce new base control wrappers or `@vicons/*`.
- `USelect` / `SelectItem` values cannot be empty strings; when an empty value is needed, use a sentinel mapping inside the component.
- Header/Trailer/Cookie editing and copying reuse `frontend/src/utils/{headers,cookies}.ts`, preserving order, duplicates, casing, empty values, and degradation hints.
- Dates, microsecond times, durations, and sizes reuse `frontend/src/utils/format.ts`; do not format backend raw values inside components.
- HAR menus and prompts reuse `useHARExport.ts`, preserving source, ID order, extension completion, concurrency protection, and result statistics.
- New copy must be completed in both Chinese and English while keeping leaf keys, placeholders, and state conditions consistent. Do not merge keys with different lifecycles just because their current translations happen to match.
- Persistent areas use short copy and avoid repeating title, description, status, and button text; high-risk semantics such as delete, overwrite, cancel, restart-to-apply, and truncation/wire-order degradation must not be omitted.
- Interactions that affect theming are checked in at least light/dark; changes affecting the tray, title bar, or window visibility require checking `backend/app`, `App.vue`, the `setting` store, and locales in sync.

## 5. Pre-Development Impact Checklist

Before implementing, confirm according to the change scope:

- Whether it changes the SQLite schema, transactions, settings persistence, API Collection managed files, or cache cleanup.
- Whether it changes HTTP models/metrics, Header/Trailer, Body availability, certificate timing, HBIN/HAR, or protocol/fingerprint.
- Whether it changes the Python Worker, hook order, temp files, SSE restrictions, execution ID, or script persistence boundaries.
- Whether it changes process attribution, connection close races, cross-platform providers, the icon cache, or `ProcessInfo`.
- Whether it changes Wails events, shortcut commands, window/tray lifecycle, bindings, i18n, docs, or tests.

For multi-step tasks, first define "what changes, how it is verified, and the completion criteria".

## 6. Development and Verification Commands

Environment requirements: Go, Node.js 24 LTS (24.21+, see `.node-version` for the CI version), npm, wails3; Task and Python 3.11+ as needed per feature. CI uses the npm bundled with the specified Node.js version, while locally use the npm in the current environment; no separate npm version constraint applies.

```shell
wails3 generate bindings -ts -i
task dev
task build
task package
task run
task version VERSION=1.2.3
task version VERSION=1.2.3 CHECK=true
```

To start development mode directly, use `wails3 dev -config ./build/config.yml`. The desktop frontend port defaults to `9245` and can be overridden by `WAILS_VITE_PORT`; the frontend UI must be debugged through the Wails desktop window.

Default backend verification:

```shell
go test ./...
```

Add per scope:

- SQLite: `go test ./backend/pkg/database ./backend/services/setting_service ./backend/services/api_collection_service`
- Shortcuts: `go test ./backend/services/setting_service ./backend/services/shortcut_service`
- Process attribution: `go test ./backend/pkg/process_attribution ./backend/services/proxy_service ./backend/services/history_service ./backend/services/setting_service`
- Metrics, Body, HBIN, HAR: `go test ./backend/services/proxy_service ./backend/services/history_service`
- Python plugins: `go test ./backend/services/python_plugin_service ./backend/services/proxy_service ./backend/services/setting_service`
- Concurrency/lifecycle: when the toolchain supports it, run `go test -race ./backend/pkg/process_attribution ./backend/services/proxy_service`

When the Worker protocol, SDK, interpreter, or third-party packages are involved, confirm that the Python 3.11+ integration tests actually run rather than being skipped.

Frontend verification:

```shell
cd frontend
npm run type-check
npm run lint
npm run test:process-icon-cache
npm run test:request-editor-state
npm run test:traffic-utils
npm run lint:tailwind
npm run build
```

Choose the minimal set by scope; for changes involving component structure, theming, or production packaging, run `npm run build`. For full Tailwind diagnostics use `npm run lint:tailwind -- --all`, and for automatic fixes use only the explicit `lint:fix` or `lint:tailwind -- --fix`. Under Windows PowerShell, use `npm` when forwarding script arguments, for example `npm run lint:tailwind -- --all`.

## 7. Docs, Commits, and Definition of Done

- When Python plugin entry points, scope, hook contracts, execution order, limits, security model, or console change, update the Chinese and English technical docs, examples, and README in sync.
- Commits use English Conventional Commits: `<type>(<scope>): <summary>`. One commit addresses only one class of problem; larger changes add a short description.
- Before committing, confirm: in-scope files are verified, there are no unrelated changes, generated artifacts are in sync, bilingual copy is consistent, and frontend/backend interfaces match.
- When done, state the implementation result, the verification commands, and any unresolved risk; do not treat "theoretically feasible" as done.
