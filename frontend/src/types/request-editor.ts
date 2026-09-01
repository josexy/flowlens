import type {
  WebSocketDirectionFilter,
  WebSocketDisplayMessage,
  WebSocketViewMode,
} from './websocket.js'
import type { HTTPRequestPluginExecution } from '../../bindings/github.com/josexy/flowlens/backend/services/proxy_service/models.js'
import type { PluginLogEntry } from '../../bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models.js'

export type RequestDraftSource = 'new' | 'capture-edit' | 'history-edit'

export interface EditableKeyValue {
  key: string
  value: string
  enabled: boolean
}

export type HttpRequestBodyType =
  'none' | 'json' | 'text' | 'xml' | 'file' | 'form-data' | 'urlencoded'
export type HttpRequestFormDataItemType = 'text' | 'file'
export type RequestProxyMode = 'none' | 'system' | 'mitm' | 'custom'
export type HttpRequestProtocol = 'auto' | 'http1' | 'http2'
export type RequestTLSClientHelloID =
  | 'golang'
  | 'chrome_auto'
  | 'firefox_auto'
  | 'safari_auto'
  | 'edge_auto'
  | 'ios_auto'
  | 'android_11_okhttp'
  | 'randomized_alpn'
export type HttpRequestTab = 'headers' | 'query' | 'cookies' | 'body' | 'script' | 'settings'
export type HttpResponseTab =
  | 'response-headers'
  | 'response-cookies'
  | 'response-trailers'
  | 'response-body'
  | 'response-console'
  | 'response-error'

export interface RequestFileValue {
  path: string
  name: string
  size: number
}

export interface HttpRequestFormDataItem {
  id: string
  enabled: boolean
  name: string
  itemType: HttpRequestFormDataItemType
  value: string
  file: RequestFileValue | null
}

export interface HttpRequestSendSettings {
  proxyMode: RequestProxyMode
  protocol: HttpRequestProtocol
  customProxy: string
  timeoutMs: number
  tlsClientHelloId: RequestTLSClientHelloID
  http2Fingerprint: string
}

export interface HttpRequestResponse {
  kind: 'success' | 'plugin' | 'error' | 'cancelled'
  outcome: string
  pluginExecution: HTTPRequestPluginExecution | null
  streamState: '' | 'streaming' | 'completed' | 'stopped' | 'error'
  statusCode: number
  statusText: string
  protocol: string
  headers: EditableKeyValue[]
  headersHaveWireOrder: boolean
  headersTruncated: boolean
  trailers: EditableKeyValue[]
  trailersHaveWireOrder: boolean
  trailersTruncated: boolean
  body: string
  bodyEncoding: '' | 'base64'
  errorMessage: string
}

export interface HttpRequestEditorState {
  source: RequestDraftSource
  sourceEntryId?: number
  sourceHistoryKey?: string
  name: string
  isSending: boolean
  isStreaming: boolean
  streamSessionId: string
  pluginsEnabled: boolean
  inlineScriptEnabled: boolean
  inlineScriptSource: string
  scriptExecutionId: string
  scriptConsoleEntries: PluginLogEntry[]
  activeRequestTab: HttpRequestTab
  activeResponseTab: HttpResponseTab
  method: string
  url: string
  params: EditableKeyValue[]
  headers: EditableKeyValue[]
  requestBodyType: HttpRequestBodyType
  requestBodyText: string
  requestBodyFile: RequestFileValue | null
  requestBodyFormData: HttpRequestFormDataItem[]
  requestBodyUrlEncoded: EditableKeyValue[]
  settings: HttpRequestSendSettings
  response: HttpRequestResponse | null
}

export type WebSocketDraftType = 'text' | 'json' | 'xml' | 'binary-file'
export type WebSocketClientConnectionStatus =
  'idle' | 'connecting' | 'connected' | 'closed' | 'error' | 'cancelled'
export type WebSocketClientViewMode = WebSocketViewMode
export type WebSocketClientDirectionFilter = WebSocketDirectionFilter
export type WebSocketClientMessage = WebSocketDisplayMessage
export type WebSocketClientLeftTab = 'message' | 'headers' | 'query' | 'cookies' | 'settings'
export type WebSocketClientRightTab =
  'response-headers' | 'response-cookies' | 'response' | 'response-error'

export interface WebSocketClientSettings {
  proxyMode: RequestProxyMode
  customProxy: string
  timeoutMs: number
  tlsClientHelloId: RequestTLSClientHelloID
}

export interface WebSocketClientState {
  source: RequestDraftSource
  sourceEntryId?: number
  sourceHistoryKey?: string
  name: string
  isSendingMessage: boolean
  activeLeftTab: WebSocketClientLeftTab
  activeRightTab: WebSocketClientRightTab
  url: string
  params: EditableKeyValue[]
  headers: EditableKeyValue[]
  responseHeaders: EditableKeyValue[]
  responseError: string
  settings: WebSocketClientSettings
  sessionId: string
  draftType: WebSocketDraftType
  draftText: string
  draftFile: RequestFileValue | null
  messages: WebSocketClientMessage[]
  directionFilter: WebSocketClientDirectionFilter
  viewMode: WebSocketClientViewMode
  connectionStatus: WebSocketClientConnectionStatus
}
