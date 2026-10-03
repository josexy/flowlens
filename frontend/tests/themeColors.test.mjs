import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as pinia from 'pinia'

function evaluate(source, require, globals = {}) {
  const exports = {}
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  runInNewContext(code, { exports, require, ...globals })
  return exports
}

const readSource = (path) =>
  readFileSync(new URL(`../${path}`, import.meta.url), 'utf8').replace(/^\uFEFF/, '')
const palette = evaluate(readSource('src/utils/themeColors.ts'), () => {
  throw new Error('unexpected import')
})
const eventName = 'app:theme-colors-changed'

function state(revision = 1, extra = {}) {
  return {
    primaryColor: 'blue',
    neutralColor: 'slate',
    preview: false,
    revision,
    sessionID: 1,
    sequence: 0,
    ...extra,
  }
}

function backend(initial = state()) {
  const listeners = new Set()
  return {
    current: initial,
    requests: [],
    listeners,
    async get() {
      return structuredClone(this.current)
    },
    async preview(request) {
      this.requests.push(request)
      const next = state(this.current.revision + 1, {
        ...request,
        preview: true,
      })
      this.emit(next)
      return next
    },
    emit(next) {
      this.current = next
      for (const listener of listeners) listener({ data: next })
    },
  }
}

function windowStore(t, service, environment = {}) {
  const ui = vue.reactive({ ui: { colors: { primary: 'blue', neutral: 'slate' } } })
  const nativeWrites = []
  const mediaListeners = new Set()
  const media = {
    matches: false,
    addEventListener: (_, handler) => mediaListeners.add(handler),
    removeEventListener: (_, handler) => mediaListeners.delete(handler),
  }
  const exports = evaluate(
    readSource('src/stores/theme.ts'),
    (id) => {
      if (id === 'vue') return vue
      if (id === 'pinia') return pinia
      if (id === '@wailsio/runtime')
        return {
          Events: {
            On(name, handler) {
              assert.equal(name, eventName)
              service.listeners.add(handler)
              return () => service.listeners.delete(handler)
            },
          },
          Window: {
            SetBackgroundColour: async (...rgba) => {
              nativeWrites.push(rgba)
            },
          },
        }
      if (id.includes('useAppConfig')) return { useAppConfig: () => ui }
      if (id.endsWith('/runtime/appEvents')) return { THEME_COLORS_CHANGED_EVENT: eventName }
      if (id.endsWith('/utils/themeColors')) return palette
      if (id.endsWith('/settingservice'))
        return {
          GetThemeColorState: () => service.get(),
          PreviewThemeColors: (request) => service.preview(request),
        }
      throw new Error(`Unexpected import: ${id}`)
    },
    { window: { matchMedia: () => media }, ...environment },
  )
  const root = pinia.createPinia()
  pinia.setActivePinia(root)
  const store = exports.useThemeStore()
  t.after(() => {
    store.cleanup()
    store.$dispose()
  })
  return { store, ui, root, nativeWrites, mediaListeners }
}

test('all supported palettes have bilingual names; invalid state cannot alter appearance', (t) => {
  assert.equal(palette.THEME_PRIMARY_COLORS.length, 17)
  assert.equal(palette.THEME_NEUTRAL_COLORS.length, 9)
  const en = JSON.parse(readSource('src/locales/en.json')).settings.theme_colors
  const zh = JSON.parse(readSource('src/locales/zh.json')).settings.theme_colors
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort())
  assert.deepEqual(Object.keys(en.palette).sort(), Object.keys(zh.palette).sort())
  for (const color of [...palette.THEME_PRIMARY_COLORS, ...palette.THEME_NEUTRAL_COLORS]) {
    assert.ok(en.palette[color] && zh.palette[color])
  }
  const { store } = windowStore(t, backend())
  for (const invalid of [
    state(1, { primaryColor: '#ff00ff' }),
    state(1, { neutralColor: 'red' }),
    state(-1),
    state(1, { sequence: 0.5 }),
    state(1, { preview: 'true' }),
    null,
  ])
    store.applyThemeColorState(invalid)
  assert.equal(store.primaryColor, 'blue')
  assert.equal(store.neutralColor, 'slate')
})

