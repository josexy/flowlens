import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc'
import * as vue from 'vue'
import * as pinia from 'pinia'

const code = ts.transpileModule(
  readFileSync(new URL('../src/stores/rewriteRules.ts', import.meta.url), 'utf8'),
  {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  },
).outputText
const clone = (value) => JSON.parse(JSON.stringify(value))
function rule(id, name = id) {
  return {
    id,
    name,
    enabled: false,
    method: 'ALL',
    urlPattern: 'https://example.com/*',
    action: {
      version: 1,
      type: 'request',
      targetURL: '',
      hostPolicy: 'target',
      headers: [],
      query: [],
      body: { mode: 'none', text: '', pattern: '', replacement: '' },
    },
    createdAt: 1,
    updatedAt: 1,
    unavailableReason: '',
  }
}
function setup(
  initial = { revision: 1, enabled: false, rules: [rule('a'), rule('b')] },
  globals = {},
) {
  let server = clone(initial)
  let nextId = 0
  const listeners = new Map()
  const calls = []
  const mutate =
    (kind, apply) =>
    async (...args) => {
      const revision = args.at(-1)
      calls.push({ kind, args: clone(args) })
      assert.equal(revision, server.revision, 'every write uses the current revision')
      const next = clone(server)
      apply(next, ...args)
      next.revision++
      server = next
      return clone(server)
    }
  const bridge = {
    GetState: async () => clone(server),
    SaveRule: mutate('save', (next, candidate) => {
      const id = candidate.id || `new-${++nextId}`
      const saved = { ...clone(candidate), id, enabled: candidate.id ? candidate.enabled : false }
      const index = next.rules.findIndex((item) => item.id === id)
      if (index < 0) next.rules.push(saved)
      else next.rules[index] = saved
    }),
    SetEnabled: mutate('enabled', (next, enabled) => {
      next.enabled = enabled
    }),
    SetRuleEnabled: mutate('ruleEnabled', (next, id, enabled) => {
      next.rules.find((r) => r.id === id).enabled = enabled
    }),
    ReorderRules: mutate('reorder', (next, ids) => {
      next.rules = ids.map((id) => next.rules.find((r) => r.id === id))
    }),
    DeleteRule: mutate('delete', (next, id) => {
      next.rules = next.rules.filter((r) => r.id !== id)
    }),
  }
  const exports = {}
  runInNewContext(code, {
    ...globals,
    exports,
    TextEncoder,
    require(id) {
      if (id === 'vue') return vue
      if (id === 'pinia') return pinia
      if (id === '@wailsio/runtime')
        return {
          Events: {
            On(name, callback) {
              listeners.set(name, callback)
              return () => listeners.delete(name)
            },
          },
        }
      if (id.endsWith('/rewriteservice')) return bridge
      throw new Error(`Unexpected import: ${id}`)
    },
  })
  pinia.setActivePinia(pinia.createPinia())
  const store = exports.useRewriteRulesStore()
  return {
    store,
    bridge,
    calls,
    listeners,
    get server() {
      return server
    },
    setServer(next) {
      server = clone(next)
    },
    emit(revision) {
      listeners.get('rewrite:changed')?.({ data: { revision } })
    },
  }
}
function edit(store, name) {
  store.update({ ...store.selectedRule, name })
}
async function flush() {
  await new Promise((resolve) => setImmediate(resolve))
}
function deferred() {
  let resolve
  let reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

test('drafts survive selection; new rules are disabled and saving publishes only the selected draft', async () => {
  const { store, calls } = setup()
  await store.initialize()
  edit(store, 'first edit')
  store.select('b')
  edit(store, 'second edit')
  store.select('a')
  assert.equal(store.selectedRule.name, 'first edit')
  assert.equal((await store.save()).saved, true)
  assert.equal(store.isDirty('a'), false)
  assert.equal(store.isDirty('b'), true)
  assert.equal(calls[0].args[0].name, 'first edit')
  store.create('new rule')
  assert.equal(store.selectedRule.enabled, false)
  assert.equal(store.selectedRule.method, 'ALL')
  assert.equal(store.selectedRule.id, '')
  assert.equal((await store.save()).saved, true)
  assert.equal(store.selectedId, 'new-1')
  assert.equal(store.selectedRule.enabled, false)
})

test('blank rule fields stay local and show field errors that clear as the draft is corrected', async () => {
  const { store, calls } = setup()
  await store.initialize()
  store.update({ ...store.selectedRule, name: '  ', urlPattern: '' })
  assert.equal((await store.save()).saved, false)
  assert.equal(calls.length, 0)
  assert.equal(store.error, '')
  assert.equal(store.selectedFieldErrors.name, 'rewrite_rules.validation.name_required')
  assert.equal(store.selectedFieldErrors.urlPattern, 'rewrite_rules.validation.url_required')
  store.select('b')
  assert.equal(Object.keys(store.selectedFieldErrors).length, 0)
  store.select('a')
  edit(store, 'Valid name')
  assert.equal(store.selectedFieldErrors.name, undefined)
  assert.equal(store.selectedFieldErrors.urlPattern, 'rewrite_rules.validation.url_required')
  store.update({ ...store.selectedRule, urlPattern: 'https://example.com/*' })
  assert.equal(Object.keys(store.selectedFieldErrors).length, 0)
  assert.equal((await store.save()).saved, true)
  assert.equal(calls.length, 1)
})

test('rule field limits use UTF-8 bytes and accept the exact backend boundaries', async () => {
  const { store, calls } = setup()
  await store.initialize()
  store.update({ ...store.selectedRule, name: '中'.repeat(86), urlPattern: '中'.repeat(5462) })
  assert.equal((await store.save()).saved, false)
  assert.equal(calls.length, 0)
  assert.equal(store.selectedFieldErrors.name, 'rewrite_rules.validation.name_too_long')
  assert.equal(store.selectedFieldErrors.urlPattern, 'rewrite_rules.validation.url_too_long')
  store.update({
    ...store.selectedRule,
    name: '中'.repeat(85) + 'a',
    urlPattern: '中'.repeat(5461) + '*',
  })
  assert.equal((await store.save()).saved, true)
  assert.equal(calls.length, 1)
})

test('save all selects the invalid draft, and revert clears its field errors', async () => {
  const { store, calls } = setup()
  await store.initialize()
  store.update({ ...store.selectedRule, urlPattern: '   ' })
  store.select('b')
  assert.equal(await store.saveAll(), false)
  assert.equal(store.selectedId, 'a')
  assert.equal(store.selectedFieldErrors.urlPattern, 'rewrite_rules.validation.url_required')
  assert.equal(calls.length, 0)
  store.revert()
  assert.equal(Object.keys(store.selectedFieldErrors).length, 0)
  assert.equal(store.hasDirtyDrafts, false)
})

test('external edits preserve dirty content and require explicit conflict acknowledgement', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'local')
  ctx.setServer({ ...ctx.server, revision: 2, rules: [rule('a', 'remote'), rule('b')] })
  ctx.emit(2)
  await flush()
  assert.equal(ctx.store.selectedRule.name, 'local')
  assert.equal(ctx.store.selectedDraft.conflict, true)
  assert.equal((await ctx.store.save()).saved, false)
  assert.equal(ctx.calls.length, 0)
  ctx.store.acceptConflict()
  assert.equal((await ctx.store.save()).saved, true)
  assert.equal(ctx.server.rules[0].name, 'local')
})

