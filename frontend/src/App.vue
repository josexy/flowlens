<script setup lang="ts">
import AppLoading from '@/components/common/AppLoading.vue'
import DefaultLayout from '@/layouts/DefaultLayout.vue'
import AppShortcutHost from '@/shortcuts/AppShortcutHost.vue'
import { useNotify } from '@/composables/useNotify'
import { clearProcessIconCache } from '@/components/common/processIconLoader'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Application, Events, Window as WailsWindow } from '@wailsio/runtime'
import { useThemeStore } from './stores/theme'
import { useSettingStore } from './stores/setting'
import { useI18n } from 'vue-i18n'
import {
  PREFERENCES_CHANGED_EVENT,
  LOCAL_DATA_CLEARED_EVENT,
  SETTINGS_SAVED_EVENT,
  SHUTDOWN_REQUESTED_EVENT,
  SHUTDOWN_UI_READY_EVENT,
  RESET_REQUESTED_EVENT,
  RESET_READY_EVENT,
  RESET_FAILED_EVENT,
  RESET_COMPLETED_EVENT,
  SHORTCUTS_CHANGED_EVENT,
  parsePreferencesChangedPayload,
  parseShortcutsChangedPayload,
} from './runtime/appEvents'
import { useHistoryStore } from './stores/history'
import { useTrafficWorkspaceStore } from './stores/trafficWorkspace'
import {
  parseLocalDataClearedPayload,
  syncLocalDataClearedWindow,
} from './utils/localDataCleared'
import type * as settingservice from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

const themeStore = useThemeStore()
const settingStore = useSettingStore()
const historyStore = useHistoryStore()
const trafficWorkspaceStore = useTrafficWorkspaceStore()
const { locale, t } = useI18n()
const notify = useNotify()
let offPreferencesChanged: (() => void) | null = null
let offSettingsSaved: (() => void) | null = null
let offShortcutsChanged: (() => void) | null = null
let offShutdownRequested: (() => void) | null = null
let offLocalDataCleared: (() => void) | null = null
let offResetRequested: (() => void) | null = null
let offResetReady: (() => void) | null = null
let offResetFailed: (() => void) | null = null
let offResetCompleted: (() => void) | null = null
let currentWindowName = ''
const shutdownVisible = ref(false)

function emitTrayLabels() {
  try {
    void Events.Emit('app:tray-labels', {
      openMainWindow: t('tray.open_main_window'),
      close: t('tray.close'),
    }).catch(() => {})
  } catch {
    // Ignore event delivery errors while the application is starting.
  }
}

function showWindowWhenReady() {
  if (currentWindowName === 'updater') {
    return
  }
  try {
    void Events.Emit('app:frontend-ready').catch(() => {})
  } catch {
    // The backend retains its startup timeout if delivery fails.
  }
}

async function applyPreferencesChanged(data: unknown) {
  const preferences = parsePreferencesChangedPayload(data)
  if (!preferences) {
    return
  }
  if (preferences.language && locale.value !== preferences.language) {
    locale.value = preferences.language
  }
  if (preferences.themeMode && themeStore.themeMode !== preferences.themeMode) {
    themeStore.setThemeMode(preferences.themeMode)
  }
  await settingStore.syncExternalPreferences(preferences)
}

function applySettingsSaved(data: unknown) {
  settingStore.syncExternalSettings(data as settingservice.Settings | null | undefined)
  if (locale.value !== settingStore.language) {
    locale.value = settingStore.language
  }
  if (themeStore.themeMode !== settingStore.themeMode) {
    themeStore.setThemeMode(settingStore.themeMode)
  }
}

async function resolveCurrentWindowName() {
  if (currentWindowName) {
    return
  }
  try {
    currentWindowName = await WailsWindow.Name()
  } catch {
    currentWindowName = ''
  }
}

function listenPreferenceChanges() {
  if (offPreferencesChanged) {
    return
  }
  offPreferencesChanged = Events.On(PREFERENCES_CHANGED_EVENT, (event) => {
    void applyPreferencesChanged(event.data)
  })
}

function listenSettingsSaved() {
  if (offSettingsSaved) {
    return
  }
  offSettingsSaved = Events.On(SETTINGS_SAVED_EVENT, (event) => {
    if (event.sender && event.sender === currentWindowName) {
      return
    }
    applySettingsSaved(event.data)
  })
}

function listenShortcutChanges() {
  if (offShortcutsChanged) {
    return
  }
  offShortcutsChanged = Events.On(SHORTCUTS_CHANGED_EVENT, (event) => {
    const payload = parseShortcutsChangedPayload(event.data)
    if (!payload?.config) {
      return
    }
    settingStore.syncExternalShortcutState(payload.config, payload.runtimeState)
  })
}

