# Spec Delta

## MODIFIED Requirements

### Requirement: Release artifacts
Releases SHALL provide Windows x64 and ARM64 NSIS installers, macOS Apple Silicon and universal disk images, and Linux x64 and ARM64 Debian packages, RPM packages, and tar archives with a separate install.sh and a checksum file. Local Linux packaging SHALL also generate a PKGBUILD for each architecture using the default package formats.

#### Scenario: Building a release
- **WHEN** the release workflow runs for a version tag
- **THEN** those installers, disk images, Debian packages, RPM packages, tar archives, and install.sh are published to a draft GitHub Release
- **AND** the release asset check fails if an expected RPM architecture, install.sh, or their checksum entries are missing

#### Scenario: Local Linux packages
- **WHEN** a developer packages Linux x64 or ARM64 with the repository configuration
- **THEN** the output includes a Debian package, an RPM package, a tar archive, a separate install.sh, and a PKGBUILD with its required icon files
- **AND** the PKGBUILD and its icon source files remain local build outputs rather than automatically uploaded release assets

## ADDED Requirements

### Requirement: Native Linux package dependencies
Linux packages SHALL declare GTK dependencies for the native interface without requiring WebKitGTK, and the archive installer SHALL NOT require WebKitGTK. Desktop entries SHALL identify FlowLens as a development and networking application with a short description and package metadata SHALL identify its maintainer.

#### Scenario: Inspecting native Linux packages
- **WHEN** a Debian package, RPM package, PKGBUILD, or archive installer is generated
- **THEN** package dependencies retain GTK and omit WebKitGTK, and the archive installer omits the WebKitGTK prerequisite check
- **AND** the package and desktop metadata describe FlowLens rather than relying on generic defaults

### Requirement: Package identity and license
Packaged applications SHALL include the project's LICENSE text. Windows file properties and the installer SHALL identify the publisher; Windows file properties and macOS bundle metadata SHALL include the project's copyright notice. Linux formats that declare a license SHALL identify it as MIT.

#### Scenario: Inspecting a package
- **WHEN** a desktop application is packaged
- **THEN** LICENSE is included in its application resource directory alongside the retained font license
- **AND** the platform's supported publisher, copyright, and license metadata matches the project