test('shared palettes apply across windows and light, dark, and system modes', async (t) => {
  const service = backend()
  const main = windowStore(t, service)
  const settings = windowStore(t, service)
  main.store.initializeTheme('auto')
  settings.store.initializeTheme('light')
  await Promise.all([main.store.loadThemeColors(), settings.store.loadThemeColors()])
  await settings.store.previewThemeColors('violet', 'mauve')
  for (const app of [main, settings]) {
    assert.equal(app.ui.ui.colors.primary, 'violet')
    assert.equal(app.ui.ui.colors.neutral, 'mauve')
  }
  settings.store.setThemeMode('dark')
  for (const listener of main.mediaListeners) listener({ matches: true })
  assert.equal(main.store.isDark, true)
  assert.equal(settings.store.isDark, true)
  assert.equal(main.store.primaryColor, 'violet')
  assert.equal(settings.store.neutralColor, 'mauve')
  assert.equal(service.requests[0].sessionID, 1)
  assert.equal(service.requests[0].sequence, 1)
  // One window's cleanup must leave the other window's listener intact.
  settings.store.cleanup()
  assert.equal(service.listeners.size, 1)
  service.emit(state(3, { sessionID: 0 }))
  assert.equal(main.store.primaryColor, 'blue')
  assert.equal(settings.store.primaryColor, 'violet')
})

test('late hydration and out-of-order events cannot replace a newer preview', async (t) => {
  const service = backend()
  let hydrate
  service.get = () =>
    new Promise((resolve) => {
      hydrate = resolve
    })
  const { store, ui } = windowStore(t, service)
  const loading = store.loadThemeColors()
  service.emit(
    state(5, { primaryColor: 'rose', neutralColor: 'olive', preview: true, sequence: 4 }),
  )
  hydrate(state(2))
  await loading
  service.emit(state(4, { primaryColor: 'green' }))
  assert.equal(ui.ui.colors.primary, 'rose')
  assert.equal(ui.ui.colors.neutral, 'olive')
  await store.previewThemeColors('cyan', 'mist')
  assert.equal(service.requests[0].sequence, 5)
})

test('new windows join the effective preview; save and subsequent discard use the saved baseline', async (t) => {
  const service = backend(
    state(8, { primaryColor: 'teal', neutralColor: 'taupe', preview: true, sequence: 3 }),
  )
  const app = windowStore(t, service)
  await app.store.loadThemeColors()
  assert.equal(app.store.primaryColor, 'teal')
  service.emit(state(9, { primaryColor: 'teal', neutralColor: 'taupe', sequence: 3 }))
  await app.store.previewThemeColors('pink', 'zinc')
  assert.equal(service.requests[0].sequence, 4)
  service.emit(state(11, { primaryColor: 'teal', neutralColor: 'taupe', sessionID: 0 }))
  assert.equal(app.store.primaryColor, 'teal')
  assert.equal(app.store.neutralColor, 'taupe')
})

test('save flush waits for pending preview requests, including failed requests', async (t) => {
  const service = backend()
  const app = windowStore(t, service)
  await app.store.loadThemeColors()
  let resolvePreview
  service.preview = () =>
    new Promise((resolve) => {
      resolvePreview = resolve
    })
  const pending = app.store.previewThemeColors('red', 'gray')
  let flushed = false
  const flushing = app.store.flushThemeColorPreview().then(() => {
    flushed = true
  })
  await Promise.resolve()
  assert.equal(flushed, false)
  resolvePreview(
    state(2, { primaryColor: 'red', neutralColor: 'gray', sequence: 1, preview: true }),
  )
  await Promise.all([pending, flushing])
  assert.equal(flushed, true)
  service.preview = async () => {
    throw new Error('unavailable')
  }
  await assert.rejects(app.store.previewThemeColors('green', 'stone'), /unavailable/)
  await app.store.flushThemeColorPreview()
  assert.equal(app.store.primaryColor, 'red')
})

