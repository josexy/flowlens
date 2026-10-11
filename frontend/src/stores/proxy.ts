import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  Start,
  Stop,
  GetStatus,
  GetSystemProxyStatus,
  SetSystemProxyEnabled,
  GetDNSCacheStatus,
  ClearDNSCache,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { Events } from '@wailsio/runtime'

export const useProxyStore = defineStore('proxy', () => {
  const status = ref<proxyservice.ProxyStatus | null>(null)
  const systemProxyStatus = ref<proxyservice.SystemProxyStatus | null>(null)
  const lastError = ref<string | null>(null)
  const dnsCacheStatus = ref<proxyservice.DNSCacheStatus | null>(null)
  const isRunning = computed(() => status.value?.running ?? false)
  let offProxyStatus: (() => void) | null = null
  let offProxyError: (() => void) | null = null
  let offSystemProxyStatus: (() => void) | null = null
  let offDNSCacheStatus: (() => void) | null = null

  async function start() {
    try {
      lastError.value = null
      const newStatus = await Start()
      status.value = newStatus
    } catch (error) {
      lastError.value = String(error)
      throw error
    }
  }

  async function stop() {
    try {
      lastError.value = null
      const newStatus = await Stop()
      status.value = newStatus
    } catch (error) {
      lastError.value = String(error)
      throw error
    }
  }

  async function initialize() {
    const [currentStatus, currentSystemProxyStatus, currentDNSCacheStatus] = await Promise.all([
      GetStatus(),
      GetSystemProxyStatus(),
      GetDNSCacheStatus(),
    ])
    status.value = currentStatus
    systemProxyStatus.value = currentSystemProxyStatus
    dnsCacheStatus.value = currentDNSCacheStatus

    if (!offProxyStatus) {
      offProxyStatus = Events.On('proxy:status', (event) => {
        status.value = event.data as proxyservice.ProxyStatus
      })
    }

    if (!offProxyError) {
      offProxyError = Events.On('proxy:error', (event) => {
        lastError.value = String(event.data ?? '')
      })
    }

    if (!offSystemProxyStatus) {
      offSystemProxyStatus = Events.On('proxy:system-proxy-status', (event) => {
        systemProxyStatus.value = event.data as proxyservice.SystemProxyStatus
      })
    }
    if (!offDNSCacheStatus) {
      offDNSCacheStatus = Events.On('proxy:dns-cache-status', (event) => {
        dnsCacheStatus.value = event.data as proxyservice.DNSCacheStatus
      })
    }
  }

  async function refreshDNSCacheStatus() {
    dnsCacheStatus.value = await GetDNSCacheStatus()
    return dnsCacheStatus.value
  }

  async function clearDNSCache() {
    dnsCacheStatus.value = await ClearDNSCache()
    return dnsCacheStatus.value
  }

  async function setSystemProxyEnabled(enabled: boolean) {
    const newStatus = await SetSystemProxyEnabled(enabled)
    systemProxyStatus.value = newStatus
    return newStatus
  }

  function cleanup() {
    offProxyStatus?.()
    offProxyStatus = null
    offProxyError?.()
    offProxyError = null
    offSystemProxyStatus?.()
    offSystemProxyStatus = null
    offDNSCacheStatus?.()
    offDNSCacheStatus = null
  }

  return {
    status,
    systemProxyStatus,
    dnsCacheStatus,
    refreshDNSCacheStatus,
    clearDNSCache,
    isRunning,
    lastError,
    start,
    stop,
    setSystemProxyEnabled,
    initialize,
    cleanup,
  }
})
