<script setup lang="ts">
import { computed, defineAsyncComponent, reactive, watch } from 'vue'
import { useWorkbenchStore } from '@/stores/workbench'
import TrafficWorkspaceSurface from '@/components/traffic-workspace/TrafficWorkspaceSurface.vue'

const workbenchStore = useWorkbenchStore()

const MemStatsSurface = defineAsyncComponent(() => import('@/components/memstats/MemStatsSurface.vue'))
const PythonPluginSurface = defineAsyncComponent(
  () => import('@/components/python-plugins/PythonPluginSurface.vue'),
)

const mountedAuxiliaryViews = reactive({
  memstats: false,
  pythonPlugins: false,
})

watch(
  () => workbenchStore.activeContent,
  (activeContent) => {
    if (
      activeContent === 'memstats' ||
      activeContent === 'pythonPlugins'
    ) {
      mountedAuxiliaryViews[activeContent] = true
    }
  },
  { immediate: true },
)

const isTrafficActive = computed(() => workbenchStore.activeContent === 'traffic')
const isMemStatsActive = computed(() => workbenchStore.activeContent === 'memstats')
const isPythonPluginsActive = computed(
  () => workbenchStore.activeContent === 'pythonPlugins',
)
</script>

<template>
  <div class="relative h-full w-full flex-1">
    <TrafficWorkspaceSurface v-show="isTrafficActive" class="h-full w-full flex-1" />
    <MemStatsSurface
      v-if="mountedAuxiliaryViews.memstats"
      v-show="isMemStatsActive"
      class="h-full w-full flex-1"
    />
    <PythonPluginSurface
      v-if="mountedAuxiliaryViews.pythonPlugins"
      v-show="isPythonPluginsActive"
      class="h-full w-full flex-1"
    />
  </div>
</template>
