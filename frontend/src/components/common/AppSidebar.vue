<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Events } from '@wailsio/runtime'
import { useThemeStore } from '@/stores/theme'
import type { ThemeMode } from '@/stores/theme'
import { useSettingStore, type AppLanguage } from '@/stores/setting'
import { useWorkbenchStore, type WorkspaceSection } from '@/stores/workbench'
import { OPEN_SETTINGS_WINDOW_EVENT, PREFERENCES_CHANGED_EVENT } from '@/runtime/appEvents'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'

defineOptions({
  name: 'AppSidebar',
})

const { t, locale } = useI18n()
const themeStore = useThemeStore()
const settingStore = useSettingStore()
const workbenchStore = useWorkbenchStore()
const openSettingsShortcutKbds = useShortcutKbds('app.openSettings')
const captureShortcutKbds = useShortcutKbds('workbench.capture')
const categoryShortcutKbds = useShortcutKbds('workbench.category')
const apiCollectionShortcutKbds = useShortcutKbds('workbench.apiCollection')
const pythonPluginsShortcutKbds = useShortcutKbds('workbench.pythonPlugins')
const memstatsShortcutKbds = useShortcutKbds('workbench.memstats')
const sidebarShortcutKbds = computed(() => ({
  settings: openSettingsShortcutKbds.value,
  capture: captureShortcutKbds.value,
  category: categoryShortcutKbds.value,
  apiCollection: apiCollectionShortcutKbds.value,
  pythonPlugins: pythonPluginsShortcutKbds.value,
  memstats: memstatsShortcutKbds.value,
}))

const menuItems = computed(() => [
  { icon: 'i-lucide-radio', label: t('menu.capture'), name: 'capture' as WorkspaceSection },
  {
    icon: 'i-lucide-folder-open',
    label: t('menu.category'),
    name: 'category' as WorkspaceSection,
  },
  {
    icon: 'i-lucide-git-branch',
    label: t('menu.api_collection'),
    name: 'apiCollection' as WorkspaceSection,
  },
  {
    icon: 'i-lucide-file-code-2',
    label: t('menu.python_plugins'),
    name: 'pythonPlugins' as WorkspaceSection,
  },
  { icon: 'i-lucide-activity', label: t('menu.memstats'), name: 'memstats' as WorkspaceSection },
])

const themeIcon = computed(() => {
  switch (themeStore.themeMode) {
    case 'light':
      return 'i-lucide-sun'
    case 'dark':
      return 'i-lucide-moon'
    case 'auto':
    default:
      return 'i-lucide-monitor-smartphone'
  }
})

const themeLabel = computed(() => {
  return t('theme.tooltip', { mode: t(`theme.${themeStore.themeMode}`) })
})

const nextThemeMode = (mode: ThemeMode): ThemeMode => {
  const modes: ThemeMode[] = ['auto', 'light', 'dark']
  const currentIndex = modes.indexOf(mode)
  return modes[(currentIndex + 1) % modes.length] as ThemeMode
}

async function broadcastPreferencesChanged(payload: {
  themeMode: ThemeMode
  language: AppLanguage
}) {
  try {
    await Events.Emit(PREFERENCES_CHANGED_EVENT, payload)
  } catch (e) {
    console.error('Broadcast preference change failed', e)
  }
}

const toggleTheme = async () => {
  const previous = themeStore.themeMode
  const next = nextThemeMode(previous)
  themeStore.setThemeMode(next)
  try {
    await settingStore.setThemeModePreference(next)
    await broadcastPreferencesChanged({
      themeMode: next,
      language: settingStore.language,
    })
  } catch (e) {
    console.error('Save theme preference failed', e)
    themeStore.setThemeMode(previous)
  }
}

const toggleLanguage = async () => {
  const previous = locale.value as AppLanguage
  const next: AppLanguage = previous === 'zh' ? 'en' : 'zh'
  locale.value = next
  try {
    await settingStore.setLanguagePreference(next)
    await broadcastPreferencesChanged({
      themeMode: settingStore.themeMode,
      language: next,
    })
  } catch (e) {
    console.error('Save language preference failed', e)
    locale.value = previous
  }
}

function activateWorkspaceSection(section: WorkspaceSection) {
  workbenchStore.activateSection(section)
}

async function openSettings() {
  try {
    await Events.Emit(OPEN_SETTINGS_WINDOW_EVENT)
  } catch (e) {
    console.error('Open settings window failed', e)
  }
}

const languageLabel = computed(() => t('language.switch'))

