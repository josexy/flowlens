import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'

const frontendRoot = fileURLToPath(new URL('..', import.meta.url))
let requestEditorState
let viteServer

before(async () => {
  viteServer = await createServer({
    configFile: false,
    root: frontendRoot,
    optimizeDeps: { noDiscovery: true },
    server: { middlewareMode: true },
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('../src', import.meta.url)),
        '#bindings': fileURLToPath(new URL('../bindings', import.meta.url)),
      },
    },
  })
  requestEditorState = await viteServer.ssrLoadModule(
    '/src/stores/traffic-workspace/requestEditorState.ts',
  )
})

after(async () => {
  await viteServer?.close()
})

function savedHTTPRequest(overrides = {}) {
  return {
    method: 'GET',
    url: 'https://example.test/',
    bodyType: 'none',
    bodyText: '',
    proxyMode: 'none',
    timeoutMs: 0,
    ...overrides,
  }
}

test('traffic editing rejects missing request bodies while keeping actual empty bodies', () => {
  const args = {
    source: 'history-edit',
    entry: { id: 1, method: 'POST', url: 'https://example.test/', host: 'example.test', request: { headerFields: [] } },
    bodyView: { reqBody: '', rspBody: '', reqBodyUnavailable: true },
  }
  assert.throws(() => requestEditorState.toHttpRequestEditorState(args), /request_body_unavailable/)
  args.bodyView.reqBodyUnavailable = false
  assert.equal(requestEditorState.toHttpRequestEditorState(args).requestBodyText, '')
})

test('HTTP Request Editor keeps TLS verification per tab and includes it in saved and dirty state', () => {
  const state = requestEditorState.buildEmptyHttpRequestEditorState('new')
  const otherState = requestEditorState.buildEmptyHttpRequestEditorState('new')
  const tab = {
    key: 'http-request:tls',
    type: 'http-request',
    title: '',
    closable: true,
    httpRequest: state,
  }

  assert.equal(state.settings.skipVerifyTls, false)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), false)
  const initialSnapshot = requestEditorState.createSnapshotForTab(tab)
  state.settings.skipVerifyTls = true
  assert.equal(otherState.settings.skipVerifyTls, false)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), true)
  assert.notEqual(requestEditorState.createSnapshotForTab(tab), initialSnapshot)

  for (const skipVerifyTls of [true, false]) {
    state.settings.skipVerifyTls = skipVerifyTls
    const saved = requestEditorState.buildSavedHTTPRequestFromState(state)
    assert.equal(saved.skipVerifyTls, skipVerifyTls)
    const restored = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(saved, 'Saved')
    assert.equal(restored.settings.skipVerifyTls, skipVerifyTls)
    otherState.settings.skipVerifyTls = !skipVerifyTls
    requestEditorState.applySavedHTTPRequestToState(otherState, saved, 'Saved')
    assert.equal(otherState.settings.skipVerifyTls, skipVerifyTls)
  }
  assert.equal(requestEditorState.createSnapshotForTab(tab), initialSnapshot)
})

test('HTTP Request Editor verifies TLS by default for legacy APIs and traffic drafts', () => {
  const legacy = savedHTTPRequest()
  const restored = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(legacy, 'Legacy')
  assert.equal(restored.settings.skipVerifyTls, false)
  restored.settings.skipVerifyTls = true
  requestEditorState.applySavedHTTPRequestToState(restored, legacy, 'Legacy')
  assert.equal(restored.settings.skipVerifyTls, false)

  const trafficDraft = requestEditorState.toHttpRequestEditorState({
    source: 'capture-edit',
    entry: { id: 1, method: 'GET', url: 'https://example.test/', request: { headerFields: [] } },
    bodyView: null,
  })
  assert.equal(trafficDraft.settings.skipVerifyTls, false)
})

