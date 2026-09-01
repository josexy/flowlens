import * as apicollectionservice from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import type {
  EditableKeyValue,
  RequestDraftSource,
  RequestTLSClientHelloID,
  RequestFileValue,
  HttpRequestFormDataItem,
  HttpRequestSendSettings,
  HttpRequestEditorState,
  WebSocketClientSettings,
  WebSocketClientState,
} from '@/types/request-editor'
import {
  convertRequestRouteHeaders,
  editableHeaderFieldsToRows,
  firstHeaderFieldValue,
  inferRequestProtocolFromHTTPMessage,
  normalizeRequestProtocol,
} from '@/utils/headers'
import {
  hasRequestURLQuery,
  parseRequestQueryRows,
  replaceRequestURLQuery,
} from '@/utils/requestQuery'
import { DEFAULT_HTTP_TAB_TITLE, DEFAULT_WS_TAB_TITLE, type WorkspaceTab } from './tabs'

const requestTLSClientHelloIDs: readonly RequestTLSClientHelloID[] = [
  'golang',
  'chrome_auto',
  'firefox_auto',
  'safari_auto',
  'edge_auto',
  'ios_auto',
  'android_11_okhttp',
  'randomized_alpn',
]

export const DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT = `from flowlens import *

def onRequest(context, request):
    print(f"request {request.method} {request.url}")
    return request

def onResponse(context, response):
    print(f"response {response.code}")
    return response
`

export const DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED = false

