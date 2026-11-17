# Building and Packaging

FlowLens is a Go application. Building it needs Go, the MyGo CLI that `go.mod` pins as a tool (`go tool mygo`), and preferably Task. Nothing else is required on Windows or Linux; macOS also needs the Xcode Command Line Tools.

## Cgo

| Target | `CGO_ENABLED` | Why |
| --- | --- | --- |
| Windows, Linux | `0` | Neither MyGo nor FlowLens needs cgo there, so one machine can build every target |
| macOS | `1` | Process attribution with icons and the managed system proxy use Objective-C |

The Taskfile sets this from the host system. A production `mygo build` for macOS with cgo disabled fails on purpose (`cgo_guard_darwin.go`), rather than producing an application without those features. Because one `mygo build` applies one `CGO_ENABLED` to all its targets, build macOS separately from Windows and Linux.

## CI Verification

CI and release verification run only on Ubuntu 24.04 with `CGO_ENABLED=0`. They invoke `gofmt`, `go tool mygo vet .`, `go tool mygo i18n check`, and `go test ./...` directly, without installing Task or golangci-lint. CI also checks that `go mod tidy` leaves `go.mod` and `go.sum` unchanged. Verification uses a separate Go cache from release packaging.

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
| `windows/amd64` | Executable and `flowlens-setup-<version>-amd64.exe`, made with NSIS. On Linux, install `nsis` or pass `-skip-installer`. |
| `darwin/arm64`, `darwin/universal` | Application bundle and disk image. These need macOS. |
| `linux/amd64` | Executable, Debian package, `flowlens-<version>-linux-amd64.tar.gz` with `install.sh`, and a `PKGBUILD` |

## Updates

Installed applications accept only updates signed with the key whose public half is built into them. One key pair serves all releases:

1. Run `go tool mygo keygen`. It writes `mygo-update.key` and `mygo-update.pub` outside the repository. Never commit the private key.
2. Add this to `mygo.json`, with the contents of `mygo-update.pub`:

   ```json
   "updates": {
     "publicKey": "<contents of mygo-update.pub>",
     "github": "josexy/flowlens"
   }
   ```

3. Store the private key as a repository secret: `gh secret set MYGO_UPDATER_PRIVATE_KEY < path/to/mygo-update.key`.

Losing the private key means installed copies can no longer update; they must be reinstalled from a release built with a new key.

An installation updates itself when MyGo can write to where it is installed. Packages installed by a package manager, such as the Debian package, and development builds cannot, so the Software Update window offers the release page instead.

## Releasing

`mygo.json` is the single source of the application version. `go tool mygo dev` and `go tool mygo build` embed it, and FlowLens reads it through `mygo.App.Version()` for environment details, update checks, and HAR exports. Plain `go run` or `go build` without MyGo metadata uses `0.0.0-dev`. `go tool mygo version` prints the framework CLI version; it does not change the application version.

1. Add a `## <version>` section to `CHANGELOG.md`. Its text becomes the release notes.
2. Set `version` in `mygo.json` to the same version, such as `1.2.3`, then commit.
3. Push the matching tag `v1.2.3`.

`.github/workflows/release.yml` checks that the tag or manually supplied version matches `mygo.json`. Each `mygo build -upload` validates the update configuration and signing key, including whether the private key matches the public key, before compiling or uploading. MyGo also handles update notes from `CHANGELOG.md`. The workflow builds Windows and Linux with cgo disabled, builds macOS with cgo enabled, and uploads everything to a draft GitHub Release to review and publish. The draft is created before these build checks.

Windows executables and installers are unsigned. macOS applications use MyGo's default ad-hoc signing and are not notarized. No platform-signing certificates or secrets are required. `MYGO_UPDATER_PRIVATE_KEY` is still required to sign update archives.
