import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import * as vue from 'vue'
import * as pinia from 'pinia'

const filename = new URL('../src/stores/updater.ts', import.meta.url)
const code = ts.transpileModule(readFileSync(filename, 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

const phases = {
  $zero: '',
  UpdatePhaseIdle: 'idle',
  UpdatePhaseChecking: 'checking',
  UpdatePhaseUpToDate: 'up-to-date',
  UpdatePhaseAvailable: 'available',
  UpdatePhaseDownloading: 'downloading',
  UpdatePhaseVerifying: 'verifying',
  UpdatePhasePreparing: 'preparing',
  UpdatePhaseReady: 'ready',
  UpdatePhaseError: 'error',
}
const applyModes = {
  $zero: '',
  UpdateApplyModeSelf: 'self',
  UpdateApplyModeManual: 'manual',
}
const eventNames = {
  OPEN_UPDATE_WINDOW_EVENT: 'app:open-update-window',
  UPDATE_PROGRESS_EVENT: 'app:update-progress',
  UPDATE_STATE_CHANGED_EVENT: 'app:update-state-changed',
}

function snapshot(revision, phase, extra = {}) {
  return {
    revision,
    phase,
    currentVersion: '1.0.2',
    applyMode: 'self',
    release: null,
    progress: null,
    failure: null,
    canCancel: false,
    ...extra,
  }
}

function setupUpdaterStore(initialSnapshot = snapshot(1, 'idle')) {
  const listeners = new Map()
  const calls = { checks: 0, downloads: 0, cancels: 0, restarts: 0, opens: 0 }
  const bridge = {
    GetUpdateSnapshot: async () => initialSnapshot,
    CheckForUpdates: async () => {
      calls.checks++
    },
    DownloadUpdate: async () => {
      calls.downloads++
    },
    CancelUpdate: async () => {
      calls.cancels++
    },
    RestartForUpdate: async () => {
      calls.restarts++
    },
  }
  const runtime = {
    Events: {
      On(name, handler) {
        listeners.set(name, handler)
        return () => listeners.delete(name)
      },
      async Emit(name) {
        if (name === eventNames.OPEN_UPDATE_WINDOW_EVENT) calls.opens++
      },
    },
  }
  const exports = {}
  runInNewContext(code, {
    exports,
    require(id) {
      if (id === 'vue') return vue
      if (id === 'pinia') return pinia
      if (id === '@wailsio/runtime') return runtime
      if (id.startsWith('#bindings/') && id.endsWith('/appservice')) return bridge
      if (id.startsWith('#bindings/') && id.endsWith('/models')) {
        return { UpdatePhase: phases, UpdateApplyMode: applyModes }
      }
      if (id.endsWith('/runtime/appEvents')) return eventNames
      throw new Error(`Unexpected import: ${id}`)
    },
  })
  pinia.setActivePinia(pinia.createPinia())
  const store = exports.useUpdaterStore()
  return {
    store,
    calls,
    emit(name, data) {
      listeners.get(name)?.({ data })
    },
    get listenerCount() {
      return listeners.size
    },
  }
}

test('snapshot hydration and events keep the newest revision', async () => {
  const app = setupUpdaterStore(
    snapshot(2, 'available', {
      release: {
        version: '1.1.0',
        name: '',
        notes: '',
        publishedAt: '',
        releaseURL: '',
        artifactName: 'update.exe',
        artifactSize: 100,
      },
    }),
  )
  await app.store.initialize()
  assert.equal(app.store.phase, 'available')
  assert.equal(app.listenerCount, 2)

  app.emit(eventNames.UPDATE_STATE_CHANGED_EVENT, snapshot(1, 'idle'))
  assert.equal(app.store.phase, 'available', 'older snapshots must not roll state back')

  app.emit(eventNames.UPDATE_PROGRESS_EVENT, {
    revision: 3,
    progress: { written: 25, total: 100, rate: 50 },
  })
  assert.equal(app.store.snapshot.revision, 3)
  assert.equal(app.store.progress.written, 25)

  app.emit(eventNames.UPDATE_STATE_CHANGED_EVENT, snapshot(4, 'verifying'))
  assert.equal(app.store.phase, 'verifying')
  app.emit(eventNames.UPDATE_PROGRESS_EVENT, {
    revision: 3,
    progress: { written: 50, total: 100, rate: 50 },
  })
  assert.equal(app.store.progress, null, 'stale progress must not overwrite a newer phase')

  app.store.cleanup()
  assert.equal(app.listenerCount, 0)
})

test('actions call the split updater bindings and window event', async () => {
  const app = setupUpdaterStore()
  await app.store.initialize()
  await app.store.check()
  await app.store.download()
  await app.store.cancel()
  await app.store.restart()
  await app.store.openWindow()
  assert.deepEqual(app.calls, { checks: 1, downloads: 1, cancels: 1, restarts: 1, opens: 1 })
  assert.equal(app.store.actionPending, false)
  app.store.cleanup()
})

test('initialize is idempotent and cleanup permits a fresh subscription', async () => {
  const app = setupUpdaterStore()
  await Promise.all([app.store.initialize(), app.store.initialize()])
  assert.equal(app.listenerCount, 2)
  app.store.cleanup()
  await app.store.initialize()
  assert.equal(app.listenerCount, 2)
  app.store.cleanup()
})
