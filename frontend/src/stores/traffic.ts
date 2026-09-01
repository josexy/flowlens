import { defineStore } from 'pinia'
import { ref, shallowRef, triggerRef, watch } from 'vue'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  GetTraffic,
  GetTrafficBodyView,
  ClearTraffic,
  GetStatistics,
  DeleteTraffic,
  SetLiveTrafficDetail,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { onBatchedAppEvent } from '@/runtime/batchedAppEvents'
import {
  advanceTrafficEvictionWatermark,
  getTrafficCapabilities,
  isRawTCPTraffic,
  isTrafficEntryEvicted,
} from '@/utils/traffic'
import {
  applyTrafficEntryPatch,
  getNewTerminalHTTPMessageSides,
  TerminalBodyRefreshQueue,
  type TrafficEntryPatchPayload,
} from '@/utils/traffic-patch'
import {
  createTrafficTableColumns,
  type TrafficTableColumnKey,
} from '@/utils/traffic-table-columns'
import { createLatestOperationGuard } from '@/utils/latestOperation'

export interface SortConfig {
  key: TrafficTableColumnKey | null
  order: 'asc' | 'desc' | null
}

export const TRAFFIC_ENTRY_CAP = 5000

const TRAFFIC_ENTRY_BATCH_MS = 100
const LIVE_UPDATE_BATCH_MS = 100
const TRAFFIC_PATCH_BUFFER_PER_ENTRY = 16
const TRAFFIC_RESET_EVENT_NAME = 'traffic:reset'

type TrafficLiveUpdateKind = 'sse-chunk' | 'websocket-message' | 'websocket-truncated'

interface TrafficLiveUpdatePayload {
  trafficId: number
  kind: TrafficLiveUpdateKind
  offset?: number
  chunkBase64?: string
  messageIndex?: number
  message?: proxyservice.WebSocketMessage | null
}

interface InitialTrafficBuffer {
  entries: Map<number, proxyservice.TrafficEntry>
  patches: Map<number, TrafficEntryPatchPayload[]>
  deletedIds: Set<number>
  evictedThroughEntryId: number
}

interface NormalizedTrafficSnapshot {
  entries: proxyservice.TrafficEntry[]
  evictedThroughEntryId: number
}

