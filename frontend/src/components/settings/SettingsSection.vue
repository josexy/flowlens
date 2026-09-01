<script setup lang="ts">
defineProps<{
  title?: string
  danger?: boolean
  separated?: boolean
}>()
</script>

<template>
  <section
    class="settings-section min-w-0 [&+&:not(.settings-section--plain)]:border-t [&+&:not(.settings-section--plain)]:border-app-border [&+&:not(.settings-section--plain)]:pt-4.5"
    :class="{ 'settings-section--plain': separated === false }"
  >
    <div
      v-if="title || $slots.actions"
      class="mb-2.5 flex items-start justify-between gap-4 max-[760px]:flex-col max-[760px]:items-stretch"
    >
      <div v-if="title" class="min-w-0">
        <div
          class="text-sm font-bold leading-[1.4]"
          :class="danger ? 'text-(--app-error-color)' : 'text-(--app-text-primary)'"
        >
          {{ title }}
        </div>
      </div>
      <div
        v-if="$slots.actions"
        class="flex shrink-0 flex-wrap items-center justify-end gap-2 max-[760px]:justify-start"
      >
        <slot name="actions" />
      </div>
    </div>
    <div class="min-w-0">
      <slot />
    </div>
  </section>
</template>
