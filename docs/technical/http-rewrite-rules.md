# Live HTTP Rewrite Rules

For setup and examples, see the [user guide](../user-guide.md#live-https-rewrite-rules).

## Scope and Ownership

Rules apply to live HTTP/HTTPS proxy traffic. Request Editor, resend, imported history, WebSocket, CONNECT tunnels, and raw TCP bypass them. `ALL` selects HTTP methods without expanding this scope; Python hooks remain specific to Request Editor.

`rewrite_service` owns persistence, validation, compiled matchers, and snapshots. `proxy_service` applies actions and records final traffic. `backend/app` owns startup/shutdown and quit guards; the `rewriteRules` store owns drafts and revisions.

HTTPS interception may require an original-target connection and TLS handshake. Redirecting an unreachable original HTTPS endpoint is not guaranteed to work.

## Matching and Execution

Each rule has one action: `redirect`, `request`, or `response`.

- Match the **original method and complete URL once**, retaining the snapshot and captures for both phases. Request and response rules each run in list order; changes do not trigger rematching.
- Apply existing anti-cache/anti-compression options before request rules. Request actions apply query → headers → body; response actions apply headers → body.
- URL patterns are anchored. Scheme/authority are case-insensitive; path/query preserve case, encoding, and order. `*` captures any text, including empty text; `\*` is a literal star and `?` is literal. Other backslash escapes are invalid.
- Redirects expand `${1}`, `${2}`, etc.; `$$` inserts `$`. Captured bytes retain their encoding. Targets must be absolute HTTP/HTTPS URLs without user information or fragments and supply the complete query.
- Redirects change upstream routing without returning a 302. The target controls scheme, host, port, and TLS SNI. Host defaults to the target; `preserve` retains its value immediately before that rule, including HTTP/2 authority.

Header/query operations use ordered arrays. `add` appends; `set` replaces the first match and removes later matches, or appends if absent; `delete` removes all matches. Header names are case-insensitive; decoded query names are case-sensitive. Untouched query fields retain encoding, duplicates, and position.

Headers preserve duplicates, casing, empty values, and interleaving. Missing received wire order remains marked unavailable. Host permits only a nonempty request-side `set`; pseudo-headers, encoding, framing, and hop-by-hop fields remain backend-owned. Response rules cannot edit queries. Body changes recalculate framing and remove stale validators, digests, range fields, and incompatible trailers.

**Failures stop the exchange.** Errors carry the rule/action and `mitmproxy.ErrDropHTTP`: HTTP/1 closes the connection without a substitute final response; HTTP/2 aborts only the affected stream. Terminal paths close bodies and cancel owned work.

## Body Processing and Limits

URL/header/query-only edits remain streaming. Body actions buffer finite UTF-8 text, replace it directly or through Go `regexp`/RE2, then re-encode it. Regexes are compiled before publication, replace all matches, and support numbered/named captures and `$$`; no match is a successful no-op.

| Resource | Limit |
| --- | --- |
| Encoded input, decoded text, replacement, re-encoded output | **8 MiB each** |
| Concurrent body phases | **4** across requests and responses |
| Phase deadline | **10 seconds**, including slot wait and subsequent body actions |
| Buffered payload | Above **4 MiB**, use managed `body_spool` files |
| Regex match indexes | Approximately **16 MiB**, adjusted for capture count; not a total-memory bound |

Supported encodings are gzip, deflate, br, zstd, and snappy. Unsupported encodings/charsets, invalid UTF-8, NUL-containing text, SSE/NDJSON/continuous streams, and binary media reject body edits. Both original and edited content types are validated. Responses to HEAD or with status below 200, 204, or 304 cannot acquire a body. Header-only rules can still apply.

Regex scanning uses a cancellable rune reader, preserving anchors, boundaries, captures, and empty-match behavior. Extreme expressions that cannot fit the continuation wrapper use a synchronous fallback only when `(input bytes + 1)² × program cost ≤ 16 × 1024 × 1024`; cost includes instructions, rune tables, and capture slots. It checks cancellation before/after scanning and rejects excess work before execution. No detached regex task outlives its phase.

Spooling does not remove decoded-text or regex memory costs. Successful output readers own temporary files until closed; failure/cancellation closes intermediate readers and removes files. Temporary paths never enter configuration, frontend payloads, or history.

## Persistence, Publication, and Frontend Drafts

SQLite stores the master switch/revision in `rewrite_state` and ordered, versioned rule configurations in `rewrite_rules`. New rules and a new database's master switch default off.

Mutations are serialized and require the expected revision: validate/compile → commit transaction → publish immutable snapshot → emit `rewrite:changed` with `{ revision }`. Failures retain the previous snapshot. Unchanged matchers are reused; master toggles write only state, enable/order changes write metadata, and content saves write only changed configurations. Requests neither query SQLite nor compile rules.

| Lifecycle | Behavior |
| --- | --- |
| Startup | One background load, registered after synchronous database consumers; publish only a complete snapshot |
| Before readiness | `GetState` waits with caller cancellation; proxy `Start` waits before binding; mutations are rejected |
| Load failure | Log and return the error to reads/proxy startup; keep the app usable; restart to retry |
| Unsupported configuration | Preserve stored JSON and report an unavailable reason; do not execute or silently delete it |
| Shutdown | Cancel loading, join the task and active mutations, then close SQLite; cleanup is idempotent |

Canceling a read caller does not cancel the shared loader. Database waits/queries are cancellable; JSON decoding and regex compilation finish the current operation before checking cancellation. Async startup still loads all rule bodies and does not reduce total work or memory use.

The frontend fetches rules on first opening the feature. Drafts share immutable strings, cache comparisons per rule, and merge snapshots in one batch. Stale revisions are ignored; clean drafts refresh, dirty drafts survive and flag external conflicts. Saved changes apply to new requests, including on existing connections.

Deleting dirty rules, quitting, and update restart offer save/discard/cancel. Save failures keep drafts and block continuation; backend quit confirmation validates the main-window sender and request identity. Drafts are not crash recovery data. Event subscriptions use their returned disposers.

## Capture, Metrics, History, and Export

Capture uses the final request/response fields, body, URL, Host, and effective protocol. Request metrics come from upstream transport events. Changed responses use actual downstream send events (`responseMetricsSource: downstream`); unchanged responses, including regex no-ops, retain upstream metrics. Upstream EOF during buffering never proves downstream completion.

Use the shared [size and timing conventions](../user-guide.md#timing-sizes-and-har-importexport). `BodySize` counts encoded entity bytes actually sent; failures and incomplete sends retain unknown/incomplete metrics.

Live execution summaries retain the latest **64 actions**, with reasons capped at **512 characters**, and follow traffic retention. HBIN writes v2/reads v1/v2 and stores final content/errors, but not live summaries or the metrics-source label. Shared export writers preserve final fields/metrics and missing-body reporting; exports do not automatically redact sensitive data.

## Transport Dependencies

| Transport API | Contract |
| --- | --- |
| `WithHTTPUpstreamTarget` | Validate original authority; route to the final target while preserving proxy/TLS/cancellation settings. Same-origin edits reuse connections; cross-origin edits own a transport |
| HeaderBlock overrides and request-send observer | Preserve ordered fields and capture the actual emitted request head |
| `ErrDropHTTP` / response-send observer | Terminate failures and report final protocol, headers, timestamps, bytes, and send outcome |
| `AbortHTTPRequestRead` | Interrupt request-body reads; an aborted HTTP/1 body must not advance pipeline parsing |
| `xhttp.FinishResponse` | Finish HTTP/2 body/trailer/END_STREAM writes before reporting completion; idempotent, with no peer-acknowledgement guarantee |

`go.mod` and `go.sum` pin published transport commits. A local `go.work` can use sibling checkouts; standalone builds and tests use `GOWORK=off`.

## Verification

Follow [repository checks](../../AGENTS.md#verification), including rewrite/proxy/history/app tests, relevant race checks, and frontend `test:rewrite-rules` and traffic tests.

Transport tests cover real HTTP/1 and HTTP/2 routing, ordered headers, stream isolation, partial writes, cancellation, and timeout cleanup. Lifecycle tests cover async readiness, first-request enforcement, failures, and shutdown. Desktop acceptance should cover editing, preview, ordering, conflicts, quit/update choices, persistence, and exports.
