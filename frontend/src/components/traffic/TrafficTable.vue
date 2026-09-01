<script setup lang="ts">
import { useVirtualizer } from '@tanstack/vue-virtual'
import type { VirtualItem } from '@tanstack/vue-virtual'
import type { ContextMenuItem } from '@nuxt/ui'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { ProcessStatus } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { ref, onMounted, onUnmounted, nextTick, computed, inject, watch, useTemplateRef } from 'vue'
import type { CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getTrafficMethodLabel,
  getTrafficPathLabel,
  getTrafficProtocol,
  getTrafficTarget,
  getTrafficTotalDurationMicros,
  getTrafficTotalSizeBytes,
  getTrafficTypeLabel,
  splitHostportToIP,
} from '@/utils/traffic'
import { formatDurationMicros, formatFileSize } from '@/utils/format'
import { storeToRefs } from 'pinia'
import TrafficContextMenu from '../menu/TrafficContextMenu.vue'
import { TRAFFIC_STORE_KEY, FILTER_STORE_KEY } from '@/types/inject-keys'
import { useThemeStore } from '@/stores/theme'
import { useSettingStore } from '@/stores/setting'
import { isMacOS } from '@/shortcuts/binding'
import AppProcessIcon from '@/components/common/AppProcessIcon.vue'
import { useNotify } from '@/composables/useNotify'
import { getErrorMessage } from '@/utils/dialog'
import {
  getVisibleTrafficColumns,
  reorderVisibleTrafficColumns,
  type TrafficTableColumn,
  type TrafficTableColumnKey,
} from '@/utils/traffic-table-columns'

const { t } = useI18n()
const trafficStore = inject(TRAFFIC_STORE_KEY)!
const filterStore = inject(FILTER_STORE_KEY)!
const themeStore = useThemeStore()
const settingStore = useSettingStore()
const notify = useNotify()
const ROW_HEIGHT = 32
const ROW_OVERSCAN = 6
const SCROLL_SAVE_DELAY = 160
const SELECTION_DRAG_THRESHOLD = 4
const SELECTION_EDGE_SIZE = 40
const SELECTION_MAX_SCROLL_SPEED = 18

let scrollSaveTimerId = 0
let latestScrollTop = 0

const scrollRef = useTemplateRef<HTMLElement>('trafficScroll')
const headerTrackRef = useTemplateRef<HTMLElement>('headerTrack')
const contextMenuRef = ref<InstanceType<typeof TrafficContextMenu> | null>(null)

// Use store state for columns and sorting to persist across remounts (e.g. toggle detail panel)
const { columns, sortConfig } = storeToRefs(trafficStore)
const visibleColumns = computed(() =>
  getVisibleTrafficColumns(columns.value, [...settingStore.hiddenTrafficColumnKeys]),
)
const selectedEntryId = computed(() => trafficStore.selectedEntry?.id ?? null)
const selectedEntryIds = ref<Set<number>>(new Set())
watch(
  () => selectedEntryIds.value.size,
  (count) => {
    trafficStore.selectedEntryCount = count
  },
  { immediate: true },
)
const columnsLayoutKey = computed(() =>
  visibleColumns.value
    .map((col) => `${col.key}:${col.width}:${col.minWidth}:${col.isFlex ? 1 : 0}`)
    .join('|'),
)
const tableMinWidth = computed(() =>
  visibleColumns.value.reduce(
    (total, col) => total + Math.max(col.width, col.minWidth),
    0,
  ),
)

watch(
  () => [...settingStore.hiddenTrafficColumnKeys].join('|'),
  () => {
    const key = sortConfig.value.key
    if (key && settingStore.hiddenTrafficColumnKeys.has(key as TrafficTableColumnKey)) {
      sortConfig.value = { key: null, order: null }
    }
  },
  { immediate: true },
)

interface SelectionDragState {
  pointerId: number
  startClientX: number
  startClientY: number
  startEntryId: number
  lastClientX: number
  lastClientY: number
  endpointEntryId: number
  activated: boolean
}

let selectionDrag: SelectionDragState | null = null
let selectionAutoScrollFrameId = 0
let selectionClickUnlockFrameId = 0
let suppressNextRowClick = false
let isTrafficHovered = false

const methodColorMap: Record<string, string> = {
  GET: '#16a34a',
  POST: '#2563eb',
  PUT: '#d97706',
  DELETE: '#dc2626',
  PATCH: '#7c3aed',
  HEAD: '#0891b2',
  OPTIONS: '#4f46e5',
  CONNECT: '#9333ea',
}

// Drag and Drop State (Reordering)
const draggedColKey = ref<TrafficTableColumnKey | null>(null)
let columnDragStartScrollTop: number | null = null
let columnDragUnlockFrameId = 0

function cancelColumnDragUnlock() {
  if (!columnDragUnlockFrameId) return
  window.cancelAnimationFrame(columnDragUnlockFrameId)
  columnDragUnlockFrameId = 0
}

function restoreColumnDragScrollTop() {
  if (columnDragStartScrollTop === null) return

  const top = columnDragStartScrollTop
  latestScrollTop = top

  const element = scrollRef.value
  if (!element || element.scrollTop === top) return

  element.scrollTop = top
}

function finishColumnDrag() {
  if (columnDragStartScrollTop === null) {
    draggedColKey.value = null
    return
  }

  draggedColKey.value = null
  restoreColumnDragScrollTop()
  cancelColumnDragUnlock()

  columnDragUnlockFrameId = window.requestAnimationFrame(() => {
    restoreColumnDragScrollTop()
    columnDragUnlockFrameId = window.requestAnimationFrame(() => {
      restoreColumnDragScrollTop()
      columnDragUnlockFrameId = 0
      columnDragStartScrollTop = null
    })
  })
}

