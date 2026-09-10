import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { Worker } from 'node:worker_threads'
import * as Vue from 'vue'
import * as I18n from 'vue-i18n'
import { effectScope, nextTick, reactive } from 'vue'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createServer } from 'vite'
import ts from 'typescript'

const root = fileURLToPath(new URL('..', import.meta.url))
let server, utils, viewport, useHexdumpDecoder, useHexdumpViewState
before(async () => {
  server = await createServer({
    configFile: false,
    root,
    server: { middlewareMode: true },
    optimizeDeps: { noDiscovery: true },
  })
  utils = await server.ssrLoadModule('/src/utils/hexdump.ts')
  viewport = await server.ssrLoadModule('/src/utils/hexdumpViewport.ts')
  ;({ useHexdumpDecoder } = await server.ssrLoadModule('/src/composables/useHexdumpDecoder.ts'))
  ;({ useHexdumpViewState } = await server.ssrLoadModule('/src/composables/useHexdumpViewState.ts'))
})
after(async () => {
  await server?.close()
})

function scoped(t, fn) {
  const scope = effectScope()
  t.after(() => scope.stop())
  return scope.run(fn)
}

async function until(predicate, timeout = 5000) {
  const deadline = Date.now() + timeout
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error('Timed out waiting for decoder')
    await new Promise((resolve) => setTimeout(resolve, 5))
  }
}

function source(input, overrides = {}) {
  return reactive({ input, isBase64: false, active: true, appendOnly: false, ...overrides })
}

class FakeWorker {
  messages = []
  terminated = false
  onmessage = null
  onerror = null
  postMessage(message) {
    this.messages.push(message)
  }
  terminate() {
    this.terminated = true
  }
  finish(bytes) {
    this.onmessage({ data: { id: this.messages[0].id, ok: true, buffer: bytes.buffer } })
  }
}

function transpile(path) {
  return ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText
}

// Run the real Web Worker handler in a separate Node thread; only the transport
// adapter changes. This checks chunk boundaries without relying on a browser shim.
const workerScript = `
const { parentPort } = require('node:worker_threads');
const util = (() => { const exports = {}; ${transpile('../src/utils/hexdump.ts')} return exports; })();
globalThis.self = { postMessage: (message, transfer) => parentPort.postMessage(message, transfer) };
((require, exports) => { ${transpile('../src/workers/hexdumpDecoder.worker.ts')} })(() => util, {});
parentPort.on('message', data => self.onmessage({ data }));
`

class RealWorker {
  onmessage = null
  onerror = null
  maxChunkChars = 0
  totalChars = 0
  maxPostMs = 0
  constructor() {
    this.worker = new Worker(workerScript, { eval: true })
    this.worker.on('message', (data) => this.onmessage?.({ data }))
    this.worker.on('error', (error) =>
      this.onerror?.({ message: error.message, preventDefault() {} }),
    )
  }
  postMessage(message) {
    const start = performance.now()
    this.worker.postMessage(message)
    this.maxPostMs = Math.max(this.maxPostMs, performance.now() - start)
    if (message.type === 'chunk') {
      this.maxChunkChars = Math.max(this.maxChunkChars, message.input.length)
      this.totalChars += message.input.length
    }
  }
  terminate() {
    void this.worker.terminate()
  }
}

test('UTF-8 size estimates are exact for bounded text and conservative for large text', () => {
  for (const input of ['', 'ASCII', '中文', '😀', '\ud800', '\udc00', 'a😀中\ud800']) {
    assert.equal(utils.estimateDecodedByteLength(input), new TextEncoder().encode(input).length)
  }
  for (const input of ['x'.repeat(400000), '中'.repeat(400000), '😀'.repeat(100000)]) {
    assert.ok(utils.estimateDecodedByteLength(input) >= new TextEncoder().encode(input).length)
  }
  assert.ok(utils.estimateDecodedByteLength('中'.repeat(100000)) >= 256 * 1024)
  for (let length = 0; length < 20; length++) {
    const data = Buffer.alloc(length, 255)
    assert.equal(utils.estimateDecodedByteLength(data.toString('base64'), true), length)
  }
})

