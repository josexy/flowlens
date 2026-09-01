<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkbenchStore } from '@/stores/workbench'

const { t } = useI18n()
const workbenchStore = useWorkbenchStore()

const active = computed(() => workbenchStore.sectionSelections.memstats === 'memstats:panel')

function selectMemStats() {
  workbenchStore.selectMemStatsItem()
}
</script>

<template>
  <div class="flex h-full w-full min-w-0 flex-col overflow-hidden bg-app-sidebar">
    <div
      class="flex min-h-10 items-center gap-2 border-l-[3px] px-2.5 py-2 transition-colors duration-150"
      :class="active ? 'border-l-app-accent bg-app-accent-soft' : 'border-l-transparent hover:bg-app-control'"
      @click="selectMemStats"
    >
      <div class="flex shrink-0 items-center" :class="active ? 'text-app-accent opacity-100' : 'text-app-text-muted opacity-70'">
        <UIcon name="i-lucide-activity" class="size-4 shrink-0" />
      </div>
      <div class="flex min-w-0 flex-1 flex-col">
        <span class="truncate text-sm font-semibold text-app-text">{{ t('menu.memstats') }}</span>
      </div>
    </div>
  </div>
</template>