function onDragStart(event: DragEvent, column: TrafficTableColumn) {
  cancelColumnDragUnlock()
  draggedColKey.value = column.key
  columnDragStartScrollTop = scrollRef.value?.scrollTop ?? latestScrollTop
  latestScrollTop = columnDragStartScrollTop

  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', column.key)
  }
}

function onColumnDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
  restoreColumnDragScrollTop()
}

function onDrop(event: DragEvent, targetVisibleIndex: number) {
  event.preventDefault()
  if (draggedColKey.value !== null) {
    columns.value = reorderVisibleTrafficColumns(
      columns.value,
      [...settingStore.hiddenTrafficColumnKeys],
      draggedColKey.value,
      targetVisibleIndex,
    )
  }
  finishColumnDrag()
}

// Resizing State
const resizingColKey = ref<TrafficTableColumnKey | null>(null)
const startX = ref(0)
const startWidth = ref(0)
const isResizing = ref(false)

function onResizeStart(e: MouseEvent, column: TrafficTableColumn) {
  e.preventDefault()
  e.stopPropagation()

  if (column.isFlex) {
    const headerCell = (e.target as HTMLElement).closest('[data-col-key]') as HTMLElement | null
    if (headerCell) {
      column.width = headerCell.offsetWidth
    }
    column.isFlex = false
  }

  isResizing.value = true
  resizingColKey.value = column.key
  startX.value = e.clientX
  startWidth.value = column.width

  document.addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseup', onMouseUp)
  document.body.style.cursor = 'e-resize'
}

function onMouseMove(e: MouseEvent) {
  if (resizingColKey.value === null) return
  const column = columns.value.find((candidate) => candidate.key === resizingColKey.value)
  if (!column) return

  const diff = e.clientX - startX.value
  column.width = Math.max(column.minWidth, startWidth.value + diff)
}

function onMouseUp() {
  resizingColKey.value = null
  document.removeEventListener('mousemove', onMouseMove)
  document.removeEventListener('mouseup', onMouseUp)
  document.body.style.cursor = ''
  setTimeout(() => {
    isResizing.value = false
  }, 0)
}

async function updateTrafficTableColumnVisibility(
  key: TrafficTableColumnKey,
  visible: boolean,
) {
  try {
    await settingStore.setTrafficTableColumnVisible(key, visible)
  } catch (error) {
    notify.error(t('traffic.column_settings_save_failed', { error: getErrorMessage(error) }))
  }
}

async function showAllTrafficTableColumns() {
  try {
    await settingStore.showAllTrafficTableColumns()
  } catch (error) {
    notify.error(t('traffic.column_settings_save_failed', { error: getErrorMessage(error) }))
  }
}

const columnMenuItems = computed<ContextMenuItem[]>(() => {
  const visibleKeys = new Set(visibleColumns.value.map((column) => column.key))
  const saving = settingStore.isSavingTrafficTableConfig
  return [
    { type: 'label', label: t('traffic.columns') },
    ...columns.value.map((column) => {
      const checked = visibleKeys.has(column.key)
      return {
        type: 'checkbox' as const,
        label: t(column.title),
        checked,
        disabled: saving || (checked && visibleColumns.value.length === 1),
        onSelect: (event: Event) => event.preventDefault(),
        onUpdateChecked: (nextChecked: boolean) => {
          void updateTrafficTableColumnVisibility(column.key, nextChecked)
        },
      }
    }),
    { type: 'separator' },
    {
      label: t('traffic.show_all_columns'),
      icon: 'i-lucide-list-checks',
      disabled: saving || settingStore.hiddenTrafficColumnKeys.size === 0,
      onSelect: (event: Event) => {
        event.preventDefault()
        void showAllTrafficTableColumns()
      },
    },
  ]
})

// Sort function
const toggleSort = (key: TrafficTableColumnKey) => {
  if (isResizing.value || columnDragStartScrollTop !== null) return

  if (sortConfig.value.key === key) {
    if (sortConfig.value.order === 'asc') {
      sortConfig.value.order = 'desc'
    } else if (sortConfig.value.order === 'desc') {
      sortConfig.value.order = null
      sortConfig.value.key = null
    } else {
      sortConfig.value.order = 'asc'
    }
  } else {
    sortConfig.value.key = key
    sortConfig.value.order = 'asc'
  }
}

