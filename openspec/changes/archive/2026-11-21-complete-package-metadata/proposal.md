# Proposal

## Why

FlowLens's native Linux packages still inherit WebKitGTK dependencies, and package metadata omits the project's MIT license and publisher. The release documentation says RPM is unavailable even though the pinned MyGo CLI generates and uploads it by default.

## What Changes

- Declare native Linux packaging, maintainer, description, and desktop categories while retaining MyGo's default package formats.
- Populate copyright, publisher, and MIT license metadata and ship the root LICENSE in every package.
- Align installation/build/release documentation and release asset checks with Debian, RPM, tar archives, and locally generated PKGBUILD files.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `build-and-release`: Specify native Linux package dependencies, package identity/license resources, and the existing default Linux release formats.

## Impact

The owners are `mygo.json`, release automation, and build/install documentation. Windows gains publisher/copyright metadata; every platform gains the license resource; Linux package metadata and declared dependencies change. App identifiers, version, update keys, signing policy, and macOS deployment target stay the same. Persistence, traffic protocols/models, Python Workers, process attribution, events/shortcuts, windows, and interface copy are unaffected.

Completion requires successful configuration/catalog checks, Windows executable and both Linux architecture package builds with cgo disabled, inspection of metadata/resources/dependencies, release-check positive/negative cases, and strict OpenSpec validation. Publishing releases, platform signing, native UI/runtime acceptance, and dependency upgrades are out of scope.
