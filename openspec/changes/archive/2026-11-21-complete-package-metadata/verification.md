# Verification

Verified on Windows amd64 on 2026-11-21, using the repository's MyGo v0.10.0. No release was uploaded and no signing key was used.

## Configuration and packages

- `CGO_ENABLED=0 go tool mygo i18n check`: passed, 1334 messages in en/zh-Hans.
- With `CGO_ENABLED=0` and `MYGO_LDFLAGS=-macos=13.0`, `go tool mygo build -platform windows/amd64,linux/amd64,linux/arm64 -skip-installer -o .mygo/package-metadata-check`: passed in 59.1 seconds. Generated catalog accessors were already up to date.
- Windows file properties: CompanyName `joxrays`, LegalCopyright `Copyright (c) 2026 joxrays`, FileVersion `2.0.0`, ProductName `FlowLens`.
- Linux amd64/arm64 each generated a DEB, RPM, tar.gz, separate install.sh, PKGBUILD, desktop entry, and icon source files with `linux.formats` absent.
- Parsed both DEB control/data archives and both RPM headers/payloads. Confirmed matching architecture, maintainer and description; RPM license is MIT. DEB dependencies include GTK 3; RPM dependencies include its GTK 3 soname; neither includes WebKitGTK. PKGBUILD declares MIT and `depends=('gtk3')`.
- Compared LICENSE and fonts/OFL.txt bytes with repository sources in Windows resources and both architectures' DEB, RPM, and tar payloads. No duplicate TTF or Go source files were packaged. Desktop entries contain Development/Network categories and the configured description.
- Verified the separate install.sh has no WebKitGTK prerequisite invocation. MyGo's shared script template retains unused WebKit helper definitions; these are not dependency checks executed by this build.
- Recomputed every generated SHA256SUMS entry: all matched.

## Release automation

- Parsed release.yml with PyYAML and ran Git Bash `bash -n` against every run block: passed.
- Ran the actual asset-check run block with mocked `gh release view/download` and local package filenames: all eight cases passed (complete release, missing install.sh, missing install.sh checksum, missing x86_64 RPM, missing x86_64 RPM checksum, missing aarch64 RPM, missing aarch64 RPM checksum, prerelease version).
- Read the pinned MyGo upload filter: DEB/RPM/tar archives and install.sh are uploaded; PKGBUILD and its icon files stay local. The revised documentation reflects this boundary.
- `go run github.com/rhysd/actionlint/cmd/actionlint@latest -shellcheck= -pyflakes= .github/workflows/release.yml` could not download the tool because proxy.golang.org timed out. YAML/Bash checks and the actual checker execution above supplied local validation; actionlint was not reported as passed.

## Acceptance and limits

- `openspec validate complete-package-metadata --strict --no-interactive`: passed.
- After synchronizing build-and-release, `openspec validate --specs --strict --no-interactive`: all 17 specifications passed.
- `git diff --check`: passed.
- Reviewed changed requirements against configuration, generated package contents, release checks, and documentation. The pre-existing README removal of the beta notice is preserved.
- No application Go code or UI changed, so application unit tests, UI render checks, and Python integration tests were not needed for this packaging-only change.
- Linux packages were cross-built and inspected, not installed or launched. macOS bundle/signing and Windows installer execution were not tested on this host. Their metadata paths were checked against the pinned CLI source; the existing native macOS acceptance requirements remain unchanged. Update signatures/private-key matching and live GitHub publication were not exercised.
