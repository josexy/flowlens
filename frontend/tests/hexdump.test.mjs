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
let layoutUtils, selectionUtils
let dialogUtils, monacoLargeText
before(async () => {
  server = await createServer({
    configFile: false,
    root,
    server: { middlewareMode: true },
    optimizeDeps: { noDiscovery: true },
  })
  utils = await server.ssrLoadModule('/src/utils/hexdump.ts')
  viewport = await server.ssrLoadModule('/src/utils/hexdumpViewport.ts')
  layoutUtils = await server.ssrLoadModule('/src/utils/hexdumpLayout.ts')
  selectionUtils = await server.ssrLoadModule('/src/utils/hexdumpSelection.ts')
  dialogUtils = await server.ssrLoadModule('/src/utils/dialog.ts')
  monacoLargeText = await server.ssrLoadModule('/src/components/common/monacoLargeText.ts')
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
  constructor() {
    this.worker = new Worker(workerScript, { eval: true })
    this.worker.on('message', (data) => this.onmessage?.({ data }))
    this.worker.on('error', (error) =>
      this.onerror?.({ message: error.message, preventDefault() {} }),
    )
  }
  postMessage(message) {
    this.worker.postMessage(message)
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

async function mountViewer(t, input, options = {}) {
  const documentStub = {
    hidden: false, activeElement: null,
    addEventListener() {}, removeEventListener() {},
  }
  const frames = new Map()
  let frameId = 0
  const feedback = { copied: [], success: [], error: [], exports: [] }
  let copyError = null
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
    new Function('require', 'exports', 'ResizeObserver', 'IntersectionObserver', 'document', 'requestAnimationFrame', 'cancelAnimationFrame', code)(
      requireModule, exports, ResizeObserverStub, VisibilityObserverStub, documentStub,
      (callback) => { frames.set(++frameId, callback); return frameId },
      (id) => frames.delete(id),
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
    if (id.endsWith('/hexdumpLayout')) return layoutUtils
    if (id.endsWith('/hexdumpSelection')) return selectionUtils
    if (id.endsWith('/hexdump')) return utils
    if (id.endsWith('/clipboard')) return { copyText: async (content) => {
      if (copyError) throw copyError
      // Wails on Windows silently resolves after its native clipboard rejects NUL.
      if (content.includes('\u0000')) return
      feedback.copied.push(content)
    } }
    if (id.endsWith('/dialog')) return { getErrorMessage: (error) => error.message }
    if (id.endsWith('/useNotify')) return { useNotify: () => ({
      success: (message) => feedback.success.push(message), error: (message) => feedback.error.push(message),
    }) }
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
      scrollLeft: 0,
      get dataset() { return { byteIdx: this.props['data-byte-idx'], byteColumn: this.props['data-byte-column'] } },
      focus() { documentStub.activeElement = this },
      setPointerCapture(id) { this.pointerCapture = id },
      hasPointerCapture(id) { return this.pointerCapture === id },
      releasePointerCapture() { this.pointerCapture = null },
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
        return { width: this.props['aria-hidden'] === 'true' ? 84 : width, left: 0, top: 0, bottom: 500, height: 500 }
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
            onExportSelection: (base64) => feedback.exports.push(base64),
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
  app.component('UContextMenu', {
    props: ['items', 'content'],
    inheritAttrs: false,
    render() { return Vue.cloneVNode(this.$slots.default()[0], { ...this.$attrs, 'data-menu-items': this.items, 'data-menu-content': this.content }) },
  })
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
  function pointer(index, column = 'hex', overrides = {}) {
    const scroller = find(el => el.props.onScroll)
    const byte = find(el => el.props['data-byte-idx'] === index && el.props['data-byte-column'] === column)
    const layout = layoutUtils.getHexdumpLayout(width, 8.4, currentInput.value.length, scroller.scrollHeight > 500 ? 14 : 10)
    const row = Math.floor(index / layout.bytesPerRow)
    const page = viewport.getHexdumpPosition(index, currentInput.value.length, layout.bytesPerRow, viewerProps.rowHeight ?? 22).page
    const startRow = viewport.getHexdumpPage(currentInput.value.length, layout.bytesPerRow, viewerProps.rowHeight ?? 22, page).startRow
    return {
      target: byte ?? scroller, currentTarget: scroller, button: 0, buttons: 1, pointerId: 1, shiftKey: false,
      clientX: 8 + (column === 'hex' ? layout.hexStart : layout.asciiStart) + ((index % layout.bytesPerRow) + 0.5) * 8.4 * (column === 'hex' ? 3 : 1),
      clientY: viewport.HEX_PADDING_TOP + (row - startRow + 0.5) * (viewerProps.rowHeight ?? 22) - scroller.scrollTop,
      preventDefault() {}, ...overrides,
    }
  }
  return {
    currentInput, viewerProps, visible, offset, stats, find, findAll, flush, feedback, pointer,
    selected: () => findAll(el => el.props['data-selected']).map(el => el.props['data-byte-idx']),
    menu: () => find(el => el.props['data-menu-items'])?.props['data-menu-items'],
    failCopy: (error) => { copyError = error },
    async select(index, column = 'hex', shiftKey = false) {
      const scroller = find(el => el.props.onScroll)
      const event = pointer(index, column, { shiftKey })
      scroller.props.onPointerdown(event)
      scroller.props.onPointerup({ ...event, buttons: 0 })
      await flush()
    },
    async drag(start, end, column = 'hex', endOverrides = {}) {
      const scroller = find(el => el.props.onScroll)
      scroller.props.onPointerdown(pointer(start, column))
      scroller.props.onPointermove(pointer(end, column, endOverrides))
      await flush()
      return () => scroller.props.onPointerup(pointer(end, column, { ...endOverrides, buttons: 0 }))
    },
    async frame() {
      const pending = [...frames.values()]
      frames.clear()
      pending.forEach(callback => callback())
      await flush()
    },
    clear() { const scroller = find(el => el.props.onScroll); scroller.props.onPointerdown(pointer(0, 'hex', { target: scroller })) },
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
  assert.equal(view.find(el => el.props['data-byte-idx'] === 36).text, '5A')
  assert.equal(view.find(el => el.props['data-byte-idx'] === 0), firstByte)
  assert.deepEqual(view.stats.updatedRows, [columns.toString(16).toUpperCase().padStart(5, '0')],
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

test('hex layout accounts for offset digits, cell spacing and all column borders', () => {
  for (const width of [160, 320, 480, 720, 1100, 4000]) {
    for (const length of [3, 0x100000, 100 * 1024 * 1024]) {
      const layout = layoutUtils.getHexdumpLayout(width, 8.4, length, 14)
      assert.ok(layout.bytesPerRow >= 1 && layout.bytesPerRow <= 64)
      assert.equal(layout.offsetDigits, Math.max(5, (length - 1).toString(16).length))
      assert.ok(layout.asciiStart + layout.bytesPerRow * 8.4 <= width - 14 - 16)
      assert.equal(layoutUtils.getHexdumpPointerByte(layout.hexStart + 4 * 3 * 8.4 + 1, 22, 'hex', layout, 8.4, 22, 0, 3, length),
        Math.min(length - 1, layout.bytesPerRow + Math.min(4, layout.bytesPerRow - 1)))
    }
  }
  assert.equal(layoutUtils.getHexdumpLayout(80, 8.4, 100 * 1024 * 1024, 14).bytesPerRow, 1)
})

test('selection serialization preserves exact binary ranges and Base64 chunk boundaries', async () => {
  const bytes = Uint8Array.from({ length: 100003 }, (_, index) => index % 256)
  const store = new utils.HexdumpBytes()
  store.append(bytes.subarray(0, 13))
  store.append(bytes.subarray(13, 65537))
  store.append(bytes.subarray(65537))
  const selected = Buffer.from(bytes.subarray(11, 100001))
  const controller = new AbortController()
  assert.equal(await selectionUtils.serializeHexdumpSelection(store, 11, 100001, 'base64', controller.signal), selected.toString('base64'))
  assert.equal(await selectionUtils.serializeHexdumpSelection(store, 11, 100001, 'hex', controller.signal), selected.toString('hex').match(/../g).join(' ').toUpperCase())
  const sample = new utils.HexdumpBytes()
  sample.append(Uint8Array.of(0, 10, 31, 32, 65, 126, 127, 255))
  assert.equal(await selectionUtils.serializeHexdumpSelection(sample, 0, 8, 'text', controller.signal), '\u0000\n\u001f A~\u007f\u00ff')
  assert.ok([...store.range(11, 100001)].every(chunk => chunk.length <= 0x8000))
})

test('UTF-8 text selection preserves Unicode, BOM and control characters across storage chunks', async () => {
  const text = '\uFEFF' + 'a'.repeat(32764) + '中🙂\r\n\t尾\u0000'
  const bytes = new TextEncoder().encode(text)
  const store = new utils.HexdumpBytes()
  store.append(bytes)
  const controller = new AbortController()
  assert.equal(await selectionUtils.serializeHexdumpSelection(store, 0, store.length, 'text', controller.signal, 'utf8'), text)
  assert.equal(await selectionUtils.serializeHexdumpSelection(store, 0, store.length, 'base64', controller.signal), Buffer.from(bytes).toString('base64'))
})

test('large selection serialization yields and aborts before producing a result', async () => {
  const store = new utils.HexdumpBytes()
  store.append(new Uint8Array(2 * 1024 * 1024))
  const controller = new AbortController()
  const operation = selectionUtils.serializeHexdumpSelection(store, 0, store.length, 'hex', controller.signal)
  setTimeout(() => controller.abort(), 0)
  await assert.rejects(operation, { name: 'AbortError' })
})

test('clipboard NUL escaping preserves all other characters across chunk boundaries', async () => {
  const signal = new AbortController().signal
  const unchanged = '你好\uFEFF🙂\\x00\r\n\t\u001f\u007f\u0080\u00ff'
  assert.equal(await selectionUtils.escapeHexdumpClipboardNullBytes(unchanged, signal), unchanged)
  assert.equal(await selectionUtils.escapeHexdumpClipboardNullBytes('', signal), '')
  const input = 'a'.repeat(65535) + '\u0000\u0000' + unchanged + 'b'.repeat(32768) + '\u0000'
  assert.equal(await selectionUtils.escapeHexdumpClipboardNullBytes(input, signal), input.replaceAll('\u0000', '\\x00'))
  assert.equal(await selectionUtils.escapeHexdumpClipboardNullBytes('\u0000\u0000', signal), '\\x00\\x00')
})

test('clipboard NUL escaping yields and aborts before returning prepared text', async () => {
  const controller = new AbortController()
  const operation = selectionUtils.escapeHexdumpClipboardNullBytes('\u0000'.repeat(200000), controller.signal)
  setTimeout(() => controller.abort(), 0)
  await assert.rejects(operation, { name: 'AbortError' })
  await assert.rejects(selectionUtils.escapeHexdumpClipboardNullBytes('', controller.signal), { name: 'AbortError' })
})

test('mounted bytes have compact uppercase offsets, four-byte groups and unselectable padding', async (t) => {
  const view = await mountViewer(t, Uint8Array.from({ length: 37 }, (_, index) => index))
  const first = view.find(el => el.props['data-byte-idx'] === 0)
  assert.equal(first.parent.parent.children[0].text, '00000')
  assert.equal(view.find(el => el.props['data-byte-idx'] === 15).text, '0F')
  assert.equal(view.find(el => el.props['data-byte-idx'] === 4).props.class.includes('before:border-l'), true)
  assert.equal(view.find(el => el.props['data-byte-idx'] === 3).props.class.includes('before:border-l'), false)
  assert.equal(view.findAll(el => el.props['data-byte-idx'] != null).length, 74)
  assert.ok(view.find(el => el.props.class?.includes('pointer-events-none text-transparent')))
  const last = view.find(el => el.props['data-byte-idx'] === 36)
  const padding = last.parent.children.find(el => el.props['data-byte-idx'] == null)
  await view.select(36)
  const scroller = view.find(el => el.props.onScroll)
  scroller.props.onPointerdown({ target: padding, button: 0, pointerId: 1 })
  await view.flush()
  assert.equal(view.selected().length, 0)
})

test('mounted clicks keep the DOM byte even when release geometry maps to the preceding row', async (t) => {
  const view = await mountViewer(t, new Uint8Array(400).fill(65))
  const scroller = view.find(el => el.props.onScroll)
  const columns = view.find(el => el.props['data-byte-idx'] === 0).parent.children.length
  for (const column of ['hex', 'ascii']) {
    const index = columns + 4
    const event = view.pointer(index, column)
    // Hit-testing the DOM resolved this byte, but coordinate rounding falls just above its row.
    event.clientY -= 11.5
    scroller.props.onPointerdown(event)
    scroller.props.onPointerup({ ...event, target: scroller, buttons: 0 })
    await view.flush()
    assert.deepEqual(view.selected(), [index, index])
    assert.equal(scroller.hasPointerCapture(event.pointerId), false)
  }
})

test('mounted click jitter at a row boundary does not start dragging or expand Shift selection', async (t) => {
  const view = await mountViewer(t, new Uint8Array(400).fill(65))
  const scroller = view.find(el => el.props.onScroll)
  const columns = view.find(el => el.props['data-byte-idx'] === 0).parent.children.length
  for (const column of ['hex', 'ascii']) {
    const index = columns + 4
    for (const shiftKey of [false, true]) {
      const clicked = index + (shiftKey ? 2 : 0)
      const event = view.pointer(clicked, column, { shiftKey })
      event.clientY -= 10
      scroller.props.onPointerdown(event)
      const moved = { ...event, target: scroller, clientY: event.clientY - 2 }
      scroller.props.onPointermove(moved)
      await view.frame()
      scroller.props.onPointerup({ ...moved, buttons: 0 })
      await view.flush()
      const selected = [...new Set(view.selected())].sort((a, b) => a - b)
      assert.deepEqual(selected, shiftKey ? [index, index + 1, index + 2] : [index])
    }
  }
})

test('mounted stationary presses at the viewport edge do not auto-scroll or extend selection', async (t) => {
  const view = await mountViewer(t, new Uint8Array(10000).fill(65))
  const scroller = view.find(el => el.props.onScroll)
  const columns = view.find(el => el.props['data-byte-idx'] === 0).parent.children.length
  for (const column of ['hex', 'ascii']) {
    const index = 21 * columns + 4
    const event = view.pointer(index, column, { clientY: 485 })
    scroller.props.onPointerdown(event)
    await view.frame()
    await view.frame()
    assert.equal(scroller.scrollTop, 0)
    assert.deepEqual(view.selected(), [index, index])
    scroller.props.onPointerup({ ...event, buttons: 0 })
    await view.flush()
    assert.deepEqual(view.selected(), [index, index])
  }
})

test('mounted released buttons and canceled captures stop extending the selection', async (t) => {
  const view = await mountViewer(t, new Uint8Array(10000).fill(65))
  const scroller = view.find(el => el.props.onScroll)
  for (const stop of ['buttons', 'other-button', 'onPointercancel', 'onLostpointercapture']) {
    await view.drag(3, 10)
    const before = view.selected()
    if (stop === 'buttons' || stop === 'other-button') scroller.props.onPointermove(view.pointer(20, 'hex', { target: scroller, buttons: stop === 'buttons' ? 0 : 2 }))
    else scroller.props[stop]()
    scroller.props.onPointermove(view.pointer(100, 'hex', { target: scroller, clientY: 530 }))
    await view.frame()
    assert.deepEqual(view.selected(), before)
    assert.equal(scroller.scrollTop, 0)
    assert.equal(scroller.hasPointerCapture(1), false)
  }
})

test('mounted drag and Shift selection synchronize both columns and keep unaffected rows stable', async (t) => {
  const view = await mountViewer(t, new Uint8Array(400).fill(65))
  const columns = view.find(el => el.props['data-byte-idx'] === 0).parent.children.length
  const expected = (start, end) => Array.from({ length: end - start + 1 }, (_, index) => start + index).flatMap(index => [index, index]).sort((a, b) => a - b)
  const selected = () => view.selected().sort((a, b) => a - b)
  let release = await view.drag(3, columns + 2)
  assert.deepEqual(selected(), expected(3, columns + 2))
  release()
  release = await view.drag(columns + 2, 3, 'ascii')
  assert.deepEqual(selected(), expected(3, columns + 2))
  release()
  await view.select(columns + 5, 'hex', true)
  assert.deepEqual(selected(), expected(columns + 2, columns + 5))
  view.stats.updatedRows.length = 0
  view.stats.byteReads = 0
  await view.select(columns + 6, 'hex', true)
  assert.equal(view.stats.updatedRows.length, 1)
  assert.equal(view.stats.byteReads, 0)
  view.find(el => el.props.onScroll).props.onContextmenu()
  assert.deepEqual(selected(), expected(columns + 2, columns + 6))
  await view.hover(columns + 3)
  assert.equal(view.find(el => el.props['data-byte-idx'] === columns + 3).props.class.includes('text-app-accent'), false)
})

test('mounted edge dragging auto-scrolls and extends the selection beyond the viewport', async (t) => {
  const view = await mountViewer(t, new Uint8Array(10000).fill(65))
  const release = await view.drag(2, 100, 'hex', { clientY: 530 })
  await view.frame()
  assert.equal(view.find(el => el.props.onScroll).scrollTop, 24)
  assert.ok(Math.max(...view.selected()) > 100)
  release()
})

test('mounted selection persists across resize, scrolling and SSE appends, and clears on replacement', async (t) => {
  const view = await mountViewer(t, 'A'.repeat(200), { appendOnly: true })
  await view.select(3)
  await view.select(28, 'ascii', true)
  const before = view.selected().sort((a, b) => a - b)
  view.setWidth(480)
  view.resize()
  await view.flush()
  assert.deepEqual(view.selected().sort((a, b) => a - b), before)
  await view.scroll(1)
  assert.deepEqual(view.selected().sort((a, b) => a - b), before)
  view.currentInput.value += 'Z'
  await until(() => view.find(el => el.props['data-byte-idx'] === 200))
  assert.deepEqual(view.selected().sort((a, b) => a - b), before)
  view.currentInput.value = 'B'.repeat(201)
  await view.flush()
  assert.equal(view.selected().length, 0)
})

test('mounted Shift selection spans segments without materializing off-screen rows', async (t) => {
  const input = new Uint8Array(20 * 1024 * 1024 + 7)
  const view = await mountViewer(t, input)
  await view.select(3)
  view.find(el => el.props['aria-label'] === 'Last segment').props.onClick()
  await view.flush()
  const scroller = view.find(el => el.props.onScroll)
  await view.scroll(scroller.scrollHeight - scroller.clientHeight)
  await view.select(input.length - 2, 'ascii', true)
  assert.ok(view.selected().length < 6000)
  assert.ok(view.selected().includes(input.length - 2))
  assert.ok(!view.selected().includes(input.length - 1))
  view.find(el => el.props['aria-label'] === 'First segment').props.onClick()
  await view.flush()
  assert.ok(view.selected().includes(3))
  assert.ok(!view.selected().includes(0))
  view.visible.value = false
  await view.flush()
  view.visible.value = true
  await view.flush()
  assert.equal(view.selected().length, 0)
})

test('mounted menu requires selection and copies or exports only the selected bytes', async (t) => {
  const view = await mountViewer(t, Uint8Array.of(0, 65, 10, 126, 255))
  assert.deepEqual(view.menu().map(item => item.label ?? item.type), ['Copy text', 'Copy HEX', 'Copy as Base64', 'separator', 'Export'])
  assert.ok(view.menu().filter(item => item.label).every(item => item.disabled))
  view.menu().find(item => item.label === 'Copy HEX').onSelect()
  assert.equal(view.feedback.copied.length, 0)
  await view.select(0)
  await view.select(4, 'ascii', true)
  for (const [label, expected] of [['Copy text', '\\x00A\n~\u00ff'], ['Copy HEX', '00 41 0A 7E FF'], ['Copy as Base64', 'AEEKfv8=']]) {
    const count = view.feedback.copied.length
    view.menu().find(item => item.label === label).onSelect()
    view.menu().find(item => item.label === label).onSelect()
    await until(() => view.feedback.copied.length > count)
    await view.flush()
    assert.equal(view.feedback.copied.length, count + 1)
    assert.equal(view.feedback.copied.at(-1), expected)
    assert.equal(view.feedback.success.at(-1), 'Selection copied')
  }
  view.menu().find(item => item.label === 'Export').onSelect()
  await until(() => view.feedback.exports.length === 1)
  assert.deepEqual(Buffer.from(view.feedback.exports[0], 'base64'), Buffer.from([0, 65, 10, 126, 255]))
  view.viewerProps.exporting = true
  await view.flush()
  assert.ok(view.menu().find(item => item.label === 'Export').disabled)
  view.viewerProps.exporting = false
  view.failCopy(new Error('clipboard unavailable'))
  await view.flush()
  view.menu().find(item => item.label === 'Copy text').onSelect()
  await until(() => view.feedback.error.length === 1)
  assert.match(view.feedback.error[0], /clipboard unavailable/)
  assert.ok(!view.menu().find(item => item.label === 'Copy text').disabled)
  view.clear()
  await view.flush()
  assert.ok(view.menu().find(item => item.label === 'Export').disabled)
})

test('mounted UTF-8 copying escapes NUL for Windows while Base64 preserves the original bytes', async (t) => {
  const input = '你好\r\n世界\t\u0000🙂'
  const view = await mountViewer(t, input)
  await view.select(0)
  await view.select(Buffer.byteLength(input, 'utf8') - 1, 'ascii', true)
  view.menu().find(item => item.label === 'Copy text').onSelect()
  await until(() => view.feedback.copied.length === 1)
  assert.equal(view.feedback.copied[0], '你好\r\n世界\t\\x00🙂')
  assert.equal(view.feedback.success.at(-1), 'Selection copied')
  view.menu().find(item => item.label === 'Copy as Base64').onSelect()
  await until(() => view.feedback.copied.length === 2)
  assert.deepEqual(Buffer.from(view.feedback.copied[1], 'base64'), Buffer.from(input, 'utf8'))
})

test('mounted Base64 binary bodies copy original byte characters without ASCII display substitutions', async (t) => {
  const input = Buffer.from([0, 10, 31, 127, 128, 255])
  const view = await mountViewer(t, input.toString('base64'), { isBase64: true })
  assert.equal(view.find(el => el.props['data-byte-idx'] === 0 && el.props['data-byte-column'] === 'ascii').text, '.')
  await view.select(0)
  await view.select(input.length - 1, 'ascii', true)
  view.menu().find(item => item.label === 'Copy text').onSelect()
  await until(() => view.feedback.copied.length === 1)
  assert.equal(view.feedback.copied[0], '\\x00\n\u001f\u007f\u0080\u00ff')
  assert.equal(view.feedback.success.at(-1), 'Selection copied')
  view.menu().find(item => item.label === 'Copy as Base64').onSelect()
  await until(() => view.feedback.copied.length === 2)
  assert.deepEqual(Buffer.from(view.feedback.copied[1], 'base64'), input)
})

test('mounted selection operations abort on replacement and unmount without copying stale data', async (t) => {
  const view = await mountViewer(t, new Uint8Array(200000).fill(65))
  await view.select(0)
  await view.scroll(50000)
  await view.select(view.offset.value + 3, 'ascii', true)
  view.menu().find(item => item.label === 'Copy HEX').onSelect()
  view.currentInput.value = Uint8Array.of(66)
  await view.flush()
  await new Promise(resolve => setTimeout(resolve, 20))
  assert.equal(view.feedback.copied.length, 0)
  assert.equal(view.feedback.error.length, 0)
  await view.select(0)
  view.menu().find(item => item.label === 'Export').onSelect()
  view.visible.value = false
  await view.flush()
  await new Promise(resolve => setTimeout(resolve, 20))
  assert.equal(view.feedback.exports.length, 0)
})

function setupExportHost(t, kind) {
  const path = kind === 'http' ? 'traffic/BodyViewer' : 'modal/WebSocketMessageDetailModal'
  const file = readFileSync(new URL(`../src/components/${path}.vue`, import.meta.url), 'utf8')
  const compiled = compileScript(parse(file, { filename: `${path}.vue` }).descriptor, { id: 'hex-export-host-test' }).content
  const calls = { dialogs: [], saves: [], errors: [] }
  let savePath = 'E:/tmp/selected.bin'
  let saveError = null
  let dialogResult = null
  const dependencies = {
    vue: { ...Vue, onMounted() {}, onUnmounted() {} },
    'vue-i18n': { useI18n: () => ({ t: (key, args) => `${key} ${args?.error ?? ''}` }) },
    '@wailsio/runtime': { Dialogs: { SaveFile: async (options) => {
      calls.dialogs.push(options)
      return dialogResult ? await dialogResult : savePath
    } } },
    '@/utils/clipboard': { copyText: async () => {} },
    '@/utils/dialog': dialogUtils,
    '@/utils/hexdump': utils,
    '@/utils/format': { formatFileSize: (value) => String(value) },
    '@/composables/useHexdumpViewState': { useHexdumpViewState },
    '@/composables/useNotify': { useNotify: () => ({ success() {}, error: message => calls.errors.push(message) }) },
    '@/components/common/emptyState': {},
    '@/components/common/monacoLargeText': monacoLargeText,
    '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice': { SaveBodyToFile: async (request) => {
      if (saveError) throw saveError
      calls.saves.push(request)
    } },
  }
  const exports = {}
  new Function('require', 'exports', ts.transpileModule(compiled, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText)((id) => {
    if (id.endsWith('.vue')) return { __esModule: true, default: {} }
    if (dependencies[id]) return dependencies[id]
    throw new Error(`Unexpected export host dependency: ${id}`)
  }, exports)
  const props = Vue.reactive(kind === 'http'
    ? { body: 'original', bodyEncoding: '', contentType: 'text/plain', sourcePath: '/original.txt' }
    : { show: true, message: { data: 'b3JpZ2luYWw=', msgType: 'binary', direction: 'receive', dataSize: 8 } })
  const state = scoped(t, () => exports.default.setup(props, { expose() {}, emit() {} }))
  return {
    state, calls, props,
    export: kind === 'http' ? state.saveHexSelection : state.exportMessage,
    download: kind === 'http' ? state.saveCurrentBodyContent : () => state.exportMessage(),
    setPath(value) { savePath = value },
    fail(value) { saveError = value },
    delayDialog(value) { dialogResult = value },
    busy: () => (kind === 'http' ? state.exportingBody : state.exporting).value,
  }
}

for (const kind of ['http', 'websocket']) {
  test(`${kind} host exports exact selection bytes and handles cancellation, errors and repeated saves`, async (t) => {
    const host = setupExportHost(t, kind)
    await host.export('AApB/w==')
    assert.deepEqual(host.calls.saves[0], {
      path: 'E:/tmp/selected.bin', body: 'AApB/w==', bodyEncoding: 'base64', contentType: 'application/octet-stream',
    })
    assert.deepEqual(Buffer.from(host.calls.saves[0].body, 'base64'), Buffer.from([0, 10, 65, 255]))
    assert.equal(host.calls.dialogs[0].Filename, kind === 'http' ? 'hex-selection.bin' : 'websocket-selection.bin')
    host.setPath(' ')
    await host.export('QQ==')
    assert.equal(host.calls.saves.length, 1)
    assert.equal(host.calls.errors.length, 0)
    host.delayDialog(Promise.reject(new Error('cancelled by user')))
    await host.export('QQ==')
    assert.equal(host.calls.errors.length, 0)
    host.delayDialog(null)
    host.setPath('E:/tmp/selected.bin')
    host.fail(new Error('disk full'))
    await host.export('QQ==')
    assert.match(host.calls.errors[0], /disk full/)
    assert.equal(host.busy(), false)
    host.fail(null)
    let release
    host.delayDialog(new Promise(resolve => { release = resolve }))
    const pending = host.export('QQ==')
    assert.equal(host.busy(), true)
    const before = host.calls.dialogs.length
    await host.export('Qg==')
    assert.equal(host.calls.dialogs.length, before)
    release('E:/tmp/selected.bin')
    await pending
    assert.equal(host.calls.saves.at(-1).body, 'QQ==')
    assert.equal(host.busy(), false)
  })

  test(`${kind} toolbar downloads the whole body independently of Hex selection`, async (t) => {
    const host = setupExportHost(t, kind)
    if (kind === 'http') {
      Object.assign(host.props, { body: 'b3JpZ2luYWw=', bodyEncoding: 'base64', contentType: 'application/octet-stream', sourcePath: '/original.bin' })
      host.state.activeTab.value = 'hex'
    }
    await host.download()
    assert.deepEqual(host.calls.saves[0], {
      path: 'E:/tmp/selected.bin', body: 'b3JpZ2luYWw=', bodyEncoding: 'base64', contentType: 'application/octet-stream',
    })
    assert.equal(host.calls.dialogs[0].Filename, kind === 'http' ? 'original.bin' : 'websocket-message.bin')

    const text = '完整内容\r\n🙂'
    if (kind === 'http') Object.assign(host.props, { body: text, bodyEncoding: '', contentType: 'text/plain', sourcePath: '/original.txt' })
    else Object.assign(host.props.message, { data: text, msgType: 'text' })
    await host.download()
    assert.equal(host.calls.saves[1].body, text)
    assert.equal(host.calls.saves[1].bodyEncoding, '')
    assert.equal(host.calls.dialogs[1].Filename, kind === 'http' ? 'original.txt' : 'websocket-message.txt')

    host.setPath(' ')
    await host.download()
    assert.equal(host.calls.saves.length, 2)
    assert.equal(host.calls.errors.length, 0)
    host.setPath('E:/tmp/whole.bin')
    host.fail(new Error('disk full'))
    await host.download()
    assert.match(host.calls.errors[0], /disk full/)
    assert.equal(host.busy(), false)
    host.fail(null)
    let release
    host.delayDialog(new Promise(resolve => { release = resolve }))
    const pending = host.download()
    assert.equal(host.busy(), true)
    const before = host.calls.dialogs.length
    await host.download()
    await host.export('QQ==')
    assert.equal(host.calls.dialogs.length, before)
    release('E:/tmp/whole.bin')
    await pending
    assert.equal(host.calls.saves.at(-1).body, text)
    assert.equal(host.busy(), false)
  })
}

test('Hex menu translations have matching bilingual keys and placeholders', () => {
  const locales = ['en', 'zh'].map(locale => JSON.parse(readFileSync(new URL(`../src/locales/${locale}.json`, import.meta.url), 'utf8').replace(/^\uFEFF/, '')))
  for (const key of ['hex_copy_text', 'hex_copy_hex', 'hex_copy_base64', 'hex_export', 'hex_selection_copied']) {
    assert.equal(typeof locales[0].detail[key], 'string')
    assert.equal(typeof locales[1].detail[key], 'string')
    assert.deepEqual(locales[0].detail[key].match(/\{.*?\}/g), locales[1].detail[key].match(/\{.*?\}/g))
  }
})
