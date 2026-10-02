import { defineStore } from 'pinia'
import { shallowRef, computed, onScopeDispose } from 'vue'
import { Events, Window } from '@wailsio/runtime'
import { THEME_COLORS_CHANGED_EVENT } from '@/runtime/appEvents'
import { useAppConfig } from '@nuxt/ui/runtime/vue/composables/useAppConfig.js'
import {
  GetThemeColorState,
  PreviewThemeColors,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/settingservice'
import {
  DEFAULT_THEME_PRIMARY_COLOR,
  DEFAULT_THEME_NEUTRAL_COLOR,
  parseThemeColorState,
} from '@/utils/themeColors'

export type ThemeMode = 'auto' | 'light' | 'dark'

export const useThemeStore = defineStore('theme', () => {
  const themeMode = shallowRef<ThemeMode>('light')
  const systemPrefersDark = shallowRef(false)
  const primaryColor = shallowRef(DEFAULT_THEME_PRIMARY_COLOR as string)
  const neutralColor = shallowRef(DEFAULT_THEME_NEUTRAL_COLOR as string)
  const appearanceRevision = shallowRef(0)
  const appConfig = useAppConfig()
  let colorRevision = -1
  let previewSession = 0
  let previewSequence = 0
  let backgroundFrame = 0
  let stopSystemPreference: (() => void) | undefined
  let colorsObserver: MutationObserver | undefined
  let nativeBackgroundWrite: Promise<void> = Promise.resolve()
  let disposed = false
  let offThemeColorsChanged: (() => void) | undefined
  const pendingPreviews = new Set<Promise<void>>()

  const isDark = computed(() => {
    if (themeMode.value === 'auto') {
      return systemPrefersDark.value
    }
    return themeMode.value === 'dark'
  })

  // Nuxt UI patches its color stylesheet through useHead. Observe that patch
  // so CSS readers and the native window see the applied palette, even when
  // head rendering finishes after Vue's nextTick.
  const scheduleAppearanceRefresh = () => {
    if (disposed) return
    if (typeof document === 'undefined') {
      appearanceRevision.value++
      return
    }
    cancelAnimationFrame(backgroundFrame)
    backgroundFrame = requestAnimationFrame(() => {
      backgroundFrame = 0
      const revision = ++appearanceRevision.value
      const color = getComputedStyle(document.body).backgroundColor
      const context = document
        .createElement('canvas')
        .getContext('2d', { willReadFrequently: true })
      if (!context) return
      // Canvas converts CSS colors, including Tailwind's OKLCH, to sRGB bytes.
      context.fillStyle = color
      context.fillRect(0, 0, 1, 1)
      const rgba = context.getImageData(0, 0, 1, 1).data
      nativeBackgroundWrite = nativeBackgroundWrite
        .catch(() => {})
        .then(async () => {
          if (!disposed && revision === appearanceRevision.value) {
            await Window.SetBackgroundColour(rgba[0]!, rgba[1]!, rgba[2]!, rgba[3]!)
          }
        })
        .catch(() => {})
    })
  }

  const applyThemeAppearance = () => {
    if (typeof document !== 'undefined') {
      document.documentElement.classList.toggle('dark', isDark.value)
      for (const element of [
        document.documentElement,
        document.body,
        document.getElementById('app'),
      ]) {
        element?.style.removeProperty('background-color')
      }
      if (!colorsObserver && typeof MutationObserver !== 'undefined') {
        colorsObserver = new MutationObserver((mutations) => {
          if (
            mutations.some(
              (mutation) =>
                (mutation.target instanceof Element
                  ? mutation.target
                  : mutation.target.parentElement
                )?.closest('#nuxt-ui-colors') ||
                Array.from(mutation.addedNodes).some(
                  (node) => (node as Element).id === 'nuxt-ui-colors',
                ),
            )
          )
            scheduleAppearanceRefresh()
        })
        colorsObserver.observe(document.head, {
          childList: true,
          subtree: true,
          characterData: true,
        })
      }
    }
    scheduleAppearanceRefresh()
  }

  function applyThemeColorState(value: unknown) {
    if (disposed) return
    const state = parseThemeColorState(value)
    if (!state || state.revision <= colorRevision) return
    colorRevision = state.revision
    previewSequence =
      state.sessionID === previewSession
        ? Math.max(previewSequence, state.sequence)
        : state.sequence
    previewSession = state.sessionID
    primaryColor.value = state.primaryColor
    neutralColor.value = state.neutralColor
    appConfig.ui.colors.primary = state.primaryColor
    appConfig.ui.colors.neutral = state.neutralColor
    applyThemeAppearance()
  }

  async function loadThemeColors() {
    if (disposed) return
    offThemeColorsChanged ??= Events.On(THEME_COLORS_CHANGED_EVENT, (event) => {
      applyThemeColorState(event.data)
    })
    applyThemeColorState(await GetThemeColorState())
  }

  function previewThemeColors(primary: string, neutral: string): Promise<void> {
    const run = async () => {
      if (!previewSession) await loadThemeColors()
      if (disposed) return
      applyThemeColorState(
        await PreviewThemeColors({
          sessionID: previewSession,
          sequence: ++previewSequence,
          primaryColor: primary,
          neutralColor: neutral,
        }),
      )
    }
    const promise = run().finally(() => pendingPreviews.delete(promise))
    pendingPreviews.add(promise)
    return promise
  }

  async function flushThemeColorPreview() {
    await Promise.allSettled([...pendingPreviews])
  }

  const initializeTheme = (configuredMode?: string) => {
    if (configuredMode && ['auto', 'light', 'dark'].includes(configuredMode)) {
      themeMode.value = configuredMode as ThemeMode
    }

    if (typeof window === 'undefined') {
      applyThemeAppearance()
      return
    }

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    systemPrefersDark.value = mediaQuery.matches

    stopSystemPreference?.()
    const onChange = (e: MediaQueryListEvent) => {
      systemPrefersDark.value = e.matches
      if (themeMode.value === 'auto') {
        applyThemeAppearance()
      }
    }
    mediaQuery.addEventListener('change', onChange)
    stopSystemPreference = () => mediaQuery.removeEventListener('change', onChange)

    applyThemeAppearance()
  }

  // Set theme mode
  const setThemeMode = (mode: ThemeMode) => {
    themeMode.value = mode
    applyThemeAppearance()
  }

  const cycleTheme = () => {
    const modes: ThemeMode[] = ['auto', 'light', 'dark']
    const currentIndex = modes.indexOf(themeMode.value)
    const nextIndex = (currentIndex + 1) % modes.length
    setThemeMode(modes[nextIndex] as ThemeMode)
  }

  const themeModeLabel = computed(() => {
    switch (themeMode.value) {
      case 'auto':
        return 'Auto'
      case 'light':
        return 'Light'
      case 'dark':
        return 'Dark'
      default:
        return 'Auto'
    }
  })

  function cleanup() {
    disposed = true
    stopSystemPreference?.()
    offThemeColorsChanged?.()
    offThemeColorsChanged = undefined
    colorsObserver?.disconnect()
    if (typeof document !== 'undefined') cancelAnimationFrame(backgroundFrame)
  }
  onScopeDispose(cleanup)

  return {
    themeMode,
    isDark,
    systemPrefersDark,
    primaryColor,
    neutralColor,
    appearanceRevision,
    themeModeLabel,
    initializeTheme,
    setThemeMode,
    cycleTheme,
    applyThemeColorState,
    loadThemeColors,
    previewThemeColors,
    flushThemeColorPreview,
    cleanup,
  }
})
