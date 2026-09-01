import type { AppLanguage } from '@/stores/setting'
import type { ThemeMode } from '@/stores/theme'
import type * as settingservice from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import type * as shortcutservice from '#bindings/github.com/josexy/flowlens/backend/services/shortcut_service/models'

export const OPEN_SETTINGS_WINDOW_EVENT = 'app:open-settings-window'
export const PREFERENCES_CHANGED_EVENT = 'app:preferences-changed'
export const SETTINGS_SAVED_EVENT = 'app:settings-saved'
export const SETTINGS_WINDOW_DIRTY_CHANGED_EVENT = 'app:settings-window-dirty-changed'
export const CONFIRM_QUIT_REQUEST_EVENT = 'app:confirm-quit-request'
export const QUIT_CONFIRMED_EVENT = 'app:quit-confirmed'
export const SHUTDOWN_REQUESTED_EVENT = 'app:shutdown-requested'
export const SHUTDOWN_UI_READY_EVENT = 'app:shutdown-ui-ready'
export const SHORTCUTS_CHANGED_EVENT = 'app:shortcuts-changed'
export const SHORTCUT_INVOKE_EVENT = 'app:shortcut-invoke'
export const REQUEST_EDITOR_FILE_DROP_EVENT = 'request-editor:file-drop'
export const LOCAL_DATA_CLEARED_EVENT = 'app:local-data-cleared'

export interface PreferencesChangedPayload {
  themeMode?: ThemeMode
  language?: AppLanguage
}

export interface ShortcutsChangedPayload {
  sourceWindow?: string
  config?: settingservice.ShortcutConfig
  runtimeState?: shortcutservice.ShortcutRuntimeState
  warnings?: string[]
  applied?: boolean
  errorCode?: string
}

export interface ShortcutInvokePayload {
  commandId: string
}

function isThemeMode(value: unknown): value is ThemeMode {
  return value === 'auto' || value === 'light' || value === 'dark'
}

function isLanguage(value: unknown): value is AppLanguage {
  return value === 'zh' || value === 'en'
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

export function parsePreferencesChangedPayload(value: unknown): PreferencesChangedPayload | null {
  const payload = asRecord(value)
  if (!payload) {
    return null
  }

  const preferences: PreferencesChangedPayload = {}
  if (isThemeMode(payload.themeMode)) {
    preferences.themeMode = payload.themeMode
  }
  if (isLanguage(payload.language)) {
    preferences.language = payload.language
  }

  return preferences.themeMode || preferences.language ? preferences : null
}

export function parseShortcutsChangedPayload(value: unknown): ShortcutsChangedPayload | null {
  const payload = asRecord(value)
  if (!payload) return null
  const result: ShortcutsChangedPayload = {}
  if (typeof payload.sourceWindow === 'string') {
    result.sourceWindow = payload.sourceWindow
  }
  const config = asRecord(payload.config)
  if (config && asRecord(config.overrides)) {
    result.config = config as unknown as settingservice.ShortcutConfig
  }
  const runtimeState = asRecord(payload.runtimeState)
  if (runtimeState && asRecord(runtimeState.commands)) {
    result.runtimeState = runtimeState as unknown as shortcutservice.ShortcutRuntimeState
  }
  if (Array.isArray(payload.warnings)) {
    result.warnings = payload.warnings.filter((warning): warning is string => typeof warning === 'string')
  }
  if (typeof payload.applied === 'boolean') {
    result.applied = payload.applied
  }
  if (typeof payload.errorCode === 'string') {
    result.errorCode = payload.errorCode
  }
  return result
}

export function parseShortcutInvokePayload(value: unknown): ShortcutInvokePayload | null {
  if (typeof value === 'string') {
    return value ? { commandId: value } : null
  }
  const payload = asRecord(value)
  if (!payload || typeof payload.commandId !== 'string' || !payload.commandId) {
    return null
  }
  return { commandId: payload.commandId }
}