// Computed property for sorted entries
const sortedEntries = computed(() => {
  const { key, order } = sortConfig.value

  if (!key || !order) {
    return filterStore.filteredEntries
  }

  const metricSortValues =
    key === 'duration' || key === 'size'
      ? new Map(
          filterStore.filteredEntries.map((entry) => [
            entry.id,
            key === 'duration'
              ? getTrafficTotalDurationMicros(entry)
              : getTrafficTotalSizeBytes(entry),
          ]),
        )
      : null

  return [...filterStore.filteredEntries].sort((a, b) => {
    let valA: string | number | null = ''
    let valB: string | number | null = ''

    // Handle special cases
    if (key === 'destination') {
      valA = a.metadata?.remoteDestinationAddr || ''
      valB = b.metadata?.remoteDestinationAddr || ''
    } else if (key === 'protocol') {
      valA = getTrafficProtocol(a)
      valB = getTrafficProtocol(b)
    } else if (key === 'process') {
      valA = a.metadata?.process?.displayName ?? ''
      valB = b.metadata?.process?.displayName ?? ''
    } else if (key === 'host') {
      valA = getTrafficTarget(a)
      valB = getTrafficTarget(b)
    } else if (key === 'method') {
      valA = getTrafficMethodLabel(a)
      valB = getTrafficMethodLabel(b)
    } else if (key === 'path') {
      valA = getTrafficPathLabel(a)
      valB = getTrafficPathLabel(b)
    } else if (key === 'type') {
      valA = getTrafficTypeLabel(a)
      valB = getTrafficTypeLabel(b)
    } else if (key === 'duration' || key === 'size') {
      valA = metricSortValues?.get(a.id) ?? null
      valB = metricSortValues?.get(b.id) ?? null
    } else {
      const valueA = a[key]
      const valueB = b[key]
      valA = typeof valueA === 'string' || typeof valueA === 'number' ? valueA : ''
      valB = typeof valueB === 'string' || typeof valueB === 'number' ? valueB : ''
    }

    // Incomplete metrics stay at the end in both sort directions.
    if (valA === null && valB === null) return a.id - b.id
    if (valA === null) return 1
    if (valB === null) return -1

    // String comparison
    if (typeof valA === 'string' && typeof valB === 'string') {
      const comparison = valA.localeCompare(valB)
      if (comparison !== 0) {
        return order === 'asc' ? comparison : -comparison
      }
      return a.id - b.id
    }

    // Number comparison
    if (valA < valB) return order === 'asc' ? -1 : 1
    if (valA > valB) return order === 'asc' ? 1 : -1
    return a.id - b.id
  })
})

function formatTrafficTotalDuration(entry: proxyservice.TrafficEntry) {
  return formatDurationMicros(0, getTrafficTotalDurationMicros(entry) ?? -1)
}

function formatTrafficTotalSize(entry: proxyservice.TrafficEntry) {
  return formatFileSize(getTrafficTotalSizeBytes(entry) ?? -1)
}

function getProcessDisplayName(process: proxyservice.ProcessInfo) {
  return (
    process.displayName ||
    process.processName ||
    (process.pid ? t('traffic.process_pid', { pid: process.pid }) : '')
  )
}

const rowVirtualizer = useVirtualizer<HTMLElement, HTMLElement>(
  computed(() => ({
    count: sortedEntries.value.length,
    getScrollElement: () => scrollRef.value,
    estimateSize: () => ROW_HEIGHT,
    measureElement: () => ROW_HEIGHT,
    overscan: ROW_OVERSCAN,
    getItemKey: (index) => sortedEntries.value[index]?.id ?? index,
    initialRect: {
      width: 0,
      height: 480,
    },
    useCachedMeasurements: true,
  })),
)

const virtualRows = computed(() =>
  rowVirtualizer.value
    .getVirtualItems()
    .map((virtualRow) => ({
      virtualRow,
      item: sortedEntries.value[virtualRow.index],
    }))
    .filter(
      (row): row is { virtualRow: VirtualItem; item: proxyservice.TrafficEntry } =>
        row.item !== undefined,
    ),
)

const virtualContentHeight = computed(() => rowVirtualizer.value.getTotalSize())

function scrollToOffset(top: number) {
  latestScrollTop = top
  persistScrollTop(top)
  rowVirtualizer.value.scrollToOffset(top)

  const el = scrollRef.value
  if (!el) return

  el.scrollTop = top
  if (typeof el.scrollTo === 'function') {
    el.scrollTo({ top })
  }
}

onMounted(() => {
  latestScrollTop = trafficStore.scrollTop
  if (trafficStore.scrollTop > 0) {
    // Attempt to restore scroll position
    const restoreScroll = () => {
      scrollToOffset(trafficStore.scrollTop)
    }

    // Try immediately
    restoreScroll()

    // and after a tick to ensure layout
    nextTick(restoreScroll)

    // and after a small delay for virtual list to calculate sizes
    setTimeout(restoreScroll, 50)
  }
})

onUnmounted(() => {
  clearTimeout(scrollSaveTimerId)
  cancelColumnDragUnlock()
  finishSelectionDrag(false, false)
  clearCompatibilityClickSuppression()
  resumeLiveEntryEviction()
  persistScrollTop()
  trafficStore.selectedEntryCount = 0
})

function handleScroll(e: Event) {
  const target = e.target as HTMLElement
  if (!target) return
  // The header lives outside the scrollable element (so its own vertical
  // scrollbar stays viewport-relative instead of anchoring to the wide
  // content), so it doesn't scroll horizontally on its own — mirror the
  // row viewport's horizontal offset onto it directly via transform
  // (cheap, compositor-only) instead of assigning scrollLeft on a second
  // element, which forces a synchronous scroll reflow on every event.
  if (headerTrackRef.value) {
    headerTrackRef.value.style.transform = `translateX(${-target.scrollLeft}px)`
  }
  if (columnDragStartScrollTop !== null) {
    restoreColumnDragScrollTop()
    return
  }
  const nextScrollTop = target.scrollTop
  if (nextScrollTop !== latestScrollTop) {
    latestScrollTop = nextScrollTop
    schedulePersistScrollTop()
  }
}

function handleTrafficMouseEnter() {
  isTrafficHovered = true
  if ('pauseLiveEntryEviction' in trafficStore) {
    trafficStore.pauseLiveEntryEviction()
  }
}

