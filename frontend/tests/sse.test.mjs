import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { compileScript, parse } from '@vue/compiler-sfc'
import * as Vue from 'vue'
import * as Virtual from '@tanstack/vue-virtual'
import ts from 'typescript'

let server, sse, useSseMessages, largeText, dialog
before(async () => {
  server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true },
    server: { middlewareMode: true },
  })
  sse = await server.ssrLoadModule('/src/utils/sse.ts')
  ;({ useSseMessages } = await server.ssrLoadModule('/src/composables/useSseMessages.ts'))
  largeText = await server.ssrLoadModule('/src/components/common/monacoLargeText.ts')
  dialog = await server.ssrLoadModule('/src/utils/dialog.ts')
})
after(async () => {
  await server?.close()
})

function scope(t, setup) {
  const effect = Vue.effectScope()
  t.after(() => effect.stop())
  return effect.run(setup)
}

function parseChunks(chunks) {
  const parser = new sse.SseParser()
  const events = chunks.flatMap((chunk) => parser.append(chunk))
  const body = chunks.join('')
  return { events, parser, details: events.map((event) => sse.getSseMessageDetail(body, event)) }
}

async function until(predicate) {
  const deadline = Date.now() + 5000
  while (!predicate()) {
    assert.ok(Date.now() < deadline, 'SSE parsing must finish')
    await new Promise((resolve) => setTimeout(resolve, 5))
  }
  await Vue.nextTick()
}

test('all text boundaries preserve events, raw blocks, BOM, Unicode and mixed line endings', () => {
  const body =
    '\ufeff: ping\r\nid: control\r\nretry: 1500\r\n\r\n' +
    'event: server\rdata: 中文😀: hello\rdata:  indented\rid: 1\r\r' +
    'data: next\n\n' +
    'event:\r\ndata\r\ndata:\r\nid:\r\n\r\n' +
    'event: unfinished\ndata: tail'
  const expected = parseChunks([body])
  assert.deepEqual(
    expected.details.map((item) => [item.eventType, item.lastEventId, item.data]),
    [
      ['server', '1', '中文😀: hello\n indented'],
      ['message', '1', 'next'],
      ['message', '', '\n'],
    ],
  )
  assert.equal(expected.parser.hasPendingEvent, true)
  for (let index = 0; index <= body.length; index++) {
    assert.deepEqual(
      parseChunks([body.slice(0, index), body.slice(index)]).details,
      expected.details,
      `boundary ${index}`,
    )
  }
  assert.deepEqual(
    parseChunks(Array.from({ length: body.length }, (_, index) => body[index])).details,
    expected.details,
  )
})

test('stream-decoded UTF-8 byte boundaries preserve Chinese and emoji messages', () => {
  const body = '\ufeffdata: 中文😀\r\n\r\ndata: done\n\n'
  const bytes = new TextEncoder().encode(body)
  const expected = parseChunks([body]).details.map(({ data }) => data)
  for (let index = 0; index <= bytes.length; index++) {
    const decoder = new TextDecoder()
    const result = parseChunks([
      decoder.decode(bytes.slice(0, index), { stream: true }),
      decoder.decode(bytes.slice(index)),
    ])
    assert.deepEqual(
      result.details.map(({ data }) => data),
      expected,
    )
  }
})

test('only data blocks dispatch, including empty data, while control IDs persist and reset', () => {
  const result = parseChunks([
    ': heartbeat\n\nid: control\nretry: 123\nevent: ignored\n\n' +
      'DATA: ignored\nx-extra: field\n\ndata:\n\n' +
      'data: second\nid: control\n\n' +
      'id: bad\0id\nevent: custom\nevent: final\ndata: third\n\n' +
      'id\n\ndata: reset\n\n',
  ])
  assert.deepEqual(
    result.details.map((item) => [item.sequence, item.eventType, item.lastEventId, item.data]),
    [
      [1, 'message', 'control', ''],
      [2, 'message', 'control', 'second'],
      [3, 'final', 'control', 'third'],
      [4, 'message', '', 'reset'],
    ],
  )
  assert.equal(result.parser.hasPendingEvent, false)
})

test('CR dispatch is immediate and a later LF extends only its original block', () => {
  const parser = new sse.SseParser()
  const [first] = parser.append('data: one\r\r')
  assert.equal(first.rawEnd, 'data: one\r\r'.length)
  assert.equal(parser.hasPendingEvent, false)
  const [second] = parser.append('\ndata: two\r\n\r\n')
  const body = 'data: one\r\r\ndata: two\r\n\r\n'
  assert.equal(sse.getSseMessageDetail(body, first).raw, 'data: one\r\r\n')
  assert.equal(sse.getSseMessageDetail(body, second).raw, 'data: two\r\n\r\n')
  assert.equal(parser.hasPendingEvent, false)
})

