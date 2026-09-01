import { defineStore } from 'pinia'
import { shallowRef, computed } from 'vue'
import { Window } from '@wailsio/runtime'

export type ThemeMode = 'auto' | 'light' | 'dark'

const LIGHT_WINDOW_BACKGROUND = '#f9faf9'
const DARK_WINDOW_BACKGROUND = '#1d2229'
const LIGHT_BACKGROUND_RGB = { r: 249, g: 250, b: 249, a: 255 } as const
const DARK_BACKGROUND_RGB = { r: 29, g: 34, b: 41, a: 255 } as const

export const useThemeStore = defineStore('theme', () => {
  const themeMode = shallowRef<ThemeMode>('light')
  const systemPrefersDark = shallowRef(false)

  const isDark = computed(() => {
    if (themeMode.value === 'auto') {
      return systemPrefersDark.value
    }
    return themeMode.value === 'dark'
  })

  const applyWindowAndRootBackground = () => {
    if (typeof document === 'undefined') return

    const useDark = isDark.value
    const rgb = useDark ? DARK_BACKGROUND_RGB : LIGHT_BACKGROUND_RGB
    const backgroundColor = useDark ? DARK_WINDOW_BACKGROUND : LIGHT_WINDOW_BACKGROUND

    document.documentElement.classList.toggle('dark', useDark)
    document.documentElement.style.backgroundColor = backgroundColor
    document.body.style.backgroundColor = backgroundColor

    const app = document.getElementById('app')
    if (app) {
      app.style.backgroundColor = backgroundColor
    }

    void Window.SetBackgroundColour(rgb.r, rgb.g, rgb.b, rgb.a).catch(() => {})
  }

  const applyThemeAppearance = () => {
    applyWindowAndRootBackground()
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

    mediaQuery.addEventListener('change', (e) => {
      systemPrefersDark.value = e.matches
      if (themeMode.value === 'auto') {
        applyThemeAppearance()
      }
    })

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

  return {
    themeMode,
    isDark,
    systemPrefersDark,
    themeModeLabel,
    initializeTheme,
    setThemeMode,
    cycleTheme,
  }
})