function handleTrafficMouseLeave() {
  isTrafficHovered = false
  if (!selectionDrag) {
    resumeLiveEntryEviction()
  }
}

function resumeLiveEntryEviction() {
  if ('resumeLiveEntryEviction' in trafficStore) {
    trafficStore.resumeLiveEntryEviction()
  }
}

function schedulePersistScrollTop() {
  clearTimeout(scrollSaveTimerId)
  scrollSaveTimerId = window.setTimeout(() => {
    scrollSaveTimerId = 0
    persistScrollTop()
  }, SCROLL_SAVE_DELAY)
}

function persistScrollTop(top = latestScrollTop) {
  if (trafficStore.scrollTop !== top) {
    trafficStore.scrollTop = top
  }
}

function getMethodBadgeStyle(method: string) {
  const methodColor = methodColorMap[method.trim().toUpperCase()] ?? '#475569'
  return {
    color: methodColor,
    background: `color-mix(in srgb, ${methodColor} 13%, var(--app-panel-bg))`,
  }
}

function getStatusColor(statusCode: number) {
  if (statusCode >= 200 && statusCode < 300) return 'success'
  if (statusCode >= 300 && statusCode < 400) return 'info'
  if (statusCode >= 400 && statusCode < 500) return 'warning'
  if (statusCode >= 500) return 'error'
  return 'default'
}

const badgeToneClass: Record<'success' | 'info' | 'warning' | 'error' | 'default', string> = {
  success: 'text-[#16a34a] bg-[rgba(34,197,94,0.14)]',
  info: 'text-[#0284c7] bg-[rgba(14,165,233,0.14)]',
  warning: 'text-[#b45309] bg-[rgba(245,158,11,0.16)]',
  error: 'text-[#dc2626] bg-[rgba(239,68,68,0.14)]',
  default: 'text-app-text-muted bg-app-control',
}

const badgeBase =
  'inline-flex h-5 min-w-9 items-center justify-center rounded-full px-2 font-sans text-xs font-bold leading-none shadow-[inset_0_0_0_1px_color-mix(in_srgb,currentColor_10%,transparent)]'

function getHighlightColor(row: proxyservice.TrafficEntry) {
  return trafficStore.highlightMap.get(row.id) ?? ''
}

function isEntrySelected(entryId: number) {
  return selectedEntryIds.value.has(entryId)
}

function getRowStyle(virtualRow: VirtualItem, row: proxyservice.TrafficEntry): CSSProperties {
  const color = getHighlightColor(row)
  const style: CSSProperties = {
    position: 'absolute',
    top: '0',
    left: '0',
    height: `${virtualRow.size}px`,
    transform: `translateY(${virtualRow.start}px)`,
  }

  if (!color || isEntrySelected(row.id)) {
    return style
  }

  const backgroundAlpha = themeStore.isDark ? '59' : '26'
  const outlineAlpha = themeStore.isDark ? '73' : '40'
  style.backgroundColor = `${color}${backgroundAlpha}`
  style.borderLeft = `3px solid ${color}`
  style.boxShadow = `inset 0 0 0 1px ${color}${outlineAlpha}`
  return style
}

const trafficTableThemeVars = computed(() => {
  const sharedVars = {
    '--traffic-table-content-width': `max(100%, ${tableMinWidth.value}px)`,
  }

  if (themeStore.isDark) {
    return {
      ...sharedVars,
      '--traffic-selected-bg': 'var(--app-accent-selected)',
      '--traffic-selected-outline': 'color-mix(in srgb, var(--app-accent-color) 35%, transparent)',
    }
  }
  return {
    ...sharedVars,
    '--traffic-selected-bg': 'var(--app-accent-selected)',
    '--traffic-selected-outline': 'color-mix(in srgb, var(--app-accent-color) 22%, transparent)',
  }
})

function handleRowClick(event: MouseEvent, row: proxyservice.TrafficEntry) {
  if (suppressNextRowClick) {
    clearCompatibilityClickSuppression()
    return
  }

  if (event.shiftKey) {
    toggleEntrySelection(row)
    return
  }

  const primaryModifierPressed = isMacOS() ? event.metaKey : event.ctrlKey
  if (primaryModifierPressed) {
    if (!isEntrySelected(row.id)) {
      replaceSelectionWithEntry(row)
    }
    if (trafficStore.selectedEntry?.id === row.id) {
      trafficStore.showDetailPanel = !trafficStore.showDetailPanel
    } else {
      void trafficStore.selectEntry(row)
      trafficStore.showDetailPanel = true
    }
    return
  }

  if (selectedEntryIds.value.size === 1 && isEntrySelected(row.id)) {
    clearSelection()
    return
  }

  replaceSelectionWithEntry(row)
  void trafficStore.selectEntry(row)
}

function replaceSelectionWithEntry(row: proxyservice.TrafficEntry) {
  selectedEntryIds.value = new Set([row.id])
}

function toggleEntrySelection(row: proxyservice.TrafficEntry) {
  const nextSelection = new Set(selectedEntryIds.value)
  if (nextSelection.has(row.id)) {
    nextSelection.delete(row.id)
  } else {
    nextSelection.add(row.id)
  }
  selectedEntryIds.value = nextSelection

  if (nextSelection.has(row.id)) {
    void trafficStore.selectEntry(row)
    return
  }
  if (nextSelection.size === 0) {
    void trafficStore.selectEntry(null)
    return
  }
  if (trafficStore.selectedEntry?.id === row.id) {
    const nextFocusedEntry = sortedEntries.value.find((entry) => nextSelection.has(entry.id))
    void trafficStore.selectEntry(nextFocusedEntry ?? null)
  }
}