test('stale events are ignored; unrelated external toggle does not conflict with drafts', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'local')
  ctx.setServer({ ...ctx.server, revision: 2, rules: [{ ...rule('a'), enabled: true }, rule('b')] })
  ctx.emit(2)
  await flush()
  assert.equal(ctx.store.selectedRule.enabled, true)
  assert.equal(ctx.store.selectedDraft.conflict, false)
  const getState = ctx.bridge.GetState
  ctx.bridge.GetState = () => {
    throw new Error('stale event must not fetch')
  }
  ctx.emit(1)
  ctx.emit(2)
  ctx.emit(NaN)
  await flush()
  assert.equal(ctx.store.error, '')
  ctx.bridge.GetState = getState
})

test('failed toggles and reorder retain authoritative state and dirty draft', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'draft')
  ctx.bridge.SetEnabled = async () => {
    throw new Error('disk full')
  }
  assert.equal(await ctx.store.setEnabled(true), false)
  assert.equal(ctx.store.state.enabled, false)
  ctx.bridge.ReorderRules = async () => {
    throw new Error('disk full')
  }
  assert.equal(await ctx.store.reorder(['b', 'a']), false)
  assert.equal(ctx.store.rules.map((r) => r.id).join(','), 'a,b')
  assert.equal(ctx.store.selectedRule.name, 'draft')
  assert.match(ctx.store.error, /disk full/)
})

test('mutations wait for persistence and prevent concurrent writes', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  const wait = deferred()
  ctx.bridge.SetEnabled = () => wait.promise
  const pending = ctx.store.setEnabled(true)
  assert.equal(ctx.store.state.enabled, false)
  assert.equal(ctx.store.busy, true)
  assert.equal(await ctx.store.setRuleEnabled('a', true), false)
  wait.resolve({ ...ctx.server, revision: 2, enabled: true })
  assert.equal(await pending, true)
  assert.equal(ctx.store.state.enabled, true)
  assert.equal(ctx.store.busy, false)
})

