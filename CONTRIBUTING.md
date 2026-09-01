# Contributing to FlowLens

Thank you for helping improve FlowLens. This document covers the common contribution path; [AGENTS.md](AGENTS.md) contains the complete repository-specific engineering rules.

## Before You Start

- Search existing issues and pull requests before opening a duplicate.
- Use a focused issue to discuss changes that alter persisted data, public models, capture semantics, security boundaries, or user-facing workflows.
- Report security vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.
- Never attach unredacted captures, HAR files, databases, certificates, private keys, API Collections, logs, or screenshots containing sensitive data.

## Development Setup

You need Go 1.27+, Node.js 20.19+ or 22.12+, npm, the Wails v3 CLI, and preferably Task. Python 3.11+ is optional unless you are testing HTTP Request Editor Python plugins.

```shell
cd frontend
npm install
cd ..
wails3 generate bindings -ts -i
task dev
```

Without Task, start the desktop development app with:

```shell
wails3 dev -config ./build/config.yml
```

FlowLens desktop development uses the embedded Wails WebView. Opening the Vite page directly does not provide a working Go backend.

## Project Boundaries

- Keep `main.go` limited to embedded assets and delegation to `backend/app`. Application assembly, the single-instance guard, database startup, windows, tray, and shutdown coordination belong in `backend/app`.
- In `backend/app/app.go`, create the Wails application before opening `flowlens.db`, so a second process exits through the single-instance guard before contending for SQLite.
- Prefer existing services and packages over one-off abstraction layers. Access settings and API Collection data through their repositories rather than adding scattered SQL.
- Keep API Collection transactions and managed body files consistent across commit, rollback, orphan cleanup, and startup validation.
- Put captured-body storage behavior in `backend/pkg/body_cache` or the owning proxy path. Keep HTTP Request Editor Python large-body handoff in `backend/pkg/body_spool`.
- Do not hand-edit files under `frontend/bindings`; regenerate them after exported Wails APIs or models change.

## Capture, History, and Request Editor Contracts

- Preserve request headers and response headers/trailers as ordered `HTTPHeaderField` slices. Do not flatten observable field-line order, duplicates, casing, or empty values into maps. Carry truncation and order-unavailable state through live traffic, history, Request Editor, bindings, and frontend warnings.
- Source `HTTPMessageMetrics` timestamps, logical header sizes, encoded body sizes, and terminal states from transport-boundary observations. Preserve microsecond precision, retry isolation, and `-1` for unknown values; do not synthesize completion for failed, canceled, or partial exchanges.
- Keep shared HAR 1.2 generation and streamed atomic file writes in `backend/services/proxy_service`; history supplies persisted entries and bodies to that implementation. Preserve the distinction between an empty body and a missing cache body.
- Store message metrics and certificate validity as Unix microseconds. Use the shared frontend date, duration, and size formatters rather than formatting raw values inside components.
- Request Editor routing, pseudo-headers, `Host`, framing, generated content types, fallback user agent, protocol selection, TLS profiles, HTTP/2 fingerprints, redirects, proxies, and h2c must remain in the existing request normalization and synthetic transport paths. Reject layouts a selected transport cannot represent instead of silently reordering them.
- Protocol and fingerprint settings must round-trip through API Collections and generated bindings without changing ordinary MITM capture behavior.
- When traffic models, metrics, certificate fields, or ordered headers/trailers change, update the HBIN v1 codec, history and HAR tests, Wails bindings, frontend helpers, and traffic utility tests together.

## Process Attribution

- Keep platform lookup, process identity caches, and icon extraction in `backend/pkg/process_attribution`; proxy connection acceptance must not block on process lookup.
- Only direct local connections participate in attribution. Remote clients must be marked and skipped.
- A process cache identity is PID plus process start token, never PID alone.
- Coordinate Manager shutdown, pending work, stale icon keys, disk files, and per-window frontend caches when changing cache cleanup or icon recovery.
- Changes to `ProcessInfo` require coordinated proxy models, HBIN codec/tests, history tests, bindings, and frontend rendering updates.

## Frontend Conventions

- Use Vue `script setup`, existing Pinia stores, Nuxt UI v4 primitives, and Tailwind CSS v4 conventions already present in the owning feature.
- Prefer Nuxt UI semantic classes and `UIcon` with `i-lucide-*`. Add a shared wrapper only when it provides actual application behavior, not only styling.
- Reuse the existing API Collection, traffic workspace, workbench, setting, logging, and theme stores instead of creating parallel state.
- Reuse `AppProcessIcon.vue`, the window-scoped `processIconLoader.ts`, and the pure `processIconCache.ts` cache implementation for process icons.
- Put new user-visible text in both `frontend/src/locales/en.json` and `frontend/src/locales/zh.json`, with matching keys, placeholders, and state conditions.
- Keep application shortcuts in `frontend/src/shortcuts`. System-wide shortcuts must go through the allowlisted backend `shortcut_service`.
- Pair every Wails `Events.On()` subscription with the returned off function; do not remove unrelated listeners by event name.
- Nuxt UI select values cannot be empty strings. Map a business-level default through a component-local sentinel value.

## Validation

Run the smallest relevant package or utility tests while developing. Before requesting review, run the applicable full checks:

```shell
go test . ./backend/...
go vet . ./backend/...

cd frontend
npm run type-check
npm run lint
npm run lint:tailwind -- --all
npm run test:process-icon-cache
npm run test:request-editor-state
npm run test:traffic-utils
npm run build
```

Changes involving Python Workers should confirm that integration tests actually ran with Python 3.11+, rather than being skipped because no interpreter was available. Concurrency-sensitive process attribution changes should also run the relevant Go tests with `-race` on a supported local toolchain.

## Pull Requests

Keep a pull request focused and describe:

- the user-visible or architectural problem;
- the chosen behavior and important non-goals;
- persisted-data, privacy, compatibility, and cross-platform effects;
- the tests and manual checks performed;
- screenshots only when they materially help review, with sensitive data removed.

Do not include unrelated formatting, generated files that were not affected, local agent settings, credentials, captures, databases, certificates, logs, or build output.
