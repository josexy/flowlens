import { defineStore } from 'pinia'
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import {
  GetMemStats,
  GetMonitoringStatus,
  StartMonitoring,
  StopMonitoring,
} from '#bindings/github.com/josexy/flowlens/backend/services/mem_stats_service/memstatsservice'
import type * as memstatsservice from '#bindings/github.com/josexy/flowlens/backend/services/mem_stats_service/models'

export type MemSnapshot = memstatsservice.MemSnapshot
export type MonitoringStatus = memstatsservice.MonitoringStatus

const MAX_SNAPSHOTS = 60
const memStatsEventName = 'memstats:update'

export const useMemStatsStore = defineStore('memStats', () => {
  const snapshots = ref<MemSnapshot[]>([])
  const latestSnapshot = ref<MemSnapshot | null>(null)
  const isMonitoring = ref(false)
  const intervalMs = ref(2000)
  let offMemStatsUpdate: (() => void) | null = null

  function addSnapshot(snapshot: MemSnapshot) {
    latestSnapshot.value = snapshot
    snapshots.value.push(snapshot)
    if (snapshots.value.length > MAX_SNAPSHOTS) {
      snapshots.value.shift()
    }
  }

  async function startMonitoring(ms?: number) {
    const interval = ms ?? intervalMs.value
    intervalMs.value = interval
    await StartMonitoring(interval)
    isMonitoring.value = true
  }

  async function stopMonitoring() {
    await StopMonitoring()
    isMonitoring.value = false
  }

  async function fetchOnce() {
    const snapshot = await GetMemStats()
    addSnapshot(snapshot)
  }

  function clearHistory() {
    snapshots.value = []
    latestSnapshot.value = null
  }

  async function initialize() {
    try {
      const status = await GetMonitoringStatus()
      isMonitoring.value = status.monitoring
      if (status.intervalMs > 0) {
        intervalMs.value = status.intervalMs
      }
    } catch {
      // ignore �?bindings not ready yet during dev hot-reload
    }

    if (!offMemStatsUpdate) {
      offMemStatsUpdate = Events.On(memStatsEventName, (event) => {
        addSnapshot(event.data as MemSnapshot)
      })
    }
  }

  function cleanup() {
    offMemStatsUpdate?.()
    offMemStatsUpdate = null
  }

  return {
    snapshots,
    latestSnapshot,
    isMonitoring,
    intervalMs,
    startMonitoring,
    stopMonitoring,
    fetchOnce,
    clearHistory,
    initialize,
    cleanup,
  }
})
