# Proposal

## Why

Capture and history rows pass complete long paths to the text engine, causing large allocations and stalls when new rows enter view. Ordinary live patches also rebuild whole-list filter/sort maps and native row identities even when membership and order do not change.

## What Changes

- Bound displayed traffic-cell text to a UTF-8 prefix of at most 256 bytes including an ellipsis; retain complete values for filtering, sorting, inspection, copy, and export.
- Reuse main-thread filter/sorter storage, evict obsolete records, and release oversized storage after substantial shrink or reset.
- Refresh native row identities only when ordered IDs change; let native selection validation prune removed IDs.
- Add long-text scrolling, patch allocation, selection, and release regressions and document the display limit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-capture`: bounded cell previews shared by capture and history; clarify that ordinary method/status badges remain complete within the preview limit.

## Impact

Owners: `internal/desktop/capture`, `internal/desktop/logic`, and `internal/desktop/store`. Same main-window behavior on Windows, macOS, and Linux, in both themes and UI languages. No dependency, protocol, persistence, file format, networking, Python Worker, attribution, event, shortcut, or window lifecycle changes; no migrations or new localized messages.

Completion requires targeted and full repository checks, UI lifecycle validation, light/dark rendering, and matched allocation benchmarks demonstrating bounded long-text rendering and lower steady patch allocation. Native process/GPU measurements on other platforms are outside this change; limitations must be reported. Replacing the table, paginating history, and changing original traffic data are non-goals.
