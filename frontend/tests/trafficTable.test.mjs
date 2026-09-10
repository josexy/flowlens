import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'
import { createServer } from 'vite'
import { Virtualizer } from '@tanstack/vue-virtual'

let server
let state
let useTrafficStore
let useFilterStore

before(async () => {
  server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true },
    server: { middlewareMode: true },
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('../src', import.meta.url)),
        '#bindings': fileURLToPath(new URL('../bindings', import.meta.url)),
      },
    },
    plugins: [
      {
        name: 'traffic-table-test-backend',
        enforce: 'pre',
        resolveId(id) {
          if (id.endsWith('/proxy_service/proxyservice')) return '\0traffic-test-service'
          if (id.endsWith('/runtime/batchedAppEvents')) return '\0traffic-test-events'
        },
        load(id) {
          if (id === '\0traffic-test-service')
            return `
          export const GetTraffic = async () => globalThis.__flowlensTrafficTableTest.snapshot;
          export const GetStatistics = async () => ({ total: 0, totalHttp: 0, totalWs: 0, totalTcp: 0 });
          export const GetTrafficBodyView = async () => null;
          export const ClearTraffic = async () => {};
          export const DeleteTraffic = async () => {};
          export const SetLiveTrafficDetail = async () => {};
        `
          if (id === '\0traffic-test-events')
            return `
          export function onBatchedAppEvent(name, onData, onDropped) {
            const listeners = globalThis.__flowlensTrafficTableTest.listeners;
            listeners.set(name, { onData, onDropped });
            return () => listeners.delete(name);
          }
        `
        },
      },
    ],
  })
  state = await server.ssrLoadModule('/src/utils/traffic-table-state.ts')
  ;({ useTrafficStore } = await server.ssrLoadModule('/src/stores/traffic.ts'))
  ;({ useFilterStore } = await server.ssrLoadModule('/src/stores/filter.ts'))
})

after(async () => {
  await server?.close()
  delete globalThis.__flowlensTrafficTableTest
})

function entry(id, overrides = {}) {
  return {
    id,
    revision: 1,
    type: 'http',
    method: 'GET',
    startedAt: '',
    url: `https://example.test/${id}`,
    host: 'example.test',
    path: `/${id}`,
    statusCode: 0,
    status: '',
    ...overrides,
  }
}

async function createStore(t, initial = 5000) {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  setActivePinia(createPinia())
  const backend = {
    snapshot: typeof initial === 'number'
      ? Array.from({ length: initial }, (_, i) => entry(i + 1))
      : initial,
    listeners: new Map(),
  }
  globalThis.__flowlensTrafficTableTest = backend
  const store = useTrafficStore()
  await store.initialize()
  t.after(() => {
    store.cleanup()
    store.$dispose()
  })
  return { store, backend }
}

test('the newest edge follows ID ordering and is unknown for non-chronological sorts', () => {
  const descending = { key: 'id', order: 'desc' }
  const ascending = { key: 'id', order: 'asc' }
  assert.equal(state.getTrafficLatestEdge(descending), 'start')
  assert.equal(state.getTrafficLatestEdge(ascending), 'end')
  assert.equal(state.getTrafficLatestEdge({ key: null, order: null }), 'end')
  assert.equal(state.getTrafficLatestEdge({ key: 'duration', order: 'desc' }), null)
  assert.equal(state.getTrafficLatestEdge({ key: 'host', order: 'asc' }), null)
  assert.equal(state.isTrafficAtLatestEdge(descending, 0.5, 160000, 480), true)
  assert.equal(state.isTrafficAtLatestEdge(descending, 32, 160000, 480), false)
  assert.equal(state.isTrafficAtLatestEdge(descending, 159520, 160000, 480), false)
  assert.equal(state.isTrafficAtLatestEdge(ascending, 159519.5, 160000, 480), true)
  assert.equal(state.isTrafficAtLatestEdge(ascending, 0, 160000, 480), false)
  assert.equal(state.isTrafficAtLatestEdge(ascending, 0, 480, 480), true)
  assert.equal(state.isTrafficAtLatestEdge(descending, 0, 0, 0), false)
  assert.equal(state.isTrafficAtLatestEdge({ key: 'size', order: 'desc' }, 0, 160000, 480), false)
})

