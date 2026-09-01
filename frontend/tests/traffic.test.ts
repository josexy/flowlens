import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick, ref } from 'vue'
// @ts-expect-error Monaco does not publish declarations for its internal Monarch compiler.
import { compile as compileMonarchLanguage } from 'monaco-editor/editor/standalone/common/monarch/monarchCompile.js'
// @ts-expect-error Monaco does not publish declarations for its internal Monarch tokenizer.
import { MonarchTokenizer } from 'monaco-editor/editor/standalone/common/monarch/monarchLexer.js'
import {
  PROCESS_CATEGORY_UNAVAILABLE_KEY,
  advanceTrafficEvictionWatermark,
  getTrafficCapabilities,
  getTrafficCategoryHost,
  getTrafficProtocol,
  getTrafficProcessCategory,
  getTrafficTarget,
  getTrafficTargetPort,
  getTrafficTotalDurationMicros,
  getTrafficTotalSizeBytes,
  getTrafficTypeLabel,
  isHTTPTraffic,
  isHARExportableHistoryFormat,
  isRawTCPTraffic,
  isTrafficEntryEvicted,
  isWebSocketTraffic,
  parseHostPort,
  trafficMatchesCategoryFilters,
  trafficMatchesSearch,
  type TrafficEntryLike,
  type TrafficProcessLike,
} from '../src/utils/traffic.js'
import {
  TRAFFIC_TABLE_COLUMN_KEYS,
  createTrafficTableColumns,
  getVisibleTrafficColumns,
  normalizeHiddenTrafficColumnKeys,
  reorderVisibleTrafficColumns,
} from '../src/utils/traffic-table-columns.js'
import {
  buildCategoryFacetSummaries,
  buildProcessSummary,
  filterProcessSummary,
} from '../src/utils/traffic-category.js'
import {
  editableRowsToHeaderFields,
  editableRowsToHeadersRecord,
  convertRequestRouteHeaders,
  editableHeaderFieldsToRows,
  findInvalidRequestHeaderName,
  formatHeaderFieldsAsJson,
  formatHeaderFieldsAsText,
  firstHeaderFieldValue,
  headerFieldsToEditableRows,
  inferRequestProtocolFromHTTPMessage,
  normalizeRequestProtocol,
  normalizeHeaderFields,
  sortHeaderFields,
} from '../src/utils/headers.js'
import {
  countRequestCookieRows,
  requestCookiesRecord,
  responseCookiesRecord,
} from '../src/utils/cookies.js'
import {
  UNKNOWN_FORMATTED_VALUE,
  formatDateTimeLocal,
  formatDurationMicros,
  formatFileSize,
  formatUnixMicrosLocal,
  getLogicalHTTPRequestStartLineSize,
  getLogicalHTTPResponseStartLineSize,
  sumKnownByteSizes,
  summarizeHTTPMessageSize,
} from '../src/utils/format.js'
import {
  applyTrafficEntryPatch,
  getNewTerminalHTTPMessageSides,
  TerminalBodyRefreshQueue,
} from '../src/utils/traffic-patch.js'
import {
  RAW_HTTP_BINARY_BODY,
  formatRawHTTPRequest,
  formatRawHTTPResponse,
} from '../src/utils/httpRaw.js'
import { parseUrlQuery } from '../src/utils/urlHighlight.js'
import {
  countRequestQueryRows,
  parseRequestQueryRows,
  replaceRequestURLQuery,
  serializeRequestQueryRows,
} from '../src/utils/requestQuery.js'
import { useRequestQuerySync } from '../src/composables/useRequestQuerySync.js'
import { getReadonlyMonacoAppendText } from '../src/components/common/monacoTextUpdate.js'
import { remeasureMonacoFontsAfterLoad } from '../src/components/common/monacoFontMeasurements.js'
import {
  MONACO_LARGE_TEXT_THRESHOLD_CHARS,
  MONACO_LONG_LINE_THRESHOLD_CHARS,
  MONACO_WRAPPED_CHUNK_SIZE_CHARS,
  getMonacoWrappedTextChunk,
  requiresMonacoLargeTextOptimizations,
} from '../src/components/common/monacoLargeText.js'
import { syncMonacoModelBracketPairColorization } from '../src/components/common/monacoModelOptions.js'
import { HTTP_LANGUAGE } from '../src/components/common/monacoLightLanguages.js'
import {
  getFlowLensPythonCompletions,
  getFlowLensPythonHover,
  getFlowLensPythonSignature,
} from '../src/components/common/monacoFlowLensPythonApi.js'
import { createBatchedAppEventRouter } from '../src/runtime/batchedAppEvents.js'
import {
  buildRequestBodyFileDropTarget,
  buildRequestFormDataFileDropTarget,
  parseRequestFileDropPayload,
} from '../src/utils/requestFileDrop.js'
import { IncrementalBase64Encoder } from '../src/utils/incrementalBase64.js'
import {
  parseLocalDataClearedPayload,
  syncLocalDataClearedWindow,
} from '../src/utils/localDataCleared.js'
import {
  createKeyedLatestOperationGuard,
  createLatestOperationGuard,
  createOperationGenerationGuard,
} from '../src/utils/latestOperation.js'
import { toWebSocketDisplayMessage } from '../src/utils/websocket.js'
import {
  HTTPMessageState,
  ProcessStatus,
  type TrafficEntry,
} from '../bindings/github.com/josexy/flowlens/backend/services/proxy_service/models.js'

type CategoryTrafficEntry = Parameters<typeof buildProcessSummary>[0][number]

