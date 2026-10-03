import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import * as vue from 'vue'
import * as pinia from 'pinia'

const readSource = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8').replace(/^\uFEFF/, '')

function evaluate(source, require, globals = {}) {
  const exports = {}
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  runInNewContext(code, { exports, require, ...globals })
  return exports
}

const dependencies = new Map()
for (const [id, path] of [
  ['#bindings/github.com/josexy/flowlens/backend/services/setting_service/models', 'bindings/github.com/josexy/flowlens/backend/services/setting_service/models.ts'],
  ['@/utils/themeColors', 'src/utils/themeColors.ts'],
  ['@/utils/traffic-table-columns', 'src/utils/traffic-table-columns.ts'],
  ['@/shortcuts/catalog', 'src/shortcuts/catalog.ts'],
]) {
  dependencies.set(id, evaluate(readSource(path), (name) => {
    throw new Error(`Unexpected dependency: ${name}`)
  }))
}

function setup(t, layout = 'vertical') {
  const service = {
    settings: { trafficDetailConfig: { layout } },
    calls: [],
    async saveDetail(config) {
      this.calls.push(config)
      this.settings.trafficDetailConfig = structuredClone(config)
    },
  }
  const require = (id) => {
    if (id === 'vue') return vue
    if (id === 'pinia') return pinia
    if (dependencies.has(id)) return dependencies.get(id)
    if (id.endsWith('/settingservice')) return {
      Get: async () => structuredClone(service.settings),
      GetActiveWindowFrameMode: async () => 'custom',
      SaveTrafficDetailConfig: (config) => service.saveDetail(config),
      UpdatePreservingShortcuts: async (settings) => { service.settings = JSON.parse(JSON.stringify(settings)) },
      Save: async () => {},
    }
    if (id.endsWith('/shortcutservice')) return {
      GetShortcutRuntimeState: async () => ({ commands: {}, warnings: [] }),
    }
    if (id.endsWith('/pythonpluginservice')) return { GetRuntimeStatus: async () => null }
    if (id.endsWith('/proxyservice')) return {}
    throw new Error(`Unexpected import: ${id}`)
  }
  const { useSettingStore } = evaluate(readSource('src/stores/setting.ts'), require)
  pinia.setActivePinia(pinia.createPinia())
  const store = useSettingStore()
  store.syncExternalSettings(structuredClone(service.settings))
  t.after(() => store.$dispose())
  const { useTrafficDetailSplit } = evaluate(readSource('src/composables/useTrafficDetailSplit.ts'), (id) => {
    if (id === '@/stores/setting') return { useSettingStore }
    return require(id)
  })
  return { service, store, useTrafficDetailSplit }
}

test('layout toggles immediately and reloads the saved preference', async (t) => {
  const { store, service } = setup(t)
  const saving = store.toggleTrafficDetailLayout()
  assert.equal(store.trafficDetailLayout, 'horizontal')
  assert.equal(store.isSavingTrafficDetailConfig, true)
  assert.equal(await saving, true)
  assert.equal(store.isSavingTrafficDetailConfig, false)
  assert.equal(service.calls.length, 1)
  assert.equal(service.calls[0].layout, 'horizontal')
  await store.load()
  assert.equal(store.trafficDetailLayout, 'horizontal')
  await store.toggleTrafficDetailLayout()
  assert.equal(store.trafficDetailLayout, 'vertical')
  assert.equal(store.isDirty, false)
})

test('a failed save restores the layout and preserves unrelated settings edits', async (t) => {
  const { store, service } = setup(t, 'horizontal')
  store.settings.commonConfig.language = 'en'
  store.markDirty()
  const failure = new Error('database unavailable')
  service.saveDetail = async () => { throw failure }
  await assert.rejects(store.toggleTrafficDetailLayout(), failure)
  assert.equal(store.trafficDetailLayout, 'horizontal')
  assert.equal(store.settings.commonConfig.language, 'en')
  assert.equal(store.isDirty, true)
  assert.equal(store.isSavingTrafficDetailConfig, false)
})

test('repeated clicks do not start overlapping saves', async (t) => {
  const { store, service } = setup(t)
  let finish
  service.saveDetail = (config) => {
    service.calls.push(config)
    return new Promise((resolve) => { finish = resolve })
  }
  const saving = store.toggleTrafficDetailLayout()
  assert.equal(await store.toggleTrafficDetailLayout(), false)
  assert.equal(service.calls.length, 1)
  assert.equal(store.trafficDetailLayout, 'horizontal')
  finish()
  await saving
  assert.equal(store.isSavingTrafficDetailConfig, false)
})

test('missing and invalid preferences use the existing vertical layout', (t) => {
  const { store } = setup(t, 'diagonal')
  assert.equal(store.trafficDetailLayout, 'vertical')
  store.syncExternalSettings({})
  assert.equal(store.trafficDetailLayout, 'vertical')
})

test('ordinary settings saves carry the latest layout instead of the stale draft', async (t) => {
  const { store, service } = setup(t)
  service.settings.trafficDetailConfig.layout = 'horizontal'
  store.settings.commonConfig.language = 'en'
  const result = await store.save({ dirtySections: ['common'] })
  assert.equal(store.trafficDetailLayout, 'horizontal')
  assert.equal(result.persistedSettings.trafficDetailConfig.layout, 'horizontal')
})

