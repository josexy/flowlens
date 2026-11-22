# Design

## Context

See proposal.md for motivation. All intercepted HTTP, WebSocket, and TCP entries use the proxy service's serialized registration path. The HTTP interceptor also performs rewrite and forwarding work, which must continue for unrecorded requests. WebSocket interception must keep consuming and invoking frames even without a capture entry. The current desktop proxy store already owns lifecycle jobs and status updates; nui provides FieldGroup and DropdownMenu.

## Goals / Non-Goals

**Goals:** Make a single admission decision per exchange, preserve complete in-flight data as explicitly selected by the user, and reproduce the approved control using native components.

**Non-Goals:** Freezing existing exchanges, reconnecting streams, recovering skipped traffic, changing protocol handling, or persisting pause state.

## Decisions

- The proxy service owns a recording-paused flag. Pause transitions serialize with lifecycle operations and traffic registration. The registration lock linearizes pause against initial ID allocation and publication; no network or body I/O is placed under it. Paused admission returns no entry and a no-op producer release. Already registered entries keep their existing generation and lifetime.
- HTTP forwarding and rewrite handling remain in the existing interceptor, with capture-only work conditional on admission. No body capture, timing observers, entry attribution, or frontend events are installed for an unrecorded request. WebSocket loops always invoke and release frames; rejected handshakes have no entry or message storage for the lifetime of the connection. Raw TCP admission uses the same gate.
- Explicit Start enables recording. A service Restart operation serializes Stop/Start and restores the chosen recording state before starting the new listener. This avoids a transient recording interval during configuration restarts; UI-only restoration after Start would be too late. Stopping and capture reset retain the existing cleanup contracts.
- ProxyStatus adds transient RecordingPaused state and a monotonic lifecycle/recording revision, delivered through the existing status channel. The desktop store rejects older revisions and runs recording/lifecycle changes through its shared guarded runtime job. Status callbacks update view state on the main thread. Existing event unsubscription and job teardown stay in place.
- The toolbar builds a nui FieldGroup containing a text button and, while running, a dropdown arrow. Its stable width is measured from the current localized labels and font metrics, reserving the arrow only for the running states. English action labels are Pause and Resume. Start and Resume use primary solid styling; Pause uses neutral outline. Stop is a danger menu item. The proxy indicator stays green while paused; the split button expresses recording state without extra status text beside the address. Standard nui focus/menu behavior handles keyboard input; business actions share the command dispatcher.
- Both locale catalogs carry matching short button, status, and menu copy. The split control's primary action and dropdown arrow do not show tooltips; the stop menu retains the resolved proxy shortcut. The user guide explains the action, and the technical guide defines admission, stream lifetime, persistence, and export behavior. No TrafficEntry/HBIN field is added, so history format and timestamp/unknown-value semantics are unchanged.

## Risks / Trade-offs

- Existing streams can continue producing records for a long time while paused -> document this user-selected boundary and cover HTTP/SSE/WebSocket completion in regression tests.
- Skipping WebSocket callbacks would block frames -> keep a forwarding loop for every admitted handshake, with capture work conditional on the original decision.
- Pausing near registration or restarting could leak records -> serialize admission and lifecycle transitions and exercise them with controlled concurrent tests.
- Toolbar width and localized text can crowd quick settings -> verify both locales/themes with native render tests and keep the two segments within one stable group width.

## Migration Plan

No data migration, dependency update, or settings change. New state starts unpaused in each process. Rollback removes only this transient behavior; stored traffic remains compatible.