test('unfinished blocks never dispatch and can complete in a later update', () => {
  for (const tail of ['data: tail', 'data: tail\n', 'event: x\nid: y\ndata: tail\r\n']) {
    const parser = new sse.SseParser()
    assert.equal(parser.append(tail).length, 0)
    assert.equal(parser.hasPendingEvent, true)
    const end = tail.endsWith('\n') ? '\n' : '\n\n'
    assert.equal(parser.append(end).length, 1)
    assert.equal(parser.hasPendingEvent, false)
  }
  assert.equal(parseChunks(['\ufeff']).parser.hasPendingEvent, false)
})

test('previews remain Unicode-safe and bounded; full payloads are materialized on demand', () => {
  const data = '😀'.repeat(1000) + '\n' + '中'.repeat(1024 * 1024)
  const body = 'data: ' + data.replaceAll('\n', '\ndata: ') + '\n\n'
  const { events } = parseChunks([body])
  assert.equal(Array.from(events[0].preview).length, 160)
  assert.equal(events[0].preview, '😀'.repeat(159) + '…')
  assert.equal('data' in events[0], false)
  assert.equal('raw' in events[0], false)
  assert.equal(sse.getSseMessageDetail(body, events[0]).data, data)
  assert.equal(parseChunks(['data: a\ndata: b\n\n']).events[0].preview, 'a ↵ b')
  assert.equal(parseChunks(['data: ' + 'x'.repeat(160) + '\n\n']).events[0].preview.length, 160)
})

test('append updates keep indices, replacements reset IDs, and inactive tabs catch up', async (t) => {
  const source = Vue.reactive({ body: 'id: old\ndata: first\n\n', bodyEncoding: '', active: true })
  const state = scope(t, () => useSseMessages(source))
  const first = state.messages.value[0]
  source.body += 'data: next\n\n'
  await Vue.nextTick()
  assert.equal(state.messages.value[0], first)
  assert.equal(state.messages.value[1].lastEventId, 'old')
  source.active = false
  await Vue.nextTick()
  source.body += 'data: third\n\n'
  await Vue.nextTick()
  assert.equal(state.messages.value.length, 2)
  source.active = true
  await Vue.nextTick()
  assert.equal(state.messages.value.length, 3)
  const revision = state.revision.value
  source.body = 'data: replacement\n\n'
  await Vue.nextTick()
  assert.equal(state.revision.value, revision + 1)
  assert.deepEqual(
    state.messages.value.map((item) => [item.sequence, item.lastEventId, item.preview]),
    [[1, '', 'replacement']],
  )
  source.body = ''
  await Vue.nextTick()
  assert.equal(state.messages.value.length, 0)
  assert.equal(state.pending.value, false)
})

test('encoding changes reset parsing and base64 is never interpreted as event text', async (t) => {
  const source = Vue.reactive({ body: 'data: encoded\n\n', bodyEncoding: 'base64', active: true })
  const state = scope(t, () => useSseMessages(source))
  assert.equal(state.messages.value.length, 0)
  source.bodyEncoding = ''
  await Vue.nextTick()
  assert.equal(state.messages.value.length, 1)
  source.bodyEncoding = 'base64'
  await Vue.nextTick()
  assert.equal(state.messages.value.length, 0)
})

test('ten thousand events parse in bounded asynchronous batches and stale work is canceled', async (t) => {
  const body = Array.from(
    { length: 10000 },
    (_, index) => `id: ${index}\ndata: ${'x'.repeat(60)}\n\n`,
  ).join('')
  const source = Vue.reactive({ body, bodyEncoding: '', active: true })
  const state = scope(t, () => useSseMessages(source))
  assert.equal(state.parsing.value, true)
  assert.ok(state.messages.value.length > 0 && state.messages.value.length < 10000)
  await until(() => !state.parsing.value)
  assert.equal(state.messages.value.length, 10000)
  source.body = body + 'data: pending'
  await Vue.nextTick()
  assert.equal(state.pending.value, true)
  source.body = body.replaceAll('data:', 'data: replacement')
  await Vue.nextTick()
  assert.equal(state.parsing.value, true)
  source.body = 'data: new\n\n'
  await Vue.nextTick()
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.deepEqual(
    state.messages.value.map((item) => item.preview),
    ['new'],
  )
})

test('disposing a parser cancels large-snapshot tasks', async () => {
  const effect = Vue.effectScope()
  const state = effect.run(() =>
    useSseMessages(
      Vue.reactive({
        body: 'data: x\n\n'.repeat(100000),
        bodyEncoding: '',
        active: true,
      }),
    ),
  )
  const count = state.messages.value.length
  effect.stop()
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(state.messages.value.length, count)
  assert.equal(state.parsing.value, false)
})

