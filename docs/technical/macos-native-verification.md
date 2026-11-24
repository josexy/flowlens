# macOS Native Verification

Run these checks in a repository checkout on macOS. Record the macOS version, hardware, process architecture, command, exit status, and any failures. Both `arm64` and `amd64` must execute natively (label Rosetta runs explicitly). Never treat a cross-compiled test binary, an unavailable feature, or a skipped fixture as a native pass.

## Ordinary Checks

```sh
export CGO_ENABLED=0
go env GOOS GOARCH GOVERSION
sw_vers
FLOWLENS_APPKIT_TEST=1 task verify
go mod tidy
git diff --exit-code -- go.mod go.sum
```

- **Graphical session:** Running `FLOWLENS_APPKIT_TEST=1 go test ./...` exercises the AppKit main-loop fixture. The `process_attribution` test binary pumps the main run loop while reading a temporary bundle's name, identifier, and icons.
- **Headless session:** Omit `FLOWLENS_APPKIT_TEST=1`. The GUI test skips while `libproc`, socket, and provider tests still execute.
- **AppKit adapter invariants:** The adapter retains only opaque callback IDs in `libdispatch`. Cancellation releases waiting workers and Go requests immediately; a canceled ID stays as a bounded tombstone until its callback fires and cannot publish a stale result. A 2-second fallback deadline bounds calls without a caller deadline. Autorelease pools and image representations are released on the thread that used them, and process start tokens are revalidated after enrichment and icon loading.

On Apple Silicon with Rosetta installed, also compile and run the Intel (`amd64`) tests separately (in addition to the `arm64` run above):

```sh
GOARCH=amd64 FLOWLENS_APPKIT_TEST=1 go test -count=1 \
  ./internal/pkg/process_attribution ./internal/pkg/systemproxy ./internal/services/mem_stats_service
```

## Disposable Network Configuration

Run these manual checks in a disposable macOS VM or disposable network configuration after recording the original proxy dictionary. They are intentionally excluded from unattended tests. The native driver uses Security authorization, a preferences lock, and full binary property-list snapshots (including unknown and auxiliary keys).

1. Enable HTTP/HTTPS and SOCKS5 system proxy management when existing bypass and PAC settings are present. Disable management and confirm the complete original dictionary is restored.
2. Deny Security authorization and confirm an error is returned with no active ownership. Retry with authorization granted and confirm success.
3. While managed, modify an auxiliary key externally. Disable management and confirm the external change is preserved and a conflict is reported.
4. Change the active network service or configuration between a controller check and write; confirm the comparison inside the preferences lock prevents overwriting the newer configuration.
5. Delete the managed network service; confirm a conflict is reported, managed ownership is cleared, and no other service is modified. Also exercise a commit/apply failure on the disposable configuration and verify compensation restores only a dictionary that still equals FlowLens's own write.
6. Quit FlowLens normally while it owns the system proxy configuration, then verify that the original configuration is restored and that upstream-proxy selection behaves properly after restart.

## Packages and Minimum OS

```sh
export CGO_ENABLED=0 MYGO_LDFLAGS=-macos=13.0
go tool mygo build -platform darwin/arm64,darwin/universal
lipo -archs build/darwin-universal/FlowLens.app/Contents/MacOS/FlowLens
otool -arch all -l build/darwin-universal/FlowLens.app/Contents/MacOS/FlowLens
/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' build/darwin-universal/FlowLens.app/Contents/Info.plist
codesign --verify --deep --strict build/darwin-universal/FlowLens.app
```

Verify the following on the built artifacts:

- Both Mach-O slices report `LC_BUILD_VERSION` with `minos 13.0` (or the equivalent `LC_VERSION_MIN_MACOSX`).
- The app bundle declares `LSMinimumSystemVersion=13.0`.
- Each generated DMG passes `hdiutil verify <path-to-dmg>`.
- Using the local release-validation key setup, signed update archives, `update-*.json` manifests, and checksums are generated and valid (do not pass `-upload` during verification).

Launch the packaged app on both macOS 13.0 and current macOS across both architectures (`arm64` and `amd64`). Confirm process names and icons, direct IPv4/IPv6 attribution, managed proxy controls, positive physical footprint and touched-memory growth, and cancellation/normal shutdown. Inspect process placeholders, managed-proxy indicators, and the **Memory** view in both light and dark themes. Mach-O deployment metadata alone does not establish this runtime baseline.
