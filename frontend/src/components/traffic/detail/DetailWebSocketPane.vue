<script setup lang="ts">
import AppLoading from '@/components/common/AppLoading.vue'
import WebSocketMessageStream from '@/components/common/websocket/WebSocketMessageStream.vue'
import type {
  WebSocketDirectionFilter,
  WebSocketDisplayMessage,
  WebSocketViewMode,
} from '@/types/websocket'

const props = defineProps<{
  messages: WebSocketDisplayMessage[]
  directionFilter: WebSocketDirectionFilter
  viewMode: WebSocketViewMode
  isLoading: boolean
  isRefreshing: boolean
  hasBodyView: boolean
  showEmptyFallback: boolean
  showLoadingPlaceholder: boolean
  showLoadingOverlay: boolean
  messagesTruncated: boolean
  truncatedTitle: string
}>()

const emit = defineEmits<{
  'update:directionFilter': [value: WebSocketDirectionFilter]
  'update:viewMode': [value: WebSocketViewMode]
}>()
</script>

<template>
  <div
    class="relative flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
    :aria-busy="props.isLoading ? 'true' : 'false'"
  >
    <template v-if="props.hasBodyView || props.showEmptyFallback">
      <div
        v-if="props.messagesTruncated"
        class="mt-2 mb-2 shrink-0 px-2"
      >
        <UAlert
          color="warning"
          orientation="horizontal"
          variant="subtle"
          :title="props.truncatedTitle"
          class="w-full px-3 py-2"
          :ui="{ title: 'leading-5' }"
        />
      </div>
      <WebSocketMessageStream
        :class="{ 'pointer-events-none': props.isRefreshing }"
        :messages="props.messages"
        :direction-filter="props.directionFilter"
        :view-mode="props.viewMode"
        @update:direction-filter="emit('update:directionFilter', $event)"
        @update:view-mode="emit('update:viewMode', $event)"
      />
    </template>
    <AppLoading
      v-if="props.showLoadingPlaceholder"
      fill
      size="sm"
    />
    <AppLoading
      v-else-if="props.showLoadingOverlay"
      fill
      size="sm"
      class="absolute inset-0 z-5 bg-[color-mix(in_srgb,var(--app-panel-bg)_82%,transparent)]"
    />
  </div>
</template>