function setupComponent(t, kind, overrides = {}) {
  const filename = kind === 'body' ? 'BodyViewer' : 'SseMessageViewer'
  const file = readFileSync(
    new URL(`../src/components/traffic/${filename}.vue`, import.meta.url),
    'utf8',
  )
  const compiled = compileScript(parse(file, { filename: `${filename}.vue` }).descriptor, {
    id: 'sse-view-test',
  }).content
  const calls = { copies: [], saves: [], dialogs: [], errors: [] }
  let dialogResult = 'E:/tmp/sse-test.txt'
  const dependencies = {
    vue: { ...Vue, onMounted() {}, onUnmounted() {} },
    '@tanstack/vue-virtual': Virtual,
    'vue-i18n': { useI18n: () => ({ t: (key, args) => `${key}${args?.error ?? ''}` }) },
    '@wailsio/runtime': {
      Dialogs: {
        SaveFile: async (options) => {
          calls.dialogs.push(options)
          return await dialogResult
        },
      },
    },
    '@/composables/useSseMessages': { useSseMessages },
    '@/composables/useHexdumpViewState': {
      useHexdumpViewState: () => ({
        byteOffset: Vue.ref(0),
        gated: Vue.ref(false),
        canRender: Vue.ref(true),
        load() {},
      }),
    },
    '@/composables/useNotify': {
      useNotify: () => ({ success() {}, error: (message) => calls.errors.push(message) }),
    },
    '@/utils/sse': sse,
    '@/utils/dialog': dialog,
    '@/utils/clipboard': { copyText: async (body) => calls.copies.push(body) },
    '@/utils/hexdump': { estimateDecodedByteLength: (value) => value.length },
    '@/utils/format': { formatFileSize: (value) => String(value) },
    '@/components/common/emptyState': {},
    '@/components/common/monacoLargeText': largeText,
    '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice': {
      SaveBodyToFile: async (args) => calls.saves.push(args),
    },
  }
  const exports = {}
  new Function(
    'require',
    'exports',
    ts.transpileModule(compiled, {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
    }).outputText,
  )((id) => {
    if (id.endsWith('.vue')) return { __esModule: true, default: {} }
    if (dependencies[id]) return dependencies[id]
    throw new Error(`Unexpected dependency ${id}`)
  }, exports)
  const props = Vue.reactive({
    body: '',
    contentType: 'text/event-stream',
    bodyEncoding: '',
    enableSse: true,
    active: true,
    ...overrides,
  })
  const state = scope(t, () => exports.default.setup(props, { expose() {}, emit() {} }))
  return {
    props,
    state,
    calls,
    setDialog(value) {
      dialogResult = value
    },
  }
}

test('SSE defaults apply to empty responses, respect manual selection, and require opt-in', async (t) => {
  const { state, props } = setupComponent(t, 'body')
  assert.deepEqual(state.availableTabs.value, ['raw', 'sse', 'hex'])
  assert.equal(state.activeTab.value, 'sse')
  state.activeTab.value = 'raw'
  props.body = 'data: first\n\n'
  await Vue.nextTick()
  assert.equal(state.activeTab.value, 'raw')
  props.enableSse = false
  await Vue.nextTick()
  assert.deepEqual(state.availableTabs.value, ['raw', 'hex'])
  props.enableSse = true
  await Vue.nextTick()
  assert.equal(state.activeTab.value, 'sse')
  props.contentType = 'text/plain'
  await Vue.nextTick()
  assert.equal(state.activeTab.value, 'raw')
  props.contentType = 'TEXT/EVENT-STREAM; charset=utf-8'
  await Vue.nextTick()
  assert.equal(state.activeTab.value, 'sse')
})

test('response toolbar copies and saves the whole stream while message actions use the selected view', async (t) => {
  const body = 'id: 1\ndata: {"a":1}\n\ndata: second\n\n'
  const host = setupComponent(t, 'body', { body })
  await host.state.copyBodyContent()
  await host.state.saveCurrentBodyContent()
  assert.deepEqual(host.calls.copies, [body])
  assert.equal(host.calls.saves[0].body, body)
  const viewer = setupComponent(t, 'sse', { body })
  viewer.state.openDetail(viewer.state.messages.value[0])
  await Vue.nextTick()
  await viewer.state.copyMessage()
  await viewer.state.saveMessage()
  assert.equal(viewer.calls.copies[0], '{"a":1}')
  assert.equal(viewer.calls.saves[0].body, '{"a":1}')
  viewer.state.detailTab.value = 'raw'
  await viewer.state.copyMessage()
  assert.equal(viewer.calls.copies[1], 'id: 1\ndata: {"a":1}\n\n')
  viewer.state.detailTab.value = 'formatted'
  await viewer.state.saveMessage()
  assert.equal(viewer.calls.saves[1].body, '{\n  "a": 1\n}')
  assert.equal(viewer.calls.saves[1].contentType, 'application/json')
})

