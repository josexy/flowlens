<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ProxyToolbarIcon from '@/components/traffic/ProxyToolbarIcon.vue'

const props = defineProps<{
  port: number
  saving?: boolean
}>()

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{
  save: [port: number | null]
}>()

const { t } = useI18n()
const draftPort = ref<number | null>(props.port)
const validPort = computed(
  () =>
    typeof draftPort.value === 'number' &&
    Number.isInteger(draftPort.value) &&
    draftPort.value >= 1 &&
    draftPort.value <= 65535,
)
const buttonUI = { base: 'disabled:opacity-100 aria-disabled:opacity-100' }

watch(
  open,
  (isOpen) => {
    if (!isOpen) {
      return
    }
    draftPort.value = props.port
  },
  { immediate: true },
)

function handleCancel() {
  if (props.saving) return
  open.value = false
}

function handleSave() {
  if (props.saving || !validPort.value) return
  emit('save', draftPort.value)
}
</script>

<template>
  <UPopover v-model:open="open" :dismissible="!props.saving" :content="{ side: 'bottom' }">
    <UTooltip :text="t('toolbar.quick_port', { port: props.port })" :content="{ side: 'bottom' }">
      <UButton
        class="p-1"
        color="neutral"
        variant="ghost"
        size="md"
        square
        :disabled="props.saving"
        :ui="buttonUI"
        :aria-label="t('toolbar.quick_port', { port: props.port })"
      >
        <ProxyToolbarIcon name="port" />
      </UButton>
    </UTooltip>

    <template #content>
      <form class="w-72 space-y-3 p-4" :aria-busy="props.saving" @submit.prevent="handleSave">
        <UFormField
          :label="t('toolbar.port')"
          :error="!validPort ? t('toolbar.invalid_port') : undefined"
        >
          <UInputNumber
            v-model="draftPort"
            :min="1"
            :max="65535"
            :step="1"
            :disabled="props.saving"
            :ui="{ base: 'disabled:opacity-100' }"
            :increment="{ ui: buttonUI }"
            :decrement="{ ui: buttonUI }"
            class="w-full"
          />
        </UFormField>
        <div class="flex justify-end gap-2">
          <UButton
            type="button"
            color="neutral"
            variant="outline"
            :disabled="props.saving"
            :ui="buttonUI"
            class="disabled:hover:bg-elevated"
            :label="t('toolbar.cancel')"
            @click="handleCancel"
          />
          <UButton
            type="submit"
            :disabled="props.saving || !validPort"
            :ui="buttonUI"
            class="disabled:hover:bg-primary/75"
            :label="t('toolbar.save')"
          />
        </div>
      </form>
    </template>
  </UPopover>
</template>
