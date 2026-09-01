import { defineStore } from 'pinia'
import { ref, computed, nextTick } from 'vue'
import type * as settingservice from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import {
  Get,
  Save,
  SetLanguage,
  SetThemeMode,
  GetActiveWindowFrameMode,
  SaveTrafficTableConfig,
  UpdatePreservingShortcuts,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/settingservice'
import {
  ApplyShortcutConfig,
  GetShortcutRuntimeState,
} from '#bindings/github.com/josexy/flowlens/backend/services/shortcut_service/shortcutservice'
import { ApplyCurrentProxyConfig } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import {
  ConfigureRuntime as ConfigurePythonRuntime,
  DiscoverInterpreters as DiscoverPythonInterpreters,
  GetRuntimeStatus as GetPythonRuntimeStatus,
  TestInterpreter as TestPythonInterpreter,
} from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/pythonpluginservice'
import type * as pythonpluginservice from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import type * as shortcutservice from '#bindings/github.com/josexy/flowlens/backend/services/shortcut_service/models'
import {
  HistoryRetentionUnit,
  MainWindowCloseBehavior,
  ProxyMode,
  UpstreamProxyMode,
  WindowFrameMode,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import type { ThemeMode } from '@/stores/theme'
import type { ShortcutConfig, ShortcutModifier } from '@/shortcuts/types'
import { shortcutCatalog } from '@/shortcuts/catalog'
import {
  TRAFFIC_TABLE_COLUMN_KEYS,
  normalizeHiddenTrafficColumnKeys,
  type TrafficTableColumnKey,
} from '@/utils/traffic-table-columns'

export type AppLanguage = 'zh' | 'en'
export type AppWindowFrameMode =
  | WindowFrameMode.WindowFrameModeCustom
  | WindowFrameMode.WindowFrameModeSystem
export type SettingsDirtySection =
  | 'common'
  | 'proxy'
  | 'processAttribution'
  | 'window'
  | 'cache'
  | 'historyRetention'
  | 'trafficTable'
  | 'pythonPlugins'
  | 'shortcuts'

export interface SettingsSaveOptions {
  dirtySections?: SettingsDirtySection[]
}

export interface SettingsSaveResult {
  complete: boolean
  persistedSettings: settingservice.Settings
}

const DEFAULT_APP_FONT_FAMILY =
  "'DM Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Microsoft YaHei UI', 'Microsoft YaHei', 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans CJK SC', 'Source Han Sans SC', sans-serif"
const DEFAULT_CODE_FONT_FAMILY =
  "'JetBrains Mono', 'Cascadia Mono', 'Cascadia Code', 'Fira Code', 'Consolas', 'SFMono-Regular', 'Menlo', 'Monaco', 'Courier New', monospace"
const DEFAULT_THEME_MODE: ThemeMode = 'light'
const DEFAULT_LANGUAGE: AppLanguage = 'zh'
const DEFAULT_WINDOW_FRAME_MODE: AppWindowFrameMode = WindowFrameMode.WindowFrameModeCustom
const DEFAULT_MAIN_WINDOW_CLOSE_BEHAVIOR =
  MainWindowCloseBehavior.MainWindowCloseBehaviorHideToTray
const DEFAULT_UPSTREAM_PROXY_MODE = UpstreamProxyMode.UpstreamProxyModeSystem
const DEFAULT_LOG_LEVEL = 'info'
const DEFAULT_BODY_CACHE_THRESHOLD_BYTES = 10 * 1024
const DEFAULT_MAX_WS_MESSAGES = 1000
const DEFAULT_HISTORY_RETENTION_VALUE = 7
const DEFAULT_HISTORY_RETENTION_UNIT = HistoryRetentionUnit.HistoryRetentionUnitDay
const MIN_HISTORY_RETENTION_VALUE = 1
const MAX_HISTORY_RETENTION_VALUE = 9999
const DEFAULT_PYTHON_PLUGIN_HOOK_TIMEOUT_MS = 5000

function buildFontStack(fontFamily: string | undefined, fallback: string): string {
  const value = fontFamily?.trim()
  if (!value) return fallback
  const safeValue = value.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
  return `'${safeValue}', ${fallback}`
}

function applyFontSettings(commonConfig: settingservice.CommonConfig | null | undefined) {
  if (typeof document === 'undefined') return
  document.documentElement.style.setProperty(
    '--app-font-family',
    buildFontStack(commonConfig?.appFontFamily, DEFAULT_APP_FONT_FAMILY),
  )
  document.documentElement.style.setProperty(
    '--code-font-family',
    buildFontStack(commonConfig?.codeFontFamily, DEFAULT_CODE_FONT_FAMILY),
  )
}

function isThemeMode(value: string | undefined): value is ThemeMode {
  return value === 'auto' || value === 'light' || value === 'dark'
}

function isLanguage(value: string | undefined): value is AppLanguage {
  return value === 'zh' || value === 'en'
}

function isWindowFrameMode(value: string | undefined): value is AppWindowFrameMode {
  return (
    value === WindowFrameMode.WindowFrameModeCustom ||
    value === WindowFrameMode.WindowFrameModeSystem
  )
}

function isMainWindowCloseBehavior(value: string | undefined): value is MainWindowCloseBehavior {
  return (
    value === MainWindowCloseBehavior.MainWindowCloseBehaviorHideToTray ||
    value === MainWindowCloseBehavior.MainWindowCloseBehaviorQuit
  )
}

function isUpstreamProxyMode(value: string | undefined): value is UpstreamProxyMode {
  return (
    value === UpstreamProxyMode.UpstreamProxyModeNone ||
    value === UpstreamProxyMode.UpstreamProxyModeSystem ||
    value === UpstreamProxyMode.UpstreamProxyModeCustom
  )
}

function sanitizeWindowFrameMode(value: string | undefined): AppWindowFrameMode {
  return isWindowFrameMode(value) ? value : DEFAULT_WINDOW_FRAME_MODE
}

function resolveUpstreamProxyMode(config: settingservice.ProxyConfig): UpstreamProxyMode {
  if (isUpstreamProxyMode(config.upstreamProxyMode)) {
    return config.upstreamProxyMode
  }
  if (config.upstreamProxy?.trim()) {
    return UpstreamProxyMode.UpstreamProxyModeCustom
  }
  if (config.disableProxy) {
    return UpstreamProxyMode.UpstreamProxyModeNone
  }
  return DEFAULT_UPSTREAM_PROXY_MODE
}

function ensureCommonConfig(settings: settingservice.Settings) {
  if (!settings.commonConfig) {
    settings.commonConfig = {
      logLevel: DEFAULT_LOG_LEVEL,
      logDisabled: false,
      appFontFamily: '',
      codeFontFamily: '',
      themeMode: DEFAULT_THEME_MODE,
      language: DEFAULT_LANGUAGE,
    }
  }
  settings.commonConfig.logLevel ||= DEFAULT_LOG_LEVEL
  settings.commonConfig.logDisabled ??= false
  return settings.commonConfig
}

function ensureWindowConfig(settings: settingservice.Settings) {
  if (!settings.windowConfig) {
    settings.windowConfig = {
      positionX: 0,
      positionY: 0,
      width: 0,
      height: 0,
      hasPosition: false,
      isMaximized: false,
      isFullScreen: false,
      frameMode: DEFAULT_WINDOW_FRAME_MODE,
      mainWindowCloseBehavior: DEFAULT_MAIN_WINDOW_CLOSE_BEHAVIOR,
    }
  }
  if (!isWindowFrameMode(settings.windowConfig.frameMode)) {
    settings.windowConfig.frameMode = DEFAULT_WINDOW_FRAME_MODE
  }
  if (!isMainWindowCloseBehavior(settings.windowConfig.mainWindowCloseBehavior)) {
    settings.windowConfig.mainWindowCloseBehavior = DEFAULT_MAIN_WINDOW_CLOSE_BEHAVIOR
  }
  return settings.windowConfig
}

function ensureProxyConfig(settings: settingservice.Settings) {
  if (!settings.proxyConfig) {
    settings.proxyConfig = {
      mode: ProxyMode.ProxyModeHTTP,
      host: '127.0.0.1',
      port: 8080,
      caCertPath: 'certs/ca.crt',
      caKeyPath: 'certs/ca.key',
      upstreamProxyMode: DEFAULT_UPSTREAM_PROXY_MODE,
      upstreamProxy: '',
      disableProxy: false,
      disableHttp2: false,
      skipVerifyTls: false,
      includeHosts: [],
      excludeHosts: [],
      rootCAPaths: [],
      clientCerts: [],
    }
  }
  settings.proxyConfig.upstreamProxyMode = resolveUpstreamProxyMode(settings.proxyConfig)
  settings.proxyConfig.includeHosts ??= []
  settings.proxyConfig.excludeHosts ??= []
  settings.proxyConfig.rootCAPaths ??= []
  settings.proxyConfig.clientCerts ??= []
  return settings.proxyConfig
}

function ensureCacheConfig(settings: settingservice.Settings) {
  if (!settings.cacheConfig) {
    settings.cacheConfig = {
      bodyCacheThresholdBytes: DEFAULT_BODY_CACHE_THRESHOLD_BYTES,
      maxWsMessages: DEFAULT_MAX_WS_MESSAGES,
    }
  }
  return settings.cacheConfig
}

function isHistoryRetentionUnit(value: string | undefined): value is HistoryRetentionUnit {
  return (
    value === HistoryRetentionUnit.HistoryRetentionUnitHour ||
    value === HistoryRetentionUnit.HistoryRetentionUnitDay ||
    value === HistoryRetentionUnit.HistoryRetentionUnitWeek ||
    value === HistoryRetentionUnit.HistoryRetentionUnitMonth ||
    value === HistoryRetentionUnit.HistoryRetentionUnitYear
  )
}

function ensureHistoryRetentionConfig(settings: settingservice.Settings) {
  if (!settings.historyRetentionConfig) {
    settings.historyRetentionConfig = {
      enabled: false,
      value: DEFAULT_HISTORY_RETENTION_VALUE,
      unit: DEFAULT_HISTORY_RETENTION_UNIT,
    }
  }
  if (
    settings.historyRetentionConfig.value < MIN_HISTORY_RETENTION_VALUE ||
    settings.historyRetentionConfig.value > MAX_HISTORY_RETENTION_VALUE
  ) {
    settings.historyRetentionConfig.value = DEFAULT_HISTORY_RETENTION_VALUE
  }
  if (!isHistoryRetentionUnit(settings.historyRetentionConfig.unit)) {
    settings.historyRetentionConfig.unit = DEFAULT_HISTORY_RETENTION_UNIT
  }
  return settings.historyRetentionConfig
}

function ensureProcessAttributionConfig(settings: settingservice.Settings) {
  if (!settings.processAttributionConfig) {
    settings.processAttributionConfig = { enabled: true }
  }
  return settings.processAttributionConfig
}

function ensureTrafficTableConfig(settings: settingservice.Settings) {
  if (!settings.trafficTableConfig) {
    settings.trafficTableConfig = { hiddenColumns: [] }
  }
  settings.trafficTableConfig.hiddenColumns = normalizeHiddenTrafficColumnKeys(
    settings.trafficTableConfig.hiddenColumns,
  )
  return settings.trafficTableConfig
}

function ensurePythonPluginConfig(settings: settingservice.Settings) {
  if (!settings.pythonPluginConfig) {
    settings.pythonPluginConfig = {
      enabled: false,
      interpreterPath: '',
      hookTimeoutMs: DEFAULT_PYTHON_PLUGIN_HOOK_TIMEOUT_MS,
    }
  }
  return settings.pythonPluginConfig
}

function ensureShortcutConfig(settings: settingservice.Settings) {
  if (!settings.shortcuts) {
    settings.shortcuts = { overrides: {} }
  }
  settings.shortcuts.overrides ??= {}
  return settings.shortcuts
}

function cloneCommonConfig(config: settingservice.CommonConfig): settingservice.CommonConfig {
  return {
    logLevel: config.logLevel,
    logDisabled: config.logDisabled,
    appFontFamily: config.appFontFamily,
    codeFontFamily: config.codeFontFamily,
    themeMode: config.themeMode,
    language: config.language,
  }
}

function cloneClientCertConfig(config: settingservice.ClientCertConfig): settingservice.ClientCertConfig {
  return {
    enabled: config.enabled,
    hostname: config.hostname,
    certPath: config.certPath,
    keyPath: config.keyPath,
  }
}

function cloneProxyConfig(config: settingservice.ProxyConfig): settingservice.ProxyConfig {
  return {
    mode: config.mode,
    host: config.host,
    port: config.port,
    caCertPath: config.caCertPath,
    caKeyPath: config.caKeyPath,
    upstreamProxyMode: config.upstreamProxyMode,
    upstreamProxy: config.upstreamProxy,
    disableProxy: config.disableProxy,
    disableHttp2: config.disableHttp2,
    skipVerifyTls: config.skipVerifyTls,
    includeHosts: [...(config.includeHosts ?? [])],
    excludeHosts: [...(config.excludeHosts ?? [])],
    rootCAPaths: [...(config.rootCAPaths ?? [])],
    clientCerts: (config.clientCerts ?? []).map(cloneClientCertConfig),
  }
}

function cloneWindowConfig(config: settingservice.WindowConfig): settingservice.WindowConfig {
  return {
    positionX: config.positionX,
    positionY: config.positionY,
    width: config.width,
    height: config.height,
    hasPosition: config.hasPosition,
    isMaximized: config.isMaximized,
    isFullScreen: config.isFullScreen,
    frameMode: config.frameMode,
    mainWindowCloseBehavior: config.mainWindowCloseBehavior,
  }
}

function cloneCacheConfig(config: settingservice.CacheConfig): settingservice.CacheConfig {
  return {
    bodyCacheThresholdBytes: config.bodyCacheThresholdBytes,
    maxWsMessages: config.maxWsMessages,
  }
}

function cloneHistoryRetentionConfig(
  config: settingservice.HistoryRetentionConfig,
): settingservice.HistoryRetentionConfig {
  return {
    enabled: config.enabled,
    value: config.value,
    unit: config.unit,
  }
}

function cloneProcessAttributionConfig(
  config: settingservice.ProcessAttributionConfig,
): settingservice.ProcessAttributionConfig {
  return { enabled: config.enabled }
}

function cloneTrafficTableConfig(
  config: settingservice.TrafficTableConfig,
): settingservice.TrafficTableConfig {
  return {
    hiddenColumns: [...normalizeHiddenTrafficColumnKeys(config.hiddenColumns)],
  }
}

function clonePythonPluginConfig(
  config: settingservice.PythonPluginConfig,
): settingservice.PythonPluginConfig {
  return {
    enabled: config.enabled,
    interpreterPath: config.interpreterPath,
    hookTimeoutMs: config.hookTimeoutMs,
  }
}

function cloneShortcutConfig(config: settingservice.ShortcutConfig): settingservice.ShortcutConfig {
  const overrides: Record<string, settingservice.ShortcutOverride> = {}
  for (const [id, override] of Object.entries(config.overrides ?? {})) {
    if (!override) continue
    overrides[id] = {
      binding: override.binding
        ? {
            modifiers: [...(override.binding.modifiers ?? [])],
            key: override.binding.key,
          }
        : null,
      scope: override.scope,
    }
  }
  return { overrides }
}

function shortcutConfigsEqual(
  left: settingservice.ShortcutConfig | null | undefined,
  right: settingservice.ShortcutConfig | null | undefined,
) {
  const leftOverrides = left?.overrides ?? {}
  const rightOverrides = right?.overrides ?? {}
  const leftIds = Object.keys(leftOverrides).sort()
  const rightIds = Object.keys(rightOverrides).sort()
  if (leftIds.length !== rightIds.length) return false

  return leftIds.every((commandId, index) => {
    if (commandId !== rightIds[index]) return false
    const leftOverride = leftOverrides[commandId]
    const rightOverride = rightOverrides[commandId]
    if (!leftOverride || !rightOverride || leftOverride.scope !== rightOverride.scope) {
      return false
    }
    if (!leftOverride.binding || !rightOverride.binding) {
      return leftOverride.binding === rightOverride.binding
    }
    const leftModifiers = leftOverride.binding.modifiers ?? []
    const rightModifiers = rightOverride.binding.modifiers ?? []
    return (
      leftOverride.binding.key === rightOverride.binding.key &&
      leftModifiers.length === rightModifiers.length &&
      leftModifiers.every((modifier, modifierIndex) => modifier === rightModifiers[modifierIndex])
    )
  })
}

function resolveShortcutConfig(settings: settingservice.Settings): ShortcutConfig {
  const config = ensureShortcutConfig(settings)
  const overrides: ShortcutConfig['overrides'] = {}
  for (const [id, override] of Object.entries(config.overrides ?? {})) {
    if (!override) continue
    const binding = override.binding
      ? {
          modifiers: (override.binding.modifiers ?? []).map(String).filter(
            (modifier): modifier is ShortcutModifier =>
              modifier === 'primary' ||
              modifier === 'control' ||
              modifier === 'alt' ||
              modifier === 'shift' ||
              modifier === 'super',
          ),
          key: override.binding.key,
        }
      : null
    overrides[id] = {
      binding,
      scope: override.scope === 'global' ? 'global' : 'application',
    }
  }
  return { overrides }
}

function cloneSettings(settings: settingservice.Settings): settingservice.Settings {
  return {
    commonConfig: settings.commonConfig ? cloneCommonConfig(settings.commonConfig) : null,
    proxyConfig: settings.proxyConfig ? cloneProxyConfig(settings.proxyConfig) : null,
    windowConfig: settings.windowConfig ? cloneWindowConfig(settings.windowConfig) : null,
    cacheConfig: settings.cacheConfig ? cloneCacheConfig(settings.cacheConfig) : null,
    historyRetentionConfig: settings.historyRetentionConfig
      ? cloneHistoryRetentionConfig(settings.historyRetentionConfig)
      : null,
    processAttributionConfig: settings.processAttributionConfig
      ? cloneProcessAttributionConfig(settings.processAttributionConfig)
      : null,
    trafficTableConfig: settings.trafficTableConfig
      ? cloneTrafficTableConfig(settings.trafficTableConfig)
      : null,
    pythonPluginConfig: settings.pythonPluginConfig
      ? clonePythonPluginConfig(settings.pythonPluginConfig)
      : null,
    shortcuts: settings.shortcuts ? cloneShortcutConfig(settings.shortcuts) : null,
  }
}

function emptyShortcutRuntimeState(): shortcutservice.ShortcutRuntimeState {
  return { commands: {}, warnings: [] }
}

function cloneShortcutRuntimeState(
  state: shortcutservice.ShortcutRuntimeState | null | undefined,
): shortcutservice.ShortcutRuntimeState {
  const commands: NonNullable<shortcutservice.ShortcutRuntimeState['commands']> = {}
  for (const [commandId, commandState] of Object.entries(state?.commands ?? {})) {
    if (commandState) {
      commands[commandId] = { ...commandState }
    }
  }
  return {
    commands,
    warnings: [...(state?.warnings ?? [])],
  }
}

export const useSettingStore = defineStore('setting', () => {
  const settings = ref<settingservice.Settings | null>(null)
  const isDirty = ref(false)
  const isSaving = ref(false)
  const isSavingTrafficTableConfig = ref(false)
  const lastProxyApplyResult = ref<proxyservice.ProxyConfigApplyResult | null>(null)
  const lastShortcutApplyResult = ref<shortcutservice.ShortcutApplyResult | null>(null)
  const lastPythonRuntimeStatus = ref<pythonpluginservice.RuntimeStatus | null>(null)
  const shortcutRuntimeState = ref<shortcutservice.ShortcutRuntimeState>(
    emptyShortcutRuntimeState(),
  )
  const activeWindowFrameMode = ref<AppWindowFrameMode>(DEFAULT_WINDOW_FRAME_MODE)

  let isSyncingPreferences = false
  let loadPromise: Promise<void> | null = null
  let hasLoadedSettings = false

  // Reactive font-stack string consumed directly by Monaco editor options.
  // Computed here so Monaco components get a plain string (not a CSS var).
  const resolvedCodeFontFamily = computed(() =>
    buildFontStack(settings.value?.commonConfig?.codeFontFamily, DEFAULT_CODE_FONT_FAMILY),
  )
  const hiddenTrafficColumnKeys = computed(
    () =>
      new Set<TrafficTableColumnKey>(
        normalizeHiddenTrafficColumnKeys(settings.value?.trafficTableConfig?.hiddenColumns),
      ),
  )

  async function runLoad() {
    settings.value = await Get()
    if (settings.value) {
      ensureCommonConfig(settings.value)
      ensureProxyConfig(settings.value)
      ensureWindowConfig(settings.value)
      ensureCacheConfig(settings.value)
      ensureHistoryRetentionConfig(settings.value)
      ensureProcessAttributionConfig(settings.value)
      ensureTrafficTableConfig(settings.value)
      ensurePythonPluginConfig(settings.value)
      ensureShortcutConfig(settings.value)
    }
    activeWindowFrameMode.value = sanitizeWindowFrameMode(await GetActiveWindowFrameMode())
    shortcutRuntimeState.value = cloneShortcutRuntimeState(
      await GetShortcutRuntimeState().catch(() => emptyShortcutRuntimeState()),
    )
    lastPythonRuntimeStatus.value = await GetPythonRuntimeStatus().catch(() => null)
    applyFontSettings(settings.value?.commonConfig)
    isDirty.value = false
    lastProxyApplyResult.value = null
    lastShortcutApplyResult.value = null
    hasLoadedSettings = true
  }

  async function load() {
    if (hasLoadedSettings) {
      return
    }
    if (loadPromise) {
      await loadPromise
      return
    }

    loadPromise = runLoad()
    try {
      await loadPromise
    } finally {
      loadPromise = null
    }
  }

  function syncExternalSettings(nextSettings: settingservice.Settings | null | undefined) {
    if (!nextSettings) return
    ensureCommonConfig(nextSettings)
    ensureProxyConfig(nextSettings)
    ensureWindowConfig(nextSettings)
    ensureCacheConfig(nextSettings)
    ensureHistoryRetentionConfig(nextSettings)
    ensureProcessAttributionConfig(nextSettings)
    ensureTrafficTableConfig(nextSettings)
    ensurePythonPluginConfig(nextSettings)
    ensureShortcutConfig(nextSettings)
    settings.value = nextSettings
    applyFontSettings(nextSettings.commonConfig)
    isDirty.value = false
    lastProxyApplyResult.value = null
  }

  function syncExternalShortcutState(
    config: settingservice.ShortcutConfig | null | undefined,
    runtimeState?: shortcutservice.ShortcutRuntimeState | null,
  ) {
    if (
      settings.value &&
      config &&
      !shortcutConfigsEqual(settings.value.shortcuts, config)
    ) {
      settings.value.shortcuts = cloneShortcutConfig(config)
    }
    if (runtimeState) {
      shortcutRuntimeState.value = cloneShortcutRuntimeState(runtimeState)
    }
    lastShortcutApplyResult.value = null
  }

  async function refreshShortcutRuntimeState() {
    shortcutRuntimeState.value = cloneShortcutRuntimeState(await GetShortcutRuntimeState())
  }

  function clearShortcutApplyResult() {
    lastShortcutApplyResult.value = null
  }

  function markDirty() {
    if (isSyncingPreferences) return
    isDirty.value = true
  }

  function mergeSettingsForSave(
    currentSettings: settingservice.Settings,
    latestSettings: settingservice.Settings | null,
    dirtySections: SettingsDirtySection[],
  ) {
    if (!latestSettings) {
      return currentSettings
    }

    ensureCommonConfig(currentSettings)
    ensureProxyConfig(currentSettings)
    ensureWindowConfig(currentSettings)
    ensureCacheConfig(currentSettings)
    ensureHistoryRetentionConfig(currentSettings)
    ensureProcessAttributionConfig(currentSettings)
    ensureTrafficTableConfig(currentSettings)
    ensurePythonPluginConfig(currentSettings)
    ensureShortcutConfig(currentSettings)
    ensureCommonConfig(latestSettings)
    ensureProxyConfig(latestSettings)
    ensureWindowConfig(latestSettings)
    ensureCacheConfig(latestSettings)
    ensureHistoryRetentionConfig(latestSettings)
    ensureProcessAttributionConfig(latestSettings)
    ensureTrafficTableConfig(latestSettings)
    ensurePythonPluginConfig(latestSettings)
    ensureShortcutConfig(latestSettings)

    const mergedSettings = cloneSettings(latestSettings)
    const sections = new Set(dirtySections)

    if (sections.has('common') && currentSettings.commonConfig) {
      const latestCommonConfig = ensureCommonConfig(latestSettings)
      mergedSettings.commonConfig = cloneCommonConfig(currentSettings.commonConfig)
      mergedSettings.commonConfig.themeMode = latestCommonConfig.themeMode
      mergedSettings.commonConfig.language = latestCommonConfig.language
    }
    if (sections.has('proxy') && currentSettings.proxyConfig) {
      mergedSettings.proxyConfig = cloneProxyConfig(currentSettings.proxyConfig)
    }
    if (sections.has('cache') && currentSettings.cacheConfig) {
      mergedSettings.cacheConfig = cloneCacheConfig(currentSettings.cacheConfig)
    }
    if (sections.has('historyRetention') && currentSettings.historyRetentionConfig) {
      mergedSettings.historyRetentionConfig = cloneHistoryRetentionConfig(
        currentSettings.historyRetentionConfig,
      )
    }
    if (sections.has('processAttribution') && currentSettings.processAttributionConfig) {
      mergedSettings.processAttributionConfig = cloneProcessAttributionConfig(
        currentSettings.processAttributionConfig,
      )
    }
    if (sections.has('window') && currentSettings.windowConfig) {
      mergedSettings.windowConfig = cloneWindowConfig(ensureWindowConfig(latestSettings))
      mergedSettings.windowConfig.frameMode = currentSettings.windowConfig.frameMode
      mergedSettings.windowConfig.mainWindowCloseBehavior =
        currentSettings.windowConfig.mainWindowCloseBehavior
    }
    if (sections.has('trafficTable') && currentSettings.trafficTableConfig) {
      mergedSettings.trafficTableConfig = cloneTrafficTableConfig(
        currentSettings.trafficTableConfig,
      )
    }
    if (sections.has('shortcuts') && currentSettings.shortcuts) {
      mergedSettings.shortcuts = cloneShortcutConfig(currentSettings.shortcuts)
    }

    return mergedSettings
  }

  async function save(options?: SettingsSaveOptions): Promise<SettingsSaveResult | null> {
    if (!settings.value) return null
    isSaving.value = true
    const requestedSections = options?.dirtySections ?? ['proxy']
    const ordinaryDirtySections = requestedSections.filter(
      (section): section is Exclude<SettingsDirtySection, 'shortcuts' | 'pythonPlugins'> =>
        section !== 'shortcuts' && section !== 'pythonPlugins',
    )
    const shortcutCandidate =
      options?.dirtySections?.includes('shortcuts') && settings.value.shortcuts
        ? cloneShortcutConfig(settings.value.shortcuts)
        : null
    const pythonPluginCandidate =
      options?.dirtySections?.includes('pythonPlugins') && settings.value.pythonPluginConfig
        ? clonePythonPluginConfig(settings.value.pythonPluginConfig)
        : null
    try {
      const latestSettings = await Get()
      if (!latestSettings) {
        throw new Error('application settings are not available')
      }
      settings.value = mergeSettingsForSave(
        settings.value,
        latestSettings,
        ordinaryDirtySections,
      )

      if (!options?.dirtySections || ordinaryDirtySections.length > 0) {
        await UpdatePreservingShortcuts(settings.value)
        await Save()
        const persistedSettings = await Get()
        if (persistedSettings) {
          ensureCommonConfig(persistedSettings)
          ensureProxyConfig(persistedSettings)
          ensureWindowConfig(persistedSettings)
          ensureCacheConfig(persistedSettings)
          ensureHistoryRetentionConfig(persistedSettings)
          ensureProcessAttributionConfig(persistedSettings)
          ensureTrafficTableConfig(persistedSettings)
          ensurePythonPluginConfig(persistedSettings)
          ensureShortcutConfig(persistedSettings)
          settings.value = persistedSettings
        }
      }

      if (pythonPluginCandidate) {
        try {
          const status = await ConfigurePythonRuntime(pythonPluginCandidate)
          if (!status) {
            throw new Error('Python runtime status is not available')
          }
          lastPythonRuntimeStatus.value = status
          const refreshedSettings = await Get()
          if (refreshedSettings?.pythonPluginConfig) {
            settings.value.pythonPluginConfig = clonePythonPluginConfig(
              refreshedSettings.pythonPluginConfig,
            )
          } else {
            settings.value.pythonPluginConfig = clonePythonPluginConfig(pythonPluginCandidate)
          }
        } catch (pythonError) {
          const refreshedSettings = await Get().catch(() => null)
          const persistedConfig = refreshedSettings
            ? clonePythonPluginConfig(ensurePythonPluginConfig(refreshedSettings))
            : {
                enabled: false,
                interpreterPath: '',
                hookTimeoutMs: DEFAULT_PYTHON_PLUGIN_HOOK_TIMEOUT_MS,
              }
          settings.value.pythonPluginConfig = {
            ...pythonPluginCandidate,
            enabled: persistedConfig.enabled,
          }
          lastPythonRuntimeStatus.value = await GetPythonRuntimeStatus().catch(() => null)
          throw pythonError
        }
      }

      let complete = true
      const persistedSettings = cloneSettings(settings.value)
      if (shortcutCandidate) {
        const applyResult = await ApplyShortcutConfig(shortcutCandidate)
        if (!applyResult || !applyResult.config) {
          throw new Error('shortcut apply result is not available')
        }
        lastShortcutApplyResult.value = applyResult
        persistedSettings.shortcuts = cloneShortcutConfig(applyResult.config)
        if (applyResult.applied) {
          settings.value.shortcuts = cloneShortcutConfig(applyResult.config)
          shortcutRuntimeState.value = cloneShortcutRuntimeState(applyResult.runtimeState)
        } else {
          settings.value.shortcuts = shortcutCandidate
          complete = false
        }
      }

      const shouldApplyProxy = !options?.dirtySections || options.dirtySections.includes('proxy')
      lastProxyApplyResult.value = shouldApplyProxy ? await ApplyCurrentProxyConfig() : null
      applyFontSettings(settings.value.commonConfig)
      isDirty.value = !complete
      return { complete, persistedSettings }
    } catch (error) {
      if (shortcutCandidate && settings.value) {
        settings.value.shortcuts = shortcutCandidate
      }
      isDirty.value = true
      throw error
    } finally {
      isSaving.value = false
    }
  }

  async function replaceTrafficTableHiddenColumns(
    nextValues: readonly string[],
  ): Promise<boolean> {
    if (!settings.value || isSavingTrafficTableConfig.value) {
      return false
    }

    const config = ensureTrafficTableConfig(settings.value)
    const previousHiddenColumns = [...normalizeHiddenTrafficColumnKeys(config.hiddenColumns)]
    const nextHiddenColumns = normalizeHiddenTrafficColumnKeys(nextValues)
    if (
      previousHiddenColumns.length === nextHiddenColumns.length &&
      previousHiddenColumns.every((key, index) => key === nextHiddenColumns[index])
    ) {
      return true
    }

    const previousDirty = isDirty.value
    config.hiddenColumns = nextHiddenColumns
    isSavingTrafficTableConfig.value = true
    try {
      await SaveTrafficTableConfig({ hiddenColumns: nextHiddenColumns })
      return true
    } catch (error) {
      if (settings.value) {
        ensureTrafficTableConfig(settings.value).hiddenColumns = previousHiddenColumns
      }
      isDirty.value = previousDirty
      throw error
    } finally {
      isSavingTrafficTableConfig.value = false
    }
  }

  async function setTrafficTableColumnVisible(
    key: TrafficTableColumnKey,
    visible: boolean,
  ): Promise<boolean> {
    const hidden = new Set(hiddenTrafficColumnKeys.value)
    if (visible) {
      hidden.delete(key)
    } else {
      const visibleCount = TRAFFIC_TABLE_COLUMN_KEYS.length - hidden.size
      if (!hidden.has(key) && visibleCount <= 1) {
        return false
      }
      hidden.add(key)
    }
    return replaceTrafficTableHiddenColumns([...hidden])
  }

  async function showAllTrafficTableColumns(): Promise<boolean> {
    return replaceTrafficTableHiddenColumns([])
  }

  function resetToDefaults() {
    if (!settings.value) return
    if (!settings.value.commonConfig) {
      settings.value.commonConfig = {
        logLevel: DEFAULT_LOG_LEVEL,
        logDisabled: false,
        appFontFamily: '',
        codeFontFamily: '',
        themeMode: DEFAULT_THEME_MODE,
        language: DEFAULT_LANGUAGE,
      }
    }
    settings.value.commonConfig.logLevel = DEFAULT_LOG_LEVEL
    settings.value.commonConfig.logDisabled = false
    settings.value.commonConfig.appFontFamily = ''
    settings.value.commonConfig.codeFontFamily = ''
    const windowConfig = ensureWindowConfig(settings.value)
    windowConfig.frameMode = DEFAULT_WINDOW_FRAME_MODE
    windowConfig.mainWindowCloseBehavior = DEFAULT_MAIN_WINDOW_CLOSE_BEHAVIOR
    const cfg = ensureProxyConfig(settings.value)
    cfg.mode = ProxyMode.ProxyModeHTTP
    cfg.host = '127.0.0.1'
    cfg.port = 8080
    cfg.caCertPath = 'certs/ca.crt'
    cfg.caKeyPath = 'certs/ca.key'
    cfg.upstreamProxyMode = DEFAULT_UPSTREAM_PROXY_MODE
    cfg.upstreamProxy = ''
    cfg.disableProxy = false
    cfg.disableHttp2 = false
    cfg.skipVerifyTls = false
    cfg.includeHosts = []
    cfg.excludeHosts = []
    cfg.rootCAPaths = []
    cfg.clientCerts = []
    const cacheConfig = ensureCacheConfig(settings.value)
    cacheConfig.bodyCacheThresholdBytes = DEFAULT_BODY_CACHE_THRESHOLD_BYTES
    cacheConfig.maxWsMessages = DEFAULT_MAX_WS_MESSAGES
    const historyRetentionConfig = ensureHistoryRetentionConfig(settings.value)
    historyRetentionConfig.enabled = false
    historyRetentionConfig.value = DEFAULT_HISTORY_RETENTION_VALUE
    historyRetentionConfig.unit = DEFAULT_HISTORY_RETENTION_UNIT
    ensureProcessAttributionConfig(settings.value).enabled = true
    ensureTrafficTableConfig(settings.value).hiddenColumns = []
    const pythonPluginConfig = ensurePythonPluginConfig(settings.value)
    pythonPluginConfig.enabled = false
    pythonPluginConfig.interpreterPath = ''
    pythonPluginConfig.hookTimeoutMs = DEFAULT_PYTHON_PLUGIN_HOOK_TIMEOUT_MS
    const shortcuts = ensureShortcutConfig(settings.value)
    const nextOverrides = { ...(shortcuts.overrides ?? {}) }
    for (const command of shortcutCatalog) {
      delete nextOverrides[command.id]
    }
    shortcuts.overrides = nextOverrides
    isDirty.value = true
    lastProxyApplyResult.value = null
    lastShortcutApplyResult.value = null
    // Note: applyFontSettings is NOT called here.
    // The watch(commonConfigRef) in SettingsView.vue calls previewFonts(),
    // which drives CSS variable updates reactively whenever commonConfig mutates.
  }

  function previewFonts() {
    applyFontSettings(settings.value?.commonConfig)
  }

  const themeMode = computed<ThemeMode>(() => {
    const value = settings.value?.commonConfig?.themeMode
    return isThemeMode(value) ? value : DEFAULT_THEME_MODE
  })

  const language = computed<AppLanguage>(() => {
    const value = settings.value?.commonConfig?.language
    return isLanguage(value) ? value : DEFAULT_LANGUAGE
  })

  const windowFrameMode = computed<AppWindowFrameMode>(() => {
    const value = settings.value?.windowConfig?.frameMode
    return isWindowFrameMode(value) ? value : DEFAULT_WINDOW_FRAME_MODE
  })

  const usesCustomWindowFrame = computed(
    () => activeWindowFrameMode.value === WindowFrameMode.WindowFrameModeCustom,
  )

  const shortcutConfig = computed<ShortcutConfig>(() => {
    if (!settings.value) {
      return { overrides: {} }
    }
    return resolveShortcutConfig(settings.value)
  })

  async function syncPreferenceMutation(mutator: () => void) {
    isSyncingPreferences = true
    try {
      mutator()
      await nextTick()
    } finally {
      isSyncingPreferences = false
    }
  }

  async function setThemeModePreference(mode: ThemeMode) {
    await SetThemeMode(mode)
    const currentSettings = settings.value
    if (currentSettings) {
      await syncPreferenceMutation(() => {
        ensureCommonConfig(currentSettings).themeMode = mode
      })
    }
  }

  async function setLanguagePreference(value: AppLanguage) {
    await SetLanguage(value)
    const currentSettings = settings.value
    if (currentSettings) {
      await syncPreferenceMutation(() => {
        ensureCommonConfig(currentSettings).language = value
      })
    }
  }

  async function syncExternalPreferences(preferences: {
    themeMode?: ThemeMode
    language?: AppLanguage
  }) {
    const currentSettings = settings.value
    if (!currentSettings) return
    await syncPreferenceMutation(() => {
      const commonConfig = ensureCommonConfig(currentSettings)
      if (isThemeMode(preferences.themeMode)) {
        commonConfig.themeMode = preferences.themeMode
      }
      if (isLanguage(preferences.language)) {
        commonConfig.language = preferences.language
      }
    })
  }

  async function syncLogPreference(enabled: boolean, level: string) {
    const currentSettings = settings.value
    if (!currentSettings) return
    await syncPreferenceMutation(() => {
      const commonConfig = ensureCommonConfig(currentSettings)
      commonConfig.logDisabled = !enabled
      commonConfig.logLevel = level || DEFAULT_LOG_LEVEL
    })
  }

  async function refreshPythonRuntimeStatus() {
    lastPythonRuntimeStatus.value = await GetPythonRuntimeStatus()
    return lastPythonRuntimeStatus.value
  }

  async function testPythonInterpreter(interpreterPath: string) {
    const status = await TestPythonInterpreter(interpreterPath)
    if (!status) {
      throw new Error('Python interpreter test returned no status')
    }
    return status
  }

  async function discoverPythonInterpreters(configuredPath: string) {
    return (await DiscoverPythonInterpreters(configuredPath)) ?? []
  }

  return {
    settings,
    isDirty,
    isSaving,
    hiddenTrafficColumnKeys,
    isSavingTrafficTableConfig,
    lastProxyApplyResult,
    lastShortcutApplyResult,
    lastPythonRuntimeStatus,
    shortcutRuntimeState,
    activeWindowFrameMode,
    resolvedCodeFontFamily,
    themeMode,
    language,
    windowFrameMode,
    usesCustomWindowFrame,
    shortcutConfig,
    load,
    syncExternalSettings,
    syncExternalShortcutState,
    refreshShortcutRuntimeState,
    clearShortcutApplyResult,
    save,
    setTrafficTableColumnVisible,
    showAllTrafficTableColumns,
    markDirty,
    resetToDefaults,
    previewFonts,
    setThemeModePreference,
    setLanguagePreference,
    syncExternalPreferences,
    syncLogPreference,
    refreshPythonRuntimeStatus,
    discoverPythonInterpreters,
    testPythonInterpreter,
  }
})
