<script setup lang="ts">
import { computed } from 'vue'

type WindowControlIconName = 'minimize' | 'maximize' | 'restore' | 'close'

const props = withDefaults(
  defineProps<{
    name: WindowControlIconName
    size?: number | string
  }>(),
  {
    size: 15,
  },
)

const iconSize = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))
</script>

<template>
  <svg
    class="block flex-none overflow-visible text-current pointer-events-none [shape-rendering:geometricPrecision] [stroke-linecap:round] [stroke-linejoin:round] stroke-[currentColor]"
    viewBox="0 0 48 48"
    fill="none"
    aria-hidden="true"
    focusable="false"
    :style="{ width: iconSize, height: iconSize }"
  >
    <path v-if="name === 'minimize'" d="M8 24L40 24" stroke-width="4" />
    <path
      v-else-if="name === 'maximize'"
      d="M39 6H9C7.34315 6 6 7.34315 6 9V39C6 40.6569 7.34315 42 9 42H39C40.6569 42 42 40.6569 42 39V9C42 7.34315 40.6569 6 39 6Z"
      stroke-width="4"
    />
    <template v-else-if="name === 'restore'">
      <path
        d="M13 12.4316V7.8125C13 6.2592 14.2592 5 15.8125 5H40.1875C41.7408 5 43 6.2592 43 7.8125V32.1875C43 33.7408 41.7408 35 40.1875 35H35.5163"
        stroke-width="4"
      />
      <path
        d="M32.1875 13H7.8125C6.2592 13 5 14.2592 5 15.8125V40.1875C5 41.7408 6.2592 43 7.8125 43H32.1875C33.7408 43 35 41.7408 35 40.1875V15.8125C35 14.2592 33.7408 13 32.1875 13Z"
        stroke-width="4"
      />
    </template>
    <template v-else>
      <path d="M8 8L40 40" stroke-width="4" />
      <path d="M8 40L40 8" stroke-width="4" />
    </template>
  </svg>
</template>
