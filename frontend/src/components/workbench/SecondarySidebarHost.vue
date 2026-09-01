<script setup lang="ts">
import { reactive, watch } from 'vue'
import WorkspaceSidebar from '@/components/traffic-workspace/WorkspaceSidebar.vue'
import CategoryWorkspaceSidebar from '@/components/workbench/CategoryWorkspaceSidebar.vue'
import ApiCollectionWorkspaceSidebar from '@/components/workbench/ApiCollectionWorkspaceSidebar.vue'
import MemStatsWorkspaceSidebar from '@/components/workbench/MemStatsWorkspaceSidebar.vue'
import PythonPluginWorkspaceSidebar from '@/components/python-plugins/PythonPluginWorkspaceSidebar.vue'
import { useWorkbenchStore } from '@/stores/workbench'
import type { WorkspaceSection } from '@/stores/workbench'

const workbenchStore = useWorkbenchStore()

const mountedSidebars = reactive<Record<WorkspaceSection, boolean>>({
  capture: false,
  category: false,
  apiCollection: false,
  pythonPlugins: false,
  memstats: false,
})

watch(
  () => workbenchStore.activeSection,
  (section) => {
    mountedSidebars[section] = true
  },
  { immediate: true },
)
</script>

<template>
  <div class="h-full w-full">
    <WorkspaceSidebar
      v-if="mountedSidebars.capture"
      v-show="workbenchStore.activeSection === 'capture'"
      class="h-full w-full"
    />
    <CategoryWorkspaceSidebar
      v-if="mountedSidebars.category"
      v-show="workbenchStore.activeSection === 'category'"
      class="h-full w-full"
    />
    <ApiCollectionWorkspaceSidebar
      v-if="mountedSidebars.apiCollection"
      v-show="workbenchStore.activeSection === 'apiCollection'"
      class="h-full w-full"
    />
    <PythonPluginWorkspaceSidebar
      v-if="mountedSidebars.pythonPlugins"
      v-show="workbenchStore.activeSection === 'pythonPlugins'"
      class="h-full w-full"
    />
    <MemStatsWorkspaceSidebar
      v-if="mountedSidebars.memstats"
      v-show="workbenchStore.activeSection === 'memstats'"
      class="h-full w-full"
    />
  </div>
</template>