async function acknowledgeShutdownLoading() {
  shutdownVisible.value = true
  await nextTick()
  await new Promise<void>((resolve) => {
    requestAnimationFrame(() => resolve())
  })
  try {
    await Events.Emit(SHUTDOWN_UI_READY_EVENT)
  } catch {
    // The backend timeout will continue shutdown if the acknowledgement fails.
  }
}

function listenShutdownRequests() {
  if (offShutdownRequested) {
    return
  }
  offShutdownRequested = Events.On(SHUTDOWN_REQUESTED_EVENT, () => {
    if (shutdownVisible.value) {
      return
    }
    void acknowledgeShutdownLoading()
  })
}

function listenLocalDataCleared() {
  if (offLocalDataCleared) {
    return
  }
  offLocalDataCleared = Events.On(LOCAL_DATA_CLEARED_EVENT, (event) => {
    const payload = parseLocalDataClearedPayload(event.data)
    if (!payload) {
      return
    }
    syncLocalDataClearedWindow(payload, {
      clearProcessIconCache,
      resetHistory: historyStore.resetState,
      reloadHistory: () => void historyStore.loadList(),
      clearRequestDraftCacheFileReferences: trafficWorkspaceStore.clearRequestDraftCacheFileReferences,
    })
  })
}

function listenApplicationReset() {
  offResetRequested = Events.On(RESET_REQUESTED_EVENT, () => {
    settingStore.isResetting = true
    settingStore.isResetAwaitingRestart = false
  })
  offResetReady = Events.On(RESET_READY_EVENT, () => {
    settingStore.isResetting = true
    settingStore.isResetAwaitingRestart = true
  })
  offResetFailed = Events.On(RESET_FAILED_EVENT, () => {
    settingStore.isResetting = false
    settingStore.isResetAwaitingRestart = false
  })
  offResetCompleted = Events.On(RESET_COMPLETED_EVENT, () => {
    notify.success(t('settings.reset_application_success'), undefined, 5000)
  })
}

async function exitForReset() {
  try {
    await Application.Quit()
  } catch (error) {
    notify.error(t('settings.reset_application_exit_failed', { error: String(error) }))
  }
}

watch(locale, emitTrayLabels, { immediate: true })

onMounted(async () => {
  await resolveCurrentWindowName()
  listenPreferenceChanges()
  listenSettingsSaved()
  listenShortcutChanges()
  listenShutdownRequests()
  listenLocalDataCleared()
  listenApplicationReset()
  try {
    await settingStore.load()
    locale.value = settingStore.language
    themeStore.initializeTheme(settingStore.themeMode)
    await themeStore.loadThemeColors()
  } catch (e) {
    console.error('Load settings failed', e)
    themeStore.initializeTheme()
  } finally {
    await nextTick()
    await themeStore.syncNativeBackground()
    showWindowWhenReady()
  }
})

onBeforeUnmount(() => {
  offPreferencesChanged?.()
  offPreferencesChanged = null
  offSettingsSaved?.()
  offSettingsSaved = null
  themeStore.cleanup()
  offShortcutsChanged?.()
  offShortcutsChanged = null
  offShutdownRequested?.()
  offShutdownRequested = null
  offLocalDataCleared?.()
  offLocalDataCleared = null
  offResetRequested?.()
  offResetRequested = null
  offResetReady?.()
  offResetReady = null
  offResetFailed?.()
  offResetFailed = null
  offResetCompleted?.()
  offResetCompleted = null
})
</script>

<template>
  <UApp :toaster="{ position: 'top-center', progress: false }">
    <AppShortcutHost :disabled="settingStore.isResetting || shutdownVisible">
      <DefaultLayout />
    </AppShortcutHost>

    <UModal
      :open="shutdownVisible || settingStore.isResetting"
      :close="false"
      :dismissible="false"
      :title="t(shutdownVisible ? 'app.shutdown_title' : settingStore.isResetAwaitingRestart ? 'settings.reset_application_ready' : 'settings.reset_application_progress')"
      :description="t(shutdownVisible ? 'app.shutdown_description' : settingStore.isResetAwaitingRestart ? 'settings.reset_application_ready_description' : 'settings.reset_application_progress_description')"
      :ui="{ content: 'max-w-sm' }"
    >
      <template v-if="!settingStore.isResetAwaitingRestart || shutdownVisible" #body>
        <AppLoading size="lg" :label="t(shutdownVisible ? 'app.shutdown_title' : 'settings.reset_application_progress')" />
      </template>
      <template v-if="settingStore.isResetAwaitingRestart && !shutdownVisible" #footer>
        <div class="flex w-full justify-end">
          <UButton
            icon="i-lucide-log-out"
            :label="t('settings.reset_application_exit')"
            @click="exitForReset"
          />
        </div>
      </template>
    </UModal>
  </UApp>
</template>
