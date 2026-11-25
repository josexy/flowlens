# FlowLens Website

The FlowLens homepage, a static Astro and TypeScript site. English is served at `/` and Simplified Chinese at `/zh-cn/`.

The website is optional. Building, testing, and packaging the FlowLens application never need Node.js. Go tooling skips this directory because its name starts with `_`, and `task verify` does not run website tasks.

## Development

Requires Node.js 24 (see `.node-version`). From the repository root:

```shell
task website:install   # npm ci
task website:dev       # http://127.0.0.1:4321/
task website:check     # astro check + vitest
task website:build     # static output in _website/dist
```

Run `npx astro preview` after a build to serve `dist/`. Astro collects anonymous telemetry unless `ASTRO_TELEMETRY_DISABLED=1` is set, or you run `npx astro telemetry disable` once.

## Content and Assets

- Copy lives in `src/i18n/en.ts` and `src/i18n/zh-cn.ts`. The Chinese dictionary is typed against the English one, so a missing key fails `astro check`.
- Write for users: say what a feature does for them, not how it works. Link the user guide for details. Technical and developer documentation stays in the repository and is not linked from the site.
- Screenshots (`screenshots/`), the logo (`assets/icons/app-icon.svg`), and the touch icon (`resources/icon.png`) are imported from the repository and optimized at build time. Do not copy them into this directory.
- The release repository, license, copyright, and minimum macOS version come from `mygo.json`.

## Download Links

At build time, `src/lib/github.ts` reads the latest published release (drafts and prereleases are excluded). `src/lib/release.ts` then maps its assets to download buttons:

- **Asset patterns:** They mirror the `check-assets` job in `.github/workflows/release.yml`. Update both together if release asset names change.
- **Ignored assets:** Update archives for Windows and macOS, delta files, and update manifests are never offered.
- **Missing assets:** A missing asset links to the release page. If no release data is available, every link opens `https://github.com/josexy/flowlens/releases/latest`.
- **In the browser:** `src/scripts/downloads.ts` recommends a package for the visitor's OS and architecture. If GitHub reports a newer release than the page was built with, it updates the links. Every link still works without JavaScript.

| Variable | Effect |
| --- | --- |
| `GITHUB_TOKEN` | Optional. Authenticates the build-time API request to avoid the anonymous rate limit. |
| `SITE_REQUIRE_RELEASE=true` | Fails the build when release data cannot be fetched or validated, instead of falling back to the latest-release page. Deployments set it. |

## Deployment

`.github/workflows/website.yml` deploys the site to GitHub Pages:

- **Push to `main`:** Changes to the website, screenshots, icons, `mygo.json`, or the workflow trigger a build and deploy.
- **Release published:** Publishing a GitHub Release dispatches a rebuild on `main`, so the new release's links are baked in. This works around the `github-pages` environment allowing only the default branch to deploy.
- **Manual:** Run the workflow from `main` with **Run workflow**.
- **Pull requests:** These type-check, test, and build the site without deploying.

### Custom Domain

GitHub Pages hosts the site at the root of a custom domain, with DNS on Cloudflare. The domain is set once, as `site` in `astro.config.mjs`. Canonical, language-alternate, and social-preview URLs all derive from it.

One-time setup:

1. **GitHub:** in the repository's **Settings › Pages**, set **Source** to **GitHub Actions**. Then enter the domain from `site` as the **Custom domain**. Actions deployments use this setting, so the site needs no `CNAME` file.
2. **Cloudflare DNS:** add a `CNAME` record for that subdomain that points to `josexy.github.io`.
   - Keep it **DNS only** (grey cloud) until GitHub has issued the certificate, then turn on **Enforce HTTPS**.
   - If you later proxy the record through Cloudflare, set **SSL/TLS** to **Full (strict)**. **Flexible** combined with Enforce HTTPS causes a redirect loop.
3. **Optional:** verify the parent domain under your GitHub account's **Settings › Pages › Verified domains** so no other repository can claim the subdomain.
