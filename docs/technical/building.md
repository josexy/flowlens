# Building and Packaging

FlowLens is a Go application. Building it needs Go, the MyGo CLI that `go.mod` pins as a tool (`go tool mygo`), and preferably Task. Nothing else is required on Windows or Linux; macOS also needs the Xcode Command Line Tools.

## Cgo

| Target | `CGO_ENABLED` | Why |
| --- | --- | --- |
| Windows, Linux | `0` | Neither MyGo nor FlowLens needs cgo there, so one machine can build every target |
| macOS | `1` | Process attribution with icons and the managed system proxy use Objective-C |

The Taskfile sets this from the host system. A production `mygo build` for macOS with cgo disabled fails on purpose (`cgo_guard_darwin.go`), rather than producing an application without those features. Because one `mygo build` applies one `CGO_ENABLED` to all its targets, build macOS separately from Windows and Linux.

## CI Verification

CI and release verification run on `ubuntu-latest` with `CGO_ENABLED=0` and `macos-latest` with `CGO_ENABLED=1`. macOS keeps the deployment target at 12.0. They invoke `gofmt`, `go tool mygo vet .`, `go tool mygo i18n check`, and `go test ./...` directly, without installing Task or golangci-lint. CI also checks that `go mod tidy` leaves `go.mod` and `go.sum` unchanged on both platforms. Verification uses a separate Go cache from release packaging.

Local `task verify` still includes golangci-lint. Run platform-specific tests locally when changing Windows or macOS behavior. Pull requests and main-branch pushes do not cross-compile release targets; the release workflow builds Windows, Linux, and macOS packages.

## Common Builds

```shell
task run                      # build under .mygo/run/ and launch once
task package                  # packages for this system, under build/
task package -- -platform windows/amd64,linux/amd64,linux/arm64 -skip-installer
```

`task run` skips the Windows installer and macOS disk image. MyGo still generates its Linux distribution packages under `.mygo/run/` on Linux.

`task package` writes to `build/<target>/`:

| Target | Output |
| --- | --- |
| `windows/amd64`, `windows/arm64` | Executable and `flowlens-setup-<version>-<arch>.exe`, made with NSIS. MyGo downloads NSIS on Windows if needed. On Linux, install `nsis` or pass `-skip-installer`. |
| `darwin/arm64`, `darwin/universal` | Application bundle and disk image. These need macOS. |
| `linux/amd64`, `linux/arm64` | Executable, Debian package, `flowlens-<version>-linux-<arch>.tar.gz` with `install.sh`, and a `PKGBUILD` |

With `MYGO_UPDATER_PRIVATE_KEY` set, every target also gets a signed update archive, `flowlens-<version>-<target>.tar.gz`, and its `update-<target>.json` manifest. Windows targets are `windows-amd64` and `windows-arm64`; macOS targets are `darwin-arm64` and `darwin-universal`. `mygo build -upload` uploads these alongside the installers and disk images. Linux uses its existing tar archive for updates too. Without the signing key, Windows and macOS builds do not produce these archives or manifests.

## Application and Tray Icons

All platforms use the same shield-and-nodes artwork. Its sources are `assets/icons/app-icon.svg` and, for the macOS menu bar, `assets/icons/tray-template.svg`:

| Use | Generated file |
| --- | --- |
| Windows and Linux application and tray icon, macOS Dock icon under `mygo dev` | `resources/icon.png` |
| macOS application bundle (`macos.icon` in `mygo.json`) | `assets/macos/app-icon.icns` |
| macOS menu bar | `resources/trayicon-template.png` |

After editing the SVG sources, run `task icons:generate` (or `go run ./assets/icons/generate.go`) to export all three with supersampled anti-aliasing. The ICNS holds several resolutions rendered from the SVG and works with the macOS 12.0 deployment target, without Icon Composer or a newer Xcode.

The menu-bar icon is a 72-pixel template image shown at 18 points with `IconIsTemplate`, so AppKit colors it for the menu bar's appearance and selection state. Check the packaged ICNS with `task run` or `task package` on macOS: Finder and Dock at small sizes, and the tray in light, dark, and selected states on 1× and Retina displays.

## Updates

`mygo.json` sets the update source (`updates.github`) and verification key (`updates.publicKey`). Applications accept only updates signed with the matching private key.

Store the contents of `mygo-update.key` in the repository Actions secret `MYGO_UPDATER_PRIVATE_KEY`; the release workflow already uses it. Local signed builds use an environment variable of the same name. Keep the private key outside Git, back it up, and reuse it across releases.

Development builds and installations without write access, including package-managed installs, use the release page instead of in-app installation.

## Releasing

`mygo.json` is the single source of the application version. `go tool mygo dev` and `go tool mygo build` embed it, and FlowLens reads it through `mygo.App.Version()` for environment details, update checks, and HAR exports. Plain `go run` or `go build` without MyGo metadata uses `0.0.0-dev`. `go tool mygo version` prints the framework CLI version; it does not change the application version.

1. Add a `## <version>` section to `CHANGELOG.md`. Its text becomes the release notes.
2. Set `version` in `mygo.json` to the same version, such as `1.2.3`, then commit.
3. Push the matching tag `v1.2.3`.

`.github/workflows/release.yml` checks that the tag or manually supplied version matches `mygo.json`. Each `mygo build -upload` validates the update configuration and signing key, including whether the private key matches the public key, before compiling or uploading. MyGo also handles update notes from `CHANGELOG.md`. The workflow builds Windows amd64/arm64 on `windows-latest`, Linux amd64/arm64 on `ubuntu-latest`, and macOS arm64/universal on `macos-latest`, with cgo enabled only on macOS. `darwin/universal` combines arm64 and amd64 in one application; listing `darwin/arm64,darwin/amd64` would produce two separate applications.

Build jobs upload to a draft GitHub Release to review and publish. They run one at a time because MyGo merges and replaces the shared `SHA256SUMS` during each upload. The final check requires both Windows installers, both macOS disk images, all six platform update archives, both Debian architectures, and their checksum entries. It also requires all six `update-<target>.json` manifests, which MyGo excludes from `SHA256SUMS`. The draft is created before the build checks.

Windows executables and installers are unsigned. macOS applications use MyGo's default ad-hoc signing and are not notarized. No platform-signing certificates or secrets are required. `MYGO_UPDATER_PRIVATE_KEY` is still required to sign update archives.
