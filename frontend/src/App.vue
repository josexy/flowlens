<script setup lang="ts">
import AppLoading from '@/components/common/AppLoading.vue'
import DefaultLayout from '@/layouts/DefaultLayout.vue'
import AppShortcutHost from '@/shortcuts/AppShortcutHost.vue'
import { clearProcessIconCache } from '@/components/common/processIconLoader'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Events, Window as WailsWindow } from '@wailsio/runtime'
import { useThemeStore } from './stores/theme'
import { useSettingStore } from './stores/setting'
import { useI18n } from 'vue-i18n'
import {
  PREFERENCES_CHANGED_EVENT,
  LOCAL_DATA_CLEARED_EVENT,
  SETTINGS_SAVED_EVENT,
  SHUTDOWN_REQUESTED_EVENT,
  SHUTDOWN_UI_READY_EVENT,
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
let offPreferencesChanged: (() => void) | null = null
let offSettingsSaved: (() => void) | null = null
let offShortcutsChanged: (() => void) | null = null
let offShutdownRequested: (() => void) | null = null
let offLocalDataCleared: (() => void) | null = null
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

watch(locale, emitTrayLabels, { immediate: true })

onMounted(async () => {
  await resolveCurrentWindowName()
  listenPreferenceChanges()
  listenSettingsSaved()
  listenShortcutChanges()
  listenShutdownRequests()
  listenLocalDataCleared()
  try {
    await settingStore.load()
    locale.value = settingStore.language
    themeStore.initializeTheme(settingStore.themeMode)
  } catch (e) {
    console.error('Load settings failed', e)
    themeStore.initializeTheme()
  } finally {
    await nextTick()
    showWindowWhenReady()
  }
})

onBeforeUnmount(() => {
  offPreferencesChanged?.()
  offPreferencesChanged = null
  offSettingsSaved?.()
  offSettingsSaved = null
  offShortcutsChanged?.()
  offShortcutsChanged = null
  offShutdownRequested?.()
  offShutdownRequested = null
  offLocalDataCleared?.()
  offLocalDataCleared = null
})
</script>

<template>
  <UApp :toaster="{ position: 'top-center' }">
    <AppShortcutHost>
      <DefaultLayout />
    </AppShortcutHost>

    <UModal
      :open="shutdownVisible"
      :close="false"
      :dismissible="false"
      :title="t('app.shutdown_title')"
      :description="t('app.shutdown_description')"
      :ui="{ content: 'max-w-sm' }"
    >
      <template #body>
        <AppLoading size="lg" :label="t('app.shutdown_title')" />
      </template>
    </UModal>
  </UApp>
</template>
