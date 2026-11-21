# Tasks

## 1. Packaging and release consistency

- [x] 1.1 Complete mygo.json metadata/resources/native Linux settings, synchronize README/CHANGELOG/building documentation and release guidance/checks, and verify configuration/catalog loading with `go tool mygo i18n check` and review of the in-scope diff.
- [x] 1.2 With `CGO_ENABLED=0` and `MYGO_LDFLAGS=-macos=13.0`, run `go tool mygo build -platform windows/amd64,linux/amd64,linux/arm64 -skip-installer -o .mygo/package-metadata-check`; inspect package dependencies, metadata, license resources, generated formats, and Windows file properties, and validate the workflow and release checker's positive/missing-artifact/missing-checksum cases.

## 2. Acceptance

- [x] 2.1 Run `openspec validate complete-package-metadata --strict --no-interactive` and `git diff --check`, compare implementation against every changed scenario, and record verification evidence and platform limits.

## Workflow follow-up

- Synchronize build-and-release deltas, archive the completed change, and run `openspec validate --specs --strict --no-interactive`.