test('native background and CSS consumers refresh after the Nuxt UI stylesheet patch', async (t) => {
  const frames = new Map()
  let frameID = 0
  let onMutation
  let cssColor = 'oklch(0.208 0.042 265.755)'
  let fillStyle
  const context = {
    set fillStyle(value) {
      fillStyle = value
    },
    fillRect() {},
    getImageData: () => ({ data: [15, 23, 42, 255] }),
  }
  class Element {
    style = { removeProperty() {} }
    classList = { toggle() {} }
    closest() {
      return this
    }
  }
  const body = new Element()
  const app = windowStore(t, backend(), {
    Element,
    document: {
      body,
      head: new Element(),
      documentElement: new Element(),
      getElementById: () => new Element(),
      createElement: () => ({ getContext: () => context }),
    },
    MutationObserver: class {
      constructor(callback) {
        onMutation = callback
      }
      observe() {}
      disconnect() {}
    },
    getComputedStyle: () => ({ backgroundColor: cssColor }),
    requestAnimationFrame: (callback) => {
      frames.set(++frameID, callback)
      return frameID
    },
    cancelAnimationFrame: (id) => frames.delete(id),
  })
  const render = async () => {
    const current = [...frames.values()]
    frames.clear()
    for (const callback of current) callback()
    await new Promise((resolve) => setImmediate(resolve))
  }
  await app.store.loadThemeColors()
  await render()
  assert.equal(fillStyle, cssColor)
  assert.deepEqual(app.nativeWrites.at(-1), [15, 23, 42, 255])
  const revision = app.store.appearanceRevision
  cssColor = 'oklch(0.21 0.006 285.885)'
  onMutation([{ target: new Element(), addedNodes: [] }])
  await render()
  assert.equal(fillStyle, cssColor)
  assert.ok(app.store.appearanceRevision > revision)
})

function settingsStore(t, service, globals = {}) {
  const cache = new Map()
  const load = (path) => {
    if (cache.has(path)) return cache.get(path)
    const exports = evaluate(
      readSource(path),
      (id) => {
        if (id === 'vue') return vue
        if (id === 'pinia') return pinia
        if (id === '@/utils/themeColors') return palette
        if (id.startsWith('@/')) return load(`src/${id.slice(2)}.ts`)
        if (id.endsWith('/setting_service/models'))
          return load(
            'bindings/github.com/josexy/flowlens/backend/services/setting_service/models.ts',
          )
        if (id.endsWith('/settingservice')) return service
        if (id.endsWith('/shortcutservice'))
          return { GetShortcutRuntimeState: async () => ({ commands: {}, warnings: [] }) }
        if (id.endsWith('/pythonpluginservice')) return { GetRuntimeStatus: async () => null }
        if (id.endsWith('/proxyservice')) return {}
        throw new Error(`Unexpected settings import: ${id}`)
      },
      globals,
    )
    cache.set(path, exports)
    return exports
  }
  pinia.setActivePinia(pinia.createPinia())
  const store = load('src/stores/setting.ts').useSettingStore()
  t.after(() => store.$dispose())
  return store
}

test('font sizes preview, save, sync across windows, reload, and reset independently', async (t) => {
  let persisted = { commonConfig: { themeMode: 'dark', language: 'en' } }
  let staged
  const bridge = {
    Get: async () => structuredClone(persisted),
    GetActiveWindowFrameMode: async () => 'custom',
    UpdatePreservingShortcuts: async (value) => {
      staged = JSON.parse(JSON.stringify(value))
    },
    Save: async () => {
      persisted = staged
    },
  }
  const styles = new Map()
  const store = settingsStore(t, bridge, {
    document: {
      documentElement: {
        style: { setProperty: (name, value) => styles.set(name, value) },
        toggleAttribute() {},
      },
    },
  })
  await store.load()
  assert.equal(store.settings.commonConfig.appFontSize, 16)
  assert.equal(store.resolvedCodeFontSize, 13)
  assert.equal(styles.get('--app-font-size'), '16px')

  store.settings.commonConfig.appFontSize = 20
  store.settings.commonConfig.codeFontSize = 18
  store.previewAppearance()
  assert.equal(styles.get('--app-font-size'), '20px')
  assert.equal(store.resolvedCodeFontSize, 18)
  assert.equal(persisted.commonConfig.appFontSize, undefined, 'preview does not save')
  await store.save({ dirtySections: ['common'] })
  assert.equal(persisted.commonConfig.appFontSize, 20)
  assert.equal(persisted.commonConfig.codeFontSize, 18)

  const mainWindow = settingsStore(t, bridge)
  await mainWindow.load()
  assert.equal(mainWindow.settings.commonConfig.appFontSize, 20)
  assert.equal(mainWindow.resolvedCodeFontSize, 18)
  mainWindow.syncExternalSettings({
    ...structuredClone(persisted),
    commonConfig: {
      ...persisted.commonConfig,
      appFontSize: 24,
      codeFontSize: 32,
    },
  })
  assert.equal(mainWindow.settings.commonConfig.appFontSize, 24)
  assert.equal(mainWindow.resolvedCodeFontSize, 32)
  store.resetToDefaults()
  store.previewAppearance()
  assert.equal(styles.get('--app-font-size'), '16px')
  assert.equal(store.resolvedCodeFontSize, 13)
})

