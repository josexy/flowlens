import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

const filename = new URL('../src/components/settings/CACertificateInfoPanel.vue', import.meta.url)
const { descriptor } = parse(readFileSync(filename, 'utf8'))
const code = ts.transpileModule(descriptor.scriptSetup.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function panelState(overrides = {}) {
  const props = vue.reactive({
    caInfo: { certExists: true, keyExists: true, validPair: true, isCa: true, error: '' },
    caTrustStatus: { supported: true, installed: false, sha256Fingerprint: 'fingerprint', error: '' },
    caHasExistingFiles: true,
    caTrustLoading: false,
    caTrustLoadFailed: false,
    isChangingCaTrust: false,
    isGenerating: false,
    caSettingsDirty: false,
    certGeneratedSuccess: false,
    ...overrides,
  })
  const exports = {}
  runInNewContext(`${code}\nexports.state = () => ({
    action: trustAction.value,
    actionDisabled: trustActionDisabled.value,
    generationDisabled: generationDisabled.value,
    generationHint: generationHint.value,
    summary: trustSummary.value,
  });`, {
    exports,
    defineProps: () => props,
    defineEmits: () => () => {},
    require: (name) => {
      if (name === 'vue') return vue
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (name === '@/utils/format') return { formatUnixMicrosLocal: String }
      return { default: {} }
    },
  }, { filename: filename.pathname })
  return { props, state: exports.state }
}

test('the current-user action switches between installation and removal', () => {
  const panel = panelState()
  assert.equal(panel.state().action, 'settings.ca_trust_install')
  assert.equal(panel.state().actionDisabled, false)
  panel.props.caTrustStatus.installed = true
  assert.equal(panel.state().action, 'settings.ca_trust_uninstall')
  assert.equal(panel.state().summary, 'settings.ca_trust_installed')
  assert.equal(panel.state().generationDisabled, true)
  assert.equal(panel.state().generationHint, 'settings.ca_trust_uninstall_first')
})

test('a missing key prevents installation but allows removal of an installed CA', () => {
  const panel = panelState({ caInfo: { certExists: true, keyExists: false, error: 'missing key' } })
  assert.equal(panel.state().actionDisabled, true)
  panel.props.caTrustStatus.installed = true
  assert.equal(panel.state().actionDisabled, false)
})

test('unknown trust is never presented as not installed and prevents changes', () => {
  for (const overrides of [
    { caTrustLoadFailed: true },
    { caTrustStatus: { supported: true, installed: false, sha256Fingerprint: 'fingerprint', error: 'denied' } },
  ]) {
    const panel = panelState(overrides)
    assert.equal(panel.state().summary, 'settings.ca_trust_status_unavailable')
    assert.equal(panel.state().actionDisabled, true)
    assert.equal(panel.state().generationDisabled, true)
  }
  const loading = panelState({ caTrustLoading: true })
  assert.equal(loading.state().summary, 'settings.ca_trust_loading')
  assert.equal(loading.state().actionDisabled, true)
})

test('pending mutations and unsaved settings prevent repeated trust actions', () => {
  for (const key of ['isGenerating', 'isChangingCaTrust', 'caSettingsDirty']) {
    assert.equal(panelState({ [key]: true }).state().actionDisabled, true)
  }
  assert.equal(panelState({ isChangingCaTrust: true }).state().generationDisabled, true)
})

test('a malformed certificate can still be regenerated to repair its configuration', () => {
  const panel = panelState({
    caInfo: { certExists: true, keyExists: true, error: 'malformed' },
    caTrustStatus: { supported: true, installed: false, sha256Fingerprint: '', error: 'malformed' },
  })
  assert.equal(panel.state().actionDisabled, true)
  assert.equal(panel.state().generationDisabled, false)
})

test('unsupported platforms retain CA generation', () => {
  assert.equal(panelState({ caTrustStatus: { supported: false } }).state().generationDisabled, false)
})

test('CA trust copy has matching bilingual keys and placeholders', () => {
  const locales = ['zh', 'en'].map((locale) =>
    JSON.parse(readFileSync(new URL(`../src/locales/${locale}.json`, import.meta.url), 'utf8').replace(/^\uFEFF/, '')).settings,
  )
  const keys = (settings) => Object.keys(settings).filter((key) => key.startsWith('ca_trust_')).sort()
  assert.deepEqual(keys(locales[0]), keys(locales[1]))
  assert.deepEqual(Object.keys(locales[0].ca_trust_errors).sort(), Object.keys(locales[1].ca_trust_errors).sort())
  for (const key of keys(locales[0])) {
    if (typeof locales[0][key] !== 'string') continue
    const placeholders = (value) => value.match(/\{\w+\}/g) ?? []
    assert.deepEqual(placeholders(locales[0][key]), placeholders(locales[1][key]))
  }
})

const viewFilename = new URL('../src/views/SettingsView.vue', import.meta.url)
const viewScript = parse(readFileSync(viewFilename, 'utf8')).descriptor.scriptSetup.content
const viewSource = ts.createSourceFile('SettingsView.ts', viewScript, ts.ScriptTarget.ES2022, true)
const loadFunction = viewSource.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === 'loadCAInfo')
const loadCode = ts.transpileModule(loadFunction.getText(viewSource), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const guardCode = ts.transpileModule(readFileSync(new URL('../src/utils/latestOperation.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function refreshHarness() {
  const exports = {}
  runInNewContext(guardCode, { exports })
  const requests = []
  const notifications = []
  function deferred(kind) {
    return new Promise((resolve, reject) => requests.push({ kind, resolve, reject }))
  }
  const context = {
    caInfoRequestGuard: exports.createLatestOperationGuard(),
    caInfo: vue.ref(null),
    caTrustStatus: vue.ref(null),
    caTrustLoadFailed: vue.ref(false),
    isLoadingCAInfo: vue.ref(false),
    isUnmounted: false,
    GetCACertificateInfo: () => deferred('info'),
    GetCurrentCACertificateTrustStatus: () => deferred('trust'),
    notify: { error: (message) => notifications.push(message) },
    t: (key) => key,
    exports: {},
  }
  runInNewContext(`${loadCode}\nexports.load = loadCAInfo;`, context)
  return { context, requests, notifications, load: context.exports.load }
}

test('late status results cannot overwrite a newer certificate or trust state', async () => {
  const harness = refreshHarness()
  const older = harness.load()
  const newer = harness.load()
  await Promise.resolve()
  harness.requests[2].resolve({ subject: 'new certificate' })
  harness.requests[3].resolve({ supported: true, installed: true, sha256Fingerprint: 'new' })
  await newer
  harness.requests[0].resolve({ subject: 'old certificate' })
  harness.requests[1].reject(new Error('old failed query'))
  await older
  assert.equal(harness.context.caInfo.value.subject, 'new certificate')
  assert.equal(harness.context.caTrustStatus.value.sha256Fingerprint, 'new')
  assert.equal(harness.context.caTrustLoadFailed.value, false)
  assert.deepEqual(harness.notifications, [])
})

test('a rejected refresh marks trust as unknown while retaining platform support', async () => {
  const harness = refreshHarness()
  harness.context.caTrustStatus.value = { supported: true, installed: false, sha256Fingerprint: 'old' }
  const pending = harness.load()
  await Promise.resolve()
  harness.requests[0].resolve({ subject: 'certificate' })
  harness.requests[1].reject(new Error('denied'))
  await pending
  assert.equal(harness.context.caTrustLoadFailed.value, true)
  assert.equal(harness.context.caTrustStatus.value.supported, true)
  assert.equal(harness.context.isLoadingCAInfo.value, false)
  assert.deepEqual(harness.notifications, ['settings.ca_trust_status_unavailable'])
})

test('closing the settings window invalidates requests and prevents further refreshes', async () => {
  const harness = refreshHarness()
  const pending = harness.load()
  await Promise.resolve()
  harness.context.isUnmounted = true
  harness.context.caInfoRequestGuard.invalidate()
  harness.requests[0].resolve({ subject: 'late certificate' })
  harness.requests[1].resolve({ supported: true, installed: true })
  await pending
  await harness.load()
  assert.equal(harness.context.caInfo.value, null)
  assert.equal(harness.context.caTrustStatus.value, null)
  assert.equal(harness.requests.length, 2)
})

test('synchronous bridge failures release loading and mark trust as unknown', async () => {
  const harness = refreshHarness()
  harness.context.GetCACertificateInfo = () => { throw new Error('bridge unavailable') }
  harness.context.GetCurrentCACertificateTrustStatus = () => { throw new Error('bridge unavailable') }
  await harness.load()
  assert.equal(harness.context.isLoadingCAInfo.value, false)
  assert.equal(harness.context.caTrustLoadFailed.value, true)
  assert.equal(harness.context.caInfo.value, null)
})
