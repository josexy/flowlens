<script setup lang="ts">
import type { HexLine } from '@/utils/hexdump'

// Stable row data and a row-local hover index keep unrelated byte nodes out
// of scroll and pointer updates in the parent viewer.
defineProps<{
  line: HexLine
  top: number
  rowHeight: number
  hoveredByteIdx: number
}>()
</script>

<template>
  <div
    class="absolute left-0 top-0 grid h-(--hex-row-height) w-full min-w-0 grid-cols-[82px_max-content_max-content] items-center gap-x-2.5 leading-(--hex-row-height)"
    :style="{ height: `${rowHeight}px`, transform: `translateY(${top}px)` }"
  >
    <span class="min-w-0 select-none whitespace-nowrap text-app-text-muted">{{ line.offsetHex }}:</span>
    <span class="grid min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),2ch)] gap-0.5">
      <span
        v-for="(byte, index) in line.bytes"
        :key="index"
        class="cursor-default whitespace-pre rounded-[3px] px-px text-center text-app-text"
        :class="[
          byte !== null && hoveredByteIdx === byte.globalIdx
            ? 'bg-[color-mix(in_srgb,var(--app-accent-color)_18%,transparent)] text-app-accent'
            : '',
          byte !== null && byte.value === 0 ? 'opacity-35' : '',
          byte === null ? 'pointer-events-none opacity-0' : '',
        ]"
        :data-byte-idx="byte?.globalIdx"
        >{{ byte?.hex ?? '  ' }}</span
      >
    </span>
    <span class="grid min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),1ch)] whitespace-pre text-app-text">
      <span
        v-for="(byte, index) in line.bytes"
        :key="index"
        class="cursor-default whitespace-pre rounded-[3px] text-center text-app-text"
        :class="[
          byte !== null && hoveredByteIdx === byte.globalIdx
            ? 'bg-[color-mix(in_srgb,var(--app-accent-color)_18%,transparent)] text-app-accent'
            : '',
          byte !== null && (byte.value < 0x20 || byte.value >= 0x7f) ? 'opacity-35' : '',
          byte === null ? 'pointer-events-none opacity-0' : '',
        ]"
        :data-byte-idx="byte?.globalIdx"
        >{{ byte?.ascii ?? ' ' }}</span
      >
    </span>
  </div>
</template>
