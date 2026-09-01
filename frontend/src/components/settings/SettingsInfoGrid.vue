<script setup lang="ts">
export interface SettingsInfoGridItem {
  label: string
  value: string
  monospace?: boolean
  span?: 'wide'
  tone?: 'neutral' | 'success' | 'warning' | 'error'
}

defineProps<{
  items: readonly SettingsInfoGridItem[]
  compact?: boolean
}>()

const toneClass: Record<NonNullable<SettingsInfoGridItem['tone']>, string> = {
  neutral: 'text-app-text-secondary',
  success: 'text-app-success',
  warning: 'text-app-warning',
  error: 'text-app-error',
}
</script>

<template>
  <div
    class="grid min-w-0 gap-2"
    :class="
      compact
        ? 'grid-cols-[repeat(auto-fit,minmax(118px,max-content))]'
        : 'grid-cols-[repeat(auto-fit,minmax(150px,1fr))]'
    "
  >
    <div
      v-for="item in items"
      :key="item.label"
      class="min-w-0 rounded-md border border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_72%,transparent)] px-2.5 py-2.25"
      :class="{ 'col-span-full': item.span === 'wide' }"
    >
      <div
        class="truncate text-sm font-semibold leading-[1.4] text-app-text-muted"
      >
        {{ item.label }}
      </div>
      <div
        class="mt-1 leading-[1.35] wrap-anywhere"
        :class="
          item.monospace
            ? 'text-sm font-medium'
            : 'text-sm font-semibold'
        "
        :style="item.monospace ? 'font-family: var(--code-font-family)' : ''"
      >
        <span :class="item.tone ? toneClass[item.tone] : 'text-app-text-secondary'">{{
          item.value
        }}</span>
      </div>
    </div>
  </div>
</template>
