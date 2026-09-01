<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DropdownMenuItem } from '@nuxt/ui'
import { ResolveRequestDraftFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { Dialogs, Events } from '@wailsio/runtime'
import CellTextareaEditor from '@/components/common/CellTextareaEditor.vue'
import { useNotify } from '@/composables/useNotify'
import { REQUEST_EDITOR_FILE_DROP_EVENT, LOCAL_DATA_CLEARED_EVENT } from '@/runtime/appEvents'
import type { RequestFileValue, HttpRequestFormDataItem } from '@/types/request-editor'
import {
  buildRequestFormDataFileDropTarget,
  parseRequestFileDropPayload,
} from '@/utils/requestFileDrop'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { createKeyedLatestOperationGuard } from '@/utils/latestOperation'
import { parseLocalDataClearedPayload } from '@/utils/localDataCleared'

const props = defineProps<{
  tabKey: string
  items: HttpRequestFormDataItem[]
}>()

const emit = defineEmits<{
  'update:items': [value: HttpRequestFormDataItem[]]
}>()

const { t } = useI18n()
const notify = useNotify()
const fileResolveGuard = createKeyedLatestOperationGuard<string>()

const formDataFileDropRowId = ref<string | null>(null)
const editingFormDataCell = ref<{ rowId: string; field: 'name' | 'value' } | null>(null)
const formDataTableRef = ref<HTMLElement | null>(null)
const CELL_TEXTAREA_OVERLAY_CLASS = 'absolute inset-x-2 top-1 z-20 min-w-0'

const itemsModel = computed({
  get: () => props.items,
  set: (value: HttpRequestFormDataItem[]) => emit('update:items', value),
})

function rowMenuItems(index: number): DropdownMenuItem[] {
  return [
    {
      label: t('workspace.http_request.form_data_row_menu_text'),
      onSelect: () => handleFormDataRowMenuSelect(index, 'text'),
    },
    {
      label: t('workspace.http_request.form_data_row_menu_file'),
      onSelect: () => handleFormDataRowMenuSelect(index, 'file'),
    },
    {
      label: t('workspace.http_request.form_data_row_menu_upload'),
      onSelect: () => handleFormDataRowMenuSelect(index, 'upload'),
    },
    {
      label: t('workspace.http_request.form_data_row_menu_delete'),
      color: 'error',
      onSelect: () => handleFormDataRowMenuSelect(index, 'delete'),
    },
  ]
}

function createFormDataRow(): HttpRequestFormDataItem {
  return {
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    enabled: true,
    name: '',
    itemType: 'text',
    value: '',
    file: null,
  }
}

function replaceRow(
  rowId: string,
  updater: (item: HttpRequestFormDataItem) => HttpRequestFormDataItem,
) {
  itemsModel.value = itemsModel.value.map((item) => {
    if (item.id !== rowId) {
      return item
    }
    return updater({ ...item })
  })
}

function addRow() {
  itemsModel.value = [...itemsModel.value, createFormDataRow()]
}

function clearRows() {
  fileResolveGuard.invalidateAll()
  itemsModel.value = [createFormDataRow()]
}

function removeRow(index: number) {
  const removedRowId = itemsModel.value[index]?.id
  if (removedRowId) {
    fileResolveGuard.invalidate(removedRowId)
  }
  if (itemsModel.value.length <= 1) {
    itemsModel.value = [createFormDataRow()]
    return
  }
  const nextItems = [...itemsModel.value]
  nextItems.splice(index, 1)
  itemsModel.value = nextItems
}

async function focusActiveEditor() {
  await nextTick()
  const editor = formDataTableRef.value?.querySelector<HTMLTextAreaElement>(
    '.form-data-cell-editor-input',
  )
  editor?.focus({ preventScroll: true })
  editor?.select()
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!editingFormDataCell.value || !(event.target instanceof Node)) {
    return
  }

  const editor = formDataTableRef.value?.querySelector<HTMLTextAreaElement>(
    '.form-data-cell-editor-input',
  )
  if (editor?.contains(event.target)) {
    return
  }

  stopFormDataEdit()
}

function startFormDataEdit(rowId: string, field: 'name' | 'value') {
  editingFormDataCell.value = { rowId, field }
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  void focusActiveEditor()
}