function replaceSelectionWithRange(startIndex: number, endIndex: number) {
  const from = Math.min(startIndex, endIndex)
  const to = Math.max(startIndex, endIndex)
  selectedEntryIds.value = new Set(
    sortedEntries.value.slice(from, to + 1).map((entry) => entry.id),
  )
}

function clearSelection() {
  selectedEntryIds.value = new Set()
  void trafficStore.selectEntry(null)
}

function cancelSelectionClickUnlock() {
  if (!selectionClickUnlockFrameId) return
  window.cancelAnimationFrame(selectionClickUnlockFrameId)
  selectionClickUnlockFrameId = 0
}

function clearCompatibilityClickSuppression() {
  cancelSelectionClickUnlock()
  suppressNextRowClick = false
}

function suppressCompatibilityClick() {
  clearCompatibilityClickSuppression()
  suppressNextRowClick = true
  selectionClickUnlockFrameId = window.requestAnimationFrame(() => {
    selectionClickUnlockFrameId = 0
    suppressNextRowClick = false
  })
}

function cancelSelectionAutoScroll() {
  if (!selectionAutoScrollFrameId) return
  window.cancelAnimationFrame(selectionAutoScrollFrameId)
  selectionAutoScrollFrameId = 0
}

function getPointerRowIndex(clientY: number) {
  const element = scrollRef.value
  if (!element || sortedEntries.value.length === 0) return -1

  const rect = element.getBoundingClientRect()
  const contentY = element.scrollTop + clientY - rect.top
  return Math.min(
    sortedEntries.value.length - 1,
    Math.max(0, Math.floor(contentY / ROW_HEIGHT)),
  )
}

function updateSelectionDragEndpoint(clientY: number) {
  const drag = selectionDrag
  if (!drag?.activated) return

  const startIndex = sortedEntries.value.findIndex((entry) => entry.id === drag.startEntryId)
  const endpointIndex = getPointerRowIndex(clientY)
  const endpoint = sortedEntries.value[endpointIndex]
  if (startIndex === -1 || !endpoint) {
    finishSelectionDrag(false, false)
    return
  }

  drag.endpointEntryId = endpoint.id
  replaceSelectionWithRange(startIndex, endpointIndex)
}

function getSelectionAutoScrollDelta(clientY: number) {
  const element = scrollRef.value
  if (!element) return 0

  const rect = element.getBoundingClientRect()
  const viewportTop = rect.top
  const viewportBottom = rect.top + element.clientHeight
  if (clientY < viewportTop + SELECTION_EDGE_SIZE) {
    const intensity = Math.min(1, (viewportTop + SELECTION_EDGE_SIZE - clientY) / SELECTION_EDGE_SIZE)
    return -Math.max(1, Math.ceil(SELECTION_MAX_SCROLL_SPEED * intensity))
  }
  if (clientY > viewportBottom - SELECTION_EDGE_SIZE) {
    const intensity = Math.min(
      1,
      (clientY - (viewportBottom - SELECTION_EDGE_SIZE)) / SELECTION_EDGE_SIZE,
    )
    return Math.max(1, Math.ceil(SELECTION_MAX_SCROLL_SPEED * intensity))
  }
  return 0
}

function runSelectionAutoScroll() {
  selectionAutoScrollFrameId = 0
  const drag = selectionDrag
  const element = scrollRef.value
  if (!drag?.activated || !element) return

  const delta = getSelectionAutoScrollDelta(drag.lastClientY)
  if (delta === 0) return

  const maxScrollTop = Math.max(0, element.scrollHeight - element.clientHeight)
  const nextScrollTop = Math.min(maxScrollTop, Math.max(0, element.scrollTop + delta))
  if (nextScrollTop === element.scrollTop) return

  element.scrollTop = nextScrollTop
  latestScrollTop = nextScrollTop
  schedulePersistScrollTop()
  updateSelectionDragEndpoint(drag.lastClientY)
  if (selectionDrag?.activated) {
    selectionAutoScrollFrameId = window.requestAnimationFrame(runSelectionAutoScroll)
  }
}

function scheduleSelectionAutoScroll() {
  if (selectionAutoScrollFrameId || !selectionDrag?.activated) return
  if (getSelectionAutoScrollDelta(selectionDrag.lastClientY) === 0) return
  selectionAutoScrollFrameId = window.requestAnimationFrame(runSelectionAutoScroll)
}

function handleRowPointerDown(event: PointerEvent, row: proxyservice.TrafficEntry) {
  if (event.pointerType !== 'mouse' || event.button !== 0 || !event.isPrimary || selectionDrag) {
    return
  }

  const element = scrollRef.value
  if (!element) return

  selectionDrag = {
    pointerId: event.pointerId,
    startClientX: event.clientX,
    startClientY: event.clientY,
    startEntryId: row.id,
    lastClientX: event.clientX,
    lastClientY: event.clientY,
    endpointEntryId: row.id,
    activated: false,
  }

  if ('pauseLiveEntryEviction' in trafficStore) {
    trafficStore.pauseLiveEntryEviction()
  }
  try {
    element.setPointerCapture(event.pointerId)
  } catch {
    selectionDrag = null
    if (!isTrafficHovered) {
      resumeLiveEntryEviction()
    }
  }
}