const sidebarButtonClass = [
  'relative',
  'flex',
  'aspect-square',
  'w-full',
  'items-center',
  'justify-center',
  'rounded-lg',
  'border-0',
  'p-0!',
  'text-app-text-muted',
  'shadow-none',
  'transition-[color,background-color]',
  'duration-[220ms]',
  'before:absolute',
  'before:left-0.75',
  'before:top-2.25',
  'before:bottom-2.25',
  'before:w-0.5',
  'before:origin-center',
  'before:scale-y-[0.55]',
  'before:rounded-full',
  'before:bg-app-accent',
  'before:opacity-0',
  'before:transition-[opacity,transform]',
  'before:duration-[220ms]',
  "before:content-['']",
  'hover:bg-app-control',
  'hover:text-app-text',
  'hover:before:scale-y-[0.72]',
  'hover:before:opacity-[0.38]',
  'focus-visible:outline-2',
  'focus-visible:outline-offset-1',
  'focus-visible:outline-[color-mix(in_srgb,var(--app-accent-color)_42%,transparent)]',
].join(' ')
const sidebarButtonActiveClass = [
  'bg-[color-mix(in_srgb,var(--app-accent-color)_12%,var(--app-nav-bg))]',
  'text-app-accent',
  'shadow-none',
  'before:scale-y-100',
  'before:opacity-100',
  'hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,var(--app-nav-bg))]',
  'hover:text-app-accent',
  'hover:before:scale-y-100',
  'hover:before:opacity-100',
].join(' ')
const sidebarUtilityButtonClass = [
  'before:hidden',
  'hover:bg-app-control',
  'hover:text-app-text',
  'hover:shadow-none',
].join(' ')
const sidebarIconClass = 'size-4.5 shrink-0 transition-colors duration-[220ms]'

function isTopSectionActive(section: WorkspaceSection) {
  return workbenchStore.activeSection === section
}

const offShortcutHandlers = [
  registerShortcutHandler({
    commandId: 'app.openSettings',
    when: () => true,
    run: () => openSettings(),
  }),
  ...(['capture', 'category', 'apiCollection', 'pythonPlugins', 'memstats'] as const).map((section) =>
    registerShortcutHandler({
      commandId: `workbench.${section}`,
      when: () => true,
      run: () => activateWorkspaceSection(section),
    }),
  ),
]

onBeforeUnmount(() => {
  offShortcutHandlers.forEach((off) => off())
})
</script>

<template>
  <div
    class="sidebar relative z-(--z-dropdown,10) flex w-13 shrink-0 flex-col bg-app-nav pt-2.5 pb-2 [border-right:1px_solid_var(--app-border-strong-color)] shadow-[8px_0_20px_-18px_rgba(15,23,42,0.32)]"
  >
    <div class="flex flex-1 flex-col gap-0.5 px-1.5">
      <UTooltip
        v-for="item in menuItems"
        :key="item.name"
        :text="item.label"
        :kbds="sidebarShortcutKbds[item.name]"
        :content="{ side: 'right' }"
      >
        <UButton
          :class="[
            sidebarButtonClass,
            isTopSectionActive(item.name) ? sidebarButtonActiveClass : '',
          ]"
          color="neutral"
          variant="ghost"
          :aria-label="item.label"
          @click="activateWorkspaceSection(item.name)"
        >
          <UIcon :name="item.icon" :class="sidebarIconClass" aria-hidden="true" />
        </UButton>
      </UTooltip>
    </div>
    <div class="relative mt-2.5 flex flex-col gap-0.5 px-1.5 pt-2.5">
      <!-- Theme Toggle -->
      <UTooltip :text="themeLabel" :content="{ side: 'right' }">
        <UButton
          :class="[sidebarButtonClass, sidebarUtilityButtonClass]"
          color="neutral"
          variant="ghost"
          :aria-label="themeLabel"
          @click="toggleTheme"
        >
          <UIcon :name="themeIcon" :class="sidebarIconClass" aria-hidden="true" />
        </UButton>
      </UTooltip>

      <!-- Language Toggle -->
      <UTooltip :text="languageLabel" :content="{ side: 'right' }">
        <UButton
          :class="[sidebarButtonClass, sidebarUtilityButtonClass]"
          color="neutral"
          variant="ghost"
          :aria-label="languageLabel"
          @click="toggleLanguage"
        >
          <UIcon name="i-lucide-languages" :class="sidebarIconClass" aria-hidden="true" />
        </UButton>
      </UTooltip>

      <!-- Settings -->
      <UTooltip
        :text="t('menu.settings')"
        :kbds="sidebarShortcutKbds.settings"
        :content="{ side: 'right' }"
      >
        <UButton
          :class="[sidebarButtonClass, sidebarUtilityButtonClass]"
          color="neutral"
          variant="ghost"
          :aria-label="t('menu.settings')"
          @click="openSettings"
        >
          <UIcon name="i-lucide-settings" :class="sidebarIconClass" aria-hidden="true" />
        </UButton>
      </UTooltip>
    </div>
  </div>
</template>
