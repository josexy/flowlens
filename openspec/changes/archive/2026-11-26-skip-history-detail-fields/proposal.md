# Proposal

## Why

History rows already release unneeded details, but opening a history still allocates every Cookie, certificate, and TLS field before discarding it. Selective decoding should avoid those transient allocations while preserving the complete table and on-demand details.

## What Changes

- Skip header values other than the first Content-Type, all trailers, certificates, and unused TLS fields during history-list decoding.
- Keep table/filter/action fields identical, including original Content-Type casing, message flags, metrics, process data, and TCP selected ALPN.
- Preserve full decoding for inspection, cURL, editing, resend, and export; retain existing errors and cancellation while skipping.
- Complete when v1-v3 parity and corruption tests pass and allocation measurements demonstrate that skipped payload size does not drive scan allocations.
- Non-goals: file-format changes, paging, body decoding changes, new caches, forced GC, or UI changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-history`: Bound initial list allocations for fields only needed by details, while retaining compatibility and failure behavior.

## Impact

The proxy service owns shared HBIN decoding and the history service selects the summary path. Update the memory diagnostics and traffic format references. The same reader logic runs on Windows, macOS, and Linux; no storage migration, writer, protocol, network model, process attribution, Python Worker, event/shortcut, window, or localization changes are needed. Existing UI state and cancellation ownership stay in place.
