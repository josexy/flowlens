# Live HTTP Rewrite Rules

For setup and examples, see the [user guide](../user-guide.md#live-https-rewrite-rules).

## Scope and Ownership

Rules apply only to live HTTP/HTTPS proxy traffic. Request Editor, resend, imported history, WebSocket, CONNECT tunnels, and raw TCP bypass them, including with `ALL`. Python hooks remain specific to Request Editor.

`rewrite_service` owns persistence, validation, compiled matchers, and snapshots. `proxy_service` applies actions and records final traffic. `backend/app` owns startup/shutdown and quit guards; the `rewriteRules` store owns drafts and revisions.

HTTPS interception may require connecting to the original target and completing TLS, so redirects from unreachable HTTPS endpoints may fail.

## Matching and Execution

Each rule has one action: `redirect`, `request`, or `response`. Match the **original method and complete URL once**, retaining the snapshot and captures for both phases. Rules run in list order within each phase; edits never trigger rematching.

- Apply anti-cache/anti-compression options before request rules. Request actions apply query → headers → body; response actions apply headers → body.
- URL patterns are anchored. Scheme/authority are case-insensitive; path/query preserve case, encoding, and order. `*` captures any text, including empty text; `\*` matches a literal star and `?` is literal. Other backslash escapes are invalid.
- Redirect targets expand `${1}`, `${2}`, etc. without re-encoding captures; `$$` inserts `$`. Targets must be absolute HTTP/HTTPS URLs without user information or fragments and include the complete query.
- Redirects change routing without a 302. The target controls scheme, host, port, and TLS SNI. Host defaults to the target; `preserve` keeps the value immediately before that rule, including HTTP/2 authority.

Header/query operations use ordered arrays: `add` appends; `set` replaces the first match and removes later matches, or appends if absent; `delete` removes all matches. Header names are case-insensitive; decoded query names are case-sensitive. Untouched query fields retain encoding, duplicates, and position.

Headers preserve duplicates, casing, empty values, and interleaving; missing wire order stays unavailable. Host permits only a nonempty request-side `set`; pseudo-headers, encoding, framing, and hop-by-hop fields remain backend-owned. Response rules cannot edit queries. Body changes recalculate framing and remove stale validators, digests, range fields, and incompatible trailers.

**Failures stop the exchange.** Errors identify the rule/action and wrap `mitmproxy.ErrDropHTTP`: HTTP/1 closes without a substitute final response; HTTP/2 aborts only the affected stream. Terminal paths close bodies and cancel owned work.

## Body Processing and Limits

URL/header/query-only edits remain streaming. Body actions buffer finite UTF-8 text, replace it directly or with Go `regexp`/RE2, then re-encode it. Regexes compile before publication and replace all matches, supporting numbered/named captures and `$$`. No match is a successful no-op; matches may leave content unchanged or produce an empty body.

| Resource | Limit |
| --- | --- |
| Encoded input, decoded text, replacement, re-encoded output | **8 MiB each** |
| Concurrent body phases | **4** across requests and responses |
| Phase deadline | **10 seconds**, including slot wait and all body actions |
| Buffered payload | Above **4 MiB**, use managed `body_spool` files |
| Regex match indexes | Approximately **16 MiB**, adjusted for capture count; not a total-memory bound |

Supported encodings: gzip, deflate, br, zstd, and snappy. Body edits reject unsupported encodings/charsets, invalid UTF-8, NULs, SSE/NDJSON/continuous streams, and binary media, validating both original and edited content types. Responses to HEAD or with status below 200, 204, or 304 cannot acquire a body; header-only rules can still apply.

Regex scanning uses a cancellable rune reader that preserves Go matching semantics. Expressions too large for the continuation wrapper use a synchronous fallback, bounded before execution by `(input bytes + 1)² × program cost ≤ 16 × 1024 × 1024` (instructions, rune tables, and capture slots). Cancellation is checked before/after fallback scanning; no regex task outlives its phase.

Spooling does not reduce decoded-text or regex memory costs. Output readers own temporary files until closed; failure/cancellation closes intermediate readers and deletes files. Temporary paths never enter configuration, frontend payloads, or history.

## Persistence, Publication, and Frontend Drafts

SQLite stores the master switch/revision in `rewrite_state` and ordered, versioned configurations in `rewrite_rules`. New rules and a new database's master switch default off.

Mutations serialize and require the expected revision: validate/compile → commit transaction → publish immutable snapshot → emit `rewrite:changed` with `{ revision }`. Failure retains the previous snapshot. Reuse unchanged matchers and write only affected state, enable/order metadata, or configurations. Requests never query SQLite or compile rules.

| Lifecycle | Behavior |
| --- | --- |
| Startup | One background load, registered after synchronous database consumers; publish only a complete snapshot |
| Before readiness | `GetState` waits with caller cancellation; proxy `Start` waits before binding; mutations are rejected |
| Load failure | Log and return the error to reads/proxy startup; app stays usable; restart to retry |
| Unsupported configuration | Preserve JSON and report why it is unavailable; never execute or silently delete it |
| Shutdown | Cancel/join loading and join active mutations before closing SQLite; cleanup is idempotent |

Read cancellation leaves the shared loader running. Database waits/queries are cancellable; JSON decoding and regex compilation check cancellation between operations. Background loading still loads all rule bodies, with the same total work and memory use.

The frontend loads rules on first opening. Drafts share immutable strings, cache comparisons per rule, and merge snapshots in one batch. Ignore stale revisions; refresh clean drafts and retain dirty drafts with conflict flags. Saved changes affect new requests, including on existing connections.

Writes serialize while forms remain editable. A save captures the submitted draft; later edits remain unsaved against the returned baseline. Metadata-only snapshots preserve rule objects, editor state, and previews. Show progress on the active save button/switch and initial/manual loads; event refreshes stay silent.

Deleting dirty rules, quitting, and update restart offer save/discard/cancel. Failed saves retain drafts and block continuation. Backend quit confirmation validates the main-window sender and request identity. Drafts are not crash recovery data; event subscriptions use returned disposers.

## Regex Replacement Preview

`PreviewBody` validates and tests an unsaved regex using the live body-action compiler, bounded matcher, and replacement expansion. One scan returns match count and output, with **8 MiB** sample/output limits, a **10-second** deadline, and caller cancellation. It bypasses URL matching and creates no network traffic, rule mutations, or traffic records.

The panel keeps samples only while mounted and always previews the original sample. Pattern/replacement edits debounce by **300 ms**; obsolete calls are canceled and stale results ignored. The sample editor stays mounted to preserve undo/view state. Pending or failed results cannot be copied; errors reveal the sample.

The read-only inline diff includes whitespace changes and has a **1-second** computation limit. If either input or output reaches **512 Ki characters** total or **128 Ki on one line**, show the complete plain result without diffing or wrapping. Fonts/themes use the existing stores. See the [user guide](../user-guide.md#live-https-rewrite-rules) for controls.

## Capture, Metrics, History, and Export

Capture records final fields, bodies, URL, Host, and protocol. Request metrics use upstream transport events. Changed responses use downstream send events (`responseMetricsSource: downstream`); unchanged responses, including regex no-ops, retain upstream metrics. Upstream EOF during buffering never proves downstream completion.

Use the shared [size and timing conventions](../user-guide.md#timing-sizes-and-har-importexport). `BodySize` counts encoded entity bytes actually sent; failures and incomplete sends retain unknown/incomplete metrics.

Live summaries retain the latest **64 actions**, cap reasons at **512 characters**, and follow traffic retention. HBIN writes v2/reads v1/v2, storing final content/errors without live summaries or the metrics-source label. Shared exports preserve final fields/metrics and missing-body counts; sensitive data is not automatically redacted.

## Transport Dependencies

| Transport API | Contract |
| --- | --- |
| `WithHTTPUpstreamTarget` | Validate original authority; route to the final target while preserving proxy/TLS/cancellation settings. Same-origin edits reuse connections; cross-origin edits own a transport |
| HeaderBlock overrides and request-send observer | Preserve ordered fields and capture the actual emitted request head |
| `ErrDropHTTP` / response-send observer | Terminate failures and report final protocol, headers, timestamps, bytes, and send outcome |
| `AbortHTTPRequestRead` | Interrupt request-body reads; an aborted HTTP/1 body must not advance pipeline parsing |
| `xhttp.FinishResponse` | Finish HTTP/2 body/trailer/END_STREAM writes before reporting completion; idempotent, with no peer-acknowledgement guarantee |

`xhttp.HeaderOrder` orders name groups under normal serialization; exact `xhttp.HeaderBlock` sending supplies and strictly validates every occurrence. Both share HTTP/1 serialization and HTTP/2 HPACK encoding. The HTTP/1 transfer writer generates framing once; ordered trailers need no serialize/parse round trip.

mitmproxy block overrides reconcile occurrences with sanitized values and regenerate protocol-owned fields. HTTP/1 response ordering uses one parsed head and one serialization pass. Received metadata stays separate; bodies remain streaming.

`go.mod` and `go.sum` pin published transport commits. A local `go.work` can use sibling checkouts; standalone builds and tests use `GOWORK=off`.

## Verification

Follow [repository checks](../../AGENTS.md#verification), including rewrite/proxy/history/app tests, relevant race checks, and frontend `test:rewrite-rules` and traffic tests.

Transport tests cover real HTTP/1 and HTTP/2 routing, ordered headers, stream isolation, partial writes, cancellation, and timeout cleanup. Lifecycle tests cover async readiness, first-request enforcement, failures, and shutdown. Desktop acceptance should cover editing, preview, ordering, conflicts, quit/update choices, persistence, and exports.
