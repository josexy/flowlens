# Live HTTP Rewrite Rules

This document specifies execution semantics, body-processing limits, persistence, preview behavior, and transport contracts for live HTTP rewrite rules. For step-by-step usage and examples, see the [User Guide](../user-guide.md#rewrite-rules).

## Scope and Ownership

Rewrite rules apply **only to live HTTP/HTTPS proxy traffic**. Request Editor sends, resends, imported history, WebSocket upgrades/frames, `CONNECT` tunnels, and raw TCP tunnels bypass rewrite rules (even when a rule uses method `ALL`). Python hooks remain exclusive to the HTTP Request Editor.

| Owner | Responsibility |
| --- | --- |
| `internal/services/rewrite_service` | Persistence, validation, compiled matchers, and immutable rule snapshots |
| `internal/services/proxy_service` | Applying rule actions and recording final rewritten traffic |
| `internal/app` | Startup loading, shutdown coordination, and quit guards |
| `rewriteRules` store (`internal/desktop/store`) | Editor drafts, dirty tracking, and revision reconciliation |

Because HTTPS interception may require connecting to the original upstream target and completing TLS first, redirects away from unreachable HTTPS endpoints can fail before the redirect executes.

## Matching and Execution

Each rule defines one action type: `redirect`, `request`, or `response`.

- **Single upfront match:** Every enabled rule is matched **once** against the original HTTP method and complete URL. The matched rule snapshot and wildcard captures are retained across both request and response phases; mutations by earlier rules never trigger rematching.
- **Phase order:** Rules run in list order within each phase. Anti-cache and anti-compression options run before request rules. Request rules apply `query → headers → body`; response rules apply `headers → body`.
- **URL pattern syntax:** Patterns are anchored to the full URL. Scheme and authority are case-insensitive; path and query preserve case, percent-encoding, and parameter order. `*` captures zero or more characters (including empty strings), `\*` matches a literal `*`, and `?` matches a literal `?`. All other backslash escapes are rejected.
- **Redirect targets:** Targets expand `${1}`, `${2}`, etc. without re-encoding captured text, and `$$` emits a literal `$`. Targets must be absolute `http` or `https` URLs without userinfo or fragments and must supply the complete query string. Redirects change upstream routing without synthesizing a `302` response: the target determines scheme, host, port, and TLS SNI. The `Host` header defaults to the target authority unless `preserve` is selected, which retains the value immediately preceding that rule (including HTTP/2 `:authority`).

### Header and Query Mutations

Header and query edits use ordered operation lists:

- `add` appends a new field.
- `set` replaces the first matching field and removes later duplicates, or appends if absent.
- `delete` removes all matching fields.

Header names are matched case-insensitively; decoded query parameter names are case-sensitive. Untouched query parameters preserve their original encoding, duplicates, and order.

Headers preserve duplicate names, original casing, empty values, and field interleaving; if raw wire order was unavailable, it stays unavailable. `Host` allows only a nonempty request-side `set`. Pseudo-headers, transfer/content encoding, framing headers, and hop-by-hop fields remain backend-owned. Response rules cannot edit query parameters. Whenever a body changes, FlowLens recalculates framing and strips stale validators, digests, range headers, and incompatible trailers.

### Failure Handling

**Any rewrite failure aborts the exchange.** Errors identify the failing rule and action and wrap `mitmproxy.ErrDropHTTP`:

- On HTTP/1, the connection closes without emitting a substitute response.
- On HTTP/2, only the affected stream is aborted.
- Terminal paths close open bodies and cancel all rule-owned work.

## Body Processing and Limits

URL, header, and query-only rules keep bodies streaming. Body actions buffer finite UTF-8 text, apply literal or Go `regexp` (RE2) replacement, and re-encode the body. Regular expressions are compiled prior to publication and replace all matches, supporting numbered/named capture groups and `$$`. Zero matches succeed as a no-op; matches may leave content unchanged or produce an empty body.

| Resource | Limit |
| --- | --- |
| Encoded input, decoded text, replacement result, and re-encoded output | `8 MiB` each |
| Concurrent body phases (across requests and responses combined) | `4` |
| Total body-phase deadline (including concurrency slot wait and all body actions) | `10` seconds |
| Disk spooling threshold for buffered payloads | Above `4 MiB` (managed `body_spool` files) |
| Regex match-index buffer | ~`16 MiB`, scaled by capture count (not a total-memory bound) |

- **Encodings and media checks:** Supported content encodings are `gzip`, `deflate`, `br`, `zstd`, and `snappy`. Body edits reject unsupported encodings or charsets, `zstd` frames that declare a window larger than `8 MiB`, invalid UTF-8, embedded NUL bytes, SSE/NDJSON/continuous streams, and binary media types (validating both original and edited `Content-Type` headers). Responses to `HEAD` or with status `< 200`, `204`, or `304` cannot receive a body, though header-only rules still apply.
- **Bounded regex scanning:** Regex scanning runs through a cancellable rune reader that preserves Go `regexp` semantics. Expressions too large for the continuation wrapper fall back to synchronous matching only when `(input bytes + 1)² × program cost ≤ 16 × 1024 × 1024` (where program cost accounts for instructions, rune tables, and capture slots). Cancellation is checked before and after fallback scans; no regex task outlives its phase.
- **Spool ownership:** Disk spooling bounds disk/transport buffering but does not reduce decoded-text or regex memory usage. Output readers own temporary spool files until closed, and failure or cancellation closes intermediate readers and deletes files immediately. Temporary spool paths never enter configuration, UI state, or history.

## Persistence, Publication, and Interface Drafts

SQLite stores the master switch and revision counter in `rewrite_state`, and ordered, versioned rule configurations in `rewrite_rules`. Both the master switch on a new database and newly created rules default to disabled.

Mutations are serialized and checked against the caller's expected revision:

```text
validate & compile → commit SQLite transaction → publish immutable snapshot → emit rewrite:changed { revision }
```

Failures preserve the prior snapshot. Unchanged compiled matchers are reused across updates, and writes touch only affected state, enable/order metadata, or rule configurations. Live traffic never queries SQLite or compiles rules on the request path.

| Lifecycle Stage | Behavior |
| --- | --- |
| **Startup** | Starts a single background loader (registered after synchronous DB consumers) and publishes only a complete snapshot. |
| **Before readiness** | `GetState` waits while honoring caller cancellation; proxy `Start` waits before binding; mutations are rejected. |
| **Load failure** | Logs and returns the error to reads and proxy startup; the app stays usable and requires restart to retry. |
| **Unsupported rule version** | Preserves the raw JSON and surfaces why the rule is unavailable; never executes or silently deletes it. |
| **Shutdown** | Cancels/joins background loading and joins active mutations before closing SQLite; cleanup is idempotent. |

Canceling an individual read leaves the shared startup loader running. Database waits and queries honor cancellation, and JSON decoding and regex compilation poll cancellation between items. Background loading still loads all rule bodies, using the same total work and memory as synchronous loading.

### UI Drafts and Editing Lifecycle

- The UI loads rules on first open. Drafts share immutable strings, cache dirty comparisons per rule, and merge incoming snapshots in a single batch.
- Stale revisions are ignored; clean drafts refresh automatically, while dirty drafts stay intact with conflict flags.
- Saved changes take effect for new requests immediately (including on reused connections).
- Writes serialize while forms stay editable. Saving snapshots the submitted draft so subsequent edits remain dirty relative to the returned baseline. Metadata-only snapshots preserve rule objects, editor state, and previews.
- Progress indicators appear on the active save button/switch and on initial/manual reloads; background event refreshes stay silent.
- Deleting a rule offers **Cancel** or **Delete** and removes the confirmed rule and its draft without saving or validating the draft first. Unsaved new rules discard locally; failed deletes retain the draft and keep the dialog open for retry.
- Quitting and update restarts prompt to save, discard, or cancel. Failed saves preserve drafts and abort shutdown/restart. Backend quit confirmation validates both the main-window sender and request identity. Drafts are not crash-recovery storage, and event subscriptions are cleaned up via their returned disposers.

## Regex Replacement Preview

`PreviewBody` validates and executes an unsaved regex replacement using the live body-action compiler, bounded matcher, and replacement expander:

- A single scan returns the match count and output under the same `8 MiB` sample/output cap, `10`-second deadline, and caller cancellation rules.
- Previews bypass URL matching and produce no network traffic, rule mutations, or traffic records.
- The preview panel keeps sample text only while mounted and always evaluates against the original sample. Pattern and replacement edits debounce by `300 ms`; superseded calls are canceled and stale responses ignored. The sample editor remains mounted to preserve undo history and view state.
- Pending or failed results cannot be copied, and errors automatically switch back to reveal the sample.
- The read-only inline diff highlights whitespace changes under a `1`-second computation limit. If either input or output reaches `524,288` characters (`512 Ki`) total or `131,072` characters (`128 Ki`) on a single line, the panel shows the complete plain result without diffing or wrapping. Fonts and themes follow the shared desktop stores.

## Capture, Metrics, History, and Export

- **Recorded content:** Capture records final rewritten headers, bodies, URL, `Host`, and protocol.
- **Metrics attribution:** Request metrics come from upstream transport events. Rewritten responses record downstream send events (`responseMetricsSource: downstream`), while unchanged responses (including regex no-ops) keep upstream metrics. Upstream EOF during body buffering never proves downstream delivery.
- **Shared conventions:** Timing and sizes follow [Traffic Metrics and HAR](traffic-metrics-and-har.md). `BodySize` counts encoded entity bytes actually transmitted; failures and incomplete sends preserve unknown (`-1`) or incomplete metrics.
- **Live summaries and HBIN:** Live rewrite summaries retain up to `64` recent actions per exchange, truncate reasons at `512` characters, and follow normal traffic retention. HBIN writes v3 (and reads v1–v3), storing final content and errors without live summaries or the `responseMetricsSource` label. Shared exports emit final fields, metrics, and missing-body counts without automatic redaction.

## Transport Dependencies

| Transport API | Contract |
| --- | --- |
| `WithHTTPUpstreamTarget` | Validates original authority and routes to the final target while preserving proxy, TLS, and cancellation settings. Same-origin edits reuse connections; cross-origin edits own an isolated transport. |
| `HeaderBlock` overrides and request-send observer | Preserve exact ordered fields and report the actual emitted request head. |
| `ErrDropHTTP` / response-send observer | Abort failed exchanges and report final protocol, headers, timestamps, byte counts, and send outcome. |
| `AbortHTTPRequestRead` | Interrupts request-body reads; an aborted HTTP/1 body must not advance pipeline parsing. |
| `xhttp.FinishResponse` | Completes HTTP/2 body, trailer, and `END_STREAM` writes before reporting completion (idempotent; does not guarantee peer acknowledgment). |

- `xhttp.HeaderOrder` orders name groups under standard serialization, whereas `xhttp.HeaderBlock` supplies and strictly validates every individual occurrence. Both share HTTP/1 serialization and HTTP/2 HPACK encoding. The HTTP/1 transfer writer generates framing once, and ordered trailers avoid serialize/parse round trips.
- `mitmproxy-go` block overrides reconcile field occurrences with sanitized values and regenerate protocol-owned fields. HTTP/1 response ordering uses a single parsed head and one serialization pass while keeping received metadata separate and bodies streaming.
- `go.mod` and `go.sum` pin published transport commits. A local `go.work` may point to sibling checkouts during development; standalone builds and tests run with `GOWORK=off`.

## Verification

Run the checks mapped in [AGENTS.md](../../AGENTS.md#verification), including rewrite, proxy, history, and app tests, race checks where relevant, and the `internal/desktop/rewrite`, `internal/desktop/store`, and `internal/desktop/logic` test suites.

- **Transport tests:** Cover real HTTP/1 and HTTP/2 routing, ordered headers, HTTP/2 stream isolation, partial writes, cancellation, and timeout cleanup.
- **Lifecycle tests:** Cover asynchronous readiness, first-request enforcement, startup failures, and clean shutdown.
- **Desktop acceptance:** Verify rule editing, regex preview, drag reordering, revision conflicts, quit/update prompts, persistence, and exports.
