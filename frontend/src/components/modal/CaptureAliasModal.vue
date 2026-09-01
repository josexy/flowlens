<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  show: boolean
  currentAlias: string
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  save: [alias: string]
}>()

const { t } = useI18n()
const draftAlias = ref('')
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
    draftAlias.value = props.currentAlias
  },
  { immediate: true },
)

function handleCancel() {
  emit('update:show', false)
}

function handleSave() {
  emit('save', draftAlias.value)
}
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="t('capture.set_alias')"
    :close="!props.saving"
    :dismissible="!props.saving"
  >
    <template #body>
      <UInput
        v-model="draftAlias"
        :maxlength="120"
        :disabled="props.saving"
        :placeholder="t('capture.alias_placeholder')"
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
          :label="t('history.cancel')"
          @click="handleCancel"
        />
        <UButton :loading="props.saving" :label="t('capture.save_alias')" @click="handleSave" />
      </div>
    </template>
  </UModal>
</template>
