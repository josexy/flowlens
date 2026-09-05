<script setup lang="ts">
defineProps<{
  label: string
  hint?: string
  hintPlacement?: 'meta' | 'aside' | 'control'
  danger?: boolean
  wide?: boolean
  align?: 'center' | 'start'
}>()
</script>

<template>
  <div
    class="grid min-h-9.5 gap-3 py-1.5 max-[680px]:items-stretch max-[680px]:gap-1.5 max-[680px]:py-2.5"
    :class="[
      wide
        ? 'grid-cols-[170px_minmax(0,1fr)] max-[920px]:grid-cols-[160px_minmax(0,1fr)] max-[680px]:grid-cols-1'
        : 'grid-cols-[170px_minmax(220px,430px)_minmax(180px,1fr)] max-[920px]:grid-cols-[160px_minmax(220px,1fr)] max-[680px]:grid-cols-1',
      align === 'start' || (hint && hintPlacement === 'control') ? 'items-start' : 'items-center',
    ]"
  >
    <div
      class="min-w-0"
      :class="{
        'pt-1.5 max-[680px]:pt-0': align === 'start' || (hint && hintPlacement === 'control'),
      }"
    >
      <div
        class="text-sm font-semibold leading-[1.4]"
        :class="danger ? 'text-(--app-error-color)' : 'text-(--app-text-secondary)'"
      >
        {{ label }}
      </div>
      <div
        v-if="hint && (!hintPlacement || hintPlacement === 'meta')"
        class="mt-0.75 text-sm leading-[1.45] text-(--app-text-muted)"
      >
        {{ hint }}
      </div>
    </div>
    <div class="min-w-0">
      <slot />
      <div
        v-if="hint && hintPlacement === 'control'"
        class="mt-1.5 text-sm leading-relaxed text-app-text-muted"
      >
        {{ hint }}
      </div>
    </div>
    <div
      v-if="$slots.aside || (hint && hintPlacement === 'aside')"
      class="min-w-0 text-sm leading-[1.45] text-(--app-text-muted) max-[920px]:col-start-2 max-[680px]:col-auto"
      :class="{ 'col-start-2': wide }"
    >
      <slot name="aside" />
      <span v-if="hint && hintPlacement === 'aside'">{{ hint }}</span>
    </div>
  </div>
</template>
