# Design

## Context

See proposal.md for motivation. HBIN uses length-prefixed strings and ordered fields; v1-v3 share these header layouts. The history service currently decodes a complete record, then strips it into a summary. Detail and action reads already have a separate complete-record path.

## Goals / Non-Goals

**Goals:** Make summary allocations independent of skipped payload sizes while keeping exact summary semantics and decoder validation.

**Non-Goals:** Reduce disk traffic by seeking, change persistence, paginate the list, or redesign views and lifecycle jobs.

## Decisions

- Add an explicit summary entry point in the proxy codec, sharing entry, message, TLS, and certificate field traversal with complete decoding. Retain the existing full API for all callers that need details. This avoids a second independent record parser.
- Header decoding has full, first-Content-Type, and discard modes. In summary mode use the encoded name length and a fixed Content-Type-sized buffer to recognize the first matching field without allocating arbitrary header names. Preserve its original case and even an empty first value. Read flags and enforce counts in every mode.
- String and string-list readers can consume lengths and discard bounded chunks without creating the corresponding payload strings or slices. TLS retains only selected ALPN for TCP; certificate and name objects are only returned when retaining details. Preserve the full decoder's presence semantics.
- Consume bytes instead of seeking so truncation and reader errors remain observable. Buffered history readers continue checking context on underlying reads. The existing runtime jobs, storage locks, selection tokens, and main-thread publication remain unchanged.
- Replace the service's decode-then-strip path with the shared summary decoder. Compare actual decoded summaries with the previous summary semantics, whose table parity tests cover filters, sorting, copying, and actions; keep full-detail and export paths unchanged.

## Risks / Trade-offs

- Skipping bytes still reads them through a bounded buffer; this reduces allocations and GC work, not necessarily I/O or Task Manager memory immediately.
- Shared-code changes could alter full details or alignment; verify both modes across v1-v3, all truncation points, malformed header flags/counts, nil/empty collections, and exact full content after summary reads.
- Summary rows and required strings still scale with record count and their own sizes; no constant-memory claim is made for the whole list.

## Migration Plan

No format or data migration. Rollback selects the former full-read path without touching history files. Tests run on Windows with CGO disabled; UI layout, theme, copy, and native platform behavior are unaffected.
