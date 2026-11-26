# Proposal

## Why

History selection immediately replaces complete details with empty pending panes, so even a fast read flashes the content, tab counts, and response chips. Live capture already has metadata in memory and does not take this intermediate path.

## What Changes

- Retain the last displayed history detail for at most 180 ms while replacing it, with its content and tabs disabled without visual dimming and its close action available.
- Replace the retained view as soon as the selected panes have their complete metadata and any required body. Slow reads release the old view and use the existing loading presentation.
- Keep only one previous detail, do not extend its deadline during rapid selections, and release it on failure, close, deletion, history replacement, or timeout, including when hidden.
- Verify atomic fast transitions, slow and failed reads, body demand, cancellation, cleanup, and both layouts, themes, and languages. Update the user guide and technical reference.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: Stable detail transitions and the bounded exception to immediate selection-cache release.

## Impact

The change belongs to `internal/desktop/store` and `internal/desktop/inspect`, with tests covering the capture integration. It applies equally to Windows, macOS, and Linux; automated acceptance runs with the headless MyGo tester on Windows. No changes to persistent data, HBIN codecs, networking/models, Python workers, process attribution, window lifecycle, shortcuts, localization strings, or dependencies are required. Prefetching whole histories, retaining multiple body caches, and changing live capture are outside scope. Completion requires no blank intermediate frame on fast replacement, no actionable stale content, bounded cleanup, and passing scoped Go/UI checks.
