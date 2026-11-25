# Design

## Context

- **Toolchain:** The application builds with Go, Task, and MyGo only. CI and `task verify` must stay Node-free (see `build-and-release`).
- **Release assets:** The release workflow (`.github/workflows/release.yml`) publishes a draft release, which a maintainer then publishes. Asset names contain the version, and the `check-assets` job defines the patterns the website has to match. The v2.0.1 release also contains assets that are not user downloads: per-target update archives, `.delta` files, `update-*.json` manifests, and `install.ps1`.
- **Branding:** The repository already holds the brand artwork (`assets/icons/app-icon.svg`, `resources/icon.png`) and light/dark screenshots (`screenshots/`).
- **Copy:** The page content is based on `README.md` and `README.zh-CN.md`, rewritten in plain language for users. Protocol, signing, and packaging terms are replaced with what the visitor needs to do or know, and technical details stay in the repository's guides.

## Goals / Non-Goals

**Goals:**
- Static output that needs no runtime server and is fully usable without JavaScript.
- Add as few dependencies as possible, and reuse the repository's existing artwork and copy instead of copying them into the site.

**Non-Goals:**
- A theme toggle, client-side routing, a CMS, or a docs site generator.
- Reading `mygo.json`'s version for links: the version may be bumped before its release is published.
- Recreating the screenshots per locale: the repository's screenshots are reused as they are.

## Decisions

### Framework, location, and isolation

- **Astro with strict TypeScript in `_website/`.** Astro renders static HTML and ships only the small client script this site needs. It also has built-in i18n routing and `astro:assets` image optimization.
  - *Rejected:* Vite with plain TypeScript, which would need hand-built localization and image handling.
  - *Rejected:* React, which would ship a runtime for a mostly static page.
- **Repository artwork is imported directly.** Screenshots, the SVG icon, and the PNG icon are imported from their repository paths through `astro:assets`, so artwork updates reach the site automatically. Vite's dev server is allowed to read the repository root.
- **`_website/` directory name.** Some npm packages ship `.go` files, which would break `go test ./...`, golangci-lint, and `go mod tidy` once `node_modules` exists. Go skips directories whose names start with `_` when matching packages, so the leading underscore keeps all Go tooling out without touching `go.mod`.
  - *Rejected:* an `ignore ./website` directive in `go.mod` (Go 1.25+). It works as well, but changes the module file for a non-Go concern.
  - Tools that walk paths directly, such as `gofmt -l .`, are not covered. The repository's format check names `main.go internal` explicitly.
  - *Rejected:* renaming the directory to `_website`. That is less clear, and it does not protect tools that walk directories by path.
- **npm with a committed lockfile and `.node-version` set to 24.** Node.js ships with npm, so CI needs no extra package-manager setup.
- **Opt-in Task entries.** `website:install`, `website:dev`, `website:check`, and `website:build` all run in `_website/`. None is added to `verify`.

### Release model

The release model is pure TypeScript, shared by the build and the browser.

- **Classification.** `classifyAsset` maps an asset name to a download key, using the same patterns as `check-assets`.
  - The Windows installers, the DMGs, and the Linux tar archives are matched by their exact version-stamped names, using the version from `tag_name` with the leading `v` removed.
  - Debian and RPM packages are matched only by architecture and extension, so prerelease version rewrites still match.
  - Every other asset is ignored.
- **`buildReleaseModel`.** It validates an untrusted API payload and returns `null` when the payload is unusable: the fields have the wrong types, the release is a draft or prerelease, or the URLs are not `https://github.com/`. Otherwise it returns `{tag, version, publishedAt, htmlUrl, items}`.
- **Missing keys.** A key that is absent renders as a link to `htmlUrl`, so no asset URL is ever guessed.
- **Repository slug.** The slug comes from `mygo.json`'s `updates.github`, which is the source of truth for the release repository.

### Build-time and runtime data

- **At build time,** `/repos/{slug}/releases/latest` is fetched once per build and cached in memory, with a 15-second timeout. `GITHUB_TOKEN` is used when it is set, which avoids the 60-requests-per-hour anonymous limit in CI.
  - Locally, a failure logs a warning and the build uses the fallback model.
  - With `SITE_REQUIRE_RELEASE=true`, as the deploy workflow sets it, a failure throws and fails the build.
- **In the browser,** a client script fetches the same endpoint anonymously and caches the response in `sessionStorage` for 10 minutes.
  - It rebuilds the model only when the tag differs from the page's `data-release-tag`.
  - It patches elements marked `data-asset-key`, plus the version and date nodes.
  - Errors are swallowed, so the built-in links stay in place.
- **Why both:** baked links work without JavaScript and are crawlable. The refresh covers the gap between publishing a release and the redeploy, and keeps links right if a deploy fails.

### Platform detection

