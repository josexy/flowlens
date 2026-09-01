import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  ClearLogs,
  GetLogStatus,
  OpenLogDir,
  SetLogEnabled,
  SetLogLevel,
} from '#bindings/github.com/josexy/flowlens/backend/services/logging_service/loggingservice'
import type { LogStatus } from '#bindings/github.com/josexy/flowlens/backend/pkg/logger/models'
import { useSettingStore } from '@/stores/setting'

export const useLoggingStore = defineStore('logging', () => {
  const status = ref<LogStatus | null>(null)
  const isLoading = ref(false)
  const isUpdatingEnabled = ref(false)
  const isUpdatingLevel = ref(false)
  const isOpeningDir = ref(false)
  const isClearingLogs = ref(false)

  async function syncSettingPreference(nextStatus: LogStatus | null) {
    if (!nextStatus) return
    const settingStore = useSettingStore()
    await settingStore.syncLogPreference(nextStatus.enabled, nextStatus.level)
  }

  async function refresh() {
    isLoading.value = true
    try {
      status.value = await GetLogStatus()
      await syncSettingPreference(status.value)
      return status.value
    } finally {
      isLoading.value = false
    }
  }

  async function setEnabled(enabled: boolean) {
    isUpdatingEnabled.value = true
    try {
      status.value = await SetLogEnabled(enabled)
      await syncSettingPreference(status.value)
      return status.value
    } finally {
      isUpdatingEnabled.value = false
    }
  }

  async function setLevel(level: LogStatus['level']) {
    isUpdatingLevel.value = true
    try {
      status.value = await SetLogLevel(level)
      await syncSettingPreference(status.value)
      return status.value
    } finally {
      isUpdatingLevel.value = false
    }
  }

  async function clearLogs() {
    isClearingLogs.value = true
    try {
      await ClearLogs()
      await refresh()
    } finally {
      isClearingLogs.value = false
    }
  }

  async function openDir() {
    isOpeningDir.value = true
    try {
      await OpenLogDir()
    } finally {
      isOpeningDir.value = false
    }
  }

  return {
    status,
    isLoading,
    isUpdatingEnabled,
    isUpdatingLevel,
    isOpeningDir,
    isClearingLogs,
    refresh,
    setEnabled,
    setLevel,
    clearLogs,
    openDir,
  }
})