test('HTTP redirect limits are per tab, saved, restored, and included in dirty state', () => {
  const state = requestEditorState.buildEmptyHttpRequestEditorState('new')
  const otherState = requestEditorState.buildEmptyHttpRequestEditorState('new')
  const tab = { key: 'http-request:redirects', type: 'http-request', httpRequest: state }
  const initialSnapshot = requestEditorState.createSnapshotForTab(tab)
  assert.equal(state.settings.maxRedirects, 0)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), false)

  for (const limit of [1, 12, 0]) {
    state.settings.maxRedirects = limit
    assert.equal(otherState.settings.maxRedirects, 0)
    assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), limit > 0)
    assert.equal(requestEditorState.createSnapshotForTab(tab) === initialSnapshot, limit === 0)
    const saved = requestEditorState.buildSavedHTTPRequestFromState(state)
    assert.equal(saved.maxRedirects, limit)
    const restored = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(saved, 'Saved')
    assert.equal(restored.settings.maxRedirects, limit)
    const copy = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(saved, 'Copy')
    copy.settings.maxRedirects = 5
    assert.equal(restored.settings.maxRedirects, limit)
    requestEditorState.applySavedHTTPRequestToState(copy, saved, 'Saved')
    assert.equal(copy.settings.maxRedirects, limit)
  }
})

test('legacy saved requests and traffic drafts disable redirects', () => {
  const legacy = savedHTTPRequest()
  const state = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(legacy, 'Legacy')
  assert.equal(state.settings.maxRedirects, 0)
  state.settings.maxRedirects = 12
  requestEditorState.applySavedHTTPRequestToState(state, legacy, 'Legacy')
  assert.equal(state.settings.maxRedirects, 0)
  for (const source of ['capture-edit', 'history-edit']) {
    const draft = requestEditorState.toHttpRequestEditorState({
      source,
      entry: { id: 1, method: 'GET', url: 'https://example.test/', request: { headerFields: [] } },
      bodyView: null,
    })
    assert.equal(draft.settings.maxRedirects, 0)
  }
})

test('redirect input clears to zero and only retains nonnegative whole numbers', () => {
  for (const value of [null, undefined, NaN, Infinity, -1, -Infinity]) {
    assert.equal(requestEditorState.normalizeHttpMaxRedirects(value), 0)
  }
  assert.equal(requestEditorState.normalizeHttpMaxRedirects(1.8), 1)
  assert.equal(requestEditorState.normalizeHttpMaxRedirects(12), 12)
})

test('WebSocket Client keeps TLS verification per tab and includes it in saved and dirty state', () => {
  const state = requestEditorState.buildEmptyWebSocketClientState('new')
  const otherState = requestEditorState.buildEmptyWebSocketClientState('new')
  const tab = {
    key: 'websocket-client:tls',
    type: 'websocket-client',
    title: '',
    closable: true,
    webSocketClient: state,
  }

  assert.equal(state.settings.skipVerifyTls, false)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), false)
  const initialSnapshot = requestEditorState.createSnapshotForTab(tab)
  state.settings.skipVerifyTls = true
  assert.equal(otherState.settings.skipVerifyTls, false)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), true)
  assert.notEqual(requestEditorState.createSnapshotForTab(tab), initialSnapshot)

  for (const skipVerifyTls of [true, false]) {
    state.settings.skipVerifyTls = skipVerifyTls
    const saved = requestEditorState.buildSavedWebSocketRequestFromState(state)
    assert.equal(saved.skipVerifyTls, skipVerifyTls)
    const restored = requestEditorState.buildWebSocketClientStateFromSavedRequest(saved, 'Saved')
    assert.equal(restored.settings.skipVerifyTls, skipVerifyTls)
    otherState.settings.skipVerifyTls = !skipVerifyTls
    requestEditorState.applySavedWebSocketRequestToState(otherState, saved, 'Saved')
    assert.equal(otherState.settings.skipVerifyTls, skipVerifyTls)
  }
  assert.equal(requestEditorState.createSnapshotForTab(tab), initialSnapshot)
})

test('WebSocket Client verifies TLS by default for legacy APIs and traffic drafts', () => {
  const legacy = { url: 'wss://example.test/', draftType: 'text', proxyMode: 'none', timeoutMs: 0 }
  const restored = requestEditorState.buildWebSocketClientStateFromSavedRequest(legacy, 'Legacy')
  assert.equal(restored.settings.skipVerifyTls, false)
  restored.settings.skipVerifyTls = true
  requestEditorState.applySavedWebSocketRequestToState(restored, legacy, 'Legacy')
  assert.equal(restored.settings.skipVerifyTls, false)

  const trafficDraft = requestEditorState.toWebSocketClientState({
    source: 'history-edit',
    entry: { id: 1, url: 'wss://example.test/', request: { headerFields: [] } },
    bodyView: null,
  })
  assert.equal(trafficDraft.settings.skipVerifyTls, false)
})