test('save failure refreshes revisions while preserving draft and conflict', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'local')
  ctx.bridge.SaveRule = async () => {
    throw new Error('revision conflict')
  }
  ctx.setServer({ ...ctx.server, revision: 2, rules: [rule('a', 'external'), rule('b')] })
  assert.equal((await ctx.store.save()).saved, false)
  assert.equal(ctx.store.state.revision, 2)
  assert.equal(ctx.store.selectedRule.name, 'local')
  assert.equal(ctx.store.selectedDraft.conflict, true)
  assert.equal(ctx.store.hasDirtyDrafts, true)
})

test('save all stops on failure and leaves remaining drafts for the quit guard', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'first')
  ctx.store.select('b')
  edit(ctx.store, 'second')
  const save = ctx.bridge.SaveRule
  ctx.bridge.SaveRule = (candidate, revision) =>
    candidate.id === 'b' ? Promise.reject(new Error('cannot save')) : save(candidate, revision)
  assert.equal(await ctx.store.saveAll(), false)
  assert.equal(ctx.store.isDirty('a'), false)
  assert.equal(ctx.store.isDirty('b'), true)
  assert.equal(ctx.store.selectedId, 'b')
  ctx.store.discardAll()
  assert.equal(ctx.store.hasDirtyDrafts, false)
  assert.equal(ctx.store.selectedRule.name, 'b')
})

test('externally deleted draft is preserved and can be deliberately recreated disabled', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'retained')
  ctx.setServer({ ...ctx.server, revision: 2, rules: [rule('b')] })
  ctx.emit(2)
  await flush()
  assert.equal(ctx.store.selectedDraft.conflict, true)
  assert.equal(ctx.store.rows.length, 2)
  ctx.store.acceptConflict()
  assert.equal(ctx.store.selectedRule.id, '')
  assert.equal((await ctx.store.save()).saved, true)
  assert.equal(ctx.store.selectedRule.name, 'retained')
  assert.equal(ctx.store.selectedRule.enabled, false)
})

test('cleanup disposes event listener and ignores outstanding snapshot', async () => {
  const ctx = setup()
  const wait = deferred()
  ctx.bridge.GetState = () => wait.promise
  const pending = ctx.store.initialize()
  assert.equal(ctx.listeners.size, 1)
  ctx.store.cleanup()
  assert.equal(ctx.listeners.size, 0)
  wait.resolve({ revision: 5, enabled: true, rules: [] })
  await pending
  assert.equal(ctx.store.state.revision, -1)
})

test('initial asynchronous rule loading keeps writes unavailable until the saved snapshot arrives', async () => {
  const ctx = setup({ revision: 4, enabled: true, rules: [rule('saved')] })
  const wait = deferred()
  ctx.bridge.GetState = () => wait.promise
  const pending = ctx.store.initialize()
  assert.equal(ctx.store.loading, true)
  assert.equal(ctx.store.state.revision, -1)
  assert.equal(ctx.store.selectedRule, null)
  await ctx.store.setEnabled(false)
  assert.equal(ctx.calls.length, 0)
  wait.resolve(ctx.server)
  await pending
  assert.equal(ctx.store.loading, false)
  assert.equal(ctx.store.state.revision, 4)
  assert.equal(ctx.store.state.enabled, true)
  assert.equal(ctx.store.selectedId, 'saved')
  ctx.store.cleanup()
})

test('initial rule-load failure stays visible without creating a writable empty state', async () => {
  const ctx = setup()
  ctx.bridge.GetState = async () => {
    throw new Error('load rewrite rules: database read failed')
  }
  await ctx.store.initialize()
  assert.equal(ctx.store.loading, false)
  assert.equal(ctx.store.state.revision, -1)
  assert.match(ctx.store.error, /load rewrite rules: database read failed/)
  await ctx.store.setEnabled(true)
  assert.equal(ctx.calls.length, 0)
  assert.equal(ctx.store.hasDirtyDrafts, false)
  ctx.store.cleanup()
})

test('event arriving during initial read is followed by a newer read', async () => {
  const ctx = setup()
  const wait = deferred()
  const getState = ctx.bridge.GetState
  ctx.bridge.GetState = () => wait.promise
  const pending = ctx.store.initialize()
  ctx.setServer({ ...ctx.server, revision: 2, enabled: true })
  ctx.emit(2)
  ctx.bridge.GetState = getState
  wait.resolve({ revision: 1, enabled: false, rules: [rule('a')] })
  await pending
  assert.equal(ctx.store.state.revision, 2)
  assert.equal(ctx.store.state.enabled, true)
})

test('a save response cannot replace a newer external snapshot that arrived first', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'saved by us')
  const wait = deferred()
  ctx.bridge.SaveRule = () => wait.promise
  const pending = ctx.store.save()
  ctx.setServer({ ...ctx.server, revision: 3, rules: [rule('a', 'newer remote edit'), rule('b')] })
  ctx.emit(3)
  await flush()
  wait.resolve({ ...ctx.server, revision: 2, rules: [rule('a', 'saved by us'), rule('b')] })
  assert.equal((await pending).saved, true)
  assert.equal(ctx.store.selectedRule.name, 'newer remote edit')
  assert.equal(ctx.store.state.revision, 3)
  assert.equal(ctx.store.isDirty('a'), false)
})

