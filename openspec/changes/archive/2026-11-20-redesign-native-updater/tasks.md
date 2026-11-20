# Tasks

## 1. Update operation and restart

- [x] 1.1 Replace inferred framework phases with one install operation and measured progress; test size completion, delta fallback, cancellation, and failures in app and app_service.
- [x] 1.2 Capture the restart choice, request a guarded restart once on successful completion, suppress it on cancellation/shutdown, and allow restart retry; verify controller/service and existing lifecycle guard tests and document the lifecycle in building.md.

## 2. Native update window

- [x] 2.1 Redesign the window, shared restart choice, and shell status; add matching English/Chinese copy and update user-guide.md; verify window/store/shell interaction tests including hide, retry, and choice retention.
- [x] 2.2 Add meaningful render/layout coverage and inspect both themes and languages at minimum size with long content; run UI vet and locale checks.

## 3. Integration

- [x] 3.1 Run the affected app/service/update/store/shell tests, review the scoped diff and docs, and validate the OpenSpec change.

## Workflow follow-up

- Synchronize the application-updates delta and archive the completed change.
