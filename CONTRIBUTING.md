# Contributing to FlowLens

Thank you for helping improve FlowLens. This guide covers the contribution workflow. For architectural boundaries, high-risk contracts, and package-level verification rules, read [AGENTS.md](AGENTS.md) before making code changes.

## Before You Start

- Search existing issues and pull requests to avoid duplicate work.
- Open a focused issue first before changing persisted data formats, public models, capture semantics, security boundaries, or core user-facing workflows.
- Report security vulnerabilities through [SECURITY.md](SECURITY.md), never in a public issue.
- Never attach unredacted captures, HAR files, databases, certificates, private keys, API Collections, logs, or screenshots containing sensitive data.

## Development Setup

You need Go 1.27.3 or newer and, preferably, [Task](https://taskfile.dev). Ordinary builds and tests run with `CGO_ENABLED=0` and require no C/Objective-C compiler or JavaScript toolchain. macOS requires version 13.0 or later, and macOS signing and DMG packaging require macOS platform tools. Python 3 is required only for testing Python plugins.

```shell
task dev
```

Without Task, run `go tool mygo dev` with `CGO_ENABLED=0`. The Taskfile sets `CGO_ENABLED=0` automatically on Windows, Linux, and macOS. See [Building and Packaging](docs/technical/building.md) for macOS linker flags, packaging, and release details.

### Common Commands

| Command | Purpose |
| --- | --- |
| `task dev` | Run FlowLens with live reload |
| `task run` | Build with MyGo and launch once |
| `task package` | Build release packages for the current platform |
| `task icons:generate` | Regenerate application and tray icons from SVG sources |
| `task test` | Run all Go tests |
| `task test:race` | Run concurrency-heavy packages (or packages passed after `--`) with the race detector |
| `task verify` | Run formatting, Go lint, UI lifecycle, message catalog, and test checks |
| `task lint:go` / `task lint:go:fix` | Check Go lint and formatting, or apply safe fixes |
| `task vet:ui` | Verify native MyGo UI lifecycle rules |
| `task i18n:generate` / `task i18n:check` | Regenerate or validate the message catalogs |

## Making Changes

- Keep changes within their owning package, as defined in [AGENTS.md](AGENTS.md#module-ownership).
- Add new user-visible text to both `locales/en.json` and `locales/zh-Hans.json` with matching keys and placeholders, then run `task i18n:generate`.
- Update the [User Guide](docs/user-guide.md), relevant [technical docs](docs/technical/), and `openspec/specs/` whenever you change documented behavior.
- Write commit messages as English [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <summary>`, with one class of change per commit and concrete details describing the change or fix.

## Validation

Run the smallest relevant package tests while developing; [AGENTS.md](AGENTS.md#verification) maps each area to its test packages. Before requesting review, run the full check:

```shell
task verify
```

- **Python Worker changes:** Run the Python 3 integration tests with a real Python 3 interpreter instead of skipping them.
- **Concurrency-sensitive changes:** Run `task test:race` with a supported C compiler installed (`task test:race` sets `CGO_ENABLED=1` on all desktop platforms):

```shell
task test:race                                       # desktop, process attribution, and proxy service
task test:race -- ./internal/services/proxy_service  # only the packages you changed
```

## Pull Requests

Keep pull requests focused and clearly describe:

- **Problem:** The user-visible or architectural issue being solved.
- **Design:** The chosen behavior and important non-goals.
- **Impact:** Effects on persisted data, privacy, backward compatibility, and each supported platform.
- **Verification:** The automated tests and manual checks performed.
- **Screenshots:** Include only when helpful for review, with all sensitive data removed.

Exclude unrelated formatting, unaffected generated files, local agent settings, credentials, captures, databases, certificates, logs, and build artifacts.
