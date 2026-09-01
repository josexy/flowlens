import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { useHistoryTrafficStore } from './historyTraffic'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  isHTTPTraffic,
  isRawTCPTraffic,
  isWebSocketTraffic,
  trafficMatchesCategoryFilters,
  trafficMatchesSearch,
} from '@/utils/traffic'
import { firstHeaderFieldValue } from '@/utils/headers'

export const useHistoryFilterStore = defineStore('historyFilter', () => {
  const searchText = ref<string>('')
  const activeFilterTab = ref<string>('all')
  const selectedMethods = ref<string[]>([
    'GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS',
  ])
  const selectedStatuses = ref<string[]>(['2xx', '3xx', '4xx', '5xx'])
  const selectedTypes = ref<string[]>(['http', 'https', 'websocket', 'tcp'])
  const selectedHosts = ref<string[]>([])
  const selectedProcessKeys = ref<string[]>([])

  const historyTrafficStore = useHistoryTrafficStore()

  function setActiveTab(tab: string) {
    activeFilterTab.value = tab
  }

  function toggleHost(host: string) {
    if (selectedHosts.value.includes(host)) {
      selectedHosts.value = selectedHosts.value.filter((item) => item !== host)
      return
    }
    selectedHosts.value = [...selectedHosts.value, host]
  }

  function setSelectedHosts(hosts: string[]) {
    selectedHosts.value = [...hosts]
  }

  function clearSelectedHosts() {
    selectedHosts.value = []
  }

  function toggleProcess(processKey: string) {
    if (selectedProcessKeys.value.includes(processKey)) {
      selectedProcessKeys.value = selectedProcessKeys.value.filter((item) => item !== processKey)
      return
    }
    selectedProcessKeys.value = [...selectedProcessKeys.value, processKey]
  }

  function clearSelectedProcesses() {
    selectedProcessKeys.value = []
  }

  function matchesContentType(entry: proxyservice.TrafficEntry, contentType: string): boolean {
    const ct = firstHeaderFieldValue(entry.response?.headerFields, 'Content-Type')?.toLowerCase() || ''
    switch (contentType) {
      case 'json':   return ct.includes('json')
      case 'xml':    return ct.includes('xml')
      case 'text':   return ct.includes('text/plain')
      case 'html':   return ct.includes('html')
      case 'js':     return ct.includes('javascript') || ct.includes('ecmascript')
      case 'image':  return ct.includes('image/')
      case 'media':  return ct.includes('video/') || ct.includes('audio/')
      case 'binary': return ct.includes('octet-stream') || ct.includes('application/pdf')
      default:       return false
    }
  }

  function matchesProtocol(entry: proxyservice.TrafficEntry, protocol: string): boolean {
    switch (protocol) {
      case 'http':      return isHTTPTraffic(entry) && entry.type === 'http'
      case 'https':     return isHTTPTraffic(entry) && entry.type === 'https'
      case 'websocket': return isWebSocketTraffic(entry)
      case 'tcp':       return isRawTCPTraffic(entry)
      case 'http1':     return entry.response?.proto.startsWith('HTTP/1.') || false
      case 'http2':     return entry.response?.proto.startsWith('HTTP/2.') || false
      default:          return false
    }
  }

  const baseFilteredEntries = computed(() => {
    const entries = historyTrafficStore.entries
    const query = searchText.value
    const tab = activeFilterTab.value

    return entries.filter((entry) => {
      if (!trafficMatchesSearch(entry, query)) {
        return false
      }

      if (tab !== 'all') {
        if (['http', 'https', 'websocket', 'tcp', 'http1', 'http2'].includes(tab)) {
          if (!matchesProtocol(entry, tab)) return false
        } else if (['json', 'xml', 'text', 'html', 'js', 'image', 'media', 'binary'].includes(tab)) {
          if (!matchesContentType(entry, tab)) return false
        } else if (['1xx', '2xx', '3xx', '4xx', '5xx'].includes(tab)) {
          if (entry.statusCode > 0) {
            const statusCategory = `${Math.floor(entry.statusCode / 100)}xx`
            if (statusCategory !== tab) return false
          } else {
            return false
          }
        }
      }

      return true
    })
  })

  const selectedHostSet = computed(() => new Set(selectedHosts.value))
  const selectedProcessKeySet = computed(() => new Set(selectedProcessKeys.value))

  const filteredEntries = computed(() => {
    const hostSet = selectedHostSet.value
    const processKeySet = selectedProcessKeySet.value
    if (hostSet.size === 0 && processKeySet.size === 0) {
      return baseFilteredEntries.value
    }
    return baseFilteredEntries.value.filter((entry) =>
      trafficMatchesCategoryFilters(entry, hostSet, processKeySet),
    )
  })

  function clearFilters() {
    searchText.value = ''
    activeFilterTab.value = 'all'
    selectedMethods.value = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']
    selectedStatuses.value = ['2xx', '3xx', '4xx', '5xx']
    selectedTypes.value = ['http', 'https', 'websocket', 'tcp']
    selectedHosts.value = []
    selectedProcessKeys.value = []
  }

  return {
    searchText,
    activeFilterTab,
    selectedMethods,
    selectedStatuses,
    selectedTypes,
    selectedHosts,
    selectedProcessKeys,
    baseFilteredEntries,
    filteredEntries,
    setActiveTab,
    toggleHost,
    setSelectedHosts,
    clearSelectedHosts,
    toggleProcess,
    clearSelectedProcesses,
    clearFilters,
  }
})
