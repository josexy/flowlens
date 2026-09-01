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
