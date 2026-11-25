# Proposal

## Why

FlowLens has no homepage. Visitors land on the GitHub README, where downloads are listed as a table of version-stamped release asset names, so they must work out which installer matches their OS and architecture themselves. A small static site can introduce the product in English and Simplified Chinese and send each visitor to the right package for the latest published release.

## What Changes

- Add a self-contained static website under `_website/`, built with Astro and TypeScript, with an English page at `/` and a Simplified Chinese page at `/zh-cn/`. It covers the product summary, light/dark screenshots, highlights, getting-started steps, downloads, and local-data and responsible-use notes, written in plain language for users. It links the user guide but no technical documentation.
- Resolve download links from the latest published GitHub Release at build time, using asset patterns that mirror the release workflow's asset check. The page refreshes the links in the browser when a newer release exists and recommends a package for the detected OS and architecture. Without JavaScript or release data, the links fall back to the release page.
- Deploy the site to GitHub Pages from a new workflow: on pushes to `main` that touch the site's inputs, when a release is published, and on manual dispatch. Pull requests build and check the site without deploying it. The site is served at the root of a custom domain, with DNS on Cloudflare.
- Keep the website out of Go tooling by naming its directory `_website/` (Go skips directories starting with `_`), and add opt-in `website:*` Task entries that `task verify` does not run.
- Document the website in `_website/README.md` and in the building guide, and register its ownership and checks in `AGENTS.md`. The repository READMEs are unchanged.

## Capabilities

### New Capabilities

- `project-website`: Public homepage content, localized routes, download resolution and platform recommendation, fallbacks, presentation, and deployment.

### Modified Capabilities

- `build-and-release`: Narrow the toolchain and documentation requirements. Compiling, testing, and packaging the application still need no JavaScript toolchain, and Node.js is limited to the optional, isolated website.

## Impact

**What changes**

- New `_website/` directory and `.github/workflows/website.yml`.
- Opt-in Taskfile entries.
- Documentation: `docs/technical/building.md` and `AGENTS.md`.

**Dependencies**

- npm packages pinned by `_website/package-lock.json`: Astro, `@astrojs/check`, TypeScript, Vitest, and `lucide-static`.
- Node.js 24 is required only to build the site.

**Unaffected**

- Application services, persistence, traffic protocols and models, Python Workers, process attribution, events and shortcuts, windows, application locales, packaging, the release workflow, and update signing.
- Windows, macOS, and Linux application behavior.
- `task verify` and CI stay Node-free.

**Scope and non-goals**

- Content is limited to one landing page per locale. The user guide stays on GitHub; it is linked, not ported.
- Out of scope:
  - Analytics, cookies, server-side code, and Cloudflare features beyond DNS.
  - Changes to release asset names or to the release workflow.
  - Linking or translating technical and developer documentation.

**Completion criteria**

- Type checks, unit tests, and the production build pass.
- The built pages for both locales contain links that resolve to real assets of the latest release.
- Fallback and required-release build modes behave as specified.
- Go tooling excludes `_website/` after `npm ci`, and `go.mod` and `go.sum` are unchanged.
- The workflow is reviewed or linted.
- `openspec validate add-project-website --strict --no-interactive` passes.
- Live deployment needs one-time maintainer setup: set Pages to GitHub Actions, set the custom domain, and add the Cloudflare DNS record. It is confirmed only after the first deploy run.
