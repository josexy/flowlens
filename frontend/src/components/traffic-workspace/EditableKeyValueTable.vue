<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppTooltip from '@/components/common/AppTooltip.vue'
import CellTextareaEditor from '@/components/common/CellTextareaEditor.vue'
import type { EditableKeyValue } from '@/types/request-editor'
import { useThemeStore } from '@/stores/theme'
import {
  isValidRequestHeaderName,
  type RequestHTTPProtocol,
} from '@/utils/headers'

const modelValue = defineModel<EditableKeyValue[]>({ required: true })
const props = withDefaults(
  defineProps<{
    keyPlaceholder?: string
    valuePlaceholder?: string
    showDuplicateWarning?: boolean
    validateHeaderNames?: boolean
    protocol?: RequestHTTPProtocol
  }>(),
  {
    keyPlaceholder: '',
    valuePlaceholder: '',
    showDuplicateWarning: true,
    validateHeaderNames: false,
    protocol: 'auto',
  },
)
const { t } = useI18n()
const themeStore = useThemeStore()

const editingCell = ref<{ rowIndex: number; field: 'key' | 'value' } | null>(null)
const tableRef = ref<HTMLElement | null>(null)
const keyColumnWidth = ref<number | null>(null)
const isResizing = ref(false)
const resizeStartX = ref(0)
const resizeStartWidth = ref(0)

const KEY_COLUMN_MIN_WIDTH = 140
const VALUE_COLUMN_MIN_WIDTH = 160
const FIXED_SIDE_COLUMNS_WIDTH = 92
const CELL_TEXTAREA_OVERLAY_CLASS = 'absolute inset-x-2 top-1 z-20 min-w-0'

const warningColor = computed(() => (themeStore.isDark ? '#fbbf24' : '#d97706'))
const keyPlaceholderText = computed(
  () => props.keyPlaceholder || t('workspace.http_request.header_key_placeholder'),
)
const valuePlaceholderText = computed(
  () => props.valuePlaceholder || t('workspace.http_request.header_value_placeholder'),
)

const duplicateRowIndexes = computed(() => {
  const seenKeys = new Set<string>()
  const duplicates = new Set<number>()

  modelValue.value.forEach((item, index) => {
    if (!item.enabled) {
      return
    }
    const key = item.key.trim()
    if (!key) {
      return
    }
    const normalizedKey = key.toLowerCase()
    if (seenKeys.has(normalizedKey)) {
      duplicates.add(index)
      return
    }
    seenKeys.add(normalizedKey)
  })

  return duplicates
})

const invalidNameRowIndexes = computed(() => {
  const invalidRows = new Set<number>()
  if (!props.validateHeaderNames) {
    return invalidRows
  }
  modelValue.value.forEach((item, index) => {
    const key = item.key.trim()
    if (item.enabled && key && !isValidRequestHeaderName(key, props.protocol)) {
      invalidRows.add(index)
    }
  })
  return invalidRows
})

function createEmptyRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

function ensureHasRow() {
  if (modelValue.value.length === 0) {
    modelValue.value.push(createEmptyRow())
  }
}

function removeRow(index: number) {
  if (modelValue.value.length <= 1) {
    modelValue.value[0] = createEmptyRow()
    return
  }
  modelValue.value.splice(index, 1)
}

async function focusActiveEditor() {
  await nextTick()
  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.cell-editor-input')
  editor?.focus({ preventScroll: true })
  editor?.select()
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!editingCell.value || !(event.target instanceof Node)) {
    return
  }

  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.cell-editor-input')
  if (editor?.contains(event.target)) {
    return
  }

  stopEdit()
}

function startEdit(rowIndex: number, field: 'key' | 'value') {
  if (isResizing.value) return
  editingCell.value = { rowIndex, field }
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  void focusActiveEditor()
}

function stopEdit() {
  editingCell.value = null
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
}

function isDuplicateRow(index: number): boolean {
  return duplicateRowIndexes.value.has(index)
}

function isInvalidNameRow(index: number): boolean {
  return invalidNameRowIndexes.value.has(index)
}

ensureHasRow()

const tableStyles = computed(() => {
  const widthStyle = keyColumnWidth.value === null ? 'minmax(0, 1fr)' : `${keyColumnWidth.value}px`
  return {
    '--request-key-value-key-column-width': widthStyle,
  }
})

function onResizeStart(event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()

  const table = tableRef.value
  if (!table) return

  const keyCell = (event.target as HTMLElement).closest('.key-col') as HTMLElement | null
  if (!keyCell) return

  isResizing.value = true
  resizeStartX.value = event.clientX
  resizeStartWidth.value = keyColumnWidth.value ?? keyCell.offsetWidth

  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = 'e-resize'
}

