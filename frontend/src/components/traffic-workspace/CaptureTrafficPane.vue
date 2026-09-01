<script setup lang="ts">
import { computed, provide, ref } from 'vue'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import FilterBar from '@/components/traffic/FilterBar.vue'
import TrafficTable from '@/components/traffic/TrafficTable.vue'
import DetailPanel from '@/components/traffic/DetailPanel.vue'
import { useTrafficStore } from '@/stores/traffic'
import { useFilterStore } from '@/stores/filter'
import { TRAFFIC_STORE_KEY, FILTER_STORE_KEY } from '@/types/inject-keys'

const trafficStore = useTrafficStore()
const filterStore = useFilterStore()
const detailSplitSize = ref(60)
const isDetailVisible = computed(() => !!trafficStore.selectedEntry && trafficStore.showDetailPanel)

function handleLayout(sizes: number[]) {
  const nextSize = sizes[0]
  if (!isDetailVisible.value || typeof nextSize !== 'number') {
    return
  }
  detailSplitSize.value = nextSize
}

provide(TRAFFIC_STORE_KEY, trafficStore)
provide(FILTER_STORE_KEY, filterStore)
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
    <FilterBar context="capture" />
    <div
      class="min-h-0 flex-1 overflow-hidden bg-app-content"
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
          <div class="flex h-full min-h-0 min-w-0 flex-col">
            <TrafficTable />
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
    </div>
  </div>
</template>