function handleSelectionPointerMove(event: PointerEvent) {
  const drag = selectionDrag
  if (!drag || drag.pointerId !== event.pointerId) return

  drag.lastClientX = event.clientX
  drag.lastClientY = event.clientY
  if (!drag.activated) {
    const distance = Math.hypot(
      event.clientX - drag.startClientX,
      event.clientY - drag.startClientY,
    )
    if (distance < SELECTION_DRAG_THRESHOLD) return

    drag.activated = true
  }

  event.preventDefault()
  updateSelectionDragEndpoint(event.clientY)
  scheduleSelectionAutoScroll()
}

function finishSelectionDrag(commitFocus: boolean, suppressClick: boolean) {
  const drag = selectionDrag
  if (!drag) return

  const element = scrollRef.value
  if (element) {
    const rect = element.getBoundingClientRect()
    isTrafficHovered =
      drag.lastClientX >= rect.left &&
      drag.lastClientX <= rect.right &&
      drag.lastClientY >= rect.top &&
      drag.lastClientY <= rect.bottom
  } else {
    isTrafficHovered = false
  }

  selectionDrag = null
  cancelSelectionAutoScroll()

  if (element?.hasPointerCapture(drag.pointerId)) {
    element.releasePointerCapture(drag.pointerId)
  }

  if (drag.activated && commitFocus) {
    const endpoint = sortedEntries.value.find((entry) => entry.id === drag.endpointEntryId)
    if (endpoint) {
      void trafficStore.selectEntry(endpoint)
    }
  }
  if (drag.activated && suppressClick) {
    suppressCompatibilityClick()
  }
  if (!isTrafficHovered) {
    resumeLiveEntryEviction()
  }
}

function handleSelectionPointerUp(event: PointerEvent) {
  const drag = selectionDrag
  if (!drag || drag.pointerId !== event.pointerId) return

  drag.lastClientX = event.clientX
  drag.lastClientY = event.clientY
  if (!drag.activated) {
    const clickedEntry = sortedEntries.value.find((entry) => entry.id === drag.startEntryId)
    finishSelectionDrag(false, false)
    if (clickedEntry) {
      handleRowClick(event, clickedEntry)
      // Pointer capture retargets the browser's compatibility click to the
      // scroll container in most engines. Suppress it as well in case an
      // engine dispatches it to the original row.
      suppressCompatibilityClick()
    }
    return
  }

  updateSelectionDragEndpoint(event.clientY)
  finishSelectionDrag(true, true)
}

function handleSelectionPointerCancel(event: PointerEvent) {
  if (selectionDrag?.pointerId !== event.pointerId) return
  selectionDrag.lastClientX = event.clientX
  selectionDrag.lastClientY = event.clientY
  finishSelectionDrag(true, false)
}

function handleSelectionLostPointerCapture(event: PointerEvent) {
  if (selectionDrag?.pointerId !== event.pointerId) return
  selectionDrag.lastClientX = event.clientX
  selectionDrag.lastClientY = event.clientY
  finishSelectionDrag(true, false)
}

function handleRowContextMenu(row: proxyservice.TrafficEntry) {
  if (!isEntrySelected(row.id)) {
    replaceSelectionWithEntry(row)
    void trafficStore.selectEntry(row)
  }
  contextMenuRef.value?.setEntries(
    sortedEntries.value.filter((entry) => selectedEntryIds.value.has(entry.id)),
  )
}

function handleTableContextMenu(event: MouseEvent) {
  // Only rows open the context menu; suppress right-clicks on empty table area so
  // the wrapping UContextMenu doesn't open with an empty or stale entry.
  if (!(event.target as HTMLElement | null)?.closest('.traffic-row')) {
    event.preventDefault()
    event.stopPropagation()
  }
}

watch(
  () => [trafficStore.pendingFocusEntryId, sortedEntries.value] as const,
  async ([entryId, entries]) => {
    if (entryId === null) return
    const index = entries.findIndex((entry) => entry.id === entryId)
    if (index === -1) return

    const entry = entries[index]
    if (!entry) return

    finishSelectionDrag(false, false)
    replaceSelectionWithEntry(entry)
    await trafficStore.selectEntry(entry)

    scrollToOffset(index * ROW_HEIGHT)

    trafficStore.clearPendingFocusEntryId()
  },
  { flush: 'post' },
)

const dataContextKey = computed(() =>
  'currentKey' in trafficStore ? trafficStore.currentKey : null,
)

watch(dataContextKey, () => {
  finishSelectionDrag(false, false)
  clearCompatibilityClickSuppression()
  selectedEntryIds.value = new Set()
  contextMenuRef.value?.setEntries([])
  void trafficStore.selectEntry(null)
})

watch(
  sortedEntries,
  (entries) => {
    const drag = selectionDrag
    if (drag) {
      const startEntryStillDisplayed = entries.some((entry) => entry.id === drag.startEntryId)
      if (!startEntryStillDisplayed) {
        finishSelectionDrag(false, false)
      } else if (drag.activated) {
        updateSelectionDragEndpoint(drag.lastClientY)
        scheduleSelectionAutoScroll()
      }
    }

    const previousSelectionSize = selectedEntryIds.value.size
    if (previousSelectionSize === 0) return

    const remainingEntries = entries.filter((entry) => selectedEntryIds.value.has(entry.id))
    if (remainingEntries.length !== previousSelectionSize) {
      selectedEntryIds.value = new Set(remainingEntries.map((entry) => entry.id))
    }

    const focusedId = trafficStore.selectedEntry?.id
    if (remainingEntries.length === 0) {
      void trafficStore.selectEntry(null)
    } else if (!focusedId || !remainingEntries.some((entry) => entry.id === focusedId)) {
      void trafficStore.selectEntry(remainingEntries[0]!)
    }
  },
  { flush: 'post' },
)