function stopFormDataEdit() {
  editingFormDataCell.value = null
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
}

function assignPickedFile(rowId: string, requestFile: RequestFileValue) {
  replaceRow(rowId, (item) => ({
    ...item,
    itemType: 'file',
    file: requestFile,
    value: '',
  }))
}

async function openRequestFilePicker(rowId: string) {
  const operationToken = fileResolveGuard.begin(rowId)
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
    assignPickedFile(rowId, {
      path: selectedFile.path,
      name: selectedFile.name,
      size: selectedFile.size,
    })
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

function handleFormDataFileDropEnter(rowId: string, event: DragEvent) {
  event.preventDefault()
  formDataFileDropRowId.value = rowId
}

function handleFormDataFileDropLeave(rowId: string, event: DragEvent) {
  event.preventDefault()
  if (formDataFileDropRowId.value === rowId) {
    formDataFileDropRowId.value = null
  }
}

async function handleRuntimeFileDrop(rowId: string, paths: string[]) {
  const operationToken = fileResolveGuard.begin(rowId)
  formDataFileDropRowId.value = null
  if (!rowId) {
    return
  }
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
    assignPickedFile(rowId, {
      path: selectedFile.path,
      name: selectedFile.name,
      size: selectedFile.size,
    })
  } catch (error) {
    if (fileResolveGuard.isCurrent(operationToken)) {
      notify.error(String(error))
    }
  }
}

function handleFormDataRowMenuSelect(index: number, key: string) {
  const row = itemsModel.value[index]
  if (!row) {
    return
  }
  if (key === 'delete') {
    removeRow(index)
    return
  }
  if (key === 'text') {
    fileResolveGuard.invalidate(row.id)
    replaceRow(row.id, (item) => ({
      ...item,
      itemType: 'text',
      file: null,
    }))
    return
  }
  if (key === 'file') {
    replaceRow(row.id, (item) => ({
      ...item,
      itemType: 'file',
    }))
    return
  }
  if (key === 'upload') {
    void openRequestFilePicker(row.id)
  }
}

function getFormDataDisplayValue(item: HttpRequestFormDataItem): string {
  if (item.itemType === 'file') {
    return item.file?.name || t('workspace.http_request.form_data_file_placeholder')
  }
  return item.value
}

const offRequestFileDrop = Events.On(REQUEST_EDITOR_FILE_DROP_EVENT, async (event) => {
  const payload = parseRequestFileDropPayload(event.data)
  if (
    !payload ||
    payload.target.kind !== 'form-data-file' ||
    payload.target.tabKey !== props.tabKey
  ) {
    return
  }
  await handleRuntimeFileDrop(payload.target.rowId, payload.paths)
})

const offLocalDataCleared = Events.On(LOCAL_DATA_CLEARED_EVENT, (event) => {
  const payload = parseLocalDataClearedPayload(event.data)
  if (payload?.requestDraftCacheRoot) {
    fileResolveGuard.invalidateAll()
  }
})

onBeforeUnmount(() => {
  fileResolveGuard.invalidateAll()
  offRequestFileDrop()
  offLocalDataCleared()
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
})

