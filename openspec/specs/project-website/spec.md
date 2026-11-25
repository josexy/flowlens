# project-website Specification

## Purpose
Defines the public FlowLens homepage: its localized content and its download navigation for the latest published release. It also defines the site's fallbacks, its presentation, its isolation from the application build, and its deployment.

## Requirements

### Requirement: Landing page content
The website SHALL publish a landing page per locale with these sections:
- Product summary with screenshots.
- Feature highlights.
- Getting-started steps.
- Downloads.
- Local-data and responsible-use notes.
- Links to the user guide, release notes, license, security policy, issue tracker, and source.

Copy SHALL use plain, user-facing language. The page SHALL NOT link technical or developer documentation.

#### Scenario: Visiting the homepage
- **WHEN** a visitor opens the English or Chinese homepage
- **THEN** the page shows the product summary, a light or dark application screenshot, the highlights, getting-started steps, the download section, and local-data and responsible-use notes
- **AND** the user guide link opens the repository's user guide

#### Scenario: Technical documentation
- **WHEN** a visitor looks through the homepage
- **THEN** it links no technical or developer guides, such as the Python plugin, rewrite-rule, metrics, building, or contributing documents

#### Scenario: Switching the product screenshot
- **WHEN** a visitor selects the light or dark screenshot option
- **THEN** the matching application screenshot is shown without reloading the page

### Requirement: Localized routes
The website SHALL serve English at its base path and Simplified Chinese under `zh-cn/`. Each page SHALL declare its language and link the other locale as an alternate, and a visible switch SHALL move between the two locales.

#### Scenario: Language metadata
- **WHEN** a crawler fetches either page
- **THEN** the document language is `en` or `zh-Hans` respectively, and alternate links name both locales

#### Scenario: Switching language
- **WHEN** a visitor uses the language switch
- **THEN** the other locale's homepage opens

### Requirement: Release download resolution
The website SHALL take download links, version, publication date, and file sizes from the latest published, non-prerelease GitHub Release. It SHALL use the same asset name patterns that the release asset check uses, and SHALL ignore update archives, delta files, and update manifests.

#### Scenario: Complete release
- **WHEN** the latest release contains the documented assets
- **THEN** the site links the Windows x64 and ARM64 installers, the macOS Apple Silicon and universal disk images, the Linux x64 and ARM64 Debian packages, RPM packages, and tar archives, `install.sh`, and `SHA256SUMS`
- **AND** each package link shows its file size

#### Scenario: Prerelease package names
- **WHEN** a Debian or RPM file name carries a rewritten version string
- **THEN** it is still matched by architecture and package format

#### Scenario: Update-only assets
- **WHEN** a release also contains per-target update archives for Windows or macOS, delta files, or `update-<target>.json` manifests
- **THEN** none of them is offered as a download

### Requirement: Download fallbacks
The website SHALL never link a guessed asset URL. A package missing from the release SHALL link to that release's page, and missing release data SHALL make every download link point to the repository's latest-release page.

#### Scenario: Missing asset
- **WHEN** the latest release lacks one package
- **THEN** that package's link opens the release page and the other links still point to their assets

#### Scenario: Release data unavailable during a local build
- **WHEN** the release API cannot be reached and a release is not required
- **THEN** the build succeeds with a warning, and every download link opens the latest-release page

#### Scenario: Release data required
- **WHEN** a build that requires release data cannot obtain a valid release
- **THEN** the build fails instead of publishing fallback links

### Requirement: Live release refresh
When scripts run, the page SHALL check the latest release in the browser and, if its tag differs from the one built into the page and its data is valid, SHALL update the download links, sizes, version, and date. Failures SHALL leave the built-in links unchanged.

#### Scenario: Newer release published after deployment
- **WHEN** the browser receives a valid latest release with a different tag
- **THEN** the download links, sizes, version, and date show that release

#### Scenario: Refresh failure
- **WHEN** the request fails, is rate-limited, or returns invalid data
- **THEN** the page keeps the links it was built with and shows no error

### Requirement: Platform recommendation
When scripts run, the primary download action SHALL recommend a package for the visitor's detected desktop OS and architecture and SHALL mark the matching platform in the download section. Without detection or scripts, it SHALL lead to the full download section.

#### Scenario: Windows visitor
- **WHEN** a Windows visitor's architecture is ARM64
- **THEN** the primary action downloads the ARM64 installer; otherwise it downloads the x64 installer

#### Scenario: macOS visitor
- **WHEN** a macOS visitor is detected as Apple Silicon
- **THEN** the primary action downloads the Apple Silicon disk image; when the architecture is unknown it downloads the universal disk image

#### Scenario: Linux visitor
- **WHEN** a Linux visitor is detected
- **THEN** the primary action leads to the Linux packages and highlights the detected architecture, without choosing a package format

#### Scenario: Unsupported or unknown platform
- **WHEN** the visitor uses a mobile OS, an unknown OS, or has scripts disabled
- **THEN** the primary action leads to the full download section

### Requirement: Download guidance
The download section SHALL state the checksum verification file, the expected first-launch warnings for unsigned Windows and ad-hoc signed macOS packages, the Linux GTK 3 requirement, and the manual one-time upgrade from 1.0.x.

#### Scenario: Reading download notes
- **WHEN** a visitor reads the download section in either locale
- **THEN** those notes are present and link `SHA256SUMS` and the release notes

### Requirement: Presentation and accessibility
The website SHALL follow the visitor's light or dark preference and adapt to narrow screens. It SHALL provide a skip link, visible keyboard focus, and text alternatives for images, and SHALL keep all content and download links usable without JavaScript.

#### Scenario: Dark preference
- **WHEN** the visitor's system prefers a dark appearance
- **THEN** the site uses its dark palette and initially shows the dark application screenshot

#### Scenario: Narrow screen
- **WHEN** the page is viewed on a phone-width screen
- **THEN** sections stack into a single column without horizontal scrolling

### Requirement: Isolation from the application build
The website SHALL live in its own directory, excluded from the Go module, so that Go builds, tests, lint, module tidying, packaging, and `task verify` never read website files or need Node.js.

#### Scenario: Installed website dependencies
- **WHEN** website dependencies are installed and Go packages are listed for the module
- **THEN** no package under the website directory is included, and module tidying leaves `go.mod` and `go.sum` unchanged

### Requirement: Website deployment
The website SHALL deploy to GitHub Pages when site inputs change on the main branch, when a release is published, or on manual dispatch. Pull requests that touch site inputs SHALL build and check the site without deploying it.

#### Scenario: Custom domain URLs
- **WHEN** the site is built
- **THEN** pages, assets, canonical links, language alternates, and social previews use the root of the configured custom domain, with no repository base path

#### Scenario: Release published
- **WHEN** a maintainer publishes a GitHub Release
- **THEN** the site rebuilds with that release's links and is redeployed

#### Scenario: Pull request
- **WHEN** a pull request changes website files
- **THEN** type checks, tests, and the production build run, and nothing is deployed
