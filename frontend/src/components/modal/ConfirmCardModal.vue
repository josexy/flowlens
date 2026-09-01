<script setup lang="ts">
import { computed } from 'vue'

type ButtonType = 'default' | 'primary' | 'warning' | 'error' | 'success'

const props = withDefaults(
  defineProps<{
    show: boolean
    title: string
    positiveText: string
    negativeText?: string
    width?: string
    positiveType?: ButtonType
    positiveDisabled?: boolean
    positiveLoading?: boolean
    negativeDisabled?: boolean
    closable?: boolean
    maskClosable?: boolean
  }>(),
  {
    negativeText: '',
    width: 'min(420px, calc(100vw - 32px))',
    positiveType: 'primary',
    positiveDisabled: false,
    positiveLoading: false,
    negativeDisabled: false,
    closable: true,
    maskClosable: true,
  },
)

const emit = defineEmits<{
  'update:show': [value: boolean]
  'positive-click': []
  'negative-click': []
}>()

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

type UiColor = 'primary' | 'success' | 'warning' | 'error' | 'neutral'
const positiveColor = computed<UiColor>(() => {
  switch (props.positiveType) {
    case 'error':
      return 'error'
    case 'warning':
      return 'warning'
    case 'success':
      return 'success'
    case 'default':
      return 'neutral'
    default:
      return 'primary'
  }
})

function handleNegativeClick() {
  emit('negative-click')
  emit('update:show', false)
}

function handlePositiveClick() {
  emit('positive-click')
}
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="props.title"
    :close="props.closable"
    :dismissible="props.maskClosable"
  >
    <template #body>
      <div class="text-sm leading-[1.6] text-app-text-secondary">
        <slot />
      </div>
    </template>

    <template #footer>
      <slot name="footer">
        <div class="flex w-full justify-end gap-2">
          <UButton
            v-if="props.negativeText"
            color="neutral"
            variant="outline"
            :disabled="props.negativeDisabled"
            :label="props.negativeText"
            @click="handleNegativeClick"
          />
          <UButton
            :color="positiveColor"
            :disabled="props.positiveDisabled"
            :loading="props.positiveLoading"
            :label="props.positiveText"
            @click="handlePositiveClick"
          />
        </div>
      </slot>
    </template>
  </UModal>
</template>
