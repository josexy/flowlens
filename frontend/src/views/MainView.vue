<script setup lang="ts">
import { ref, watch } from 'vue'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import AppSidebar from '@/components/common/AppSidebar.vue'
import StatusBar from '@/components/common/StatusBar.vue'
import ContentHost from '@/components/workbench/ContentHost.vue'
import SecondarySidebarHost from '@/components/workbench/SecondarySidebarHost.vue'
import { useWorkbenchStore } from '@/stores/workbench'

const workbenchStore = useWorkbenchStore()
const secondaryPanelRef = ref<InstanceType<typeof SplitterPanel> | null>(null)
const defaultSecondarySize = 20

// Keep the reka panel collapse state in sync with the store. The panel is the
// source of truth for its own size (reka manages layout internally); the store
// only tracks visibility, so we push visibility changes down via collapse()/
// expand() and lift drag-driven collapse/expand back up through the events.
watch(
  () => workbenchStore.secondarySidebarVisible,
  (visible) => {
    const panel = secondaryPanelRef.value
    if (!panel) {
      return
    }
    if (visible && panel.isCollapsed) {
      panel.expand()
    } else if (!visible && panel.isExpanded) {
      panel.collapse()
    }
  },
)
</script>

<template>
  <div class="relative flex h-full max-h-full w-full flex-col overflow-hidden bg-app-window">
    <div class="flex min-h-0 flex-1 overflow-hidden">
      <AppSidebar />
      <div class="flex h-full min-w-0 flex-1 overflow-hidden">
        <SplitterGroup
          direction="horizontal"
          class="flex h-full min-w-0 flex-1 overflow-hidden bg-transparent"
        >
          <SplitterPanel
            ref="secondaryPanelRef"
            collapsible
            :collapsed-size="0"
            :default-size="defaultSecondarySize"
            :min-size="15"
            :max-size="42"
            class="flex min-w-0 flex-col overflow-hidden"
            @collapse="workbenchStore.hideSecondarySidebar()"
            @expand="workbenchStore.showSecondarySidebar()"
          >
            <SecondarySidebarHost
              class="h-full w-full min-w-0 overflow-hidden bg-app-sidebar shadow-[8px_0_22px_-24px_rgba(15,23,42,0.55)]"
            />
          </SplitterPanel>
          <AppSplitterResizeHandle
            v-show="workbenchStore.secondarySidebarVisible"
          />
          <SplitterPanel
            :default-size="100 - defaultSecondarySize"
            :min-size="30"
            class="relative flex h-full min-w-0 flex-1 flex-col overflow-hidden bg-app-content before:pointer-events-none before:absolute before:inset-x-0 before:top-0 before:z-0 before:h-50 before:bg-app-accent-soft before:opacity-(--app-content-glow-opacity,0.05) before:content-['']"
          >
            <ContentHost />
          </SplitterPanel>
        </SplitterGroup>
      </div>
    </div>
    <StatusBar />
  </div>
</template>
