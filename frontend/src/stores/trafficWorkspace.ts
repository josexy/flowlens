import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import * as apicollectionservice from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'
import {
  SaveHTTPRequest as SaveHTTPRequestRPC,
  SaveWebSocketRequest as SaveWebSocketRequestRPC,
  UpdateHTTPRequest as UpdateHTTPRequestRPC,
  UpdateWebSocketRequest as UpdateWebSocketRequestRPC,
} from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/apicollectionservice'
import {
  ConnectWebSocket,
  DisconnectHTTPRequestStream as DisconnectHTTPRequestStreamRPC,
  DisconnectWebSocket as DisconnectWebSocketRPC,
  RecoverRequestBodyForEditing,
  SendWebSocketMessage as SendWebSocketMessageRPC,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { CancelError } from '@wailsio/runtime'
import type {
  RequestDraftSource,
  HttpRequestEditorState,
  WebSocketClientConnectionStatus,
  WebSocketClientState,
} from '@/types/request-editor'
import { onBatchedAppEvent } from '@/runtime/batchedAppEvents'
import { toWebSocketDisplayMessage } from '@/utils/websocket'
import { useTrafficStore } from './traffic'
import { useHistoryTrafficStore } from './historyTraffic'
import { useHistoryStore } from './history'
import { useHistoryFilterStore } from './historyFilter'
import { useCategoryContextStore } from './categoryContext'
import { getTrafficCapabilities, isWebSocketTraffic } from '@/utils/traffic'
import { editableRowsToHeaderFields, headerFieldsToEditableRows } from '@/utils/headers'
import { IncrementalBase64Encoder } from '@/utils/incrementalBase64'
import {
  CAPTURE_TAB_KEY,
  DEFAULT_HTTP_TAB_TITLE,
  DEFAULT_WS_TAB_TITLE,
  HISTORY_TAB_KEY,
  HTTP_REQUEST_EVENT_NAME,
  HTTP_REQUEST_TAB_PREFIX,
  MAX_PENDING_HTTP_REQUEST_EVENT_SESSIONS,
  MAX_PENDING_WEBSOCKET_SESSION_EVENT_SESSIONS,
  WEBSOCKET_SESSION_EVENT_NAME,
  WEBSOCKET_CLIENT_TAB_PREFIX,
  buildCaptureTab,
  deriveRequestTabTitle,
  formatHistoryTitle,
  type WorkspaceTab,
} from './traffic-workspace/tabs'
import {
  applyRequestBodyRecovery,
  clearRequestDraftCacheFileReferences as clearRequestDraftCacheFileReferencesFromTabs,
  applySavedHTTPRequestToState,
  applySavedMetadataToTab,
  applySavedWebSocketRequestToState,
  buildEmptyHttpRequestEditorState,
  buildEmptyWebSocketClientState,
  buildHttpRequestEditorStateFromSavedRequest,
  buildSavedHTTPRequestFromState,
  buildSavedWebSocketRequestFromState,
  buildWebSocketClientStateFromSavedRequest,
  createSnapshotForTab,
  hasMeaningfulRequestDraft,
  toHttpRequestEditorState,
  toWebSocketClientState,
} from './traffic-workspace/requestEditorState'
import { createOperationGenerationGuard } from '@/utils/latestOperation'

export type { WorkspaceTab, WorkspaceTabType } from './traffic-workspace/tabs'

let webSocketMessageSeed = 0
let httpRequestEventBound = false
let webSocketSessionEventBound = false
let offHttpRequestEvent: (() => void) | null = null
let offWebSocketSessionEvent: (() => void) | null = null
const pendingHTTPRequestEventsBySessionID = new Map<string, HTTPRequestRuntimeEvent[]>()
const pendingWebSocketSessionEventsBySessionID = new Map<string, WebSocketSessionRuntimeEvent[]>()
const pendingWebSocketConnectCallsByTabKey = new Map<
  string,
  ReturnType<typeof ConnectWebSocket>
>()
const webSocketConnectGenerationByTabKey = new Map<string, number>()
const ignoredHTTPRequestSessionIDs = new Set<string>()
const ignoredWebSocketSessionIDs = new Set<string>()

interface HTTPRequestRuntimeEvent {
  sessionId: string
  eventType: 'chunk' | 'complete' | 'closed' | 'error'
  offset?: number
  chunkBase64?: string
  trailerFields?: (proxyservice.HTTPHeaderField | null)[] | null
  trailersTruncated?: boolean
  trailerOrderUnavailable?: boolean
  error?: string
}

interface HTTPRequestStreamRuntime {
  sessionId: string
  state: HttpRequestEditorState
  byteOffset: number
  decoder: TextDecoder
  binaryBodyEncoder: IncrementalBase64Encoder
  pendingEvents: HTTPRequestRuntimeEvent[]
  flushTimer: ReturnType<typeof setTimeout> | null
}

const httpRequestStreamRuntimeBySessionID = new Map<string, HTTPRequestStreamRuntime>()

interface WebSocketSessionRuntimeEvent {
  sessionId: string
  eventType: 'connected' | 'message' | 'closed' | 'error'
  status: WebSocketClientConnectionStatus
  message?: proxyservice.WebSocketMessage | null
  error?: string
}

function decodeBase64Bytes(value: string): Uint8Array {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes
}

function bufferPendingHTTPRequestEvent(event: HTTPRequestRuntimeEvent) {
  const sessionID = event.sessionId?.trim()
  if (!sessionID) {
    return
  }
  if (
    !pendingHTTPRequestEventsBySessionID.has(sessionID) &&
    pendingHTTPRequestEventsBySessionID.size >= MAX_PENDING_HTTP_REQUEST_EVENT_SESSIONS
  ) {
    const oldestSessionID = pendingHTTPRequestEventsBySessionID.keys().next().value
    if (oldestSessionID) {
      pendingHTTPRequestEventsBySessionID.delete(oldestSessionID)
    }
  }
  const events = pendingHTTPRequestEventsBySessionID.get(sessionID) ?? []
  events.push(event)
  pendingHTTPRequestEventsBySessionID.set(sessionID, events)
}

function drainPendingHTTPRequestEvents(sessionID: string): HTTPRequestRuntimeEvent[] {
  const events = pendingHTTPRequestEventsBySessionID.get(sessionID) ?? []
  pendingHTTPRequestEventsBySessionID.delete(sessionID)
  return events
}

function isTerminalWebSocketClientState(state: WebSocketClientState): boolean {
  return (
    state.connectionStatus === 'closed' ||
    state.connectionStatus === 'error' ||
    state.connectionStatus === 'cancelled'
  )
}

function bufferPendingWebSocketSessionEvent(event: WebSocketSessionRuntimeEvent) {
  const sessionID = event.sessionId?.trim()
  if (!sessionID || ignoredWebSocketSessionIDs.has(sessionID)) {
    return
  }

  if (
    !pendingWebSocketSessionEventsBySessionID.has(sessionID) &&
    pendingWebSocketSessionEventsBySessionID.size >= MAX_PENDING_WEBSOCKET_SESSION_EVENT_SESSIONS
  ) {
    const oldestSessionID = pendingWebSocketSessionEventsBySessionID.keys().next().value
    if (oldestSessionID) {
      pendingWebSocketSessionEventsBySessionID.delete(oldestSessionID)
    }
  }

  const events = pendingWebSocketSessionEventsBySessionID.get(sessionID) ?? []
  events.push(event)
  pendingWebSocketSessionEventsBySessionID.set(sessionID, events)
}

function ignoreWebSocketSession(sessionID: string) {
  const normalizedSessionID = sessionID.trim()
  if (!normalizedSessionID) return
  if (ignoredWebSocketSessionIDs.size >= MAX_PENDING_WEBSOCKET_SESSION_EVENT_SESSIONS) {
    const oldestSessionID = ignoredWebSocketSessionIDs.values().next().value
    if (oldestSessionID) {
      ignoredWebSocketSessionIDs.delete(oldestSessionID)
    }
  }
  ignoredWebSocketSessionIDs.add(normalizedSessionID)
  pendingWebSocketSessionEventsBySessionID.delete(normalizedSessionID)
}

function drainPendingWebSocketSessionEvents(sessionID: string): WebSocketSessionRuntimeEvent[] {
  const normalizedSessionID = sessionID.trim()
  if (!normalizedSessionID) {
    return []
  }

  const events = pendingWebSocketSessionEventsBySessionID.get(normalizedSessionID) ?? []
  pendingWebSocketSessionEventsBySessionID.delete(normalizedSessionID)
  return events
}

function isWebSocketSessionNotFoundError(error: unknown): boolean {
  return String(error).includes('websocket session not found')
}

function nextWebSocketClientMessageID(): string {
  webSocketMessageSeed += 1
  return `websocket-client-msg:${webSocketMessageSeed}`
}

export const useTrafficWorkspaceStore = defineStore('trafficWorkspace', () => {
  const tabs = ref<WorkspaceTab[]>([buildCaptureTab()])
  const activeTabKey = ref<string>(CAPTURE_TAB_KEY)
  const requestTabSeed = ref(0)
  const requestRecoveryGenerationGuard = createOperationGenerationGuard()

  const trafficStore = useTrafficStore()
  const historyTrafficStore = useHistoryTrafficStore()
  const historyStore = useHistoryStore()
  const historyFilterStore = useHistoryFilterStore()
  const categoryContextStore = useCategoryContextStore()

  const activeTab = computed<WorkspaceTab>(() => {
    return (
      tabs.value.find((tab) => tab.key === activeTabKey.value) ?? tabs.value[0] ?? buildCaptureTab()
    )
  })

  const activeTrafficStore = computed(() => {
    if (activeTab.value.type === 'history') {
      return historyTrafficStore
    }
    if (activeTab.value.type === 'http-request') {
      return activeTab.value.httpRequest?.source === 'history-edit'
        ? historyTrafficStore
        : trafficStore
    }
    if (activeTab.value.type === 'websocket-client') {
      return activeTab.value.webSocketClient?.source === 'history-edit'
        ? historyTrafficStore
        : trafficStore
    }
    return trafficStore
  })
  const activeTrafficSelectionCount = computed(() => {
    const store = activeTrafficStore.value
    if (
      store === historyTrafficStore &&
      (historyStore.loadingHistory || historyStore.selectedKey !== historyTrafficStore.currentKey)
    ) {
      return 0
    }
    return store.selectedEntryCount
  })

  const historyCount = computed(() => historyStore.metadataList.length)

  function findWebSocketClientTab(tabKey: string): WorkspaceTab | undefined {
    return tabs.value.find((tab) => tab.key === tabKey && tab.type === 'websocket-client')
  }

  function findHTTPRequestTab(tabKey: string): WorkspaceTab | undefined {
    return tabs.value.find((tab) => tab.key === tabKey && tab.type === 'http-request')
  }

  function findWebSocketClientStateBySessionID(sessionID: string): WebSocketClientState | null {
    const normalizedSessionID = sessionID.trim()
    if (!normalizedSessionID) {
      return null
    }
    for (const tab of tabs.value) {
      if (tab.type === 'websocket-client' && tab.webSocketClient?.sessionId === normalizedSessionID) {
        return tab.webSocketClient
      }
    }
    return null
  }

  function clearRequestDraftCacheFileReferences(requestDraftCacheRoot: string): number {
    requestRecoveryGenerationGuard.invalidate()
    return clearRequestDraftCacheFileReferencesFromTabs(tabs.value, requestDraftCacheRoot)
  }

  function applyWebSocketSessionEvent(state: WebSocketClientState, event: WebSocketSessionRuntimeEvent) {
    if (event.eventType === 'connected') {
      if (isTerminalWebSocketClientState(state)) {
        return
      }
      state.sessionId = event.sessionId
      state.connectionStatus = 'connected'
      state.responseError = ''
      return
    }

    if (event.eventType === 'message' && event.message) {
      if (isTerminalWebSocketClientState(state)) {
        return
      }
      state.connectionStatus = 'connected'
      state.responseError = ''
      state.messages.push(
        toWebSocketDisplayMessage(event.message, nextWebSocketClientMessageID()),
      )
      return
    }

    if (event.eventType === 'closed' || event.eventType === 'error') {
      ignoreWebSocketSession(event.sessionId)
      state.connectionStatus =
        event.status === 'error' || event.eventType === 'error' ? 'error' : 'closed'
      state.sessionId = ''
      state.responseError = event.status === 'error' || event.eventType === 'error' ? event.error ?? '' : ''
    }
  }

  function finishHTTPRequestStream(
    runtime: HTTPRequestStreamRuntime,
    event: HTTPRequestRuntimeEvent,
  ) {
    const response = runtime.state.response
    if (response && response.bodyEncoding !== 'base64') {
      response.body += runtime.decoder.decode()
    }
    if (response && event.eventType === 'complete') {
      response.trailers = headerFieldsToEditableRows(event.trailerFields)
      response.trailersHaveWireOrder =
        event.trailerFields !== null &&
        event.trailerFields !== undefined &&
        event.trailerOrderUnavailable !== true
      response.trailersTruncated = event.trailersTruncated === true
    }
    if (response && event.eventType === 'error' && event.error) {
      response.errorMessage = event.error
    }
    if (response) {
      response.streamState = event.eventType === 'error' ? 'error' : 'completed'
    }
    runtime.state.isStreaming = false
    runtime.state.streamSessionId = ''
    if (runtime.flushTimer !== null) {
      clearTimeout(runtime.flushTimer)
      runtime.flushTimer = null
    }
    discardHTTPRequestStreamRuntime(runtime.sessionId, true)
  }

  function flushHTTPRequestStreamEvents(runtime: HTTPRequestStreamRuntime) {
    runtime.flushTimer = null
    if (httpRequestStreamRuntimeBySessionID.get(runtime.sessionId) !== runtime) {
      return
    }

    const chunkEvents: HTTPRequestRuntimeEvent[] = []
    const terminalEvents: HTTPRequestRuntimeEvent[] = []
    for (const event of runtime.pendingEvents) {
      if (
        event.eventType === 'chunk' &&
        typeof event.offset === 'number' &&
        typeof event.chunkBase64 === 'string'
      ) {
        chunkEvents.push(event)
      } else if (event.eventType !== 'chunk') {
        terminalEvents.push(event)
      }
    }
    if (chunkEvents.length > 1) {
      chunkEvents.sort((left, right) => left.offset! - right.offset!)
    }
    const deferredChunks: HTTPRequestRuntimeEvent[] = []
    const response = runtime.state.response
    const appendedText: string[] = []
    let appendedBinary = false

    for (const event of chunkEvents) {
      const chunk = decodeBase64Bytes(event.chunkBase64!)
      const startOffset = event.offset!
      const endOffset = startOffset + chunk.byteLength
      if (endOffset <= runtime.byteOffset) {
        continue
      }
      if (startOffset > runtime.byteOffset) {
        deferredChunks.push(event)
        continue
      }

      const overlap = Math.max(0, runtime.byteOffset - startOffset)
      const newBytes = chunk.subarray(overlap)
      if (response?.bodyEncoding === 'base64') {
        runtime.binaryBodyEncoder.append(newBytes)
        appendedBinary = true
      } else if (response) {
        appendedText.push(runtime.decoder.decode(newBytes, { stream: true }))
      }
      runtime.byteOffset = endOffset
    }

    if (response && appendedText.length > 0) {
      response.body += appendedText.join('')
    }
    if (response && appendedBinary) {
      response.body = runtime.binaryBodyEncoder.value()
    }

    runtime.pendingEvents = deferredChunks
    if (terminalEvents.length > 1) {
      terminalEvents.sort((left, right) => (left.offset ?? 0) - (right.offset ?? 0))
    }
    const terminalEvent = terminalEvents.find(
      (event) => typeof event.offset !== 'number' || event.offset <= runtime.byteOffset,
    )
    if (terminalEvent) {
      finishHTTPRequestStream(runtime, terminalEvent)
      return
    }
    runtime.pendingEvents.push(...terminalEvents)
  }

  function scheduleHTTPRequestStreamFlush(runtime: HTTPRequestStreamRuntime) {
    if (runtime.flushTimer !== null) {
      return
    }
    runtime.flushTimer = setTimeout(() => flushHTTPRequestStreamEvents(runtime), 100)
  }

  function queueHTTPRequestStreamEvent(event: HTTPRequestRuntimeEvent) {
    const sessionID = event.sessionId?.trim()
    if (!sessionID) {
      return
    }
    if (ignoredHTTPRequestSessionIDs.has(sessionID)) {
      return
    }
    const runtime = httpRequestStreamRuntimeBySessionID.get(sessionID)
    if (!runtime) {
      bufferPendingHTTPRequestEvent(event)
      return
    }
    runtime.pendingEvents.push(event)
    scheduleHTTPRequestStreamFlush(runtime)
  }

  function registerHTTPRequestStream(tabKey: string, sessionID: string) {
    const normalizedSessionID = sessionID.trim()
    if (!normalizedSessionID) {
      return false
    }
    const state = findHTTPRequestTab(tabKey)?.httpRequest
    if (!state || !state.response) {
      discardHTTPRequestStreamRuntime(normalizedSessionID, true)
      void DisconnectHTTPRequestStreamRPC(normalizedSessionID).catch((error) => {
        console.error('Failed to disconnect orphaned HTTP request stream:', error)
      })
      return false
    }
    ignoredHTTPRequestSessionIDs.delete(normalizedSessionID)

    const initialBinaryBody =
      state.response.bodyEncoding === 'base64' && state.response.body
        ? decodeBase64Bytes(state.response.body)
        : new Uint8Array()
    const binaryBodyEncoder = new IncrementalBase64Encoder(initialBinaryBody)
    const byteOffset =
      state.response.bodyEncoding === 'base64'
        ? binaryBodyEncoder.byteLength
        : new TextEncoder().encode(state.response.body).byteLength
    const runtime: HTTPRequestStreamRuntime = {
      sessionId: normalizedSessionID,
      state,
      byteOffset,
      decoder: new TextDecoder(),
      binaryBodyEncoder,
      pendingEvents: drainPendingHTTPRequestEvents(normalizedSessionID),
      flushTimer: null,
    }
    httpRequestStreamRuntimeBySessionID.set(normalizedSessionID, runtime)
    state.streamSessionId = normalizedSessionID
    state.isStreaming = true
    state.response.streamState = 'streaming'
    if (runtime.pendingEvents.length > 0) {
      scheduleHTTPRequestStreamFlush(runtime)
    }
    return true
  }

  function discardHTTPRequestStreamRuntime(sessionID: string, ignoreLateEvents = false) {
    const normalizedSessionID = sessionID.trim()
    if (!normalizedSessionID) {
      return
    }
    if (ignoreLateEvents) {
      if (ignoredHTTPRequestSessionIDs.size >= MAX_PENDING_HTTP_REQUEST_EVENT_SESSIONS) {
        const oldestSessionID = ignoredHTTPRequestSessionIDs.values().next().value
        if (oldestSessionID) {
          ignoredHTTPRequestSessionIDs.delete(oldestSessionID)
        }
      }
      ignoredHTTPRequestSessionIDs.add(normalizedSessionID)
    }
    const runtime = httpRequestStreamRuntimeBySessionID.get(normalizedSessionID)
    if (runtime?.flushTimer != null) {
      clearTimeout(runtime.flushTimer)
    }
    httpRequestStreamRuntimeBySessionID.delete(normalizedSessionID)
    pendingHTTPRequestEventsBySessionID.delete(normalizedSessionID)
  }

  function bindHTTPRequestEvents() {
    if (httpRequestEventBound) {
      return
    }
    httpRequestEventBound = true
    offHttpRequestEvent = onBatchedAppEvent(HTTP_REQUEST_EVENT_NAME, (data) =>
      queueHTTPRequestStreamEvent(data as HTTPRequestRuntimeEvent),
    )
  }

  function bindWebSocketSessionEvents() {
    if (webSocketSessionEventBound) {
      return
    }
    webSocketSessionEventBound = true
    offWebSocketSessionEvent = onBatchedAppEvent(
      WEBSOCKET_SESSION_EVENT_NAME,
      (data) => {
        const payload = data as WebSocketSessionRuntimeEvent
        const state = findWebSocketClientStateBySessionID(payload.sessionId)
        if (!state) {
          bufferPendingWebSocketSessionEvent(payload)
          return
        }
        applyWebSocketSessionEvent(state, payload)
      },
    )
  }

  function bindRequestEditorRuntimeEvents() {
    bindHTTPRequestEvents()
    bindWebSocketSessionEvents()
  }

  bindRequestEditorRuntimeEvents()

  function cleanupRuntimeEvents() {
    for (const [tabKey, pendingConnect] of pendingWebSocketConnectCallsByTabKey) {
      const state = findWebSocketClientTab(tabKey)?.webSocketClient
      if (state?.connectionStatus === 'connecting') {
        state.connectionStatus = 'cancelled'
        state.sessionId = ''
        state.responseHeaders = []
        state.responseError = ''
      }
      void pendingConnect.cancel()
    }
    pendingWebSocketConnectCallsByTabKey.clear()
    webSocketConnectGenerationByTabKey.clear()
    for (const [sessionID, runtime] of httpRequestStreamRuntimeBySessionID) {
      runtime.state.isStreaming = false
      runtime.state.streamSessionId = ''
      discardHTTPRequestStreamRuntime(sessionID)
      void DisconnectHTTPRequestStreamRPC(sessionID).catch((error) => {
        console.error('Failed to disconnect HTTP request stream:', error)
      })
    }
    offHttpRequestEvent?.()
    offHttpRequestEvent = null
    httpRequestEventBound = false
    offWebSocketSessionEvent?.()
    offWebSocketSessionEvent = null
    webSocketSessionEventBound = false
    pendingHTTPRequestEventsBySessionID.clear()
    ignoredHTTPRequestSessionIDs.clear()
    pendingWebSocketSessionEventsBySessionID.clear()
    ignoredWebSocketSessionIDs.clear()
  }

  function ensureCaptureTab() {
    if (!tabs.value.some((tab) => tab.key === CAPTURE_TAB_KEY)) {
      tabs.value.unshift(buildCaptureTab())
    }
  }

  function nextRequestTabKey(prefix: string): string {
    requestTabSeed.value += 1
    return `${prefix}${requestTabSeed.value}`
  }

  function activateTab(tabKey: string) {
    if (!tabs.value.some((tab) => tab.key === tabKey)) {
      return
    }
    activeTabKey.value = tabKey
  }

  function activateNextTab() {
    const currentIndex = tabs.value.findIndex((tab) => tab.key === activeTabKey.value)
    if (currentIndex === -1 || tabs.value.length === 0) {
      return
    }
    activeTabKey.value = tabs.value[(currentIndex + 1) % tabs.value.length]!.key
  }

  function activatePreviousTab() {
    const currentIndex = tabs.value.findIndex((tab) => tab.key === activeTabKey.value)
    if (currentIndex === -1 || tabs.value.length === 0) {
      return
    }
    activeTabKey.value = tabs.value[(currentIndex - 1 + tabs.value.length) % tabs.value.length]!.key
  }

  function activateCapture() {
    ensureCaptureTab()
    activeTabKey.value = CAPTURE_TAB_KEY
  }

  function ensureCategoryTargetTab(
    args: { kind: 'capture' } | { kind: 'history'; historyKey: string; title: string },
  ) {
    if (args.kind === 'capture') {
      activateCapture()
      return
    }

    const metadata = historyStore.metadataList.find((item) => item.key === args.historyKey)
    if (metadata) {
      openHistoryTab(metadata)
      return
    }

    const tab = tabs.value.find((item) => item.key === HISTORY_TAB_KEY)
    if (tab) {
      tab.title = args.title
      tab.historyKey = args.historyKey
    }
  }

  function openHistoryTab(metadata: proxyservice.HistoryMetadata) {
    const existing = tabs.value.find((tab) => tab.key === HISTORY_TAB_KEY)
    if (existing) {
      existing.title = formatHistoryTitle(metadata)
      existing.historyKey = metadata.key
      activeTabKey.value = HISTORY_TAB_KEY
      return
    }

    tabs.value.push({
      key: HISTORY_TAB_KEY,
      type: 'history',
      title: formatHistoryTitle(metadata),
      closable: true,
      historyKey: metadata.key,
    })
    activeTabKey.value = HISTORY_TAB_KEY
  }

  function createHttpRequestTab(source: RequestDraftSource = 'new', initialState?: HttpRequestEditorState) {
    const key = nextRequestTabKey(HTTP_REQUEST_TAB_PREFIX)
    const state = initialState ?? buildEmptyHttpRequestEditorState(source)
    const tab: WorkspaceTab = {
      key,
      type: 'http-request',
      title: state.name || DEFAULT_HTTP_TAB_TITLE,
      closable: true,
      httpRequest: state,
    }
    tabs.value.push(tab)
    activeTabKey.value = key
    return key
  }

  function createWebSocketClientTab(source: RequestDraftSource = 'new', initialState?: WebSocketClientState) {
    const key = nextRequestTabKey(WEBSOCKET_CLIENT_TAB_PREFIX)
    const state = initialState ?? buildEmptyWebSocketClientState(source)
    const tab: WorkspaceTab = {
      key,
      type: 'websocket-client',
      title: state.name || DEFAULT_WS_TAB_TITLE,
      closable: true,
      webSocketClient: state,
    }
    tabs.value.push(tab)
    activeTabKey.value = key
    return key
  }

  function findTab(tabKey: string) {
    return tabs.value.find((tab) => tab.key === tabKey)
  }

  function findTabByAPIID(apiID: string) {
    return tabs.value.find(
      (tab) =>
        (tab.type === 'http-request' || tab.type === 'websocket-client') && tab.apiId === apiID,
    )
  }

  function activateSavedApiTab(apiID: string) {
    const existingTab = findTabByAPIID(apiID)
    if (!existingTab) {
      return false
    }
    activeTabKey.value = existingTab.key
    return true
  }

  function openSavedApi(request: apicollectionservice.APICollectionRequest) {
    if (activateSavedApiTab(request.id)) {
      return activeTabKey.value
    }

    if (
      request.type === apicollectionservice.APICollectionNodeType.APICollectionNodeTypeHTTP &&
      request.http
    ) {
      const state = buildHttpRequestEditorStateFromSavedRequest(request.http, request.name)
      const tabKey = createHttpRequestTab('new', state)
      const tab = findTab(tabKey)
      if (tab) {
        tab.title = request.name
        applySavedMetadataToTab(tab, request)
      }
      return tabKey
    }

    if (
      request.type === apicollectionservice.APICollectionNodeType.APICollectionNodeTypeWebSocket &&
      request.websocket
    ) {
      const state = buildWebSocketClientStateFromSavedRequest(request.websocket, request.name)
      const tabKey = createWebSocketClientTab('new', state)
      const tab = findTab(tabKey)
      if (tab) {
        tab.title = request.name
        applySavedMetadataToTab(tab, request)
      }
      return tabKey
    }

    throw new Error(`Unsupported API request type: ${request.type}`)
  }

  async function openRequestEditorFromTraffic(args: {
    entry: proxyservice.TrafficEntry
    bodyView: proxyservice.TrafficBodyView | null
    source: RequestDraftSource
    sourceHistoryKey?: string
  }): Promise<string | null> {
    const { entry, bodyView, source, sourceHistoryKey } = args
    if (!getTrafficCapabilities(entry).canEditRequest) {
      throw new Error(`Traffic type ${entry.type} cannot be opened in Request Editor`)
    }
    const isWebSocket = isWebSocketTraffic(entry)
    if (isWebSocket) {
      const state = toWebSocketClientState({
        source,
        entry,
        bodyView,
        sourceHistoryKey,
      })
      return createWebSocketClientTab(source, state)
    }
    const state = toHttpRequestEditorState({
      source,
      entry,
      bodyView,
      sourceHistoryKey,
    })

    if (bodyView) {
      const recoveryGeneration = requestRecoveryGenerationGuard.capture()
      try {
        const recovery = await RecoverRequestBodyForEditing(
          entry.url || '',
          entry.request?.headerFields ?? [],
          bodyView,
        )
        if (!requestRecoveryGenerationGuard.isCurrent(recoveryGeneration)) {
          return null
        }
        applyRequestBodyRecovery(state, recovery)
      } catch (error) {
        if (!requestRecoveryGenerationGuard.isCurrent(recoveryGeneration)) {
          return null
        }
        console.error('Failed to recover request body for editing:', error)
      }
    }

    return createHttpRequestTab(source, state)
  }

  async function disconnectHTTPRequestTab(tabKey: string) {
    const state = findHTTPRequestTab(tabKey)?.httpRequest
    const sessionID = state?.streamSessionId.trim() ?? ''
    if (!state) {
      return
    }
    const runtime = httpRequestStreamRuntimeBySessionID.get(sessionID)
    if (runtime) {
      if (runtime.flushTimer !== null) {
        clearTimeout(runtime.flushTimer)
        runtime.flushTimer = null
      }
      flushHTTPRequestStreamEvents(runtime)
    }
    state.isStreaming = false
    state.streamSessionId = ''
    if (state.response?.kind === 'success') {
      state.response.streamState = 'stopped'
    }
    if (!sessionID) {
      return
    }
    discardHTTPRequestStreamRuntime(sessionID, true)
    await DisconnectHTTPRequestStreamRPC(sessionID)
  }

  async function connectWebSocketClientTab(tabKey: string) {
    const tab = findWebSocketClientTab(tabKey)
    const state = tab?.webSocketClient
    if (
      !state ||
      state.connectionStatus === 'connecting' ||
      state.connectionStatus === 'connected'
    ) {
      return
    }

    state.connectionStatus = 'connecting'
    state.responseError = ''

    const connectCall = ConnectWebSocket({
      url: state.url,
      headerFields: editableRowsToHeaderFields(state.headers),
      proxyMode: state.settings.proxyMode as proxyservice.SendRequestProxyMode,
      customProxy: state.settings.customProxy,
      timeoutMs: state.settings.timeoutMs > 0 ? state.settings.timeoutMs : 0,
      tlsClientHelloId: state.settings.tlsClientHelloId as proxyservice.TLSClientHelloID,
    })
    webSocketConnectGenerationByTabKey.set(
      tabKey,
      (webSocketConnectGenerationByTabKey.get(tabKey) ?? 0) + 1,
    )
    pendingWebSocketConnectCallsByTabKey.set(tabKey, connectCall)

    try {
      const response = await connectCall
      if (pendingWebSocketConnectCallsByTabKey.get(tabKey) !== connectCall) {
        return
      }
      const sessionID = response.sessionId?.trim() ?? ''
      ignoredWebSocketSessionIDs.delete(sessionID)
      state.sessionId = sessionID
      state.connectionStatus = response.status === 'connected' ? 'connected' : 'idle'
      state.responseHeaders = headerFieldsToEditableRows(response.headerFields)
      state.responseError = ''
      for (const bufferedEvent of drainPendingWebSocketSessionEvents(sessionID)) {
        applyWebSocketSessionEvent(state, bufferedEvent)
      }
    } catch (error) {
      if (pendingWebSocketConnectCallsByTabKey.get(tabKey) !== connectCall) {
        return
      }
      if (error instanceof CancelError) {
        state.connectionStatus = 'cancelled'
        state.sessionId = ''
        state.responseHeaders = []
        state.responseError = ''
        return
      }
      state.connectionStatus = 'error'
      state.sessionId = ''
      state.responseHeaders = []
      state.responseError = String(error)
      throw error
    } finally {
      if (pendingWebSocketConnectCallsByTabKey.get(tabKey) === connectCall) {
        pendingWebSocketConnectCallsByTabKey.delete(tabKey)
      }
    }
  }

  async function cancelWebSocketConnection(tabKey: string) {
    const state = findWebSocketClientTab(tabKey)?.webSocketClient
    const connectCall = pendingWebSocketConnectCallsByTabKey.get(tabKey)
    const connectGeneration = webSocketConnectGenerationByTabKey.get(tabKey)
    if (!state || !connectCall || state.connectionStatus !== 'connecting') {
      return
    }

    await connectCall.cancel()

    if (webSocketConnectGenerationByTabKey.get(tabKey) !== connectGeneration) {
      return
    }
    const sessionID = state.sessionId.trim()
    if (sessionID) {
      await DisconnectWebSocketRPC(sessionID)
    }
    state.sessionId = ''
    state.connectionStatus = 'cancelled'
    state.responseHeaders = []
    state.responseError = ''
  }

  async function sendWebSocketClientMessage(tabKey: string) {
    const tab = findWebSocketClientTab(tabKey)
    const state = tab?.webSocketClient
    if (!state) {
      return
    }
    if (state.connectionStatus !== 'connected' || !state.sessionId) {
      throw new Error('websocket session is not connected')
    }

    if (state.isSendingMessage) {
      return
    }

    state.isSendingMessage = true
    try {
      await SendWebSocketMessageRPC({
        sessionId: state.sessionId,
        msgType: state.draftType === 'binary-file' ? 'binary' : 'text',
        text: state.draftType === 'binary-file' ? '' : state.draftText,
        file:
          state.draftType === 'binary-file' && state.draftFile
            ? {
                path: state.draftFile.path,
                name: state.draftFile.name,
                size: state.draftFile.size,
              }
            : undefined,
      })
    } catch (error) {
      if (isWebSocketSessionNotFoundError(error)) {
        ignoreWebSocketSession(state.sessionId)
        state.sessionId = ''
        state.connectionStatus = 'closed'
        state.responseError = ''
      }
      throw error
    } finally {
      state.isSendingMessage = false
    }
  }

  async function disconnectWebSocketClientTab(tabKey: string) {
    const tab = findWebSocketClientTab(tabKey)
    const state = tab?.webSocketClient
    const sessionID = state?.sessionId?.trim() ?? ''
    if (!state || !sessionID) {
      if (state) {
        state.connectionStatus = 'closed'
        state.responseError = ''
      }
      return
    }

    await DisconnectWebSocketRPC(sessionID)
    ignoreWebSocketSession(sessionID)
    state.sessionId = ''
    state.connectionStatus = 'closed'
    state.responseError = ''
  }

  function clearWebSocketClientMessages(tabKey: string) {
    const tab = findWebSocketClientTab(tabKey)
    if (!tab?.webSocketClient) {
      return
    }
    tab.webSocketClient.messages = []
  }

  function closeTab(tabKey: string) {
    if (tabKey === CAPTURE_TAB_KEY) {
      return
    }

    const httpTab = findHTTPRequestTab(tabKey)
    const httpStreamSessionID = httpTab?.httpRequest?.streamSessionId.trim() ?? ''
    if (httpTab?.httpRequest && httpStreamSessionID) {
      httpTab.httpRequest.isStreaming = false
      httpTab.httpRequest.streamSessionId = ''
      discardHTTPRequestStreamRuntime(httpStreamSessionID, true)
      void DisconnectHTTPRequestStreamRPC(httpStreamSessionID).catch((error) => {
        console.error('Failed to disconnect HTTP request stream:', error)
      })
    }

    const wsTab = findWebSocketClientTab(tabKey)
    const pendingWsConnect = pendingWebSocketConnectCallsByTabKey.get(tabKey)
    if (pendingWsConnect) {
      pendingWebSocketConnectCallsByTabKey.delete(tabKey)
      void pendingWsConnect.cancel()
    }
    webSocketConnectGenerationByTabKey.delete(tabKey)
    if (wsTab?.webSocketClient?.sessionId) {
      ignoreWebSocketSession(wsTab.webSocketClient.sessionId)
      void DisconnectWebSocketRPC(wsTab.webSocketClient.sessionId).catch((error) => {
        console.error('Failed to disconnect websocket session:', error)
      })
    }

    const index = tabs.value.findIndex((tab) => tab.key === tabKey)
    if (index === -1) {
      return
    }

    const closingTab = tabs.value[index]
    const closingHistoryKey =
      closingTab?.type === 'history' ? (closingTab.historyKey ?? null) : null

    const wasActive = activeTabKey.value === tabKey
    tabs.value.splice(index, 1)

    if (closingHistoryKey) {
      if (historyStore.selectedKey === closingHistoryKey) {
        historyStore.selectedKey = null
        historyTrafficStore.reset()
      }
      historyFilterStore.clearFilters()

      if (
        categoryContextStore.activeContext?.kind === 'history' &&
        categoryContextStore.activeContext.historyKey === closingHistoryKey
      ) {
        categoryContextStore.clearActiveContext()
        categoryContextStore.clearSearch()
        categoryContextStore.resetExpandedKeys()
      }
    }

    if (!wasActive) {
      return
    }

    const nextTab = tabs.value[index] ?? tabs.value[index - 1]
    activeTabKey.value = nextTab?.key ?? CAPTURE_TAB_KEY
  }

  function closeHistoryTabByHistoryKey(historyKey: string) {
    const historyTab = tabs.value.find((tab) => tab.key === HISTORY_TAB_KEY)
    if (!historyTab) {
      return
    }
    if (historyTab.historyKey !== historyKey) {
      return
    }
    closeTab(HISTORY_TAB_KEY)
  }

  function closeApiTabsByAPIIds(apiIDs: string[]) {
    const apiIDSet = new Set(apiIDs.map((apiID) => apiID.trim()).filter(Boolean))
    if (apiIDSet.size === 0) {
      return
    }
    for (const tab of [...tabs.value]) {
      if (!tab.closable || !tab.apiId || !apiIDSet.has(tab.apiId)) {
        continue
      }
      closeTab(tab.key)
    }
  }

  function pruneHistoryTabs(existedKeys: Set<string>) {
    const historyTab = tabs.value.find((tab) => tab.key === HISTORY_TAB_KEY)
    if (!historyTab?.historyKey) {
      return
    }
    if (!existedKeys.has(historyTab.historyKey)) {
      closeTab(HISTORY_TAB_KEY)
    }
  }

  function closeAllHistoryTabs() {
    closeTab(HISTORY_TAB_KEY)
  }

  function isTabDirty(tabKey: string) {
    const tab = findTab(tabKey)
    if (!tab || (tab.type !== 'http-request' && tab.type !== 'websocket-client')) {
      return false
    }

    const snapshot = createSnapshotForTab(tab)
    if (tab.apiId) {
      return snapshot !== (tab.savedSnapshot ?? '')
    }
    if (!snapshot) {
      return false
    }
    return hasMeaningfulRequestDraft(tab)
  }

  function hasUnsavedNewRequestTabs() {
    return tabs.value.some((tab) => {
      if (tab.type !== 'http-request' && tab.type !== 'websocket-client') {
        return false
      }
      if (tab.apiId) {
        return false
      }
      return hasMeaningfulRequestDraft(tab)
    })
  }

  function hasDirtyClosableTabs() {
    return tabs.value.some((tab) => tab.closable && isTabDirty(tab.key))
  }

  function hasDirtyTabs(tabKeys: string[]) {
    const keySet = new Set(tabKeys)
    return tabs.value.some((tab) => tab.closable && keySet.has(tab.key) && isTabDirty(tab.key))
  }

  function closeTabs(tabKeys: string[], force: boolean) {
    for (const tabKey of tabKeys) {
      const tab = findTab(tabKey)
      if (!tab?.closable) {
        continue
      }
      if (!force && isTabDirty(tab.key)) {
        continue
      }
      closeTab(tab.key)
    }
  }

  function closeAllTabs(force: boolean) {
    closeTabs(
      tabs.value.filter((tab) => tab.closable).map((tab) => tab.key),
      force,
    )
  }

  function closeOtherTabs(tabKey: string, force: boolean) {
    closeTabs(
      tabs.value.filter((tab) => tab.closable && tab.key !== tabKey).map((tab) => tab.key),
      force,
    )
  }

  function closeRightTabs(tabKey: string, force: boolean) {
    const index = tabs.value.findIndex((tab) => tab.key === tabKey)
    if (index === -1) {
      return
    }
    closeTabs(
      tabs.value.slice(index + 1).filter((tab) => tab.closable).map((tab) => tab.key),
      force,
    )
  }

  async function saveRequestTabAsApi(tabKey: string, parentId: string, name: string) {
    const tab = findTab(tabKey)
    if (!tab) {
      throw new Error(`Tab not found: ${tabKey}`)
    }

    if (tab.type === 'http-request' && tab.httpRequest) {
      const request = await SaveHTTPRequestRPC(
        parentId,
        name,
        buildSavedHTTPRequestFromState(tab.httpRequest),
      )
      if (!request) {
        throw new Error('Save HTTP API returned no request')
      }
      if (request.http) {
        applySavedHTTPRequestToState(tab.httpRequest, request.http, request.name)
      }
      tab.title = request.name
      applySavedMetadataToTab(tab, request)
      return request
    }

    if (tab.type === 'websocket-client' && tab.webSocketClient) {
      const request = await SaveWebSocketRequestRPC(
        parentId,
        name,
        buildSavedWebSocketRequestFromState(tab.webSocketClient),
      )
      if (!request) {
        throw new Error('Save WebSocket API returned no request')
      }
      if (request.websocket) {
        applySavedWebSocketRequestToState(tab.webSocketClient, request.websocket, request.name)
      }
      tab.title = request.name
      applySavedMetadataToTab(tab, request)
      return request
    }

    throw new Error(`Tab ${tabKey} is not a request editor tab`)
  }

  async function saveExistingApiTab(tabKey: string) {
    const tab = findTab(tabKey)
    if (!tab?.apiId) {
      throw new Error(`Tab ${tabKey} is not linked to a saved API`)
    }

    if (tab.type === 'http-request' && tab.httpRequest) {
      const request = await UpdateHTTPRequestRPC(
        tab.apiId,
        buildSavedHTTPRequestFromState(tab.httpRequest),
      )
      if (!request) {
        throw new Error('Update HTTP API returned no request')
      }
      if (request.http) {
        applySavedHTTPRequestToState(tab.httpRequest, request.http, request.name)
      }
      tab.title = request.name
      applySavedMetadataToTab(tab, request)
      return request
    }

    if (tab.type === 'websocket-client' && tab.webSocketClient) {
      const request = await UpdateWebSocketRequestRPC(
        tab.apiId,
        buildSavedWebSocketRequestFromState(tab.webSocketClient),
      )
      if (!request) {
        throw new Error('Update WebSocket API returned no request')
      }
      if (request.websocket) {
        applySavedWebSocketRequestToState(tab.webSocketClient, request.websocket, request.name)
      }
      tab.title = request.name
      applySavedMetadataToTab(tab, request)
      return request
    }

    throw new Error(`Tab ${tabKey} is not a request editor tab`)
  }

  function updateRequestTabTitle(tabKey: string, rawUrl: string) {
    const tab = tabs.value.find((item) => item.key === tabKey)
    if (!tab || (tab.type !== 'http-request' && tab.type !== 'websocket-client')) {
      return
    }
    if (tab.apiId) {
      return
    }

    const nextTitle = deriveRequestTabTitle(rawUrl)
    tab.title = nextTitle

    if (tab.httpRequest) {
      tab.httpRequest.name = nextTitle
    }
    if (tab.webSocketClient) {
      tab.webSocketClient.name = nextTitle
    }
  }

  function reorderTabs(orderedMovableTabs: WorkspaceTab[]) {
    ensureCaptureTab()

    const captureTab = tabs.value.find((tab) => tab.key === CAPTURE_TAB_KEY) ?? buildCaptureTab()
    const movableTabsByKey = new Map(
      tabs.value.filter((tab) => tab.key !== CAPTURE_TAB_KEY).map((tab) => [tab.key, tab]),
    )
    const reorderedTabs: WorkspaceTab[] = []
    const seen = new Set<string>()

    for (const tab of orderedMovableTabs) {
      const currentTab = movableTabsByKey.get(tab.key)
      if (!currentTab || seen.has(currentTab.key)) {
        continue
      }

      reorderedTabs.push(currentTab)
      seen.add(currentTab.key)
    }

    for (const tab of tabs.value) {
      if (tab.key === CAPTURE_TAB_KEY || seen.has(tab.key)) {
        continue
      }

      reorderedTabs.push(tab)
    }

    tabs.value = [captureTab, ...reorderedTabs]
  }

  return {
    tabs,
    activeTab,
    activeTabKey,
    activeTrafficStore,
    activeTrafficSelectionCount,
    historyCount,
    activateTab,
    activateNextTab,
    activatePreviousTab,
    activateCapture,
    ensureCategoryTargetTab,
    openHistoryTab,
    createHttpRequestTab,
    createWebSocketClientTab,
    activateSavedApiTab,
    openSavedApi,
    openRequestEditorFromTraffic,
    registerHTTPRequestStream,
    disconnectHTTPRequestTab,
    connectWebSocketClientTab,
    cancelWebSocketConnection,
    sendWebSocketClientMessage,
    disconnectWebSocketClientTab,
    clearWebSocketClientMessages,
    clearRequestDraftCacheFileReferences,
    closeTab,
    closeTabs,
    closeAllTabs,
    closeOtherTabs,
    closeRightTabs,
    closeHistoryTabByHistoryKey,
    closeApiTabsByAPIIds,
    pruneHistoryTabs,
    closeAllHistoryTabs,
    hasUnsavedNewRequestTabs,
    hasDirtyClosableTabs,
    hasDirtyTabs,
    isTabDirty,
    saveRequestTabAsApi,
    saveExistingApiTab,
    updateRequestTabTitle,
    reorderTabs,
    initializeRuntimeEvents: bindRequestEditorRuntimeEvents,
    cleanupRuntimeEvents,
  }
})
