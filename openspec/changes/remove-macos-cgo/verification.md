# Implementation and verification record

Date: 2026-11-20. Initial and CI toolchain: Go 1.27.3. Local macOS follow-up:
Go 1.27.4; `go.mod` still declares 1.27.3 and purego remains v0.11.1.

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

## macOS follow-up and CI evidence

The follow-up ran on an Apple M1 with macOS 15.7.4 (24G517), with arm64
execution and amd64 execution under Rosetta. All Go commands below used
`CGO_ENABLED=0`. The original lack of macOS access above describes the first
implementation session; it no longer describes the available native evidence.

The [CI run for commit 1151780](https://github.com/josexy/flowlens/actions/runs/44693930436)
was inspected, including its logs and individual step results:

| Runner | Runtime evidence |
| --- | --- |
| `macos-latest`, macOS 26.7.2 (25G332), darwin/arm64 | Module tidy, formatting, UI vet, catalogs, `go test ./...`, and the separate AppKit main-loop fixture passed on the native arm64 runner |
| `macos-15-intel`, macOS 15.7.9 (24G830), darwin/amd64 | The same checks passed on the Intel runner; this is separate from local Rosetta execution |
| `ubuntu-latest`, Ubuntu 26.04, linux/amd64 | Module tidy, formatting, UI vet, catalogs, and all Go tests passed; this workflow does not invoke Task or golangci-lint |
| Linux executable-build job | All six targets passed ordinary/production builds, no-cgo dependency selection, and Darwin minimum-version checks |

These logs cover the existing implementation. The additional native proxy
regressions added during this follow-up were run locally on both architectures.

| Local command/check | Result |
| --- | --- |
| `PATH="/tmp/flowlens-cgo-tools:$PATH" CGO_ENABLED=0 task verify` | Pass: formatting, golangci-lint 2.14.0 (0 issues), UI vet, 1,334 messages, all Go packages |
| `CGO_ENABLED=0 go mod tidy -diff` | Pass, no module changes |
| Cross-module command from task 6.2, with `-count=1 -json` | All ten packages passed; 1,278 test/subtest passes. The AppKit fixture was deliberately skipped here and passed separately; the opt-in `TestCaptureSoak` was not requested |
| `CGO_ENABLED=0 FLOWLENS_APPKIT_TEST=1 go test ./internal/pkg/process_attribution -run TestDarwinAppKit -count=1 -timeout=2m -json` | Both tests passed: known bundle name/ID, 20 metadata iterations with bundle/executable PNG fallback, and representation ownership on success/failure |
| `CGO_ENABLED=0 GOARCH=amd64 go test ./internal/pkg/process_attribution ./internal/pkg/systemproxy ./internal/services/mem_stats_service -count=1 -json` | Passed under Rosetta: 141 test/subtest passes, with only the separately executed main-loop fixture skipped |
| Same AppKit command with `GOARCH=amd64` | Both tests passed under Rosetta, no skips |
| `CGO_ENABLED=0 go test ./internal/pkg/systemproxy -count=1 -json`, then with `GOARCH=amd64` after adding regressions | 59 test/subtest passes on each architecture, no skips. Real CF dictionaries now explicitly cover HTTP/HTTPS/SOCKS precedence, absent/invalid host and enable fields, invalid configuration types, IPv4 priority, IPv6 fallback, and invalid/missing service IDs |
| `CGO_ENABLED=0 /tmp/flowlens-cgo-tools/golangci-lint run ./internal/pkg/systemproxy` after the test changes | Pass, 0 issues |
| Source audit | No FlowLens `import "C"`, `#cgo`, cgo build constraints, `.m` or `.h` bridge files |

The system golangci-lint was too old to parse the version-2 configuration, so
the first Task invocation stopped at config validation. Version 2.14.0 was
installed into `/tmp/flowlens-cgo-tools` for this check without replacing the
user's executable; the complete Task rerun passed. The final code changes are
test-only and received the focused two-architecture rerun and lint above.

The native suites exercise `proc_bsdinfo` offsets and libproc failures, current
and exited process identities, real IPv4/IPv6 loopback lookup, PID reuse, queue
cancellation/late callbacks and Manager replacement. Proxy tests distinguish
CF pointer constants from callback-structure addresses, preserve complete
binary dictionaries, and count owned references through injected authorization,
lock, service, serialization, commit, apply and recovery failures. Real system
proxy discovery/snapshot was read-only; no native network configuration was
mutated. Mach tests returned positive physical footprint and passed the 64 MiB
touched-memory growth check on both architectures, plus invalid-response cases.

## Package and interface follow-up

Built without upload using:

```sh
CGO_ENABLED=0 MYGO_LDFLAGS=-macos=13.0 go tool mygo build \
  -platform darwin/arm64,darwin/universal -o /tmp/flowlens-cgo-packages
```

Both `.app` bundles and DMGs were generated successfully. `lipo -archs`
reported arm64 for the first bundle and arm64/x86_64 for universal. `otool
-arch all -l` reported `LC_BUILD_VERSION minos 13.0` in all slices, and
`PlistBuddy` reported `LSMinimumSystemVersion=13.0` in both bundles.
`codesign --verify --deep --strict` passed for both ad hoc signatures;
`hdiutil verify` passed for both DMGs. Every generated file named by
`SHA256SUMS` matched its checksum in the target directories, including both
copies of `install.sh`. An initial `shasum -c` from the parent directory could
not locate the target-local files; checking the actual target paths passed.

The build reported that no update private key was configured. It therefore
did not generate signed update archives/manifests. Ad hoc bundle signing is
not evidence of update signature or Developer ID/notarization acceptance.

Both bundles launched on this macOS 15.7.4 arm64 host and exited normally with
Command-Q; process checks confirmed their termination. The arm64 bundle showed
positive Memory values (about 79 MiB) separately from Go heap and a live chart
in light/dark themes. A delayed local HTTP fixture requested through the
loopback proxy by `/usr/bin/curl` returned 200; the capture table showed `curl`
with its executable fallback icon in both themes. The managed-system-proxy
indicator correctly showed disabled throughout. Universal launch also showed
positive Memory (about 79 MiB). No layout or locale changes were needed for
these observed states.

The inspection used the local profile and added one capture history containing
`/flowlens-native-verification`. The theme was restored to light, sampling was
paused, the proxy was stopped, and both validation apps were closed. System
network settings were not changed.

## Review follow-up (2026-11-20, Linux arm64)

A branch review found one regression against the cgo driver. When the managed
network service disappeared, the original `Matches` compared the current empty
configuration with FlowLens's write and returned a mismatch, so the controller
reported `ErrChangedExternally` and cleared ownership. The port returned a
"service disappeared" error instead, which the controller passes through without
clearing: management stayed active and could not be disabled until restart.

The fix adds `errDarwinServiceMissing`. `Matches` maps it to a mismatch, and the
locked write transaction maps it to `ErrChangedExternally` before any write.
Snapshot still fails when the primary service is absent from preferences.
`TestDarwinProxyMissingServiceEndsOwnership` covers `Matches` and `Restore`
after removal, and the `service` transaction failure now asserts
`ErrChangedExternally` with no commit. The fixture fakes unauthorized
`SCPreferencesCreate` so read paths stay under reference counting. The native
procedure's service-removal step now expects a conflict and cleared ownership.

These Darwin changes were **compiled, vetted and linted only**; they were not
executed. Rerun `CGO_ENABLED=0 go test ./internal/pkg/systemproxy -count=1` on
arm64 and amd64 macOS (or the CI runners) before accepting them.

Host: Debian 13, Linux 6.12 arm64, Go 1.27.3, Task 3.54.0, golangci-lint 2.15.0.
All commands used `CGO_ENABLED=0`.

| Command/check | Result |
| --- | --- |
| `task verify` | Formatting, golangci-lint (0 issues), UI vet and catalogs passed. `go test ./...` failed only `internal/desktop/theme` `TestCustomTextMatchesControlsAcrossSizesAndDPI` |
| The same theme test on an unmodified `main` worktree | Fails the same way. The host has no Arial (`fc-match` substitutes DejaVu Sans), so hinted widths do not scale exactly. Pre-existing and unrelated; the branch changes no `internal/desktop` file |
| `go mod tidy -diff` | No diff |
| `go run ./internal/cmd/buildcheck` | All six targets, ordinary and `mygo_noinspector`, no selected CgoFiles, Darwin Mach-O minimum 13.0 |
| `GOOS=darwin GOARCH=arm64/amd64` vet, `go test -c` and golangci-lint for systemproxy, attribution and memory | Pass, 0 issues; compile-only |
| `go build -ldflags=-macos=13.0` for windows/amd64 and linux/arm64 | Pass; the release workflow's global `MYGO_LDFLAGS` does not break non-Darwin builds |
| Source audit, `git diff --check`, `openspec validate remove-macos-cgo --strict` | Pass |

## Remaining acceptance

24 of 28 tasks now have their stated evidence. The remaining tasks are:

- **6.1:** Local macOS `task verify` and tidy passed before the review fix; earlier
  Windows checks and Linux CI remain valid. Native Linux `task verify` now runs
  every stage. Its test stage fails only on a pre-existing, font-dependent theme
  test that fails on `main` as well, so it is not a pass. The systemproxy
  change from the review needs a native macOS rerun on both architectures.
- **6.4:** The user confirmed no disposable macOS network environment is
  available. Real authorization denial, service removal, auxiliary-key edits,
  compensation and owned restoration on shutdown remain untested there.
  Injected transaction tests do not replace this acceptance.
- **6.5:** Bundle/DMG integrity, both architecture slices and current-host launch
  passed. No macOS 13.0 runtime environment or configured update-signing key is
  available; minimum-OS feature execution and signed update artifacts remain
  pending. Universal was launched as arm64, not as an amd64 application.
- **6.6:** Positive Memory, process names/icons, and the disabled managed-proxy
  indicator were inspected in light/dark themes. Packaged active/conflict proxy
  states and unavailable/placeholder states have not been exercised; existing
  automated view tests passing does not count as their packaged visual review.

Continue with [the native verification procedure](../../../docs/technical/macos-native-verification.md)
when the missing environments are available. Keep this change unarchived and
the PR in draft until the remaining acceptance is complete.
