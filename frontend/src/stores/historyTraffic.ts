import { defineStore } from 'pinia'
import { ref, shallowRef, computed, watch } from 'vue'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { GetHistoryTrafficBodyView } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import type { SortConfig } from './traffic'
import {
  getTrafficCapabilities,
  isHTTPTraffic,
  isRawTCPTraffic,
  isWebSocketTraffic,
} from '@/utils/traffic'
import { createTrafficTableColumns } from '@/utils/traffic-table-columns'

export const useHistoryTrafficStore = defineStore('historyTraffic', () => {
  const entries = shallowRef<proxyservice.TrafficEntry[]>([])
  const selectedEntry = ref<proxyservice.TrafficEntry | null>(null)
  const selectedEntryCount = ref(0)
  const selectedEntryBodyView = ref<proxyservice.TrafficBodyView | null>(null)
  const selectedEntryBodyViewLoading = ref(false)
  const showDetailPanel = ref(false)
  const scrollTop = ref(0)
  const pendingFocusEntryId = ref<number | null>(null)
  let loadedBodyViewEntryId: number | null = null
  let bodyViewRequestToken = 0

  // Key of the currently loaded history - used by selectEntry to call GetHistoryTrafficBodyView
  const currentKey = ref<string | null>(null)

  const columns = ref(createTrafficTableColumns())

  const sortConfig = ref<SortConfig>({ key: null, order: null })
  const highlightMap = ref<Map<number, string>>(new Map())

  const statistics = computed<proxyservice.TrafficStatistics>(() => {
    let totalHttp = 0
    let totalWs = 0
    let totalTcp = 0
    for (const e of entries.value) {
      if (isWebSocketTraffic(e)) totalWs++
      else if (isHTTPTraffic(e)) totalHttp++
      else if (isRawTCPTraffic(e)) totalTcp++
    }
    return { total: entries.value.length, totalHttp, totalWs, totalTcp }
  })

  async function selectEntry(entry: proxyservice.TrafficEntry | null) {
    const previousEntryId = selectedEntry.value?.id ?? null
    selectedEntry.value = entry
    if (!entry) {
      bodyViewRequestToken++
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
      showDetailPanel.value = false
      return
    }
    if (previousEntryId !== entry.id) {
      bodyViewRequestToken++
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
    }
  }

  async function ensureSelectedEntryBodyViewLoaded() {
    const entry = selectedEntry.value
    if (
      !entry ||
      !showDetailPanel.value ||
      !currentKey.value ||
      !getTrafficCapabilities(entry).canLoadBody
    ) {
      return
    }
    if (selectedEntryBodyView.value && loadedBodyViewEntryId === entry.id) {
      return
    }

    const requestToken = ++bodyViewRequestToken
    selectedEntryBodyViewLoading.value = true
    try {
      const bodyView = await GetHistoryTrafficBodyView(currentKey.value, entry.id)
      if (
        requestToken !== bodyViewRequestToken ||
        !showDetailPanel.value ||
        selectedEntry.value?.id !== entry.id
      ) {
        return
      }
      selectedEntryBodyView.value = bodyView
      loadedBodyViewEntryId = entry.id
    } catch (error) {
      if (requestToken !== bodyViewRequestToken || selectedEntry.value?.id !== entry.id) {
        return
      }
      console.error('Failed to get history traffic body view:', error)
      selectedEntryBodyView.value = null
      loadedBodyViewEntryId = null
    } finally {
      if (requestToken === bodyViewRequestToken) {
        selectedEntryBodyViewLoading.value = false
      }
    }
  }

  async function getBodyView(
    entryId: number,
    historyKey?: string | null,
  ): Promise<proxyservice.TrafficBodyView | null> {
    const key = historyKey === undefined ? currentKey.value : historyKey
    if (!key) return null
    const entry = entries.value.find((candidate) => candidate.id === entryId)
    if (isRawTCPTraffic(entry)) return null
    try {
      return await GetHistoryTrafficBodyView(key, entryId)
    } catch (error) {
      console.error('Failed to get history traffic body view:', error)
      return null
    }
  }

  /** Load entries from a HistoryEntries object. Resets all state. */
  function loadFromHistory(key: string, historyEntries: proxyservice.TrafficEntry[]) {
    bodyViewRequestToken++
    currentKey.value = key
    highlightMap.value.clear()
    selectedEntry.value = null
    selectedEntryCount.value = 0
    selectedEntryBodyView.value = null
    selectedEntryBodyViewLoading.value = false
    loadedBodyViewEntryId = null
    showDetailPanel.value = false
    scrollTop.value = 0
    pendingFocusEntryId.value = null
    sortConfig.value = { key: null, order: null }
    entries.value = historyEntries ?? []
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

  async function deleteEntries(ids: number[]): Promise<void> {
    const idSet = new Set(ids.filter((id) => typeof id === 'number' && Number.isFinite(id)))
    if (idSet.size === 0) {
      return
    }

    entries.value = entries.value.filter((entry) => !idSet.has(entry.id))
    for (const id of idSet) {
      highlightMap.value.delete(id)
    }
    if (selectedEntry.value && idSet.has(selectedEntry.value.id)) {
      bodyViewRequestToken++
      selectedEntry.value = null
      selectedEntryBodyView.value = null
      selectedEntryBodyViewLoading.value = false
      loadedBodyViewEntryId = null
      showDetailPanel.value = false
    }
  }

  function deleteEntry(id: number) {
    return deleteEntries([id])
  }

  /** Reset to empty state. */
  function reset() {
    bodyViewRequestToken++
    currentKey.value = null
    entries.value = []
    selectedEntry.value = null
    selectedEntryCount.value = 0
    selectedEntryBodyView.value = null
    selectedEntryBodyViewLoading.value = false
    loadedBodyViewEntryId = null
    showDetailPanel.value = false
    scrollTop.value = 0
    highlightMap.value.clear()
    pendingFocusEntryId.value = null
    sortConfig.value = { key: null, order: null }
  }

  watch(
    [selectedEntry, showDetailPanel, currentKey],
    ([entry, isDetailPanelShown, key]) => {
      if (!entry || !key) {
        return
      }
      if (!getTrafficCapabilities(entry).canLoadBody) {
        bodyViewRequestToken++
        selectedEntryBodyView.value = null
        selectedEntryBodyViewLoading.value = false
        loadedBodyViewEntryId = null
        return
      }
      if (!isDetailPanelShown) {
        bodyViewRequestToken++
        selectedEntryBodyView.value = null
        selectedEntryBodyViewLoading.value = false
        loadedBodyViewEntryId = null
        return
      }
      void ensureSelectedEntryBodyViewLoaded()
    },
    { immediate: true },
  )

  return {
    currentKey,
    entries,
    selectedEntry,
    selectedEntryCount,
    selectedEntryBodyView,
    selectedEntryBodyViewLoading,
    getBodyView,
    showDetailPanel,
    columns,
    sortConfig,
    highlightMap,
    scrollTop,
    pendingFocusEntryId,
    statistics,
    selectEntry,
    setHighlight,
    focusEntryById,
    clearPendingFocusEntryId,
    deleteEntries,
    deleteEntry,
    loadFromHistory,
    reset,
  }
})
