<script setup lang="ts">
import { computed, shallowRef, useId, watch } from 'vue'
import type { DropdownMenuItem } from '@nuxt/ui'
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
const expanded = shallowRef(false)
const contentId = useId()
const addActions = computed<DropdownMenuItem[]>(() => [
  {
    label: t('settings.select_files'),
    icon: 'i-lucide-folder-open',
    onSelect: () => selectRootCAFiles(),
  },
  {
    label: t('settings.add_path_manually'),
    icon: 'i-lucide-pencil',
    onSelect: () => addPath(),
  },
])

watch(
  () => paths.value.some((path) => !path.trim()),
  (incomplete) => {
    if (incomplete) expanded.value = true
  },
  { immediate: true },
)

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
  expanded.value = true
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
    expanded.value = true
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
  <section class="min-w-0 py-2">
    <div class="flex min-w-0 items-center justify-between gap-3">
      <UButton
        color="neutral"
        variant="ghost"
        :icon="expanded ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
        :aria-expanded="expanded"
        :aria-controls="contentId"
        class="-ml-2 min-w-0 flex-1 justify-start py-2 text-left"
        @click="expanded = !expanded"
      >
        <span class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
          <span class="font-semibold text-app-text-secondary">{{
            t('settings.section_root_cas')
          }}</span>
          <span class="font-normal text-app-text-muted">{{
            paths.length
              ? t('settings.root_cas_count', { count: paths.length })
              : t('settings.no_root_cas')
          }}</span>
        </span>
      </UButton>
      <UDropdownMenu :items="addActions" :content="{ align: 'end' }">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-plus"
          trailing-icon="i-lucide-chevron-down"
          :loading="isSelecting"
          :label="t('settings.add_root_ca')"
          :aria-label="t('settings.add_root_ca_action')"
          class="shrink-0"
        />
      </UDropdownMenu>
    </div>

    <UCollapsible
      :id="contentId"
      v-model:open="expanded"
      :ui="{ content: 'motion-reduce:animate-none' }"
    >
      <template #content>
        <p v-if="paths.length === 0" class="m-0 py-3 text-sm text-app-text-muted">
          {{ t('settings.no_root_cas_desc') }}
        </p>
        <div v-else class="flex flex-col gap-2 py-3">
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
      </template>
    </UCollapsible>
  </section>
</template>