const guardEvents = {
  REWRITE_DRAFTS_DIRTY_CHANGED_EVENT: 'app:rewrite-drafts-dirty-changed',
  CONFIRM_REWRITE_QUIT_REQUEST_EVENT: 'app:confirm-rewrite-quit-request',
  REWRITE_QUIT_CONFIRMED_EVENT: 'app:rewrite-quit-confirmed',
}
function setupGuard(store) {
  const source = readFileSync(
    new URL('../src/components/rewrite-rules/RewriteDraftGuard.vue', import.meta.url),
    'utf8',
  )
  const script = compileScript(parse(source).descriptor, { id: 'rewrite-guard-test' }).content
  const js = ts.transpileModule(script, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const mounted = []
  const unmount = []
  const listeners = new Map()
  const emitted = []
  let selected = 0
  const exports = {}
  const runtime = {
    Events: {
      On(name, callback) {
        listeners.set(name, callback)
        return () => listeners.delete(name)
      },
      async Emit(name, payload) {
        emitted.push({ name, payload: clone(payload) })
      },
    },
  }
  runInNewContext(js, {
    exports,
    require(id) {
      if (id === 'vue')
        return {
          ...vue,
          onMounted(fn) {
            mounted.push(fn)
          },
          onBeforeUnmount(fn) {
            unmount.push(fn)
          },
        }
      if (id === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (id === '@wailsio/runtime') return runtime
      if (id.endsWith('/stores/rewriteRules')) return { useRewriteRulesStore: () => store }
      if (id.endsWith('/stores/workbench'))
        return {
          useWorkbenchStore: () => ({
            selectRewriteRulesItem() {
              selected++
            },
          }),
        }
      if (id.endsWith('/runtime/appEvents')) return guardEvents
      throw new Error(`Unexpected guard import: ${id}`)
    },
  })
  const scope = vue.effectScope()
  const component = scope.run(() => exports.default.setup({}, { expose() {} }))
  mounted.forEach((fn) => fn())
  return {
    component,
    emitted,
    listeners,
    get selected() {
      return selected
    },
    request(reason = 'quit') {
      listeners.get(guardEvents.CONFIRM_REWRITE_QUIT_REQUEST_EVENT)?.({
        data: { requestId: 'request-1', reason },
      })
    },
    cleanup() {
      unmount.forEach((fn) => fn())
      scope.stop()
    },
    replies() {
      return emitted.filter((item) => item.name === guardEvents.REWRITE_QUIT_CONFIRMED_EVENT)
    },
  }
}

test('quit guard waits for successful save before acknowledging restart and cleans up', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'draft')
  const guard = setupGuard(ctx.store)
  assert.equal(guard.emitted[0].payload, true)
  const wait = deferred()
  const save = ctx.bridge.SaveRule
  ctx.bridge.SaveRule = () => wait.promise
  guard.request('update')
  const pending = guard.component.reply('save')
  assert.equal(guard.replies().length, 0)
  wait.resolve(await save(clone(ctx.store.selectedRule), ctx.server.revision))
  await pending
  assert.deepEqual(guard.replies()[0].payload, {
    requestId: 'request-1',
    reason: 'update',
    decision: 'save',
  })
  assert.equal(ctx.store.hasDirtyDrafts, false)
  guard.cleanup()
  assert.equal(guard.listeners.size, 0)
  assert.equal(ctx.listeners.size, 0)
})

test('quit guard keeps failed saves open, cancel retains drafts, discard is explicit', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'draft')
  ctx.bridge.SaveRule = async () => {
    throw new Error('disk full')
  }
  const guard = setupGuard(ctx.store)
  guard.request()
  await guard.component.reply('save')
  assert.equal(guard.replies().length, 0)
  assert.equal(guard.component.pending.value.requestId, 'request-1')
  assert.equal(ctx.store.hasDirtyDrafts, true)
  assert.equal(guard.selected, 1)
  await guard.component.reply('cancel')
  assert.equal(guard.replies()[0].payload.decision, 'cancel')
  assert.equal(ctx.store.hasDirtyDrafts, true)
  guard.request()
  await guard.component.reply('discard')
  assert.equal(guard.replies()[1].payload.decision, 'discard')
  assert.equal(ctx.store.hasDirtyDrafts, false)
  guard.cleanup()
})

test('quit guard keeps invalid drafts open and exposes a localized field error without writing', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, '')
  const guard = setupGuard(ctx.store)
  guard.request()
  await guard.component.reply('save')
  assert.equal(guard.replies().length, 0)
  assert.equal(guard.component.pending.value.requestId, 'request-1')
  assert.equal(guard.component.fieldError.value, 'rewrite_rules.validation.name_required')
  assert.equal(ctx.calls.length, 0)
  assert.equal(ctx.store.hasDirtyDrafts, true)
  guard.cleanup()
})

