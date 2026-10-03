<script setup lang="ts">
import type { HexLine } from '@/utils/hexdump'

// Stable row data and a row-local hover index keep unrelated byte nodes out
// of scroll and pointer updates in the parent viewer.
const props = defineProps<{
  line: HexLine
  top: number
  rowHeight: number
  hoveredByteIdx: number
  selectionStart: number
  selectionEnd: number
}>()

function isSelected(index: number) {
  return index >= props.selectionStart && index < props.selectionEnd
}
</script>

<template>
  <div
    class="absolute left-0 top-0 grid h-(--hex-row-height) w-full min-w-0 grid-cols-[var(--hex-offset-width)_max-content_max-content] items-center leading-(--hex-row-height)"
    :style="{ height: `${rowHeight}px`, transform: `translateY(${top}px)` }"
  >
    <span
      class="h-full min-w-0 select-none whitespace-nowrap border-r border-app-border text-app-text-muted"
      >{{ line.offsetHex }}</span
    >
    <span
      class="grid h-full min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),3ch)] border-r border-app-border px-2"
    >
      <span
        v-for="(byte, index) in line.bytes"
        :key="index"
        class="relative cursor-default whitespace-pre px-[0.5ch] text-center text-app-text before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:border-app-border before:content-['']"
        :class="[
          byte !== null && isSelected(byte.globalIdx)
            ? 'bg-app-accent/30'
            : byte !== null && hoveredByteIdx === byte.globalIdx
              ? 'bg-app-accent-strong text-app-accent'
              : '',
          index > 0 && index % 4 === 0 ? 'before:border-l' : '',
          byte !== null && byte.value === 0 && !isSelected(byte.globalIdx)
            ? 'text-app-text-muted'
            : '',
          byte === null ? 'pointer-events-none text-transparent' : '',
        ]"
        :data-byte-idx="byte?.globalIdx"
        data-byte-column="hex"
        :data-selected="byte !== null && isSelected(byte.globalIdx) ? true : undefined"
        >{{ byte?.hex ?? '  ' }}</span
      >
    </span>
    <span
      class="grid h-full min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),1ch)] whitespace-pre pl-2 text-app-text"
    >
      <span
        v-for="(byte, index) in line.bytes"
        :key="index"
        class="cursor-default whitespace-pre text-center text-app-text"
        :class="[
          byte !== null && isSelected(byte.globalIdx)
            ? 'bg-app-accent/30'
            : byte !== null && hoveredByteIdx === byte.globalIdx
              ? 'bg-app-accent-strong text-app-accent'
              : '',
          byte !== null && (byte.value < 0x20 || byte.value >= 0x7f) && !isSelected(byte.globalIdx)
            ? 'opacity-35'
            : '',
          byte === null ? 'pointer-events-none opacity-0' : '',
        ]"
        :data-byte-idx="byte?.globalIdx"
        data-byte-column="ascii"
        :data-selected="byte !== null && isSelected(byte.globalIdx) ? true : undefined"
        >{{ byte?.ascii ?? ' ' }}</span
      >
    </span>
  </div>
</template>
