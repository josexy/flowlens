# Proposal

## Why

Read-only response buffers retain obsolete text versions, and hidden formatted viewers allocate full copies of large bodies. Opening a saved history also retains every record's complete headers and certificate data even though its table needs only summaries.

## What Changes

- Release obsolete read-only text and response viewer state on replacement while preserving incremental SSE updates.
- Prepare text and formatted body views on demand, respecting the existing large-text threshold and exact copy/save behavior.
- Transfer owned decoded HBIN body bytes into views without a second full allocation.
- Load lightweight history rows, retrieve complete details and action inputs on demand, and cancel obsolete history reads.
- Verify memory retention and compatibility; update memory diagnostics and archive the implemented change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: Bound read-only response retention and avoid materializing unused body views.
- `traffic-history`: Browse lightweight history rows with complete on-demand details and actions.

## Impact

Owners: desktop widgets/inspect/editor/store/capture, history_service, and proxy_service's HBIN reader. All changes use existing Go/MyGo paths on Windows, macOS, and Linux. No on-disk migration, protocol change, dependency upgrade, new settings, copy, or locale keys. Capture, exports, header ordering and truncation, body availability, timestamps, and binary payload semantics remain intact. Python Workers, process attribution, shortcuts, windows/tray, CA trust, and updates are unaffected.

Non-goals: changing capture retention, imposing a process memory limit, periodically forcing GC, changing history files, or paginating away searchable rows. Completion requires regression and memory tests, task verify, strict spec validation, documentation, and archive; native platform acceptance is not inferred from headless tests.
