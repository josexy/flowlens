# Tasks

## 1. ICO preview

- [x] 1.1 Add the pure-Go decoder and ICO preflight checks in the existing image path; cover PNG/DIB transparency, multi-image files, malformed ranges/dimensions, exact Hex/save bytes, and ICO filename suggestions with `CGO_ENABLED=0 go test ./internal/desktop/inspect ./internal/desktop/logic`; update the user guide.
- [x] 1.2 Verify the reported Baidu favicon and inspect light/dark renders using `CGO_ENABLED=0 FLOWLENS_RENDER_DIR=<temporary-directory> go test ./internal/desktop/inspect -run ICO -count=1`; record actual dimensions and results.

## 2. Integration

- [x] 2.1 Run `task vet:ui`, `CGO_ENABLED=0 go test ./...`, and `openspec validate support-ico-preview --strict --no-interactive`; compare implementation, docs, and acceptance evidence, recording platform limits.

## Workflow follow-up

- Synchronize and archive the completed change, then run `openspec validate --specs --strict --no-interactive` and review the final diff.

## Acceptance evidence

- Windows amd64, `CGO_ENABLED=0`: inspect and logic package tests passed, including PNG and 1/4/8/24/32-bit DIB icons, composited alpha/mask pixel checks, two ICO MIME types, multi-image ordering, 256-pixel dimensions, invalid ranges/oversized dimensions, Hex/save bytes, and `.ico` filename suggestions.
- Downloaded the reported `https://www.baidu.com/favicon.ico` to the system temporary directory for a one-off integration check: decoded successfully at 64x64, 16,958 bytes; saving preserved every byte. The temporary test source was removed and no network-dependent fixture was added to the test suite.
- Reviewed light/dark renders of the actual favicon and synthetic PNG/DIB icons in `flowlens-ico-renders` under the system temporary directory.
- `task vet:ui`, strict change validation, and `git diff --check` passed. No new message keys are needed.
- Final `CGO_ENABLED=0 go test ./...` passed across all packages, including existing raster/SVG/AVIF rendering and image lifecycle tests. Implementation and user guide match the new ICO requirement.
- Tests use the headless MyGo UI tester. Native macOS/Linux runtime acceptance was not performed; the implementation introduces no native dependencies or platform branches.
