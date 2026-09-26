import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

// Exercise the actual status-bar setup with a mocked desktop bridge. No DOM or
// running Wails application is required for these event/action regressions.
const filename = new URL('../src/components/common/StatusBar.vue', import.meta.url)
const { descriptor } = parse(readFileSync(filename, 'utf8'))
const script = compileScript(descriptor, { id: 'updater-test' })
const code = ts.transpileModule(script.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function setupStatusBar() {
  const listeners = new Map()
  const mounted = []
  const unmounted = []
  let checks = 0
  let restarts = 0
  const names = Object.fromEntries([
    'CheckStarted', 'UpdateAvailable', 'DownloadStarted', 'DownloadComplete',
    'Verifying', 'Installing', 'NoUpdate', 'UpdateReady', 'Error',
  ].map((name) => [name, name]))
  names.User = { Cancel: 'Cancel', Remind: 'Remind', Skip: 'Skip' }
  const bridge = {
    CanSelfUpdate: async () => true,
    CheckForUpdates: async () => { checks++ },
    RestartForUpdate: async () => { restarts++ },
  }
  const exports = {}
  runInNewContext(code, {
    exports,
    require(id) {
      if (id === 'vue') return {
        ...vue,
        onMounted: (fn) => mounted.push(fn),
        onBeforeUnmount: (fn) => unmounted.push(fn),
      }
      if (id === '@wailsio/runtime') return {
        Browser: {}, Updater: { Events: names },
        Events: { On(name, fn) {
          listeners.set(name, fn)
          return () => listeners.delete(name)
        } },
      }
      if (id === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (id.startsWith('#bindings/')) return bridge
      if (id.endsWith('/useNotify')) return { useNotify: () => ({ error() {} }) }
      if (id.endsWith('/trafficWorkspace')) return { useTrafficWorkspaceStore: () => ({}) }
      throw new Error(`Unexpected import: ${id}`)
    },
  })
  const state = exports.default.setup({}, { expose() {} })
  mounted.forEach((fn) => fn())
  return {
    state, bridge,
    emit: (name) => listeners.get(name)?.({ data: null }),
    unmount: () => unmounted.forEach((fn) => fn()),
    get checks() { return checks },
    get restarts() { return restarts },
    get listenerCount() { return listeners.size },
  }
}

test('closing during download keeps progress and permits restart after Ready', async () => {
  const app = setupStatusBar()
  await app.state.checkForUpdates()
  app.emit('DownloadStarted')
  app.emit('Cancel')
  assert.equal(app.state.updateBusy.value, true)
  await app.state.checkForUpdates()
  assert.equal(app.checks, 1, 'closing must not permit an overlapping check')
  app.emit('UpdateReady')
  app.emit('Cancel')
  assert.equal(app.state.updateReady.value, true)
  assert.equal(app.state.updateBusy.value, false)
  await app.state.checkForUpdates()
  assert.equal(app.restarts, 1, 'restart works after the updater session was closed')
  assert.equal(app.checks, 1, 'a staged update must not be downloaded again')
  app.unmount()
  assert.equal(app.listenerCount, 0)
})

test('restart confirmation can be requested again after continuing to edit', async () => {
  const app = setupStatusBar()
  app.emit('UpdateReady')
  await app.state.checkForUpdates()
  assert.equal(app.state.updateActionPending.value, false)
  await app.state.checkForUpdates()
  assert.equal(app.restarts, 2)
  app.unmount()
})

test('failed check releases the action so it can be retried', async () => {
  const app = setupStatusBar()
  app.bridge.CheckForUpdates = async () => { throw new Error('bridge failure') }
  await app.state.checkForUpdates()
  assert.equal(app.state.updateBusy.value, false)
  assert.equal(app.state.updateActionPending.value, false)
  assert.equal(app.state.updateFailed.value, true)
  app.unmount()
})