test('large size checks never encode or scan the complete string', (t) => {
  let scans = 0
  const original = String.prototype.charCodeAt
  t.mock.method(String.prototype, 'charCodeAt', function (...args) {
    scans++
    return original.apply(this, args)
  })
  const input = '中'.repeat(10 * 1024 * 1024)
  for (let i = 0; i < 100; i++) utils.estimateDecodedByteLength(input)
  assert.equal(scans, 0)
})

test('byte storage preserves binary values and tiny appends across storage boundaries', () => {
  const store = new utils.HexdumpBytes()
  const prefix = Uint8Array.from({ length: 65536 }, (_, i) => i & 255)
  store.append(prefix)
  for (let i = 0; i < 70000; i++) store.append(Uint8Array.of(i & 255))
  assert.equal(store.length, 135536)
  for (const i of [0, 255, 65535, 65536, 65537, 131071, 135535]) {
    assert.equal(store.get(i), i & 255)
  }
  assert.equal(store.get(-1), undefined)
  assert.equal(store.get(store.length), undefined)
})

test('huge bodies have contiguous bounded pages and a reachable final byte at every layout width', () => {
  const length = 1024 ** 4 + 7 // geometry only; no giant allocation
  for (let columns = 1; columns <= 64; columns++) {
    for (const rowHeight of [22, 24]) {
      const first = viewport.getHexdumpPage(length, columns, rowHeight, 0)
      const second = viewport.getHexdumpPage(length, columns, rowHeight, 1)
      assert.equal(first.startRow + first.rowCount, second.startRow)
      const position = viewport.getHexdumpPosition(length - 1, length, columns, rowHeight)
      const last = viewport.getHexdumpPage(length, columns, rowHeight, position.page)
      assert.equal(last.index, last.count - 1)
      assert.ok(last.height <= viewport.HEX_MAX_PAGE_HEIGHT)
      assert.ok(first.height <= viewport.HEX_MAX_PAGE_HEIGHT)
      assert.equal(last.startRow + last.rowCount, Math.ceil(length / columns))
      assert.ok(position.top < last.height)
    }
  }
})

test('byte positions survive a change in row width and page boundaries', () => {
  for (const offset of [0, 10000, 31_234_567, 100 * 1024 * 1024 - 1]) {
    for (const columns of [1, 16, 21, 64]) {
      const position = viewport.getHexdumpPosition(offset, 100 * 1024 * 1024, columns, 22)
      const page = viewport.getHexdumpPage(100 * 1024 * 1024, columns, 22, position.page)
      const row =
        page.startRow + Math.floor(Math.max(0, position.top - viewport.HEX_PADDING_TOP) / 22)
      assert.ok(row * columns <= offset && (row + 1) * columns > offset)
    }
  }
})

test('large SSE load approval and byte offset survive appends and tab switches', async (t) => {
  const body = reactive({
    body: '中'.repeat(400000),
    contentType: 'text/event-stream',
    bodyEncoding: '',
    active: true,
  })
  const view = scoped(t, () => useHexdumpViewState(body))
  assert.equal(view.gated.value, true)
  view.load()
  view.byteOffset.value = 123456
  body.body += '\ndata: hello\n\n'
  await nextTick()
  assert.equal(view.canRender.value, true)
  assert.equal(view.byteOffset.value, 123456)
  body.active = false
  await nextTick()
  assert.equal(view.canRender.value, false)
  body.body += '\ndata: hidden\n\n'
  await nextTick()
  body.active = true
  await nextTick()
  assert.equal(view.canRender.value, true)
  assert.equal(view.byteOffset.value, 123456)
  body.body = 'reset'
  await nextTick()
  assert.equal(view.byteOffset.value, 0)
})