test('deleting a dirty rule selects a remaining rule when its event precedes the reply', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'discard me')
  const wait = deferred()
  ctx.bridge.DeleteRule = () => wait.promise
  const pending = ctx.store.remove()
  ctx.setServer({ ...ctx.server, revision: 2, rules: [rule('b')] })
  ctx.emit(2)
  await flush()
  assert.equal(ctx.store.selectedId, 'a')
  wait.resolve(ctx.server)
  assert.equal(await pending, true)
  assert.equal(ctx.store.selectedId, 'b')
  assert.equal(ctx.store.hasDirtyDrafts, false)
})

function setupSurface(store) {
  const mounted = []
  const path = new URL('../src/components/rewrite-rules/RewriteRulesSurface.vue', import.meta.url)
  const script = compileScript(parse(readFileSync(path, 'utf8')).descriptor, {
    id: 'review-surface',
  }).content
  const js = ts.transpileModule(script, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const exports = {}
  runInNewContext(js, {
    exports,
    require(id) {
      if (id === 'vue') return { ...vue, onBeforeUnmount() {}, onMounted: (fn) => mounted.push(fn) }
      if (id === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (id.endsWith('/stores/rewriteRules')) return { useRewriteRulesStore: () => store }
      if (id.endsWith('/stores/workbench'))
        return { useWorkbenchStore: () => ({ activeContent: 'rewriteRules' }) }
      if (id === '@/shortcuts')
        return { registerShortcutHandler: () => () => {}, useShortcutKbds: () => vue.ref([]) }
      if (id.endsWith('.vue')) return {}
      throw new Error(id)
    },
  })
  const surface = exports.default.setup({}, { expose() {} })
  mounted.forEach((fn) => fn())
  return surface
}

test('save-and-delete retains an invalid draft and displays its field error', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, '')
  const surface = setupSurface(ctx.store)
  surface.openDelete()
  await surface.remove(true)
  assert.equal(surface.deleteOpen.value, true)
  assert.equal(surface.deleteFieldError.value, 'rewrite_rules.validation.name_required')
  assert.equal(ctx.calls.length, 0)
  assert.equal(ctx.store.hasDirtyDrafts, true)
})

test('match preview uses the same field validation before calling the backend', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  ctx.store.update({ ...ctx.store.selectedRule, urlPattern: '' })
  const source = readFileSync(
    new URL('../src/components/rewrite-rules/RewritePreview.vue', import.meta.url),
    'utf8',
  )
  const script = compileScript(parse(source).descriptor, { id: 'preview-validation-test' }).content
  const js = ts.transpileModule(script, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  let previews = 0
  const exports = {}
  runInNewContext(js, {
    exports,
    require(id) {
      if (id === 'vue') return vue
      if (id === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (id.endsWith('/rewriteservice'))
        return {
          async Preview() {
            previews++
            return { matched: true, captures: [], targetURL: '' }
          },
        }
      throw new Error(`Unexpected preview import: ${id}`)
    },
  })
  const props = vue.reactive({ rule: ctx.store.selectedRule, validate: () => ctx.store.validate() })
  const scope = vue.effectScope()
  const preview = scope.run(() => exports.default.setup(props, { expose() {} }))
  await preview.preview()
  assert.equal(previews, 0)
  assert.equal(preview.busy.value, false)
  assert.equal(preview.error.value, 'rewrite_rules.validation.fix_fields')
  assert.equal(ctx.store.selectedFieldErrors.urlPattern, 'rewrite_rules.validation.url_required')
  ctx.store.update({ ...ctx.store.selectedRule, urlPattern: 'https://example.com/*' })
  props.rule = ctx.store.selectedRule
  await vue.nextTick()
  assert.equal(preview.error.value, '')
  await preview.preview()
  assert.equal(previews, 1)
  assert.equal(preview.result.value.matched, true)
  scope.stop()
})

test('save-and-delete must never delete fallback selection after concurrent removal', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  edit(ctx.store, 'saved by us')
  const surface = setupSurface(ctx.store)
  surface.openDelete()
  const wait = deferred()
  ctx.bridge.SaveRule = () => wait.promise
  const pending = surface.remove(true)
  ctx.setServer({ revision: 3, enabled: false, rules: [rule('b')] })
  ctx.emit(3)
  await flush()
  wait.resolve({ revision: 2, enabled: false, rules: [rule('a', 'saved by us'), rule('b')] })
  await pending
  assert.deepEqual(
    ctx.calls.filter((x) => x.kind === 'delete').map((x) => x.args[0]),
    [],
    'rule a was already deleted; unrelated b must survive',
  )
})

