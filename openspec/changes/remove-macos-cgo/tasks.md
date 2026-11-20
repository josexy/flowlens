# Tasks

Task checkboxes include their stated verification. Source implementation and the available macOS-native follow-up are complete; tasks 6.1, 6.4, 6.5, and 6.6 retain missing acceptance checks. The user confirmed no additional disposable-network or macOS 13.0 environment is available. Both native CI architectures, local arm64/Rosetta tests, package integrity, and the partial packaged-interface inspection are recorded in [verification.md](verification.md). The approved minimum macOS version is 13.0 to match Go 1.27. Use `CGO_ENABLED=0` for ordinary checks below; run macOS binaries on macOS and identify compile-only evidence explicitly. Follow the binding ownership and native-test strategy in [design.md](design.md).

## 1. Establish native binding conventions

- [x] 1.1 Promote the pinned `github.com/ebitengine/purego` v0.11.1 dependency to direct use when the first binding lands; verify `go mod tidy` produces only expected metadata changes and no dependency versions change.
- [x] 1.2 Add private, lazily initialized binding tables in the three owning packages, using checked symbols, explicit native widths, and process-lifetime library handles; verify missing-library/symbol tests return feature errors and Darwin arm64/amd64 compile checks pass.
- [x] 1.3 Document native signatures, data-symbol interpretation, and ownership next to each binding; verify focused tests distinguish CF pointer constants from callback-structure addresses and validate all struct/buffer sizes and offsets used on both Darwin architectures.

## 2. Migrate process identity and icons

- [x] 2.1 Replace the libproc cgo boundary with typed Go bindings while retaining the sysctl socket lookup and `seconds:microseconds` start tokens; verify current-process identity, short native responses, exited processes, permission failures, and PID-reuse handling in `./internal/pkg/process_attribution`.
- [x] 2.2 Implement the bounded AppKit main-queue adapter with opaque request IDs, same-thread autorelease cleanup, and context cancellation; verify canceled, late, already-main-thread, absent-run-loop, rotation, and shutdown cases terminate without stale publication or retained requests.
- [x] 2.3 Port running-application/bundle metadata and icon fallback/PNG conversion into Go bindings; verify a macOS main-loop test process reads known fixture metadata and decodes a non-empty icon, exercises fallback paths, and releases owned native references on success and failure.
- [x] 2.4 Remove `bridge_darwin.m`, `bridge_darwin.h`, and the Darwin no-cgo provider after replacement, enable Darwin provider tests without cgo, and replace C allocation-counter/unavailability assertions with binding ownership and functional tests; verify no attribution source imports `C` and native IPv4/IPv6 loopback, cache-refresh, Manager, and icon-cache tests pass.
- [x] 2.5 Document the package's AppKit dispatch, test run-loop, and cancellation rules beside the implementation, wiring lifecycle only through `internal/app` if required; verify `go tool mygo vet .` and affected application shutdown tests pass and no desktop view state is retained by native callbacks.

## 3. Migrate system proxy discovery and management

- [x] 3.1 Replace the Darwin proxy reader with SystemConfiguration/CoreFoundation Go bindings; verify boolean/numeric flags, missing or wrongly typed values, Unicode/IPv6 hosts, environment precedence, HTTP/HTTPS/SOCKS ordering, and cleanup using native and injected-value tests in `./internal/pkg/systemproxy`.
- [x] 3.2 Port primary-service lookup, complete binary property-list snapshots, and dictionary equality; verify IPv4/IPv6 service fallback, missing service/configuration, empty dictionaries, unknown keys, bypass lists, PAC values, and dictionary round trips without altering snapshot semantics.
- [x] 3.3 Port authorization and locked apply/restore transactions, preserving the existing controller and error contracts; verify HTTP/HTTPS and SOCKS5 settings, authorization denial, lock failure, and external edits between the initial check and locked comparison with focused driver tests.
- [x] 3.4 Add failure coverage for serialization, commit, apply-after-commit, and guarded recovery; verify failed operations do not falsely report active ownership, compensation never overwrites external edits, and every branch releases locks, authorization state, and owned CF references.
- [x] 3.5 Remove the Darwin no-cgo driver after functional replacement and document native ownership/transaction boundaries in the package; verify existing controller/URL tests plus a native read-only discovery/snapshot smoke test pass with cgo disabled. Keep tests that mutate network settings outside ordinary unattended tests.

## 4. Migrate process physical-footprint sampling

