<script setup lang="ts">
import { SplitterResizeHandle } from 'reka-ui'

defineOptions({
  inheritAttrs: false,
})

// Keep the fine hit margin within half of the 10px native scrollbar. The
// preceding-side DOM rail below makes that part of the virtual margin clickable.
const splitterHitAreaMargins = {
  fine: 5,
  coarse: 22,
} as const
</script>

<template>
  <SplitterResizeHandle
    v-bind="$attrs"
    class="group relative z-10 shrink-0 bg-(--app-splitter-handle-color) outline-none transition-colors duration-120 data-[orientation=horizontal]:w-(--app-splitter-handle-thickness) data-[orientation=horizontal]:min-w-(--app-splitter-handle-thickness) data-[orientation=horizontal]:cursor-ew-resize data-[orientation=vertical]:h-(--app-splitter-handle-thickness) data-[orientation=vertical]:min-h-(--app-splitter-handle-thickness) data-[orientation=vertical]:cursor-ns-resize hover:bg-(--app-splitter-handle-hover-color) data-resize-handle-active:bg-(--app-splitter-handle-hover-color) data-[resize-handle-state=drag]:bg-(--app-splitter-handle-hover-color) data-[resize-handle-state=hover]:bg-(--app-splitter-handle-hover-color) data-[state=drag]:bg-(--app-splitter-handle-hover-color) data-[state=hover]:bg-(--app-splitter-handle-hover-color)"
    :hit-area-margins="splitterHitAreaMargins"
  >
    <span
      aria-hidden="true"
      class="pointer-events-auto absolute bg-transparent group-data-[orientation=horizontal]:inset-y-0 group-data-[orientation=horizontal]:right-full group-data-[orientation=horizontal]:w-1.25 group-data-[orientation=vertical]:inset-x-0 group-data-[orientation=vertical]:bottom-full group-data-[orientation=vertical]:h-1.25 pointer-coarse:group-data-[orientation=horizontal]:w-5.5 pointer-coarse:group-data-[orientation=vertical]:h-5.5"
    ></span>
  </SplitterResizeHandle>
</template>
