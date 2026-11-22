# Spec Delta

## ADDED Requirements

### Requirement: Independent recording command
FlowLens SHALL expose a configurable in-application Toggle Recording command with no default binding. It SHALL pause or resume admission of captured exchanges only while the proxy is running and no conflicting proxy operation is pending. Existing application and global Toggle Proxy commands SHALL continue to start and stop the listener.

#### Scenario: Recording shortcut
- **WHEN** the user invokes Toggle Recording on a page exposing the proxy toolbar while the proxy is running
- **THEN** it performs the same pause or resume action as the primary split button without stopping the proxy

#### Scenario: Stopped proxy
- **WHEN** Toggle Recording is invoked while the proxy is stopped
- **THEN** the proxy remains stopped and the command does not run

#### Scenario: Existing proxy binding
- **WHEN** the user invokes an existing Toggle Proxy binding while recording is paused
- **THEN** the proxy stops, rather than resuming recording
