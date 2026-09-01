<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import { useI18n } from 'vue-i18n'
import CertificateFileInput from '@/components/settings/CertificateFileInput.vue'
import { useNotify } from '@/composables/useNotify'
import {
  getErrorMessage,
  isDialogCancelError,
  withAllFilesFilter,
  type FileFilter,
} from '@/utils/dialog'

const paths = defineModel<string[]>({ required: true })

const { t } = useI18n()
const notify = useNotify()
const isSelecting = shallowRef(false)

const certificateFilters = computed<FileFilter[]>(() => [
  {
    DisplayName: t('settings.certificate_file_filter'),
    Pattern: '*.crt;*.cer;*.pem',
  },
])
const dialogFilters = computed(() =>
  withAllFilesFilter(certificateFilters.value, t('settings.all_files_filter')),
)

function addPath() {
  paths.value.push('')
}

function removePath(index: number) {
  paths.value.splice(index, 1)
}

function updatePath(index: number, value: string) {
  paths.value[index] = value
}

function appendPaths(selectedPaths: string[]) {
  const existingPaths = new Set(paths.value.map((path) => path.trim()).filter(Boolean))
  for (const selectedPath of selectedPaths) {
    const normalizedPath = selectedPath.trim()
    if (!normalizedPath || existingPaths.has(normalizedPath)) {
      continue
    }
    paths.value.push(normalizedPath)
    existingPaths.add(normalizedPath)
  }
}

async function selectRootCAFiles() {
  isSelecting.value = true
  try {
    const selectedPaths = await Dialogs.OpenFile({
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: true,
      AllowsOtherFiletypes: true,
      Filters: dialogFilters.value,
      Title: t('settings.select_root_ca_files'),
      Message: t('settings.select_root_ca_files_message'),
    })
    if (Array.isArray(selectedPaths)) {
      appendPaths(selectedPaths)
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
  <div class="flex min-w-0 flex-col gap-2.5">
    <div class="flex flex-wrap items-center gap-2">
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-folder-open"
        :loading="isSelecting"
        :label="t('settings.select_files')"
        @click="selectRootCAFiles"
      />
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-plus"
        :label="t('settings.add_path_manually')"
        @click="addPath"
      />
    </div>

    <div
      v-if="paths.length === 0"
      class="flex items-start gap-2.5 rounded-md border border-dashed border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_36%,transparent)] p-3"
    >
      <div
        class="flex size-7 shrink-0 items-center justify-center rounded-md bg-app-accent-softer text-[18px] text-app-accent"
      >
        <UIcon name="i-lucide-shield-check" class="size-[1em]" />
      </div>
      <div class="min-w-0">
        <div class="text-sm font-semibold leading-[1.35] text-app-text-secondary">{{ t('settings.no_root_cas') }}</div>
        <div class="mt-0.5 text-sm leading-[1.45] text-app-text-muted">{{ t('settings.no_root_cas_desc') }}</div>
      </div>
    </div>

    <div v-else class="flex flex-col gap-2">
      <div
        v-for="(_, index) in paths"
        :key="index"
        class="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-end gap-2 rounded-md border border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_28%,transparent)] p-2 max-[680px]:grid-cols-[minmax(0,1fr)_auto]"
      >
        <div
          class="flex size-8 items-center justify-center rounded-md bg-app-control text-sm font-bold text-app-text-muted max-[680px]:hidden"
        >
          {{ index + 1 }}
        </div>
        <CertificateFileInput
          :model-value="paths[index] ?? ''"
          @update:model-value="updatePath(index, $event)"
          :placeholder="t('settings.root_ca_path_placeholder')"
          :dialog-title="t('settings.select_root_ca_file')"
          :dialog-message="t('settings.select_root_ca_file_message')"
          :filters="certificateFilters"
          class="min-w-0"
        />
        <UTooltip :text="t('settings.remove_root_ca')" :content="{ side: 'top' }">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-trash-2"
            class="self-end"
            :aria-label="t('settings.remove_root_ca')"
            @click="removePath(index)"
          />
        </UTooltip>
      </div>
    </div>
  </div>
</template>
