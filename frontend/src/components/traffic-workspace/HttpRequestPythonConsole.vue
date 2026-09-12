<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import type { VirtualItem } from '@tanstack/vue-virtual'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { SaveBodyToFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type { PluginLogEntry } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { formatUnixMicrosLocal } from '@/utils/format'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { useNotify } from '@/composables/useNotify'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { pythonLogLevelKey, pythonLogLineCount, pythonLogPreview, pythonLogStreamKey, pythonLogTone } from '@/utils/pythonConsole'

const props = defineProps<{ entries: PluginLogEntry[]; running: boolean }>()
const emit = defineEmits<{ clear: [] }>()
const { t } = useI18n()
const notify = useNotify()
const wordWrap = ref(true)
const detailVisible = ref(false)
const selectedEntry = ref<PluginLogEntry | null>(null)
const scrollRef = ref<HTMLElement | null>(null)
const headerTrackRef = ref<HTMLElement | null>(null)
const shouldFollowTail = ref(true)
const ROW_HEIGHT = 40
const OVERSCAN = 10
type ConsoleColumnKey = 'time' | 'type' | 'source' | 'output'
const columns: { key: ConsoleColumnKey; label: string; min: number; max: number }[] = [
  { key: 'time', label: 'workspace.http_request.console_column_time', min: 120, max: 360 },
  { key: 'type', label: 'workspace.http_request.console_column_type', min: 80, max: 260 },
  { key: 'source', label: 'workspace.http_request.console_column_source', min: 96, max: 420 },
  { key: 'output', label: 'workspace.http_request.console_column_output', min: 240, max: 900 },
]
const columnWidths = ref<Record<ConsoleColumnKey, number>>({
  time: 152,
  type: 96,
  source: 120,
  output: 260,
})
const gridTemplateColumns = computed(() =>
  `${columnWidths.value.time}px ${columnWidths.value.type}px ${columnWidths.value.source}px minmax(${columnWidths.value.output}px, 1fr)`,
)
const tableMinWidth = computed(
  () => `${columnWidths.value.time + columnWidths.value.type + columnWidths.value.source + columnWidths.value.output + 24 + (columns.length - 1) * 8}px`,
)
const resizingColumn = ref<ConsoleColumnKey | null>(null)
const resizeStartX = ref(0)
const resizeStartWidth = ref(0)

function onResizeStart(event: MouseEvent, key: ConsoleColumnKey) {
  event.preventDefault()
  event.stopPropagation()
  const column = columns.find((item) => item.key === key)
  if (!column) return
  resizingColumn.value = key
  resizeStartX.value = event.clientX
  resizeStartWidth.value = columnWidths.value[key]
  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = 'e-resize'
}

function onResizeMove(event: MouseEvent) {
  const key = resizingColumn.value
  if (!key) return
  const column = columns.find((item) => item.key === key)
  if (!column) return
  const nextWidth = resizeStartWidth.value + event.clientX - resizeStartX.value
  columnWidths.value[key] = Math.min(column.max, Math.max(column.min, nextWidth))
}

function onResizeEnd() {
  resizingColumn.value = null
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = ''
}

const consoleText = computed(() => props.entries.map((entry) => {
  const owner = pluginLabel(entry)
  const stream = entry.stream || entry.level || 'log'
  return `[${formatUnixMicrosLocal(entry.timestamp)}] [${owner}] [${stream}] ${entry.message}`
}).join('\n'))

const virtualizer = useVirtualizer<HTMLElement, HTMLElement>(computed(() => ({
  count: props.entries.length,
  getScrollElement: () => scrollRef.value,
  estimateSize: () => ROW_HEIGHT,
  overscan: OVERSCAN,
  getItemKey: (index) => props.entries[index]?.eventId || index,
  initialRect: { width: 0, height: 480 },
})))

const virtualRows = computed(() => virtualizer.value.getVirtualItems().map((virtualRow) => ({
  virtualRow,
  entry: props.entries[virtualRow.index],
})).filter((row): row is { virtualRow: VirtualItem; entry: PluginLogEntry } => row.entry !== undefined))
const virtualContentHeight = computed(() => virtualizer.value.getTotalSize())
const toneClasses: Record<ReturnType<typeof pythonLogTone>, string> = {
  neutral: 'bg-app-control text-app-text-secondary',
  info: 'bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] text-app-accent',
  warning: 'bg-[color-mix(in_srgb,var(--app-warning-color)_14%,transparent)] text-app-warning',
  error: 'bg-[color-mix(in_srgb,var(--app-error-color)_14%,transparent)] text-app-error',
}
const typeIcon: Record<string, string> = { stdout: 'i-lucide-terminal', stderr: 'i-lucide-triangle-alert' }

function pluginLabel(entry: PluginLogEntry) {
  if (entry.pluginId === 'current-request-script') {
    return t('workspace.http_request.console_current_script')
  }
  return entry.pluginName?.trim() || entry.pluginId || t('workspace.http_request.console_unknown_plugin')
}
function typeLabelKey(entry: PluginLogEntry) { return `workspace.http_request.console_type_${pythonLogStreamKey(entry)}` }
function levelLabelKey(entry: PluginLogEntry) { return `workspace.http_request.console_level_${pythonLogLevelKey(entry)}` }
function typeClass(entry: PluginLogEntry) { return toneClasses[pythonLogTone(entry)] }
function openDetail(entry: PluginLogEntry) { selectedEntry.value = entry; detailVisible.value = true }
function onScroll(event: Event) {
  const element = event.target as HTMLElement
  shouldFollowTail.value = element.scrollHeight - element.scrollTop - element.clientHeight < 80
  if (headerTrackRef.value) {
    headerTrackRef.value.style.transform = `translateX(${-element.scrollLeft}px)`
  }
}

watch(() => props.entries.length, async () => {
  if (!shouldFollowTail.value) return
  await nextTick()
  if (scrollRef.value) scrollRef.value.scrollTop = scrollRef.value.scrollHeight
})
watch(() => props.entries, (entries) => {
  if (selectedEntry.value && !entries.includes(selectedEntry.value)) {
    detailVisible.value = false
    selectedEntry.value = null
  }
})

async function copyText(text: string, successKey: string, failedKey: string) {
  if (!text) return
  try {
    await copyTextToClipboard(text)
    notify.success(t(successKey))
  } catch (error) {
    notify.error(t(failedKey, { error: getErrorMessage(error) }))
  }
}
async function copyAll() { await copyText(consoleText.value, 'workspace.http_request.console_copied', 'workspace.http_request.console_copy_failed') }
async function saveText(text: string, filename: string) {
  if (!text) return
  try {
    const selectedPath = await Dialogs.SaveFile({ Filename: filename })
    const savePath = selectedPath.trim()
    if (!savePath) return
    await SaveBodyToFile({ path: savePath, body: text, bodyEncoding: '', contentType: 'text/plain; charset=utf-8' })
  } catch (error) {
    if (isDialogCancelError(error)) return
    notify.error(t('workspace.http_request.console_save_failed', { error: getErrorMessage(error) }))
  }
}
async function saveAll() { await saveText(consoleText.value, 'flowlens-python-console.log') }
async function copySelected() { if (selectedEntry.value) await copyText(selectedEntry.value.message, 'workspace.http_request.console_entry_copied', 'workspace.http_request.console_copy_failed') }
async function saveSelected() { if (selectedEntry.value) await saveText(selectedEntry.value.message, 'flowlens-python-console-entry.log') }
function clear() { detailVisible.value = false; selectedEntry.value = null; emit('clear') }
onBeforeUnmount(() => {
  selectedEntry.value = null
  onResizeEnd()
})
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden" role="tabpanel">
    <div class="flex shrink-0 items-center justify-between gap-2 px-2.5 py-2">
      <div class="flex min-w-0 items-center gap-2 text-sm text-muted">
        <span class="size-2 shrink-0 rounded-full" :class="running ? 'animate-pulse bg-primary' : 'bg-muted'" aria-hidden="true" />
        <span>{{ running ? t('workspace.http_request.console_running') : t('workspace.http_request.console_entries', { count: entries.length }) }}</span>
      </div>
      <div class="flex shrink-0 items-center gap-1">
        <UTooltip :text="t('workspace.http_request.console_copy_all')"><UButton icon="i-lucide-copy" color="neutral" variant="ghost" size="sm" square :disabled="entries.length === 0" :aria-label="t('workspace.http_request.console_copy_all')" @click="copyAll" /></UTooltip>
        <UTooltip :text="t('workspace.http_request.console_save')"><UButton icon="i-lucide-download" color="neutral" variant="ghost" size="sm" square :disabled="entries.length === 0" :aria-label="t('workspace.http_request.console_save')" @click="saveAll" /></UTooltip>
        <UTooltip :text="t('workspace.http_request.console_clear')"><UButton icon="i-lucide-trash-2" color="neutral" variant="ghost" size="sm" square :disabled="entries.length === 0" :aria-label="t('workspace.http_request.console_clear')" @click="clear" /></UTooltip>
      </div>
    </div>

    <UEmpty v-if="entries.length === 0" icon="i-lucide-terminal" :title="running ? t('workspace.http_request.console_waiting_output') : t('workspace.http_request.console_empty')" :size="appEmptyStateSize" variant="naked" :ui="appEmptyStateUi" />
    <div v-else class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden px-2.5 pb-2.5">
      <div class="shrink-0 overflow-hidden border border-app-border bg-app-elevated text-xs font-semibold text-app-text-secondary">
        <div ref="headerTrackRef" class="grid w-full items-center gap-2 px-3" :style="{ gridTemplateColumns, minWidth: tableMinWidth }">
          <div v-for="column in columns" :key="column.key" class="relative flex h-8 min-w-0 items-center select-none">
          <span class="truncate">{{ t(column.label) }}</span>
          <div class="absolute right-0 top-0 bottom-0 z-10 w-1 cursor-e-resize bg-transparent hover:bg-app-accent hover:opacity-[0.65]" @mousedown="onResizeStart($event, column.key)" />
          </div>
        </div>
      </div>
      <div ref="scrollRef" class="min-h-0 flex-1 overflow-auto border-x border-b border-app-border [overflow-anchor:none]" @scroll="onScroll">
        <div class="relative min-h-full w-full" :style="{ height: `${virtualContentHeight}px`, minWidth: tableMinWidth }">
          <button v-for="{ virtualRow, entry } in virtualRows" :key="String(virtualRow.key)" v-memo="[entry, virtualRow.index, virtualRow.start, virtualRow.size, gridTemplateColumns]" type="button" class="absolute left-0 grid h-10 w-full items-center gap-2 border-b border-app-border bg-transparent px-3 text-left text-sm text-app-text hover:bg-app-hover focus-visible:z-10 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-app-accent" :style="{ transform: `translateY(${virtualRow.start}px)`, gridTemplateColumns, minWidth: tableMinWidth }" @click="openDetail(entry)">
            <span class="truncate tabular-nums text-app-text-secondary">{{ formatUnixMicrosLocal(entry.timestamp) }}</span>
            <span class="flex min-w-0 items-center gap-1.5">
              <UIcon :name="typeIcon[pythonLogStreamKey(entry)]" class="size-3.5 shrink-0" aria-hidden="true" />
              <span class="sr-only">{{ t(typeLabelKey(entry)) }}</span>
              <span class="min-w-0 truncate rounded-full px-1.5 py-0.5 text-[11px] font-semibold" :class="typeClass(entry)">{{ t(levelLabelKey(entry)) }}</span>
            </span>
            <span class="truncate text-app-text-secondary">{{ pluginLabel(entry) }}</span>
            <span class="flex min-w-0 items-center gap-2"><span class="min-w-0 truncate">{{ pythonLogPreview(entry.message) }}</span><span v-if="pythonLogLineCount(entry.message) > 1" class="shrink-0 text-xs text-app-text-muted">{{ t('workspace.http_request.console_lines', { count: pythonLogLineCount(entry.message) }) }}</span></span>
          </button>
        </div>
      </div>
    </div>

    <UModal v-model:open="detailVisible" :title="t('workspace.http_request.console_entry_detail')" :ui="{ content: 'max-w-[min(960px,92vw)]' }">
      <template #body>
        <template v-if="selectedEntry">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
            <div class="flex min-w-0 flex-wrap items-center gap-2 text-sm"><span class="font-medium text-app-text-secondary">{{ formatUnixMicrosLocal(selectedEntry.timestamp) }}</span><span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold" :class="typeClass(selectedEntry)"><UIcon :name="typeIcon[pythonLogStreamKey(selectedEntry)]" class="size-3.5" aria-hidden="true" />{{ t(typeLabelKey(selectedEntry)) }} · {{ t(levelLabelKey(selectedEntry)) }}</span><span class="truncate text-app-text-muted">{{ pluginLabel(selectedEntry) }}</span></div>
            <div class="flex shrink-0 items-center gap-1"><UTooltip :text="t('workspace.http_request.console_wrap')"><UButton icon="i-lucide-corner-down-left" color="neutral" variant="ghost" size="sm" square :aria-label="t('workspace.http_request.console_wrap')" :aria-pressed="wordWrap" @click="wordWrap = !wordWrap" /></UTooltip><UTooltip :text="t('workspace.http_request.console_copy_entry')"><UButton icon="i-lucide-copy" color="neutral" variant="ghost" size="sm" square :aria-label="t('workspace.http_request.console_copy_entry')" @click="copySelected" /></UTooltip><UTooltip :text="t('workspace.http_request.console_save_entry')"><UButton icon="i-lucide-download" color="neutral" variant="ghost" size="sm" square :aria-label="t('workspace.http_request.console_save_entry')" @click="saveSelected" /></UTooltip></div>
          </div>
          <div class="h-[min(70vh,720px)] overflow-hidden rounded-lg border border-app-border bg-app-elevated"><textarea class="size-full resize-none overflow-auto border-none bg-transparent p-3 text-sm leading-[1.6] text-app-text outline-none" style="font-family: var(--app-font-family)" :class="wordWrap ? 'whitespace-pre-wrap wrap-break-word' : 'whitespace-pre'" :value="selectedEntry.message" readonly spellcheck="false" :wrap="wordWrap ? 'soft' : 'off'" /></div>
        </template>
      </template>
    </UModal>
  </div>
</template>