test('invalid font sizes cannot reach CSS or editor options', async (t) => {
  for (const value of [undefined, null, NaN, Infinity, -1, 0, 9, 33, 16.5]) {
    const store = settingsStore(t, {
      Get: async () => ({ commonConfig: { appFontSize: value, codeFontSize: value } }),
      GetActiveWindowFrameMode: async () => 'custom',
    })
    await store.load()
    assert.equal(store.settings.commonConfig.appFontSize, 16)
    assert.equal(store.resolvedCodeFontSize, 13)
  }
})

test('ordinary settings saves clone colors while retaining the latest mode and language', async (t) => {
  let persisted = { commonConfig: { themeMode: 'light', language: 'zh' } }
  let staged
  const bridge = {
    Get: async () => structuredClone(persisted),
    GetActiveWindowFrameMode: async () => 'custom',
    UpdatePreservingShortcuts: async (value) => {
      staged = JSON.parse(JSON.stringify(value))
    },
    Save: async () => {
      persisted = staged
    },
  }
  const store = settingsStore(t, bridge)
  await store.load()
  assert.equal(store.settings.commonConfig.themePrimaryColor, 'blue')
  assert.equal(store.settings.commonConfig.themeNeutralColor, 'slate')
  store.settings.commonConfig.themePrimaryColor = 'violet'
  store.settings.commonConfig.themeNeutralColor = 'mauve'
  persisted.commonConfig.themeMode = 'dark'
  persisted.commonConfig.language = 'en'
  await store.save({ dirtySections: ['common'] })
  assert.equal(persisted.commonConfig.themePrimaryColor, 'violet')
  assert.equal(persisted.commonConfig.themeNeutralColor, 'mauve')
  assert.equal(persisted.commonConfig.themeMode, 'dark')
  assert.equal(persisted.commonConfig.language, 'en')
  bridge.Save = async () => {
    throw new Error('disk full')
  }
  store.settings.commonConfig.themePrimaryColor = 'pink'
  await assert.rejects(store.save({ dirtySections: ['common'] }), /disk full/)
  assert.equal(store.isDirty, true)
  assert.equal(store.settings.commonConfig.themePrimaryColor, 'pink')
  store.resetToDefaults()
  assert.equal(store.settings.commonConfig.themePrimaryColor, 'blue')
  assert.equal(store.settings.commonConfig.themeNeutralColor, 'slate')
})

test('settings remain busy through preview flushing and saving; duplicate saves are ignored', async () => {
  const { descriptor } = parse(readSource('src/views/SettingsView.vue'))
  const source = ts.createSourceFile(
    'SettingsView.ts',
    descriptor.scriptSetup.content,
    ts.ScriptTarget.ES2022,
  )
  const save = source.statements.find(
    (node) => ts.isFunctionDeclaration(node) && node.name?.text === 'handleSave',
  )
  const declaration = source.statements
    .filter(ts.isVariableStatement)
    .flatMap((node) => [...node.declarationList.declarations])
    .find((node) => node.name.getText(source) === 'isCommittingSettings')
  let finishPreview, finishSave
  const preview = new Promise((resolve) => {
    finishPreview = resolve
  })
  const saved = new Promise((resolve) => {
    finishSave = resolve
  })
  let previewCalls = 0,
    saveCalls = 0
  const setting = {
    isSaving: false,
    isDirty: true,
    async save() {
      saveCalls++
      return saved
    },
  }
  const result = evaluate(
    `const ${declaration.getText(source)};\n${save.getText(source)}\nexport { handleSave, isCommittingSettings };`,
    () => {
      throw new Error('unexpected import')
    },
    {
      ref: vue.ref,
      themeStore: {
        flushThemeColorPreview: () => {
          previewCalls++
          return preview
        },
      },
      settingStore: setting,
      dirtySections: new Set(['common']),
      nextTick: vue.nextTick,
      activeTab: vue.ref('general'),
      showSaveStatusNotice: () => {},
      emitSettingsSaved: () => {},
      notify: { error: assert.fail },
    },
  )
  const first = result.handleSave()
  assert.equal(result.isCommittingSettings.value, true)
  await result.handleSave()
  assert.equal(previewCalls, 1)
  assert.equal(saveCalls, 0)
  finishPreview()
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(saveCalls, 1)
  assert.equal(result.isCommittingSettings.value, true)
  await result.handleSave()
  assert.equal(saveCalls, 1)
  finishSave({ complete: true, persistedSettings: {} })
  await first
  assert.equal(result.isCommittingSettings.value, false)
})
