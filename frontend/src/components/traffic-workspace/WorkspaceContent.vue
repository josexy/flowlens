<script setup lang="ts">
import { computed } from 'vue'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import type { WorkspaceTab } from '@/stores/trafficWorkspace'
import type { HttpRequestEditorState, WebSocketClientState } from '@/types/request-editor'
import CaptureTrafficPane from './CaptureTrafficPane.vue'
import HistoryTrafficPane from './HistoryTrafficPane.vue'
import HttpRequestEditorPane from './HttpRequestEditorPane.vue'
import WebSocketClientPane from './WebSocketClientPane.vue'

const workspaceStore = useTrafficWorkspaceStore()

const activeTab = computed(() => workspaceStore.activeTab)
const isCaptureTabActive = computed(() => activeTab.value.type === 'capture')
const isHistoryTabActive = computed(() => activeTab.value.type === 'history')

type HttpRequestTab = WorkspaceTab & {
  type: 'http-request'
  httpRequest: HttpRequestEditorState
}

type WebSocketClientTab = WorkspaceTab & {
  type: 'websocket-client'
  webSocketClient: WebSocketClientState
}

const httpRequestTabs = computed(() =>
  workspaceStore.tabs.filter(
    (tab): tab is HttpRequestTab => tab.type === 'http-request' && Boolean(tab.httpRequest),
  ),
)

const webSocketClientTabs = computed(() =>
  workspaceStore.tabs.filter(
    (tab): tab is WebSocketClientTab => tab.type === 'websocket-client' && Boolean(tab.webSocketClient),
  ),
)

function updateHttpRequestEditorState(tab: HttpRequestTab, value: HttpRequestEditorState) {
  tab.httpRequest = value
}

function updateWebSocketClientState(tab: WebSocketClientTab, value: WebSocketClientState) {
  tab.webSocketClient = value
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
    <CaptureTrafficPane v-show="isCaptureTabActive" />
    <HistoryTrafficPane v-show="isHistoryTabActive" />
    <HttpRequestEditorPane
      v-for="tab in httpRequestTabs"
      v-show="activeTab.key === tab.key"
      :key="tab.key"
      :state="tab.httpRequest"
      :tab-key="tab.key"
      @update:state="updateHttpRequestEditorState(tab, $event)"
    />
    <WebSocketClientPane
      v-for="tab in webSocketClientTabs"
      v-show="activeTab.key === tab.key"
      :key="tab.key"
      :state="tab.webSocketClient"
      :tab-key="tab.key"
      @update:state="updateWebSocketClientState(tab, $event)"
    />
  </div>
</template>
