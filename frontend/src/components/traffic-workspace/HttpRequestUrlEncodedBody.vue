<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppTooltip from '@/components/common/AppTooltip.vue'
import CellTextareaEditor from '@/components/common/CellTextareaEditor.vue'
import type { EditableKeyValue } from '@/types/request-editor'

const props = defineProps<{
  items: EditableKeyValue[]
}>()

const emit = defineEmits<{
  'update:items': [value: EditableKeyValue[]]
}>()

const { t } = useI18n()

const editingCell = ref<{ rowIndex: number; field: 'key' | 'value' } | null>(null)
const tableRef = ref<HTMLElement | null>(null)
const CELL_TEXTAREA_OVERLAY_CLASS = 'absolute inset-x-2 top-1 z-20 min-w-0'

const itemsModel = computed({
  get: () => props.items,
  set: (value: EditableKeyValue[]) => emit('update:items', value),
})

function createEmptyRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

function replaceRow(rowIndex: number, updater: (item: EditableKeyValue) => EditableKeyValue) {
  itemsModel.value = itemsModel.value.map((item, index) => {
    if (index !== rowIndex) {
      return item
    }
    return updater({ ...item })
  })
}

function addRow() {
  itemsModel.value = [...itemsModel.value, createEmptyRow()]
}

function clearRows() {
  itemsModel.value = [createEmptyRow()]
}

function removeRow(index: number) {
  if (itemsModel.value.length <= 1) {
    itemsModel.value = [createEmptyRow()]
    return
  }
  const nextItems = [...itemsModel.value]
  nextItems.splice(index, 1)
  itemsModel.value = nextItems
}

async function focusActiveEditor() {
  await nextTick()
  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.urlencoded-cell-editor-input')
  editor?.focus({ preventScroll: true })
  editor?.select()
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!editingCell.value || !(event.target instanceof Node)) {
    return
  }

  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.urlencoded-cell-editor-input')
  if (editor?.contains(event.target)) {
    return
  }

  stopEdit()
}

function startEdit(rowIndex: number, field: 'key' | 'value') {
  editingCell.value = { rowIndex, field }
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  void focusActiveEditor()
}

function stopEdit() {
  editingCell.value = null
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
}

onUnmounted(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="mb-2 flex justify-end gap-1">
      <AppTooltip :text="t('workspace.http_request.urlencoded_add_row')">
        <template #trigger>
          <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-plus" @click="addRow" />
        </template>
      </AppTooltip>
      <AppTooltip :text="t('workspace.http_request.urlencoded_clear_all')">
        <template #trigger>
          <UButton
            size="sm"
            color="neutral"
            variant="ghost"
            icon="i-lucide-trash-2"
            @click="clearRows"
          />
        </template>
      </AppTooltip>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div ref="tableRef" class="min-h-0 overflow-visible border border-app-border bg-app-panel">
        <div
          v-for="(item, index) in itemsModel"
          :key="`urlencoded-${index}`"
          class="grid h-10 min-h-10 grid-cols-[46px_minmax(0,1fr)_minmax(0,1fr)_46px] bg-app-panel hover:bg-app-control"
          :class="index === 0 ? '' : 'border-t border-t-app-border'"
        >
          <div
            class="urlencoded-cell relative flex h-10 min-h-10 min-w-0 items-center justify-center overflow-visible border-r border-app-border p-1.5"
          >
            <UCheckbox
              :model-value="item.enabled"
              @update:model-value="
                replaceRow(index, (row) => ({ ...row, enabled: Boolean($event) }))
              "
            />
          </div>
          <div
            class="urlencoded-cell editable relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible border-r border-app-border p-1.5"
            :class="editingCell?.rowIndex === index && editingCell?.field === 'key' ? 'z-10' : ''"
            @click="startEdit(index, 'key')"
          >
            <CellTextareaEditor
              v-if="editingCell?.rowIndex === index && editingCell?.field === 'key'"
              :model-value="item.key"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="urlencoded-cell-editor-input"
              @update:model-value="
                replaceRow(index, (row) => ({
                  ...row,
                  key: $event,
                }))
              "
              @blur="stopEdit"
              @enter="stopEdit"
            />
            <span
              v-else
              class="w-full truncate text-sm font-medium leading-[1.45]"
              :class="item.key ? 'text-app-text' : 'text-app-text-muted'"
            >
              {{ item.key || t('workspace.http_request.urlencoded_name_placeholder') }}
            </span>
          </div>
          <div
            class="urlencoded-cell editable relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible border-r border-app-border p-1.5"
            :class="editingCell?.rowIndex === index && editingCell?.field === 'value' ? 'z-10' : ''"
            @click="startEdit(index, 'value')"
          >
            <CellTextareaEditor
              v-if="editingCell?.rowIndex === index && editingCell?.field === 'value'"
              :model-value="item.value"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="urlencoded-cell-editor-input"
              @update:model-value="
                replaceRow(index, (row) => ({
                  ...row,
                  value: $event,
                }))
              "
              @blur="stopEdit"
              @enter="stopEdit"
            />
            <span
              v-else
              class="w-full overflow-hidden text-sm leading-[1.45] break-all wrap-break-word [display:-webkit-box] [-webkit-line-clamp:1] [-webkit-box-orient:vertical] [line-clamp:1]"
              :class="item.value ? 'text-app-text' : 'text-app-text-muted'"
            >
              {{ item.value || t('workspace.http_request.urlencoded_value_placeholder') }}
            </span>
          </div>
          <div
            class="urlencoded-cell flex h-10 min-h-10 min-w-0 items-center justify-center overflow-visible p-1.5"
          >
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-circle-minus"
              @click="removeRow(index)"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
