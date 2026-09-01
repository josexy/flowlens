<script setup lang="ts">
import AppTooltip from '@/components/common/AppTooltip.vue'

const props = defineProps<{
  active: boolean
  label: string
}>()

const emit = defineEmits<{
  toggle: []
}>()

const activeButtonClass = [
  'bg-(--app-primary-action-bg)!',
  'text-white!',
  'shadow-[0_4px_10px_var(--app-primary-action-shadow),0_0_0_1px_color-mix(in_srgb,#fff_18%,transparent)_inset]',
  'hover:bg-(--app-primary-action-bg)!',
  'hover:text-white!',
  'focus:bg-(--app-primary-action-bg)!',
  'focus:text-white!',
  'focus-visible:bg-(--app-primary-action-bg)!',
  'focus-visible:text-white!',
  'active:bg-[var(--app-accent-pressed-color,var(--app-primary-action-hover-bg))]!',
].join(' ')
const inactiveButtonClass =
  'hover:bg-transparent! hover:text-app-accent focus-visible:bg-transparent! active:bg-transparent!'
</script>

<template>
  <AppTooltip :text="props.label" placement="bottom">
    <template #trigger>
      <UButton
        color="neutral"
        variant="link"
        :icon="props.active ? 'i-lucide-shield-check' : 'i-lucide-shield'"
        class="size-6.5 min-w-6.5 justify-center p-0! text-app-text-muted transition-[color,background-color,box-shadow] duration-[0.16s]"
        :class="props.active ? activeButtonClass : inactiveButtonClass"
        :aria-label="props.label"
        :aria-pressed="props.active"
        @click.stop="emit('toggle')"
      />
    </template>
  </AppTooltip>
</template>