test('save-and-delete uses a new draft’s assigned ID independently of selection', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  ctx.store.create('new draft')
  const surface = setupSurface(ctx.store)
  surface.openDelete()
  const wait = deferred()
  const save = ctx.bridge.SaveRule
  const candidate = clone(ctx.store.selectedRule)
  ctx.bridge.SaveRule = () => wait.promise
  const pending = surface.remove(true)
  ctx.store.select('b')
  wait.resolve(await save(candidate, ctx.server.revision))
  await pending
  assert.deepEqual(
    ctx.calls.filter((call) => call.kind === 'delete').map((call) => call.args[0]),
    ['new-1'],
  )
  assert.equal(ctx.store.selectedId, 'b')
  assert.deepEqual(
    ctx.server.rules.map((item) => item.id),
    ['a', 'b'],
  )
  assert.equal(surface.deleteOpen.value, false)
})

function renderedRuleFormKey(surface) {
  const source = readFileSync(
    new URL('../src/components/rewrite-rules/RewriteRulesSurface.vue', import.meta.url),
    'utf8',
  )
  const template = compileTemplate({
    source: parse(source).descriptor.template.content,
    id: 'surface-key-test',
  }).code
  const js = ts.transpileModule(template, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const exports = {}
  runInNewContext(js, {
    exports,
    require(id) {
      if (id === 'vue') return { ...vue, resolveComponent: (name) => name }
      throw new Error(`Unexpected template import: ${id}`)
    },
  })
  const tree = exports.render(vue.proxyRefs(surface), [])
  function find(node) {
    if (!node || typeof node !== 'object') return undefined
    if (node.type === 'RewriteRuleForm') return node.key
    if (Array.isArray(node.children)) {
      for (const child of node.children) {
        const key = find(child)
        if (key !== undefined) return key
      }
    }
    return undefined
  }
  return find(tree)
}

test('saving a new body rule retains its editor component identity but switching rules changes it', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  ctx.store.create('new body rule')
  ctx.store.update({
    ...ctx.store.selectedRule,
    action: {
      ...ctx.store.selectedRule.action,
      body: { mode: 'replace', text: 'replacement', pattern: '', replacement: '' },
    },
  })
  const surface = setupSurface(ctx.store)
  const draftKey = renderedRuleFormKey(surface)
  assert.ok(draftKey)
  const result = await ctx.store.save()
  assert.equal(result.saved, true)
  assert.equal(renderedRuleFormKey(surface), draftKey)
  ctx.store.select('b')
  assert.notEqual(renderedRuleFormKey(surface), draftKey)
  ctx.store.select(result.ruleId)
  assert.equal(renderedRuleFormKey(surface), draftKey)
})