test('shrinking a scrolled table renders the clamped viewport before its scroll event', () => {
  for (const count of [0, 1, 10, 13, 50]) {
    for (const height of [413, 480, 800]) {
      const options = {
        count: 5000,
        getScrollElement: () => null,
        estimateSize: () => 32,
        initialRect: { width: 1000, height },
        initialOffset: 32007,
        overscan: 6,
        observeElementRect: () => {},
        observeElementOffset: () => {},
        scrollToFn: () => {},
        rangeExtractor: range => state.getTrafficVirtualRange(range, height),
      }
      const virtualizer = new Virtualizer(options)
      virtualizer.getVirtualItems()
      // A filter changes count synchronously; the native scroll event is later.
      virtualizer.setOptions({ ...options, count })
      const rows = virtualizer.getVirtualItems()
      if (count === 0) {
        assert.equal(rows.length, 0)
        continue
      }
      const clampedTop = Math.max(0, count * 32 - height)
      assert.ok(rows[0].start <= clampedTop, `missing rows above viewport: ${count}/${height}`)
      assert.ok(rows.at(-1).end >= Math.min(count * 32, clampedTop + height))
      assert.ok(rows.length <= Math.ceil(height / 32) + 13, 'range remains virtualized')
      // The delayed browser event must not change the visible request set.
      const visibleIds = items => items.filter(row => row.end > clampedTop && row.start < clampedTop + height).map(row => row.index)
      virtualizer.scrollOffset = clampedTop
      assert.deepEqual(visibleIds(virtualizer.getVirtualItems()), visibleIds(rows))
      // Expanding back to the full list must retain ordinary virtualization.
      virtualizer.setOptions(options)
      assert.ok(virtualizer.getVirtualItems().length <= Math.ceil(height / 32) + 13)
    }
  }
})

test('payload updates reuse sort values and order for a 10000-request table', () => {
  const sort = state.createTrafficTableSorter()
  let reads = 0
  const entries = Array.from({ length: 10000 }, (_, index) =>
    entry(index + 1, {
      get statusCode() {
        reads++
        return (index % 3) + 200
      },
    }),
  )
  const first = sort(entries, { key: 'statusCode', order: 'asc' })
  assert.equal(reads, 10000)
  const next = entries.slice()
  next[1] = entry(2, { statusCode: 201, status: 'updated content' })
  const second = sort(next, { key: 'statusCode', order: 'asc' })
  assert.equal(reads, 10000, 'unchanged objects must not recalculate sort values')
  assert.deepEqual(
    second.map((row) => row.id),
    first.map((row) => row.id),
  )
  assert.equal(
    second.find((row) => row.id === 2),
    next[1],
    'cached order must still show new content',
  )
})

test('sorting handles new values, same-count replacements, ties and changing columns', () => {
  const sort = state.createTrafficTableSorter()
  const entries = [
    entry(3, { statusCode: 200 }),
    entry(2, { statusCode: 500 }),
    entry(1, { statusCode: 200 }),
  ]
  assert.deepEqual(
    sort(entries, { key: 'statusCode', order: 'asc' }).map((row) => row.id),
    [1, 3, 2],
  )
  assert.deepEqual(
    sort(entries, { key: 'statusCode', order: 'desc' }).map((row) => row.id),
    [2, 1, 3],
  )
  const updated = [entries[0], entry(2, { statusCode: 100 }), entry(4, { statusCode: 404 })]
  assert.deepEqual(
    sort(updated, { key: 'statusCode', order: 'asc' }).map((row) => row.id),
    [2, 3, 4],
  )
  assert.deepEqual(
    sort(updated, { key: 'id', order: 'desc' }).map((row) => row.id),
    [4, 3, 2],
  )
  assert.equal(sort(updated, { key: null, order: null }), updated)
})

test('incomplete metrics stay last in either sort direction', () => {
  const sort = state.createTrafficTableSorter()
  const metrics = (duration) => ({
    state: 'completed',
    startedAtMicros: 100,
    endedAtMicros: 100 + duration,
    headerSize: 0,
    bodySize: 0,
  })
  const entries = [
    entry(1),
    entry(2, { request: { metrics: metrics(10) }, response: { metrics: metrics(20) } }),
    entry(3),
  ]
  for (const order of ['asc', 'desc']) {
    assert.deepEqual(
      sort(entries, { key: 'duration', order }).map((row) => row.id),
      [2, 1, 3],
    )
  }
})

test('eviction preserves an anchor and its partial-row offset even across a large batch', () => {
  const previous = Array.from({ length: 5000 }, (_, i) => ({ id: i + 1 }))
  const next = Array.from({ length: 5000 }, (_, i) => ({ id: i + 101 }))
  assert.equal(state.getTrafficEvictionScrollTop(previous, next, 32007, 480), 28807)
  assert.equal(state.getTrafficEvictionScrollTop(previous, next, 159520, 480), 159520)
  assert.equal(state.getTrafficEvictionScrollTop(previous, next, 32, 480), null)
  assert.equal(state.getTrafficEvictionScrollTop(previous, next, 32007, 0), null)
})

