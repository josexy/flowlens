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
import {
  createTrafficTableSorter,
  getTrafficEvictionScrollTop,
  getTrafficLatestEdge,
  getTrafficVirtualRange,
  isTrafficAtLatestEdge,
  TRAFFIC_ROW_HEIGHT as ROW_HEIGHT,
  TRAFFIC_ROW_OVERSCAN as ROW_OVERSCAN,
} from '@/utils/traffic-table-state'

const { t } = useI18n()
const trafficStore = inject(TRAFFIC_STORE_KEY)!
const filterStore = inject(FILTER_STORE_KEY)!
const themeStore = useThemeStore()
const settingStore = useSettingStore()
const notify = useNotify()
const SCROLL_SAVE_DELAY = 160
const SELECTION_DRAG_THRESHOLD = 4
const SELECTION_EDGE_SIZE = 40
const SELECTION_MAX_SCROLL_SPEED = 18

let scrollSaveTimerId = 0
const initialScrollTop = trafficStore.scrollTop
let latestScrollTop = initialScrollTop
let initialScrollPending = initialScrollTop > 0
let disposed = false
let revealingLiveEntries = false

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

const sortEntries = createTrafficTableSorter()
const sortedEntries = computed(() => sortEntries(filterStore.filteredEntries, sortConfig.value))
const rowCount = computed(() => sortedEntries.value.length)
const pendingLiveEntryCount = computed(() =>
  'pendingLiveEntryCount' in trafficStore ? trafficStore.pendingLiveEntryCount : 0,
)
const pendingLiveEntriesIcon = computed(() => {
  const edge = getTrafficLatestEdge(sortConfig.value)
  if (edge === 'start') return 'i-lucide-arrow-up-to-line'
  if (edge === 'end') return 'i-lucide-arrow-down-to-line'
  return 'i-lucide-refresh-cw'
})

function isAtLatestEdge(element: HTMLElement) {
  return isTrafficAtLatestEdge(
    sortConfig.value, element.scrollTop, element.scrollHeight, element.clientHeight,
  )
}

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
    count: rowCount.value,
    getScrollElement: () => scrollRef.value,
    estimateSize: () => ROW_HEIGHT,
    overscan: ROW_OVERSCAN,
    rangeExtractor: (range) => getTrafficVirtualRange(range, scrollRef.value?.clientHeight ?? 480),
    // Fixed-size slots use the default index key. Request IDs belong to the
    // keyed DOM rows, so payload/order changes do not rebuild all measurements.
    initialOffset: initialScrollTop,
    initialRect: {
      width: 0,
      height: 480,
    },
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
const virtualPaddingTop = computed(() => virtualRows.value[0]?.virtualRow.start ?? 0)
// A filtered list that fits the viewport should not retain a forced scrolling
// layer. Keep the scrolling hint for long lists, including horizontal-bar space.
const needsScrollLayer = computed(() => {
  const measuredHeight = rowVirtualizer.value.scrollRect?.height ?? 0
  const viewportHeight = Math.min(measuredHeight, scrollRef.value?.clientHeight ?? measuredHeight)
  return viewportHeight > 0 && virtualContentHeight.value > viewportHeight
})

function scrollToOffset(top: number) {
  const el = scrollRef.value
  if (!el) return
  const target = Math.max(0, Math.min(top, el.scrollHeight - el.clientHeight))
  latestScrollTop = target
  persistScrollTop(target)
  rowVirtualizer.value.scrollToOffset(target)
}

function restoreInitialScroll() {
  if (!initialScrollPending || !scrollRef.value?.clientHeight || rowCount.value === 0) return
  initialScrollPending = false
  scrollToOffset(initialScrollTop)
  if (!isAtLatestEdge(scrollRef.value)) {
    pauseLiveEntryEviction()
  }
}

function handleScrollIntent() {
  initialScrollPending = false
}

// Restore once when a visible layout exists, including panes initially hidden
// with v-show. User input cancels restoration; no delayed timer can pull it back.
onMounted(restoreInitialScroll)
watch([rowCount, () => rowVirtualizer.value.scrollRect?.height], restoreInitialScroll, { flush: 'post' })

onUnmounted(() => {
  disposed = true
  initialScrollPending = false
  clearTimeout(scrollSaveTimerId)
  cancelColumnDragUnlock()
  finishSelectionDrag(false, false)
  clearCompatibilityClickSuppression()
  resumeLiveEntryEvictionIfIdle()
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
    initialScrollPending = false
    latestScrollTop = nextScrollTop
    schedulePersistScrollTop()
    if (!revealingLiveEntries && !isAtLatestEdge(target)) {
      pauseLiveEntryEviction()
    }
  }
}

function handleTrafficMouseEnter() {
  isTrafficHovered = true
  pauseLiveEntryEviction()
}

function pauseLiveEntryEviction() {
  if ('pauseLiveEntryEviction' in trafficStore) {
    trafficStore.pauseLiveEntryEviction()
  }
}

function handleTrafficMouseLeave() {
  isTrafficHovered = false
  if (!selectionDrag) {
    resumeLiveEntryEvictionIfIdle()
  }
}

function resumeLiveEntryEvictionIfIdle() {
  const element = scrollRef.value
  if (
    pendingLiveEntryCount.value > 0 ||
    (element && !isAtLatestEdge(element))
  ) return
  if ('resumeLiveEntryEviction' in trafficStore) {
    trafficStore.resumeLiveEntryEviction()
  }
}