function onResizeMove(event: MouseEvent) {
  if (!isResizing.value || !tableRef.value) return

  const diff = event.clientX - resizeStartX.value
  const availableWidth = Math.max(0, tableRef.value.clientWidth - FIXED_SIDE_COLUMNS_WIDTH)
  const maxWidth = Math.max(KEY_COLUMN_MIN_WIDTH, availableWidth - VALUE_COLUMN_MIN_WIDTH)
  const nextWidth = Math.min(
    maxWidth,
    Math.max(KEY_COLUMN_MIN_WIDTH, resizeStartWidth.value + diff),
  )

  keyColumnWidth.value = nextWidth
}

function onResizeEnd() {
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = ''

  setTimeout(() => {
    isResizing.value = false
  }, 0)
}

onUnmounted(() => {
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  document.body.style.cursor = ''
})
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
    <div class="min-h-0 flex-1 overflow-y-auto px-2.5 pb-2.5">
      <div
        ref="tableRef"
        class="request-key-value-table min-h-0 overflow-visible border border-app-border bg-app-panel"
        :style="tableStyles"
      >
        <div
          v-for="(item, index) in modelValue"
          :key="`key-value-${index}`"
          class="grid h-10 min-h-10 grid-cols-[46px_var(--request-key-value-key-column-width,minmax(0,1fr))_minmax(0,1fr)_46px] bg-app-panel [contain-intrinsic-size:40px] hover:bg-app-control"
          :class="[
            index === 0 ? '' : 'border-t border-t-app-border',
            editingCell?.rowIndex === index
              ? '[content-visibility:visible]'
              : '[content-visibility:auto]',
          ]"
        >
          <div
            class="relative flex h-10 min-h-10 min-w-0 items-center justify-center overflow-visible border-r border-app-border p-1.5"
          >
            <UCheckbox v-model="item.enabled" />
          </div>

          <div
            class="key-col relative flex h-10 min-h-10 min-w-0 cursor-text items-center gap-1.5 overflow-visible border-r border-app-border p-1.5"
            :class="editingCell?.rowIndex === index && editingCell?.field === 'key' ? 'z-10' : ''"
            @click="startEdit(index, 'key')"
          >
            <CellTextareaEditor
              v-if="editingCell?.rowIndex === index && editingCell?.field === 'key'"
              v-model="item.key"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="cell-editor-input"
              @blur="stopEdit"
              @enter="stopEdit"
            />
            <template v-else>
              <span
                class="cell-text w-full min-w-0 truncate text-sm leading-[1.45]"
                :class="item.key ? 'text-app-text' : 'cell-text--placeholder text-app-text-muted'"
              >
                {{ item.key || keyPlaceholderText }}
              </span>
              <AppTooltip
                v-if="isInvalidNameRow(index) && editingCell?.rowIndex !== index"
                :text="
                  t('workspace.http_request.header_invalid_name', {
                    name: item.key.trim(),
                  })
                "
              >
                <template #trigger>
                  <span
                    class="inline-flex flex-[0_0_auto] items-center justify-center text-app-error"
                  >
                    <UIcon name="i-lucide-circle-x" class="size-4" />
                  </span>
                </template>
              </AppTooltip>
              <AppTooltip
                v-else-if="
                  props.showDuplicateWarning &&
                  isDuplicateRow(index) &&
                  editingCell?.rowIndex !== index
                "
                :text="t('workspace.http_request.header_duplicate_ignored')"
              >
                <template #trigger>
                  <span
                    class="inline-flex flex-[0_0_auto] items-center justify-center"
                    :style="{ color: warningColor }"
                  >
                    <UIcon name="i-lucide-circle-alert" class="size-4" />
                  </span>
                </template>
              </AppTooltip>
            </template>
            <div
              class="resizer absolute -right-0.75 top-0 bottom-0 z-2 w-1.5 cursor-e-resize bg-transparent hover:bg-app-accent hover:opacity-[0.35]"
              @mousedown="onResizeStart"
              @click.stop
            ></div>
          </div>

          <div
            class="value-col relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible border-r border-app-border p-1.5"
            :class="editingCell?.rowIndex === index && editingCell?.field === 'value' ? 'z-10' : ''"
            @click="startEdit(index, 'value')"
          >
            <CellTextareaEditor
              v-if="editingCell?.rowIndex === index && editingCell?.field === 'value'"
              v-model="item.value"
              :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
              textarea-class="cell-editor-input"
              @blur="stopEdit"
              @enter="stopEdit"
            />
            <span
              v-else
              class="cell-text w-full min-w-0 overflow-hidden text-sm leading-[1.45] break-all wrap-break-word [display:-webkit-box] [-webkit-line-clamp:1] [-webkit-box-orient:vertical] [line-clamp:1]"
              :class="item.value ? 'text-app-text' : 'cell-text--placeholder text-app-text-muted'"
            >
              {{ item.value || valuePlaceholderText }}
            </span>
          </div>

          <div class="flex h-10 min-h-10 items-center justify-center gap-1.5 p-1.5">
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
