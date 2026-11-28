# Building and Packaging

Building FlowLens requires Go 1.27.3 or newer, the MyGo CLI pinned in `go.mod` (`go tool mygo`), and preferably [Task](https://taskfile.dev). Ordinary builds and Go tests require no C/Objective-C compiler, Node.js, or npm. Python request hooks use an external Python 3 interpreter at runtime.

## Native UI Dependency

Nuxt UI Go is pinned to [v0.2.0](https://github.com/josexy/nuxt-ui-go/releases/tag/v0.2.0), which includes the borrowed-row and collection optimizations from PRs #18 and #20. No local checkout or `replace` directive is required. MyGo remains at v0.10.0.

The dependency's choice controls own submitted data. Keep each control's state per window and submit options only on initialization or a data/locale change. `widgets.SelectState` prepares fixed or localized choices; category lists and font menus submit their snapshots explicitly. Multi-selection uses `nui.SelectionState`, with snapshots copied to application state in change callbacks.

Shared menu controls cache submitted items. Their revision arguments must cover changes to both descriptors and captured callback targets, including asynchronous data and settings updates. Context menus prepare lazily on opening; dropdowns prepare while closed too, preserving first-open keyboard navigation. Tables use the combined `OnChange` notification for native selection, cursor, sort and column-layout changes.

## Cgo and Platform Tools

Task, CI, release verification, and packaging all run with `CGO_ENABLED=0` on Windows, Linux, and macOS. On macOS, process attribution and icons, system proxy discovery and control, and physical-footprint memory sampling call system frameworks through the pinned `purego` dependency—no helper dylib or generated cgo wrapper is built.

- **Cross-compilation:** Building executables requires no macOS SDK.
- **macOS packaging:** Application signing and DMG creation still run on macOS using `codesign` and `hdiutil` (install the Xcode Command Line Tools if needed). System frameworks remain runtime dependencies, so compilation success alone does not verify native behavior.
- **Race detector:** The optional `task test:race` task sets `CGO_ENABLED=1` on all desktop platforms and requires a supported C compiler. Pass package paths after `--` to narrow the run.
- **Minimum macOS version:** `mygo.json` sets `macos.minimumSystemVersion` to `13.0` because Go 1.27 requires macOS 13 or later and no longer supports macOS 12 ([Go 1.27 Darwin release notes](https://go.dev/doc/go1.27#darwin)). Task and release packaging pass `MYGO_LDFLAGS=-macos=13.0` to embed Mach-O deployment metadata without C compiler flags; supply the same environment variable when invoking `go tool mygo` directly, or pass `-ldflags=-macos=13.0` for plain Darwin `go build`. Deployment metadata does not prove runtime compatibility—launching and testing both architecture slices on macOS 13.0 remains a required acceptance check.

## CI Verification

CI and release verification run Go formatting, UI lifecycle checks (`go tool mygo vet .`), message catalog checks (`go tool mygo i18n check`), and `go test ./...` with `CGO_ENABLED=0` on `ubuntu-latest` and `macos-latest`.

- On `macos-latest`, the test step sets `FLOWLENS_APPKIT_TEST=1` so the AppKit main-loop fixture runs as part of the standard suite.
- CI also verifies `go mod tidy` consistency (`go.mod` and `go.sum`).
- Local `task verify` additionally runs `golangci-lint`.
- The release workflow builds all supported desktop packages, including the universal macOS application.

Native macOS validation steps—including Intel/Rosetta execution and manual proxy and package checks—are documented in [macOS Native Verification](macos-native-verification.md). Pure-Go macOS native acceptance remains pending until those results are recorded; Windows checks and cross-compilation do not count as macOS runtime passes.

## Common Builds

```shell
task run                      # build under .mygo/run/ and launch once
task package                  # build packages for the current system under build/
task package -- -platform windows/amd64,linux/amd64,linux/arm64 -skip-installer
```

`task run` skips the Windows installer and macOS DMG, though MyGo still generates Linux distribution packages under `.mygo/run/` on Linux.

`task package` writes artifacts to `build/<target>/`:

| Target | Outputs |
| --- | --- |
| `windows/amd64`, `windows/arm64` | Executable and NSIS installer (`flowlens-setup-<version>-<arch>.exe`). MyGo downloads NSIS automatically on Windows; on Linux, install `nsis` or pass `-skip-installer`. |
| `darwin/arm64`, `darwin/universal` | Application bundle (`.app`) and disk image (`.dmg`). Must be built on macOS. |
| `linux/amd64`, `linux/arm64` | Executable, Debian (`.deb`) and RPM (`.rpm`) packages, `flowlens-<version>-linux-<arch>.tar.gz`, a separate `install.sh`, and a `PKGBUILD` with its icon source files. |

### Packaging Metadata and Formats

- **Linux formats:** `linux.formats` is left unset in `mygo.json` to keep MyGo's defaults (`deb`, `rpm`, and `pkgbuild`), and the tar archive is always produced. RPM architectures use `x86_64` and `aarch64`. `mygo build -upload` publishes `.deb`, `.rpm`, `.tar.gz`, and `install.sh` (keep `install.sh` beside its matching archive and run `sh install.sh` to install locally). The `PKGBUILD` and icon sources stay in the local build directory for `makepkg -si` on compatible Arch Linux systems.
- **Linux dependencies:** `linux.webview` is `false`, so Linux packages depend on GTK 3 without WebKitGTK, and neither `install.sh` nor `mygo doctor` requires WebKitGTK. The Linux maintainer, short description, and `Development`/`Network` desktop categories are configured in `mygo.json`.
- **Package identity and licenses:** Publisher, copyright, and license identifier (`MIT`) come from `mygo.json`. The root `LICENSE` is bundled via `resources` (beside the executable on Windows/Linux, or in `Contents/Resources` on macOS). The font license at `fonts/OFL.txt` is retained, while embedded fonts and PNG images are excluded from duplicate resource copies.
- **Signed update artifacts:** When `MYGO_UPDATER_PRIVATE_KEY` is set, every target also produces a signed update archive (`flowlens-<version>-<target>.tar.gz`) and manifest (`update-<target>.json`), where targets are `windows-amd64`, `windows-arm64`, `darwin-arm64`, `darwin-universal`, `linux-amd64`, and `linux-arm64`. Linux reuses its standard tar archive for updates. `mygo build -upload` uploads these alongside installers and DMGs. Without the signing key, Windows and macOS builds omit update archives and manifests.

## Application and Tray Icons

All platforms share the same shield-and-nodes artwork generated from `assets/icons/app-icon.svg` and, for the macOS menu bar, `assets/icons/tray-template.svg`:

| Usage | Generated File |
| --- | --- |
| Windows and Linux app/tray icon; macOS Dock icon under `mygo dev` | `resources/icon.png` |
| macOS application bundle (`macos.icon` in `mygo.json`) | `assets/macos/app-icon.icns` |
| macOS menu bar | `resources/trayicon-template.png` |

After editing either SVG source, run `task icons:generate` (or `go run ./assets/icons/generate.go`) to regenerate all three outputs with supersampled anti-aliasing. The generated `.icns` includes multiple resolutions and works with the macOS 13.0 deployment target without Icon Composer or a newer Xcode.

The macOS menu-bar icon is a 72-pixel template image displayed at 18 points with `IconIsTemplate`, allowing AppKit to tint it for the menu bar's appearance and selection state. Verify the packaged `.icns` on macOS via `task run` or `task package`: inspect Finder and Dock at small sizes, and verify the tray icon in light, dark, and selected states on both 1x and Retina displays.

## Updates

`mygo.json` configures the update source (`updates.github`) and verification public key (`updates.publicKey`). FlowLens installs only updates signed with the matching private key.

- **Signing key:** Store the contents of `mygo-update.key` in the GitHub Actions secret `MYGO_UPDATER_PRIVATE_KEY` (already wired into the release workflow), or set the same environment variable for local signed builds. Keep the private key out of Git, back it up securely, and reuse it across releases.
- **Manual fallback:** Development builds and installations without write access (including package-managed installs) direct users to the release page instead of running in-app installation.
- **Progress reporting:** FlowLens drives MyGo's `Updater.Check` and `Update.Install` from its native update window. Installation is a single operation: download bytes are reported as progress, while signature verification and file replacement have no separate progress callbacks, so the window transitions from byte progress to an indeterminate finishing state. A delta fallback resets byte progress and re-enables download cancellation.
- **Restart lifecycle:** **Restart automatically when finished** defaults to on for the current session and is captured when installation starts. On success, FlowLens requests a single restart through its standard unsaved-work guards and normal shutdown sequence, even if the update window was closed. Cancellation, failure, or application shutdown suppresses automatic restart. If installation finishes concurrently with cancellation, the installed update remains ready for a later restart; declining a quit guard keeps it ready without re-prompting, and a failed restart can be retried without re-downloading.
- **File replacement:** On Windows and Linux, MyGo replaces program files during installation; on macOS, it stages an application bundle and swaps it after the running process exits. **Restart later** defers launching the new version, not installing it. Closing or hiding the update window never cancels an active installation. Explicit cancellation applies during checks and incomplete downloads, and quitting FlowLens requests cancellation and waits for the framework call to finish. MyGo's file-replacement recovery is not a post-launch health-check rollback.

## Releasing

`mygo.json` is the single source of truth for the application version. `go tool mygo dev` and `go tool mygo build` embed it, and FlowLens reads it via `mygo.App.Version()` for environment info, update checks, and HAR exports. Plain `go run` or `go build` without MyGo metadata reports `0.0.0-dev`. (`go tool mygo version` prints the MyGo CLI version and does not change the application version.)

To cut a release:

1. Add a `## <version>` section to `CHANGELOG.md` (its content becomes the in-app release notes).
2. Update `version` in `mygo.json` to the matching version (for example, `1.2.3`) and commit.
3. Push the matching tag (for example, `v1.2.3`).

### Release Workflow Behavior

- `.github/workflows/release.yml` verifies that the tag (or manually supplied `workflow_dispatch` version) matches `mygo.json` and creates a draft GitHub Release before running verification and build jobs.
- Each `mygo build -upload` validates the update configuration and checks that `MYGO_UPDATER_PRIVATE_KEY` matches `updates.publicKey` before compiling or uploading. MyGo also extracts update notes from `CHANGELOG.md`.
- The workflow builds Windows `amd64`/`arm64` on `windows-latest`, Linux `amd64`/`arm64` on `ubuntu-latest`, and macOS `arm64`/`universal` on `macos-latest`, all with `CGO_ENABLED=0`. `darwin/universal` bundles `arm64` and `amd64` into one app; specifying `darwin/arm64,darwin/amd64` would instead build two separate apps.
- Build jobs run sequentially (`max-parallel: 1`) because MyGo merges and replaces the shared `SHA256SUMS` file on each upload.
- The final `check-assets` job verifies both Windows installers, both macOS DMGs, all six platform update archives, both Debian and RPM architectures, `install.sh`, their `SHA256SUMS` entries, and all six `update-<target>.json` manifests (which MyGo intentionally excludes from `SHA256SUMS`).
- Windows executables and installers are unsigned; macOS bundles use MyGo's default ad-hoc signing and are not notarized. No platform-signing certificates or secrets are required beyond `MYGO_UPDATER_PRIVATE_KEY`.

## Project Website

The project homepage is a static Astro and TypeScript site in `_website/`. It is separate from the application:

- Go skips directories whose names start with `_`, so Go builds, tests, lint, and `go mod tidy` never read it, even with `node_modules` installed.
- `task verify`, CI, and release packaging never need Node.js.

To work on the site, install Node.js 24 and use the opt-in `website:install`, `website:dev`, `website:check`, and `website:build` tasks. See [_website/README.md](../../_website/README.md) for details.

- **Download links:** `_website/src/lib/release.ts` maps release assets to download buttons using the same names that the release workflow's `check-assets` job requires. Keep the two in step when asset names change. A missing asset links to its release page.
- **Deployment:** `.github/workflows/website.yml` deploys from `main` to GitHub Pages on a custom domain, which is set as `site` in `_website/astro.config.mjs`. Domain and DNS setup are in [_website/README.md](../../_website/README.md#custom-domain). The workflow:
  - Runs on pushes that change the site's inputs, on manual dispatch, and when a release is published (by dispatching a rebuild on `main`).
  - Pull requests only build and check the site.
  - Deployment builds fail rather than publish fallback links if the GitHub API cannot supply the latest release.
