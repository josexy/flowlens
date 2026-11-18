# Proposal

## Why

Clearing a capture drops its store data while hidden detail viewers still retain body text and editor buffers. A reproduction retained 33 MiB after clearing an approximately 8 MiB response and running GC; releasing the panel removed it.

## What Changes

- Release inspection caches when a capture is cleared or restarted, a selected entry changes or disappears, or details close.
- Release derived capture and history lists when their source is reset, including hidden filters, tables, and categories.
- Cancel obsolete viewer jobs and discard stale results; preserve view preferences and normal reopening.
- Check related message dialogs and body replacement for retained payloads.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: specify cache release when the inspected selection becomes obsolete.

## Impact

Changes are confined to desktop stores, capture/inspection views, app wiring, tests, and the user guide. Persistence formats, networking, Python Workers, process attribution, shortcuts, and window appearance stay compatible. No forced GC or new dependency is required.
