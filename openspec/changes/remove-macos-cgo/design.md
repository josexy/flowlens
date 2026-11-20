# Design

## Context

See [proposal.md](proposal.md) for motivation and [the build delta](specs/build-and-release/spec.md) for the acceptance contract.

### Observed cgo inventory

The audit used the pinned module graph and `go list -deps` with `GOOS=darwin`, each of `GOARCH=arm64` and `amd64`, and `CGO_ENABLED=1`. Both architectures select exactly these application cgo files, plus Go's `runtime/cgo` support package:

| Owner and source | Native operations currently used | Current behavior with cgo disabled |
| --- | --- | --- |
| `internal/pkg/process_attribution/provider_darwin.go`, with `bridge_darwin.m` and `bridge_darwin.h` | `proc_pidinfo`/`proc_pidpath` for start time, process name and executable; Foundation/AppKit for running-app and bundle metadata and PNG icons; libdispatch for main-thread calls | `provider_darwin_nocgo.go` returns `darwin_requires_cgo`; its test requires unavailability |
| `internal/pkg/systemproxy/system_proxy_darwin.go` | SystemConfiguration/CoreFoundation proxy discovery through `SCDynamicStoreCopyProxies` | `fromPlatform` returns an empty proxy |
| `internal/pkg/systemproxy/system_proxy_control_darwin.go` | Primary-service discovery; complete proxy-dictionary snapshots; Security authorization; locked SystemConfiguration apply/restore transactions | The no-cgo driver reports unsupported |
| `internal/services/mem_stats_service/process_memory_darwin.go` | Mach `task_info(TASK_VM_INFO)` physical footprint | Memory is unavailable; a test requires that outcome and the memory-growth test excludes no-cgo Darwin |

Socket-to-PID lookup already uses `unix.SysctlRaw("net.inet.tcp.pcblist_n")` and the Go parser in `darwin_pcb.go`; cgo only supplies process metadata and icons in that module. Preserve the parser, generation checks, 100 ms socket snapshot, and refresh-on-miss behavior.

The dependency graph with cgo disabled selects no `CgoFiles`; this is dependency-selection evidence, not a successful functional build. `cgo_guard_darwin.go` deliberately fails builds using `darwin && !cgo && mygo_noinspector`. Task and both GitHub workflows select cgo on macOS, with C compiler/linker deployment flags. `README.md`, `docs/technical/building.md`, `AGENTS.md`, and the current build spec repeat this policy. The policy change must be explicit and synchronized; simply deleting the guard would ship unavailable features.

MyGo v0.10.0 already uses purego for macOS UI, system APIs, and networking helpers. `github.com/ebitengine/purego` v0.11.1 is already pinned indirectly. `modernc.org/sqlite` is selected without cgo; Python runs as external CPython workers; current-user CA trust uses `/usr/bin/security`. No direct cgo use was found in these features, shortcuts, or the remaining selected dependencies. This audit describes FlowLens's pinned desktop graph, not every optional package in each upstream module.

MyGo's pinned `macos.minimumSystemVersion` default is 12.0, but this migration explicitly raises it to 13.0 to match Go 1.27, and its universal-binary writer is implemented in Go. Signing and disk-image production still invoke platform tools. Neither removing C flags nor cross-compiling an executable proves the packaged application's minimum OS version.

### Existing tests and gaps

- Darwin attribution tests cover IPv4/IPv6 tuple matching, snapshot refresh, current-process metadata, exited processes, loopback ownership, and C allocation balance. They currently require cgo. The portable PCB parser and Manager tests remain useful unchanged.
- System proxy tests exercise URL selection and a fake `platformDriver`, including external-change conflicts. There is no dedicated Darwin native driver test, so they cannot validate CoreFoundation ownership, authorization, or a real preferences transaction.
- The memory-growth test touches 64 MiB and expects a substantial increase in the platform measure. Its Darwin build constraint currently requires cgo.
- CI runs the tests for its host platform. Release packaging builds the supported desktop targets.

## Goals / Non-Goals

**Goals:**

- Keep public Go service APIs and data formats stable while replacing the native call boundary inside each owning module.
- Make native signatures, buffer sizes, object lifetimes, error conversion, and cancellation inspectable and testable on both macOS architectures.
- Use one functional Darwin implementation regardless of the cgo build flag, with cgo disabled for normal builds.

**Non-Goals:**

