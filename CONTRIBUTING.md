# Contributing to FlowLens

Thank you for helping improve FlowLens. This document covers the common contribution path; [AGENTS.md](AGENTS.md) contains the complete repository-specific engineering rules.

## Before You Start

- Search existing issues and pull requests before opening a duplicate.
- Use a focused issue to discuss changes that alter persisted data, public models, capture semantics, security boundaries, or user-facing workflows.
- Report security vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.
- Never attach unredacted captures, HAR files, databases, certificates, private keys, API Collections, logs, or screenshots containing sensitive data.

## Development Setup

You need Go 1.27.3+ and preferably Task. On macOS you also need the Xcode Command Line Tools, because macOS builds use cgo. Python 3 is optional unless you are testing HTTP Request Editor Python plugins. No JavaScript toolchain is involved.

```shell
task dev
```

Without Task, start the desktop development app with:

```shell
go tool mygo dev
```

Windows and Linux build with `CGO_ENABLED=0`; macOS builds with `CGO_ENABLED=1`, which the Taskfile sets for you.

## Project Boundaries

- Keep `main.go` limited to delegation to `backend/app`. Application assembly, the single-instance guard, database startup, windows, tray, and shutdown coordination belong in `backend/app`.
- In `backend/app/app.go`, take the single-instance lock before opening `flowlens.db`, so a second process exits before contending for SQLite.
- Prefer existing services and packages over one-off abstraction layers. Access settings and API Collection data through their repositories rather than adding scattered SQL.
- Keep API Collection transactions and managed body files consistent across commit, rollback, orphan cleanup, and startup validation.
- Put captured-body storage behavior in `backend/pkg/body_cache` or the owning proxy path. Keep HTTP Request Editor Python large-body handoff in `backend/pkg/body_spool`.
- Keep interface state in `desktop/store` and pure rules in `desktop/logic`, so they can be tested without a window.

## Capture, History, and Request Editor Contracts

- Preserve request headers and response headers/trailers as ordered `HTTPHeaderField` slices. Do not flatten observable field-line order, duplicates, casing, or empty values into maps. Carry truncation and order-unavailable state through live traffic, history, Request Editor, and the warnings the interface shows.
- Source `HTTPMessageMetrics` timestamps, logical header sizes, encoded body sizes, and terminal states from transport-boundary observations. Preserve microsecond precision, retry isolation, and `-1` for unknown values; do not synthesize completion for failed, canceled, or partial exchanges.
- Keep shared HAR 1.2 generation and streamed atomic file writes in `backend/services/proxy_service`; history supplies persisted entries and bodies to that implementation. Preserve the distinction between an empty body and a missing cache body.
- Store message metrics and certificate validity as Unix microseconds. Use the shared formatters in `desktop/logic` rather than formatting raw values inside views.
- Request Editor routing, pseudo-headers, `Host`, framing, generated content types, fallback user agent, protocol selection, TLS profiles, HTTP/2 fingerprints, redirects, proxies, and h2c must remain in the existing request normalization and synthetic transport paths. Reject layouts a selected transport cannot represent instead of silently reordering them.
- Protocol and fingerprint settings must round-trip through API Collections without changing ordinary MITM capture behavior.
- When traffic models, metrics, certificate fields, or ordered headers/trailers change, update the HBIN codec, history and HAR tests, `desktop/logic` helpers, and traffic tests together.

## Process Attribution

- Keep platform lookup, process identity caches, and icon extraction in `backend/pkg/process_attribution`; proxy connection acceptance must not block on process lookup.
- Only direct local connections participate in attribution. Remote clients must be marked and skipped.
- A process cache identity is PID plus process start token, never PID alone.
- Coordinate Manager shutdown, pending work, stale icon keys, disk files, and the interface's icon cache when changing cache cleanup or icon recovery.
- Changes to `ProcessInfo` require coordinated proxy models, HBIN codec/tests, history tests, and interface updates.

## Interface Conventions

- Views rebuild every frame from Go state on the main thread. Keep state in `desktop/store` or a view's own struct, never store a `*ui.Context` or an `Element`, and run `task vet:ui` after changing views.
- Background work goes through `runtime.Run`, whose completion runs on the main thread. Workers must not touch view state.
- Use `github.com/josexy/nuxt-ui-go` components and `lucide` icons already used by the owning feature.
- Reuse `widgets.ProcessIcon` for process icons instead of adding another cache.
- Put new user-visible text in both `locales/en.json` and `locales/zh-Hans.json`, with matching keys and placeholders, then run `task i18n:generate`. Generated accessors take their arguments in alphabetical order of placeholder name.
- Keep application shortcuts in `desktop/shortcuts`. System-wide shortcuts must go through the allowlisted backend `shortcut_service`.
- Keep the function returned by `EventBus.On` and call it when the listener goes away; do not remove listeners by event name.
- Select option values cannot be empty strings. Map a business-level default through a local sentinel value.

## Validation

Run the smallest relevant package tests while developing. Before requesting review, run the full check:

```shell
task verify
```

Changes involving Python Workers should confirm that integration tests actually ran with Python 3, rather than being skipped because no interpreter was available. Concurrency-sensitive changes should also run the relevant Go tests with `-race`, which needs cgo: `CGO_ENABLED=1 go test -race ./desktop/... ./backend/pkg/process_attribution ./backend/services/proxy_service`.

## Pull Requests

Keep a pull request focused and describe:

- the user-visible or architectural problem;
- the chosen behavior and important non-goals;
- persisted-data, privacy, compatibility, and cross-platform effects;
- the tests and manual checks performed;
- screenshots only when they materially help review, with sensitive data removed.

Do not include unrelated formatting, generated files that were not affected, local agent settings, credentials, captures, databases, certificates, logs, or build output.