test('an already open stream stays open when it crosses the load threshold', async (t) => {
  const body = reactive({ body: 'small', contentType: 'text/event-stream', active: true })
  const view = scoped(t, () => useHexdumpViewState(body))
  assert.equal(view.canRender.value, true)
  body.body += 'x'.repeat(2 * 1024 * 1024)
  await nextTick()
  assert.equal(view.canRender.value, true)
})

test('SSE updates during decoding are coalesced, then only the suffix is decoded', async (t) => {
  const input = '中'.repeat(100000)
  const data = source(input, { appendOnly: true })
  const workers = []
  const decoder = scoped(t, () =>
    useHexdumpDecoder(data, () => {
      const worker = new FakeWorker()
      workers.push(worker)
      return worker
    }),
  )
  assert.equal(workers.length, 1)
  data.input += '\ndata: one\n\n'
  await nextTick()
  data.input += 'data: 😀\n\n'
  await nextTick()
  assert.equal(workers.length, 1)
  workers[0].finish(new TextEncoder().encode(input))
  const originalStore = decoder.bytes.value
  await until(() => decoder.bytes.value.length === Buffer.byteLength(data.input))
  assert.equal(decoder.bytes.value, originalStore)
  assert.equal(workers.length, 1, 'small suffix must not re-decode the full large body')
  assert.equal(decoder.bytes.value.get(decoder.bytes.value.length - 1), 10)
})

test('inactive viewers terminate workers, discard buffers and ignore late results', async (t) => {
  const data = source('x'.repeat(1024 * 1024))
  const workers = []
  const decoder = scoped(t, () =>
    useHexdumpDecoder(data, () => {
      const worker = new FakeWorker()
      workers.push(worker)
      return worker
    }),
  )
  const stale = workers[0]
  data.active = false
  await nextTick()
  assert.equal(stale.terminated, true)
  stale.finish(Uint8Array.of(99))
  assert.equal(decoder.bytes.value.length, 0)
  data.input += 'hidden'
  await nextTick()
  assert.equal(workers.length, 1)
  data.active = true
  await nextTick()
  assert.equal(workers.length, 2)
})

test('a failed worker never falls back to a large main-thread encoding', (t) => {
  let encodes = 0
  t.mock.method(TextEncoder.prototype, 'encode', () => {
    encodes++
    throw new Error('Unexpected UI encode')
  })
  const data = source('中'.repeat(400000))
  const decoder = scoped(t, () =>
    useHexdumpDecoder(data, () => {
      throw new Error('Worker unavailable')
    }),
  )
  assert.equal(decoder.state.value, 'error')
  assert.equal(decoder.error.value, 'Worker unavailable')
  assert.equal(encodes, 0)
})

test('worker chunk boundaries preserve Unicode, Base64 padding and invalid-input errors', async (t) => {
  const worker = new RealWorker()
  t.after(() => worker.terminate())
  let id = 0
  async function decode(input, isBase64, chunkSize) {
    id++
    const response = new Promise((resolve) => {
      worker.onmessage = (event) => resolve(event.data)
    })
    worker.postMessage({ id, type: 'start', isBase64 })
    for (let i = 0; i < input.length; i += chunkSize)
      worker.postMessage({ id, type: 'chunk', input: input.slice(i, i + chunkSize) })
    worker.postMessage({ id, type: 'end' })
    return response
  }
  for (const input of ['a😀中\ud800', '\ud800\ud800\udc00']) {
    const response = await decode(input, false, 1)
    assert.equal(response.ok, true)
    assert.deepEqual(Buffer.from(response.buffer), Buffer.from(input))
  }
  const binary = Buffer.from(Array.from({ length: 257 }, (_, i) => i & 255))
  for (const encoded of [
    binary.toString('base64'),
    binary.toString('base64').replace(/=/g, ''),
    binary.toString('base64').replace(/.{5}/g, '$&\n'),
  ]) {
    const response = await decode(encoded, true, 3)
    assert.equal(response.ok, true)
    assert.deepEqual(Buffer.from(response.buffer), binary)
  }
  for (const invalid of ['A', 'YQ==Yg==', '!!!!'])
    assert.equal((await decode(invalid, true, 4)).ok, false)
})