- Reimplement Apple's system frameworks, produce a statically linked macOS application, or eliminate native APIs from MyGo.
- Add a C/Objective-C shim, bundled replacement dylib, privileged helper, shell-command implementation of attribution/proxy/memory, or new dependency family.
- Change proxy selection to per-URL PAC resolution, alter capture/export formats, redesign UI, or change existing CA trust and Python execution.
- Expand support to another OS/architecture or promise that DMG/signing steps run on Windows/Linux.

## Decisions

### 1. Use the pinned purego dependency within each owner

Promote purego v0.11.1 to a direct dependency; retain MyGo and all other versions. Bind exported system APIs with `Dlopen`, checked `Dlsym`, and typed function registration. Use `purego/objc` for Objective-C messages where appropriate. Keep binding tables and conversion helpers private to `process_attribution`, `systemproxy`, and `mem_stats_service`, with narrowly scoped injectable call tables for meaningful error tests. Do not create a general macOS framework package for these three consumers.

Initialize bindings once, returning an initialization error to existing feature error paths where possible. Treat missing libraries/symbols as real runtime failures and surface them in native tests. Keep loaded handles for process lifetime while any registered function or callback may use them. Never report a feature unavailable merely because cgo is disabled.

Represent native integer widths and pointer-sized values explicitly. Distinguish a symbol's address from the value of an exported pointer variable: CoreFoundation string constants need dereferencing, dictionary callback structures use their addresses, and Mach's task port is not a callable macro. Copy native strings/data to Go before releasing their owners, validate lengths before constructing slices, and keep Go buffers alive through synchronous calls. Native callbacks retain opaque IDs, not Go pointers or UI objects. Capture `errno` on the same locked OS thread as any call whose failure depends on it.

Alternatives considered: retaining cgo behind a default-off tag would leave two implementations and weaken acceptance; compiling a native shim would retain the toolchain dependency; parsing command output would change latency, privileges, and proxy transaction semantics. MyGo's `netproxy` is also unsuitable as a drop-in: its per-URL CFNetwork/PAC behavior differs from FlowLens's existing single upstream selection and it does not provide this managed driver.

### 2. Preserve attribution and move only metadata/icon calls

Keep the existing sysctl owner lookup. Bind `proc_pidinfo(PROC_PIDTBSDINFO)` and `proc_pidpath` directly for the same metadata. Define and test the `proc_bsdinfo` size/field offsets on arm64 and amd64, check returned byte counts, and preserve `StartToken` as `seconds:microseconds`. Preserve the existing permission/exit classifications and partial metadata behavior without inventing an identity when no start time is available.

Use the same `NSRunningApplication`, enclosing `.app` bundle search, localized display-name fallback, bundle identifier, and executable-path rules. Load icons through the running application, then bundle/executable `NSWorkspace` fallback, and convert via `NSBitmapImageRep` to PNG. Copy PNG bytes within the autorelease scope and decode in Go after leaving the main thread. Retain the existing Manager, icon storage, generation checks, and `widgets.ProcessIcon` consumer; do not change `Result`, `ProcessInfo`, HBIN, or icon-key formats.

AppKit calls use a module-local, bounded main-queue adapter backed by libdispatch. Execute directly only when already on the macOS main thread. Worker callers enqueue through one process-lifetime callback and wait on a context-aware result channel. The registry uses non-reused opaque IDs; cancellation removes the request and a late callback becomes a no-op. Copy only Go-owned results out of the callback and release Objective-C objects/autorelease pools on the thread that used them. Do not hold Manager or UI-state locks across a main-thread wait.

Headless tests must not dispatch synchronously onto an unpumped main queue. Core libproc attribution tests run without AppKit enrichment; tests that assert application metadata/icons use a dedicated macOS test process with a running main loop. The production app must pass the latter checks. Cancellation during rotation/shutdown must release waiters even after the run loop stops. If lifecycle wiring is necessary, put it in `internal/app`; never import desktop view state into the package or retain `ui.Context`/`Element` values.

Alternatives considered: replacing libproc with a new process enumeration mechanism would unnecessarily change PID-instance semantics; parsing ICNS/plist files alone loses running-application and localized/icon fallback behavior; copying the existing synchronous dispatch literally risks stranded goroutines when the main loop is absent or shutting down.

### 3. Preserve complete proxy snapshots and locked comparison

