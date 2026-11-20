# Proposal

## Why

FlowLens currently requires cgo on macOS for process identity and icons, system proxy discovery and management, and physical-footprint sampling. Disabling cgo silently removes these features in development and deliberately breaks release builds, although the pinned MyGo stack already supports macOS through Go bindings to native APIs.

## What Changes

- Replace the four FlowLens Go files that import `C` and the process-attribution Objective-C bridge with Go implementations using the already-pinned `github.com/ebitengine/purego` and its Objective-C support where needed.
- Define the target as complete macOS functionality with `CGO_ENABLED=0`: no C/Objective-C compilation, generated cgo wrappers, or replacement native helper library. macOS system frameworks remain runtime dependencies.
- Preserve asynchronous local process attribution, PID/start-token identity, application metadata and icons, upstream-proxy selection, guarded managed-proxy restoration, and the physical-footprint memory measure.
- Remove the no-cgo feature stubs and release failure guard after their functional replacements are verified. Run ordinary Task, CI, and release builds with cgo disabled on all desktop platforms.
- Set the minimum macOS version to 13.0, as approved during implementation to match the pinned Go 1.27 toolchain; retain arm64 and amd64 support, universal packaging, signing, and existing release artifacts. Distinguish compilation requirements from macOS packaging tools and race-detector requirements.
- Replace tests that expect no-cgo unavailability with functional coverage and add native ABI, ownership, cancellation, and proxy-transaction regression checks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `build-and-release`: Replace the macOS cgo requirement and intentional release failure with a no-cgo build contract that includes process attribution and icons, system proxy discovery and control, and physical-footprint sampling; update toolchain and verification requirements accordingly.

The existing `traffic-capture` and `memory-statistics` user-facing contracts remain unchanged. Their behavior must also hold in builds with cgo disabled, as required by the updated build contract.

## Impact

- Implementation owners: `internal/pkg/process_attribution`, `internal/pkg/systemproxy`, and `internal/services/mem_stats_service`; narrowly scoped application lifecycle integration if needed for AppKit dispatch.
- Build and dependency files: `cgo_guard_darwin.go`, `go.mod`/`go.sum` as needed, `Taskfile.yml`, `.github/workflows/ci.yml`, and `.github/workflows/release.yml`. Promote the existing purego dependency to direct use without a dependency upgrade.
- Documentation: `README.md`, `docs/technical/building.md`, and the macOS build rule in `AGENTS.md`. User-guide terminology, settings, localization keys, and UI layout should remain stable.
- No migration of SQLite data, API Collection files, HBIN traffic records, process models, icon keys, or settings is planned. No change is planned to xhttp networking, Python Workers, CA trust commands, shortcuts, update signatures, or window behavior.
- Native execution on macOS is required before implementation is complete; the Windows planning environment can inspect source and dependency selection but cannot establish native runtime correctness.