export const useTrafficStore = defineStore('traffic', () => {
  const entries = shallowRef<proxyservice.TrafficEntry[]>([])
  const selectedEntry = ref<proxyservice.TrafficEntry | null>(null)
  const selectedEntryCount = ref(0)
  const selectedEntryBodyView = ref<proxyservice.TrafficBodyView | null>(null)
  const selectedEntryBodyViewLoading = ref(false)
  const trafficSurfaceActive = ref(true)
  const statistics = ref<proxyservice.TrafficStatistics>({
    total: 0,
    totalHttp: 0,
    totalWs: 0,
    totalTcp: 0,
  })

  const columns = ref(createTrafficTableColumns())

  const sortConfig = ref<SortConfig>({
    key: null,
    order: null,
  })

  const highlightMap = ref<Map<number, string>>(new Map()) // id -> CSS color

  const showDetailPanel = ref(false)
  const scrollTop = ref(0)
  const pendingFocusEntryId = ref<number | null>(null)

  const idMap = new Map<number, number>()
  const isLiveEntryEvictionPaused = shallowRef(false)
  const pausedLiveEntries = new Map<number, proxyservice.TrafficEntry>()
  let loadedBodyViewEntryId: number | null = null
  let bodyViewRequestToken = 0
  const terminalBodyRefreshQueue = new TerminalBodyRefreshQueue()
  let offTrafficEntry: (() => void) | null = null
  let offTrafficPatch: (() => void) | null = null
  let offTrafficLiveUpdate: (() => void) | null = null
  let offTrafficReset: (() => void) | null = null
  let trafficLifecycleGeneration = 0
  let trafficSnapshotEpoch = 0
  let trafficRecoveryEpoch = 0
  let evictedThroughEntryId = 0
  let initialTrafficBuffer: InitialTrafficBuffer | null = null
  let trafficEntryTimer: ReturnType<typeof setTimeout> | null = null
  const pendingTrafficEntries = new Map<number, proxyservice.TrafficEntry>()
  let liveUpdateTimer: ReturnType<typeof setTimeout> | null = null
  let pendingLiveUpdates: TrafficLiveUpdatePayload[] = []
  let liveCursorEntryId: number | null = null
  let liveSseByteOffset = 0
  let liveSseDecoder = new TextDecoder()
  let liveSseCursorReady = false
  let liveWsMessageCount = 0
  let liveRecoveryInFlight = false
  let liveSubscriptionReady = false
  let liveSubscriptionSyncRunning = false
  let desiredLiveDetailId = 0
  let appliedLiveDetailId = 0
  let trafficResyncRequested = false
  let trafficResyncInFlight = false
  const statisticsRequestGuard = createLatestOperationGuard()

  // Debounce timer: coalesce rapid statistics IPC calls into one per 300 ms
  let statsDebounceTimer: ReturnType<typeof setTimeout> | null = null

  function parseLiveUpdatePayload(value: unknown): TrafficLiveUpdatePayload | null {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      return null
    }
    const payload = value as Record<string, unknown>
    if (typeof payload.trafficId !== 'number') {
      return null
    }
    if (
      payload.kind !== 'sse-chunk' &&
      payload.kind !== 'websocket-message' &&
      payload.kind !== 'websocket-truncated'
    ) {
      return null
    }
    return payload as unknown as TrafficLiveUpdatePayload
  }

  function decodeBase64Bytes(value: string): Uint8Array {
    const binary = atob(value)
    const bytes = new Uint8Array(binary.length)
    for (let index = 0; index < binary.length; index++) {
      bytes[index] = binary.charCodeAt(index)
    }
    return bytes
  }

  function clearLiveUpdateTimer() {
    if (liveUpdateTimer === null) {
      return
    }
    clearTimeout(liveUpdateTimer)
    liveUpdateTimer = null
  }

  function resetLiveUpdateReconciliation() {
    clearLiveUpdateTimer()
    pendingLiveUpdates = []
    liveCursorEntryId = null
    liveSseByteOffset = 0
    liveSseDecoder = new TextDecoder()
    liveSseCursorReady = false
    liveWsMessageCount = 0
    liveRecoveryInFlight = false
  }

  function initializeLiveCursors(entryId: number, bodyView: proxyservice.TrafficBodyView) {
    liveCursorEntryId = entryId
    liveSseDecoder = new TextDecoder()
    liveSseCursorReady = bodyView.rspBodyEnc !== 'base64'
    liveSseByteOffset = liveSseCursorReady
      ? new TextEncoder().encode(bodyView.rspBody || '').byteLength
      : 0
    liveWsMessageCount = bodyView.wsMsgs?.length ?? 0
  }

  function prepareBase64SseSnapshot(bodyView: proxyservice.TrafficBodyView) {
    if (liveSseCursorReady || bodyView.rspBodyEnc !== 'base64') {
      return bodyView
    }
    const bytes = decodeBase64Bytes(bodyView.rspBody || '')
    liveSseDecoder = new TextDecoder()
    const decodedBody = liveSseDecoder.decode(bytes, { stream: true })
    liveSseByteOffset = bytes.byteLength
    liveSseCursorReady = true
    return {
      ...bodyView,
      rspBody: decodedBody,
      rspBodyEnc: '',
    }
  }

  function setDesiredLiveDetailId(id: number) {
    desiredLiveDetailId = id
    if (!liveSubscriptionReady || liveSubscriptionSyncRunning) {
      return
    }
    void syncLiveDetailSubscription()
  }

  async function syncLiveDetailSubscription() {
    if (!liveSubscriptionReady || liveSubscriptionSyncRunning) {
      return
    }
    liveSubscriptionSyncRunning = true
    try {
      while (appliedLiveDetailId !== desiredLiveDetailId) {
        const targetId = desiredLiveDetailId
        await SetLiveTrafficDetail(targetId)
        appliedLiveDetailId = targetId
      }
    } catch (error) {
      console.error('Failed to update live traffic detail subscription:', error)
      // Avoid a tight retry loop. The next detail target change will retry.
      appliedLiveDetailId = desiredLiveDetailId
    } finally {
      liveSubscriptionSyncRunning = false
      if (liveSubscriptionReady && appliedLiveDetailId !== desiredLiveDetailId) {
        // A target change may have arrived while the previous IPC call was in flight.
        void syncLiveDetailSubscription()
      }
    }
  }

  async function selectEntry(entry: proxyservice.TrafficEntry | null) {
    const previousEntryId = selectedEntry.value?.id ?? null
    selectedEntry.value = entry
    if (!entry) {
      terminalBodyRefreshQueue.cancel()
      bodyViewRequestToken++
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
      showDetailPanel.value = false
      resetLiveUpdateReconciliation()
      return
    }
    if (previousEntryId !== entry.id) {
      terminalBodyRefreshQueue.activate(entry.id)
      bodyViewRequestToken++
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
      resetLiveUpdateReconciliation()
    }
  }

  async function ensureSelectedEntryBodyViewLoaded(force = false) {
    const entry = selectedEntry.value
    if (
      !entry ||
      !showDetailPanel.value ||
      !trafficSurfaceActive.value ||
      !getTrafficCapabilities(entry).canLoadBody
    ) {
      return
    }
    if (!force && selectedEntryBodyView.value && loadedBodyViewEntryId === entry.id) {
      return
    }

    const requestToken = ++bodyViewRequestToken
    selectedEntryBodyViewLoading.value = true
    try {
      const entryWithBody = await GetTrafficBodyView(entry.id)
      if (!entryWithBody) {
        throw new Error(`Traffic body view ${entry.id} was not returned`)
      }
      if (
        requestToken !== bodyViewRequestToken ||
        !showDetailPanel.value ||
        !trafficSurfaceActive.value ||
        selectedEntry.value?.id !== entry.id
      ) {
        return
      }

      selectedEntryBodyView.value = entryWithBody
      loadedBodyViewEntryId = entry.id
      initializeLiveCursors(entry.id, entryWithBody)
      if (pendingLiveUpdates.length > 0) {
        scheduleLiveUpdateFlush(0)
      }
    } catch (error) {
      if (requestToken !== bodyViewRequestToken || selectedEntry.value?.id !== entry.id) {
        return
      }
      if (force && selectedEntryBodyView.value && loadedBodyViewEntryId === entry.id) {
        initializeLiveCursors(entry.id, selectedEntryBodyView.value)
      } else {
        selectedEntryBodyView.value = null
        loadedBodyViewEntryId = null
      }
      console.error('Failed to get traffic entry with body:', error)
    } finally {
      if (requestToken === bodyViewRequestToken) {
        selectedEntryBodyViewLoading.value = false
        if (pendingLiveUpdates.length > 0) {
          scheduleLiveUpdateFlush(0)
        }
        if (
          terminalBodyRefreshQueue.completeLoad(entry.id) &&
          selectedEntry.value?.id === entry.id &&
          showDetailPanel.value &&
          trafficSurfaceActive.value
        ) {
          void ensureSelectedEntryBodyViewLoaded(true)
        }
      }
    }
  }

  function refreshBodyViewForTerminalMessages(
    previous: proxyservice.TrafficEntry,
    next: proxyservice.TrafficEntry,
  ) {
    const sides = getNewTerminalHTTPMessageSides(previous, next)
    if (
      sides.length === 0 ||
      selectedEntry.value?.id !== next.id ||
      !showDetailPanel.value ||
      !trafficSurfaceActive.value ||
      !getTrafficCapabilities(next).canLoadBody
    ) {
      return
    }

    terminalBodyRefreshQueue.activate(next.id)
    if (
      terminalBodyRefreshQueue.request(
        next.id,
        sides,
        selectedEntryBodyViewLoading.value,
      )
    ) {
      void ensureSelectedEntryBodyViewLoaded(true)
    }
  }

  async function getBodyView(
    entryId: number,
    historyKey?: string | null,
  ): Promise<proxyservice.TrafficBodyView | null> {
    void historyKey
    const entryIndex = idMap.get(entryId)
    if (entryIndex !== undefined && isRawTCPTraffic(entries.value[entryIndex])) {
      return null
    }
    try {
      return await GetTrafficBodyView(entryId)
    } catch (error) {
      console.error('Failed to get traffic body view:', error)
      return null
    }
  }

  function scheduleLiveUpdateFlush(delay = LIVE_UPDATE_BATCH_MS) {
    if (liveUpdateTimer !== null) {
      return
    }
    liveUpdateTimer = setTimeout(() => {
      liveUpdateTimer = null
      flushPendingLiveUpdates()
    }, delay)
  }

  function recoverSelectedEntryBodyView() {
    if (liveRecoveryInFlight) {
      return
    }
    liveRecoveryInFlight = true
    void ensureSelectedEntryBodyViewLoaded(true).finally(() => {
      liveRecoveryInFlight = false
    })
  }

  function flushPendingLiveUpdates() {
    const entryId = selectedEntry.value?.id
    if (
      !entryId ||
      !showDetailPanel.value ||
      !trafficSurfaceActive.value ||
      pendingLiveUpdates.length === 0
    ) {
      return
    }
    const currentBodyView = selectedEntryBodyView.value
    if (
      !currentBodyView ||
      loadedBodyViewEntryId !== entryId ||
      selectedEntryBodyViewLoading.value
    ) {
      return
    }
    if (liveCursorEntryId !== entryId) {
      initializeLiveCursors(entryId, currentBodyView)
    }

    const batch = pendingLiveUpdates
    pendingLiveUpdates = []
    const sseUpdates: TrafficLiveUpdatePayload[] = []
    const websocketUpdates: TrafficLiveUpdatePayload[] = []
    let websocketTruncated = false
    for (const update of batch) {
      if (
        update.kind === 'sse-chunk' &&
        typeof update.offset === 'number' &&
        typeof update.chunkBase64 === 'string'
      ) {
        sseUpdates.push(update)
      } else if (
        update.kind === 'websocket-message' &&
        typeof update.messageIndex === 'number' &&
        !!update.message
      ) {
        websocketUpdates.push(update)
      } else if (update.kind === 'websocket-truncated') {
        websocketTruncated = true
      }
    }
    if (sseUpdates.length > 1) {
      sseUpdates.sort((left, right) => left.offset! - right.offset!)
    }
    if (websocketUpdates.length > 1) {
      websocketUpdates.sort((left, right) => left.messageIndex! - right.messageIndex!)
    }

    let nextBodyView = currentBodyView
    let hasChanges = false

    try {
      if (sseUpdates.length > 0) {
        const snapshotWasBase64 = nextBodyView.rspBodyEnc === 'base64'
        nextBodyView = prepareBase64SseSnapshot(nextBodyView)
        if (snapshotWasBase64 && nextBodyView.rspBodyEnc !== 'base64') {
          hasChanges = true
        }
        let responseBody = nextBodyView.rspBody || ''
        const appendedText: string[] = []
        for (const update of sseUpdates) {
          const chunk = decodeBase64Bytes(update.chunkBase64!)
          const startOffset = update.offset!
          const endOffset = startOffset + chunk.byteLength
          if (endOffset <= liveSseByteOffset) {
            continue
          }
          if (startOffset > liveSseByteOffset) {
            throw new Error(
              `SSE live update gap: expected ${liveSseByteOffset}, received ${startOffset}`,
            )
          }
          const overlap = Math.max(0, liveSseByteOffset - startOffset)
          appendedText.push(
            liveSseDecoder.decode(chunk.subarray(overlap), { stream: true }),
          )
          liveSseByteOffset = endOffset
          hasChanges = true
        }
        responseBody += appendedText.join('')
        nextBodyView = {
          ...nextBodyView,
          rspBody: responseBody,
          rspBodyEnc: '',
        }
      }

      if (websocketUpdates.length > 0) {
        const messages = nextBodyView.wsMsgs ?? []
        if (!nextBodyView.wsMsgs) {
          nextBodyView.wsMsgs = messages
        }
        for (const update of websocketUpdates) {
          const messageIndex = update.messageIndex!
          if (messageIndex < liveWsMessageCount) {
            continue
          }
          if (messageIndex > liveWsMessageCount) {
            throw new Error(
              `WebSocket live update gap: expected ${liveWsMessageCount}, received ${messageIndex}`,
            )
          }
          messages.push(update.message!)
          liveWsMessageCount++
          hasChanges = true
        }
      }

      if (websocketTruncated) {
        nextBodyView = {
          ...nextBodyView,
          wsMsgsTruncated: true,
        }
        hasChanges = true
      }
    } catch (error) {
      console.warn('Live traffic updates lost ordering; reloading body snapshot:', error)
      recoverSelectedEntryBodyView()
      return
    }

    if (hasChanges && selectedEntry.value?.id === entryId && showDetailPanel.value) {
      selectedEntryBodyView.value = nextBodyView
    }
  }

  function handleTrafficLiveUpdate(value: unknown) {
    const update = parseLiveUpdatePayload(value)
    const entryId = selectedEntry.value?.id
    if (
      !update ||
      !entryId ||
      !showDetailPanel.value ||
      !trafficSurfaceActive.value ||
      update.trafficId !== entryId
    ) {
      return
    }
    pendingLiveUpdates.push(update)
    scheduleLiveUpdateFlush()
  }

  function resetState() {
    statisticsRequestGuard.invalidate()
    trafficRecoveryEpoch++
    trafficResyncRequested = false
    invalidateInitialTrafficSnapshot()
    bodyViewRequestToken++
    terminalBodyRefreshQueue.cancel()
    resetTrafficEntryBatch()
    entries.value = []
    idMap.clear()
    evictedThroughEntryId = 0
    selectedEntry.value = null
    selectedEntryCount.value = 0
    selectedEntryBodyView.value = null
    selectedEntryBodyViewLoading.value = false
    loadedBodyViewEntryId = null
    statistics.value = {
      total: 0,
      totalHttp: 0,
      totalWs: 0,
      totalTcp: 0,
    }
    highlightMap.value.clear()
    showDetailPanel.value = false
    scrollTop.value = 0
    pendingFocusEntryId.value = null
    pausedLiveEntries.clear()
    resetLiveUpdateReconciliation()
  }

  async function clearAll() {
    trafficRecoveryEpoch++
    invalidateInitialTrafficSnapshot()
    await ClearTraffic()
    await updateStatistics()
  }

  async function updateStatistics() {
    const requestToken = statisticsRequestGuard.begin()
    try {
      const nextStatistics = await GetStatistics()
      if (statisticsRequestGuard.isCurrent(requestToken)) {
        statistics.value = nextStatistics
      }
    } catch (error) {
      if (statisticsRequestGuard.isCurrent(requestToken)) {
        console.error('Failed to get statistics:', error)
      }
    }
  }

  function scheduleUpdateStatistics() {
    if (statsDebounceTimer !== null) {
      clearTimeout(statsDebounceTimer)
    }
    statsDebounceTimer = setTimeout(async () => {
      statsDebounceTimer = null
      await updateStatistics()
    }, 300)
  }

  function rebuildIdMap() {
    idMap.clear()
    entries.value.forEach((entry, index) => idMap.set(entry.id, index))
  }

  function reconcileSelectedEntry() {
    const selectedId = selectedEntry.value?.id
    if (!selectedId) return
    const selectedIndex = idMap.get(selectedId)
    void selectEntry(selectedIndex === undefined ? null : entries.value[selectedIndex]!)
  }

  function normalizeTrafficEntries(
    snapshot: Array<proxyservice.TrafficEntry | null> | null | undefined,
  ): NormalizedTrafficSnapshot {
    const validEntries = (snapshot ?? []).filter(
      (entry): entry is proxyservice.TrafficEntry => entry !== null,
    )
    if (validEntries.length <= TRAFFIC_ENTRY_CAP) {
      return {
        entries: validEntries,
        evictedThroughEntryId: 0,
      }
    }
    const overflow = validEntries.length - TRAFFIC_ENTRY_CAP
    return {
      entries: validEntries.slice(overflow),
      evictedThroughEntryId: advanceTrafficEvictionWatermark(
        0,
        validEntries.slice(0, overflow).map((entry) => entry.id),
      ),
    }
  }

  function invalidateInitialTrafficSnapshot() {
    trafficSnapshotEpoch++
    initialTrafficBuffer?.entries.clear()
    initialTrafficBuffer?.patches.clear()
    initialTrafficBuffer?.deletedIds.clear()
    if (initialTrafficBuffer) {
      initialTrafficBuffer.evictedThroughEntryId = 0
    }
  }

  function trafficEntryRevision(entry: proxyservice.TrafficEntry) {
    return Number.isSafeInteger(entry.revision) ? (entry.revision ?? 0) : 0
  }

  function processInfoProgress(process: proxyservice.ProcessInfo | null | undefined) {
    if (!process) return -1
    if (process.status === 'pending') return 0
    if (process.status === 'resolved' && process.iconKey) return 2
    return 1
  }

  function mergeInitialHTTPMessage(
    snapshot: proxyservice.HTTPMessage | null | undefined,
    buffered: proxyservice.HTTPMessage | null | undefined,
  ): proxyservice.HTTPMessage | null | undefined {
    if (!snapshot) return buffered
    if (!buffered) return snapshot

    const snapshotHasTrailers = (snapshot.trailerFields?.length ?? 0) > 0
    const bufferedHasTrailers = (buffered.trailerFields?.length ?? 0) > 0
    const useSnapshotTrailers = snapshotHasTrailers || !bufferedHasTrailers
    const snapshotHasHeaderFields = snapshot.headerFields !== null && snapshot.headerFields !== undefined
    return {
      ...snapshot,
      proto: snapshot.proto || buffered.proto,
      headerFields: snapshotHasHeaderFields ? snapshot.headerFields : buffered.headerFields,
      headersTruncated: snapshotHasHeaderFields
        ? snapshot.headersTruncated
        : buffered.headersTruncated,
      headerOrderUnavailable: snapshotHasHeaderFields
        ? snapshot.headerOrderUnavailable
        : buffered.headerOrderUnavailable,
      trailerFields: useSnapshotTrailers ? snapshot.trailerFields : buffered.trailerFields,
      trailersTruncated: useSnapshotTrailers
        ? snapshot.trailersTruncated
        : buffered.trailersTruncated,
      trailerOrderUnavailable: useSnapshotTrailers
        ? snapshot.trailerOrderUnavailable
        : buffered.trailerOrderUnavailable,
    }
  }

  function mergeInitialTrafficEntry(
    snapshot: proxyservice.TrafficEntry,
    buffered: proxyservice.TrafficEntry,
  ): proxyservice.TrafficEntry {
    const snapshotRevision = trafficEntryRevision(snapshot)
    const bufferedRevision = trafficEntryRevision(buffered)
    if (snapshotRevision > 0 || bufferedRevision > 0) {
      return bufferedRevision > snapshotRevision ? buffered : snapshot
    }

    // Snapshot reads and event delivery are not atomic, so keep fields that only move forward.
    const snapshotProcess = snapshot.metadata?.process
    const bufferedProcess = buffered.metadata?.process
    let metadata = snapshot.metadata ?? buffered.metadata
    if (
      metadata &&
      processInfoProgress(bufferedProcess) > processInfoProgress(snapshotProcess)
    ) {
      metadata = { ...metadata, process: bufferedProcess }
    }

    return {
      ...snapshot,
      statusCode: snapshot.statusCode || buffered.statusCode,
      status: snapshot.status || buffered.status,
      metadata,
      rawTcp: snapshot.rawTcp ?? buffered.rawTcp,
      request: mergeInitialHTTPMessage(snapshot.request, buffered.request),
      response: mergeInitialHTTPMessage(snapshot.response, buffered.response),
      error: snapshot.error ?? buffered.error,
    }
  }

  function mergeRecoveredTrafficSnapshot(snapshot: NormalizedTrafficSnapshot) {
    const currentById = new Map(entries.value.map((entry) => [entry.id, entry]))
    const recoveredEntries = snapshot.entries.map((entry) => {
      const current = currentById.get(entry.id)
      currentById.delete(entry.id)
      return current ? mergeInitialTrafficEntry(entry, current) : entry
    })
    const newestRecoveredId = recoveredEntries.reduce(
      (newest, entry) => Math.max(newest, entry.id),
      0,
    )

    for (const current of entries.value) {
      if (!currentById.has(current.id)) continue
      if (newestRecoveredId !== 0 && current.id <= newestRecoveredId) continue
      recoveredEntries.push(current)
    }

    if (recoveredEntries.length > TRAFFIC_ENTRY_CAP) {
      const evicted = recoveredEntries.splice(0, recoveredEntries.length - TRAFFIC_ENTRY_CAP)
      evictedThroughEntryId = advanceTrafficEvictionWatermark(
        evictedThroughEntryId,
        evicted.map((entry) => entry.id),
      )
    }
    evictedThroughEntryId = advanceTrafficEvictionWatermark(evictedThroughEntryId, [
      snapshot.evictedThroughEntryId,
    ])
    entries.value = recoveredEntries
    rebuildIdMap()
    reconcileSelectedEntry()
    scheduleUpdateStatistics()
  }

  function requestTrafficSnapshotRecovery() {
    trafficResyncRequested = true
    if (trafficResyncInFlight || initialTrafficBuffer || !trafficSurfaceActive.value) return
    void recoverTrafficSnapshot()
  }

  async function recoverTrafficSnapshot() {
    if (trafficResyncInFlight || initialTrafficBuffer || !trafficSurfaceActive.value) return
    trafficResyncInFlight = true
    try {
      while (
        trafficResyncRequested &&
        !initialTrafficBuffer &&
        trafficSurfaceActive.value
      ) {
        trafficResyncRequested = false
        const generation = trafficLifecycleGeneration
        const recoveryEpoch = trafficRecoveryEpoch
        const snapshot = normalizeTrafficEntries(await GetTraffic())
        if (
          generation !== trafficLifecycleGeneration ||
          recoveryEpoch !== trafficRecoveryEpoch
        ) {
          return
        }
        if (!trafficSurfaceActive.value) {
          trafficResyncRequested = true
          return
        }
        mergeRecoveredTrafficSnapshot(snapshot)
      }
    } catch (error) {
      console.error('Failed to recover dropped traffic events:', error)
    } finally {
      trafficResyncInFlight = false
      if (
        trafficResyncRequested &&
        !initialTrafficBuffer &&
        trafficSurfaceActive.value
      ) {
        void recoverTrafficSnapshot()
      }
    }
  }

  function recoverDroppedLiveUpdates() {
    if (!trafficSurfaceActive.value) return
    resetLiveUpdateReconciliation()
    recoverSelectedEntryBodyView()
  }

  function setTrafficSurfaceActive(active: boolean) {
    if (trafficSurfaceActive.value === active) return
    trafficSurfaceActive.value = active
    if (!active) {
      trafficResyncRequested = true
      terminalBodyRefreshQueue.cancel()
      bodyViewRequestToken++
      selectedEntryBodyViewLoading.value = false
      resetLiveUpdateReconciliation()
      return
    }
    requestTrafficSnapshotRecovery()
  }

  function setBoundedTrafficEntry(
    target: Map<number, proxyservice.TrafficEntry>,
    entry: proxyservice.TrafficEntry,
  ): proxyservice.TrafficEntry | null {
    let evictedEntry: proxyservice.TrafficEntry | null = null
    if (!target.has(entry.id) && target.size >= TRAFFIC_ENTRY_CAP) {
      const oldestEntryId = target.keys().next().value
      if (oldestEntryId !== undefined) {
        evictedEntry = target.get(oldestEntryId) ?? null
        target.delete(oldestEntryId)
      }
    }
    target.set(entry.id, entry)
    return evictedEntry
  }

  function addBoundedDeletedEntryId(target: Set<number>, id: number) {
    if (!target.has(id) && target.size >= TRAFFIC_ENTRY_CAP) {
      const oldestEntryId = target.values().next().value
      if (oldestEntryId !== undefined) {
        target.delete(oldestEntryId)
      }
    }
    target.add(id)
  }

  function bufferInitialTrafficPatch(
    buffer: InitialTrafficBuffer,
    patch: TrafficEntryPatchPayload,
  ) {
    if (buffer.deletedIds.has(patch.trafficId)) return
    let patches = buffer.patches.get(patch.trafficId)
    if (!patches) {
      if (buffer.patches.size >= TRAFFIC_ENTRY_CAP) {
        const oldestEntryId = buffer.patches.keys().next().value
        if (oldestEntryId !== undefined) buffer.patches.delete(oldestEntryId)
      }
      patches = []
      buffer.patches.set(patch.trafficId, patches)
    }
    patches.push(patch)
    if (patches.length > TRAFFIC_PATCH_BUFFER_PER_ENTRY) {
      patches.splice(0, patches.length - TRAFFIC_PATCH_BUFFER_PER_ENTRY)
    }
  }

  function isEntryOutsideCurrentWindow(entry: proxyservice.TrafficEntry) {
    return isTrafficEntryEvicted(entry.id, evictedThroughEntryId)
  }

  function resetTrafficEntryBatch() {
    if (trafficEntryTimer !== null) {
      clearTimeout(trafficEntryTimer)
      trafficEntryTimer = null
    }
    pendingTrafficEntries.clear()
  }

  function applyTrafficEntryBatch(batch: proxyservice.TrafficEntry[]) {
    if (batch.length === 0) {
      return
    }

    const list = entries.value
    let changed = false

    for (const entry of batch) {
      const index = idMap.get(entry.id)
      if (index !== undefined) {
        const current = list[index]
        if (
          current &&
          trafficEntryRevision(current) > 0 &&
          trafficEntryRevision(entry) < trafficEntryRevision(current)
        ) {
          continue
        }
        list[index] = entry
        changed = true
        if (selectedEntry.value?.id === entry.id) {
          selectedEntry.value = entry
        }
        continue
      }

      if (isEntryOutsideCurrentWindow(entry)) {
        continue
      }
      if (isLiveEntryEvictionPaused.value && list.length >= TRAFFIC_ENTRY_CAP) {
        queuePausedLiveEntry(entry)
        continue
      }

      idMap.set(entry.id, list.length)
      list.push(entry)
      changed = true
    }

    if (list.length > TRAFFIC_ENTRY_CAP) {
      const evictedEntries = list.splice(0, list.length - TRAFFIC_ENTRY_CAP)
      evictedThroughEntryId = advanceTrafficEvictionWatermark(
        evictedThroughEntryId,
        evictedEntries.map((entry) => entry.id),
      )
      rebuildIdMap()
    }

    if (changed) {
      triggerRef(entries)
    }
    scheduleUpdateStatistics()
  }

  function flushPendingTrafficEntries() {
    if (trafficEntryTimer !== null) clearTimeout(trafficEntryTimer)
    trafficEntryTimer = null
    if (pendingTrafficEntries.size === 0) {
      return
    }

    const batch = Array.from(pendingTrafficEntries.values())
    pendingTrafficEntries.clear()
    applyTrafficEntryBatch(batch)
  }

  function addOrUpdateEntry(entry: proxyservice.TrafficEntry) {
    const pending = pendingTrafficEntries.get(entry.id)
    if (
      pending &&
      trafficEntryRevision(pending) > 0 &&
      trafficEntryRevision(entry) <= trafficEntryRevision(pending)
    ) {
      return
    }
    const currentIndex = idMap.get(entry.id)
    const current = currentIndex === undefined ? undefined : entries.value[currentIndex]
    if (
      current &&
      trafficEntryRevision(current) > 0 &&
      trafficEntryRevision(entry) <= trafficEntryRevision(current)
    ) {
      return
    }
    const evictedEntry = setBoundedTrafficEntry(pendingTrafficEntries, entry)
    if (evictedEntry && !idMap.has(evictedEntry.id)) {
      evictedThroughEntryId = advanceTrafficEvictionWatermark(evictedThroughEntryId, [
        evictedEntry.id,
      ])
    }
    if (trafficEntryTimer !== null) {
      return
    }
    trafficEntryTimer = setTimeout(flushPendingTrafficEntries, TRAFFIC_ENTRY_BATCH_MS)
  }

  function updateEntryPatch(patch: TrafficEntryPatchPayload) {
    const pending = pendingTrafficEntries.get(patch.trafficId)
    if (pending) {
      const updated = applyTrafficEntryPatch(pending, patch)
      if (updated !== pending) {
        pendingTrafficEntries.set(patch.trafficId, updated)
        refreshBodyViewForTerminalMessages(pending, updated)
      }
      return
    }
    const index = idMap.get(patch.trafficId)
    if (index === undefined) {
      return
    }
    const entry = entries.value[index]
    if (entry) {
      const updated = applyTrafficEntryPatch(entry, patch)
      if (updated !== entry) {
        refreshBodyViewForTerminalMessages(entry, updated)
        addOrUpdateEntry(updated)
      }
    }
  }

  function queuePausedLiveEntry(entry: proxyservice.TrafficEntry) {
    if (isEntryOutsideCurrentWindow(entry)) {
      return
    }
    if (!pausedLiveEntries.has(entry.id) && pausedLiveEntries.size >= TRAFFIC_ENTRY_CAP) {
      const oldestQueuedEntryId = pausedLiveEntries.keys().next().value
      if (oldestQueuedEntryId !== undefined) {
        evictedThroughEntryId = advanceTrafficEvictionWatermark(evictedThroughEntryId, [
          oldestQueuedEntryId,
        ])
        pausedLiveEntries.delete(oldestQueuedEntryId)
      }
    }
    pausedLiveEntries.set(entry.id, entry)
  }

  function pauseLiveEntryEviction() {
    isLiveEntryEvictionPaused.value = true
  }

  function resumeLiveEntryEviction() {
    isLiveEntryEvictionPaused.value = false
    if (pausedLiveEntries.size === 0) return

    const queuedEntries = Array.from(pausedLiveEntries.values())
    pausedLiveEntries.clear()
    applyTrafficEntryBatch(queuedEntries)
  }

  async function deleteEntries(ids: number[]): Promise<void> {
    const idSet = new Set(ids.filter((id) => typeof id === 'number' && Number.isFinite(id)))
    if (idSet.size === 0) {
      return
    }

    const recoverAfterDelete = trafficResyncRequested || trafficResyncInFlight
    trafficRecoveryEpoch++

    for (const id of idSet) {
      initialTrafficBuffer?.entries.delete(id)
      initialTrafficBuffer?.patches.delete(id)
      if (initialTrafficBuffer) {
        addBoundedDeletedEntryId(initialTrafficBuffer.deletedIds, id)
      }
      pendingTrafficEntries.delete(id)
      pausedLiveEntries.delete(id)
      highlightMap.value.delete(id)
    }
    entries.value = entries.value.filter((entry) => !idSet.has(entry.id))
    rebuildIdMap()

    if (selectedEntry.value && idSet.has(selectedEntry.value.id)) {
      terminalBodyRefreshQueue.cancel()
      bodyViewRequestToken++
      selectedEntry.value = null
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
      showDetailPanel.value = false
      resetLiveUpdateReconciliation()
    }
    await DeleteTraffic(Array.from(idSet))
    if (recoverAfterDelete) {
      requestTrafficSnapshotRecovery()
    }
    scheduleUpdateStatistics()
  }

  function deleteEntry(id: number) {
    return deleteEntries([id])
  }

  function setHighlight(id: number, color: string | null) {
    if (color) {
      highlightMap.value.set(id, color)
    } else {
      highlightMap.value.delete(id)
    }
  }

  function focusEntryById(id: number) {
    pendingFocusEntryId.value = id
  }

  function clearPendingFocusEntryId() {
    pendingFocusEntryId.value = null
  }

  async function loadInitialTrafficSnapshot(generation: number) {
    while (generation === trafficLifecycleGeneration) {
      const snapshotEpoch = trafficSnapshotEpoch
      const snapshot = normalizeTrafficEntries(await GetTraffic())
      if (generation !== trafficLifecycleGeneration) {
        return null
      }
      if (snapshotEpoch === trafficSnapshotEpoch) {
        return snapshot
      }
    }
    return null
  }

  async function initialize() {
    const generation = ++trafficLifecycleGeneration
    resetTrafficEntryBatch()
    offTrafficEntry?.()
    offTrafficEntry = null
    offTrafficPatch?.()
    offTrafficPatch = null
    offTrafficLiveUpdate?.()
    offTrafficLiveUpdate = null
    offTrafficReset?.()
    offTrafficReset = null
    liveSubscriptionReady = false

    const buffer: InitialTrafficBuffer = {
      entries: new Map(),
      patches: new Map(),
      deletedIds: new Set(),
      evictedThroughEntryId: 0,
    }
    initialTrafficBuffer = buffer
    let buffering = true

    const offReset = onBatchedAppEvent(TRAFFIC_RESET_EVENT_NAME, () => {
      if (generation !== trafficLifecycleGeneration) return
      resetState()
    })
    const offEntry = onBatchedAppEvent('traffic:entry', (data) => {
      if (generation !== trafficLifecycleGeneration) return
      if (!trafficSurfaceActive.value) {
        trafficResyncRequested = true
        return
      }
      const entry = data as proxyservice.TrafficEntry
      if (buffering) {
        if (!buffer.deletedIds.has(entry.id)) {
          const evictedEntry = setBoundedTrafficEntry(buffer.entries, entry)
          if (evictedEntry) {
            buffer.patches.delete(evictedEntry.id)
            buffer.evictedThroughEntryId = advanceTrafficEvictionWatermark(
              buffer.evictedThroughEntryId,
              [evictedEntry.id],
            )
          }
        }
        return
      }
      addOrUpdateEntry(entry)
    }, requestTrafficSnapshotRecovery)
    const offPatch = onBatchedAppEvent('traffic:patch', (data) => {
      if (generation !== trafficLifecycleGeneration) return
      if (!trafficSurfaceActive.value) {
        trafficResyncRequested = true
        return
      }
      const patch = data as TrafficEntryPatchPayload
      if (buffering) {
        bufferInitialTrafficPatch(buffer, patch)
        return
      }
      updateEntryPatch(patch)
    }, requestTrafficSnapshotRecovery)
    const offLiveUpdate = onBatchedAppEvent('traffic:live-update', (data) => {
      if (generation === trafficLifecycleGeneration) {
        handleTrafficLiveUpdate(data)
      }
    }, recoverDroppedLiveUpdates)
    offTrafficEntry = offEntry
    offTrafficPatch = offPatch
    offTrafficLiveUpdate = offLiveUpdate
    offTrafficReset = offReset

    const releaseInitializationListeners = () => {
      if (offTrafficEntry === offEntry) {
        offEntry()
        offTrafficEntry = null
      }
      if (offTrafficPatch === offPatch) {
        offPatch()
        offTrafficPatch = null
      }
      if (offTrafficLiveUpdate === offLiveUpdate) {
        offLiveUpdate()
        offTrafficLiveUpdate = null
      }
      if (offTrafficReset === offReset) {
        offReset()
        offTrafficReset = null
      }
    }

    try {
      const snapshot = await loadInitialTrafficSnapshot(generation)
      if (!snapshot || generation !== trafficLifecycleGeneration) {
        releaseInitializationListeners()
        return generation
      }

      evictedThroughEntryId = advanceTrafficEvictionWatermark(evictedThroughEntryId, [
        snapshot.evictedThroughEntryId,
        buffer.evictedThroughEntryId,
      ])
      entries.value = snapshot.entries.filter((entry) => !buffer.deletedIds.has(entry.id))
      rebuildIdMap()

      const bufferedEntries: proxyservice.TrafficEntry[] = []
      for (const entry of buffer.entries.values()) {
        if (buffer.deletedIds.has(entry.id)) continue
        const snapshotIndex = idMap.get(entry.id)
        if (snapshotIndex === undefined && isEntryOutsideCurrentWindow(entry)) {
          continue
        }
        bufferedEntries.push(
          snapshotIndex === undefined
            ? entry
            : mergeInitialTrafficEntry(entries.value[snapshotIndex]!, entry),
        )
      }
      buffering = false
      if (initialTrafficBuffer === buffer) initialTrafficBuffer = null
      applyTrafficEntryBatch(bufferedEntries)
      for (const [trafficId, patches] of buffer.patches) {
        if (buffer.deletedIds.has(trafficId)) continue
        for (const patch of patches) updateEntryPatch(patch)
      }
      flushPendingTrafficEntries()
      reconcileSelectedEntry()

      await updateStatistics()
    } catch (error) {
      buffering = false
      releaseInitializationListeners()
      if (generation !== trafficLifecycleGeneration) {
        return generation
      }
      if (initialTrafficBuffer === buffer) initialTrafficBuffer = null
      resetTrafficEntryBatch()
      throw error
    }

    if (generation !== trafficLifecycleGeneration) {
      releaseInitializationListeners()
      return generation
    }

    liveSubscriptionReady = true
    setDesiredLiveDetailId(
      selectedEntry.value &&
        showDetailPanel.value &&
        getTrafficCapabilities(selectedEntry.value).canSubscribeLiveDetail
        ? selectedEntry.value.id
        : 0,
    )
    if (trafficResyncRequested) {
      void recoverTrafficSnapshot()
    }
    return generation
  }

  watch(
    [() => selectedEntry.value?.id ?? null, showDetailPanel, trafficSurfaceActive],
    ([entryId, isDetailPanelShown, isSurfaceActive], previous) => {
      const entry = selectedEntry.value
      const canShowLiveDetail = getTrafficCapabilities(entry).canSubscribeLiveDetail
      setDesiredLiveDetailId(
        entryId && isDetailPanelShown && isSurfaceActive && canShowLiveDetail ? entryId : 0,
      )
      if (!entryId) {
        terminalBodyRefreshQueue.cancel()
        return
      }
      if (!canShowLiveDetail) {
        terminalBodyRefreshQueue.cancel()
        bodyViewRequestToken++
        selectedEntryBodyView.value = null
        selectedEntryBodyViewLoading.value = false
        loadedBodyViewEntryId = null
        resetLiveUpdateReconciliation()
        return
      }
      if (!isDetailPanelShown) {
        terminalBodyRefreshQueue.cancel()
        bodyViewRequestToken++
        selectedEntryBodyView.value = null
        selectedEntryBodyViewLoading.value = false
        loadedBodyViewEntryId = null
        resetLiveUpdateReconciliation()
        return
      }
      if (!isSurfaceActive) {
        terminalBodyRefreshQueue.cancel()
        return
      }
      terminalBodyRefreshQueue.activate(entryId)
      void ensureSelectedEntryBodyViewLoaded(previous?.[2] === false)
    },
    { immediate: true },
  )

  function cleanup(generation?: number) {
    if (generation !== undefined && generation !== trafficLifecycleGeneration) {
      return
    }
    trafficLifecycleGeneration++
    statisticsRequestGuard.invalidate()
    bodyViewRequestToken++
    terminalBodyRefreshQueue.cancel()
    selectedEntryBodyViewLoading.value = false
    initialTrafficBuffer = null
    trafficResyncRequested = false
    resetTrafficEntryBatch()
    if (statsDebounceTimer !== null) {
      clearTimeout(statsDebounceTimer)
      statsDebounceTimer = null
    }
    isLiveEntryEvictionPaused.value = false
    pausedLiveEntries.clear()
    setDesiredLiveDetailId(0)
    offTrafficEntry?.()
    offTrafficEntry = null
    offTrafficPatch?.()
    offTrafficPatch = null
    offTrafficLiveUpdate?.()
    offTrafficLiveUpdate = null
    offTrafficReset?.()
    offTrafficReset = null
    resetLiveUpdateReconciliation()
  }

  return {
    entries,
    selectedEntry,
    selectedEntryCount,
    selectedEntryBodyView,
    selectedEntryBodyViewLoading,
    setTrafficSurfaceActive,
    getBodyView,
    showDetailPanel,
    statistics,
    scrollTop,
    columns,
    sortConfig,
    highlightMap,
    pendingFocusEntryId,
    selectEntry,
    clearAll,
    resetState,
    addOrUpdateEntry,
    deleteEntries,
    deleteEntry,
    setHighlight,
    focusEntryById,
    clearPendingFocusEntryId,
    pauseLiveEntryEviction,
    resumeLiveEntryEviction,
    initialize,
    cleanup,
  }
})
