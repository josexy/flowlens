<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import CellTextareaEditor from '@/components/common/CellTextareaEditor.vue'
import { headersRecordToFields, type HeaderField } from '@/utils/headers'

const props = defineProps<{
  fields?: HeaderField[]
  headers?: Record<string, string[]>
}>()

const headerEntries = computed(() => props.fields ?? headersRecordToFields(props.headers))
const lastHeaderIndex = computed(() => headerEntries.value.length - 1)

const editingCell = ref<{ index: number; field: 'name' | 'value' } | null>(null)
const tableRef = ref<HTMLElement | null>(null)
const keyColumnWidth = ref<number | null>(null)
const isResizing = ref(false)
const resizeStartX = ref(0)
const resizeStartWidth = ref(0)

const KEY_COLUMN_MIN_WIDTH = 140
const VALUE_COLUMN_MIN_WIDTH = 160
const CELL_TEXTAREA_OVERLAY_CLASS = 'absolute inset-x-2 top-1 z-20 min-w-0'

async function focusActiveEditor() {
  await nextTick()
  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.cell-editor-input')
  editor?.focus({ preventScroll: true })
  editor?.select()
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!editingCell.value || !(event.target instanceof Node)) return

  const editor = tableRef.value?.querySelector<HTMLTextAreaElement>('.cell-editor-input')
  if (editor?.contains(event.target)) return

  stopEdit()
}

const startEdit = (index: number, field: 'name' | 'value') => {
  if (isResizing.value) return
  editingCell.value = { index, field }
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  void focusActiveEditor()
}

const stopEdit = () => {
  editingCell.value = null
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
}

const tableStyles = computed(() => {
  const widthStyle = keyColumnWidth.value === null ? 'minmax(0, 1fr)' : `${keyColumnWidth.value}px`
  return {
    '--headers-key-column-width': widthStyle,
  }
})

function onResizeStart(event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()

  const table = tableRef.value
  if (!table) return

  const keyCell = (event.target as HTMLElement).closest('.name-col') as HTMLElement | null
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
  const tableWidth = tableRef.value.clientWidth
  const maxWidth = Math.max(KEY_COLUMN_MIN_WIDTH, tableWidth - VALUE_COLUMN_MIN_WIDTH)
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
  <div
    ref="tableRef"
    class="headers-table grid h-auto min-h-0 grid-cols-[var(--headers-key-column-width,minmax(0,1fr))_minmax(0,1fr)] border border-app-border bg-app-panel"
    :style="tableStyles"
  >
    <div
      v-for="(header, index) in headerEntries"
      :key="`${index}:${header.name}:${header.value}`"
      class="group/row contents"
    >
      <div
        class="name-col relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible border-r border-app-border px-2 py-1.5 font-medium text-app-text group-hover/row:bg-app-control"
        :class="[
          index === lastHeaderIndex ? '' : 'border-b border-b-app-border',
          editingCell?.index === index && editingCell?.field === 'name' ? 'z-10' : '',
        ]"
        @click="startEdit(index, 'name')"
      >
        <CellTextareaEditor
          v-if="editingCell?.index === index && editingCell?.field === 'name'"
          :model-value="header.name"
          :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
          textarea-class="cell-editor-input"
          readonly
          @blur="stopEdit"
        />
        <span v-else class="w-full min-w-0 truncate text-sm leading-[1.45] text-app-text">{{ header.name }}</span>
        <div
          class="resizer absolute -right-0.75 top-0 bottom-0 z-2 w-1.5 cursor-e-resize bg-transparent hover:bg-app-accent hover:opacity-[0.35]"
          @mousedown="onResizeStart"
          @click.stop
        ></div>
      </div>

      <div
        class="relative flex h-10 min-h-10 min-w-0 cursor-text items-center overflow-visible px-2 py-1.5 text-app-text group-hover/row:bg-app-control"
        :class="[
          index === lastHeaderIndex ? '' : 'border-b border-b-app-border',
          editingCell?.index === index && editingCell?.field === 'value' ? 'z-10' : '',
        ]"
        @click="startEdit(index, 'value')"
      >
        <CellTextareaEditor
          v-if="editingCell?.index === index && editingCell?.field === 'value'"
          :model-value="header.value"
          :root-class="CELL_TEXTAREA_OVERLAY_CLASS"
          textarea-class="cell-editor-input"
          readonly
          @blur="stopEdit"
        />
        <span
          v-else
          class="w-full min-w-0 overflow-hidden text-sm leading-[1.45] text-app-text break-all wrap-break-word [display:-webkit-box] [-webkit-line-clamp:1] [-webkit-box-orient:vertical] [line-clamp:1]"
          >{{ header.value }}</span
        >
      </div>
    </div>
  </div>
</template>
