<script setup lang="ts">
import { computed, type CSSProperties } from 'vue'

const props = withDefaults(
  defineProps<{
    active: boolean
    size?: number
  }>(),
  {
    size: 5,
  },
)

const hitAreaStyle = computed<CSSProperties>(() => ({
  '--wails-resize-hit-area-size': `${Math.max(0, props.size)}px`,
}))
</script>

<template>
  <div
    v-if="active"
    data-window-resize-hit-area
    aria-hidden="true"
    class="pointer-events-auto absolute inset-y-0 right-0 z-(--wails-resize-hit-area-z,30) w-(--wails-resize-hit-area-size) bg-transparent [--wails-draggable:no-drag]"
    :style="hitAreaStyle"
  ></div>
</template>
