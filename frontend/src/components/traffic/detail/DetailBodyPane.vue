<script setup lang="ts">
import type { TrafficBodyView } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import AppLoading from '@/components/common/AppLoading.vue'
import BodyViewer from '../BodyViewer.vue'

const props = defineProps<{
  bodyView: TrafficBodyView | null
  bodySide: 'request' | 'response'
  bodyIdentity?: number | string | null
  contentType?: string
  fallbackContentType?: string
  sourcePath: string
  fallbackSourcePath: string
  isLoading: boolean
  isRefreshing: boolean
  showEmptyFallback: boolean
  showLoadingPlaceholder: boolean
  showLoadingOverlay: boolean
}>()

const bodyViewerPanelClass = 'py-2.5 pl-2.5'
</script>

<template>
  <div
    class="relative flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
    :aria-busy="props.isLoading ? 'true' : 'false'"
  >
    <BodyViewer
      v-if="props.bodyView"
      :key="`${props.bodyIdentity ?? 'unknown'}:${props.bodySide}`"
      :class="[bodyViewerPanelClass, props.isRefreshing ? 'pointer-events-none' : '']"
      :body="
        props.bodySide === 'request' ? props.bodyView.reqBody || '' : props.bodyView.rspBody || ''
      "
      :content-type="props.contentType"
      :body-encoding="
        props.bodySide === 'request' ? props.bodyView.reqBodyEnc : props.bodyView.rspBodyEnc
      "
      :source-path="props.sourcePath"
    />
    <BodyViewer
      v-else-if="props.showEmptyFallback"
      :class="[bodyViewerPanelClass, props.isRefreshing ? 'pointer-events-none' : '']"
      body=""
      :content-type="props.fallbackContentType"
      body-encoding=""
      :source-path="props.fallbackSourcePath"
    />
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