// Anchor the viewport while the store evicts the oldest entries during live
// capture. At the maxEntries cap each new request drops row 0 and shifts every
// remaining row up by ROW_HEIGHT; without this the rows slide under a stationary
// cursor and read as hover "jitter". Re-pin the row that was at the top of the
// viewport so the visible rows stay put — but not when the user sits at the very
// top (oldest is meant to churn off) or near the tail (following newest), nor on
// sort/filter reorders (detected as a large anchor drift).
const MAX_ANCHOR_DRIFT_ROWS = 64
const ANCHOR_SHIFT_SAMPLE_OFFSETS = [-2, -1, 1, 2]

function isSameEntryAt(
  listA: proxyservice.TrafficEntry[],
  indexA: number,
  listB: proxyservice.TrafficEntry[],
  indexB: number,
) {
  return listA[indexA]?.id === listB[indexB]?.id
}

function isContiguousAnchorShift(
  oldList: proxyservice.TrafficEntry[],
  newList: proxyservice.TrafficEntry[],
  oldIndex: number,
  newIndex: number,
) {
  let comparableSamples = 0
  let matchingSamples = 0
  for (const offset of ANCHOR_SHIFT_SAMPLE_OFFSETS) {
    const oldSampleIndex = oldIndex + offset
    const newSampleIndex = newIndex + offset
    if (oldSampleIndex < 0 || newSampleIndex < 0) continue
    if (oldSampleIndex >= oldList.length || newSampleIndex >= newList.length) continue
    comparableSamples++
    if (isSameEntryAt(oldList, oldSampleIndex, newList, newSampleIndex)) {
      matchingSamples++
    }
  }
  return comparableSamples > 0 && matchingSamples === comparableSamples
}

watch(
  sortedEntries,
  (newList, oldList) => {
    if (!oldList || oldList.length === 0) return
    if (trafficStore.pendingFocusEntryId || columnDragStartScrollTop !== null) return

    const element = scrollRef.value
    if (!element) return

    const prevScrollTop = element.scrollTop
    if (prevScrollTop <= 0) return

    const maxScrollTop = element.scrollHeight - element.clientHeight
    if (maxScrollTop <= 0 || prevScrollTop >= maxScrollTop - ROW_HEIGHT) return

    const firstVisibleIndex = Math.floor(prevScrollTop / ROW_HEIGHT)
    const anchor = oldList[firstVisibleIndex]
    if (!anchor) return

    const newIndex = newList.findIndex((entry) => entry.id === anchor.id)
    if (newIndex === -1) return

    const driftRows = newIndex - firstVisibleIndex
    if (driftRows === 0) return
    if (
      Math.abs(driftRows) > MAX_ANCHOR_DRIFT_ROWS &&
      !isContiguousAnchorShift(oldList, newList, firstVisibleIndex, newIndex)
    ) {
      return
    }

    const targetScrollTop = Math.min(
      maxScrollTop,
      Math.max(0, prevScrollTop + driftRows * ROW_HEIGHT),
    )
    if (targetScrollTop !== prevScrollTop) {
      scrollToOffset(targetScrollTop)
    }
  },
  { flush: 'post' },
)
</script>

