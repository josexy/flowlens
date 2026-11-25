# Tasks

## 1. Website foundation

- [x] 1.1 Scaffold `_website/`:
  - `package.json` and its lockfile, `.node-version`, and `.gitignore`.
  - `astro.config.mjs` (site, base `/flowlens`, i18n, and repository-root `fs.allow`) and a strict `tsconfig.json`.
  - Vitest configuration.
  - Run `npm ci` in `_website/`.
- [x] 1.2 Name the directory `_website/` so Go tooling skips it. With `_website/node_modules` populated:
  - Confirm that `CGO_ENABLED=0 go list ./... | grep -c _website` prints `0`.
  - Confirm that `go mod tidy && git diff --exit-code go.mod go.sum` and `task fmt:check` pass.
  - Run `CGO_ENABLED=0 go build ./...`.
- [x] 1.3 Add the opt-in `website:install`, `website:dev`, `website:check`, and `website:build` Task entries, keeping them out of `verify`, and confirm them with `task --list`.

## 2. Release data and platform detection

- [x] 2.1 Implement the pure release model, which classifies assets and validates the payload.
  - Capture the v2.0.1 fixture with `gh release view v2.0.1 --repo josexy/flowlens --json …`.
  - Test complete, missing-asset, prerelease Debian/RPM, update-only asset, draft, prerelease, foreign-URL, and malformed payloads.
- [x] 2.2 Implement the build-time fetch: token support, a timeout, a local fallback, and the `SITE_REQUIRE_RELEASE=true` failure.
- [x] 2.3 Implement pure platform detection, and test it with Windows x64/ARM64, macOS Safari and Chromium ARM, Linux x86_64/aarch64, Android, iOS/iPadOS, ChromeOS, and unknown agents.

## 3. Pages

- [x] 3.1 Add the typed English and Chinese dictionaries, the base layout (language, metadata, alternates, favicon, skip link), and the global light/dark styles.
- [x] 3.2 Build the header, hero, screenshot switcher, features, how-it-works, downloads, privacy, and footer components. Wire up the `/` and `/zh-cn/` pages. Link only the user guide, with no technical documentation, and keep the copy user-facing.
- [x] 3.3 Add the client script for the platform recommendation and the live release refresh.
- [x] 3.4 Run `npm run check` (`astro check` and `vitest run`) and `npm run build` in `_website/`.
- [x] 3.5 Inspect `dist/`:
  - Language and alternates for both pages.
  - Every download link matches a live asset, and a HEAD request on each returns 200.
  - An offline build falls back, and an offline build with `SITE_REQUIRE_RELEASE=true` fails.
  - Preview both locales at desktop and phone widths, in light and dark.

## 4. Deployment and documentation

- [x] 4.1 Add `.github/workflows/website.yml` (build on PR, deploy on main, release publish, or dispatch), and lint it with `actionlint` or review it by hand.
- [x] 4.2 Document the website:
  - Its own README: develop, build, environment variables, the Pages setting.
  - A website section in `docs/technical/building.md`.
  - The ownership row, verification row, and release-asset coupling in `AGENTS.md`.

## 5. Acceptance

- [x] 5.1 Run `openspec validate add-project-website --strict --no-interactive` and `git diff --check`, compare the implementation against every scenario, and record the evidence and remaining limits below.

Evidence for every task is recorded in `verification.md`.

## Workflow follow-up

- Live deployment requires the maintainer to set Pages to GitHub Actions, set the custom domain, and add the Cloudflare `CNAME` record. Archive the change after the first successful deploy, then run `openspec validate --specs --strict --no-interactive`.