test('reading retains the displayed 5000 requests and bounds the new-request queue', async (t) => {
  const { store } = await createStore(t)
  store.pauseLiveEntryEviction()
  for (let batch = 0; batch < 10; batch++) {
    for (let i = 1; i <= 1000; i++) store.addOrUpdateEntry(entry(5000 + batch * 1000 + i))
    t.mock.timers.tick(100)
  }
  assert.equal(store.entries.length, 5000)
  assert.equal(store.entries[0].id, 1)
  assert.equal(store.pendingLiveEntryCount, 5000)
  assert.equal(store.liveEntryEvictionVersion, 0)
  store.resumeLiveEntryEviction()
  assert.equal(store.pendingLiveEntryCount, 0)
  assert.equal(store.entries.length, 5000)
  assert.equal(store.entries[0].id, 10001)
  assert.equal(store.entries.at(-1).id, 15000)
  assert.equal(store.liveEntryEvictionVersion, 1)
})

test('queued requests retain patches, reject stale replacements and drain in ID order', async (t) => {
  const { store, backend } = await createStore(t)
  store.pauseLiveEntryEviction()
  store.addOrUpdateEntry(entry(5001))
  t.mock.timers.tick(100)
  backend.listeners
    .get('traffic:patch')
    .onData({
      trafficId: 5001,
      revision: 3,
      responseHeaders: { statusCode: 201, status: 'Created', proto: 'HTTP/1.1', headerFields: [] },
    })
  store.addOrUpdateEntry(entry(5001, { revision: 2 }))
  // A newer patch arriving before the next batch must use the queued revision,
  // not a stale replacement waiting in the ordinary entry buffer.
  backend.listeners.get('traffic:patch').onData({
    trafficId: 5001,
    revision: 4,
    error: { timestamp: '', error: 'connection closed' },
  })
  t.mock.timers.tick(100)
  store.addOrUpdateEntry(entry(5002))
  store.resumeLiveEntryEviction()
  assert.deepEqual(
    store.entries.slice(-2).map((row) => row.id),
    [5001, 5002],
  )
  assert.equal(store.entries.at(-2).statusCode, 201)
  assert.equal(store.entries.at(-2).revision, 4)
  assert.equal(store.pendingLiveEntryCount, 0)
})

test('live requests stay in ID order within and across event batches', async (t) => {
  const { store, backend } = await createStore(t, [entry(108)])
  const receive = backend.listeners.get('traffic:entry').onData
  receive(entry(111, { statusCode: 200 }))
  receive(entry(110, { statusCode: 500 }))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), [108, 110, 111])

  await store.selectEntry(store.entries[1])
  receive(entry(109))
  receive(entry(112))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), [108, 109, 110, 111, 112])
  assert.equal(store.selectedEntry.id, 110)

  // Inserting an earlier ID must also refresh the indices used by later patches.
  backend.listeners.get('traffic:patch').onData({
    trafficId: 110,
    revision: 2,
    responseHeaders: { statusCode: 201, status: 'Created', proto: 'HTTP/1.1', headerFields: [] },
  })
  receive(entry(110, { statusCode: 500 }))
  t.mock.timers.tick(100)
  assert.equal(store.entries[2].statusCode, 201)
  assert.equal(store.selectedEntry.statusCode, 201)
  assert.equal(store.entries[3].statusCode, 200)
  assert.deepEqual(store.entries.map(row => row.id), [108, 109, 110, 111, 112])

  const sort = state.createTrafficTableSorter()
  assert.deepEqual(sort(store.entries, { key: 'id', order: 'desc' }).map(row => row.id),
    [112, 111, 110, 109, 108])
  assert.deepEqual(sort(store.entries, { key: 'statusCode', order: 'asc' }).map(row => row.id),
    [108, 109, 112, 111, 110])
  assert.deepEqual(sort(store.entries, store.sortConfig).map(row => row.id),
    [108, 109, 110, 111, 112])
})

test('initial snapshots and recovered snapshots use ID order', async (t) => {
  const { store, backend } = await createStore(t, [entry(3), entry(1), entry(2)])
  assert.deepEqual(store.entries.map(row => row.id), [1, 2, 3])
  store.addOrUpdateEntry(entry(6))
  store.addOrUpdateEntry(entry(5))
  t.mock.timers.tick(100)
  backend.snapshot = [entry(3), entry(1), entry(4), entry(2)]
  backend.listeners.get('traffic:entry').onDropped(1)
  for (let i = 0; i < 6; i++) await Promise.resolve()
  assert.deepEqual(store.entries.map(row => row.id), [1, 2, 3, 4, 5, 6])
})