test('100 MiB Base64 stays off the UI thread and uses bounded messages', async (t) => {
  const binary = Buffer.alloc(100 * 1024 * 1024, 0xa5)
  binary[binary.length - 1] = 0xff
  const input = binary.toString('base64')
  const data = source(input, { isBase64: true })
  let worker
  let uiEncodes = 0
  t.mock.method(TextEncoder.prototype, 'encode', () => {
    uiEncodes++
    throw new Error('UI encode')
  })
  const start = performance.now()
  let previousTick = start
  let maxTimerGap = 0
  const interval = setInterval(() => {
    const now = performance.now()
    maxTimerGap = Math.max(maxTimerGap, now - previousTick)
    previousTick = now
  }, 5)
  t.after(() => clearInterval(interval))
  const decoder = scoped(t, () =>
    useHexdumpDecoder(data, () => {
      worker = new RealWorker()
      return worker
    }),
  )
  await until(() => decoder.state.value === 'ready' || decoder.state.value === 'error', 30000)
  assert.equal(decoder.state.value, 'ready', decoder.error.value)
  assert.equal(decoder.bytes.value.length, binary.length)
  assert.equal(decoder.bytes.value.get(0), 0xa5)
  assert.equal(decoder.bytes.value.get(binary.length - 1), 0xff)
  assert.equal(worker.totalChars, input.length)
  assert.ok(worker.maxChunkChars <= 128 * 1024)
  assert.equal(uiEncodes, 0)
  t.diagnostic(
    `100 MiB: ${(performance.now() - start).toFixed(0)} ms total; largest postMessage ${worker.maxPostMs.toFixed(2)} ms; max 5 ms timer gap ${maxTimerGap.toFixed(2)} ms; ${worker.maxChunkChars} characters/message (Node worker transport, not desktop FPS)`,
  )
  data.active = false
  await nextTick()
  assert.equal(decoder.bytes.value.length, 0)
})

