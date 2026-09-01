import { defineStore } from 'pinia'
import { ref } from 'vue'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  ListHistoryKeys,
  GetHistory,
  DeleteHistory,
  ClearHistories,
} from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import { useHistoryTrafficStore } from './historyTraffic'
import { useHistoryFilterStore } from './historyFilter'

export const useHistoryStore = defineStore('history', () => {
  const metadataList = ref<proxyservice.HistoryMetadata[]>([])
  const selectedKey = ref<string | null>(null)
  const loading = ref(false)
  const loadingHistory = ref(false)
  const loadingHistoryKey = ref<string | null>(null)
  let historyListRequestToken = 0
  let historyLoadRequestToken = 0

  const historyTrafficStore = useHistoryTrafficStore()
  const historyFilterStore = useHistoryFilterStore()

  function clearSelectedHistoryState() {
    historyLoadRequestToken++
    selectedKey.value = null
    loadingHistory.value = false
    loadingHistoryKey.value = null
    historyTrafficStore.reset()
    historyFilterStore.clearFilters()
  }

  function resetState() {
    historyListRequestToken++
    metadataList.value = []
    loading.value = false
    clearSelectedHistoryState()
  }

  async function loadList() {
    const requestToken = ++historyListRequestToken
    try {
      loading.value = true
      const list = await ListHistoryKeys()
      if (requestToken !== historyListRequestToken) {
        return
      }
      // Sort by createdAt descending (newest first)
      const nextMetadata = (list ?? [])
        .filter((item): item is proxyservice.HistoryMetadata => item !== null)
        .sort((a, b) => b.createdAt - a.createdAt)
      metadataList.value = nextMetadata
      if (selectedKey.value && !nextMetadata.some((item) => item.key === selectedKey.value)) {
        clearSelectedHistoryState()
      }
    } catch (error) {
      if (requestToken !== historyListRequestToken) {
        return
      }
      console.error('Failed to list history keys:', error)
    } finally {
      if (requestToken === historyListRequestToken) {
        loading.value = false
      }
    }
  }

  async function selectHistory(key: string) {
    if (
      selectedKey.value === key &&
      (historyTrafficStore.currentKey === key || loadingHistoryKey.value === key)
    ) {
      return
    }
    const previousKey = historyTrafficStore.currentKey ?? selectedKey.value
    const requestToken = ++historyLoadRequestToken
    selectedKey.value = key
    loadingHistory.value = true
    loadingHistoryKey.value = key
    try {
      const historyEntries = await GetHistory(key)
      if (requestToken !== historyLoadRequestToken || selectedKey.value !== key) {
        return
      }
      historyFilterStore.clearSelectedHosts()
      historyFilterStore.clearSelectedProcesses()
      historyTrafficStore.loadFromHistory(
        key,
        (historyEntries ?? []).filter((entry): entry is proxyservice.TrafficEntry => entry !== null),
      )
    } catch (error) {
      if (requestToken !== historyLoadRequestToken) {
        return
      }
      console.error('Failed to get history:', error)
      selectedKey.value = previousKey
      if (!previousKey) {
        historyTrafficStore.reset()
      }
    } finally {
      if (requestToken === historyLoadRequestToken) {
        loadingHistory.value = false
        loadingHistoryKey.value = null
      }
    }
  }

  async function deleteHistory(key: string) {
    try {
      await DeleteHistory(key)
      // Prevent an older ListHistoryKeys response from restoring the deleted
      // entry after the backend mutation has completed.
      historyListRequestToken++
      loading.value = false
      metadataList.value = metadataList.value.filter((m) => m.key !== key)
      if (selectedKey.value === key) {
        clearSelectedHistoryState()
      }
    } catch (error) {
      console.error('Failed to delete history:', error)
      throw error
    }
  }

  async function clearAll() {
    try {
      await ClearHistories()
      resetState()
    } catch (error) {
      console.error('Failed to clear histories:', error)
      throw error
    }
  }

  return {
    metadataList,
    selectedKey,
    loading,
    loadingHistory,
    loadingHistoryKey,
    loadList,
    selectHistory,
    deleteHistory,
    clearAll,
    resetState,
  }
})