test('detail is stable during appends, retains effective IDs, and releases payloads on close or reset', async (t) => {
  const viewer = setupComponent(t, 'sse', { body: 'id: shared\n\ndata: selected\n\n' })
  viewer.state.openDetail(viewer.state.messages.value[0])
  assert.equal(viewer.state.selectedMessage.value.lastEventId, 'shared')
  viewer.props.body += 'data: newer\n\n'
  await Vue.nextTick()
  assert.equal(viewer.state.detailContent.value, 'selected')
  assert.equal(viewer.state.detailVisible.value, true)
  viewer.props.body = 'data: replacement\n\n'
  await Vue.nextTick()
  assert.equal(viewer.state.detailVisible.value, false)
  assert.equal(viewer.state.selectedMessage.value, null)
  viewer.state.openDetail(viewer.state.messages.value[0])
  viewer.props.active = false
  await Vue.nextTick()
  assert.equal(viewer.state.selectedMessage.value, null)
})

test('detail formatting is bounded and empty data remains copyable and saveable', async (t) => {
  const viewer = setupComponent(t, 'sse', { body: 'data:\n\n' })
  viewer.state.openDetail(viewer.state.messages.value[0])
  assert.equal(viewer.state.formattedData.value, null)
  await viewer.state.copyMessage()
  await viewer.state.saveMessage()
  assert.equal(viewer.calls.copies[0], '')
  assert.equal(viewer.calls.saves[0].body, '')
  viewer.props.body = 'data: "' + 'x'.repeat(largeText.MONACO_LARGE_TEXT_THRESHOLD_CHARS) + '"\n\n'
  await Vue.nextTick()
  await until(() => !viewer.state.parsing.value)
  viewer.state.openDetail(viewer.state.messages.value[0])
  assert.equal(viewer.state.formattedData.value, null)
})

test('message saves snapshot the selected view and guard concurrent dialogs and cancellation', async (t) => {
  const viewer = setupComponent(t, 'sse', { body: 'data: original\n\n' })
  viewer.state.openDetail(viewer.state.messages.value[0])
  let resolveDialog
  viewer.setDialog(
    new Promise((resolve) => {
      resolveDialog = resolve
    }),
  )
  const pendingSave = viewer.state.saveMessage()
  await viewer.state.saveMessage()
  assert.equal(viewer.calls.dialogs.length, 1)
  viewer.props.body = 'data: replacement\n\n'
  await Vue.nextTick()
  resolveDialog('E:/tmp/test.txt')
  await pendingSave
  assert.equal(viewer.calls.saves[0].body, 'original')
  viewer.state.openDetail(viewer.state.messages.value[0])
  viewer.setDialog(' ')
  await viewer.state.saveMessage()
  assert.equal(viewer.calls.saves.length, 1)
  viewer.setDialog(Promise.reject(new Error('cancelled by user')))
  await viewer.state.saveMessage()
  assert.deepEqual(viewer.calls.errors, [])
  assert.equal(viewer.state.exporting.value, false)
})

test('real virtualizer bounds visible rows and follow-tail respects user scrolling', async (t) => {
  const viewer = setupComponent(t, 'sse', { body: 'data: x\n\n'.repeat(10000) })
  await until(() => !viewer.state.parsing.value)
  assert.equal(viewer.state.messages.value.length, 10000)
  assert.ok(viewer.state.virtualRows.value.length < 40)
  const element = Vue.markRaw({
    scrollHeight: 440000,
    scrollTop: 0,
    clientHeight: 480,
    scrollLeft: 0,
  })
  viewer.state.scrollRef.value = element
  viewer.state.onScroll({ target: element })
  viewer.props.body += 'data: paused\n\n'
  await Vue.nextTick()
  await Vue.nextTick()
  assert.equal(element.scrollTop, 0)
  viewer.state.jumpToLatest()
  assert.equal(element.scrollTop, element.scrollHeight)
  viewer.props.body += 'data: following\n\n'
  element.scrollHeight += 44
  await Vue.nextTick()
  await Vue.nextTick()
  assert.equal(element.scrollTop, element.scrollHeight)
})

test('new bilingual copy has matching keys and placeholders', () => {
  const zh = JSON.parse(
    readFileSync(new URL('../src/locales/zh.json', import.meta.url), 'utf8').replace(/^\ufeff/, ''),
  ).detail.sse
  const en = JSON.parse(
    readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8').replace(/^\ufeff/, ''),
  ).detail.sse
  assert.deepEqual(Object.keys(zh), Object.keys(en))
  for (const key of Object.keys(zh)) {
    assert.deepEqual(zh[key].match(/\{[^}]+\}/g), en[key].match(/\{[^}]+\}/g), key)
  }
})