async function mountViewer(t, input, options = {}) {
  function compileComponent(name) {
    const file = readFileSync(new URL(`../src/components/common/${name}.vue`, import.meta.url), 'utf8')
    const compiled = compileScript(parse(file, { filename: `${name}.vue` }).descriptor, {
      id: 'hex-view-test',
      inlineTemplate: true,
    }).content
    const code = ts.transpileModule(
      compiled.replaceAll('import.meta.url', JSON.stringify(import.meta.url)),
      { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } },
    ).outputText
    const exports = {}
    new Function('require', 'exports', 'ResizeObserver', 'IntersectionObserver', code)(
      requireModule, exports, ResizeObserverStub, VisibilityObserverStub,
    )
    return exports.default
  }
  const i18n = I18n.createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url))) },
  })
  const requireModule = (id) => {
    if (id === 'vue') return Vue
    if (id === 'vue-i18n') return I18n
    if (id.endsWith('/useHexdumpDecoder')) return { useHexdumpDecoder }
    if (id.endsWith('/hexdumpViewport')) return viewport
    if (id.endsWith('/hexdump')) return utils
    if (id.endsWith('/HexDumpRow.vue')) return { __esModule: true, default: compileComponent('HexDumpRow') }
    if (id.endsWith('/AppLoading.vue')) return { __esModule: true, default: { render: () => Vue.h('div') } }
    if (id.endsWith('/emptyState')) return {}
    throw new Error(`Unexpected import ${id}`)
  }
  const resizeObservers = new Set()
  class ResizeObserverStub {
    constructor(callback) { this.callback = callback }
    observe() { resizeObservers.add(this) }
    disconnect() { resizeObservers.delete(this) }
  }
  let visibilityCallback
  class VisibilityObserverStub {
    constructor(callback) { visibilityCallback = callback }
    observe() {}
    disconnect() {}
  }
  const component = compileComponent('HexDumpViewer')
  let width = 720
  const stats = { byteReads: 0, updatedRows: [] }
  const readByte = utils.HexdumpBytes.prototype.get
  t.mock.method(utils.HexdumpBytes.prototype, 'get', function (index) {
    stats.byteReads++
    return readByte.call(this, index)
  })
  function node(tag, text = '') {
    return Vue.markRaw({
      tag,
      text,
      props: {},
      children: [],
      parent: null,
      scrollTop: 0,
      get dataset() { return { byteIdx: this.props['data-byte-idx'] } },
      contains(target) {
        for (let el = target; el; el = el.parent) if (el === this) return true
        return false
      },
      closest(selector) {
        assert.equal(selector, '[data-byte-idx]')
        if (this.props['data-byte-idx'] != null) return this
        return this.parent?.closest(selector) ?? null
      },
      get clientWidth() {
        return width
      },
      clientHeight: 500,
      get scrollHeight() {
        return (
          parseFloat(
            this.children.find((child) => child.props.style?.height)?.props.style.height,
          ) || 500
        )
      },
      getBoundingClientRect() {
        return { width: 84 }
      },
      scrollTo({ top }) {
        this.scrollTop = Math.min(top, Math.max(0, this.scrollHeight - this.clientHeight))
      },
    })
  }
  const renderer = Vue.createRenderer({
    createElement: node,
    createText: (text) => node('#text', text),
    createComment: (text) => node('#comment', text),
    setText(el, text) {
      el.text = text
    },
    setElementText(el, text) {
      el.text = text
      el.children = []
    },
    patchProp(el, key, _old, value) {
      el.props[key] = value
    },
    parentNode: (el) => el.parent,
    nextSibling(el) {
      return el.parent?.children[el.parent.children.indexOf(el) + 1] ?? null
    },
    insert(el, parent, anchor) {
      if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
      el.parent = parent
      const index = anchor ? parent.children.indexOf(anchor) : -1
      if (index < 0) parent.children.push(el)
      else parent.children.splice(index, 0, el)
    },
    remove(el) {
      if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
      el.parent = null
    },
  })
  const visible = Vue.ref(true)
  const offset = Vue.ref(0)
  const currentInput = Vue.shallowRef(input)
  const viewerProps = Vue.reactive(options)
  const container = node('root')
  const app = renderer.createApp({
    render: () =>
      visible.value
        ? Vue.h(component, {
            ...viewerProps,
            input: currentInput.value,
            byteOffset: offset.value,
            'onUpdate:byteOffset': (value) => {
              offset.value = value
            },
          })
        : Vue.h('div'),
  })
  app.use(i18n)
  app.config.warnHandler = (message) => { throw new Error(message) }
  app.mixin({
    beforeUpdate() {
      if (this.$options.__name === 'HexDumpRow') stats.updatedRows.push(this.$props.line.offsetHex)
    },
  })
  app.component('UButton', {
    render() {
      return Vue.h('button', this.$attrs, this.$slots.default?.())
    },
  })
  app.component('UEmpty', { render: () => Vue.h('div') })
  app.mount(container)
  t.after(() => app.unmount())
  async function flush() {
    for (let i = 0; i < 8; i++) await nextTick()
  }
  function find(predicate, parent = container) {
    if (predicate(parent)) return parent
    for (const child of parent.children) {
      const found = find(predicate, child)
      if (found) return found
    }
  }
  function findAll(predicate, parent = container) {
    return [ ...(predicate(parent) ? [parent] : []), ...parent.children.flatMap(child => findAll(predicate, child)) ]
  }
  await flush()
  return {
    currentInput, viewerProps, visible, offset, stats, find, findAll, flush,
    setWidth(value) { width = value },
    resize() { for (const observer of [...resizeObservers]) observer.callback() },
    setVisible(value) {
      visibilityCallback([{ isIntersecting: value, boundingClientRect: { width: value ? width : 0, height: value ? 500 : 0 } }])
    },
    async scroll(top) {
      const scroller = find(el => el.props.onScroll)
      scroller.scrollTop = top
      scroller.props.onScroll({ currentTarget: scroller })
      await flush()
    },
    async hover(index) {
      const scroller = find(el => el.props.onScroll)
      const target = find(el => el.props['data-byte-idx'] === index)
      assert.ok(target, `byte ${index} is rendered`)
      scroller.props.onMouseover({ target })
      await flush()
    },
  }
}

