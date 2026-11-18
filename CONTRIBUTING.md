# Contributing to FlowLens

Thank you for helping improve FlowLens. This guide covers the contribution path. The engineering rules (module ownership, high-risk contracts, and which tests each area needs) are in [AGENTS.md](AGENTS.md); read it before changing code.

## Before You Start

- Search existing issues and pull requests before opening a duplicate.
- Open a focused issue first for changes to persisted data, public models, capture semantics, security boundaries, or user-facing workflows.
- Report security vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.
- Never attach unredacted captures, HAR files, databases, certificates, private keys, API Collections, logs, or screenshots containing sensitive data.

## Development Setup

You need Go 1.27.3 or newer and, preferably, [Task](https://taskfile.dev). macOS also needs the Xcode Command Line Tools. Python 3 is needed only to test Python plugins. There is no JavaScript toolchain.

```shell
task dev
```

Without Task, run `go tool mygo dev`. The Taskfile builds Windows and Linux with `CGO_ENABLED=0` and macOS with `CGO_ENABLED=1`.

| Command | Purpose |
| --- | --- |
| `task dev` | Run with live reload |
| `task run` | Build with MyGo and launch once |
| `task package` | Build release packages for the current platform |
| `task test` | Run all Go tests |
| `task test:race` | Run the concurrency-heavy packages, or those passed after `--`, with the race detector |
| `task verify` | Run formatting, lint, UI lifecycle, catalog, and test checks |
| `task lint:go` / `task lint:go:fix` | Check Go lint and formatting, or apply safe fixes |
| `task vet:ui` | Check native UI lifecycle rules |
| `task i18n:generate` / `task i18n:check` | Regenerate or check the message catalogs |

See [Building and Packaging](docs/technical/building.md) for cgo, packaging, and release details.

## Making Changes

- Keep each change inside its owning package, as listed in [AGENTS.md](AGENTS.md#module-ownership).
- Put new user-visible text in both `locales/en.json` and `locales/zh-Hans.json`, then run `task i18n:generate`.
- Update the user guide, technical docs, and `openspec/specs/` when you change the behavior they describe.
- Write commits as English [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <summary>`, one kind of change per commit.

## Validation

Run the smallest relevant package tests while developing; [AGENTS.md](AGENTS.md#verification) maps each area to its packages. Before requesting review, run:

```shell
task verify
```

Changes to Python Workers must run the integration tests with a real Python 3 interpreter rather than skipping them. Concurrency-sensitive changes should also run the race detector, which needs cgo and a C compiler:

```shell
task test:race                                       # desktop, process attribution, proxy service
task test:race -- ./internal/services/proxy_service  # only the packages you changed
```

## Pull Requests

Keep a pull request focused and describe:

- the user-visible or architectural problem;
- the chosen behavior and important non-goals;
- effects on persisted data, privacy, compatibility, and each platform;
- the tests and manual checks performed;
- screenshots only when they help review, with sensitive data removed.

Do not include unrelated formatting, generated files that the change did not affect, local agent settings, credentials, captures, databases, certificates, logs, or build output.
