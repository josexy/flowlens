# Spec Delta

## MODIFIED Requirements

### Requirement: Proxy control
The capture toolbar SHALL start and stop the proxy and show whether it is running and on which address. A compact split control SHALL fit its localized labels and keep a stable width across states: Start Proxy when stopped, Pause while recording, and Resume while paused. When running, its dropdown SHALL offer Stop Proxy. Recording state SHALL be expressed by the split control, without an extra label beside proxy status; pending operations SHALL disable conflicting actions.

#### Scenario: Starting the proxy
- **WHEN** the user starts the proxy with a valid configuration
- **THEN** the toolbar shows the running state and listening address, and recording is enabled

#### Scenario: Start failure
- **WHEN** the proxy cannot start, such as when the port is in use
- **THEN** the error is shown and the toolbar stays in the stopped state

#### Scenario: Stop from the split menu
- **WHEN** the user chooses Stop Proxy while recording is active or paused
- **THEN** the proxy stops and the control returns to Start Proxy without clearing existing traffic

#### Scenario: Accessible split control
- **WHEN** the user operates the control by keyboard in either theme and interface language
- **THEN** its primary action and dropdown are separately focusable, clearly named, and perform the same operations as pointer activation

## ADDED Requirements

### Requirement: Pause recording independently of forwarding
Pausing recording SHALL stop admission of new captured exchanges without stopping or reconfiguring the proxy, changing system proxy settings, or interrupting client forwarding. Existing records SHALL remain readable, savable, and exportable. Resuming SHALL admit new exchanges in the same capture session without recovering traffic skipped during the pause.

#### Scenario: HTTP and HTTPS during a pause
- **WHEN** new HTTP or HTTPS requests use the paused proxy, including requests on an existing keep-alive connection
- **THEN** requests and responses are forwarded using the existing TLS, upstream, rewrite, and live-option settings without creating traffic records, captured bodies, capture metrics, or record attribution

#### Scenario: New WebSocket or TCP connection during a pause
- **WHEN** a new WebSocket or raw TCP tunnel is admitted while paused
- **THEN** traffic continues to forward without a capture record, and resuming does not start recording that already-admitted connection

#### Scenario: Exchanges admitted before pausing
- **WHEN** recording is paused with already-captured HTTP requests, SSE responses, or WebSocket connections in progress
- **THEN** those exchanges continue forwarding and recording through completion with their original metrics and body semantics

#### Scenario: Concurrent pause and admission
- **WHEN** pause or resume overlaps admission of a new exchange
- **THEN** that exchange receives exactly one admission decision and cannot appear later as a partial or resurrected record

### Requirement: Recording pause lifecycle
Recording pause SHALL be transient. Explicitly starting a stopped proxy SHALL enable recording. A configuration-driven listener restart SHALL preserve its previous recording state before accepting traffic. Clearing or restarting the capture session SHALL preserve the recording state. Pause and resume SHALL be unavailable when the proxy is stopped, and failed operations SHALL report the authoritative state.

#### Scenario: Internal restart while paused
- **WHEN** a configuration change restarts a paused proxy
- **THEN** its new listener starts paused with no interval in which new exchanges are recorded

#### Scenario: New capture while paused
- **WHEN** the user clears traffic or restarts the capture session while paused
- **THEN** the normal cleanup and late-callback protections apply and recording remains paused

#### Scenario: Explicit stop and start
- **WHEN** the user stops a paused proxy and explicitly starts it again
- **THEN** recording is enabled

#### Scenario: Application restart
- **WHEN** FlowLens starts again after a previous run was paused
- **THEN** the pause is not restored from settings or history