function createDeferred<Value>() {
  let resolve!: (value: Value) => void
  const promise = new Promise<Value>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function completionNames(source: string) {
  return getFlowLensPythonCompletions(source, source.length).items.map((item) => item.name)
}

test('FlowLens Python API completion resolves hook arguments and nested members', () => {
  assert.deepEqual(
    completionNames('def onRequest(context, request):\n    request.'),
    [
      'method',
      'url',
      'scheme',
      'host',
      'port',
      'path',
      'queries',
      'content_type',
      'headers',
      'body',
    ],
  )
  assert.deepEqual(
    completionNames('def onRequest(ctx, req):\n    req.headers.'),
    ['get', 'get_all', 'set', 'add', 'remove', 'clear'],
  )
  assert.deepEqual(
    completionNames('def onRequest(ctx, req):\n    req.body.'),
    ['kind', 'value', 'write_file'],
  )
  assert.deepEqual(
    completionNames('def onRequest(ctx, req):\n    req.queries.'),
    ['get', 'get_all', 'set', 'add', 'remove', 'clear', 'to_string'],
  )
  assert.deepEqual(
    completionNames('def onResponse(ctx, res):\n    res.request.headers.'),
    ['get', 'get_all'],
  )
  assert.deepEqual(
    completionNames('def onResponse(ctx, res):\n    res.request.queries.'),
    ['get', 'get_all', 'to_string'],
  )
  assert.deepEqual(
    completionNames('def onResponse(ctx, res):\n    res.request.body.'),
    ['kind', 'value', 'write_file'],
  )
  assert.deepEqual(completionNames('def onResponse(ctx, res):\n    res.'), [
    'code',
    'protocol',
    'status_text',
    'content_type',
    'headers',
    'trailers',
    'body',
    'request',
  ])
  assert.deepEqual(
    completionNames('def onResponse(ctx, res):\n    ctx.log.'),
    ['debug', 'info', 'warning', 'error'],
  )
})

test('FlowLens Python API completion recognizes annotations, aliases, classes, and mappings', () => {
  assert.deepEqual(
    completionNames('def helper(req: Request):\n    headers = req.headers\n    headers.'),
    ['get', 'get_all', 'set', 'add', 'remove', 'clear'],
  )
  assert.deepEqual(completionNames('FileDescriptor.'), ['from_file'])
  assert.deepEqual(completionNames('context.params.'), ['get', 'keys', 'items', 'values'])
  assert.deepEqual(
    completionNames('context.shared.'),
    ['get', 'keys', 'items', 'values', 'setdefault', 'update', 'pop', 'clear'],
  )
  assert.ok(completionNames('Bo').includes('Body'))
})

test('FlowLens Python API completion supplies snippets, signatures, and hover details', () => {
  const completion = getFlowLensPythonCompletions('request.headers.se', 'request.headers.se'.length)
  assert.equal(completion.replaceLength, 2)
  assert.equal(completion.items[0]?.name, 'set')
  assert.equal(completion.items[0]?.insertText, "set('${1:name}', '${2:value}')")

  const call = "request.headers.set('X-Test', "
  const signature = getFlowLensPythonSignature(call, call.length)
  assert.equal(signature?.label, 'Headers.set(name: str, value: str) -> None')
  assert.equal(signature?.activeParameter, 1)

  const constructor = 'Body('
  assert.equal(
    getFlowLensPythonSignature(constructor, constructor.length)?.label,
    'Body(kind: str = "none", value: Any = None)',
  )

  const multipartConstructor = 'MultipartPart('
  assert.equal(
    getFlowLensPythonSignature(multipartConstructor, multipartConstructor.length)?.label,
    'MultipartPart(name: str, value: str = "", file: FileDescriptor | None = None, enabled: bool = True, filename: str = "")',
  )

  const hoverSource = 'response.body'
  const hover = getFlowLensPythonHover(hoverSource, hoverSource.indexOf('body') + 1)
  assert.equal(hover?.signature, '(property) Response.body: Body')
  assert.match(hover?.documentation ?? '', /SSE/)
})

test('traffic table hidden columns are canonical and keep one column visible', () => {
  assert.deepEqual(
    normalizeHiddenTrafficColumnKeys([' process ', 'unknown', 'host', 'process']),
    ['host', 'process'],
  )
  assert.deepEqual(
    normalizeHiddenTrafficColumnKeys([...TRAFFIC_TABLE_COLUMN_KEYS]),
    TRAFFIC_TABLE_COLUMN_KEYS.filter((key) => key !== 'id'),
  )
})

test('traffic table visible columns retain column objects and session widths', () => {
  const columns = createTrafficTableColumns()
  const processColumn = columns.find((column) => column.key === 'process')!
  processColumn.width = 222

  const visible = getVisibleTrafficColumns(columns, ['process', 'destination'])
  assert.equal(visible.some((column) => column.key === 'process'), false)
  assert.equal(visible.some((column) => column.key === 'destination'), false)

  const restored = getVisibleTrafficColumns(columns, [])
  assert.equal(restored.find((column) => column.key === 'process'), processColumn)
  assert.equal(processColumn.width, 222)
})

test('traffic table places process between path and status by default', () => {
  assert.deepEqual(
    createTrafficTableColumns()
      .slice(3, 6)
      .map((column) => column.key),
    ['path', 'process', 'statusCode'],
  )
})

test('traffic table reorders visible columns without moving hidden column slots', () => {
  const columns = createTrafficTableColumns()
  const processColumn = columns.find((column) => column.key === 'process')!
  processColumn.width = 240

  const reordered = reorderVisibleTrafficColumns(columns, ['process'], 'id', 1)
  assert.deepEqual(
    getVisibleTrafficColumns(reordered, ['process'])
      .slice(0, 3)
      .map((column) => column.key),
    ['method', 'id', 'host'],
  )
  assert.equal(reordered[4], processColumn)
  assert.equal(reordered[4]?.width, 240)
})

test('traffic table appends duration and size as the rightmost columns', () => {
  assert.deepEqual(
    createTrafficTableColumns()
      .slice(-2)
      .map((column) => column.key),
    ['duration', 'size'],
  )
})

test('traffic table metrics use the overview duration and size semantics', () => {
  const entry: TrafficEntryLike = {
    type: 'https',
    method: 'GET',
    url: 'https://example.com/items',
    status: '200 OK',
    request: {
      proto: 'HTTP/2.0',
      metrics: {
        startedAtMicros: 1_000_000,
        endedAtMicros: 1_000_100,
        headerSize: 107,
        bodySize: 0,
      },
    },
    response: {
      proto: 'HTTP/2.0',
      metrics: {
        startedAtMicros: 1_000_200,
        endedAtMicros: 1_000_584,
        headerSize: 531,
        bodySize: 425,
      },
    },
  }

  assert.equal(getTrafficTotalDurationMicros(entry), 584)
  assert.equal(getTrafficTotalSizeBytes(entry), 1067)
})

test('traffic table metrics stay unknown until all required values are known', () => {
  const pending: TrafficEntryLike = {
    type: 'https',
    method: 'GET',
    url: 'https://example.com/',
    status: '',
    request: {
      proto: 'HTTP/1.1',
      metrics: {
        startedAtMicros: 1,
        endedAtMicros: 2,
        headerSize: 10,
        bodySize: 0,
      },
    },
    response: {
      proto: 'HTTP/1.1',
      metrics: {
        startedAtMicros: 3,
        endedAtMicros: -1,
        headerSize: -1,
        bodySize: -1,
      },
    },
  }

  assert.equal(getTrafficTotalDurationMicros(pending), null)
  assert.equal(getTrafficTotalSizeBytes(pending), null)
  assert.equal(getTrafficTotalDurationMicros({ type: 'tcp' }), null)
  assert.equal(getTrafficTotalSizeBytes({ type: 'tcp' }), null)
})

function tokenizeRawHTTPLineAfterStartLine(startLine: string, line: string) {
  const configurationService = {
    getValue: () => 20_000,
    onDidChangeConfiguration: () => ({ dispose() {} }),
  }
  const tokenizer = new MonarchTokenizer(
    {},
    {},
    'flowlens-http',
    compileMonarchLanguage('flowlens-http', HTTP_LANGUAGE),
    configurationService,
  )
  try {
    const startResult = tokenizer.tokenize(startLine, true, tokenizer.getInitialState())
    return tokenizer.tokenize(line, true, startResult.endState).tokens
  } finally {
    tokenizer.dispose()
  }
}

test('raw HTTP tokenizer highlights the first header after request and response start lines', () => {
  assert.equal(
    tokenizeRawHTTPLineAfterStartLine('GET /.sse HTTP/2.0', 'host: echo.websocket.org')[0]
      ?.type,
    'attribute.name.http',
  )
  assert.equal(
    tokenizeRawHTTPLineAfterStartLine(
      'HTTP/2.0 200',
      'access-control-allow-origin: *',
    )[0]?.type,
    'attribute.name.http',
  )
})

test('raw HTTP/1 request preserves escaped origin-form targets, query strings, and fields', () => {
  assert.equal(
    formatRawHTTPRequest({
      method: 'GET',
      url: 'https://example.test/a%2Fb?q=hello%20world&empty=#fragment',
      host: 'example.test',
      protocol: 'HTTP/1.1',
      headerFields: [
        { name: 'X-Repeat', value: 'one' },
        { name: 'x-empty', value: '' },
        { name: 'X-Repeat', value: 'two' },
      ],
    }),
    [
      'GET /a%2Fb?q=hello%20world&empty= HTTP/1.1',
      'X-Repeat: one',
      'x-empty: ',
      'X-Repeat: two',
      '',
      '',
    ].join('\r\n'),
  )
})

test('URL query parsing decodes display fields while preserving raw order and duplicates', () => {
  assert.deepEqual(
    parseUrlQuery(
      'https://example.test/search?lang=zh-CN&tag=first&tag=second&message=hello+world&path=a%2Fb#section',
    ),
    {
      rawQuery: 'lang=zh-CN&tag=first&tag=second&message=hello+world&path=a%2Fb',
      fields: [
        { name: 'lang', value: 'zh-CN' },
        { name: 'tag', value: 'first' },
        { name: 'tag', value: 'second' },
        { name: 'message', value: 'hello world' },
        { name: 'path', value: 'a/b' },
      ],
    },
  )
})

test('URL query parsing handles relative targets, key-only fields, empty values, and no query', () => {
  assert.deepEqual(parseUrlQuery('/items?flag&empty=&=unnamed&bad=%ZZ'), {
    rawQuery: 'flag&empty=&=unnamed&bad=%ZZ',
    fields: [
      { name: 'flag', value: '' },
      { name: 'empty', value: '' },
      { name: '', value: 'unnamed' },
      { name: 'bad', value: '%ZZ' },
    ],
  })
  assert.deepEqual(parseUrlQuery('/items#ignored?not-a-query'), {
    rawQuery: '',
    fields: [],
  })
})

test('request query parsing preserves duplicate order and creates an editable empty row', () => {
  assert.deepEqual(
    parseRequestQueryRows(
      'https://example.test/search?tag=first&tag=second&message=hello+world&path=a%2Fb#section',
    ),
    [
      { key: 'tag', value: 'first', enabled: true },
      { key: 'tag', value: 'second', enabled: true },
      { key: 'message', value: 'hello world', enabled: true },
      { key: 'path', value: 'a/b', enabled: true },
    ],
  )
  assert.deepEqual(parseRequestQueryRows('wss://example.test/socket'), [
    { key: '', value: '', enabled: true },
  ])
})

test('request query serialization omits disabled and blank rows and counts sent parameters', () => {
  const rows = [
    { key: ' tag ', value: 'first value', enabled: true },
    { key: 'tag', value: 'second', enabled: true },
    { key: 'disabled', value: 'ignored', enabled: false },
    { key: '', value: 'ignored', enabled: true },
    { key: 'flag', value: '', enabled: true },
  ]

  assert.equal(serializeRequestQueryRows(rows), 'tag=first+value&tag=second&flag=')
  assert.equal(countRequestQueryRows(rows), 3)
})

test('request query replacement preserves fragments and removes an empty query', () => {
  assert.equal(
    replaceRequestURLQuery('wss://example.test/socket?old=1#messages', [
      { key: 'channel', value: 'updates', enabled: true },
      { key: 'token', value: 'secret', enabled: false },
    ]),
    'wss://example.test/socket?channel=updates#messages',
  )
  assert.equal(
    replaceRequestURLQuery('https://example.test/items?old=1#result', [
      { key: '', value: '', enabled: true },
    ]),
    'https://example.test/items#result',
  )
})

test('request query sync preserves URL encoding until the table is edited', async () => {
  const url = ref('https://example.test/search?q=hello%20world#result')
  const rows = ref([{ key: '', value: '', enabled: true }])
  const { queryCount } = useRequestQuerySync(url, rows)

  await nextTick()
  assert.deepEqual(rows.value, [{ key: 'q', value: 'hello world', enabled: true }])
  assert.equal(url.value, 'https://example.test/search?q=hello%20world#result')
  assert.equal(queryCount.value, 1)

  rows.value[0]!.value = 'updated value'
  await nextTick()
  assert.equal(url.value, 'https://example.test/search?q=updated+value#result')

  url.value = 'https://example.test/search?tag=first&tag=second#result'
  await nextTick()
  assert.deepEqual(rows.value, [
    { key: 'tag', value: 'first', enabled: true },
    { key: 'tag', value: 'second', enabled: true },
  ])
})

test('raw HTTP/1 CONNECT uses an authority-form request target', () => {
  assert.equal(
    formatRawHTTPRequest({
      method: 'CONNECT',
      url: 'https://tunnel.example:8443/ignored?q=1',
      host: 'fallback.example:443',
      protocol: 'HTTP/1.1',
      headerFields: [],
    }),
    'CONNECT tunnel.example:8443 HTTP/1.1\r\n\r\n',
  )
})

test('raw HTTP/1 response preserves the reason phrase', () => {
  assert.equal(
    formatRawHTTPResponse({
      status: '418 I am a teapot',
      statusCode: 418,
      protocol: 'HTTP/1.1',
      headerFields: [{ name: 'Content-Length', value: '0' }],
    }),
    'HTTP/1.1 418 I am a teapot\r\nContent-Length: 0\r\n\r\n',
  )
})

test('raw HTTP/2 request converts pseudo-headers without losing unknown fields', () => {
  assert.equal(
    formatRawHTTPRequest({
      method: 'POST',
      url: 'https://fallback.test/ignored',
      protocol: 'HTTP/2.0',
      headerFields: [
        { name: ':method', value: 'GET' },
        { name: ':authority', value: 'api.example.test' },
        { name: ':scheme', value: 'https' },
        { name: ':path', value: '/v1/items?q=a%2Fb' },
        { name: 'X-Repeat', value: 'one' },
        { name: ':protocol', value: 'websocket' },
        { name: 'X-Repeat', value: 'two' },
      ],
    }),
    [
      'GET /v1/items?q=a%2Fb HTTP/2.0',
      'host: api.example.test',
      'X-Repeat: one',
      'protocol: websocket',
      'X-Repeat: two',
      '',
      '',
    ].join('\r\n'),
  )
})

test('raw HTTP/2 response uses status pseudo-header and omits a reason phrase', () => {
  assert.equal(
    formatRawHTTPResponse({
      status: '200 OK',
      statusCode: 200,
      protocol: 'HTTP/2.0',
      headerFields: [
        { name: ':status', value: '204' },
        { name: 'content-type', value: 'text/plain' },
      ],
    }),
    'HTTP/2.0 204\r\ncontent-type: text/plain\r\n\r\n',
  )
})

test('raw SSE can append after Monaco normalizes mixed HTTP and body line endings', () => {
  const previousValue = formatRawHTTPResponse({
    status: '200 OK',
    statusCode: 200,
    protocol: 'HTTP/1.1',
    headerFields: [{ name: 'content-type', value: 'text/event-stream' }],
    body: 'data: first\n\n',
  })
  const nextValue = formatRawHTTPResponse({
    status: '200 OK',
    statusCode: 200,
    protocol: 'HTTP/1.1',
    headerFields: [{ name: 'content-type', value: 'text/event-stream' }],
    body: 'data: first\n\ndata: second\n\n',
  })
  const monacoModelValue = previousValue.replace(/\r\n|\r|\n/g, '\r\n')

  assert.notEqual(monacoModelValue.length, previousValue.length)
  assert.equal(
    getReadonlyMonacoAppendText(previousValue, nextValue, monacoModelValue),
    'data: second\n\n',
  )
})

test('Monaco waits for the configured code font before remeasuring cached widths', async () => {
  let finishFontLoad!: () => void
  const fontLoad = new Promise<void>((resolve) => {
    finishFontLoad = resolve
  })
  let remeasureCalls = 0
  const fontFaceSet = {
    load(font: string, text?: string) {
      assert.equal(
        font,
        "400 13px 'JetBrains Mono', 'Cascadia Mono', Consolas, monospace",
      )
      assert.equal(text, 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789')
      return fontLoad
    },
  }
  const pending = remeasureMonacoFontsAfterLoad(
    { editor: { remeasureFonts: () => remeasureCalls++ } },
    {
      fontFamily: "'JetBrains Mono', 'Cascadia Mono', Consolas, monospace",
      fontSize: 13,
      fontWeight: '400',
    },
    fontFaceSet,
  )

  assert.equal(remeasureCalls, 0)
  finishFontLoad()
  await pending
  assert.equal(remeasureCalls, 1)
})

test('Monaco coalesces identical pending font loads and remeasures after load failure', async () => {
  let finishFontLoad!: () => void
  const fontLoad = new Promise<void>((resolve) => {
    finishFontLoad = resolve
  })
  let loadCalls = 0
  let remeasureCalls = 0
  const monaco = { editor: { remeasureFonts: () => remeasureCalls++ } }
  const request = { fontFamily: 'Delayed Mono, monospace', fontSize: 14 }
  const fontFaceSet = {
    load() {
      loadCalls++
      return fontLoad
    },
  }

  const first = remeasureMonacoFontsAfterLoad(monaco, request, fontFaceSet)
  const second = remeasureMonacoFontsAfterLoad(monaco, request, fontFaceSet)
  assert.equal(first, second)
  assert.equal(loadCalls, 1)
  assert.equal(remeasureCalls, 0)
  finishFontLoad()
  await first
  assert.equal(remeasureCalls, 1)

  await remeasureMonacoFontsAfterLoad(monaco, request, {
    load: () => Promise.reject(new Error('font unavailable')),
  })
  assert.equal(remeasureCalls, 2)
})

test('Monaco large-text mode covers large documents and pathological long lines', () => {
  assert.equal(MONACO_LONG_LINE_THRESHOLD_CHARS, MONACO_WRAPPED_CHUNK_SIZE_CHARS)
  assert.equal(requiresMonacoLargeTextOptimizations('short\ntext'), false)
  assert.equal(requiresMonacoLargeTextOptimizations('a'.repeat(20_000)), false)
  assert.equal(
    requiresMonacoLargeTextOptimizations(
      `${'a'.repeat(MONACO_LONG_LINE_THRESHOLD_CHARS - 1)}\nshort`,
    ),
    false,
  )
  assert.equal(
    requiresMonacoLargeTextOptimizations('a'.repeat(MONACO_LONG_LINE_THRESHOLD_CHARS)),
    true,
  )
  assert.equal(
    requiresMonacoLargeTextOptimizations(
      `${'a'.repeat(MONACO_LONG_LINE_THRESHOLD_CHARS - 1)}\r\nshort`,
    ),
    false,
  )
  assert.equal(
    requiresMonacoLargeTextOptimizations(
      'a\n'.repeat(Math.ceil(MONACO_LARGE_TEXT_THRESHOLD_CHARS / 2)),
    ),
    true,
  )
})

test('Monaco wrapped chunks preserve the complete source and clamp page indexes', () => {
  const source = 'a'.repeat(MONACO_WRAPPED_CHUNK_SIZE_CHARS * 2 + 17)
  const singleChunk = getMonacoWrappedTextChunk(
    'a'.repeat(MONACO_WRAPPED_CHUNK_SIZE_CHARS),
    0,
  )
  const multipleChunks = getMonacoWrappedTextChunk(
    'a'.repeat(MONACO_WRAPPED_CHUNK_SIZE_CHARS + 1),
    0,
  )
  const first = getMonacoWrappedTextChunk(source, -1)
  const chunks = Array.from({ length: first.count }, (_, index) =>
    getMonacoWrappedTextChunk(source, index),
  )
  const last = getMonacoWrappedTextChunk(source, Number.MAX_SAFE_INTEGER)
  const positiveInfinity = getMonacoWrappedTextChunk(source, Number.POSITIVE_INFINITY)
  const negativeInfinity = getMonacoWrappedTextChunk(source, Number.NEGATIVE_INFINITY)
  const empty = getMonacoWrappedTextChunk('', Number.NaN)

  assert.equal(first.index, 0)
  assert.equal(last.index, first.count - 1)
  assert.equal(positiveInfinity.index, first.count - 1)
  assert.equal(negativeInfinity.index, 0)
  assert.deepEqual(empty, { text: '', index: 0, count: 1, start: 0, end: 0 })
  assert.equal(singleChunk.count, 1)
  assert.equal(multipleChunks.count, 2)
  assert.equal(chunks.map((chunk) => chunk.text).join(''), source)
  assert.equal(chunks[0]?.start, 0)
  assert.equal(chunks.at(-1)?.end, source.length)
  assert.equal(chunks.every((chunk) => chunk.text.length <= MONACO_WRAPPED_CHUNK_SIZE_CHARS), true)
})

test('Monaco wrapped chunk boundaries do not split CRLF or UTF-16 surrogate pairs', () => {
  const prefix = 'a'.repeat(MONACO_WRAPPED_CHUNK_SIZE_CHARS - 1)
  const crlfSource = `${prefix}\r\nend`
  const emojiSource = `${prefix}😀end`

  const crlfChunks = [
    getMonacoWrappedTextChunk(crlfSource, 0),
    getMonacoWrappedTextChunk(crlfSource, 1),
  ]
  const emojiChunks = [
    getMonacoWrappedTextChunk(emojiSource, 0),
    getMonacoWrappedTextChunk(emojiSource, 1),
  ]

  assert.equal(crlfChunks[0]?.text.endsWith('\r'), false)
  assert.equal(crlfChunks[1]?.text.startsWith('\r\n'), true)
  assert.equal(crlfChunks.map((chunk) => chunk.text).join(''), crlfSource)
  assert.equal(emojiChunks[0]?.text.endsWith('\ud83d'), false)
  assert.equal(emojiChunks[1]?.text.startsWith('😀'), true)
  assert.equal(emojiChunks.map((chunk) => chunk.text).join(''), emojiSource)
})

test('Monaco bracket colorization is synchronized to the text model', () => {
  let currentOptions = {
    enabled: true,
    independentColorPoolPerBracketType: false,
  }
  const updates: Array<{
    bracketColorizationOptions?: {
      enabled: boolean
      independentColorPoolPerBracketType: boolean
    }
  }> = []
  const model = {
    getOptions: () => ({ bracketPairColorizationOptions: currentOptions }),
    updateOptions: (options: (typeof updates)[number]) => {
      updates.push(options)
      if (options.bracketColorizationOptions) {
        currentOptions = options.bracketColorizationOptions
      }
    },
  }

  syncMonacoModelBracketPairColorization(model, {
    enabled: false,
    independentColorPoolPerBracketType: false,
  })
  syncMonacoModelBracketPairColorization(model, {
    enabled: false,
    independentColorPoolPerBracketType: false,
  })
  // Monaco reapplies its default model options when the model language changes.
  currentOptions = { enabled: true, independentColorPoolPerBracketType: false }
  syncMonacoModelBracketPairColorization(model, {
    enabled: false,
    independentColorPoolPerBracketType: false,
  })

  assert.deepEqual(updates, [
    {
      bracketColorizationOptions: {
        enabled: false,
        independentColorPoolPerBracketType: false,
      },
    },
    {
      bracketColorizationOptions: {
        enabled: false,
        independentColorPoolPerBracketType: false,
      },
    },
  ])
})

test('raw HTTP formatter supports fallback fields and appends bodies only when available', () => {
  const input = {
    method: 'POST',
    url: 'https://example.test/submit',
    protocol: 'HTTP/1.1',
    headerFields: [{ name: 'Fallback-Header', value: '' }],
  }
  const headerOnly = formatRawHTTPRequest(input)
  assert.equal(
    headerOnly,
    'POST /submit HTTP/1.1\r\nFallback-Header: \r\n\r\n',
  )
  assert.equal(formatRawHTTPRequest({ ...input, body: 'hello\nworld' }), `${headerOnly}hello\nworld`)
  assert.equal(
    formatRawHTTPRequest({ ...input, body: 'AAEC', bodyEncoding: 'base64' }),
    `${headerOnly}${RAW_HTTP_BINARY_BODY}`,
  )
  assert.equal(
    formatRawHTTPRequest({ ...input, headerFields: null }),
    'POST /submit HTTP/1.1\r\n\r\n',
  )
})

test('batched app event router preserves order and reports bounded drops', () => {
  const router = createBatchedAppEventRouter()
  const received: number[] = []
  const dropped: number[] = []
  const off = router.subscribe(
    'traffic:entry',
    (data) => received.push((data as { id: number }).id),
    (count) => dropped.push(count),
  )

  router.dispatch({
    events: [
      { name: 'traffic:entry', data: { id: 1 } },
      { name: 'traffic:patch', data: { trafficId: 1 } },
      { name: 'traffic:entry', data: { id: 2 } },
    ],
    dropped: { 'traffic:entry': 3 },
  })

  assert.deepEqual(received, [1, 2])
  assert.deepEqual(dropped, [3])

  off()
  router.dispatch({ events: [{ name: 'traffic:entry', data: { id: 4 } }] })
  assert.deepEqual(received, [1, 2])
  assert.equal(router.listenerCount, 0)
})

test('traffic reset marker clears old batch state before later traffic events', () => {
  const router = createBatchedAppEventRouter()
  const visible: number[] = []
  router.subscribe('traffic:entry', (data) => visible.push((data as { id: number }).id))
  router.subscribe('traffic:reset', () => visible.splice(0))

  router.dispatch({ events: [{ name: 'traffic:entry', data: { id: 1 } }] })
  assert.deepEqual(visible, [1])

  router.dispatch({
    events: [
      { name: 'traffic:reset', data: { captureGeneration: 2 } },
      { name: 'traffic:entry', data: { id: 1, url: 'https://new.example/' } },
    ],
  })
  assert.deepEqual(visible, [1])
})

test('traffic reset invalidates an in-flight statistics response', async () => {
  const guard = createLatestOperationGuard()
  const staleStatistics = createDeferred<number>()
  const committedStatistics: number[] = []

  const staleRequestToken = guard.begin()
  const staleRequest = staleStatistics.promise.then((value) => {
    if (guard.isCurrent(staleRequestToken)) {
      committedStatistics.push(value)
    }
  })

  // The traffic:reset handler invalidates every request begun before the reset.
  guard.invalidate()
  staleStatistics.resolve(42)
  await staleRequest
  assert.equal(committedStatistics.length, 0)

  const currentStatistics = createDeferred<number>()
  const currentRequestToken = guard.begin()
  const currentRequest = currentStatistics.promise.then((value) => {
    if (guard.isCurrent(currentRequestToken)) {
      committedStatistics.push(value)
    }
  })
  currentStatistics.resolve(0)
  await currentRequest
  assert.deepEqual(committedStatistics, [0])
})

test('request body file resolution keeps the newest operation and respects clear boundaries', async () => {
  const guard = createLatestOperationGuard()
  let selectedFile: string | null = null

  async function resolveFile(promise: Promise<string>) {
    const operationToken = guard.begin()
    const file = await promise
    if (guard.isCurrent(operationToken)) {
      selectedFile = file
    }
  }

  const firstDrop = createDeferred<string>()
  const secondDrop = createDeferred<string>()
  const firstRequest = resolveFile(firstDrop.promise)
  const secondRequest = resolveFile(secondDrop.promise)
  secondDrop.resolve('B.bin')
  await secondRequest
  firstDrop.resolve('A.bin')
  await firstRequest
  assert.equal(selectedFile, 'B.bin')

  const clearedDrop = createDeferred<string>()
  const clearedRequest = resolveFile(clearedDrop.promise)
  selectedFile = null
  guard.invalidate() // User pressed the file clear action.
  clearedDrop.resolve('restored-after-clear.bin')
  await clearedRequest
  assert.equal(selectedFile, null)

  const cacheClearedDrop = createDeferred<string>()
  const cacheClearedRequest = resolveFile(cacheClearedDrop.promise)
  guard.invalidate() // app:local-data-cleared removed the Request draft cache.
  cacheClearedDrop.resolve('deleted-cache-file.bin')
  await cacheClearedRequest
  assert.equal(selectedFile, null)
})

test('form-data file resolution is invalidated by row type and cache changes', async () => {
  const guard = createKeyedLatestOperationGuard<string>()
  const selectedFiles = new Map<string, string>()

  async function resolveRowFile(rowId: string, promise: Promise<string>) {
    const operationToken = guard.begin(rowId)
    const file = await promise
    if (guard.isCurrent(operationToken)) {
      selectedFiles.set(rowId, file)
    }
  }

  const typeChangedFile = createDeferred<string>()
  const typeChangedRequest = resolveRowFile('row-a', typeChangedFile.promise)
  guard.invalidate('row-a') // The row was switched back to text.
  typeChangedFile.resolve('unexpected.bin')
  await typeChangedRequest
  assert.equal(selectedFiles.has('row-a'), false)

  const cachedFile = createDeferred<string>()
  const externalFile = createDeferred<string>()
  const cachedRequest = resolveRowFile('row-a', cachedFile.promise)
  const externalRequest = resolveRowFile('row-b', externalFile.promise)
  guard.invalidateAll() // app:local-data-cleared invalidates all pending row resolutions.
  cachedFile.resolve('deleted-cache-file.bin')
  externalFile.resolve('also-stale.bin')
  await Promise.all([cachedRequest, externalRequest])
  assert.equal(selectedFiles.size, 0)
})

test('request recovery generation rejects cache-backed results completed after clearing', async () => {
  const guard = createOperationGenerationGuard()
  const recoveredTabs: string[] = []

  async function recoverRequest(promise: Promise<string>) {
    const recoveryGeneration = guard.capture()
    const recoveredFile = await promise
    if (!guard.isCurrent(recoveryGeneration)) {
      return null
    }
    recoveredTabs.push(recoveredFile)
    return recoveredFile
  }

  const staleRecovery = createDeferred<string>()
  const staleRequest = recoverRequest(staleRecovery.promise)
  guard.invalidate() // clearRequestDraftCacheFileReferences() crossed the cache boundary.
  staleRecovery.resolve('deleted-request-draft-cache/body.bin')
  assert.equal(await staleRequest, null)
  assert.deepEqual(recoveredTabs, [])

  const currentRecovery = createDeferred<string>()
  const currentRequest = recoverRequest(currentRecovery.promise)
  currentRecovery.resolve('request-draft-cache/current.bin')
  assert.equal(await currentRequest, 'request-draft-cache/current.bin')
  assert.deepEqual(recoveredTabs, ['request-draft-cache/current.bin'])
})

test('incremental base64 encoder preserves byte boundaries without rebuilding prior bytes', () => {
  const encoder = new IncrementalBase64Encoder()
  encoder.append(Uint8Array.from([0, 1]))
  assert.equal(encoder.value(), Buffer.from([0, 1]).toString('base64'))

  encoder.append(Uint8Array.from([2, 3, 4, 5]))
  assert.equal(encoder.value(), Buffer.from([0, 1, 2, 3, 4, 5]).toString('base64'))
  assert.equal(encoder.byteLength, 6)

  encoder.append(Uint8Array.from([6]))
  assert.equal(encoder.value(), Buffer.from([0, 1, 2, 3, 4, 5, 6]).toString('base64'))
  assert.equal(encoder.byteLength, 7)
})

test('WebSocket display messages preserve payload metadata without retention bookkeeping', () => {
  const message = toWebSocketDisplayMessage(
    {
      direction: 'receive',
      msgType: 'binary',
      data: 'AAEC',
      dataSize: 32 * 1024 * 1024,
    },
    'large',
    123,
  )

  assert.deepEqual(message, {
    id: 'large',
    direction: 'receive',
    msgType: 'binary',
    data: 'AAEC',
    dataSize: 32 * 1024 * 1024,
    createdAt: 123,
  })
})

test('traffic patches update only selected fields and reject stale revisions', () => {
  const entry = {
    id: 42,
    revision: 1,
    type: 'https',
    startedAt: '2026-08-12T09:56:01.341Z',
    method: 'GET',
    url: 'https://example.test/',
    host: 'example.test',
    path: '/',
    statusCode: 200,
    status: '200 OK',
    metadata: { localConnectionEstablishedAt: '', remoteConnectionEstablishedAt: '', requestProcessedAt: '', sslHandshakeCompletedAt: '' },
    request: {
      proto: 'HTTP/2.0',
      headerFields: [{ name: 'accept', value: '*/*' }],
      trailerFields: null,
      metrics: { startedAtMicros: 1, endedAtMicros: -1, headerSize: 11, bodySize: -1, state: HTTPMessageState.HTTPMessageStatePending },
    },
  }
  const updated = applyTrafficEntryPatch(entry, {
    trafficId: 42,
    revision: 2,
    responseHeaders: {
      statusCode: 204,
      status: '204 No Content',
      proto: 'HTTP/2.0',
      headerFields: [{ name: 'content-type', value: 'text/plain' }],
      headersTruncated: false,
      headerOrderUnavailable: false,
    },
    metrics: {
      request: { startedAtMicros: 1, endedAtMicros: 2, headerSize: 11, bodySize: 0, state: HTTPMessageState.HTTPMessageStateCompleted },
      response: { startedAtMicros: 3, endedAtMicros: -1, headerSize: 26, bodySize: -1, state: HTTPMessageState.HTTPMessageStatePending },
    },
  })

  assert.equal(updated.revision, 2)
  assert.equal(updated.statusCode, 204)
  assert.equal(updated.request?.metrics?.state, 'completed')
  assert.equal(updated.response?.metrics?.startedAtMicros, 3)
  assert.deepEqual(updated.response?.headerFields, [
    { name: 'content-type', value: 'text/plain' },
  ])
  assert.equal(updated.request?.headerFields, entry.request.headerFields)
  assert.equal(updated.metadata, entry.metadata)

  const stale = applyTrafficEntryPatch(updated, {
    trafficId: 42,
    revision: 1,
    process: { status: ProcessStatus.ProcessStatusResolved },
  })
  assert.equal(stale, updated)
})

test('terminal message transitions identify each newly finished side', () => {
  const pending = {
    id: 1,
    request: { metrics: { state: HTTPMessageState.HTTPMessageStatePending } },
    response: { metrics: { state: HTTPMessageState.HTTPMessageStatePending } },
  } as unknown as TrafficEntry
  const terminal = {
    id: 1,
    request: { metrics: { state: HTTPMessageState.HTTPMessageStateCompleted } },
    response: { metrics: { state: HTTPMessageState.HTTPMessageStateFailed } },
  } as unknown as TrafficEntry

  assert.deepEqual(getNewTerminalHTTPMessageSides(pending, terminal), ['request', 'response'])
  assert.deepEqual(getNewTerminalHTTPMessageSides(terminal, terminal), [])
  assert.deepEqual(
    getNewTerminalHTTPMessageSides(pending, {
      ...pending,
      request: { metrics: { state: HTTPMessageState.HTTPMessageStateCanceled } },
    } as unknown as TrafficEntry),
    ['request'],
  )
})

test('terminal body refresh waits for the initial load and coalesces duplicate patches', () => {
  const queue = new TerminalBodyRefreshQueue()
  queue.activate(42)

  assert.equal(queue.request(42, ['request'], true), false)
  assert.equal(queue.request(42, ['request'], true), false)
  assert.equal(queue.request(42, ['response'], true), false)
  assert.equal(queue.completeLoad(42), true)
  assert.equal(queue.completeLoad(42), false)
})

test('terminal body refresh queue isolates entries after selection changes', () => {
  const queue = new TerminalBodyRefreshQueue()
  queue.activate(1)
  assert.equal(queue.request(1, ['request'], true), false)

  queue.activate(2)
  assert.equal(queue.completeLoad(1), false)
  assert.equal(queue.request(1, ['response'], false), false)
  assert.equal(queue.request(2, ['response'], false), true)
})

test('traffic patches preserve unrelated data across process, trailer, and failure updates', () => {
  const entry: TrafficEntry = {
    id: 7,
    revision: 1,
    type: 'https',
    startedAt: '2026-08-12T09:56:01.341Z',
    method: 'GET',
    url: 'https://example.test/',
    host: 'example.test',
    path: '/',
    statusCode: 200,
    status: '200 OK',
    metadata: {
      localConnectionEstablishedAt: '',
      remoteConnectionEstablishedAt: '',
      requestProcessedAt: '',
      sslHandshakeCompletedAt: '',
    },
    request: { proto: 'HTTP/2.0', headerFields: [], trailerFields: [] },
    response: {
      proto: 'HTTP/2.0',
      headerFields: [{ name: 'content-type', value: 'text/plain' }],
      trailerFields: [],
    },
  }

  const withProcess = applyTrafficEntryPatch(entry, {
    trafficId: 7,
    revision: 2,
    process: {
      status: ProcessStatus.ProcessStatusResolved,
      pid: 42,
      displayName: 'Example',
    },
  })
  assert.equal(withProcess.metadata?.process?.pid, 42)
  assert.equal(withProcess.request, entry.request)
  assert.equal(withProcess.response, entry.response)

  const responseHeaders = withProcess.response?.headerFields
  const withTrailers = applyTrafficEntryPatch(withProcess, {
    trafficId: 7,
    revision: 3,
    responseTrailers: {
      trailerFields: [{ name: 'x-trace', value: 'done' }],
      trailersTruncated: false,
      trailerOrderUnavailable: false,
    },
  })
  assert.equal(withTrailers.response?.headerFields, responseHeaders)
  assert.deepEqual(withTrailers.response?.trailerFields, [{ name: 'x-trace', value: 'done' }])

  const failed = applyTrafficEntryPatch(withTrailers, {
    trafficId: 7,
    revision: 4,
    metrics: {
      response: {
        startedAtMicros: 1,
        endedAtMicros: 2,
        headerSize: 26,
        bodySize: -1,
        state: HTTPMessageState.HTTPMessageStateFailed,
      },
    },
    error: { timestamp: '2026-08-12T09:56:01.342Z', error: 'connection reset' },
  })
  assert.equal(failed.revision, 4)
  assert.equal(failed.error?.error, 'connection reset')
  assert.equal(failed.response?.metrics?.state, HTTPMessageState.HTTPMessageStateFailed)
  assert.equal(failed.response?.headerFields, responseHeaders)
})

test('overview timestamps use the local microsecond format', () => {
  const localTimestamp = new Date(2026, 7, 12, 17, 56, 1, 341)
  const preciseISOString = localTimestamp.toISOString().replace('.341Z', '.341480Z')

  assert.equal(formatDateTimeLocal(preciseISOString), '2026-08-12 17:56:01.341480')
  assert.equal(formatDateTimeLocal(localTimestamp), '2026-08-12 17:56:01.341000')
  assert.equal(formatDateTimeLocal('not-a-date'), UNKNOWN_FORMATTED_VALUE)
})

test('traffic metric timestamps preserve local microsecond precision', () => {
  const localTimestamp = new Date(2026, 7, 12, 17, 56, 1, 341)
  const timestampMicros = localTimestamp.getTime() * 1000 + 480

  assert.equal(formatUnixMicrosLocal(timestampMicros), '2026-08-12 17:56:01.341480')
  assert.equal(formatUnixMicrosLocal(-1), UNKNOWN_FORMATTED_VALUE)
  assert.equal(formatUnixMicrosLocal(Number.NaN), UNKNOWN_FORMATTED_VALUE)
})

test('traffic metric durations preserve microseconds below one millisecond', () => {
  assert.equal(formatDurationMicros(1_000_000, 1_000_000), '0 μs')
  assert.equal(formatDurationMicros(1_000_000, 1_000_125), '125 μs')
  assert.equal(formatDurationMicros(1_000_000, 1_000_584), '584 μs')
  assert.equal(formatDurationMicros(1_000_000, 1_001_000), '1 ms')
  assert.equal(formatDurationMicros(1_000_000, 1_007_542), '8 ms')
  assert.equal(formatDurationMicros(1_000_000, 1_735_936), '736 ms')
  assert.equal(formatDurationMicros(-1, 1_000_000), UNKNOWN_FORMATTED_VALUE)
  assert.equal(formatDurationMicros(2_000_000, 1_000_000), UNKNOWN_FORMATTED_VALUE)
})

test('traffic metric byte sizes use compact binary units', () => {
  assert.equal(formatFileSize(0), '0 B')
  assert.equal(formatFileSize(109), '109 B')
  assert.equal(formatFileSize(1067), '1.04 KB')
  assert.equal(
    formatFileSize(1024, { precision: 2, trimTrailingZeros: false }),
    '1.00 KB',
  )
  assert.equal(formatFileSize(undefined, { unknownValue: '-' }), '-')
  assert.equal(formatFileSize(-1), UNKNOWN_FORMATTED_VALUE)
})

test('request file drop targets retain their tab and form-data row scope', () => {
  const bodyTarget = buildRequestBodyFileDropTarget('http-request:4')
  assert.deepEqual(
    parseRequestFileDropPayload({
      paths: ['C:/tmp/body.bin'],
      dataFileDropTarget: bodyTarget,
    }),
    {
      paths: ['C:/tmp/body.bin'],
      target: { kind: 'body-file', tabKey: 'http-request:4' },
    },
  )

  const formDataTarget = buildRequestFormDataFileDropTarget('http-request:5', 'row:2')
  assert.deepEqual(
    parseRequestFileDropPayload({
      paths: ['C:/tmp/upload.bin'],
      dataFileDropTarget: formDataTarget,
    }),
    {
      paths: ['C:/tmp/upload.bin'],
      target: { kind: 'form-data-file', tabKey: 'http-request:5', rowId: 'row:2' },
    },
  )
  assert.equal(
    parseRequestFileDropPayload({
      paths: ['C:/tmp/upload.bin'],
      dataFileDropTarget: 'row:2',
    }),
    null,
  )
})

test('local data cleared events reset only the state covered by their scope', () => {
  assert.deepEqual(parseLocalDataClearedPayload({ scope: 'cache' }), {
    scope: 'cache',
    historyCleared: false,
  })
  assert.deepEqual(
    parseLocalDataClearedPayload({
      scope: 'cache-and-history',
      historyCleared: true,
      requestDraftCacheRoot: ' C:/FlowLens/request-draft-cache ',
    }),
    {
      scope: 'cache-and-history',
      historyCleared: true,
      requestDraftCacheRoot: 'C:/FlowLens/request-draft-cache',
    },
  )
  assert.deepEqual(parseLocalDataClearedPayload({ scope: 'cache-and-history' }), {
    scope: 'cache-and-history',
    historyCleared: false,
  })
  assert.equal(parseLocalDataClearedPayload({ scope: 'all' }), null)
  assert.equal(parseLocalDataClearedPayload('cache'), null)

  const cacheActions: string[] = []
  syncLocalDataClearedWindow({ scope: 'cache', historyCleared: false }, {
    clearProcessIconCache: () => cacheActions.push('icons'),
    resetHistory: () => cacheActions.push('history'),
    reloadHistory: () => cacheActions.push('reload-history'),
    clearRequestDraftCacheFileReferences: () => cacheActions.push('request-draft'),
  })
  assert.deepEqual(cacheActions, ['icons', 'reload-history'])

  const historyActions: string[] = []
  syncLocalDataClearedWindow(
    {
      scope: 'cache-and-history',
      historyCleared: true,
      requestDraftCacheRoot: 'C:/FlowLens/request-draft-cache',
    },
    {
      clearProcessIconCache: () => historyActions.push('icons'),
      resetHistory: () => historyActions.push('history'),
      reloadHistory: () => historyActions.push('reload-history'),
      clearRequestDraftCacheFileReferences: (root) => historyActions.push(`request-draft:${root}`),
    },
  )
  assert.deepEqual(historyActions, ['icons', 'history', 'request-draft:C:/FlowLens/request-draft-cache'])

  const partialHistoryActions: string[] = []
  syncLocalDataClearedWindow(
    {
      scope: 'cache-and-history',
      historyCleared: false,
      requestDraftCacheRoot: 'C:/FlowLens/request-draft-cache',
    },
    {
      clearProcessIconCache: () => partialHistoryActions.push('icons'),
      resetHistory: () => partialHistoryActions.push('history'),
      reloadHistory: () => partialHistoryActions.push('reload-history'),
      clearRequestDraftCacheFileReferences: (root) =>
        partialHistoryActions.push(`request-draft:${root}`),
    },
  )
  assert.deepEqual(partialHistoryActions, [
    'icons',
    'reload-history',
    'request-draft:C:/FlowLens/request-draft-cache',
  ])
})

test('HAR export accepts the initial HBIN v1 history layout', () => {
  assert.equal(isHARExportableHistoryFormat(1), true)
  assert.equal(isHARExportableHistoryFormat(0), false)
  assert.equal(isHARExportableHistoryFormat(2), false)
  assert.equal(isHARExportableHistoryFormat(undefined), false)
})

test('traffic metric message sizes include the displayed header terminator', () => {
  const request = summarizeHTTPMessageSize(107, 0)
  const response = summarizeHTTPMessageSize(531, 425)

  assert.deepEqual(request, { header: 109, body: 0, total: 109 })
  assert.deepEqual(response, { header: 533, body: 425, total: 958 })
  assert.equal(sumKnownByteSizes(request.total, response.total), 1067)
  assert.deepEqual(summarizeHTTPMessageSize(107, 0, true), {
    header: null,
    body: 0,
    total: null,
  })
  assert.deepEqual(summarizeHTTPMessageSize(-1, -1), {
    header: null,
    body: null,
    total: null,
  })
})

test('HTTP/1 traffic metric sizes include request and status lines', () => {
  const requestLineSize = getLogicalHTTPRequestStartLineSize(
    'GET',
    'https://ifconfig.co/json',
    'HTTP/1.1',
  )
  const responseLineSize = getLogicalHTTPResponseStartLineSize('200 OK', 'HTTP/1.1')
  const request = summarizeHTTPMessageSize(57, 0, false, requestLineSize)
  const response = summarizeHTTPMessageSize(541, 404, false, responseLineSize)

  assert.equal(requestLineSize, 20)
  assert.equal(responseLineSize, 17)
  assert.deepEqual(request, { header: 79, body: 0, total: 79 })
  assert.deepEqual(response, { header: 560, body: 404, total: 964 })
  assert.equal(sumKnownByteSizes(request.total, response.total), 1043)

  assert.equal(
    getLogicalHTTPRequestStartLineSize(
      'GET',
      'https://example.test/search?q=flowlens',
      'HTTP/1.1',
    ),
    33,
  )
  assert.equal(
    getLogicalHTTPRequestStartLineSize('GET', 'https://example.test/a%2Fb?', 'HTTP/1.1'),
    22,
  )
  assert.equal(
    getLogicalHTTPRequestStartLineSize('GET', 'https://example.test/a/../long', 'HTTP/1.1'),
    25,
  )
  assert.equal(
    getLogicalHTTPRequestStartLineSize('GET', 'https://example.test/%2e%2e/a', 'HTTP/1.1'),
    24,
  )
  assert.equal(getLogicalHTTPRequestStartLineSize('OPTIONS', '*', 'HTTP/1.1'), 20)
  assert.equal(
    getLogicalHTTPRequestStartLineSize('CONNECT', 'https://example.test', 'HTTP/1.1'),
    -1,
  )
  assert.equal(getLogicalHTTPResponseStartLineSize('200 ', 'HTTP/1.1'), 15)
  assert.equal(
    getLogicalHTTPRequestStartLineSize('GET', 'https://ifconfig.co/json', 'HTTP/2.0'),
    0,
  )
  assert.equal(getLogicalHTTPResponseStartLineSize('200 OK', 'HTTP/2.0'), 0)
})

test('request header field conversion preserves order, casing, and duplicates', () => {
  const rows = [
    { key: ' X-Last ', value: 'last-1', enabled: true },
    { key: 'x-first', value: 'first', enabled: true },
    { key: 'x-last', value: 'last-2', enabled: true },
    { key: 'ignored', value: 'disabled', enabled: false },
    { key: '   ', value: 'blank', enabled: true },
  ]

  const fields = editableRowsToHeaderFields(rows)
  assert.deepEqual(fields, [
    { name: 'X-Last', value: 'last-1' },
    { name: 'x-first', value: 'first' },
    { name: 'x-last', value: 'last-2' },
  ])
  assert.deepEqual(headerFieldsToEditableRows([fields[0]!, null, ...fields.slice(1)]), [
    { key: 'X-Last', value: 'last-1', enabled: true },
    { key: 'x-first', value: 'first', enabled: true },
    { key: 'x-last', value: 'last-2', enabled: true },
  ])
  assert.deepEqual(headerFieldsToEditableRows(null), [])
})

test('editable request headers exclude protocol pseudo-headers', () => {
  assert.deepEqual(
    editableHeaderFieldsToRows([
      { name: ':method', value: 'GET' },
      { name: ':status', value: '200' },
      { name: 'X-Test', value: 'one' },
    ]),
    [{ key: 'X-Test', value: 'one', enabled: true }],
  )
})

test('request header validation rejects pseudo and non-token names', () => {
  assert.equal(
    findInvalidRequestHeaderName([
      { key: 'X-Valid', value: 'one', enabled: true },
      { key: ':status', value: '200', enabled: true },
    ]),
    ':status',
  )
  assert.equal(
    findInvalidRequestHeaderName([{ key: 'Bad Header', value: 'one', enabled: true }]),
    'Bad Header',
  )
  assert.equal(
    findInvalidRequestHeaderName([{ key: ':method', value: 'GET', enabled: false }]),
    null,
  )
  assert.equal(
    findInvalidRequestHeaderName(
      [{ key: 'Connection', value: 'keep-alive', enabled: true }],
      'http2',
    ),
    'Connection',
  )
  assert.equal(
    findInvalidRequestHeaderName(
      [{ key: 'Connection', value: 'keep-alive', enabled: true }],
      'http1',
    ),
    null,
  )
})

test('header record conversion handles prototype-like names safely', () => {
  const formatted = JSON.parse(
    formatHeaderFieldsAsJson([
      { name: '__proto__', value: 'first' },
      { name: '__proto__', value: 'second' },
      { name: 'constructor', value: 'value' },
    ]),
  ) as Record<string, string | string[]>
  assert.deepEqual(formatted.__proto__, ['first', 'second'])
  assert.equal(formatted.constructor, 'value')

  const record = editableRowsToHeadersRecord([
    { key: '__proto__', value: 'first', enabled: true },
    { key: '__proto__', value: 'second', enabled: true },
  ])
  assert.deepEqual(record.__proto__, ['first', 'second'])
})

function rawEntry(overrides: Partial<TrafficEntryLike> = {}): TrafficEntryLike {
  return {
    type: 'tcp',
    host: '[2001:db8::1]:8443',
    url: 'tcp://[2001:db8::1]:8443',
    rawTcp: {
      source: 'http_connect',
      hostPort: '[2001:db8::1]:8443',
      tls: true,
    },
    ...overrides,
  }
}

function processEntry(host: string, overrides: Partial<TrafficProcessLike> = {}): TrafficEntryLike {
  return {
    type: 'https',
    host,
    metadata: {
      process: {
        status: 'resolved',
        pid: 100,
        displayName: 'Browser',
        processName: 'browser.exe',
        executablePath: 'C:\\Apps\\Browser\\browser.exe',
        appId: 'com.example.browser',
        iconKey: 'a'.repeat(64),
        ...overrides,
      },
    },
  }
}

function categoryProcessEntry(
  id: number,
  host: string,
  overrides: Partial<TrafficProcessLike> = {},
): CategoryTrafficEntry {
  return {
    id,
    type: 'https',
    startedAt: '',
    method: 'GET',
    url: `https://${host}/`,
    host,
    path: '/',
    statusCode: 200,
    status: 'complete',
    metadata: {
      localConnectionEstablishedAt: '',
      remoteConnectionEstablishedAt: '',
      requestProcessedAt: '',
      sslHandshakeCompletedAt: '',
      process: {
        status: 'resolved',
        pid: 100,
        displayName: 'Browser',
        processName: 'browser.exe',
        executablePath: 'C:\\Apps\\Browser\\browser.exe',
        appId: 'com.example.browser',
        iconKey: 'a'.repeat(64),
        ...overrides,
      },
    },
  } as unknown as CategoryTrafficEntry
}

test('traffic type guards use explicit classifications', () => {
  assert.equal(isRawTCPTraffic(rawEntry()), true)
  assert.equal(isWebSocketTraffic({ type: 'wss' }), true)
  assert.equal(isHTTPTraffic({ type: 'https' }), true)
  assert.equal(isHTTPTraffic({ type: 'unknown' }), false)
})

test('traffic eviction watermark accepts out-of-order IDs before any eviction', () => {
  const watermark = advanceTrafficEvictionWatermark(0, [])

  assert.equal(isTrafficEntryEvicted(2, watermark), false)
  assert.equal(isTrafficEntryEvicted(1, watermark), false)
})

test('traffic eviction watermark only rejects IDs removed by the bounded window', () => {
  const watermark = advanceTrafficEvictionWatermark(0, [1, 2])

  assert.equal(watermark, 2)
  assert.equal(isTrafficEntryEvicted(1, watermark), true)
  assert.equal(isTrafficEntryEvicted(2, watermark), true)
  assert.equal(isTrafficEntryEvicted(3, watermark), false)
  assert.equal(advanceTrafficEvictionWatermark(watermark, [1]), watermark)
})

test('host-port parsing supports domains, IPv4, and bracketed IPv6', () => {
  assert.deepEqual(parseHostPort('example.com:443'), { host: 'example.com', port: '443' })
  assert.deepEqual(parseHostPort('127.0.0.1:8080'), { host: '127.0.0.1', port: '8080' })
  assert.deepEqual(parseHostPort('[2001:db8::1]:8443'), {
    host: '2001:db8::1',
    port: '8443',
  })
  assert.deepEqual(parseHostPort('2001:db8::1'), { host: '2001:db8::1', port: '' })
})

test('raw TCP display helpers prefer tunnel metadata and selected ALPN', () => {
  const entry = rawEntry({ metadata: { tls: { selectedAlpn: 'mqtt' } } })
  assert.equal(getTrafficTarget(entry), '[2001:db8::1]:8443')
  assert.equal(getTrafficCategoryHost(entry), '2001:db8::1')
  assert.equal(getTrafficTargetPort(entry), '8443')
  assert.equal(getTrafficProtocol(entry), 'mqtt')
  assert.equal(getTrafficTypeLabel(entry), 'TCP/TLS')
})

test('raw TCP search covers target, port, and source', () => {
  const entry = rawEntry()
  assert.equal(trafficMatchesSearch(entry, '2001:DB8'), true)
  assert.equal(trafficMatchesSearch(entry, '8443'), true)
  assert.equal(trafficMatchesSearch(entry, 'HTTP_CONNECT'), true)
  assert.equal(trafficMatchesSearch(entry, 'websocket'), false)
})

test('raw TCP capability gating disables body and request actions', () => {
  assert.deepEqual(getTrafficCapabilities(rawEntry()), {
    canLoadBody: false,
    canSubscribeLiveDetail: false,
    canEditRequest: false,
    canResend: false,
    canCopyCurl: false,
    canSaveToCollection: false,
  })
  assert.equal(getTrafficCapabilities({ type: 'http' }).canResend, true)
  assert.equal(getTrafficCapabilities({ type: 'ws' }).canEditRequest, true)
})

test('process category prefers app id and groups different PIDs', () => {
  const first = getTrafficProcessCategory(processEntry('one.test', { pid: 100 }))
  const second = getTrafficProcessCategory(processEntry('two.test', { pid: 200 }))

  assert.equal(first.kind, 'resolved')
  assert.equal(first.key, 'app:com.example.browser')
  assert.equal(second.key, first.key)
  assert.equal(first.label, 'Browser')
})

test('process category falls back to executable path and then process name', () => {
  assert.equal(
    getTrafficProcessCategory(
      processEntry('one.test', { appId: '', executablePath: '/opt/browser' }),
    ).key,
    'exe:/opt/browser',
  )
  assert.equal(
    getTrafficProcessCategory(
      processEntry('one.test', {
        appId: '',
        executablePath: '',
        processName: 'browser',
        displayName: '',
      }),
    ).key,
    'name:browser',
  )
})

test('process category trims identity fields while preserving case', () => {
  const category = getTrafficProcessCategory(
    processEntry('one.test', {
      appId: '  Com.Example.Browser  ',
      displayName: '  Browser Pro  ',
      processName: '  Browser.EXE  ',
      iconKey: '  ICON  ',
    }),
  )

  assert.deepEqual(category, {
    kind: 'resolved',
    key: 'app:Com.Example.Browser',
    label: 'Browser Pro',
    displayName: 'Browser Pro',
    processName: 'Browser.EXE',
    executablePath: 'C:\\Apps\\Browser\\browser.exe',
    appId: 'Com.Example.Browser',
    iconKey: 'ICON',
  })
})

test('process category maps every unavailable attribution state to one category', () => {
  const unavailableEntries: TrafficEntryLike[] = [
    { type: 'https', host: 'missing.test' },
    processEntry('pending.test', { status: 'pending' }),
    processEntry('remote.test', { status: 'remote' }),
    processEntry('not-found.test', { status: 'not_found' }),
    processEntry('permission.test', { status: 'permission_denied' }),
    processEntry('unsupported.test', { status: 'unsupported' }),
    processEntry('ambiguous.test', { status: 'ambiguous' }),
    processEntry('unknown.test', { status: '' }),
    processEntry('pid-only.test', {
      appId: '',
      executablePath: '',
      processName: '',
      displayName: '',
      pid: 100,
    }),
    processEntry('missing-label.test', {
      displayName: '',
      processName: '',
    }),
  ]

  for (const entry of unavailableEntries) {
    const category = getTrafficProcessCategory(entry)
    assert.equal(category.kind, 'unavailable')
    assert.equal(category.key, PROCESS_CATEGORY_UNAVAILABLE_KEY)
    assert.equal(category.label, '')
  }
})

test('process summary groups by stable identity and keeps distinct executable paths', () => {
  const summary = buildProcessSummary(
    [
      categoryProcessEntry(1, 'one.test', { pid: 100 }),
      categoryProcessEntry(2, 'two.test', { pid: 200 }),
      categoryProcessEntry(3, 'three.test', {
        appId: '',
        executablePath: 'C:\\Apps\\First\\tool.exe',
        displayName: 'Tool',
        processName: 'tool.exe',
      }),
      categoryProcessEntry(4, 'four.test', {
        appId: '',
        executablePath: 'C:\\Apps\\Second\\tool.exe',
        displayName: 'Tool',
        processName: 'tool.exe',
      }),
      categoryProcessEntry(5, 'pending.test', { status: 'pending' }),
    ],
    'Unidentified process',
  )

  assert.equal(summary.length, 4)
  assert.equal(summary.find((item) => item.processKey === 'app:com.example.browser')?.count, 2)
  assert.equal(
    summary.filter((item) => item.kind === 'resolved' && item.label === 'Tool').length,
    2,
  )
  assert.equal(summary.at(-1)?.kind, 'unavailable')
  assert.equal(summary.at(-1)?.count, 1)
})

test('process summary keeps a selected empty unavailable item at the end', () => {
  const summary = buildProcessSummary(
    [categoryProcessEntry(1, 'one.test')],
    'Unidentified process',
    true,
  )

  assert.equal(summary.at(-1)?.processKey, PROCESS_CATEGORY_UNAVAILABLE_KEY)
  assert.equal(summary.at(-1)?.count, 0)
})

test('process summary search only matches visible process names', () => {
  const summary = buildProcessSummary(
    [
      categoryProcessEntry(1, 'one.test'),
      categoryProcessEntry(2, 'pending.test', { status: 'pending' }),
    ],
    'Unidentified process',
  )

  assert.equal(filterProcessSummary(summary, 'Browser').length, 1)
  assert.equal(filterProcessSummary(summary, 'browser.exe').length, 1)
  assert.equal(filterProcessSummary(summary, 'Unidentified').length, 1)
  assert.equal(filterProcessSummary(summary, 'com.example.browser').length, 0)
  assert.equal(filterProcessSummary(summary, 'C:\\Apps\\Browser').length, 0)
})

test('category facet summaries narrow only by the opposite facet', () => {
  const entries = [
    categoryProcessEntry(1, 'api.example.com'),
    categoryProcessEntry(2, 'cdn.example.com'),
    categoryProcessEntry(3, 'api.example.com', {
      appId: 'com.example.terminal',
      displayName: 'Terminal',
      processName: 'terminal.exe',
      executablePath: 'C:\\Apps\\Terminal\\terminal.exe',
    }),
    categoryProcessEntry(4, 'admin.example.com', {
      appId: 'com.example.editor',
      displayName: 'Editor',
      processName: 'editor.exe',
      executablePath: 'C:\\Apps\\Editor\\editor.exe',
    }),
  ]

  const hostSelected = buildCategoryFacetSummaries(
    entries,
    new Set(['api.example.com']),
    new Set(),
    'Unidentified process',
  )
  assert.deepEqual(
    hostSelected.hosts.map((item) => item.host),
    ['admin.example.com', 'api.example.com', 'cdn.example.com'],
  )
  assert.deepEqual(
    hostSelected.processes.map((item) => item.label),
    ['Browser', 'Terminal'],
  )

  const processSelected = buildCategoryFacetSummaries(
    entries,
    new Set(),
    new Set(['app:com.example.browser']),
    'Unidentified process',
  )
  assert.deepEqual(
    processSelected.hosts.map((item) => item.host),
    ['api.example.com', 'cdn.example.com'],
  )
  assert.deepEqual(
    processSelected.processes.map((item) => item.label),
    ['Browser', 'Editor', 'Terminal'],
  )
})

test('category facet summaries retain selected zero-result items', () => {
  const entries = [
    categoryProcessEntry(1, 'api.example.com'),
    categoryProcessEntry(2, 'admin.example.com', {
      appId: 'com.example.editor',
      displayName: 'Editor',
      processName: 'editor.exe',
      executablePath: 'C:\\Apps\\Editor\\editor.exe',
    }),
  ]

  const summaries = buildCategoryFacetSummaries(
    entries,
    new Set(['api.example.com', 'admin.example.com']),
    new Set(['app:com.example.browser', 'app:com.example.editor']),
    'Unidentified process',
  )

  assert.deepEqual(
    summaries.hosts.map((item) => [item.host, item.count]),
    [
      ['admin.example.com', 1],
      ['api.example.com', 1],
    ],
  )
  assert.deepEqual(
    summaries.processes.map((item) => [item.processKey, item.count]),
    [
      ['app:com.example.browser', 1],
      ['app:com.example.editor', 1],
    ],
  )

  const incompatible = buildCategoryFacetSummaries(
    entries,
    new Set(['api.example.com']),
    new Set(['app:com.example.editor']),
    'Unidentified process',
  )
  assert.deepEqual(
    incompatible.hosts.map((item) => [item.host, item.count]),
    [
      ['admin.example.com', 1],
      ['api.example.com', 0],
    ],
  )
  assert.deepEqual(
    incompatible.processes.map((item) => [item.processKey, item.count]),
    [
      ['app:com.example.browser', 1],
      ['app:com.example.editor', 0],
    ],
  )
})

test('category filters OR within each facet and AND across facets', () => {
  const browser = processEntry('api.example.com')
  const editor = processEntry('cdn.example.com', {
    appId: 'com.example.editor',
    displayName: 'Editor',
    processName: 'editor',
    executablePath: '/opt/editor',
  })

  assert.equal(
    trafficMatchesCategoryFilters(
      browser,
      new Set(['api.example.com', 'other.example.com']),
      new Set(['app:com.example.browser', 'app:other']),
    ),
    true,
  )
  assert.equal(
    trafficMatchesCategoryFilters(
      editor,
      new Set(['api.example.com']),
      new Set(['app:com.example.editor']),
    ),
    false,
  )
  assert.equal(
    trafficMatchesCategoryFilters(
      browser,
      new Set(['api.example.com']),
      new Set(['app:other', 'app:another']),
    ),
    false,
  )
  assert.equal(
    trafficMatchesCategoryFilters(
      editor,
      new Set(),
      new Set(['app:other', 'app:com.example.editor']),
    ),
    true,
  )
  assert.equal(trafficMatchesCategoryFilters(editor, new Set(), new Set()), true)
})

test('category filters can explicitly select unavailable processes', () => {
  const pending = processEntry('api.example.com', { status: 'pending' })
  assert.equal(
    trafficMatchesCategoryFilters(pending, new Set(), new Set([PROCESS_CATEGORY_UNAVAILABLE_KEY])),
    true,
  )
})

test('ordered header helpers preserve interleaving and exact casing while copying', () => {
  const source = [
    { name: 'A', value: 'first' },
    { name: 'B', value: 'middle' },
    { name: 'A', value: 'last' },
    { name: 'a', value: 'lowercase' },
  ]

  assert.equal(formatHeaderFieldsAsText(source), 'A: first\nB: middle\nA: last\na: lowercase')
  assert.deepEqual(JSON.parse(formatHeaderFieldsAsJson(source)), {
    A: ['first', 'last'],
    B: 'middle',
    a: 'lowercase',
  })
})

test('ordered header sorting is stable for case-insensitive matching names', () => {
  const source = [
    { name: 'B', value: 'first-b' },
    { name: 'a', value: 'first-a' },
    { name: 'A', value: 'second-a' },
    { name: 'b', value: 'second-b' },
  ]

  assert.deepEqual(
    sortHeaderFields(source, 'asc').map((field) => field.value),
    ['first-a', 'second-a', 'first-b', 'second-b'],
  )
  assert.deepEqual(
    sortHeaderFields(source, 'desc').map((field) => field.value),
    ['first-b', 'second-b', 'first-a', 'second-a'],
  )
})

test('ordered header normalization reports whether wire order is available', () => {
  assert.deepEqual(normalizeHeaderFields([]), {
    fields: [],
    hasWireOrder: true,
  })
  assert.deepEqual(normalizeHeaderFields(null), {
    fields: [],
    hasWireOrder: false,
  })
  assert.deepEqual(normalizeHeaderFields([{ name: 'Fallback', value: 'one' }], true), {
    fields: [{ name: 'Fallback', value: 'one' }],
    hasWireOrder: false,
  })
})

test('field-only header lookup is case-insensitive', () => {
  const fields = [
    { name: 'Content-Type', value: 'application/json' },
    { name: 'X-Test', value: 'one' },
    { name: 'X-Test', value: 'two' },
  ]
  assert.equal(firstHeaderFieldValue(fields, 'content-type'), 'application/json')
  assert.equal(firstHeaderFieldValue(fields, 'missing'), undefined)
})

test('cookie helpers consume ordered header fields directly', () => {
  assert.deepEqual(
    {
      ...requestCookiesRecord([
        { name: 'Cookie', value: 'first=one; second=two' },
        { name: 'cookie', value: 'third=three' },
      ]),
    },
    { first: ['one'], second: ['two'], third: ['three'] },
  )
  assert.deepEqual(
    {
      ...responseCookiesRecord([
        { name: 'Set-Cookie', value: 'session=abc; HttpOnly' },
        { name: 'set-cookie', value: 'session=def; Path=/' },
      ]),
    },
    {
      'session.Value': ['abc'],
      'session.HttpOnly': ['true'],
      'session#2.Value': ['def'],
      'session#2.Path': ['/'],
    },
  )
})

test('request cookie count includes only enabled named cookies', () => {
  assert.equal(
    countRequestCookieRows([
      { key: 'Cookie', value: 'first=one; second=two', enabled: true },
      { key: 'cookie', value: 'disabled=three', enabled: false },
      { key: 'X-Test', value: 'ignored', enabled: true },
    ]),
    2,
  )
  assert.equal(countRequestCookieRows([]), 0)
})

test('request protocol conversion changes only controlled route rows', () => {
  const rows = [
    { key: 'X-First', value: '1', enabled: true },
    { key: 'Host', value: 'old.test', enabled: true },
    { key: 'X-Second', value: '2', enabled: false },
  ]
  const http2 = convertRequestRouteHeaders(rows, 'http2', 'post', 'https://new.test/a?q=1')
  assert.deepEqual(http2, [rows[0], rows[2]])

  const http1 = convertRequestRouteHeaders(http2, 'http1', 'GET', 'http://back.test/')
  assert.deepEqual(http1, [{ key: 'Host', value: 'back.test', enabled: true }, rows[0], rows[2]])
})

test('request protocol inheritance requires a complete wire-order block', () => {
  assert.equal(normalizeRequestProtocol(undefined), 'auto')
  assert.equal(
    inferRequestProtocolFromHTTPMessage({ proto: 'HTTP/2.0', headerFields: [] }),
    'http2',
  )
  assert.equal(
    inferRequestProtocolFromHTTPMessage({
      proto: 'HTTP/1.1',
      headerFields: [{ name: 'Host', value: 'example.test' }],
    }),
    'http1',
  )
  assert.equal(
    inferRequestProtocolFromHTTPMessage({
      proto: 'HTTP/2.0',
      headerFields: [],
      headersTruncated: true,
    }),
    'auto',
  )
  assert.equal(
    inferRequestProtocolFromHTTPMessage({
      proto: 'HTTP/2.0',
      headerFields: [],
      headerOrderUnavailable: true,
    }),
    'auto',
  )
  assert.equal(inferRequestProtocolFromHTTPMessage({ proto: 'HTTP/1.1' }), 'auto')
})
