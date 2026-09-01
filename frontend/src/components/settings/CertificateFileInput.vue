<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import { useI18n } from 'vue-i18n'
import { useNotify } from '@/composables/useNotify'
import {
  getErrorMessage,
  isDialogCancelError,
  withAllFilesFilter,
  type FileFilter,
} from '@/utils/dialog'

const props = defineProps<{
  label?: string
  placeholder?: string
  dialogTitle?: string
  dialogMessage?: string
  filters?: FileFilter[]
  buttonTitle?: string
}>()

const path = defineModel<string>({ required: true })

const { t } = useI18n()
const notify = useNotify()
const isSelecting = shallowRef(false)
const browseTitle = computed(() => props.buttonTitle ?? t('settings.select_file'))
const dialogFilters = computed(() =>
  withAllFilesFilter(props.filters, t('settings.all_files_filter')),
)

async function selectFile() {
  isSelecting.value = true
  try {
    const selectedPath = await Dialogs.OpenFile({
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: false,
      AllowsOtherFiletypes: true,
      Filters: dialogFilters.value,
      Title: props.dialogTitle,
      Message: props.dialogMessage,
    })
    if (typeof selectedPath === 'string' && selectedPath.trim()) {
      path.value = selectedPath.trim()
    }
  } catch (error) {
    if (isDialogCancelError(error)) {
      return
    }
    notify.error(
      t('settings.error_select_certificate_file', {
        error: getErrorMessage(error),
      }),
    )
  } finally {
    isSelecting.value = false
  }
}
</script>

<template>
  <div class="flex min-w-0 flex-col gap-1.25">
    <span
      v-if="label"
      class="text-sm font-semibold leading-[1.35] text-app-text-secondary"
      >{{ label }}</span
    >
    <span class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-1.5">
      <UInput v-model="path" :placeholder="placeholder" class="min-w-0" />
      <UTooltip :text="browseTitle" :content="{ side: 'top' }">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-folder-open"
          class="justify-self-end"
          :loading="isSelecting"
          :aria-label="browseTitle"
          @click="selectFile"
        />
      </UTooltip>
    </span>
  </div>
</template>
