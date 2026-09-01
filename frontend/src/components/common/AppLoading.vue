<script setup lang="ts">
import { useI18n } from 'vue-i18n'

type LoadingSize = 'sm' | 'md' | 'lg'

const props = withDefaults(
  defineProps<{
    label?: string
    size?: LoadingSize
    fill?: boolean
  }>(),
  {
    label: '',
    size: 'lg',
    fill: false,
  },
)

const { t } = useI18n()

const iconSizeClass: Record<LoadingSize, string> = {
  sm: 'size-4.5',
  md: 'size-5',
  lg: 'size-6',
}
</script>

<template>
  <div
    class="flex items-center justify-center text-muted"
    :class="props.fill ? 'min-h-0 flex-1' : 'min-h-22'"
    role="status"
    aria-busy="true"
    :aria-label="props.label || t('app.loading')"
  >
    <UIcon
      name="i-lucide-loader-circle"
      class="animate-spin"
      :class="iconSizeClass[props.size]"
      aria-hidden="true"
    />
  </div>
</template>
