<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  show: boolean
  port: number
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  save: [port: number | null]
}>()

const { t } = useI18n()
const draftPort = ref<number | null>(null)
const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

watch(
  () => props.show,
  (show) => {
    if (!show) {
      return
    }
    draftPort.value = props.port
  },
  { immediate: true },
)

function handleCancel() {
  emit('update:show', false)
}

function handleSave() {
  emit('save', draftPort.value)
}
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="t('toolbar.port')"
    :close="!props.saving"
    :dismissible="!props.saving"
  >
    <template #body>
      <UInputNumber
        v-model="draftPort"
        orientation="vertical"
        :min="1"
        :max="65535"
        class="w-full"
        @keyup.enter="handleSave"
      />
    </template>

    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton
          color="neutral"
          variant="outline"
          :disabled="props.saving"
          :label="t('toolbar.cancel')"
          @click="handleCancel"
        />
        <UButton :loading="props.saving" :label="t('toolbar.save')" @click="handleSave" />
      </div>
    </template>
  </UModal>
</template>
