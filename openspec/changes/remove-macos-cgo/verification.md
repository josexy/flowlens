# Implementation and verification record

Date: 2026-11-20. Toolchain: Go 1.27.3; purego remains v0.11.1.

## Approved implementation decisions

- Implement source changes and available checks on Windows; the user will run
  macOS-native acceptance later. Native-dependent task checkboxes remain open.
- Raise the minimum macOS version from the proposal's original 12.0 to 13.0,
  keeping Go 1.27.3 and all dependency versions. Go 1.27 has discontinued macOS
  12 support; changing linker metadata cannot restore it. See the
  [Go 1.27 Darwin release notes](https://go.dev/doc/go1.27#darwin).

## Implemented source

- Lazy checked native call tables for libproc/AppKit/libdispatch,
  SystemConfiguration/CoreFoundation/Security, and Mach task information.
- Unchanged sysctl socket lookup and PID/start-token format; metadata/icon
  enrichment checks process identity again before publishing.
- Bounded cancellable AppKit dispatch with opaque IDs, no retained canceled
  closures, same-thread autorelease scopes, and a separate main-loop fixture.
- Full binary property-list proxy snapshots, locked full-dictionary comparison,
  authorization cleanup, and guarded compensation after commit/apply failures.
  A failed compensation clears controller ownership.
- Physical footprint sampling using the packed TASK_VM_INFO revision-1 buffer,
  count/range validation, and the existing unavailable/chart semantics.
- Removed cgo imports, Objective-C bridge, no-cgo stubs and production guard.
- Ordinary Task/CI/release use cgo disabled; optional race checks remain separate.
  Added a six-target executable/dependency/Mach-O check and both macOS CI runners.
- Updated build/engineering documentation and explicit macOS 13.0 packaging
  metadata. No version bump, dependency upgrade, model/storage migration, view,
  locale, Python Worker, or traffic codec changes.

## Completed checks

| Environment | Command/check | Result |
| --- | --- | --- |
| Windows amd64 | `CGO_ENABLED=0 task verify` | Pass: formatting, golangci-lint 2.14.0, MyGo UI vet, 1,334 catalog messages, all Go packages |
| Windows amd64 | `CGO_ENABLED=1 go test -race ./internal/pkg/process_attribution ./internal/pkg/systemproxy ./internal/services/mem_stats_service` | Pass, using the installed GCC toolchain |
| Windows amd64 | `go mod tidy`, followed by `go mod tidy -diff` | Only purego moved from indirect to direct; no dependency versions or go.sum changed; repeat tidy has no diff |
| Windows cross-compilation | `CGO_ENABLED=0 go run ./internal/cmd/buildcheck` | All six targets passed with ordinary and `mygo_noinspector` tags; no selected CgoFiles |
| Windows cross-compilation | `go run ./internal/cmd/buildcheck -targets=darwin/arm64,darwin/amd64` after baseline change | Both modes and architectures pass; Mach-O deployment version is 13.0 |
| Windows cross-compilation | `GOOS=darwin GOARCH=arm64/amd64 CGO_ENABLED=0 go test -c` for attribution, systemproxy, memory | All six test binaries compile; none executed on Windows |
| Windows static analysis of Darwin | `GOOS=darwin GOARCH=arm64/amd64 CGO_ENABLED=0 golangci-lint run` on the three native owners | Pass on both architectures |
| Linux amd64, Ubuntu WSL2 | `CGO_ENABLED=0 go test ./...` | Pass after isolating inherited lowercase proxy variables in the environment test |
| Linux amd64, Ubuntu WSL2 | `go mod tidy -diff`, `go tool mygo vet .`, `go tool mygo i18n check` | Pass; tidy produced no diff |
| Windows static analysis of Linux | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 golangci-lint run ./...` | Pass, 0 issues |
| Repository | `git diff --check`; `openspec validate remove-macos-cgo --strict` | Pass |

The first Linux run exposed an existing test's inherited `http_proxy` override.
The test now clears both lowercase proxy variables before setting its explicit
values, and an environment-over-platform regression test was added. The focused
Linux retest and full suite passed. Application environment precedence is unchanged.

The Linux environment has Go but no Task or golangci-lint executable. Downloading
the matching tools failed with a TLS error in WSL; the Windows download stalled
without transferring bytes and was stopped. Consequently Linux `task verify`
itself was not run: Linux tests/UI vet/catalog/tidy ran directly, formatting ran
on the shared source in Windows, and Linux-target lint ran on the Windows host.
The native Linux Task invocation remains part of task 6.1's acceptance record.

## Pending macOS acceptance

No macOS hardware or remote session was available, and no macOS test binary,
AppKit fixture, CI workflow, authorization transaction, bundle or DMG was run
here. Compilation/static analysis cannot verify the native ABI or lifetimes.

Run [the macOS verification procedure](../../../docs/technical/macos-native-verification.md)
on arm64 and amd64 (record Rosetta if used). It includes native libproc/loopback
and AppKit metadata/icons; real CoreFoundation encoding plus injected transaction
failure/ownership tests; positive footprint and 64 MiB memory growth; cross-module
regressions; disposable-network authorization, external edits and restoration;
arm64/universal packaging, both Mach-O slices, macOS 13.0 launch, signed update
artifacts, and light/dark inspection.

Seven task checkboxes are verified locally. The other 21 include native or
packaging acceptance; their source implementations and test paths are present,
but they are intentionally not marked passed. Do not archive or treat this
migration as release-accepted until the pending evidence is recorded.
