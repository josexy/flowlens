# FlowLens Frontend

This directory contains the Vue 3 frontend for FlowLens. The full project overview and desktop app workflow live in the root [README](../README.md).

## Stack

- Vue 3 with `<script setup>`
- TypeScript
- Vite
- Pinia
- Nuxt UI v4
- Tailwind CSS v4
- Monaco Editor
- uPlot
- Wails generated TypeScript bindings in `bindings/`

## Commands

Install dependencies:

```shell
npm install
```

Run the Vite dev server directly:

```shell
npm run dev
```

For normal desktop development, prefer running from the repository root:

```shell
task dev
```

Type-check:

```shell
npm run type-check
```

Build:

```shell
npm run build
```

Lint and format:

```shell
npm run lint
npm run lint:fix
npm run lint:tailwind
npm run lint:tailwind -- --all
npm run lint:tailwind -- --fix
npm run format
```

## Notes

- Add UI text to both `src/locales/zh.json` and `src/locales/en.json`.
- Prefer Nuxt UI primitives for base controls and keep Tailwind classes in the canonical form reported by `npm run lint:tailwind`.
- Keep shared state in Pinia stores under `src/stores`.
- Regenerate `bindings/` from the repository root after backend Wails service or model changes.
