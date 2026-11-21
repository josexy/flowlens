# Design

## Context

MyGo v0.10.0 defaults `linux.webview` to true and `linux.formats` to deb/rpm/pkgbuild. Its upload filter publishes RPM and tar/deb files but excludes PKGBUILD and its icon files. See proposal.md for the observed metadata and documentation gaps.

## Decisions

- Keep `linux.formats` absent as requested, instead of pinning a narrower list. Set `linux.webview` false to remove the unused WebKitGTK dependency while preserving GTK.
- Use the existing LICENSE copyright and repository commit identity for publisher/maintainer metadata. Use `license: MIT` and `resources: [LICENSE]`; setting a license name alone does not copy its text.
- Keep the existing embedded-font/image exclusions and retain `fonts/OFL.txt` in package resources.
- Update README, changelog, building instructions, and draft release download guidance. Extend the existing release asset checker to require both RPM architectures, the separate install.sh, and checksum entries; keep its prerelease-friendly filename matching. Inspecting the generated archive confirmed install.sh is a sibling file, not a member of the archive.
- Keep changes in packaging configuration and automation. There are no service, asynchronous state, storage/codec, UI, localization, or lifecycle changes and no data migration. Reverting these configuration/documentation changes restores the previous packaging behavior.

## Verification and limitations

Use CGO_ENABLED=0 to build Windows amd64 and Linux amd64/arm64 into an isolated .mygo directory. Inspect Windows version properties, Linux package manifests/payloads, PKGBUILD, install.sh, license text, and checksums. Exercise the release checker with generated artifact lists and missing-RPM/missing-checksum cases. Validate the workflow syntax and the OpenSpec delta. No runtime behavior changed; native macOS packaging/signing and Linux installation are outside this Windows-host verification.
