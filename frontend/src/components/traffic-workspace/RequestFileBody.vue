<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResolveRequestDraftFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { Dialogs, Events } from '@wailsio/runtime'
import AppTooltip from '@/components/common/AppTooltip.vue'
import { useNotify } from '@/composables/useNotify'
import { REQUEST_EDITOR_FILE_DROP_EVENT, LOCAL_DATA_CLEARED_EVENT } from '@/runtime/appEvents'
import type { RequestFileValue } from '@/types/request-editor'
import { buildRequestBodyFileDropTarget, parseRequestFileDropPayload } from '@/utils/requestFileDrop'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { formatFileSize } from '@/utils/format'
import { createLatestOperationGuard } from '@/utils/latestOperation'
import { parseLocalDataClearedPayload } from '@/utils/localDataCleared'

const props = defineProps<{
  tabKey: string
  file: RequestFileValue | null
}>()

const emit = defineEmits<{
  'update:file': [value: RequestFileValue | null]
}>()

const { t } = useI18n()
const notify = useNotify()
const fileDropTarget = computed(() => buildRequestBodyFileDropTarget(props.tabKey))
const fileResolveGuard = createLatestOperationGuard()

const fileDropActive = ref(false)

const fileModel = computed({
  get: () => props.file,
  set: (value: RequestFileValue | null) => emit('update:file', value),
})

async function openRequestFilePicker() {
  const operationToken = fileResolveGuard.begin()
  try {
    const selectedPath = await Dialogs.OpenFile({
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: false,
      AllowsOtherFiletypes: true,
    })
    const filePath = typeof selectedPath === 'string' ? selectedPath.trim() : ''
    if (!filePath || !fileResolveGuard.isCurrent(operationToken)) {
      return
    }
    const selectedFile = await ResolveRequestDraftFile(filePath)
    if (!selectedFile || !fileResolveGuard.isCurrent(operationToken)) {
      return
    }
    fileModel.value = {
      path: selectedFile.path,
      name: selectedFile.name,
      size: selectedFile.size,
    }
  } catch (error) {
    if (!fileResolveGuard.isCurrent(operationToken)) {
      return
    }
    if (isDialogCancelError(error)) {
      return
    }
    notify.error(getErrorMessage(error))
  }
}

function clearBodyFile() {
  fileResolveGuard.invalidate()
  fileModel.value = null
}

function handleFileDropEnter(event: DragEvent) {
  event.preventDefault()
  fileDropActive.value = true
}

function handleFileDropLeave(event: DragEvent) {
  event.preventDefault()
  fileDropActive.value = false
}

async function handleRuntimeFileDrop(paths: string[]) {
  const operationToken = fileResolveGuard.begin()
  fileDropActive.value = false
  const filePath = String(paths[0] ?? '').trim()
  if (!filePath) {
    notify.error(t('workspace.http_request.file_path_unresolved'))
    return
  }
  try {
    const selectedFile = await ResolveRequestDraftFile(filePath)
    if (!selectedFile || !fileResolveGuard.isCurrent(operationToken)) {
      return
    }
    fileModel.value = {
      path: selectedFile.path,
      name: selectedFile.name,
      size: selectedFile.size,
    }
  } catch (error) {
    if (fileResolveGuard.isCurrent(operationToken)) {
      notify.error(String(error))
    }
  }
}

const offRequestFileDrop = Events.On(REQUEST_EDITOR_FILE_DROP_EVENT, async (event) => {
  const payload = parseRequestFileDropPayload(event.data)
  if (
    !payload ||
    payload.target.kind !== 'body-file' ||
    payload.target.tabKey !== props.tabKey
  ) {
    return
  }
  await handleRuntimeFileDrop(payload.paths)
})

const offLocalDataCleared = Events.On(LOCAL_DATA_CLEARED_EVENT, (event) => {
  const payload = parseLocalDataClearedPayload(event.data)
  if (payload?.requestDraftCacheRoot) {
    fileResolveGuard.invalidate()
  }
})

onBeforeUnmount(() => {
  fileResolveGuard.invalidate()
  offRequestFileDrop()
  offLocalDataCleared()
})
</script>