defineExpose({
  addRow,
  clearRows,
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="mb-2 flex justify-end gap-1">
      <UTooltip :text="t('workspace.http_request.form_data_add_row')">
        <UButton
          icon="i-lucide-plus"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          :aria-label="t('workspace.http_request.form_data_add_row')"
          @click="addRow"
        />
      </UTooltip>
      <UTooltip :text="t('workspace.http_request.form_data_clear_all')">
        <UButton
          icon="i-lucide-trash-2"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          :aria-label="t('workspace.http_request.form_data_clear_all')"
          @click="clearRows"
        />
      </UTooltip>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div
        ref="formDataTableRef"
        class="min-h-0 overflow-visible border border-app-border bg-app-panel"
      >
        <div
          v-for="(item, index) in items"
          :key="item.id"
          class="grid h-10 min-h-10 grid-cols-[46px_minmax(0,1fr)_minmax(0,1fr)_46px] bg-app-panel hover:bg-app-control"
          :class="index === 0 ? '' : 'border-t border-t-app-border'"
        >
          <div
            class="relative flex h-10 min-h-10 min-w-0 items-center justify-center overflow-visible border-r border-app-border p-1.5"
          >
            <UCheckbox
              :model-value="item.enabled"
              @update:model-value="
                replaceRow(item.id, (row) => ({ ...row, enabled: Boolean($event) }))
              "
            />
          </div>
          <div
            class="relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible border-r border-app-border p-1.5"
            :class="
              editingFormDataCell?.rowId === item.id && editingFormDataCell?.field === 'name'
                ? 'z-10'
                : ''
            "
            @click="startFormDataEdit(item.id, 'name')"
          >
            <CellTextareaEditor
              v-if="editingFormDataCell?.rowId === item.id && editingFormDataCell?.field === 'name'"
              :model-value="item.name"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="form-data-cell-editor-input"
              @update:model-value="
                replaceRow(item.id, (row) => ({
                  ...row,
                  name: $event,
                }))
              "
              @blur="stopFormDataEdit"
              @enter="stopFormDataEdit"
            />
            <span
              v-else
              class="w-full truncate text-sm font-medium leading-[1.45]"
              :class="item.name ? 'text-app-text' : 'text-app-text-muted'"
            >
              {{ item.name || t('workspace.http_request.form_data_name_placeholder') }}
            </span>
          </div>
          <div
            class="relative flex h-10 min-h-10 min-w-0 items-center overflow-visible border-r border-app-border p-1.5"
            :class="[
              formDataFileDropRowId === item.id ? 'bg-(--app-action-accent-softer)' : '',
              editingFormDataCell?.rowId === item.id && editingFormDataCell?.field === 'value'
                ? 'z-10'
                : '',
            ]"
            @dragover.prevent="handleFormDataFileDropEnter(item.id, $event)"
            @dragenter.prevent="handleFormDataFileDropEnter(item.id, $event)"
            @dragleave.prevent="handleFormDataFileDropLeave(item.id, $event)"
            :data-file-drop-target="buildRequestFormDataFileDropTarget(props.tabKey, item.id)"
          >
            <CellTextareaEditor
              v-if="
                item.itemType === 'text' &&
                editingFormDataCell?.rowId === item.id &&
                editingFormDataCell?.field === 'value'
              "
              :model-value="item.value"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="form-data-cell-editor-input"
              @update:model-value="
                replaceRow(item.id, (row) => ({
                  ...row,
                  value: $event,
                }))
              "
              @blur="stopFormDataEdit"
              @enter="stopFormDataEdit"
            />
            <div
              v-else-if="item.itemType === 'text'"
              class="flex w-full min-w-0 cursor-text items-center"
              @click="startFormDataEdit(item.id, 'value')"
            >
              <span
                class="w-full truncate text-sm leading-[1.45]"
                :class="item.value ? 'text-app-text' : 'text-app-text-muted'"
              >
                {{ item.value || t('workspace.http_request.form_data_value_placeholder') }}
              </span>
            </div>
            <UTooltip v-else :text="item.file?.path || ''">
              <div
                class="inline-flex w-full min-w-0 items-center gap-1.5 overflow-hidden rounded-full border border-dashed px-2.5 py-1 [&>span]:truncate"
                :class="
                  item.file?.path
                    ? 'border-(--app-action-accent-border) bg-(--app-action-accent-softer) text-(--app-action-accent-color)'
                    : 'border-app-border bg-transparent text-app-text-muted'
                "
              >
                <UIcon name="i-lucide-file" class="size-4 flex-[0_0_auto]" />
                <span>{{ getFormDataDisplayValue(item) }}</span>
              </div>
            </UTooltip>
          </div>
          <div class="flex h-10 min-h-10 items-center justify-center p-1.5">
            <UDropdownMenu
              :items="rowMenuItems(index)"
              :content="{ side: 'bottom', align: 'end' }"
              :aria-label="t('workspace.http_request.form_data_row_menu_actions')"
            >
              <UButton
                icon="i-lucide-ellipsis"
                color="neutral"
                variant="ghost"
                size="sm"
                square
                :aria-label="t('workspace.http_request.form_data_row_menu_actions')"
                @click.stop
              />
            </UDropdownMenu>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
