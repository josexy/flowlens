<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import { useI18n } from 'vue-i18n'
import { useWorkbenchStore } from '@/stores/workbench'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import TrafficWorkspaceSurface from '@/components/traffic-workspace/TrafficWorkspaceSurface.vue'

const workbenchStore = useWorkbenchStore()
const workspaceStore = useTrafficWorkspaceStore()
const { t } = useI18n()
const host = ref<HTMLElement>()
const workspaceWidth = ref(0)
const helpWidth = ref(420)
const helpTab = ref('quickstart')
const helpQuery = ref('')
const helpDocked = computed(() => workspaceWidth.value >= 1100)
const helpVisible = computed(() => workbenchStore.pythonHelpScope !== null)
let resizeObserver: ResizeObserver | undefined
let resizeFrame: number | undefined
let helpOpener: HTMLElement | undefined

const PythonPluginHelpPanel = defineAsyncComponent(
  () => import('@/components/python-plugins/PythonPluginHelpPanel.vue'),
)

onMounted(() => {
  resizeObserver = new ResizeObserver(([entry]) => {
    const width = entry?.contentRect.width ?? 0
    if (resizeFrame !== undefined) cancelAnimationFrame(resizeFrame)
    // Switch layouts after observer delivery so mounting panels cannot feed back into it.
    resizeFrame = requestAnimationFrame(() => {
      resizeFrame = undefined
      workspaceWidth.value = width
    })
  })
  if (host.value) resizeObserver.observe(host.value)
})
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  if (resizeFrame !== undefined) cancelAnimationFrame(resizeFrame)
})

watch(
  () => workbenchStore.pythonHelpScope,
  (scope, previous) => {
    if (scope && !previous) {
      helpOpener =
        document.activeElement instanceof HTMLElement ? document.activeElement : undefined
      helpTab.value = 'quickstart'
      helpQuery.value = ''
    }
  },
)
watch(
  () => [workbenchStore.activeContent, workbenchStore.activeSection],
  () => workbenchStore.closePythonHelp(),
)
watch(
  () => workspaceStore.activeTabKey,
  () => workbenchStore.closePythonHelp(),
)

function closeHelp() {
  workbenchStore.closePythonHelp()
  void nextTick(() => helpOpener?.isConnected && helpOpener.focus({ preventScroll: true }))
}

function onDrawerOpenChange(open: boolean) {
  if (!open && !helpDocked.value) closeHelp()
}

const MemStatsSurface = defineAsyncComponent(
  () => import('@/components/memstats/MemStatsSurface.vue'),
)
const PythonPluginSurface = defineAsyncComponent(
  () => import('@/components/python-plugins/PythonPluginSurface.vue'),
)

const RewriteRulesSurface = defineAsyncComponent(
  () => import('@/components/rewrite-rules/RewriteRulesSurface.vue'),
)

const mountedAuxiliaryViews = reactive({
  memstats: false,
  pythonPlugins: false,
  rewriteRules: false,
})

watch(
  () => workbenchStore.activeContent,
  (activeContent) => {
    if (
      activeContent === 'memstats' ||
      activeContent === 'pythonPlugins' ||
      activeContent === 'rewriteRules'
    ) {
      mountedAuxiliaryViews[activeContent] = true
    }
  },
  { immediate: true },
)

const isTrafficActive = computed(() => workbenchStore.activeContent === 'traffic')
const isMemStatsActive = computed(() => workbenchStore.activeContent === 'memstats')
const isPythonPluginsActive = computed(() => workbenchStore.activeContent === 'pythonPlugins')
</script>

<template>
  <div ref="host" class="relative h-full min-w-0 flex-1">
    <SplitterGroup direction="horizontal" class="h-full w-full">
      <SplitterPanel
        :order="0"
        :min-size="helpVisible && helpDocked ? 640 : 0"
        size-unit="px"
        class="min-w-0"
      >
        <TrafficWorkspaceSurface v-show="isTrafficActive" class="h-full w-full flex-1" />
        <MemStatsSurface
          v-if="mountedAuxiliaryViews.memstats"
          v-show="isMemStatsActive"
          class="h-full w-full flex-1"
        />
        <RewriteRulesSurface
          v-if="mountedAuxiliaryViews.rewriteRules"
          v-show="workbenchStore.activeContent === 'rewriteRules'"
          class="h-full w-full flex-1"
        />
        <PythonPluginSurface
          v-if="mountedAuxiliaryViews.pythonPlugins"
          v-show="isPythonPluginsActive"
          class="h-full w-full flex-1"
        />
      </SplitterPanel>
      <AppSplitterResizeHandle v-if="helpVisible && helpDocked" />
      <SplitterPanel
        v-if="helpVisible && helpDocked"
        :order="1"
        size-unit="px"
        :default-size="helpWidth"
        :min-size="360"
        :max-size="Math.min(560, workspaceWidth - 640)"
        class="min-w-0"
        @resize="helpWidth = $event"
      >
        <PythonPluginHelpPanel
          v-if="workbenchStore.pythonHelpScope"
          v-model:tab="helpTab"
          v-model:query="helpQuery"
          :scope="workbenchStore.pythonHelpScope"
          @close="closeHelp"
        />
      </SplitterPanel>
    </SplitterGroup>
    <USlideover
      :open="helpVisible && !helpDocked"
      :title="t('python_plugin_help.title')"
      side="right"
      :ui="{ content: 'w-full max-w-105' }"
      @update:open="onDrawerOpenChange"
    >
      <template #content>
        <PythonPluginHelpPanel
          v-if="workbenchStore.pythonHelpScope && !helpDocked"
          v-model:tab="helpTab"
          v-model:query="helpQuery"
          :scope="workbenchStore.pythonHelpScope"
          @close="closeHelp"
        />
      </template>
    </USlideover>
  </div>
</template>