Share a private native binding table between the Darwin proxy reader and managed driver. Read SystemConfiguration/CoreFoundation values with type checks, supporting both boolean and numeric enable fields. Keep `Get`'s environment override and HTTP, HTTPS, SOCKS ordering/normalization.

Maintain the current primary-service selection (IPv4 first, IPv6 fallback), `darwinProxySnapshot` service ID and binary property-list bytes, and `CFEqual` comparison of decoded dictionaries. Preserve unknown keys and auxiliary values such as bypass lists, PAC, and auto-discovery. Keep the existing absent-configuration-to-empty-dictionary convention; do not redefine snapshot semantics during this migration.

Apply and restore continue through Security authorization and `SCPreferencesCreateWithAuthorization`. For every write: acquire the preferences lock, reread and compare the expected complete configuration, mutate only after it matches, commit, apply, unlock, and release all owned references and authorization state on every exit. `Matches` remains an early check, never a substitute for comparison inside the write transaction. Preserve `ErrChangedExternally` and the controller's original upstream handling.

Test failure boundaries separately: authorization denial, lock failure, serialization/type failure, commit failure, and apply failure after a successful commit. Track enough original and candidate configuration to recover only while the current dictionary still equals FlowLens's own expected/written state; never compensate over an external edit or mark a failed operation active. Exercise these branches through fake native calls and a disposable macOS network configuration for real authorization/restore validation.

Alternatives considered: `networksetup`, `scutil`, and editing preference files do not retain the current in-process authorization and locked compare/update contract. Comparing only host/port would miss external changes to unrelated proxy keys and permit destructive restoration.

### 4. Keep Mach physical footprint as the memory source

Bind `task_info` and obtain the current task port from the appropriate exported libSystem symbol, replacing the `mach_task_self()` macro used by C. Request the documented `TASK_VM_INFO` revision that includes `phys_footprint`, with a buffer sized for that count. Express Mach counts in 32-bit words and validate the returned count before reading the field. Use an explicit buffer/layout that respects Apple's packing rules; do not assume Go's default struct alignment matches an SDK struct.

Return the same `ProcessMemory{Measure: MeasurePhysFootprint}` on a valid positive sample. Initialization failures, Mach errors, truncated responses, or values outside the supported range return the existing unavailable value. Keep monitoring, chart gaps, Go heap statistics, and exclusion of Python Workers unchanged. Delete the no-cgo fallback and replace its test; extend the touched-memory growth test to all Darwin builds.

Alternatives considered: RSS, `proc_taskinfo` resident size, Go runtime totals, and command-line memory readings are different measures. `proc_pid_rusage` can expose footprint but would introduce a different sampling API when a direct binding to the current Mach API preserves behavior.

### 5. Switch build policy only with functional coverage

After all three modules have functional replacements, remove their no-cgo stubs, the Objective-C bridge/header, cgo-only test constraints, and `cgo_guard_darwin.go`. Set ordinary Task, CI verification, release verification, and packaging to `CGO_ENABLED=0`; remove obsolete C compiler/linker flags. Keep optional race-test settings separate and align them with the pinned Go toolchain's actual platform requirements rather than imposing a production cgo requirement.

Keep macOS 13.0 explicit in packaging configuration/documentation as needed and verify both bundle metadata and Mach-O deployment information. The existing MyGo default is 12.0; the explicit 13.0 setting matches Go 1.27 and the user-approved baseline change; an ignored `MACOSX_DEPLOYMENT_TARGET` environment variable must not be treated as proof for internal Go linking. No application version bump is part of this work. During implementation, the user approved raising the original 12.0 target to 13.0 because Go 1.27 has discontinued macOS 12 support; changing Mach-O metadata alone cannot restore that support.

Run `go test ./...` on Linux and macOS with cgo disabled. Enable the AppKit main loop in the macOS test step. Verify amd64 during native migration acceptance, recording Intel or Rosetta execution. Package arm64 and universal on macOS using the existing signing and update mechanisms.

Alternatives considered: changing only the Taskfile would leave releases on a different implementation path; accepting no-cgo stub tests or cross-builds would validate compilation while missing feature loss and ABI defects.

## Risks / Trade-offs