test('mounted viewer restores its byte model after unmount and reaches the last segment', async (t) => {
  const input = new Uint8Array(20 * 1024 * 1024 + 7)
  const { find, flush, offset, visible, currentInput, setWidth, setVisible } = await mountViewer(t, input)
  let scroller = find((el) => el.props.onScroll)
  scroller.scrollTop = 25000
  scroller.props.onScroll({ currentTarget: scroller })
  const saved = offset.value
  assert.ok(saved > 0)
  visible.value = false
  await flush()
  setWidth(480)
  visible.value = true
  await flush()
  scroller = find((el) => el.props.onScroll)
  assert.ok(scroller.scrollTop > 0)
  assert.equal(offset.value, saved)
  assert.ok(
    find((el) => el.props['data-byte-idx'] === saved),
    'saved byte must be rendered after reflow',
  )
  setVisible(false)
  await flush()
  assert.equal(find((el) => el.props['data-byte-idx'] !== undefined), undefined)
  setVisible(true)
  await flush()
  assert.equal(offset.value, saved)
  assert.ok(find((el) => el.props['data-byte-idx'] === saved), 'outer v-show must restore the saved byte')
  const last = find((el) => el.props['aria-label'] === 'Last segment')
  assert.ok(last)
  last.props.onClick()
  await flush()
  scroller = find((el) => el.props.onScroll)
  assert.ok(scroller.scrollHeight <= viewport.HEX_MAX_PAGE_HEIGHT)
  scroller.scrollTop = scroller.scrollHeight - scroller.clientHeight
  scroller.props.onScroll({ currentTarget: scroller })
  await flush()
  assert.ok(
    find((el) => el.props['data-byte-idx'] === input.length - 1),
    'last byte must be rendered',
  )
  currentInput.value = Uint8Array.of(1, 2, 3)
  await flush()
  assert.equal(offset.value, 0, 'another message must not inherit the old reading position')
  assert.ok(find(el => el.props['data-byte-idx'] === 0))
})

test('mounted scrolling reuses overlapping rows and keeps the row cache bounded', async (t) => {
  const view = await mountViewer(t, new Uint8Array(20 * 1024 * 1024))
  const byteNodes = () => view.findAll(el => el.props['data-byte-idx'] != null)
  const firstByte = byteNodes()[0]
  const columns = firstByte.parent.children.filter(el => el.props['data-byte-idx'] != null).length
  view.stats.byteReads = 0
  view.stats.updatedRows.length = 0
  for (const top of [1, 2, 3, 4, 5]) await view.scroll(top)
  assert.equal(view.stats.byteReads, 0, 'pixel scrolling within one row window must not reformat bytes')
  assert.equal(view.stats.updatedRows.length, 0, 'unchanged byte rows must not render again')
  assert.equal(byteNodes()[0], firstByte)

  await view.scroll(5000)
  const oldBytes = new Map(byteNodes().map(el => [el.props['data-byte-idx'], el]))
  view.stats.byteReads = 0
  view.stats.updatedRows.length = 0
  await view.scroll(5022)
  assert.equal(view.stats.byteReads, columns, 'one row of scrolling must only format the entering row')
  assert.equal(view.stats.updatedRows.length, 0, 'overlapping row components must keep their render')
  // Each byte occurs in the hex and character columns; compare the character nodes.
  for (const [index, el] of new Map(byteNodes().map(el => [el.props['data-byte-idx'], el]))) {
    if (oldBytes.has(index)) assert.equal(el, oldBytes.get(index))
  }

  const initialWindowSize = byteNodes().length
  for (const top of [50000, 500000, 3000000, 5000]) {
    view.stats.byteReads = 0
    await view.scroll(top)
    assert.ok(byteNodes().length <= 2 * columns * (Math.ceil(500 / 22) + 21))
    assert.ok(view.stats.byteReads <= columns * (Math.ceil(500 / 22) + 21))
    assert.ok(view.stats.byteReads >= initialWindowSize / 2,
      'distant rows must be evicted, including when returning to a previously visited window')
  }
  // Avoid using body length as a row-cache capacity or materializing off-screen rows.
  assert.ok(byteNodes().length < 6000)
})