function normalizeAbsolutePathForComparison(value: string, windowsPath: boolean): string | null {
  const rawPath = value.trim()
  if (!rawPath) return null

  const separatedPath = windowsPath ? rawPath.replace(/\\/g, '/') : rawPath
  const driveMatch = windowsPath ? separatedPath.match(/^([a-zA-Z]:)\//) : null
  const isUNCPath = windowsPath && separatedPath.startsWith('//')
  if (!driveMatch && !isUNCPath && !separatedPath.startsWith('/')) {
    return null
  }

  const prefix = driveMatch ? `${driveMatch[1]}/` : '/'
  const remainder = driveMatch
    ? separatedPath.slice(driveMatch[0].length)
    : separatedPath.replace(/^\/+/, '')
  const segments: string[] = []
  for (const segment of remainder.split('/')) {
    if (!segment || segment === '.') continue
    if (segment === '..') {
      segments.pop()
      continue
    }
    segments.push(segment)
  }

  const normalizedPath = `${prefix}${segments.join('/')}`
  return windowsPath ? normalizedPath.toLowerCase() : normalizedPath
}

function isPathInsideRequestDraftCache(path: string, requestDraftCacheRoot: string): boolean {
  const rawRoot = requestDraftCacheRoot.trim()
  const windowsPath = /^[a-zA-Z]:[\\/]/.test(rawRoot) || /^[\\/]{2}/.test(rawRoot)
  const normalizedRoot = normalizeAbsolutePathForComparison(rawRoot, windowsPath)
  const normalizedPath = normalizeAbsolutePathForComparison(path, windowsPath)
  if (!normalizedRoot || !normalizedPath) return false

  return normalizedPath === normalizedRoot || normalizedPath.startsWith(`${normalizedRoot}/`)
}

export function clearRequestDraftCacheFileReferences(
  tabs: WorkspaceTab[],
  requestDraftCacheRoot: string,
): number {
  let clearedCount = 0
  for (const tab of tabs) {
    if (tab.type === 'http-request' && tab.httpRequest) {
      const state = tab.httpRequest
      if (
        state.requestBodyFile &&
        isPathInsideRequestDraftCache(state.requestBodyFile.path, requestDraftCacheRoot)
      ) {
        state.requestBodyFile = null
        clearedCount++
      }
      for (const item of state.requestBodyFormData) {
        if (item.file && isPathInsideRequestDraftCache(item.file.path, requestDraftCacheRoot)) {
          item.file = null
          clearedCount++
        }
      }
      continue
    }

    if (
      tab.type === 'websocket-client' &&
      tab.webSocketClient?.draftFile &&
      isPathInsideRequestDraftCache(tab.webSocketClient.draftFile.path, requestDraftCacheRoot)
    ) {
      tab.webSocketClient.draftFile = null
      clearedCount++
    }
  }
  return clearedCount
}

function createDefaultInlineScriptState() {
  return {
    inlineScriptEnabled: false,
    inlineScriptSource: DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT,
    scriptExecutionId: '',
    scriptConsoleEntries: [],
  }
}

function normalizeRequestTLSClientHelloID(
  value: string | null | undefined,
): RequestTLSClientHelloID {
  return requestTLSClientHelloIDs.includes(value as RequestTLSClientHelloID)
    ? (value as RequestTLSClientHelloID)
    : 'golang'
}

function createEmptyKVRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

function createDefaultHttpRequestSendSettings(): HttpRequestSendSettings {
  return {
    proxyMode: 'none',
    protocol: 'auto',
    customProxy: '',
    timeoutMs: 0,
    tlsClientHelloId: 'golang',
    http2Fingerprint: '',
  }
}

function createDefaultWebSocketClientSettings(): WebSocketClientSettings {
  return {
    proxyMode: 'none',
    customProxy: '',
    timeoutMs: 0,
    tlsClientHelloId: 'golang',
  }
}

function createEmptyFormDataRow(): HttpRequestFormDataItem {
  return {
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    enabled: true,
    name: '',
    itemType: 'text',
    value: '',
    file: null,
  }
}

function inferRequestBodyType(
  headerFields: (proxyservice.HTTPHeaderField | null)[] | null | undefined,
  requestBody = '',
): HttpRequestEditorState['requestBodyType'] {
  const contentType = (firstHeaderFieldValue(headerFields, 'Content-Type') ?? '').toLowerCase()
  if (!contentType) {
    if (requestBody.length > 0) {
      return 'text'
    }
    return 'none'
  }
  if (contentType.includes('json')) {
    return 'json'
  }
  if (contentType.includes('xml')) {
    return 'xml'
  }
  if (contentType.includes('multipart/form-data')) {
    return 'form-data'
  }
  if (contentType.includes('application/x-www-form-urlencoded')) {
    return 'urlencoded'
  }
  return 'text'
}

function trafficRequestHeadersToRows(
  request: proxyservice.HTTPMessage | null | undefined,
): EditableKeyValue[] {
  if (request?.headerFields === null || request?.headerFields === undefined) {
    return [createEmptyKVRow()]
  }
  const rows = editableHeaderFieldsToRows(request.headerFields)
  return rows.length > 0 ? rows : [createEmptyKVRow()]
}

function mapQueryToRows(url: string): EditableKeyValue[] {
  return parseRequestQueryRows(url)
}

function restoreSavedRequestURL(url: string, params: EditableKeyValue[]): string {
  return hasRequestURLQuery(url) ? url : replaceRequestURLQuery(url, params)
}

function mapRequestDraftFileToFileValue(
  file: proxyservice.RequestDraftFile | null | undefined,
): RequestFileValue | null {
  if (!file?.path) {
    return null
  }
  return {
    path: file.path,
    name: file.name ?? '',
    size: file.size ?? 0,
  }
}

function mapRecoveryFormDataToRows(
  formData: (proxyservice.RequestBodyRecoveryFormDataItem | null)[] | null | undefined,
): HttpRequestFormDataItem[] {
  const rows: HttpRequestFormDataItem[] = []
  for (const item of formData ?? []) {
    if (!item) {
      continue
    }
    rows.push({
      id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
      enabled: item.enabled ?? true,
      name: item.name ?? '',
      itemType: item.itemType === 'file' ? 'file' : 'text',
      value: item.value ?? '',
      file: mapRequestDraftFileToFileValue(item.file),
    })
  }
  return rows.length > 0 ? rows : [createEmptyFormDataRow()]
}

function mapRecoveryUrlEncodedToRows(
  items: (proxyservice.SendRequestURLEncodedItem | null)[] | null | undefined,
): EditableKeyValue[] {
  const rows: EditableKeyValue[] = []
  for (const item of items ?? []) {
    if (!item) {
      continue
    }
    rows.push({
      key: item.name ?? '',
      value: item.value ?? '',
      enabled: item.enabled ?? true,
    })
  }
  return rows.length > 0 ? rows : [createEmptyKVRow()]
}

export function applyRequestBodyRecovery(
  state: HttpRequestEditorState,
  recovery: proxyservice.RequestBodyRecoveryResult | null | undefined,
): HttpRequestEditorState {
  if (!recovery) {
    return state
  }

  state.requestBodyType = recovery.bodyType as HttpRequestEditorState['requestBodyType']
  state.requestBodyText = recovery.text ?? ''
  state.requestBodyFile = mapRequestDraftFileToFileValue(recovery.file)
  state.requestBodyFormData = mapRecoveryFormDataToRows(recovery.formData)
  state.requestBodyUrlEncoded = mapRecoveryUrlEncodedToRows(recovery.urlEncoded)

  if (recovery.warnings?.length) {
    console.warn('HTTP request body recovery warnings:', recovery.warnings)
  }

  return state
}

function mapEditableValuesToSavedKeyValues(
  items: EditableKeyValue[],
): apicollectionservice.SavedKeyValue[] {
  return items
    .filter((item) => item.key.trim() || item.value.trim())
    .map((item) => ({
      key: item.key,
      value: item.value,
      enabled: item.enabled,
    }))
}

function mapEditableHeadersToSavedKeyValues(
  items: EditableKeyValue[],
): apicollectionservice.SavedKeyValue[] {
  return mapEditableValuesToSavedKeyValues(items.filter((item) => !item.key.trim().startsWith(':')))
}

function mapSavedKeyValuesToEditableRows(
  items: (apicollectionservice.SavedKeyValue | null)[] | null | undefined,
): EditableKeyValue[] {
  const rows: EditableKeyValue[] = []
  for (const item of items ?? []) {
    if (!item) {
      continue
    }
    rows.push({
      key: item.key ?? '',
      value: item.value ?? '',
      enabled: item.enabled ?? true,
    })
  }
  return rows.length > 0 ? rows : [createEmptyKVRow()]
}

function withoutPseudoHeaderRows(rows: EditableKeyValue[]): EditableKeyValue[] {
  const filtered = rows.filter((item) => !item.key.trim().startsWith(':'))
  return filtered.length > 0 ? filtered : [createEmptyKVRow()]
}

function mapRequestFileToSavedFile(
  file: RequestFileValue | null | undefined,
): apicollectionservice.SavedFile | undefined {
  if (!file?.path) {
    return undefined
  }
  return {
    path: file.path,
    name: file.name,
    size: file.size,
  }
}

function mapSavedFileToRequestFile(
  file: apicollectionservice.SavedFile | null | undefined,
): RequestFileValue | null {
  if (!file?.path) {
    return null
  }
  return {
    path: file.path,
    name: file.name ?? '',
    size: file.size ?? 0,
  }
}

function mapRequestFormDataToSavedFormData(
  items: HttpRequestFormDataItem[],
): apicollectionservice.SavedFormDataItem[] {
  return items
    .filter((item) => item.name.trim() || item.value.trim() || item.file?.path)
    .map((item) => ({
      id: item.id,
      enabled: item.enabled,
      name: item.name,
      itemType: item.itemType,
      value: item.value,
      file: mapRequestFileToSavedFile(item.file),
    }))
}

function mapSavedFormDataToRequestRows(
  items: (apicollectionservice.SavedFormDataItem | null)[] | null | undefined,
): HttpRequestFormDataItem[] {
  const rows: HttpRequestFormDataItem[] = []
  for (const item of items ?? []) {
    if (!item) {
      continue
    }
    rows.push({
      id: item.id || `${Date.now()}-${Math.random().toString(16).slice(2)}`,
      enabled: item.enabled ?? true,
      name: item.name ?? '',
      itemType: item.itemType === 'file' ? 'file' : 'text',
      value: item.value ?? '',
      file: mapSavedFileToRequestFile(item.file),
    })
  }
  return rows.length > 0 ? rows : [createEmptyFormDataRow()]
}

export function buildSavedHTTPRequestFromState(
  state: HttpRequestEditorState,
): apicollectionservice.SavedHTTPRequest {
  return {
    method: state.method,
    url: state.url,
    params: mapEditableValuesToSavedKeyValues(state.params),
    headers: mapEditableHeadersToSavedKeyValues(state.headers),
    bodyType: state.requestBodyType,
    bodyText: state.requestBodyText,
    bodyFile: mapRequestFileToSavedFile(state.requestBodyFile),
    bodyFormData: mapRequestFormDataToSavedFormData(state.requestBodyFormData),
    bodyUrlEncoded: mapEditableValuesToSavedKeyValues(state.requestBodyUrlEncoded),
    inlineScriptSource: state.inlineScriptSource,
    proxyMode: state.settings.proxyMode as proxyservice.SendRequestProxyMode,
    protocol: state.settings.protocol as proxyservice.SendRequestProtocol,
    customProxy: state.settings.customProxy,
    timeoutMs: state.settings.timeoutMs,
    tlsClientHelloId: state.settings.tlsClientHelloId as proxyservice.TLSClientHelloID,
    http2Fingerprint: state.settings.http2Fingerprint,
  }
}

export function buildSavedWebSocketRequestFromState(
  state: WebSocketClientState,
): apicollectionservice.SavedWebSocketRequest {
  return {
    url: state.url,
    params: mapEditableValuesToSavedKeyValues(state.params),
    headers: mapEditableHeadersToSavedKeyValues(state.headers),
    draftType: state.draftType,
    draftText: state.draftText,
    draftFile: mapRequestFileToSavedFile(state.draftFile),
    proxyMode: state.settings.proxyMode as proxyservice.SendRequestProxyMode,
    customProxy: state.settings.customProxy,
    timeoutMs: state.settings.timeoutMs,
    tlsClientHelloId: state.settings.tlsClientHelloId as proxyservice.TLSClientHelloID,
  }
}

export function buildHttpRequestEditorStateFromSavedRequest(
  request: apicollectionservice.SavedHTTPRequest,
  name: string,
): HttpRequestEditorState {
  const method = request.method || 'GET'
  const params = mapSavedKeyValuesToEditableRows(request.params)
  const requestURL = restoreSavedRequestURL(request.url || '', params)
  const protocol = normalizeRequestProtocol(request.protocol)
  return {
    source: 'new',
    name,
    isSending: false,
    isStreaming: false,
    streamSessionId: '',
    pluginsEnabled: DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED,
    ...createDefaultInlineScriptState(),
    inlineScriptSource: request.inlineScriptSource ?? DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT,
    activeRequestTab: 'headers',
    activeResponseTab: 'response-headers',
    method,
    url: requestURL,
    params,
    headers: convertRequestRouteHeaders(
      withoutPseudoHeaderRows(mapSavedKeyValuesToEditableRows(request.headers)),
      protocol,
      method,
      requestURL,
    ),
    requestBodyType: (request.bodyType || 'none') as HttpRequestEditorState['requestBodyType'],
    requestBodyText: request.bodyText ?? '',
    requestBodyFile: mapSavedFileToRequestFile(request.bodyFile),
    requestBodyFormData: mapSavedFormDataToRequestRows(request.bodyFormData),
    requestBodyUrlEncoded: mapSavedKeyValuesToEditableRows(request.bodyUrlEncoded),
    settings: {
      proxyMode: (request.proxyMode || 'none') as HttpRequestSendSettings['proxyMode'],
      protocol,
      customProxy: request.customProxy ?? '',
      timeoutMs: request.timeoutMs ?? 0,
      tlsClientHelloId: normalizeRequestTLSClientHelloID(request.tlsClientHelloId),
      http2Fingerprint: request.http2Fingerprint ?? '',
    },
    response: null,
  }
}

export function buildWebSocketClientStateFromSavedRequest(
  request: apicollectionservice.SavedWebSocketRequest,
  name: string,
): WebSocketClientState {
  const params = mapSavedKeyValuesToEditableRows(request.params)
  const requestURL = restoreSavedRequestURL(request.url || '', params)
  return {
    source: 'new',
    name,
    isSendingMessage: false,
    activeLeftTab: 'message',
    activeRightTab: 'response-headers',
    url: requestURL,
    params,
    headers: withoutPseudoHeaderRows(mapSavedKeyValuesToEditableRows(request.headers)),
    responseHeaders: [],
    responseError: '',
    settings: {
      proxyMode: (request.proxyMode || 'none') as WebSocketClientSettings['proxyMode'],
      customProxy: request.customProxy ?? '',
      timeoutMs: request.timeoutMs ?? 0,
      tlsClientHelloId: normalizeRequestTLSClientHelloID(request.tlsClientHelloId),
    },
    sessionId: '',
    draftType: (request.draftType || 'text') as WebSocketClientState['draftType'],
    draftText: request.draftText ?? '',
    draftFile: mapSavedFileToRequestFile(request.draftFile),
    messages: [],
    directionFilter: 'all',
    viewMode: 'list',
    connectionStatus: 'idle',
  }
}

export function applySavedHTTPRequestToState(
  state: HttpRequestEditorState,
  request: apicollectionservice.SavedHTTPRequest,
  name: string,
) {
  state.name = name
  state.method = request.method || 'GET'
  const params = mapSavedKeyValuesToEditableRows(request.params)
  state.params = params
  state.url = restoreSavedRequestURL(request.url || '', params)
  const protocol = normalizeRequestProtocol(request.protocol)
  state.headers = convertRequestRouteHeaders(
    withoutPseudoHeaderRows(mapSavedKeyValuesToEditableRows(request.headers)),
    protocol,
    state.method,
    state.url,
  )
  state.requestBodyType = (request.bodyType || 'none') as HttpRequestEditorState['requestBodyType']
  state.requestBodyText = request.bodyText ?? ''
  state.requestBodyFile = mapSavedFileToRequestFile(request.bodyFile)
  state.requestBodyFormData = mapSavedFormDataToRequestRows(request.bodyFormData)
  state.requestBodyUrlEncoded = mapSavedKeyValuesToEditableRows(request.bodyUrlEncoded)
  state.inlineScriptSource = request.inlineScriptSource ?? DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT
  state.settings = {
    proxyMode: (request.proxyMode || 'none') as HttpRequestSendSettings['proxyMode'],
    protocol,
    customProxy: request.customProxy ?? '',
    timeoutMs: request.timeoutMs ?? 0,
    tlsClientHelloId: normalizeRequestTLSClientHelloID(request.tlsClientHelloId),
    http2Fingerprint: request.http2Fingerprint ?? '',
  }
}

export function applySavedWebSocketRequestToState(
  state: WebSocketClientState,
  request: apicollectionservice.SavedWebSocketRequest,
  name: string,
) {
  state.name = name
  const params = mapSavedKeyValuesToEditableRows(request.params)
  state.params = params
  state.url = restoreSavedRequestURL(request.url || '', params)
  state.headers = withoutPseudoHeaderRows(mapSavedKeyValuesToEditableRows(request.headers))
  state.settings = {
    proxyMode: (request.proxyMode || 'none') as WebSocketClientSettings['proxyMode'],
    customProxy: request.customProxy ?? '',
    timeoutMs: request.timeoutMs ?? 0,
    tlsClientHelloId: normalizeRequestTLSClientHelloID(request.tlsClientHelloId),
  }
  state.draftType = (request.draftType || 'text') as WebSocketClientState['draftType']
  state.draftText = request.draftText ?? ''
  state.draftFile = mapSavedFileToRequestFile(request.draftFile)
}

function createMeaningfulKVRows(rows: EditableKeyValue[]) {
  return rows.filter((item) => item.enabled && (item.key.trim() || item.value.trim()))
}

function createMeaningfulHeaderRows(rows: EditableKeyValue[]) {
  return rows.filter(
    (item) => !item.key.trim().startsWith(':') && (item.key.trim() || item.value.trim()),
  )
}

function createMeaningfulFormDataRows(rows: HttpRequestFormDataItem[]) {
  return rows.filter(
    (item) => item.enabled && (item.name.trim() || item.value.trim() || item.file?.path),
  )
}

function createSnapshotPayloadForTab(tab: WorkspaceTab) {
  if (tab.type === 'http-request' && tab.httpRequest) {
    const state = tab.httpRequest
    return {
      type: 'http',
      method: state.method.trim(),
      url: state.url.trim(),
      params: createMeaningfulKVRows(state.params),
      headers: createMeaningfulHeaderRows(state.headers),
      requestBodyType: state.requestBodyType,
      requestBodyText: state.requestBodyText,
      requestBodyFile: state.requestBodyFile
        ? {
            path: state.requestBodyFile.path,
            name: state.requestBodyFile.name,
            size: state.requestBodyFile.size,
          }
        : null,
      requestBodyFormData: createMeaningfulFormDataRows(state.requestBodyFormData).map((item) => ({
        enabled: item.enabled,
        name: item.name,
        itemType: item.itemType,
        value: item.value,
        file: item.file
          ? {
              path: item.file.path,
              name: item.file.name,
              size: item.file.size,
            }
          : null,
      })),
      requestBodyUrlEncoded: createMeaningfulKVRows(state.requestBodyUrlEncoded),
      inlineScriptSource: state.inlineScriptSource,
      settings: {
        proxyMode: state.settings.proxyMode,
        protocol: state.settings.protocol,
        customProxy: state.settings.customProxy,
        timeoutMs: state.settings.timeoutMs,
        tlsClientHelloId: state.settings.tlsClientHelloId,
        http2Fingerprint: state.settings.http2Fingerprint,
      },
    }
  }

  if (tab.type === 'websocket-client' && tab.webSocketClient) {
    const state = tab.webSocketClient
    return {
      type: 'websocket',
      url: state.url.trim(),
      params: createMeaningfulKVRows(state.params),
      headers: createMeaningfulHeaderRows(state.headers),
      draftType: state.draftType,
      draftText: state.draftText,
      draftFile: state.draftFile
        ? {
            path: state.draftFile.path,
            name: state.draftFile.name,
            size: state.draftFile.size,
          }
        : null,
      settings: {
        proxyMode: state.settings.proxyMode,
        customProxy: state.settings.customProxy,
        timeoutMs: state.settings.timeoutMs,
        tlsClientHelloId: state.settings.tlsClientHelloId,
      },
    }
  }

  return null
}

export function createSnapshotForTab(tab: WorkspaceTab) {
  const payload = createSnapshotPayloadForTab(tab)
  return payload ? JSON.stringify(payload) : ''
}

export function hasMeaningfulRequestDraft(tab: WorkspaceTab) {
  const payload = createSnapshotPayloadForTab(tab)
  if (!payload) {
    return false
  }
  if (payload.type === 'http') {
    return Boolean(
      payload.url ||
      payload.method !== 'GET' ||
      payload.params.length ||
      payload.headers.length ||
      payload.requestBodyType !== 'none' ||
      payload.requestBodyText ||
      payload.requestBodyFile ||
      payload.requestBodyFormData.length ||
      payload.requestBodyUrlEncoded.length ||
      payload.inlineScriptSource !== DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT ||
      payload.settings.proxyMode !== 'none' ||
      payload.settings.protocol !== 'auto' ||
      payload.settings.customProxy ||
      payload.settings.timeoutMs > 0 ||
      payload.settings.tlsClientHelloId !== 'golang' ||
      payload.settings.http2Fingerprint.trim(),
    )
  }
  return Boolean(
    payload.url ||
    payload.params.length ||
    payload.headers.length ||
    payload.draftType !== 'text' ||
    payload.draftText ||
    payload.draftFile ||
    payload.settings.proxyMode !== 'none' ||
    payload.settings.customProxy ||
    payload.settings.timeoutMs > 0 ||
    payload.settings.tlsClientHelloId !== 'golang',
  )
}

export function applySavedMetadataToTab(
  tab: WorkspaceTab,
  request: apicollectionservice.APICollectionRequest,
) {
  tab.apiId = request.id
  tab.apiUpdatedAt = request.updatedAt
  tab.savedSnapshot = createSnapshotForTab(tab)
}

export function toHttpRequestEditorState(args: {
  source: RequestDraftSource
  entry: proxyservice.TrafficEntry
  bodyView: proxyservice.TrafficBodyView | null
  sourceHistoryKey?: string
}): HttpRequestEditorState {
  const { source, entry, bodyView, sourceHistoryKey } = args
  const method = entry.method || 'GET'
  const requestURL = entry.url || ''
  const protocol = inferRequestProtocolFromHTTPMessage(entry.request)
  return {
    source,
    sourceEntryId: entry.id,
    sourceHistoryKey,
    name: entry.host || DEFAULT_HTTP_TAB_TITLE,
    isSending: false,
    isStreaming: false,
    streamSessionId: '',
    pluginsEnabled: DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED,
    ...createDefaultInlineScriptState(),
    activeRequestTab: 'headers',
    activeResponseTab: 'response-headers',
    method,
    url: requestURL,
    params: mapQueryToRows(requestURL),
    headers: convertRequestRouteHeaders(
      trafficRequestHeadersToRows(entry.request),
      protocol,
      method,
      requestURL,
    ),
    requestBodyType: inferRequestBodyType(entry.request?.headerFields, bodyView?.reqBody ?? ''),
    requestBodyText: bodyView?.reqBody ?? '',
    requestBodyFile: null,
    requestBodyFormData: [createEmptyFormDataRow()],
    requestBodyUrlEncoded: [createEmptyKVRow()],
    settings: {
      ...createDefaultHttpRequestSendSettings(),
      protocol,
    },
    response: null,
  }
}

export function toWebSocketClientState(args: {
  source: RequestDraftSource
  entry: proxyservice.TrafficEntry
  bodyView: proxyservice.TrafficBodyView | null
  sourceHistoryKey?: string
}): WebSocketClientState {
  const { source, entry, sourceHistoryKey } = args
  return {
    source,
    sourceEntryId: entry.id,
    sourceHistoryKey,
    name: entry.host || DEFAULT_WS_TAB_TITLE,
    isSendingMessage: false,
    activeLeftTab: 'message',
    activeRightTab: 'response-headers',
    url: entry.url || '',
    params: mapQueryToRows(entry.url || ''),
    headers: trafficRequestHeadersToRows(entry.request),
    responseHeaders: [],
    responseError: '',
    settings: createDefaultWebSocketClientSettings(),
    sessionId: '',
    draftType: 'text',
    draftText: '',
    draftFile: null,
    messages: [],
    directionFilter: 'all',
    viewMode: 'list',
    connectionStatus: 'idle',
  }
}

export function buildEmptyHttpRequestEditorState(source: RequestDraftSource): HttpRequestEditorState {
  return {
    source,
    name: DEFAULT_HTTP_TAB_TITLE,
    isSending: false,
    isStreaming: false,
    streamSessionId: '',
    pluginsEnabled: DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED,
    ...createDefaultInlineScriptState(),
    activeRequestTab: 'headers',
    activeResponseTab: 'response-headers',
    method: 'GET',
    url: '',
    params: [createEmptyKVRow()],
    headers: [createEmptyKVRow()],
    requestBodyType: 'none',
    requestBodyText: '',
    requestBodyFile: null,
    requestBodyFormData: [createEmptyFormDataRow()],
    requestBodyUrlEncoded: [createEmptyKVRow()],
    settings: createDefaultHttpRequestSendSettings(),
    response: null,
  }
}

export function buildEmptyWebSocketClientState(source: RequestDraftSource): WebSocketClientState {
  return {
    source,
    name: DEFAULT_WS_TAB_TITLE,
    isSendingMessage: false,
    activeLeftTab: 'message',
    activeRightTab: 'response-headers',
    url: '',
    params: [createEmptyKVRow()],
    headers: [createEmptyKVRow()],
    responseHeaders: [],
    responseError: '',
    settings: createDefaultWebSocketClientSettings(),
    sessionId: '',
    draftType: 'text',
    draftText: '',
    draftFile: null,
    messages: [],
    directionFilter: 'all',
    viewMode: 'list',
    connectionStatus: 'idle',
  }
}
