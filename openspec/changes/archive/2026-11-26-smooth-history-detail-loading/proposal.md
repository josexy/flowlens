# Proposal

## Why

History selection replaces the whole detail panel with a spinner and waits for body decompression before showing headers. Even quick reads can flash a loading frame, while large bodies unnecessarily delay metadata inspection.

## What Changes

- Publish complete history metadata independently of bodies; request payloads only for Raw, Body, or WebSocket views. Complete action reads stay unchanged.
- Preserve the split layout and noninteractive tab labels during metadata reads, without retaining old records or payloads.
- Delay loading indicators by 180 ms, with no minimum content delay; keep close available and reject stale results after selection changes or closure.
- Complete when staged reads, fast/slow transitions, cancellation, memory release, and detail actions pass regression checks and light/dark renders are inspected.
- Non-goals: persistence or capture protocol changes, full-record caches, retained previous bodies, new windows, or changing live capture subscriptions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-history`: Make complete metadata available before payloads and load inspection bodies on demand.
- `traffic-inspection`: Stabilize the loading layout and suppress indicators for quick reads.

## Impact

Owners: history service, desktop store, inspect views, and their tests. Update the user guide and memory/traffic technical references. HBIN v1-v3, persisted data, protocols, process attribution, Python Workers, shortcuts, and action inputs are unchanged. Existing localized labels and theme controls cover both languages and light/dark themes on all desktop platforms; no new copy or platform APIs are needed.
