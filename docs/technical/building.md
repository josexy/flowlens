# Building and Packaging

FlowLens is a Go application. Building it needs Go, the MyGo CLI that `go.mod` pins as a tool (`go tool mygo`), and preferably Task. Nothing else is required on Windows or Linux; macOS also needs the Xcode Command Line Tools.

## Cgo

| Target | `CGO_ENABLED` | Why |
| --- | --- | --- |
| Windows, Linux | `0` | Neither MyGo nor FlowLens needs cgo there, so one machine can build every target |
| macOS | `1` | Process attribution with icons and the managed system proxy use Objective-C |

The Taskfile sets this from the host system. A production `mygo build` for macOS with cgo disabled fails on purpose (`cgo_guard_darwin.go`), rather than producing an application without those features. Because one `mygo build` applies one `CGO_ENABLED` to all its targets, build macOS separately from Windows and Linux.

## Common Builds

```shell
task build                    # bin/flowlens for this system
task package                  # packages for this system, under build/
task package -- -platform windows/amd64,linux/amd64,linux/arm64 -skip-installer
```

`mygo build` writes to `build/<target>/`:

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

1. Add a `## <version>` section to `CHANGELOG.md`. Its text becomes the release notes.
2. Run `task version VERSION=1.2.3`, then commit.
3. Run `task release:check VERSION=1.2.3` with `MYGO_UPDATER_PRIVATE_KEY` set to check the signing key, metadata, and notes.
4. Push the tag `v1.2.3`.

`.github/workflows/release.yml` fails before building anything if the signing key, the public key, the metadata, or the release notes are missing. It then builds Windows and Linux with cgo disabled, builds macOS with cgo enabled, and uploads everything to a draft GitHub Release to review and publish.

### Optional Signing Secrets

Installers and applications are signed only when these are configured. A partial configuration fails the release.

| Platform | Secrets |
| --- | --- |
| Windows | `WINDOWS_CERTIFICATE_BASE64`, `WINDOWS_CERTIFICATE_PASSWORD`, optionally `WINDOWS_TIMESTAMP_SERVER` |
| macOS signing | `MACOS_CERTIFICATE_P12_BASE64`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGN_IDENTITY` |
| macOS notarization | `MACOS_NOTARY_APPLE_ID`, `MACOS_NOTARY_TEAM_ID`, `MACOS_NOTARY_PASSWORD` |
