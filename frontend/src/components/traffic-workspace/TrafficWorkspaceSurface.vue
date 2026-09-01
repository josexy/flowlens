<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import WorkspaceTabBar from '@/components/traffic-workspace/WorkspaceTabBar.vue'
import WorkspaceContent from '@/components/traffic-workspace/WorkspaceContent.vue'
import ProxyToolbar from '@/components/traffic/ProxyToolbar.vue'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { useProxyStore } from '@/stores/proxy'
import { useTrafficStore } from '@/stores/traffic'
import { useHistoryStore } from '@/stores/history'
import { useCategoryContextStore } from '@/stores/categoryContext'

defineOptions({
  name: 'TrafficWorkspaceSurface',
})

const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const proxyStore = useProxyStore()
const trafficStore = useTrafficStore()
const historyStore = useHistoryStore()
const categoryContextStore = useCategoryContextStore()

const activeTab = computed(() => workspaceStore.activeTab)
const captureInitialized = ref(false)
const documentVisible = ref(document.visibilityState !== 'hidden')
let captureInitPromise: Promise<void> | null = null
let proxyInitialized = false
let trafficInitialized = false
let trafficLifecycleGeneration: number | null = null
let disposed = false

onMounted(() => {
  workspaceStore.initializeRuntimeEvents()
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

function handleVisibilityChange() {
  documentVisible.value = document.visibilityState !== 'hidden'
}

async function ensureCaptureReady() {
  if (captureInitialized.value) {
    return
  }
  if (captureInitPromise) {
    await captureInitPromise
    return
  }
  captureInitPromise = (async () => {
    try {
      await proxyStore.initialize()
      proxyInitialized = true
      if (disposed) {
        cleanupCaptureStores()
        return
      }
      trafficLifecycleGeneration = await trafficStore.initialize()
      trafficInitialized = true
      if (disposed) {
        cleanupCaptureStores()
        return
      }
      captureInitialized.value = true
    } catch (error) {
      cleanupCaptureStores()
      throw error
    }
  })()
  try {
    await captureInitPromise
  } finally {
    captureInitPromise = null
  }
}

watch(
  () => [activeTab.value.type, activeTab.value.key, activeTab.value.historyKey] as const,
  async ([tabType, , historyKey]) => {
    await ensureCaptureReady()
    if (!captureInitialized.value) return
    if (tabType === 'capture') {
      categoryContextStore.setActiveCaptureContext()
      return
    }

    if (tabType === 'history' && historyKey) {
      await historyStore.selectHistory(historyKey)
      categoryContextStore.setActiveHistoryContext(historyKey, activeTab.value.title)
    }
  },
  { immediate: true },
)

watch(
  [
    () => workbenchStore.activeContent,
    () => activeTab.value.type,
    documentVisible,
  ],
  ([activeContent, tabType, isDocumentVisible]) => {
    trafficStore.setTrafficSurfaceActive(
      activeContent === 'traffic' && tabType === 'capture' && isDocumentVisible,
    )
  },
  { immediate: true },
)

watch(
  () => historyStore.metadataList,
  (list) => {
    const historyKeys = new Set(list.map((item) => item.key))
    workspaceStore.pruneHistoryTabs(historyKeys)

    const selectedCaptureItem = workbenchStore.sectionSelections.capture
    if (
      selectedCaptureItem?.startsWith('history:') &&
      !historyKeys.has(selectedCaptureItem.slice('history:'.length))
    ) {
      workbenchStore.sectionSelections.capture = 'capture'
    }
  },
  { deep: false },
)

onUnmounted(() => {
  disposed = true
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  trafficStore.setTrafficSurfaceActive(false)
  workspaceStore.cleanupRuntimeEvents()
  cleanupCaptureStores()
})

function cleanupCaptureStores() {
  if (proxyInitialized) {
    proxyStore.cleanup()
    proxyInitialized = false
  }
  if (trafficInitialized) {
    trafficStore.cleanup(trafficLifecycleGeneration ?? undefined)
    trafficInitialized = false
    trafficLifecycleGeneration = null
  }
  captureInitialized.value = false
}
</script>

<template>
  <div class="flex min-h-0 w-full flex-1 overflow-hidden bg-app-content">
    <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-app-content">
      <ProxyToolbar />
      <WorkspaceTabBar />
      <WorkspaceContent />
    </div>
  </div>
</template>
