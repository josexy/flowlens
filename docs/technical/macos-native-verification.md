# macOS native verification

Run these checks in a checkout of the migration on macOS. Record OS version,
hardware, process architecture, command, exit status, and failures. arm64 and
amd64 must both execute; label Rosetta explicitly. Do not treat a cross-compiled
test binary, an unavailable feature, or a skipped fixture as a native pass.

## Ordinary checks

```sh
export CGO_ENABLED=0
go env GOOS GOARCH GOVERSION
sw_vers
task verify
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./internal/pkg/process_attribution ./internal/pkg/systemproxy ./internal/services/mem_stats_service ./internal/services/proxy_service ./internal/services/history_service ./internal/services/setting_service ./internal/app ./internal/services/app_service ./internal/desktop/update ./internal/desktop/shell -count=1
task test:macos:appkit
task build:check -- -targets=darwin/arm64,darwin/amd64
```

The ordinary attribution tests use libproc/sysctl without AppKit enrichment.
`test:macos:appkit` starts a separate process whose initial OS thread pumps the
main CF run loop while Go tests run on a worker. It creates a temporary bundle,
checks its name/identifier, and decodes bundle and executable fallback icons.
Use a logged-in graphical session. Production uses MyGo's existing main loop;
there is no additional application thread or window lifecycle hook.

The AppKit adapter retains only opaque callback IDs in libdispatch. Cancellation
releases waiting workers and Go requests immediately. A canceled ID remains a
bounded tombstone until its callback runs; it cannot publish a stale result.
The two-second fallback deadline also bounds calls without a caller deadline.
Autorelease pools and image representations are released on the thread that
used them. Process start tokens are rechecked after enrichment and icon loading.

On Apple Silicon with Rosetta installed, compile and execute Intel tests as a
separate step (run arm64 tests above as well):

```sh
GOARCH=amd64 go test -c -o /tmp/flowlens-attribution-amd64.test ./internal/pkg/process_attribution
GOARCH=amd64 go test -c -o /tmp/flowlens-proxy-amd64.test ./internal/pkg/systemproxy
GOARCH=amd64 go test -c -o /tmp/flowlens-memory-amd64.test ./internal/services/mem_stats_service
arch -x86_64 /tmp/flowlens-attribution-amd64.test -test.v
FLOWLENS_APPKIT_TEST=1 arch -x86_64 /tmp/flowlens-attribution-amd64.test -test.run TestDarwinAppKit -test.v
arch -x86_64 /tmp/flowlens-proxy-amd64.test -test.v
arch -x86_64 /tmp/flowlens-memory-amd64.test -test.v
```

## Disposable network configuration

Use a disposable macOS VM/network configuration with a recorded original proxy
dictionary. These steps are manual and intentionally excluded from unattended
tests. The native driver uses Security authorization, a preferences lock, and
complete binary property-list snapshots, including unknown and auxiliary keys.

1. Exercise HTTP/HTTPS and SOCKS5 management with existing bypass and PAC values.
   Disable management and confirm the complete original dictionary is restored.
2. Deny authorization and confirm an error with no active ownership. Retry with
   permission and confirm success.
3. While managed, change an auxiliary key externally. Disable management and
   confirm the external configuration survives and a conflict is reported.
4. Change the service/configuration between a controller check and the write;
   confirm the comparison inside the preferences lock prevents overwriting it.
5. Remove the managed network service; confirm failure and no write to another
   service. Exercise commit/apply failure on the disposable configuration.
   Compensation may restore only a dictionary still equal to FlowLens's write.
6. Quit normally while still owning the configuration, and confirm restoration
   and upstream-proxy selection after restarting.

## Packages and minimum OS

```sh
export CGO_ENABLED=0 MYGO_LDFLAGS=-macos=13.0
go tool mygo build -platform darwin/arm64,darwin/universal
lipo -archs build/darwin-universal/FlowLens.app/Contents/MacOS/FlowLens
otool -arch all -l build/darwin-universal/FlowLens.app/Contents/MacOS/FlowLens
/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' build/darwin-universal/FlowLens.app/Contents/Info.plist
codesign --verify --deep --strict build/darwin-universal/FlowLens.app
```

Check both Mach-O slices for `LC_BUILD_VERSION minos 13.0` (or the equivalent
`LC_VERSION_MIN_MACOSX`) and the bundle for `LSMinimumSystemVersion=13.0`. Verify
the generated DMGs with `hdiutil verify <path-to-dmg>`. Using the existing local
release-validation key setup, also check signed update archives/manifests and
checksums. Do not use `-upload` for verification.

Launch on macOS 13.0 and current macOS on both architectures. Verify process
names/icons, direct IPv4/IPv6 attribution, proxy management, positive physical
footprint and touched-memory growth, and cancellation/normal shutdown. Inspect
process placeholders, managed-proxy state, and Memory in light and dark themes.
Mach-O metadata alone cannot establish this runtime baseline.