test('HTTP Request Editor saves and restores the current-request script source disabled', () => {
  const state = requestEditorState.buildEmptyHttpRequestEditorState('new')
  const tab = {
    key: 'http-request:1',
    type: 'http-request',
    title: '',
    closable: true,
    httpRequest: state,
  }

  assert.equal(state.pluginsEnabled, false)
  assert.equal(state.inlineScriptEnabled, false)
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), false)

  state.inlineScriptSource = 'def onRequest(context, request):\n    return request\n'
  assert.equal(requestEditorState.hasMeaningfulRequestDraft(tab), true)

  const saved = requestEditorState.buildSavedHTTPRequestFromState(state)
  assert.equal(saved.inlineScriptSource, state.inlineScriptSource)

  const restored = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(saved, 'Saved')
  assert.equal(restored.inlineScriptSource, state.inlineScriptSource)
  assert.equal(restored.inlineScriptEnabled, false)
  assert.equal(restored.pluginsEnabled, false)
})

test('HTTP Request Editor distinguishes legacy and explicitly empty saved scripts', () => {
  const legacy = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(savedHTTPRequest(), 'Legacy')
  assert.equal(legacy.inlineScriptSource, requestEditorState.DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT)

  const empty = requestEditorState.buildHttpRequestEditorStateFromSavedRequest(
    savedHTTPRequest({ inlineScriptSource: '' }),
    'Empty',
  )
  assert.equal(empty.inlineScriptSource, '')
  assert.equal(empty.inlineScriptEnabled, false)
})

test('Request draft cache cleanup clears only managed file references', () => {
  const httpState = requestEditorState.buildEmptyHttpRequestEditorState('capture-edit')
  httpState.requestBodyType = 'file'
  httpState.requestBodyText = 'keep hidden text body'
  httpState.requestBodyFile = {
    path: 'c:/FLOWLENS/request-draft-cache/recovered/body.bin',
    name: 'body.bin',
    size: 10,
  }
  httpState.requestBodyFormData = [
    {
      id: 'cached-row',
      enabled: true,
      name: 'cached',
      itemType: 'file',
      value: 'keep row value',
      file: {
        path: 'C:\\FlowLens\\request-draft-cache\\multipart\\upload.bin',
        name: 'upload.bin',
        size: 20,
      },
    },
    {
      id: 'managed-row',
      enabled: true,
      name: 'managed',
      itemType: 'file',
      value: '',
      file: {
        path: 'C:/FlowLens/api-collection-files/managed.bin',
        name: 'managed.bin',
        size: 30,
      },
    },
    {
      id: 'external-row',
      enabled: true,
      name: 'external',
      itemType: 'file',
      value: '',
      file: {
        path: 'C:/FlowLens/request-draft-cache-old/external.bin',
        name: 'external.bin',
        size: 40,
      },
    },
  ]

  const wsState = requestEditorState.buildEmptyWebSocketClientState('history-edit')
  wsState.draftType = 'binary-file'
  wsState.draftText = 'keep websocket text draft'
  wsState.draftFile = {
    path: 'C:/FlowLens/request-draft-cache/../request-draft-cache/ws.bin',
    name: 'ws.bin',
    size: 50,
  }

  const tabs = [
    {
      key: 'http-request:1',
      type: 'http-request',
      title: '',
      closable: true,
      httpRequest: httpState,
    },
    {
      key: 'websocket-client:1',
      type: 'websocket-client',
      title: '',
      closable: true,
      webSocketClient: wsState,
    },
  ]

  assert.equal(
    requestEditorState.clearRequestDraftCacheFileReferences(tabs, 'C:\\FlowLens\\request-draft-cache'),
    3,
  )
  assert.equal(httpState.requestBodyFile, null)
  assert.equal(httpState.requestBodyText, 'keep hidden text body')
  assert.equal(httpState.requestBodyFormData[0].file, null)
  assert.equal(httpState.requestBodyFormData[0].itemType, 'file')
  assert.equal(httpState.requestBodyFormData[0].value, 'keep row value')
  assert.equal(
    httpState.requestBodyFormData[1].file.path,
    'C:/FlowLens/api-collection-files/managed.bin',
  )
  assert.equal(
    httpState.requestBodyFormData[2].file.path,
    'C:/FlowLens/request-draft-cache-old/external.bin',
  )
  assert.equal(wsState.draftFile, null)
  assert.equal(wsState.draftType, 'binary-file')
  assert.equal(wsState.draftText, 'keep websocket text draft')
})