- Detection is a pure function of `userAgent`, `navigator.platform`, `maxTouchPoints` (which separates iPadOS from macOS), and the optional Chromium `userAgentData` values: `platform`, plus `architecture` from `getHighEntropyValues`.
  - Windows defaults to x64 and macOS to Universal unless ARM is detected. Safari cannot reveal Apple Silicon, and the Universal disk image runs on both architectures.
  - Linux reports only the architecture, so the primary action scrolls to the Linux card instead of guessing between deb and rpm.
  - Android, iOS, iPadOS, ChromeOS, and unknown systems get the generic action.
- The generic action is a link to `#download`. It is rendered in HTML and stays when scripts are off.

### Localization and presentation

- **Routing.** Astro i18n uses `defaultLocale` `en` and `zh-cn`, with `prefixDefaultLocale: false`. Both pages render one `Home` component from a typed dictionary, and the Chinese dictionary must satisfy the English one's type, so a missing key fails `astro check`. The page language is `zh-Hans` for Chinese.
- **Styling.** Plain CSS custom properties use brand blues from the icon gradient, plus a `prefers-color-scheme` dark palette.
- **Screenshot.** The light and dark shots are two lazy-loaded `<picture>` elements in AVIF and WebP. CSS shows the one matching the system preference, so the hidden one is `display: none` and is never fetched. A two-button control, revealed only when scripts run, lets visitors switch the shot manually.
- **Icons.** They are inlined from `lucide-static` at build time, so no icon font or client code is needed.

### Deployment

- **`.github/workflows/website.yml`** has three jobs:
  - `build` runs `npm ci`, `npm run check`, and `npm run build`, then uploads the Pages artifact.
  - `deploy` uses `actions/deploy-pages`. Both upload and deploy run only for `main`, never for pull requests.
  - `redeploy-on-release` handles published releases (see below).
- **Triggers:**
  - `push` to `main` with path filters for `_website/**`, `screenshots/**`, `assets/icons/app-icon.svg`, `resources/icon.png`, `mygo.json`, and the workflow.
  - The same paths on `pull_request`.
  - `release: published`.
  - `workflow_dispatch`.
- **Release published.** A release run executes on the tag ref, and the `github-pages` environment allows only the default branch by default. So `redeploy-on-release` runs `gh workflow run website.yml --ref main`, with `actions: write` permission. GitHub lets `GITHUB_TOKEN` create `workflow_dispatch` runs.
  - *Rejected:* adding a tag rule to the environment, which is a manual setting that is easy to lose.
- **`SITE_REQUIRE_RELEASE`.** It is `true` for every non-PR build, so a deployment never publishes fallback links. Pull requests tolerate API outages.
- **Concurrency.** Non-PR runs share one group and never cancel a deploy that is in progress. A queued run may be superseded by a newer one, which rebuilds with the latest release anyway. Pull request runs cancel their own older runs.
- **Custom domain.** GitHub Pages serves the site at the root of a custom domain, with DNS on Cloudflare, so there is no base path. `site` in `astro.config.mjs` is the only place the domain is set: canonical, alternate, and social-preview URLs derive from it, and the layout fails the build if it is missing.
  - The custom domain is a repository Pages setting, because Actions deployments ignore `CNAME` files.
  - The Cloudflare record is a `CNAME` to `josexy.github.io`, kept DNS-only until GitHub issues the certificate. A proxied record would need SSL/TLS **Full (strict)**, because **Flexible** loops with Enforce HTTPS.
  - *Rejected:* the default `josexy.github.io/flowlens` project URL. *Rejected:* Cloudflare Pages hosting, which would need Cloudflare credentials in CI.

## Risks / Trade-offs

- [Release asset names change in MyGo or the workflow] → The patterns mirror `check-assets`, `AGENTS.md` and the building guide record the coupling, and missing keys fall back to the release page instead of a broken URL.
- [Anonymous browser requests hit GitHub's rate limit] → The response is cached in the session and failures are silent. The baked links stay correct after a redeploy.
- [The GitHub API is unavailable during a deploy] → The build fails because `SITE_REQUIRE_RELEASE` is set. The previous deployment keeps serving until the workflow is re-run.
- [`release: published` does not fire for releases published by `GITHUB_TOKEN`] → Releases are published manually today. Manual `workflow_dispatch` and the browser refresh cover other paths.
- [The redeploy dispatch fails, for example because Actions permissions are restricted] → The release job fails visibly. Re-run it or dispatch the workflow manually; the browser refresh keeps links current meanwhile.
- [Pages is not enabled] → The deploy job fails clearly. Enabling it is a one-time maintainer step documented in `_website/README.md`.
- [The custom domain or DNS record is missing or misconfigured] → The deploy succeeds, but the site does not resolve or serves certificate errors. The DNS and HTTPS steps are documented in `_website/README.md`, and the domain can be verified on the account to prevent takeover.

## Migration Plan

There are no data or application changes to migrate.

**Rollout**
1. Merge the change.
2. Set **Settings › Pages › Source** to *GitHub Actions*.
3. Run the workflow, or push to `main`.

**Rollback**
- Disable Pages or delete the workflow.
- Removing `_website/` and the workflow restores the previous repository state.