- [Incorrect ABI or native constant interpretation] -> Check signatures, struct sizes/offsets, packing, integer widths, and pointer constants against Apple definitions; run real calls on both architectures. No unsafe binding is accepted based only on compilation.
- [Native reference leaks or use-after-release] -> Pair every Create/Copy/retain with deterministic cleanup, scope autorelease pools to an OS thread, copy data before release, and test success/failure/cancellation with ownership counters in fake bindings plus repeated native operations.
- [AppKit deadlock or stale callbacks] -> Use bounded asynchronous dispatch, context cancellation, opaque request IDs, and explicit run-loop tests for shutdown/rotation. Do not carry C allocation-counter tests forward as meaningless no-op assertions.
- [System proxy partially committed or overwritten externally] -> Compare complete dictionaries while locked, test partial failures, preserve ownership checks during compensation, and validate apply/restore on a disposable macOS setup.
- [Unsupported symbols or OS baseline] -> Use APIs available at the existing baseline, fail through existing error paths, and require a macOS 13.0 smoke test as well as current macOS verification.
- [Packaging confused with compilation] -> Document `codesign`/`hdiutil` separately. Pure-Go source compilation does not promise a package independent of macOS runtime frameworks or platform packaging tools.
- [Planning-host limits] -> This proposal was audited on Windows. Native macOS execution, both-architecture ABI checks, GUI/authorization tests, minimum-version checks, and release packaging remain implementation acceptance work, not completed evidence.

## Migration Plan

1. Implement bindings and their module tests without changing shared models, storage, or service APIs. Retain the old release guard until functional replacements for all affected modules are present.
2. Complete attribution/AppKit, proxy transaction, and Mach-memory coverage. Replace cgo-dependent tests and remove obsolete bridge/stub files.
3. Switch the build and workflow settings, reconcile module metadata, and update README, build documentation, and AGENTS.md. Recheck task-oriented user documentation for accidental behavior changes.
4. Run the verification matrix and record interactive/macOS evidence. Inspect both slices of the universal application, its OS minimum, and signed update compatibility. Do not publish a release as part of implementation verification.
5. Archive the OpenSpec change only after implementation and required verification are complete. No database, settings, history, or icon-cache migration is needed. If rollback is required, revert the native implementations, build policy, and matching documentation together to the previous cgo-based revision; do not ship the old no-cgo stubs as rollback.

### Verification matrix

| Check | Required evidence |
| --- | --- |
| Source and dependency audit | No FlowLens `import "C"`, `#cgo`, or compiled Objective-C bridge; no selected dependency `CgoFiles` in no-cgo application builds |
| Module tests on macOS | Attribution/PCB/Manager, native binding failures and lifetimes, proxy discovery and transactions, positive footprint and touched-memory growth with `CGO_ENABLED=0` |
| Consumer regressions | Proxy service, history, settings, application lifecycle, app service, and shell tests; no change to process models or HBIN round trips |
| Repository verification | `task verify`, `go mod tidy` consistency, and ordinary tests with cgo disabled; race checks separately where supported |
| Compile matrix | Windows amd64/arm64, Linux amd64/arm64, Darwin arm64/amd64, including `mygo_noinspector`; compile-only results labeled as such |
| Native macOS acceptance | Both architectures: process metadata, IPv4/IPv6 loopback attribution, icons, cancellation/shutdown, native proxy values, memory sample and growth |
| Interactive acceptance | HTTP/HTTPS and SOCKS5 apply/restore, authentication denial, external edits, network-service disappearance, normal shutdown; process icons and Memory in light/dark themes |
| Packaging and baseline | arm64 and universal bundles/DMGs, both Mach-O slices, macOS 13.0 launch/feature smoke, unchanged signing and update artifacts |

### Reference material

The implementation audit used local pinned sources (`go.mod`, MyGo v0.10.0, and purego v0.11.1). Upstream documents support the native binding approach and must not be used to silently upgrade dependencies:

- [purego project](https://github.com/ebitengine/purego): calling native libraries without cgo, including macOS arm64/amd64.
- [Apple SCPreferences](https://developer.apple.com/documentation/systemconfiguration/scpreferences-ft8) and [SCPreferencesLock](https://developer.apple.com/documentation/systemconfiguration/scpreferenceslock(_:_:)): authorization sessions and exclusive preference access.
- [Apple task_info definitions](https://github.com/apple-oss-distributions/xnu/blob/main/osfmk/mach/task_info.h): Mach packing, revision counts, and physical-footprint field.
- [Apple proc_info definitions](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info.h): process metadata layout and start-time fields.