<template>
  <div
    class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-app-panel"
    :style="trafficTableThemeVars"
  >
    <!-- pr-2.5 matches the ::-webkit-scrollbar width in style.css so the
         header columns stay aligned with rows that reserve a scrollbar gutter. -->
    <UContextMenu :items="columnMenuItems">
      <div
        class="table-header h-8 shrink-0 overflow-hidden bg-app-elevated pr-2.5 [border-bottom:1px_solid_var(--app-border-color)]"
      >
        <div
          ref="headerTrack"
          class="flex h-full w-(--traffic-table-content-width) min-w-full items-center"
        >
          <div
            v-for="(col, index) in visibleColumns"
            :key="col.key"
            class="relative flex h-full select-none items-center px-2 text-sm font-semibold text-app-text transition-colors first:pl-2.5 hover:bg-(--app-hover-bg)"
            :data-col-key="col.key"
            :style="{
              width: col.width + 'px',
              flex: col.isFlex ? '1' : 'none',
              minWidth: col.minWidth + 'px',
            }"
            draggable="true"
            @dragstart="onDragStart($event, col)"
            @dragover="onColumnDragOver"
            @drop="onDrop($event, index)"
            @dragend="finishColumnDrag"
            @click="toggleSort(col.key)"
          >
            {{ t(col.title) }}
            <UIcon
              v-if="sortConfig.key === col.key && sortConfig.order === 'asc'"
              name="i-lucide-arrow-up"
              class="ml-1 size-3 text-app-accent"
            />
            <UIcon
              v-else-if="sortConfig.key === col.key && sortConfig.order === 'desc'"
              name="i-lucide-arrow-down"
              class="ml-1 size-3 text-app-accent"
            />
            <div
              class="absolute right-0 top-0 bottom-0 z-10 w-1 cursor-e-resize bg-transparent hover:bg-app-accent hover:opacity-[0.65]"
              @mousedown="onResizeStart($event, col)"
              @click.stop
            ></div>
          </div>
        </div>
      </div>
    </UContextMenu>
    <TrafficContextMenu ref="contextMenuRef" class="flex min-h-0 flex-1 flex-col">
      <div
        ref="trafficScroll"
        class="virtual-list min-h-0 flex-1 overflow-x-auto overflow-y-auto bg-app-panel overscroll-contain scrollbar-gutter-stable will-change-scroll"
        @scroll="handleScroll"
        @mouseenter="handleTrafficMouseEnter"
        @mouseleave="handleTrafficMouseLeave"
        @contextmenu="handleTableContextMenu"
        @pointermove="handleSelectionPointerMove"
        @pointerup="handleSelectionPointerUp"
        @pointercancel="handleSelectionPointerCancel"
        @lostpointercapture="handleSelectionLostPointerCapture"
      >
      <div
        class="relative min-h-full w-(--traffic-table-content-width) min-w-full"
        :style="{ height: `${virtualContentHeight}px` }"
      >
        <div
          v-for="{ virtualRow, item } in virtualRows"
          :key="String(virtualRow.key)"
          v-memo="[
            item,
            virtualRow.start,
            virtualRow.size,
            isEntrySelected(item.id),
            selectedEntryId === item.id,
            getHighlightColor(item),
            themeStore.isDark,
            columnsLayoutKey,
          ]"
          class="traffic-row flex h-8 w-full min-w-full select-none items-center overflow-hidden transition-[background-color,box-shadow] duration-[0.12s] ease-[ease] contain-[layout_style] hover:bg-(--traffic-row-hover-bg,var(--app-hover-bg))"
          :data-entry-id="item.id"
          :data-virtual-index="virtualRow.index"
          :class="{
            'selected-row bg-(--traffic-selected-bg)!': isEntrySelected(item.id),
            'focused-row shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_var(--traffic-selected-outline)]!':
              selectedEntryId === item.id,
            'bg-[color-mix(in_srgb,var(--app-elevated-bg)_52%,var(--app-panel-bg))]':
              virtualRow.index % 2 === 1,
          }"
          :style="getRowStyle(virtualRow, item)"
          @click="handleRowClick($event, item)"
          @pointerdown="handleRowPointerDown($event, item)"
          @contextmenu="handleRowContextMenu(item)"
        >
          <div
            v-for="col in visibleColumns"
            :key="col.key"
            class="row-cell flex h-full min-h-0 items-center overflow-hidden whitespace-nowrap px-2 text-app-text first:pl-2.5"
            :style="{
              width: col.width + 'px',
              flex: col.isFlex ? '1' : 'none',
              minWidth: col.minWidth + 'px',
            }"
            :class="[
              ['host', 'path'].includes(col.key) ? 'overflow-hidden' : '',
              ['duration', 'size'].includes(col.key) ? 'tabular-nums' : '',
              'text-sm',
            ]"
          >
            <template v-if="col.key === 'id'">{{ item.id }}</template>
            <template v-else-if="col.key === 'process'">
              <span
                v-if="item.metadata?.process?.status === ProcessStatus.ProcessStatusPending"
                class="flex min-w-0 items-center gap-1.5 text-muted"
              >
                <UIcon name="i-lucide-loader-circle" class="size-4 shrink-0 animate-spin" />
                <span class="truncate">{{ t('traffic.process_identifying') }}</span>
              </span>
              <span
                v-else-if="
                  item.metadata?.process?.status === ProcessStatus.ProcessStatusResolved
                "
                class="flex w-full min-w-0 items-center gap-1.5"
              >
                <AppProcessIcon
                  :icon-key="item.metadata.process.iconKey"
                  :alt="item.metadata.process.displayName || item.metadata.process.processName"
                />
                <span class="truncate">{{ getProcessDisplayName(item.metadata.process) }}</span>
              </span>
              <span v-else-if="item.metadata?.process" class="text-muted">&mdash;</span>
              <span v-else class="text-muted">&mdash;</span>
            </template>
            <template v-else-if="col.key === 'method'">
              <span
                v-if="item.method"
                :class="badgeBase"
                :style="getMethodBadgeStyle(item.method)"
              >
                {{ getTrafficMethodLabel(item) }}
              </span>
              <span v-else class="text-muted">&mdash;</span>
            </template>
            <template v-else-if="col.key === 'host'">
              <span class="block w-full truncate">{{ getTrafficTarget(item) || '—' }}</span>
            </template>
            <template v-else-if="col.key === 'path'">
              <span class="block w-full truncate">{{ getTrafficPathLabel(item) }}</span>
            </template>
            <template v-else-if="col.key === 'statusCode'">
              <span
                v-if="item.error"
                :class="[badgeBase, badgeToneClass.error]"
              >
                ERR
              </span>
              <span
                v-else-if="item.statusCode"
                :class="[badgeBase, badgeToneClass[getStatusColor(item.statusCode)]]"
              >
                {{ item.statusCode }}
              </span>
              <span v-else class="text-muted">&mdash;</span>
            </template>
            <template v-else-if="col.key === 'type'">
              {{ getTrafficTypeLabel(item) }}
            </template>
            <template v-else-if="col.key === 'destination'">
              {{ splitHostportToIP(item.metadata?.remoteDestinationAddr || '') }}
            </template>
            <template v-else-if="col.key === 'protocol'">
              {{ getTrafficProtocol(item) || '—' }}
            </template>
            <template v-else-if="col.key === 'duration'">
              {{ formatTrafficTotalDuration(item) }}
            </template>
            <template v-else-if="col.key === 'size'">
              {{ formatTrafficTotalSize(item) }}
            </template>
          </div>
        </div>
      </div>
    </div>
    </TrafficContextMenu>
  </div>
</template>