async function showPendingLiveEntries() {
  if (!('resumeLiveEntryEviction' in trafficStore) || revealingLiveEntries) return
  handleScrollIntent()
  finishSelectionDrag(false, false)
  revealingLiveEntries = true
  trafficStore.resumeLiveEntryEviction()
  // The user explicitly leaves the old window. Locate the newest request that
  // matches the current filters, even when the table is sorted descending.
  const newestId = trafficStore.entries.reduce((id, entry) => Math.max(id, entry.id), 0)
  await nextTick()
  if (!disposed) {
    let index = sortedEntries.value.findIndex((entry) => entry.id === newestId)
    if (index === -1) {
      let newestMatchingId = 0
      sortedEntries.value.forEach((entry, entryIndex) => {
        if (entry.id > newestMatchingId) {
          newestMatchingId = entry.id
          index = entryIndex
        }
      })
    }
    if (index >= 0) scrollToOffset(index * ROW_HEIGHT)
    if (isTrafficHovered) pauseLiveEntryEviction()
  }
  revealingLiveEntries = false
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
    height: `${virtualRow.size}px`,
  }

  if (!color || isEntrySelected(row.id)) {
    return style
  }

  const backgroundAlpha = themeStore.isDark ? '59' : '26'
  const outlineAlpha = themeStore.isDark ? '73' : '40'
  style.backgroundColor = `${color}${backgroundAlpha}`
  // Paint the marker without changing cell widths when selection overrides it.
  style.boxShadow = `inset 3px 0 0 ${color}, inset 0 0 0 1px ${color}${outlineAlpha}`
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
      resumeLiveEntryEvictionIfIdle()
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
    resumeLiveEntryEvictionIfIdle()
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

const viewOrderContext = computed(() =>
  JSON.stringify([
    dataContextKey.value,
    sortConfig.value.key,
    sortConfig.value.order,
    filterStore.searchText,
    filterStore.activeFilterTab,
    filterStore.selectedHosts,
    filterStore.selectedProcessKeys,
  ]),
)

watch(
  [
    sortedEntries,
    viewOrderContext,
    () => 'liveEntryEvictionVersion' in trafficStore ? trafficStore.liveEntryEvictionVersion : 0,
  ],
  async ([newList, context, eviction], [oldList, oldContext, oldEviction], onCleanup) => {
    if (eviction <= oldEviction || context !== oldContext || sortConfig.value.key) return
    if (
      revealingLiveEntries || trafficStore.pendingFocusEntryId || columnDragStartScrollTop !== null
    ) return
    const element = scrollRef.value
    if (!element) return
    const prevScrollTop = element.scrollTop
    const targetScrollTop = getTrafficEvictionScrollTop(
      oldList, newList, prevScrollTop, element.clientHeight,
    )
    if (targetScrollTop === null || targetScrollTop === prevScrollTop) return
    let cancelled = false
    onCleanup(() => { cancelled = true })
    await nextTick()
    if (!cancelled && !disposed && !revealingLiveEntries && element.scrollTop === prevScrollTop) {
      scrollToOffset(targetScrollTop)
    }
  },
  { flush: 'pre' },
)
</script>

<template>
  <div
    class="relative flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-app-panel"
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
        class="virtual-list min-h-0 flex-1 overflow-x-auto overflow-y-auto bg-app-panel overscroll-contain scrollbar-gutter-stable [overflow-anchor:none]"
        :class="{ 'will-change-scroll': needsScrollLayer }"
        @scroll="handleScroll"
        @wheel.passive="handleScrollIntent"
        @touchstart.passive="handleScrollIntent"
        @pointerdown.capture="handleScrollIntent"
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
        :style="{ height: `${virtualContentHeight}px`, paddingTop: `${virtualPaddingTop}px` }"
      >
        <!-- Fixed-height virtual rows stay in normal flow. One leading spacer
             locates the visible range without a transform on each hovered row. -->
        <!-- Row hover backgrounds and the selection transition are both disabled
             to avoid WebView2 repaint jitter. Row background/box-shadow changes
             apply instantly on the next paint instead of animating. -->
        <div
          v-for="{ virtualRow, item } in virtualRows"
          :key="item.id"
          v-memo="[
            item,
            virtualRow.index,
            virtualRow.start,
            virtualRow.size,
            isEntrySelected(item.id),
            selectedEntryId === item.id,
            getHighlightColor(item),
            themeStore.isDark,
            columnsLayoutKey,
          ]"
          class="traffic-row flex h-8 w-full min-w-full select-none items-center overflow-hidden contain-[layout_style]"
          :data-entry-id="item.id"
          :data-virtual-index="virtualRow.index"
          :class="{
            'selected-row bg-(--traffic-selected-bg)!': isEntrySelected(item.id),
            'focused-row shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_var(--traffic-selected-outline)]!':
              selectedEntryId === item.id,
            'bg-app-panel': virtualRow.index % 2 === 1,
            'bg-[color-mix(in_srgb,var(--app-elevated-bg)_52%,var(--app-panel-bg))]':
              virtualRow.index % 2 === 0,
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
    <UButton
      v-if="pendingLiveEntryCount > 0"
      class="absolute bottom-3 left-1/2 z-20 max-w-[calc(100%-2rem)] -translate-x-1/2 shadow-lg"
      :icon="pendingLiveEntriesIcon"
      size="sm"
      :label="t('traffic.show_new_requests', { count: pendingLiveEntryCount })"
      @click="showPendingLiveEntries"
    />
  </div>
</template>