test('mounted hover updates only the affected rows and preserves both byte highlights', async (t) => {
  const view = await mountViewer(t, new Uint8Array(10000).fill(65))
  const firstByte = view.find(el => el.props['data-byte-idx'] === 0)
  const columns = firstByte.parent.children.filter(el => el.props['data-byte-idx'] != null).length
  const highlighted = () => view.findAll(el =>
    el.props['data-byte-idx'] != null && el.props.class?.includes('text-app-accent'))
  view.stats.updatedRows.length = 0
  await view.hover(0)
  assert.equal(view.stats.updatedRows.length, 1)
  assert.deepEqual(highlighted().map(el => el.props['data-byte-idx']), [0, 0])
  view.stats.updatedRows.length = 0
  await view.hover(1)
  assert.equal(view.stats.updatedRows.length, 1, 'moving within a row must not update other rows')
  assert.deepEqual(highlighted().map(el => el.props['data-byte-idx']), [1, 1])
  view.stats.updatedRows.length = 0
  await view.hover(columns)
  assert.equal(view.stats.updatedRows.length, 2, 'only the old and new hovered rows may update')
  assert.deepEqual(highlighted().map(el => el.props['data-byte-idx']), [columns, columns])
  view.stats.updatedRows.length = 0
  await view.scroll(1)
  assert.equal(view.stats.updatedRows.length, 1)
  assert.equal(highlighted().length, 0, 'scrolling clears both highlights')
})

test('mounted row reuse refreshes appended tails, replacements, and resized layouts', async (t) => {
  const view = await mountViewer(t, 'A'.repeat(25), { appendOnly: true })
  const firstByte = view.find(el => el.props['data-byte-idx'] === 0)
  const columns = firstByte.parent.children.filter(el => el.props['data-byte-idx'] != null).length
  view.stats.updatedRows.length = 0
  view.currentInput.value += 'Z'.repeat(12)
  await until(() => view.find(el => el.props['data-byte-idx'] === 36))
  await view.flush()
  assert.equal(view.find(el => el.props['data-byte-idx'] === 36).text, '5a')
  assert.equal(view.find(el => el.props['data-byte-idx'] === 0), firstByte)
  assert.deepEqual(view.stats.updatedRows, [columns.toString(16).padStart(8, '0')],
    'only the previously padded row must update after an append')

  view.currentInput.value = 'B'.repeat(37)
  await view.flush()
  assert.equal(view.find(el => el.props['data-byte-idx'] === 0).text, '42')
  assert.equal(view.find(el => el.props['data-byte-idx'] === 36).text, '42',
    'same-length replacement must invalidate rows from the previous decode')

  view.setWidth(480)
  view.resize()
  await view.flush()
  const narrowerByte = view.find(el => el.props['data-byte-idx'] === 0)
  const narrowerColumns = narrowerByte.parent.children.filter(el => el.props['data-byte-idx'] != null).length
  assert.ok(narrowerColumns < columns)
  assert.equal(view.find(el => el.props['data-byte-idx'] === 36).text, '42')
  view.viewerProps.rowHeight = 30
  await view.flush()
  assert.equal(view.find(el => el.props['data-byte-idx'] === narrowerColumns).parent.parent.props.style.height, '30px')
  assert.equal(view.find(el => el.props['data-byte-idx'] === narrowerColumns).parent.parent.props.style.transform,
    `translateY(${viewport.HEX_PADDING_TOP + 30}px)`)
})
