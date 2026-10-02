import type { ThemeColorState } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

export const DEFAULT_THEME_PRIMARY_COLOR = 'blue'
export const DEFAULT_THEME_NEUTRAL_COLOR = 'slate'

export const THEME_PRIMARY_COLORS = [
  'red',
  'orange',
  'amber',
  'yellow',
  'lime',
  'green',
  'emerald',
  'teal',
  'cyan',
  'sky',
  'blue',
  'indigo',
  'violet',
  'purple',
  'fuchsia',
  'pink',
  'rose',
] as const
export const THEME_NEUTRAL_COLORS = [
  'slate',
  'gray',
  'zinc',
  'neutral',
  'stone',
  'taupe',
  'mauve',
  'mist',
  'olive',
] as const

export function normalizeThemePrimaryColor(value: unknown): string {
  return typeof value === 'string' && THEME_PRIMARY_COLORS.some((color) => color === value)
    ? value
    : DEFAULT_THEME_PRIMARY_COLOR
}

export function normalizeThemeNeutralColor(value: unknown): string {
  return typeof value === 'string' && THEME_NEUTRAL_COLORS.some((color) => color === value)
    ? value
    : DEFAULT_THEME_NEUTRAL_COLOR
}

export function parseThemeColorState(value: unknown): ThemeColorState | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const state = value as Record<string, unknown>
  if (
    typeof state.primaryColor !== 'string' ||
    normalizeThemePrimaryColor(state.primaryColor) !== state.primaryColor ||
    typeof state.neutralColor !== 'string' ||
    normalizeThemeNeutralColor(state.neutralColor) !== state.neutralColor ||
    typeof state.preview !== 'boolean' ||
    ![state.revision, state.sessionID, state.sequence].every(
      (number) => typeof number === 'number' && Number.isSafeInteger(number) && number >= 0,
    )
  )
    return null
  return state as unknown as ThemeColorState
}