test('events buffered during snapshot loading merge into ID order', async (t) => {
  const snapshot = Promise.resolve().then(() => {
    const receive = globalThis.__flowlensTrafficTableTest.listeners.get('traffic:entry').onData
    receive(entry(112))
    receive(entry(109))
    return [entry(111), entry(108), entry(110)]
  })
  const { store } = await createStore(t, snapshot)
  assert.deepEqual(store.entries.map(row => row.id), [108, 109, 110, 111, 112])
})

test('oversized snapshots trim the lowest IDs before accepting new events', async (t) => {
  const { store, backend } = await createStore(t,
    Array.from({ length: 5001 }, (_, i) => entry(5001 - i)))
  assert.deepEqual(store.entries.map(row => row.id), Array.from({ length: 5000 }, (_, i) => i + 2))
  store.addOrUpdateEntry(entry(5002))
  t.mock.timers.tick(100)
  assert.equal(store.entries.at(-1).id, 5002)
  assert.equal(store.entries[0].id, 3)

  backend.listeners.get('traffic:reset').onData({})
  store.addOrUpdateEntry(entry(2))
  store.addOrUpdateEntry(entry(1))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), [1, 2])
})

test('late requests are ordered before eviction and cannot return after eviction', async (t) => {
  const { store } = await createStore(t, Array.from({ length: 5000 }, (_, i) => entry(i + 2)))
  store.addOrUpdateEntry(entry(1))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), Array.from({ length: 5000 }, (_, i) => i + 2))
  store.addOrUpdateEntry(entry(1, { revision: 2 }))
  store.addOrUpdateEntry(entry(5002))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), Array.from({ length: 5000 }, (_, i) => i + 3))
})

test('ordered pending batches retain the latest 5000 IDs', async (t) => {
  const { store } = await createStore(t, 0)
  for (let id = 1; id <= 5001; id++) store.addOrUpdateEntry(entry(id))
  t.mock.timers.tick(100)
  assert.deepEqual(store.entries.map(row => row.id), Array.from({ length: 5000 }, (_, i) => i + 2))
})

test('recovery merges missing IDs before queued live entries and preserves FIFO eviction', async (t) => {
  const { store, backend } = await createStore(t)
  store.pauseLiveEntryEviction()
  // The first live event was dropped before a later batch reached the frontend.
  for (let id = 5002; id <= 10000; id++) {
    store.addOrUpdateEntry(entry(id))
    if (id % 100 === 0) t.mock.timers.tick(100)
  }
  t.mock.timers.tick(100)
  store.addOrUpdateEntry(entry(5002, { revision: 3, statusCode: 201 }))
  t.mock.timers.tick(100)
  backend.snapshot = Array.from({ length: 5000 }, (_, i) => entry(i + 5001))
  backend.listeners.get('traffic:entry').onDropped(1)
  for (let i = 0; i < 6; i++) await Promise.resolve()
  assert.equal(store.entries[0].id, 1)
  assert.equal(store.pendingLiveEntryCount, 5000)
  store.addOrUpdateEntry(entry(10001))
  t.mock.timers.tick(100)
  store.resumeLiveEntryEviction()
  assert.deepEqual(store.entries.map(row => row.id), Array.from({ length: 5000 }, (_, i) => i + 5002))
  assert.equal(store.entries[0].statusCode, 201)
})

test('snapshot recovery and reset respect the held window and queue lifecycle', async (t) => {
  const { store, backend } = await createStore(t)
  store.pauseLiveEntryEviction()
  backend.snapshot = Array.from({ length: 5000 }, (_, i) => entry(i + 5001))
  backend.listeners.get('traffic:entry').onDropped(1)
  for (let i = 0; i < 6; i++) await Promise.resolve()
  assert.equal(store.entries[0].id, 1)
  assert.equal(store.pendingLiveEntryCount, 5000)
  store.resetState()
  assert.equal(store.entries.length, 0)
  assert.equal(store.pendingLiveEntryCount, 0)
})

test('filter caches refresh on query changes and replacement entries', async (t) => {
  const { store } = await createStore(t, 3)
  const filters = useFilterStore()
  t.after(() => filters.$dispose())
  filters.searchText = '/2'
  assert.deepEqual(
    filters.filteredEntries.map((row) => row.id),
    [2],
  )
  store.addOrUpdateEntry(
    entry(2, { revision: 2, url: 'https://example.test/changed', path: '/changed' }),
  )
  t.mock.timers.tick(100)
  assert.equal(filters.filteredEntries.length, 0)
  filters.searchText = 'changed'
  assert.deepEqual(
    filters.filteredEntries.map((row) => row.id),
    [2],
  )
  filters.searchText = ''
  assert.equal(filters.filteredEntries.length, 3)
})
