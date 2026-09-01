import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import { NuxtIconBundle } from '@nuxt/icon/vite'
import wails from '@wailsio/runtime/plugins/vite'
import monacoEditorEsmPlugin from 'vite-plugin-monaco-editor-esm'

const frontendRoot = fileURLToPath(new URL('.', import.meta.url))

const nuxtUiDefaultIcons = [
  'lucide:arrow-down',
  'lucide:arrow-left',
  'lucide:arrow-right',
  'lucide:arrow-up',
  'lucide:circle-alert',
  'lucide:check',
  'lucide:chevrons-left',
  'lucide:chevrons-right',
  'lucide:chevron-down',
  'lucide:chevron-left',
  'lucide:chevron-right',
  'lucide:chevron-up',
  'lucide:x',
  'lucide:copy',
  'lucide:copy-check',
  'lucide:moon',
  'lucide:grip-vertical',
  'lucide:ellipsis',
  'lucide:circle-x',
  'lucide:arrow-up-right',
  'lucide:eye',
  'lucide:eye-off',
  'lucide:file',
  'lucide:folder',
  'lucide:folder-open',
  'lucide:hash',
  'lucide:info',
  'lucide:sun',
  'lucide:loader-circle',
  'lucide:menu',
  'lucide:minus',
  'lucide:panel-left-close',
  'lucide:panel-left-open',
  'lucide:plus',
  'lucide:rotate-ccw',
  'lucide:search',
  'lucide:square',
  'lucide:circle-check',
  'lucide:monitor',
  'lucide:lightbulb',
  'lucide:upload',
  'lucide:triangle-alert',
]

const nuxtUiFormComponents = new Set([
  'Checkbox',
  'Input',
  'InputNumber',
  'Listbox',
  'Select',
  'SelectMenu',
  'Switch',
  'Textarea',
])

const nuxtUiOverlayComponents = new Set([
  'App',
  'ContextMenu',
  'DropdownMenu',
  'Modal',
  'Popover',
  'Tooltip',
])

const nuxtUiDisplayComponents = new Set([
  'Alert',
  'Badge',
  'Button',
  'Empty',
  'Tabs',
  'Tree',
])

function isVueUseInvalidAnnotationLog(log: {
  code?: unknown
  id?: unknown
  loc?: { file?: unknown }
  message?: unknown
}) {
  if (log.code !== 'INVALID_ANNOTATION') {
    return false
  }

  return [log.id, log.loc?.file, log.message].some(
    (value) => typeof value === 'string' && value.includes('@vueuse/core'),
  )
}

function isNuxtUiComponent(id: string, components: Set<string>) {
  for (const component of components) {
    if (id.includes(`/@nuxt/ui/dist/runtime/components/${component}.`)) {
      return true
    }
  }

  return false
}

function manualChunks(id: string) {
  if (id.includes('virtual:nuxt-ui-templates/')) {
    return 'vendor-nuxt-ui-theme'
  }

  if (!id.includes('/node_modules/')) {
    return
  }

  if (id.includes('/monaco-editor/') || id.includes('/@guolao/vue-monaco-editor/')) {
    return 'vendor-monaco'
  }

  if (id.includes('/@vueuse/core/') || id.includes('/@vueuse/shared/')) {
    return 'vendor-vueuse'
  }

  if (id.includes('/tailwind-merge/') || id.includes('/tailwind-variants/')) {
    return 'vendor-tailwind-utils'
  }

  if (id.includes('/uplot/')) {
    return 'vendor-uplot'
  }

  if (id.includes('/@iconify/') || id.includes('/@iconify-json/')) {
    return 'vendor-icons'
  }

  if (id.includes('/reka-ui/') || id.includes('/motion-v/') || id.includes('/vaul-vue/')) {
    return 'vendor-ui-primitives'
  }

  if (id.includes('/@nuxt/ui/dist/shared/')) {
    return 'vendor-nuxt-ui-shared'
  }

  if (isNuxtUiComponent(id, nuxtUiFormComponents)) {
    return 'vendor-nuxt-ui-form'
  }

  if (isNuxtUiComponent(id, nuxtUiOverlayComponents)) {
    return 'vendor-nuxt-ui-overlay'
  }

  if (isNuxtUiComponent(id, nuxtUiDisplayComponents)) {
    return 'vendor-nuxt-ui-display'
  }

  if (id.includes('/@nuxt/ui/dist/runtime/components/')) {
    return 'vendor-nuxt-ui-other'
  }

  if (
    id.includes('/@nuxt/ui/dist/runtime/composables/') ||
    id.includes('/@nuxt/ui/dist/runtime/utils/') ||
    id.includes('/@nuxt/ui/dist/runtime/vue/')
  ) {
    return 'vendor-nuxt-ui-runtime'
  }

  if (id.includes('/@nuxt/ui/')) {
    return 'vendor-nuxt-ui-core'
  }

  if (
    id.includes('/vue/') ||
    id.includes('/@vue/') ||
    id.includes('/vue-router/') ||
    id.includes('/pinia/') ||
    id.includes('/vue-i18n/')
  ) {
    return 'vendor-vue'
  }
}

// https://vite.dev/config/
export default defineConfig(() => {
  return {
    plugins: [
      vue(),
      NuxtIconBundle({
        icons: nuxtUiDefaultIcons,
        scan: {
          globInclude: ['src/**/*.{vue,ts,tsx,js,jsx}'],
        },
        cwd: frontendRoot,
      }),
      ui({
        colorMode: false,
        ui: {
          colors: {
            primary: 'blue',
          },
        },
      }),
      // Monaco 0.55 requires concrete worker files; plugin defaults still use extensionless paths.
      monacoEditorEsmPlugin({
        languageWorkers: [],
        customWorkers: [
          {
            label: 'editorWorkerService',
            entry: 'monaco-editor/editor/editor.worker.js',
          },
        ],
      }),
      wails('./bindings'),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
        '#bindings': fileURLToPath(new URL('./bindings', import.meta.url)),
      },
    },
    server: {
      watch: {
        ignored: ['**/bindings/**', '**/.bindings-tmp-*/**'],
      },
    },
    build: {
      rolldownOptions: {
        onLog(level, log, defaultHandler) {
          if (level === 'warn' && isVueUseInvalidAnnotationLog(log)) {
            return
          }

          defaultHandler(level, log)
        },
        output: {
          manualChunks,
        },
      },
    },
  }
})
