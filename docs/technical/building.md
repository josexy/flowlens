# Building and Packaging

FlowLens compilation needs Go, the MyGo CLI pinned in `go.mod` (`go tool mygo`), and preferably Task. No C/Objective-C compiler, Node.js, or npm is needed for ordinary builds or Go tests. Python request hooks use an external Python 3 interpreter.

## Cgo and platform tools

Task, CI, release verification, and packaging select `CGO_ENABLED=0` on Windows, Linux, and macOS. macOS process attribution/icons, system proxy discovery/control, and physical-footprint sampling call system frameworks through the pinned purego dependency. No helper dylib or generated cgo wrapper is built.

Executable cross-compilation needs no macOS SDK. macOS application signing and DMG creation still run on macOS and use `codesign` and `hdiutil` (install the Xcode Command Line Tools when required by the packaging tools). Frameworks remain runtime dependencies. Compile success alone does not verify native behavior.

Optional `task test:race` is separate: the pinned Go 1.27 toolchain supports Darwin race checks with cgo disabled; Windows/Linux race checks enable cgo and need a supported C compiler. Pass package names after `--` to narrow the run.

`mygo.json` explicitly sets `macos.minimumSystemVersion` to `13.0`. The pinned Go 1.27 toolchain requires macOS 13 or later; it no longer supports macOS 12 ([release notes](https://go.dev/doc/go1.27#darwin)). Task and release packaging pass `MYGO_LDFLAGS=-macos=13.0`; this sets Mach-O deployment metadata without C compiler flags. For a direct MyGo invocation, supply the same environment variable. Plain Darwin `go build` uses `-ldflags=-macos=13.0`. This metadata setting does not establish runtime compatibility: launching and testing both slices on macOS 13.0 remains a required acceptance check.

## CI verification

CI and release verification run formatting, UI lifecycle checks, message catalog checks, and `go test ./...` with cgo disabled on `ubuntu-latest` and `macos-latest`. The macOS test step sets `FLOWLENS_APPKIT_TEST=1` to run the AppKit main-loop fixture as part of the ordinary suite. CI also checks module tidy consistency. The release workflow builds the supported desktop packages, including the universal macOS application.

Local `task verify` also includes golangci-lint. Native macOS validation commands, including Intel/Rosetta execution and manual proxy/package checks, are in [macOS native verification](macos-native-verification.md). The migration remains pending native acceptance until those results are recorded; Windows checks and cross-compilation do not count as macOS runtime passes.

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

After editing the SVG sources, run `task icons:generate` (or `go run ./assets/icons/generate.go`) to export all three with supersampled anti-aliasing. The ICNS holds several resolutions rendered from the SVG and works with the macOS 13.0 deployment target, without Icon Composer or a newer Xcode.

The menu-bar icon is a 72-pixel template image shown at 18 points with `IconIsTemplate`, so AppKit colors it for the menu bar's appearance and selection state. Check the packaged ICNS with `task run` or `task package` on macOS: Finder and Dock at small sizes, and the tray in light, dark, and selected states on 1× and Retina displays.

## Updates

`mygo.json` sets the update source (`updates.github`) and verification key (`updates.publicKey`). Applications accept only updates signed with the matching private key.

Store the contents of `mygo-update.key` in the repository Actions secret `MYGO_UPDATER_PRIVATE_KEY`; the release workflow already uses it. Local signed builds use an environment variable of the same name. Keep the private key outside Git, back it up, and reuse it across releases.

Development builds and installations without write access, including package-managed installs, use the release page instead of in-app installation.

FlowLens uses MyGo's `Updater.Check` and `Update.Install` with its own native window. Installation is one operation: download byte counts are measured, while signature verification and file replacement have no separate framework progress callbacks. The window therefore shows download progress followed by an indeterminate finishing state, without timed verification or preparation stages. A delta fallback restarts byte progress and makes download cancellation available again.

The window defaults **Restart automatically when finished** on for the current session. Starting installation captures that choice; successful installation requests one restart through FlowLens's existing unsaved-work guards and normal service shutdown, even if the update window has closed. Cancellation, installation failure, or shutdown suppresses automatic restart. If installation succeeds despite a concurrent cancellation, the installed update remains ready for a later restart. Declining a guard leaves it ready without repeating the prompt; a failed restart can be retried without reinstalling.

MyGo replaces program files during installation on Windows and Linux; macOS stages an application bundle and replaces it after the running process exits. **Restart later** postpones launching the new version, not installation. Hiding or closing the update window never cancels installation. Explicit cancellation is available during checks and incomplete downloads; quitting the application requests cancellation and waits for the framework call to finish. MyGo's file-replacement recovery is not a health-check rollback after launching the new version.

## Releasing

`mygo.json` is the single source of the application version. `go tool mygo dev` and `go tool mygo build` embed it, and FlowLens reads it through `mygo.App.Version()` for environment details, update checks, and HAR exports. Plain `go run` or `go build` without MyGo metadata uses `0.0.0-dev`. `go tool mygo version` prints the framework CLI version; it does not change the application version.

1. Add a `## <version>` section to `CHANGELOG.md`. Its text becomes the release notes.
2. Set `version` in `mygo.json` to the same version, such as `1.2.3`, then commit.
3. Push the matching tag `v1.2.3`.

`.github/workflows/release.yml` checks that the tag or manually supplied version matches `mygo.json`. Each `mygo build -upload` validates the update configuration and signing key, including whether the private key matches the public key, before compiling or uploading. MyGo also handles update notes from `CHANGELOG.md`. The workflow builds Windows amd64/arm64 on `windows-latest`, Linux amd64/arm64 on `ubuntu-latest`, and macOS arm64/universal on `macos-latest`, with cgo disabled on every platform. `darwin/universal` combines arm64 and amd64 in one application; listing `darwin/arm64,darwin/amd64` would produce two separate applications.

Build jobs upload to a draft GitHub Release to review and publish. They run one at a time because MyGo merges and replaces the shared `SHA256SUMS` during each upload. The final check requires both Windows installers, both macOS disk images, all six platform update archives, both Debian architectures, and their checksum entries. It also requires all six `update-<target>.json` manifests, which MyGo excludes from `SHA256SUMS`. The draft is created before the build checks.

Windows executables and installers are unsigned. macOS applications use MyGo's default ad-hoc signing and are not notarized. No platform-signing certificates or secrets are required. `MYGO_UPDATER_PRIVATE_KEY` is still required to sign update archives.
