<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    readonly?: boolean
    rootClass?: string
    textareaClass?: string
  }>(),
  {
    readonly: false,
    rootClass: 'flex w-full min-w-0',
    textareaClass: '',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  blur: [event: FocusEvent]
  enter: [event: KeyboardEvent]
}>()

const textareaUi = computed(() => ({
  base: [
    'cell-textarea-editor-base min-h-8 w-full min-w-0 overflow-hidden bg-app-panel px-2.5 py-1.5 text-sm leading-5 shadow-[0_8px_20px_color-mix(in_srgb,var(--app-shadow-color,#0f172a)_12%,transparent)]',
    props.textareaClass,
  ]
    .filter(Boolean)
    .join(' '),
}))

function updateValue(value: unknown) {
  emit('update:modelValue', value == null ? '' : String(value))
}

function handleBlur(event: FocusEvent) {
  emit('blur', event)
}

function handleEnter(event: KeyboardEvent) {
  emit('enter', event)
}
</script>

<template>
  <UTextarea
    :model-value="modelValue"
    :readonly="readonly || undefined"
    autoresize
    :rows="1"
    variant="outline"
    :class="rootClass"
    :ui="textareaUi"
    @update:model-value="updateValue"
    @blur="handleBlur"
    @keydown.enter.prevent="handleEnter"
  />
</template>
