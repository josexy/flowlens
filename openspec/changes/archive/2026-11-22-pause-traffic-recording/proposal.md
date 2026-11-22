# Proposal

## Why

Users need to temporarily stop collecting new traffic without interrupting clients that depend on FlowLens. Stopping the proxy currently tears down forwarding and cannot serve this purpose.

## What Changes

- Add a transient recording pause independent of the proxy listener. Requests and connections admitted before pausing finish recording; those admitted while paused only forward, even after recording resumes.
- Replace the toolbar start/stop button with the approved split button: Start Proxy when stopped, Pause or Resume when running, and Stop Proxy in its dropdown. Its width fits the current language and font while remaining stable across states.
- Keep pause state across configuration-driven listener restarts; explicit starts enable recording. Preserve existing toggle-proxy shortcut behavior and add an independently configurable in-app recording command.
- Show recording status persistently and document pause boundaries in both interface languages and the user/technical guides.
- Complete when forwarding, capture admission, in-flight completion, state transitions, UI interactions, and required checks pass.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-capture`: Independent recording admission and the three-state split control.
- `keyboard-shortcuts`: Independent in-app pause/resume command without changing proxy toggle bindings.
- `interaction-conventions`: The split proxy control has no tooltips; other toolbar shortcut hints retain their behavior.

## Impact

`proxy_service` owns admission and lifecycle status; `desktop/store`, `desktop/capture`, and `desktop/shortcuts` own dispatch and presentation. Existing MyGo/nui controls are reused on Windows, macOS, and Linux. No new dependency, settings field, history format, or database migration is needed. Existing records, export, periodic saving, process attribution for admitted traffic, rewrite rules, upstream configuration, TLS handling, and system proxy routing retain their contracts. Request Editor and WebSocket Client result views and Python Workers are unchanged. New pause state is not persisted; no new windows or tray commands are added.
