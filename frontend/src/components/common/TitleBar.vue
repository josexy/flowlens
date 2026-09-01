<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import WindowControlIcon from './WindowControlIcon.vue'
import { System, Window } from '@wailsio/runtime'
import { useI18n } from 'vue-i18n'
import iconUrl from '@/assets/images/appicon.png'
import { inferPlatformFromUserAgent } from '@/runtime/platform'
import { useSettingStore } from '@/stores/setting'

const { t } = useI18n()
const settingStore = useSettingStore()
const isMaximized = ref(false)
const platform = ref<string>(inferPlatformFromUserAgent())

const isMacOS = computed(() => platform.value === 'darwin')
const isWindows = computed(() => platform.value === 'windows')
const isLinux = computed(() => platform.value === 'linux')
const showCustomControls = computed(
  () => settingStore.usesCustomWindowFrame && (isWindows.value || isLinux.value),
)
const titlebarButtonClass = [
  'relative',
  'z-1',
  'flex',
  'h-full',
  'w-10.5',
  'cursor-default!',
  'items-center',
  'justify-center',
  'p-0',
  'leading-none',
  'text-(--titlebar-control-color)',
  'hover:bg-(--titlebar-control-hover-bg)',
  'hover:text-(--titlebar-control-color-hover)',
  'active:bg-(--titlebar-control-hover-bg)',
  'active:text-(--titlebar-control-color-hover)',
].join(' ')
const closeButtonClass = computed(() => {
  if (isWindows.value) {
    return 'hover:bg-[#e81123]! hover:text-white active:bg-[#e81123]! active:text-white'
  }

  if (isLinux.value) {
    return 'hover:bg-[#cc0000]! hover:text-white active:bg-[#cc0000]! active:text-white'
  }

  return ''
})

const syncMaximizedState = async () => {
  if (!showCustomControls.value) {
    return
  }
  isMaximized.value = await Window.IsMaximised()
}

onMounted(async () => {
  try {
    const env = await System.Environment()
    platform.value = env.OS
  } catch (e) {
    console.error('Failed to get platform info:', e)
  }

  if (showCustomControls.value) {
    await syncMaximizedState()
    window.addEventListener('resize', syncMaximizedState)
  }
})

onUnmounted(() => {
  window.removeEventListener('resize', syncMaximizedState)
})

const handleMinimize = () => {
  if (!showCustomControls.value) {
    return
  }
  void Window.Minimise()
}

const handleMaximize = async () => {
  if (!showCustomControls.value) {
    return
  }
  await Window.ToggleMaximise()
  await syncMaximizedState()
}

const handleClose = () => {
  if (!showCustomControls.value) {
    return
  }
  void Window.Close()
}

const handleTitlebarDoubleClick = () => {
  if (isMacOS.value) {
    void Window.ToggleMaximise()
    return
  }

  if (!showCustomControls.value) {
    return
  }
  handleMaximize()
}
</script>

<template>
  <div
    class="relative z-(--z-sticky,20) flex max-h-9.5 min-h-9.5 shrink-0 select-none items-center justify-between bg-(--default-bg-color) [--titlebar-control-color:color-mix(in_srgb,var(--app-text-primary)_90%,transparent)] [--titlebar-control-color-hover:var(--app-text-primary)] [--titlebar-control-hover-bg:rgba(128,128,128,0.15)] [--titlebar-controls-width:126px] [--titlebar-resize-right-inset:7px] [--titlebar-resize-top-inset:5px] [backdrop-filter:blur(100px)] [-webkit-backdrop-filter:blur(100px)] [border-bottom:1px_solid_var(--app-border-strong-color)]"
    :class="isMacOS ? 'h-full' : 'h-9.5'"
    style="--wails-draggable: drag"
    @dblclick="handleTitlebarDoubleClick"
  >
    <div
      class="flex flex-1 cursor-default select-none items-center gap-2.5"
      :class="isMacOS ? 'justify-center px-22' : 'pl-3'"
      style="--wails-draggable: drag"
    >
      <img
        class="size-4.5 object-contain filter-[drop-shadow(0_2px_4px_rgba(0,0,0,0.1))]"
        :src="iconUrl"
        alt="App icon"
      />
      <div
        class="cursor-default select-none text-sm text-app-text"
        :class="isMacOS ? 'font-medium' : 'font-semibold'"
      >
        {{ t('app.title') }}
      </div>
    </div>
    <div v-if="showCustomControls" class="relative flex h-full items-center" @dblclick.stop>
      <div
        :class="titlebarButtonClass"
        style="--wails-draggable: no-drag"
        @click="handleMinimize"
      >
        <WindowControlIcon name="minimize" />
      </div>
      <div
        :class="titlebarButtonClass"
        style="--wails-draggable: no-drag"
        @click="handleMaximize"
      >
        <WindowControlIcon :name="isMaximized ? 'restore' : 'maximize'" />
      </div>
      <div
        :class="[titlebarButtonClass, closeButtonClass]"
        style="--wails-draggable: no-drag"
        @click="handleClose"
      >
        <WindowControlIcon name="close" />
      </div>
    </div>
    <template v-if="showCustomControls">
      <div
        class="absolute right-0 top-0 z-3 h-(--titlebar-resize-top-inset) w-(--titlebar-controls-width)"
        style="--wails-draggable: drag"
      ></div>
      <div
        class="absolute right-0 top-0 z-3 h-full w-(--titlebar-resize-right-inset)"
        style="--wails-draggable: drag"
      ></div>
    </template>
  </div>
</template>