test('split ratios are remembered per mode without recreating the panel', async (t) => {
  const { store, useTrafficDetailSplit } = setup(t)
  const scope = vue.effectScope()
  t.after(() => scope.stop())
  const visible = vue.ref(true)
  const split = scope.run(() => useTrafficDetailSplit(60, visible))
  const resized = []
  const panel = { resize: (size) => resized.push(size) }
  split.firstPanel.value = panel
  split.handleLayout([72, 28])
  await store.toggleTrafficDetailLayout()
  await vue.nextTick()
  assert.equal(split.firstPanelSize.value, 60)
  assert.equal(resized.at(-1), 60)
  split.handleLayout([55, 45])
  await store.toggleTrafficDetailLayout()
  await vue.nextTick()
  assert.equal(split.firstPanelSize.value, 72)
  assert.equal(resized.at(-1), 72)
  assert.equal(vue.toRaw(split.firstPanel.value), panel)
  visible.value = false
  await vue.nextTick()
  split.handleLayout([100])
  visible.value = true
  await vue.nextTick()
  await vue.nextTick()
  assert.equal(resized.at(-1), 72)
})

test('layout copy is available in both languages with matching placeholders', () => {
  const zh = JSON.parse(readSource('src/locales/zh.json')).status
  const en = JSON.parse(readSource('src/locales/en.json')).status
  for (const key of ['switch_horizontal_layout', 'switch_vertical_layout', 'layout_save_failed']) {
    assert.ok(zh[key] && en[key])
    assert.deepEqual(zh[key].match(/\{\w+\}/g), en[key].match(/\{\w+\}/g))
  }
})

const detailScript = parse(readSource('src/components/traffic/DetailPanel.vue')).descriptor.scriptSetup.content
const detailSource = ts.createSourceFile('DetailPanel.ts', detailScript, ts.ScriptTarget.Latest, true)
const wheelHandler = detailSource.statements.find(
  (statement) => ts.isFunctionDeclaration(statement) && statement.name?.text === 'handleDetailTabsWheel',
)
assert.ok(wheelHandler, 'the detail panel must provide wheel navigation for hidden tabs')
const { handleDetailTabsWheel } = evaluate(`export ${wheelHandler.getText(detailSource)}`, () => {
  throw new Error('unexpected import')
}, { getComputedStyle: () => ({ lineHeight: '24px', fontSize: '16px' }) })

function wheelFixture(elementOverrides = {}, eventOverrides = {}) {
  const element = { scrollLeft: 0, scrollWidth: 600, clientWidth: 200, ...elementOverrides }
  const event = {
    currentTarget: { querySelector: () => element },
    ctrlKey: false,
    defaultPrevented: false,
    deltaMode: 0,
    deltaX: 0,
    deltaY: 60,
    preventDefault() { this.defaultPrevented = true },
    ...eventOverrides,
  }
  return { element, event }
}

test('a mouse wheel reveals overflowing tabs in either direction and clamps at the edges', () => {
  const { element, event } = wheelFixture()
  handleDetailTabsWheel(event)
  assert.equal(element.scrollLeft, 60)
  assert.equal(event.defaultPrevented, true)
  event.deltaY = -30
  handleDetailTabsWheel({ ...event, defaultPrevented: false })
  assert.equal(element.scrollLeft, 30)
  handleDetailTabsWheel({ ...event, deltaY: 1000, defaultPrevented: false })
  assert.equal(element.scrollLeft, 400)
  handleDetailTabsWheel({ ...event, deltaY: -1000, defaultPrevented: false })
  assert.equal(element.scrollLeft, 0)
})

test('touchpads keep horizontal and fractional scrolling; wheel line/page units are respected', () => {
  const scenarios = [
    [{ deltaX: 75, deltaY: 5 }, 75],
    [{ deltaX: 0.5, deltaY: 0 }, 0.5],
    [{ deltaY: 2, deltaMode: 1 }, 48],
    [{ deltaY: 1, deltaMode: 2 }, 200],
  ]
  for (const [overrides, expected] of scenarios) {
    const { element, event } = wheelFixture({}, overrides)
    handleDetailTabsWheel(event)
    assert.equal(element.scrollLeft, expected)
    assert.equal(event.defaultPrevented, true)
  }
})

test('wheel events are left alone when tabs fit, an edge is reached, or the user zooms', () => {
  for (const [elementOverrides, eventOverrides] of [
    [{ scrollWidth: 200 }, {}],
    [{ scrollLeft: 400 }, {}],
    [{ scrollLeft: 0 }, { deltaY: -60 }],
    [{}, { ctrlKey: true }],
    [{}, { deltaX: 0, deltaY: 0 }],
    [{}, { currentTarget: null }],
  ]) {
    const { element, event } = wheelFixture(elementOverrides, eventOverrides)
    const previousScrollLeft = element.scrollLeft
    handleDetailTabsWheel(event)
    assert.equal(element.scrollLeft, previousScrollLeft)
    assert.equal(event.defaultPrevented, false)
  }
})