- [x] 4.1 Bind the current-task port and `task_info(TASK_VM_INFO)` in Go with the documented footprint-bearing layout and count; verify native positive `MeasurePhysFootprint` samples on arm64 and amd64 and injected truncated-count, Mach-error, and out-of-range responses produce unavailable values.
- [x] 4.2 Remove the Darwin no-cgo sampler, replace its unavailability-only test, and enable `process_memory_growth_test.go` for all Darwin builds; verify `CGO_ENABLED=0 go test ./internal/services/mem_stats_service` passes, including the 64 MiB touched-memory check and existing chart/sample semantics.
- [x] 4.3 Document Mach count units, packing, field provenance, and Activity Monitor correspondence next to the sampler; verify the user guide's existing physical-footprint description and exclusion of Python Workers remain accurate without adding user-visible implementation copy.

## 5. Switch build policy and documentation

- [x] 5.1 After all three functional migrations pass, remove `cgo_guard_darwin.go` and obsolete cgo build constraints; verify source searches find no FlowLens cgo imports/directives/Objective-C bridge and both production-tagged Darwin executable builds succeed with cgo disabled.
- [x] 5.2 Set ordinary Task and both GitHub workflow paths, including release verification and packaging, to `CGO_ENABLED=0` and remove obsolete C compiler/linker flags; verify rendered Task commands/workflow environments consistently select no-cgo builds and keep race checks separately configured for the pinned Go toolchain.
- [x] 5.3 Add executable compile checks for Windows amd64/arm64, Linux amd64/arm64, and Darwin arm64/amd64, including `mygo_noinspector`; verify every target builds with cgo disabled and the selected application dependency graphs contain no `CgoFiles`.
- [x] 5.4 Enable functional macOS tests in CI and provide repeatable commands for both architecture and main-loop test processes; verify no-cgo attribution and footprint tests are executed, proxy binding tests exercise the real framework, and unavailable stubs cannot satisfy the checks.
- [x] 5.5 Preserve macOS 13.0 in MyGo packaging metadata/configuration and document how Mach-O deployment information is checked; verify both generated architecture slices and bundle metadata retain the baseline rather than relying on C-only environment flags.
- [x] 5.6 Update `README.md`, `docs/technical/building.md`, and `AGENTS.md` with the new build policy, compilation versus packaging prerequisites, separate race requirements, and native verification commands; verify obsolete macOS cgo requirements are gone from active documentation and documented commands match Task/workflows. Preserve historical change records.

## 6. Validate integrated behavior

- [ ] 6.1 Run `task verify` with cgo disabled and check tidy consistency on native macOS and the existing Linux verification environment, plus affected Windows regression tests; record platform, architecture, commands, results, and any unavailable checks without treating them as passes.
- [x] 6.2 Run the cross-module suite `go test ./internal/pkg/process_attribution ./internal/pkg/systemproxy ./internal/services/mem_stats_service ./internal/services/proxy_service ./internal/services/history_service ./internal/services/setting_service ./internal/app ./internal/services/app_service ./internal/desktop/update ./internal/desktop/shell` with cgo disabled on macOS; verify proxy lifecycle, saved process metadata, HBIN compatibility, and shutdown remain intact.
- [x] 6.3 On both macOS architectures, run native attribution/ABI/memory tests and the AppKit main-loop fixture; verify local IPv4/IPv6 connections resolve, names/icons load, repeated operations release resources, and cancellation/normal shutdown do not hang. Record hardware or Rosetta execution explicitly.
- [ ] 6.4 On a disposable macOS network configuration, exercise HTTP/HTTPS and SOCKS5 management, authentication denial, service disappearance, external auxiliary-key edits, and normal shutdown; verify full original configuration restoration only while still owned and check upstream selection after restoration. Record results without changing real user settings in unattended CI.
- [ ] 6.5 Build arm64 and universal packages with cgo disabled on macOS without publishing; verify both universal slices, application launch and required features on macOS 13.0, bundle/DMG integrity, and unchanged signing/update artifact generation using the existing local release-validation setup.
- [ ] 6.6 Inspect process names/icons, managed-proxy state, and Memory in light and dark themes in the packaged app; verify existing placeholders and unavailable states remain correct and no new layout or localization changes are required. Record macOS-native evidence and any remaining risks before marking implementation complete.

## Workflow follow-up

- After implementation and all required verification are complete, archive this change through `openspec-archive-change` so the build delta is merged into the main specification.
- If rollback is needed, revert the native implementation, build policy, and associated documentation together; no persistent-data migration is required.
