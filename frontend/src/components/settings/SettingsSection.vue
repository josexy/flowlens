<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    danger?: boolean
    separated?: boolean
  }>(),
  { separated: true },
)
</script>

<template>
  <section
    class="settings-section min-w-0 [&+&:not(.settings-section--plain)]:mt-6 [&+&:not(.settings-section--plain)]:border-t [&+&:not(.settings-section--plain)]:border-app-border [&+&:not(.settings-section--plain)]:pt-5"
    :class="{ 'settings-section--plain': separated === false }"
  >
    <div
      v-if="title || $slots.actions"
      class="mb-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-2"
    >
      <div v-if="title" class="min-w-0">
        <div
          class="text-sm font-bold leading-[1.4]"
          :class="danger ? 'text-(--app-error-color)' : 'text-(--app-text-primary)'"
        >
          {{ title }}
        </div>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 flex-wrap items-center gap-2">
        <slot name="actions" />
      </div>
    </div>
    <div class="min-w-0">
      <slot />
    </div>
  </section>
</template>
