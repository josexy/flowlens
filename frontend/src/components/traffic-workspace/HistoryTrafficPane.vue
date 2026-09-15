<script setup lang="ts">
import { computed, provide, ref } from 'vue'
import { useHARImportDrop } from '@/composables/useHARImportDrop'
import { useI18n } from 'vue-i18n'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import AppLoading from '@/components/common/AppLoading.vue'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import FilterBar from '@/components/traffic/FilterBar.vue'
import TrafficTable from '@/components/traffic/TrafficTable.vue'
import DetailPanel from '@/components/traffic/DetailPanel.vue'
import { useHistoryStore } from '@/stores/history'
import { useHistoryTrafficStore } from '@/stores/historyTraffic'
import { useHistoryFilterStore } from '@/stores/historyFilter'
import { TRAFFIC_STORE_KEY, FILTER_STORE_KEY } from '@/types/inject-keys'

const { t } = useI18n()
const historyStore = useHistoryStore()
const { dropActive, dropTarget, onDragEnter, onDragOver, onDragLeave, resetDrop } = useHARImportDrop('history')
const historyTrafficStore = useHistoryTrafficStore()
const historyFilterStore = useHistoryFilterStore()
const detailSplitSize = ref(60)
const isDetailVisible = computed(
  () => !!historyTrafficStore.selectedEntry && historyTrafficStore.showDetailPanel,
)

function handleLayout(sizes: number[]) {
  const nextSize = sizes[0]
  if (!isDetailVisible.value || typeof nextSize !== 'number') {
    return
  }
  detailSplitSize.value = nextSize
}

provide(TRAFFIC_STORE_KEY, historyTrafficStore)
provide(FILTER_STORE_KEY, historyFilterStore)
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
    <template v-if="historyStore.selectedKey">
      <FilterBar context="history" />
      <div
        class="relative min-h-0 flex-1 overflow-hidden bg-app-content"
      >
        <SplitterGroup
          direction="horizontal"
          class="flex h-full bg-transparent"
          @layout="handleLayout"
        >
          <SplitterPanel
            :default-size="isDetailVisible ? detailSplitSize : 100"
            :min-size="isDetailVisible ? 20 : 100"
            class="flex min-h-0 min-w-0 flex-col overflow-hidden! bg-app-panel"
          >
            <div
              class="relative flex h-full min-h-0 min-w-0 flex-col"
              :data-file-drop-target="dropTarget"
              @dragenter="onDragEnter" @dragover="onDragOver" @dragleave="onDragLeave" @drop="resetDrop"
            >
              <TrafficTable />
              <div v-if="dropActive" class="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-2 border-dashed border-primary bg-elevated/90 text-sm font-medium text-primary" role="status">
                {{ t('har_import.drop_hint') }}
              </div>
            </div>
          </SplitterPanel>
          <template v-if="isDetailVisible">
            <AppSplitterResizeHandle />
            <SplitterPanel
              :default-size="100 - detailSplitSize"
              :min-size="10"
              class="flex min-h-0 min-w-0 flex-col overflow-hidden! bg-app-panel"
            >
              <div class="flex h-full min-h-0 min-w-0 flex-col">
                <DetailPanel />
              </div>
            </SplitterPanel>
          </template>
        </SplitterGroup>
        <AppLoading
          v-if="historyStore.loadingHistory"
          fill
          class="pointer-events-auto absolute inset-0 z-20 bg-[color-mix(in_srgb,var(--app-panel-bg)_42%,transparent)]"
        />
      </div>
    </template>
    <div
      v-else
      class="relative flex flex-1 items-center justify-center p-4 text-sm text-app-text-muted"
      :data-file-drop-target="dropTarget"
      @dragenter="onDragEnter" @dragover="onDragOver" @dragleave="onDragLeave" @drop="resetDrop"
      role="status"
    >
      {{ t('history.selectHint') }}
      <div v-if="dropActive" class="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-2 border-dashed border-primary bg-elevated/90 text-sm font-medium text-primary">
        {{ t('har_import.drop_hint') }}
      </div>
    </div>
  </div>
</template>