<template>
  <div
    class="flex min-h-0 flex-1 items-center justify-center overflow-auto border border-dashed p-4.5 transition-colors"
    :class="
      fileDropActive
        ? 'border-(--app-action-accent-color) bg-(--app-action-accent-softer) shadow-[inset_0_0_0_1px_var(--app-action-accent-soft)]'
        : 'border-app-border'
    "
    @dragover.prevent="handleFileDropEnter"
    @dragenter.prevent="handleFileDropEnter"
    @dragleave.prevent="handleFileDropLeave"
    :data-file-drop-target="fileDropTarget"
  >
    <div v-if="!file" class="flex w-[min(420px,100%)] flex-col items-center gap-2.5 text-center text-app-text-secondary">
      <AppTooltip :text="t('workspace.http_request.file_choose')">
        <template #trigger>
          <div
            class="flex size-14 items-center justify-center rounded-[18px] bg-(--app-action-accent-soft) text-[26px] text-(--app-action-accent-color) shadow-[inset_0_0_0_1px_var(--app-action-accent-soft)] transition-[transform,box-shadow,background-color] duration-[0.16s] hover:-translate-y-px hover:bg-(--app-action-accent-selected) hover:shadow-[inset_0_0_0_1px_var(--app-action-accent-border),0_10px_20px_var(--app-action-accent-soft)] focus-visible:outline-2 focus-visible:outline-offset-[3px] focus-visible:outline-(--app-action-accent-outline)"
            role="button"
            tabindex="0"
            :aria-label="t('workspace.http_request.file_choose')"
            @click="openRequestFilePicker"
            @keydown.enter.prevent="openRequestFilePicker"
            @keydown.space.prevent="openRequestFilePicker"
          >
            <UIcon name="i-lucide-file-text" class="size-[1em]" />
          </div>
        </template>
      </AppTooltip>
      <p class="m-0 max-w-full text-sm font-bold leading-[1.45] text-[color-mix(in_srgb,var(--app-text-primary)_92%,var(--app-action-accent-color))] wrap-anywhere">{{ t('workspace.http_request.file_drop_hint') }}</p>
    </div>
    <div
      v-else
      class="flex w-[min(100%,560px)] flex-col gap-3.5 rounded-2xl bg-[color-mix(in_srgb,var(--app-panel-bg)_86%,white)] p-4 shadow-[inset_0_0_0_1px_rgba(148,163,184,0.18),0_10px_24px_rgba(15,23,42,0.06)]"
    >
      <div class="grid grid-cols-[56px_minmax(0,1fr)] items-center gap-3">
        <div class="flex size-14 items-center justify-center rounded-[18px] bg-(--app-action-accent-selected) text-[28px] text-(--app-action-accent-color) shadow-[inset_0_0_0_1px_var(--app-action-accent-soft)]">
          <UIcon name="i-lucide-file-text" class="size-[1em]" />
        </div>
        <div class="min-w-0">
          <AppTooltip :text="file.name">
            <template #trigger>
              <div class="truncate text-sm font-bold text-app-text">
                {{ file.name }}
              </div>
            </template>
          </AppTooltip>
        </div>
      </div>
      <div class="flex flex-wrap justify-center gap-2">
        <div class="inline-flex min-w-0 max-w-full items-center gap-2 rounded-full bg-app-elevated px-2.5 py-1.75 text-sm text-app-text-secondary shadow-[inset_0_0_0_1px_rgba(148,163,184,0.16)]">
          <span class="flex-[0_0_auto] font-semibold text-app-text-muted">{{ t('workspace.http_request.file_size') }}</span>
          <span>{{ formatFileSize(file.size) }}</span>
        </div>
        <AppTooltip :text="file.path">
          <template #trigger>
            <div class="flex flex-[1_1_100%] min-w-0 max-w-full items-start gap-2 rounded-[14px] bg-app-elevated px-2.5 py-1.75 text-sm text-app-text-secondary shadow-[inset_0_0_0_1px_rgba(148,163,184,0.16)]">
              <span class="flex-[0_0_auto] font-semibold text-app-text-muted">{{ t('workspace.http_request.file_path') }}</span>
              <span class="min-w-0 whitespace-normal leading-[1.45] break-all">{{ file.path }}</span>
            </div>
          </template>
        </AppTooltip>
      </div>
      <div class="flex justify-center gap-2">
        <UButton
          color="neutral"
          variant="ghost"
          :label="t('workspace.http_request.file_choose')"
          @click="openRequestFilePicker"
        />
        <UButton
          color="neutral"
          variant="ghost"
          :label="t('workspace.http_request.file_clear')"
          @click="clearBodyFile"
        />
      </div>
    </div>
  </div>
</template>
