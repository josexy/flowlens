<script setup lang="ts">
import {
  ref,
  computed,
  defineAsyncComponent,
  onMounted,
  onBeforeUnmount,
  watch,
  nextTick,
} from 'vue'
import {
  GetLocalDataSize,
  ClearCacheFiles,
  ClearCacheAndHistory,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  ListSystemFonts,
  GetCACertificateInfo,
  GenerateCurrentCACertificate,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/settingservice'
import {
  MainWindowCloseBehavior,
  WindowFrameMode,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import type * as settingservice from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import { useSettingStore, type SettingsDirtySection } from '@/stores/setting'
import { useLoggingStore } from '@/stores/logging'
import { useNotify } from '@/composables/useNotify'
import GeneralSettings from '@/components/settings/GeneralSettings.vue'
import AppLoading from '@/components/common/AppLoading.vue'
import { useI18n } from 'vue-i18n'
import { Events } from '@wailsio/runtime'
import {
  CONFIRM_QUIT_REQUEST_EVENT,
  QUIT_CONFIRMED_EVENT,
  SETTINGS_SAVED_EVENT,
  SETTINGS_WINDOW_DIRTY_CHANGED_EVENT,
} from '@/runtime/appEvents'
import { registerShortcutHandler } from '@/shortcuts'
import { formatFileSize } from '@/utils/format'
import { createLatestOperationGuard } from '@/utils/latestOperation'

const DEFAULT_FONT_OPTION_VALUE = '__default_font__'

const ConfirmCardModal = defineAsyncComponent(() => import('@/components/modal/ConfirmCardModal.vue'))
const LogSettings = defineAsyncComponent({
  loader: () => import('@/components/settings/LogSettings.vue'),
  loadingComponent: AppLoading,
  delay: 0,
})
const ProxySettings = defineAsyncComponent({
  loader: () => import('@/components/settings/ProxySettings.vue'),
  loadingComponent: AppLoading,
  delay: 0,
})
const StorageSettings = defineAsyncComponent({
  loader: () => import('@/components/settings/StorageSettings.vue'),
  loadingComponent: AppLoading,
  delay: 0,
})
const ShortcutSettings = defineAsyncComponent({
  loader: () => import('@/components/settings/ShortcutSettings.vue'),
  loadingComponent: AppLoading,
  delay: 0,
})
const PythonPluginSettings = defineAsyncComponent({
  loader: () => import('@/components/settings/PythonPluginSettings.vue'),
  loadingComponent: AppLoading,
  delay: 0,
})

type FontSelectOption = {
  label: string
  value: string
}

type SettingsTabKey = 'general' | 'proxy' | 'pythonPlugins' | 'shortcuts' | 'logs' | 'storage'

const { t } = useI18n()
const settingStore = useSettingStore()
const loggingStore = useLoggingStore()
const notify = useNotify()

const activeTab = ref<SettingsTabKey>('general')
const shortcutPanelMounted = ref(false)

const tabs = computed<{ key: SettingsTabKey; label: string; icon: string }[]>(() => [
  { key: 'general', label: t('settings.tab_general'), icon: 'i-lucide-settings' },
  { key: 'proxy', label: t('settings.tab_proxy'), icon: 'i-lucide-server' },
  { key: 'pythonPlugins', label: t('settings.tab_python_plugins'), icon: 'i-lucide-file-code-2' },
  { key: 'shortcuts', label: t('settings.tab_shortcuts'), icon: 'i-lucide-keyboard' },
  { key: 'logs', label: t('settings.tab_logs'), icon: 'i-lucide-file-text' },
  { key: 'storage', label: t('settings.tab_storage'), icon: 'i-lucide-archive' },
])
const activeSettingsNavItemClass = [
  'bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)]',
  'font-semibold',
  'text-app-accent',
  'before:absolute',
  'before:top-1.5',
  'before:bottom-1.5',
  'before:left-0',
  'before:w-0.75',
  'before:rounded-full',
  'before:bg-app-accent',
  "before:content-['']",
  'hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)]',
  'hover:text-app-accent',
  '[&_.nav-icon]:text-app-accent',
].join(' ')
const activeTabTitle = computed(
  () => tabs.value.find((tab) => tab.key === activeTab.value)?.label ?? t('menu.settings'),
)

const isLoaded = ref(false)
const dataSize = ref<proxyservice.LocalDataSize>({ cacheBytes: 0, historyBytes: 0 })
const systemFonts = ref<settingservice.FontOption[]>([])
const caInfo = ref<settingservice.CACertificateInfo | null>(null)
const isLoadingFonts = ref(false)
const isLoadingCAInfo = ref(false)
const isGeneratingCA = ref(false)
const clearCacheAndHistorySuccess = ref(false)
const clearCacheSuccess = ref(false)
const localDataClearPending = ref(false)
const certGeneratedSuccess = ref(false)
const showSaveStatus = ref(false)
const quitConfirmVisible = ref(false)

let saveStatusTimer: ReturnType<typeof setTimeout> | null = null
let hasLoadedSystemFonts = false
let offConfirmQuitRequest: (() => void) | null = null
let offSaveShortcut: (() => void) | null = null
let isCommittingSettings = false
const dataSizeRequestGuard = createLatestOperationGuard()
const dirtySections = new Set<SettingsDirtySection>()

function addFontOption(
  options: FontSelectOption[],
  seen: Set<string>,
  option: FontSelectOption | string | null | undefined,
) {
  const value = (typeof option === 'string' ? option : option?.value)?.trim()
  if (!value || seen.has(value)) {
    return
  }
  seen.add(value)
  const label = typeof option === 'string' ? value : option?.label || value
  options.push({
    label,
    value,
  })
}

const fontOptions = computed(() => {
  const options: FontSelectOption[] = [
    { label: t('settings.font_default'), value: DEFAULT_FONT_OPTION_VALUE },
  ]
  const seen = new Set(options.map((option) => option.value))
  const commonConfig = settingStore.settings?.commonConfig

  addFontOption(options, seen, commonConfig?.appFontFamily)
  addFontOption(options, seen, commonConfig?.codeFontFamily)
  systemFonts.value.forEach((font) => addFontOption(options, seen, font))

  return options
})

const windowFrameModeOptions = computed(() => [
  { label: t('settings.window_frame_mode_custom'), value: WindowFrameMode.WindowFrameModeCustom },
  { label: t('settings.window_frame_mode_system'), value: WindowFrameMode.WindowFrameModeSystem },
])

const mainWindowCloseBehaviorOptions = computed(() => [
  {
    label: t('settings.main_window_close_behavior_hide_to_tray'),
    value: MainWindowCloseBehavior.MainWindowCloseBehaviorHideToTray,
  },
  {
    label: t('settings.main_window_close_behavior_quit'),
    value: MainWindowCloseBehavior.MainWindowCloseBehaviorQuit,
  },
])

const caHasExistingFiles = computed(() =>
  Boolean(caInfo.value?.certExists || caInfo.value?.keyExists),
)
const windowFrameModePendingRestart = computed(
  () => settingStore.windowFrameMode !== settingStore.activeWindowFrameMode,
)
const proxyApplyRestartReasons = computed(
  () =>
    settingStore.lastProxyApplyResult?.restartReasons?.map((reason) =>
      t(`settings.restart_reason.${reason}`, reason),
    ) ?? [],
)
const shortcutApplyErrorTitle = computed(() => {
  const code = settingStore.lastShortcutApplyResult?.errorCode || 'unknown'
  return t(`shortcuts.apply_errors.${code}`)
})
const dataSizeItems = computed(() => [
  {
    label: t('settings.disk_usage_cache'),
    value: formatFileSize(dataSize.value.cacheBytes, {
      precision: 1,
      trimTrailingZeros: false,
    }),
    tone: 'success' as const,
  },
  {
    label: t('settings.disk_usage_history'),
    value: formatFileSize(dataSize.value.historyBytes, {
      precision: 1,
      trimTrailingZeros: false,
    }),
    tone: 'success' as const,
  },
  {
    label: t('settings.disk_usage_total'),
    value: formatFileSize(dataSize.value.cacheBytes + dataSize.value.historyBytes, {
      precision: 1,
      trimTrailingZeros: false,
    }),
    tone: 'success' as const,
  },
])

const activeTabLoading = computed(() => {
  switch (activeTab.value) {
    case 'general':
      return !settingStore.settings?.commonConfig || !settingStore.settings?.windowConfig
    case 'proxy':
      return (
        !settingStore.settings?.proxyConfig ||
        !settingStore.settings?.processAttributionConfig
      )
    case 'shortcuts':
      return !settingStore.settings?.shortcuts
    case 'pythonPlugins':
      return !settingStore.settings?.pythonPluginConfig
    case 'logs':
      return loggingStore.isLoading && !loggingStore.status
    case 'storage':
      return (
        !settingStore.settings?.cacheConfig ||
        !settingStore.settings?.historyRetentionConfig
      )
    default:
      return false
  }
})

function formatError(error: unknown) {
  if (error instanceof Error && error.message) {
    return error.message
  }
  return String(error)
}

function clearSaveStatus() {
  showSaveStatus.value = false
  if (saveStatusTimer) {
    clearTimeout(saveStatusTimer)
    saveStatusTimer = null
  }
}

function showSaveStatusNotice() {
  showSaveStatus.value = true
  if (saveStatusTimer) {
    clearTimeout(saveStatusTimer)
  }
  saveStatusTimer = setTimeout(() => {
    showSaveStatus.value = false
    saveStatusTimer = null
  }, 5000)
}

function emitSettingsDirtyChanged(dirty: boolean) {
  try {
    void Events.Emit(SETTINGS_WINDOW_DIRTY_CHANGED_EVENT, dirty).catch(() => {})
  } catch {
    // Ignore event delivery errors while the window is closing.
  }
}

function emitQuitConfirmed() {
  try {
    void Events.Emit(QUIT_CONFIRMED_EVENT).catch(() => {})
  } catch {
    // The backend retains its quit timeout if delivery fails.
  }
}

function emitSettingsSaved(settings = settingStore.settings) {
  if (!settings) {
    return
  }
  try {
    void Events.Emit(SETTINGS_SAVED_EVENT, settings).catch(() => {})
  } catch {
    // Ignore event delivery errors while the window is closing.
  }
}

function listenConfirmQuitRequest() {
  if (offConfirmQuitRequest) {
    return
  }
  offConfirmQuitRequest = Events.On(CONFIRM_QUIT_REQUEST_EVENT, () => {
    if (settingStore.isDirty) {
      quitConfirmVisible.value = true
      return
    }
    emitQuitConfirmed()
  })
}

function markSettingsSectionDirty(section: SettingsDirtySection) {
  if (!isLoaded.value || isCommittingSettings) {
    return
  }
  dirtySections.add(section)
  settingStore.markDirty()
}

async function loadDataSize() {
  const requestToken = dataSizeRequestGuard.begin()
  try {
    const nextDataSize = await GetLocalDataSize()
    if (dataSizeRequestGuard.isCurrent(requestToken)) {
      dataSize.value = nextDataSize
    }
  } catch (e) {
    if (dataSizeRequestGuard.isCurrent(requestToken)) {
      console.error('GetLocalDataSize failed', e)
    }
  }
}

async function loadSystemFonts() {
  if (hasLoadedSystemFonts || isLoadingFonts.value) return
  isLoadingFonts.value = true
  try {
    systemFonts.value = (await ListSystemFonts()) ?? []
    hasLoadedSystemFonts = true
  } catch (e) {
    console.error('ListSystemFonts failed', e)
    notify.error(t('settings.error_load_fonts'))
  } finally {
    isLoadingFonts.value = false
  }
}

function handleFontSelectOpen(open: boolean) {
  if (open) {
    void loadSystemFonts()
  }
}

async function loadCAInfo() {
  isLoadingCAInfo.value = true
  try {
    caInfo.value = await GetCACertificateInfo()
  } catch (e) {
    console.error('GetCACertificateInfo failed', e)
    notify.error(t('settings.error_load_ca_info'))
  } finally {
    isLoadingCAInfo.value = false
  }
}

async function loadLogStatus() {
  if (loggingStore.status || loggingStore.isLoading) return
  try {
    await loggingStore.refresh()
  } catch (error) {
    notify.error(
      t('settings.log_error_load', {
        error: formatError(error),
      }),
    )
  }
}

async function handleGenerateCA(overwrite: boolean) {
  if (settingStore.isDirty) {
    notify.warn(t('settings.ca_save_first'))
    return
  }
  isGeneratingCA.value = true
  try {
    caInfo.value = await GenerateCurrentCACertificate({
      overwrite,
      commonName: '',
      validDays: 0,
    })
    certGeneratedSuccess.value = true
    setTimeout(() => {
      certGeneratedSuccess.value = false
    }, 5000)
  } catch (e) {
    console.error('GenerateCurrentCACertificate failed', e)
    notify.error(t('settings.error_generate_ca'))
  } finally {
    isGeneratingCA.value = false
  }
}

async function handleClearCacheAndHistory() {
  if (localDataClearPending.value) {
    return
  }
  localDataClearPending.value = true
  clearCacheAndHistorySuccess.value = false
  try {
    await ClearCacheAndHistory()
    clearCacheAndHistorySuccess.value = true
    setTimeout(() => {
      clearCacheAndHistorySuccess.value = false
    }, 5000)
  } catch (error) {
    console.error('ClearCacheAndHistory failed', error)
    notify.error(formatError(error))
  } finally {
    await loadDataSize()
    localDataClearPending.value = false
  }
}

async function handleClearCacheFiles() {
  if (localDataClearPending.value) {
    return
  }
  localDataClearPending.value = true
  clearCacheSuccess.value = false
  try {
    await ClearCacheFiles()
    clearCacheSuccess.value = true
    setTimeout(() => {
      clearCacheSuccess.value = false
    }, 5000)
  } catch (error) {
    console.error('ClearCacheFiles failed', error)
    notify.error(formatError(error))
  } finally {
    await loadDataSize()
    localDataClearPending.value = false
  }
}

onMounted(async () => {
  listenConfirmQuitRequest()
  offSaveShortcut = registerShortcutHandler({
    commandId: 'app.save',
    when: () => settingStore.isDirty,
    enabled: () => !settingStore.isSaving && !isCommittingSettings,
    run: handleSave,
    priority: 10,
  })
  await settingStore.load()
  settingStore.isDirty = false
  dirtySections.clear()
  nextTick(() => {
    isLoaded.value = true
  })
})

onBeforeUnmount(() => {
  dataSizeRequestGuard.invalidate()
  if (saveStatusTimer) {
    clearTimeout(saveStatusTimer)
  }
  offConfirmQuitRequest?.()
  offConfirmQuitRequest = null
  offSaveShortcut?.()
  offSaveShortcut = null
  emitSettingsDirtyChanged(false)
})

const cfg = computed(() => settingStore.settings?.proxyConfig)
watch(
  cfg,
  () => {
    if (isLoaded.value) {
      markSettingsSectionDirty('proxy')
    }
  },
  { deep: true },
)

const cacheConfigRef = computed(() => settingStore.settings?.cacheConfig)
watch(
  cacheConfigRef,
  () => {
    if (isLoaded.value) {
      markSettingsSectionDirty('cache')
    }
  },
  { deep: true },
)

const processAttributionConfigRef = computed(
  () => settingStore.settings?.processAttributionConfig,
)
watch(
  processAttributionConfigRef,
  () => {
    if (isLoaded.value) {
      markSettingsSectionDirty('processAttribution')
    }
  },
  { deep: true },
)

const historyRetentionConfigRef = computed(
  () => settingStore.settings?.historyRetentionConfig,
)
watch(
  historyRetentionConfigRef,
  () => {
    if (isLoaded.value) {
      markSettingsSectionDirty('historyRetention')
    }
  },
  { deep: true },
)

const windowConfigRef = computed(() => settingStore.settings?.windowConfig)
watch(
  windowConfigRef,
  () => {
    if (isLoaded.value) {
      markSettingsSectionDirty('window')
    }
  },
  { deep: true },
)

const commonConfigRef = computed(() => settingStore.settings?.commonConfig)
watch(
  commonConfigRef,
  () => {
    if (isLoaded.value) {
      settingStore.previewFonts()
      markSettingsSectionDirty('common')
    }
  },
  { deep: true },
)

const shortcutsConfigRef = computed(() => settingStore.settings?.shortcuts)
watch(
  shortcutsConfigRef,
  () => {
    if (isLoaded.value && !isCommittingSettings) {
      settingStore.clearShortcutApplyResult()
      markSettingsSectionDirty('shortcuts')
    }
  },
  { deep: true },
)

const pythonPluginConfigRef = computed(
  () => settingStore.settings?.pythonPluginConfig,
)
watch(
  pythonPluginConfigRef,
  () => {
    if (isLoaded.value && !isCommittingSettings) {
      markSettingsSectionDirty('pythonPlugins')
    }
  },
  { deep: true },
)

watch(
  () => settingStore.isDirty,
  (dirty) => {
    emitSettingsDirtyChanged(dirty)
    if (dirty) {
      clearSaveStatus()
    }
  },
  { immediate: true },
)

watch(activeTab, (tab) => {
  if (tab === 'shortcuts') {
    shortcutPanelMounted.value = true
  }
  if (tab === 'storage') {
    void loadDataSize()
  } else if (tab === 'proxy') {
    void loadCAInfo()
  } else if (tab === 'logs') {
    void loadLogStatus()
  }
})

async function handleSave() {
  isCommittingSettings = true
  try {
    const result = await settingStore.save({ dirtySections: [...dirtySections] })
    if (!result) {
      return
    }
    dirtySections.clear()
    if (!result.complete) {
      dirtySections.add('shortcuts')
    }
    settingStore.isDirty = !result.complete
    await nextTick()
    showSaveStatusNotice()
    emitSettingsSaved(result.persistedSettings)
    if (activeTab.value === 'proxy') {
      await loadCAInfo()
    }
  } catch (error) {
    notify.error(t('settings.save_failed', { error: formatError(error) }))
  } finally {
    isCommittingSettings = false
  }
}

function handleReset() {
  settingStore.resetToDefaults()
  dirtySections.add('common')
  dirtySections.add('proxy')
  dirtySections.add('window')
  dirtySections.add('cache')
  dirtySections.add('historyRetention')
  dirtySections.add('processAttribution')
  dirtySections.add('trafficTable')
  dirtySections.add('pythonPlugins')
  dirtySections.add('shortcuts')
}

function handleCancelQuit() {
  quitConfirmVisible.value = false
}

function handleConfirmQuit() {
  quitConfirmVisible.value = false
  dirtySections.clear()
  settingStore.isDirty = false
  emitQuitConfirmed()
}
</script>

<template>
  <div class="flex h-full overflow-hidden bg-app-content">
    <nav
      class="flex w-40 shrink-0 flex-col gap-0.5 bg-app-sidebar px-2 py-4 [border-right:1px_solid_var(--app-border-strong-color)]"
      :aria-label="t('menu.settings')"
    >
      <div class="px-2 pb-2.5 text-sm font-semibold uppercase tracking-[0.06em] text-app-text-muted" aria-hidden="true">{{ t('menu.settings') }}</div>
      <div
        v-for="tab in tabs"
        :key="tab.key"
        class="relative flex select-none items-center gap-2 rounded-md px-2.5 py-1.75 text-sm text-app-text-secondary transition-colors duration-150 hover:bg-app-control hover:text-app-text"
        :class="activeTab === tab.key ? activeSettingsNavItemClass : ''"
        role="button"
        tabindex="0"
        :aria-current="activeTab === tab.key ? 'page' : undefined"
        @click="activeTab = tab.key"
        @keydown.enter.space.prevent="activeTab = tab.key"
      >
        <UIcon :name="tab.icon" class="nav-icon size-3.75 shrink-0" />
        <span>{{ tab.label }}</span>
      </div>
    </nav>

    <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <div
        class="flex-1 overflow-y-auto px-7 py-5 max-[720px]:px-4.5 max-[720px]:py-4"
      >
        <div class="mb-4.5 max-w-260 border-b border-app-border pb-3">
          <div class="text-sm font-semibold leading-[1.4] text-app-text-muted">{{ t('menu.settings') }}</div>
          <h2 class="mt-0.5 text-[18px] font-bold leading-[1.35] text-app-text">{{ activeTabTitle }}</h2>
        </div>

        <div class="relative min-w-0 max-w-260" :aria-busy="activeTabLoading">
          <AppLoading v-if="activeTabLoading" />
          <GeneralSettings
            v-if="
              activeTab === 'general' &&
              !activeTabLoading &&
              settingStore.settings?.commonConfig &&
              settingStore.settings?.windowConfig
            "
            v-model:common-config="settingStore.settings.commonConfig"
            v-model:window-config="settingStore.settings.windowConfig"
            :font-options="fontOptions"
            :is-loading-fonts="isLoadingFonts"
            :window-frame-mode-options="windowFrameModeOptions"
            :main-window-close-behavior-options="mainWindowCloseBehaviorOptions"
            :window-frame-mode-pending-restart="windowFrameModePendingRestart"
            @font-select-open="handleFontSelectOpen"
          />
          <ProxySettings
            v-if="
              activeTab === 'proxy' &&
              !activeTabLoading &&
              settingStore.settings?.proxyConfig &&
              settingStore.settings?.processAttributionConfig
            "
            v-model:proxy-config="settingStore.settings.proxyConfig"
            v-model:process-attribution-config="settingStore.settings.processAttributionConfig"
            :ca-info="caInfo"
            :ca-has-existing-files="caHasExistingFiles"
            :is-generating="isGeneratingCA"
            :cert-generated-success="certGeneratedSuccess"
            @generate-ca="handleGenerateCA"
          />
          <ShortcutSettings
            v-if="shortcutPanelMounted && settingStore.settings?.shortcuts"
            v-show="activeTab === 'shortcuts' && !activeTabLoading"
            v-model:shortcuts="settingStore.settings.shortcuts"
            :active="activeTab === 'shortcuts' && !activeTabLoading"
            :runtime-state="settingStore.shortcutRuntimeState"
            :apply-result="settingStore.lastShortcutApplyResult"
          />
          <PythonPluginSettings
            v-if="
              activeTab === 'pythonPlugins' &&
              !activeTabLoading &&
              settingStore.settings?.pythonPluginConfig
            "
            v-model="settingStore.settings.pythonPluginConfig"
          />
          <LogSettings v-if="activeTab === 'logs' && !activeTabLoading" />
          <StorageSettings
            v-if="
              activeTab === 'storage' &&
              !activeTabLoading &&
              settingStore.settings?.cacheConfig &&
              settingStore.settings?.historyRetentionConfig
            "
            v-model:cache-config="settingStore.settings.cacheConfig"
            v-model:history-retention-config="settingStore.settings.historyRetentionConfig"
            :data-size-items="dataSizeItems"
            :clear-cache-success="clearCacheSuccess"
            :clear-cache-and-history-success="clearCacheAndHistorySuccess"
            :clear-pending="localDataClearPending"
            @clear-cache="handleClearCacheFiles"
            @clear-cache-and-history="handleClearCacheAndHistory"
          />
        </div>
      </div>

      <Transition
        enter-active-class="max-h-20 overflow-hidden transition-[opacity,max-height,padding] duration-[250ms] ease-in-out"
        leave-active-class="max-h-20 overflow-hidden transition-[opacity,max-height,padding] duration-[250ms] ease-in-out"
        enter-from-class="max-h-0 opacity-0 [padding-bottom:0]! [padding-top:0]!"
        leave-to-class="max-h-0 opacity-0 [padding-bottom:0]! [padding-top:0]!"
      >
        <div v-if="showSaveStatus" class="shrink-0 bg-app-sidebar-header px-8 py-2.5">
          <UAlert
            v-if="
              settingStore.lastShortcutApplyResult &&
              !settingStore.lastShortcutApplyResult.applied
            "
            color="error"
            variant="subtle"
            :title="shortcutApplyErrorTitle"
            class="w-full"
          />
          <UAlert
            v-else-if="
              settingStore.lastProxyApplyResult?.applied &&
              !settingStore.lastProxyApplyResult.restartRequired
            "
            color="success"
            variant="subtle"
            :title="t('settings.saved_applied_notice')"
            class="w-full"
          />
          <UAlert
            v-else-if="settingStore.lastProxyApplyResult?.restartRequired"
            color="warning"
            variant="subtle"
            :title="
              t('settings.saved_partial_restart_notice', {
                reasons: proxyApplyRestartReasons.join(', '),
              })
            "
            class="w-full"
          />
          <UAlert
            v-else
            color="success"
            variant="subtle"
            :title="t('settings.saved_notice')"
            class="w-full"
          />
        </div>
      </Transition>

      <div
        class="flex shrink-0 items-center justify-end gap-2.5 bg-app-sidebar-header px-8 py-2.5 [border-top:1px_solid_var(--app-border-color)]"
      >
        <UButton
          color="neutral"
          variant="subtle"
          icon="i-lucide-refresh-cw"
          class="min-w-22"
          :label="t('settings.reset')"
          @click="handleReset"
        />
        <UButton
          icon="i-lucide-save"
          class="min-w-22"
          :disabled="!settingStore.isDirty"
          :loading="settingStore.isSaving"
          :label="t('settings.save')"
          @click="handleSave"
        />
      </div>
    </div>
    <ConfirmCardModal
      v-if="quitConfirmVisible"
      :show="quitConfirmVisible"
      :title="t('settings.quit_dirty_title')"
      :positive-text="t('settings.quit_dirty_confirm')"
      :negative-text="t('settings.quit_dirty_cancel')"
      positive-type="warning"
      @update:show="quitConfirmVisible = $event"
      @negative-click="handleCancelQuit"
      @positive-click="handleConfirmQuit"
    >
      {{ t('settings.quit_dirty_message') }}
    </ConfirmCardModal>
  </div>
</template>