function setupSidebar(store) {
  const mounted = []
  const source = readFileSync(
    new URL('../src/components/rewrite-rules/RewriteRulesSidebar.vue', import.meta.url),
    'utf8',
  )
  const script = compileScript(parse(source).descriptor, { id: 'sidebar-test' }).content
  const js = ts.transpileModule(script, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const exports = {}
  runInNewContext(js, {
    exports,
    require(id) {
      if (id === 'vue') return { ...vue, onMounted: (fn) => mounted.push(fn) }
      if (id === 'vue-draggable-plus') return { VueDraggable: {} }
      if (id === 'vue-i18n') return { useI18n: () => ({ t: (key) => key }) }
      if (id.endsWith('/stores/rewriteRules')) return { useRewriteRulesStore: () => store }
      if (id.endsWith('/stores/workbench'))
        return { useWorkbenchStore: () => ({ selectRewriteRulesItem() {} }) }
      throw new Error(`Unexpected sidebar import: ${id}`)
    },
  })
  const scope = vue.effectScope()
  const sidebar = scope.run(() => exports.default.setup({}, { expose() {} }))
  mounted.forEach((fn) => fn())
  return { sidebar, cleanup: () => scope.stop() }
}

test('fallback drag uses local order until persistence and excludes unsaved drafts', async () => {
  const ctx = setup()
  await ctx.store.initialize()
  ctx.store.create('unsaved')
  const { sidebar, cleanup } = setupSidebar(ctx.store)
  const rows = [...sidebar.sortableRows.value]
  const wait = deferred()
  let requestedIds
  ctx.bridge.ReorderRules = (ids) => {
    requestedIds = [...ids]
    return wait.promise
  }
  sidebar.startReorder()
  sidebar.sortableRows.value = [rows[1], rows[0], rows[2]]
  const pending = sidebar.finishReorder()
  assert.deepEqual(requestedIds, ['b', 'a'])
  assert.equal(ctx.store.rules.map((item) => item.id).join(','), 'a,b')
  assert.equal(ctx.store.busy, true)
  wait.resolve({ revision: 2, enabled: false, rules: [rule('b'), rule('a')] })
  await pending
  assert.equal(ctx.store.rules.map((item) => item.id).join(','), 'b,a')
  assert.equal(sidebar.sortableRows.value.map((item) => item.key).join(','), 'b,a,draft:1')
  assert.equal(ctx.store.hasDirtyDrafts, true)
  cleanup()
})

test('fallback drag restores local order on write failure and ignores filtered or stale drags', async () => {
  const ctx = setup({ revision: 1, enabled: false, rules: [rule('a', 'Alpha'), rule('b', 'Beta')] })
  await ctx.store.initialize()
  const { sidebar, cleanup } = setupSidebar(ctx.store)
  ctx.bridge.ReorderRules = async () => {
    throw new Error('disk full')
  }
  sidebar.startReorder()
  sidebar.sortableRows.value = [...sidebar.sortableRows.value].reverse()
  await sidebar.finishReorder()
  assert.equal(sidebar.sortableRows.value.map((item) => item.key).join(','), 'a,b')
  assert.match(ctx.store.error, /disk full/)
  let writes = 0
  ctx.bridge.ReorderRules = async () => {
    writes++
    throw new Error('must not write')
  }
  sidebar.search.value = 'Alpha'
  await vue.nextTick()
  sidebar.startReorder()
  await sidebar.finishReorder()
  assert.equal(sidebar.sortableRows.value.length, 1)
  assert.equal(writes, 0)
  sidebar.search.value = ''
  await vue.nextTick()
  sidebar.startReorder()
  sidebar.sortableRows.value = [...sidebar.sortableRows.value].reverse()
  ctx.setServer({ ...ctx.server, revision: 2, enabled: true })
  ctx.emit(2)
  await flush()
  await sidebar.finishReorder()
  assert.equal(writes, 0)
  assert.equal(sidebar.sortableRows.value.map((item) => item.key).join(','), 'a,b')
  cleanup()
})

test('draft content changes and exact reversions keep the synchronous quit state accurate', async () => {
  const saved = rule('a')
  saved.action.headers = [
    { operation: 'set', name: 'X-Test', value: 'original' },
    { operation: 'add', name: 'X-Test', value: '' },
  ]
  saved.action.query = [{ operation: 'set', name: 'q', value: 'original' }]
  saved.action.body = { mode: 'replace', text: 'original', pattern: 'a', replacement: 'b' }
  const ctx = setup({ revision: 1, enabled: false, rules: [saved] })
  await ctx.store.initialize()
  const guard = setupGuard(ctx.store)
  const changes = [
    (item) => {
      item.name = 'renamed'
    },
    (item) => {
      item.method = 'POST'
    },
    (item) => {
      item.urlPattern = 'https://other.example/*'
    },
    (item) => {
      item.action.version++
    },
    (item) => {
      item.action.type = 'response'
    },
    (item) => {
      item.action.targetURL = 'https://target.example/'
    },
    (item) => {
      item.action.hostPolicy = 'preserve'
    },
    (item) => {
      item.action.headers[0].operation = 'remove'
    },
    (item) => {
      item.action.headers[0].name = 'x-test'
    },
    (item) => {
      item.action.headers[0].value = 'changed'
    },
    (item) => {
      item.action.headers.reverse()
    },
    (item) => {
      item.action.query[0].value = 'changed'
    },
    (item) => {
      item.action.query = []
    },
    (item) => {
      item.action.body.mode = 'regex'
    },
    (item) => {
      item.action.body.text = 'changed'
    },
    (item) => {
      item.action.body.pattern = 'different'
    },
    (item) => {
      item.action.body.replacement = ''
    },
  ]
  for (const change of changes) {
    const next = clone(saved)
    change(next)
    ctx.store.update(next)
    assert.equal(ctx.store.hasDirtyDrafts, true)
    assert.equal(guard.emitted.at(-1).payload, true)
    ctx.store.update(saved)
    assert.equal(ctx.store.hasDirtyDrafts, false)
    assert.equal(guard.emitted.at(-1).payload, false)
  }
  ctx.store.update({
    ...saved,
    enabled: true,
    createdAt: 2,
    updatedAt: 3,
    unavailableReason: 'reason',
  })
  assert.equal(ctx.store.hasDirtyDrafts, false, 'runtime metadata is outside editable content')
  assert.equal(guard.emitted.length, 1 + changes.length * 2)
  guard.cleanup()
})

test('rule copies retain large body strings without JSON serialization and isolate editable containers', async () => {
  const saved = rule('a')
  saved.action.headers = [{ operation: 'set', name: 'X-Test', value: 'original' }]
  saved.action.body.text = '中'.repeat(1024 * 1024)
  const forbidden = () => {
    throw new Error('drafts must not serialize body strings')
  }
  const ctx = setup(
    { revision: 1, enabled: false, rules: [saved] },
    { JSON: { parse: forbidden, stringify: forbidden } },
  )
  await ctx.store.initialize()
  assert.equal(ctx.store.error, '')
  const candidate = clone(saved)
  ctx.store.update(candidate)
  candidate.action.headers[0].value = 'caller mutation'
  candidate.action.body.text = 'caller mutation'
  assert.equal(ctx.store.selectedRule.action.headers[0].value, 'original')
  assert.equal(ctx.store.selectedRule.action.body.text, saved.action.body.text)
  assert.equal(ctx.store.hasDirtyDrafts, false)
  ctx.store.selectedRule.action.headers[0].value = 'local edit'
  assert.equal(ctx.store.rules[0].action.headers[0].value, 'original')
  assert.equal(ctx.store.hasDirtyDrafts, true)
  assert.equal((await ctx.store.save()).saved, true)
  assert.equal(ctx.store.hasDirtyDrafts, false)
  ctx.store.update({ ...ctx.store.selectedRule, name: 'local' })
  ctx.store.revert()
  assert.equal(ctx.store.hasDirtyDrafts, false)
  assert.equal(ctx.store.selectedRule.action.body.text, saved.action.body.text)
})

test('editing one visited draft does not reread other draft bodies', async () => {
  const saved = Array.from({ length: 10 }, (_, index) => {
    const item = rule(String(index))
    item.action.body.text = `${index}:` + 'x'.repeat(64 * 1024)
    return item
  })
  const ctx = setup({ revision: 1, enabled: false, rules: saved })
  await ctx.store.initialize()
  const reads = new Map()
  for (const item of saved) {
    ctx.store.select(item.id)
    const body = vue.toRaw(ctx.store.selectedRule.action.body)
    const text = body.text
    Object.defineProperty(body, 'text', {
      enumerable: true,
      get() {
        reads.set(item.id, (reads.get(item.id) ?? 0) + 1)
        return text
      },
    })
  }
  const guard = setupGuard(ctx.store)
  reads.clear()
  edit(ctx.store, 'changed')
  assert.equal(ctx.store.hasDirtyDrafts, true)
  for (const item of saved.slice(0, -1)) {
    assert.equal(
      reads.get(item.id) ?? 0,
      0,
      `unchanged draft ${item.id} must use its cached dirty flag`,
    )
    assert.equal(ctx.store.isDirty(item.id), false)
  }
  edit(ctx.store, saved.at(-1).name)
  assert.equal(ctx.store.hasDirtyDrafts, false)
  assert.deepEqual(
    guard.emitted.map((event) => event.payload),
    [false, true, false],
  )
  guard.cleanup()
})

test('snapshot refresh publishes all visited drafts once to synchronous dirty observers', async () => {
  const saved = Array.from({ length: 10 }, (_, index) => rule(String(index)))
  const ctx = setup({ revision: 1, enabled: false, rules: saved })
  await ctx.store.initialize()
  for (const item of saved) ctx.store.select(item.id)
  const observed = []
  const stop = vue.watchSyncEffect(() => observed.push([...ctx.store.dirtyIds]))
  observed.length = 0
  ctx.setServer({
    revision: 2,
    enabled: true,
    rules: saved.map((item) => ({ ...item, name: 'updated' })),
  })
  ctx.emit(2)
  await flush()
  assert.deepEqual(observed, [[]], 'a snapshot replaces the draft collection atomically')
  assert.equal(ctx.store.selectedRule.name, 'updated')
  assert.equal(ctx.store.state.revision, 2)
  stop()
  ctx.store.cleanup()
})

test('quit guard stays lightweight until the rewrite feature is mounted', async () => {
  const ctx = setup()
  let reads = 0
  const getState = ctx.bridge.GetState
  ctx.bridge.GetState = () => {
    reads++
    return getState()
  }
  const guard = setupGuard(ctx.store)
  await flush()
  assert.equal(reads, 0)
  assert.equal(ctx.store.state.revision, -1)
  assert.equal(ctx.listeners.size, 0)
  assert.equal(guard.emitted[0].payload, false)
  guard.request('update')
  await flush()
  assert.equal(guard.replies()[0].payload.decision, 'save')
  assert.equal(reads, 0, 'clean quit does not need saved rule bodies')
  const { cleanup } = setupSidebar(ctx.store)
  setupSurface(ctx.store)
  await flush()
  assert.equal(reads, 1, 'simultaneously mounted feature views share the initial read')
  assert.equal(ctx.listeners.size, 1)
  assert.equal(ctx.store.selectedId, 'a')
  edit(ctx.store, 'local')
  assert.equal(guard.emitted.at(-1).payload, true)
  ctx.setServer({ ...ctx.server, revision: 2, enabled: true })
  ctx.emit(2)
  await flush()
  assert.equal(ctx.store.state.enabled, true)
  assert.equal(ctx.store.selectedRule.name, 'local')
  cleanup()
  guard.cleanup()
  assert.equal(ctx.listeners.size, 0)
})
