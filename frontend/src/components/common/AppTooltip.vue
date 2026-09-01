<script setup lang="ts">
import { computed } from 'vue'

type TooltipPlacement = 'top' | 'right' | 'bottom' | 'left'

const props = withDefaults(
  defineProps<{
    text?: string
    placement?: TooltipPlacement | string
    delay?: number
  }>(),
  {
    text: '',
    placement: 'bottom',
    delay: 500,
  },
)

const side = computed<TooltipPlacement>(() => {
  const placement = props.placement.split('-')[0]
  return placement === 'top' ||
    placement === 'right' ||
    placement === 'bottom' ||
    placement === 'left'
    ? placement
    : 'bottom'
})
</script>

<template>
  <UTooltip v-if="text" :text="text" :delay-duration="delay" :content="{ side }">
    <span class="inline-flex min-w-0">
      <slot name="trigger">
        <slot />
      </slot>
    </span>
  </UTooltip>
  <span v-else class="inline-flex min-w-0">
    <slot name="trigger">
      <slot />
    </slot>
  </span>
</template>
