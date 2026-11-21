# Tasks

## 1. Body mode availability

- [x] 1.1 Gate text-only modes on text availability, validate active selections, preserve pill-tab selection when available items change, clear hidden text buffers, update the user guide and image failure hints, and cover binary/text/SVG transitions and byte-exact saving with `CGO_ENABLED=0 go test ./internal/desktop/logic ./internal/desktop/inspect ./internal/desktop/widgets`.
- [x] 1.2 Exercise the shared viewer in English/Chinese and light/dark themes, including repeated frames and restored tabs; render with `CGO_ENABLED=0 FLOWLENS_RENDER_DIR=<temporary-directory> go test ./internal/desktop/inspect -run TestRenderBodyModes -count=1` and inspect the images.

## 2. Integration verification

- [x] 2.1 Run `task vet:ui` and `CGO_ENABLED=0 go test ./...`, then compare the implementation, documentation, delta spec, and acceptance evidence; validate with `openspec validate hide-binary-body-raw-tab --strict --no-interactive` and record platform limitations.

## Workflow follow-up

- Synchronize the delta and archive the completed change.
- Run `openspec validate --specs --strict --no-interactive` and inspect the final diff.

## Acceptance evidence

- Windows amd64, Go checks with `CGO_ENABLED=0`: logic, inspect, and widgets tests passed. The transition test detected and now prevents the old pill-tab index from overriding a valid Hex selection.
- `TestRenderBodyModes` passed; reviewed 12 screenshots covering image, binary Hex, and textual SVG Raw in English/Chinese and light/dark themes. Images are in the system temporary directory under `flowlens-body-modes`.
- `task vet:ui`, `task i18n:generate`, `task i18n:check`, and strict change validation passed. Catalogs retain matching keys and placeholders.
- Final `CGO_ENABLED=0 go test ./...` passed across all packages after the selection and catalog fixes. Reviewed the code, user guide, and delta for consistent availability, fallback, image-failure guidance, and original-byte saving behavior; `git diff --check` passed.
- Validation uses MyGo's headless UI tester; native macOS/Linux runtime tests were not run. No platform-specific behavior or APIs changed.
