<script setup lang="ts">
import WindowRightResizeHitArea from '@/components/common/WindowRightResizeHitArea.vue'
import TitleBar from '@/components/common/TitleBar.vue'
import { computed, onMounted, ref } from 'vue'
import { System } from '@wailsio/runtime'
import { inferPlatformFromUserAgent } from '@/runtime/platform'
import { useSettingStore } from '@/stores/setting'

const settingStore = useSettingStore()
const platform = ref<string>(inferPlatformFromUserAgent())
const shouldShowResizeHitArea = computed(
  () =>
    platform.value === 'windows' ||
    (settingStore.usesCustomWindowFrame && platform.value === 'linux'),
)

const contentStyle = computed(() => ({
  height: settingStore.usesCustomWindowFrame ? 'calc(100vh - 38px)' : '100vh',
  overflow: 'hidden',
  display: 'flex',
}))

onMounted(async () => {
  try {
    const environment = await System.Environment()
    platform.value = environment.OS
  } catch {
    // Retain the user-agent platform when the desktop environment lookup fails.
  }
})
</script>

<template>
  <div
    class="flex h-screen min-h-0 w-screen flex-col overflow-hidden bg-app-shell [--default-bg-color:var(--app-shell-bg)]"
  >
    <TitleBar v-if="settingStore.usesCustomWindowFrame" />
    <div data-window-content class="relative min-h-0 min-w-0 flex-1" :style="contentStyle">
      <div data-window-content-host class="flex min-h-0 min-w-0 flex-1">
        <router-view v-slot="{ Component }">
          <component :is="Component" style="height: 100%; width: 100%; flex: 1" />
        </router-view>
      </div>
      <WindowRightResizeHitArea :active="shouldShowResizeHitArea" />
    </div>
  </div>
</template>
