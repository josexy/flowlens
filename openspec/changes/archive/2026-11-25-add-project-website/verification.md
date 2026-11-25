# Verification

Verified on Linux arm64 (Debian 13) on 2026-11-24, with Node.js 24.22.2, npm 11.19.1, Go 1.27.4, and the repository's MyGo v0.10.0. Pinned site dependencies: Astro 7.4.13, @astrojs/check 0.9.12, TypeScript 6.0.3, Vitest 5.1.1, and lucide-static 1.80.0. Nothing was deployed to GitHub Pages.

## Website

- **`task website:check`** (`astro check` and `vitest run`): passed.
  - Type checking found 0 errors, warnings, or hints across 29 files.
  - All 51 tests passed. They cover asset classification for every v2.0.1 asset, a prerelease Debian/RPM name, and the ignored update, delta, manifest, `install.ps1`, and old-version names.
  - Release model tests cover: complete, missing-asset, draft, prerelease, empty-tag, foreign-host, foreign-repository, malformed, non-uploaded, off-repository, and missing-size payloads.
  - Formatting tests cover sizes and dates.
  - Platform detection tests cover: Windows x64 and ARM64, macOS Safari and Chromium on ARM and Intel, Linux x86_64, aarch64, and `navigator.platform`, Android, iPhone, iPadOS desktop mode, ChromeOS (by user agent and by client hints), and unknown agents.
- **`task website:build`:** passed. It writes `dist/index.html` (`lang="en"`) and `dist/zh-cn/index.html` (`lang="zh-Hans"`).
  - Both pages have a canonical link and alternates for `en`, `zh-Hans`, and `x-default`, all on the custom domain set as `site`. The `og:url` and `og:image` values are absolute URLs on that domain.
  - Every site-relative path is `/`, `/zh-cn/`, or `/_astro/…`. No path keeps the former `/flowlens` base.
  - The fixture was captured from the live API's `releases/latest` (v2.0.1).
- **Release links:**
  - All 12 download links on each page are `browser_download_url` values of the live v2.0.1 release.
  - `curl -sIL` returned 200 for each of them.
  - The only repository files linked are `docs/user-guide.md` (including `#security`), `LICENSE`, and `SECURITY.md`, and each exists. No technical or developer guide is linked.
- **Offline build** (`NODE_USE_ENV_PROXY=1 HTTPS_PROXY=http://127.0.0.1:9`): succeeded with the warning `[website] Using latest-release page links…`.
  - All 12 links point to `https://github.com/josexy/flowlens/releases/latest`.
  - The release line reads "Latest release", and the hero version fact is hidden.
- **Same offline build with `SITE_REQUIRE_RELEASE=true`:** exited 1 with `Release data is required but unavailable`, and wrote no `index.html`.
- **Headless Chromium 1255 over CDP**, serving `dist/` at the site root, as the custom domain serves it. The API was mocked with a 403 unless stated otherwise:
  - **Windows:** x86 maps to the x64 installer and ARM to the ARM64 installer.
  - **macOS:** Chromium on ARM maps to the Apple Silicon DMG. Safari without hints maps to the Universal DMG.
  - **Linux x86 hints:** the action leads to `#download-linux`, with the x64 group marked.
  - **Detected-platform marking:** each recommended card shows its badge and the recommended asset, and the "All downloads" link appears.
  - **Android at 390px:** shows the generic `#download` action, with no badge.
  - **Chinese page:** shows localized labels, captions, and dates.
  - **JavaScript disabled:** the generic action stays, and the screenshot toggle is hidden. The built-in links remain.
  - **Mocked v2.0.2 payload:** the links, sizes, release line, notes link, hero version, and primary action update. Assets missing from the mock fall back to the v2.0.2 release page.
  - **Mocked foreign-URL payload:** ignored.
  - **Horizontal overflow:** 0 in every scenario.
- **Copy and layout revision** (user-facing wording, no technical documentation, the Linux card spanning the full width):
  - Rebuilt and re-ran the checks above: 51 tests, 12 live asset links per page, and every CDP scenario passed with 0 horizontal overflow.
  - Reviewed 1280px light and dark renders and a 390px render. Fixed a release line that wrapped with a leading separator and Linux package labels that broke mid-word.
- **Rendered screenshots:**
  - Reviewed at 1280px in light and dark (forced dark mode) and at 390px with an iPhone user agent.
  - Dark mode shows the dark product screenshot and palette.
  - Icons render after fixing the inline-SVG attribute rewrite, which had stripped inner `<rect>` sizes.
  - This host has no CJK fonts, so Chinese glyphs render as boxes in screenshots. The Chinese copy was checked as text in the HTML.

## Go and Application Isolation

The site lives in `_website/`, and `go.mod` and `go.sum` are identical to `HEAD`. An earlier `ignore ./website` directive was dropped when the directory was renamed.

- With `_website/node_modules` installed, `CGO_ENABLED=0 go list ./...` lists no `_website` package.
- **Probe:** a temporary invalid `.go` file was placed at `_website/node_modules/zz-go-probe/`.
  - `go list ./...` excluded it, and `CGO_ENABLED=0 go vet ./...` exited 0.
  - For comparison, under the old `website/` name with no `ignore` directive, `go list` had included `github.com/josexy/flowlens/website/node_modules/zz-go-probe`.
  - The probe was removed afterwards.
- **Go checks after the rename:**
  - `go mod tidy` followed by `git diff --exit-code go.mod go.sum` passed.
  - `task fmt:check` passed, and so did `CGO_ENABLED=0 go build ./...`.
  - Before the rename, `task vet:ui` and `task i18n:check` (1338 messages in en/zh-Hans) passed, and a MyGo `linux/arm64` package build succeeded. The rename touches no Go input.
- `task --list` shows `website:install`, `website:dev`, `website:check`, and `website:build`, all running in `_website/`. `verify` is unchanged.
- After the rename, `task website:check` passed (0 errors, 51 tests), and so did `task website:build`.

## Workflow

- `go run github.com/rhysd/actionlint/cmd/actionlint@latest` (v1.7.12) on `website.yml`, `ci.yml`, and `release.yml`: no findings. ShellCheck was not installed, so `run` scripts were not shell-linted.
- `_website/package-lock.json` contains the Linux x64 native packages that the `ubuntu-latest` runner needs, for example `@img/sharp-linux-x64` and `@rolldown/binding-linux-x64-gnu`.

## Acceptance and Limits

- `openspec validate add-project-website --strict --no-interactive`: passed.
- `git diff --check`: passed.
- **Spec sync:**
  - `build-and-release`: the "Development toolchain" and "Developer documentation" requirements were replaced with the delta versions, and the other nine requirements are unchanged.
  - `project-website`: created from the delta's Purpose and its 10 ADDED requirements.
  - The synced "Development toolchain" description was tightened from 508 to 452 characters to meet strict validation. Its meaning is unchanged.
  - `openspec validate --specs --strict --no-interactive`: all 18 specifications passed.
- **Archived before deployment:** the change was archived at the maintainer's request, before the first live deployment. The deployment items below remain unverified.
- Not exercised:
  - Live GitHub Pages deployment, the `github-pages` environment, and the `release: published` → `workflow_dispatch` path.
  - The custom domain's DNS, certificate, and HTTPS enforcement.

  These need the maintainer to set **Settings › Pages** (Source **GitHub Actions** and the custom domain), add the Cloudflare `CNAME` record, and run the workflow on GitHub.
- Not checked: real Safari, Firefox, Windows, and macOS browsers (detection was exercised with emulated user agents and client hints), and an x64 CI runner build.
