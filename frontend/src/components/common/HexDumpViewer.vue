<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, shallowRef, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContextMenuItem } from '@nuxt/ui'
import AppLoading from '@/components/common/AppLoading.vue'
import HexDumpRow from '@/components/common/HexDumpRow.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { type HexByte, type HexdumpBytes } from '@/utils/hexdump'
import { useHexdumpDecoder } from '@/composables/useHexdumpDecoder'
import { getHexdumpPage, getHexdumpPosition, HEX_PADDING_TOP } from '@/utils/hexdumpViewport'
import { getHexdumpLayout, getHexdumpPointerByte } from '@/utils/hexdumpLayout'
import {
  escapeHexdumpClipboardNullBytes,
  serializeHexdumpSelection,
  type HexdumpCopyFormat,
} from '@/utils/hexdumpSelection'
import { copyText } from '@/utils/clipboard'
import { getErrorMessage } from '@/utils/dialog'
import { useNotify } from '@/composables/useNotify'

const props = withDefaults(
  defineProps<{
    input: string | Uint8Array
    isBase64?: boolean
    active?: boolean
    appendOnly?: boolean
    rowHeight?: number
    exporting?: boolean
  }>(),
  {
    isBase64: false,
    active: true,
    appendOnly: false,
    rowHeight: 22,
    exporting: false,
  },
)

const emit = defineEmits<{ exportSelection: [base64: string] }>()

const byteOffset = defineModel<number>('byteOffset', { default: 0 })

watch(
  () => [props.input, props.isBase64] as const,
  (next, previous) => {
    // Standalone users (WebSocket message details) do not own a position model.
    // Only an append within the same stream may inherit another input's offset.
    if (
      props.appendOnly &&
      !next[1] &&
      !previous[1] &&
      typeof next[0] === 'string' &&
      typeof previous[0] === 'string' &&
      next[0].length > previous[0].length
    )
      return
    byteOffset.value = 0
    clearSelection()
    cancelOperation()
  },
)

interface HexDumpLine {
  offset: number
  offsetHex: string
  byteLength: number
  bytes: (HexByte | null)[]
}

interface HexVirtualRow {
  index: number
  key: number
  size: number
  start: number
}

interface HexRenderedRows {
  revision: number
  bytesPerRow: number
  offsetDigits: number
  rows: { virtualRow: HexVirtualRow; line: HexDumpLine }[]
}

const WIDTH_FALLBACK = 720
const SCROLLBAR_GUTTER_WIDTH = 14
const NO_SCROLL_FRAME_GUTTER_WIDTH = 10
const CHAR_WIDTH_FALLBACK = 7.5
const ROW_OVERSCAN = 10
const DRAG_THRESHOLD = 4
const { t } = useI18n()
const notify = useNotify()
const rootRef = useTemplateRef<HTMLElement>('hexRoot')
const scrollRef = useTemplateRef<HTMLElement>('hexScroll')
const measureRef = useTemplateRef<HTMLElement>('hexMeasure')
const viewportHeight = shallowRef(500)
const containerWidth = shallowRef(WIDTH_FALLBACK)
const scrollTop = shallowRef(0)
const charWidth = shallowRef(CHAR_WIDTH_FALLBACK)
const hoveredByteIdx = shallowRef(-1)
const hasVerticalScrollbar = shallowRef(false)
const isVisible = shallowRef(false)
const isDocumentVisible = shallowRef(typeof document === 'undefined' || !document.hidden)
const selectionAnchor = shallowRef<number | null>(null)
const selectionFocus = shallowRef<number | null>(null)
const operationBusy = shallowRef(false)
let operationController: AbortController | null = null
let drag: {
  pointerId: number
  column: 'hex' | 'ascii'
  startX: number
  startY: number
  x: number
  y: number
  active: boolean
} | null = null
let dragFrame: number | null = null
const decoder = useHexdumpDecoder(
  {
    get input() {
      return props.input
    },
    get isBase64() {
      return props.isBase64
    },
    get appendOnly() {
      return props.appendOnly
    },
    get active() {
      return props.active && isVisible.value && isDocumentVisible.value
    },
  },
  () =>
    new Worker(new URL('../../workers/hexdumpDecoder.worker.ts', import.meta.url), {
      type: 'module',
    }),
)
const { bytes, state: decodeState, error: decodeError } = decoder
const pageIndex = shallowRef(0)
let resizeObserver: ResizeObserver | null = null
let visibilityObserver: IntersectionObserver | null = null
let restoringPosition = false

const layout = computed(() =>
  getHexdumpLayout(
    containerWidth.value,
    charWidth.value,
    bytes.value.length,
    frameGutterWidth.value,
  ),
)
const page = computed(() =>
  getHexdumpPage(bytes.value.length, layout.value.bytesPerRow, props.rowHeight, pageIndex.value),
)
const frameGutterWidth = computed(() =>
  hasVerticalScrollbar.value ? SCROLLBAR_GUTTER_WIDTH : NO_SCROLL_FRAME_GUTTER_WIDTH,
)
const shellStyle = computed(() => ({
  '--hex-frame-gutter': `${frameGutterWidth.value}px`,
}))

const visibleRowRange = computed<{ startIndex: number; endIndex: number }>((previous) => {
  const startIndex = Math.max(
    0,
    Math.floor(Math.max(0, scrollTop.value - HEX_PADDING_TOP) / props.rowHeight) - ROW_OVERSCAN,
  )
  const endIndex = Math.min(
    page.value.rowCount,
    Math.ceil(
      Math.max(0, scrollTop.value + viewportHeight.value - HEX_PADDING_TOP) / props.rowHeight,
    ) + ROW_OVERSCAN,
  )
  // Pixel scrolling within the same row window must not rebuild byte data.
  if (previous?.startIndex === startIndex && previous.endIndex === endIndex) return previous
  return { startIndex, endIndex }
})

const renderedRows = computed<HexRenderedRows>((previous) => {
  const rows: HexRenderedRows['rows'] = []
  const { startIndex, endIndex } = visibleRowRange.value
  const sourceBytes = bytes.value
  const bytesPerRow = layout.value.bytesPerRow
  const revision = decoder.revision.value
  const rowHeight = props.rowHeight
  const startRow = page.value.startRow
  const offsetDigits = layout.value.offsetDigits
  const canReuse =
    previous?.revision === revision &&
    previous.bytesPerRow === bytesPerRow &&
    previous.offsetDigits === offsetDigits
  // Retain only the preceding viewport, never a growing cache of visited rows
  // or a reference to the full byte store. A new decode invalidates all rows.
  const cached = new Map(canReuse ? previous.rows.map((row) => [row.line.offset, row]) : [])

  for (let index = startIndex; index < endIndex; index++) {
    const globalRow = startRow + index
    const offset = globalRow * bytesPerRow
    const start = HEX_PADDING_TOP + index * rowHeight
    const oldRow = cached.get(offset)
    const byteLength = Math.min(bytesPerRow, sourceBytes.length - offset)
    // Appending may fill the last, previously padded row. Earlier rows keep
    // their identity so their byte nodes do not need another render.
    const line =
      oldRow?.line.byteLength === byteLength
        ? oldRow.line
        : buildRow(sourceBytes, globalRow, bytesPerRow)
    if (
      oldRow?.line === line &&
      oldRow.virtualRow.start === start &&
      oldRow.virtualRow.size === rowHeight
    ) {
      rows.push(oldRow)
      continue
    }
    rows.push({
      virtualRow: {
        index: globalRow,
        key: offset,
        size: rowHeight,
        start,
      },
      line,
    })
  }

  if (
    canReuse &&
    rows.length === previous.rows.length &&
    rows.every((row, index) => row === previous.rows[index])
  ) {
    return previous
  }
  return { revision, bytesPerRow, offsetDigits, rows }
})
const virtualRows = computed(() => renderedRows.value.rows)

const virtualContentHeight = computed(() => page.value.height)
const isDecoding = computed(() => decodeState.value === 'loading')
const hasDecodeError = computed(() => decodeState.value === 'error')

const viewerStyle = computed(() => ({
  height: `${virtualContentHeight.value}px`,
  '--hex-row-height': `${props.rowHeight}px`,
  '--hex-bytes-per-row': String(layout.value.bytesPerRow),
  '--hex-offset-width': `${layout.value.offsetWidth}px`,
  minWidth: `${layout.value.asciiStart + layout.value.bytesPerRow * charWidth.value}px`,
}))

const selection = computed(() => {
  if (selectionAnchor.value === null || selectionFocus.value === null) return null
  return {
    start: Math.min(selectionAnchor.value, selectionFocus.value),
    endExclusive: Math.max(selectionAnchor.value, selectionFocus.value) + 1,
  }
})
const canSelect = computed(
  () =>
    props.active &&
    isVisible.value &&
    isDocumentVisible.value &&
    decodeState.value === 'ready' &&
    bytes.value.length > 0,
)
const canOperate = computed(
  () => canSelect.value && selection.value !== null && !operationBusy.value && !props.exporting,
)
const menuItems = computed<ContextMenuItem[]>(() => [
  {
    label: t('detail.hex_copy_text'),
    disabled: !canOperate.value,
    onSelect: () => void runSelectionOperation('text'),
  },
  {
    label: t('detail.hex_copy_hex'),
    disabled: !canOperate.value,
    onSelect: () => void runSelectionOperation('hex'),
  },
  {
    label: t('detail.hex_copy_base64'),
    disabled: !canOperate.value,
    onSelect: () => void runSelectionOperation('base64'),
  },
  { type: 'separator' },
  {
    label: t('detail.hex_export'),
    disabled: !canOperate.value,
    onSelect: () => void runSelectionOperation('base64', true),
  },
])

function rowSelection(offset: number, byteLength: number) {
  const range = selection.value
  if (!range || range.endExclusive <= offset || range.start >= offset + byteLength)
    return { start: -1, endExclusive: -1 }
  return {
    start: Math.max(offset, range.start),
    endExclusive: Math.min(offset + byteLength, range.endExclusive),
  }
}

function clearSelection() {
  stopDrag()
  selectionAnchor.value = null
  selectionFocus.value = null
}

function cancelOperation() {
  operationController?.abort()
  operationController = null
  operationBusy.value = false
}

async function runSelectionOperation(format: HexdumpCopyFormat, exporting = false) {
  const range = selection.value
  if (!canOperate.value || !range) return
  const controller = new AbortController()
  operationController = controller
  operationBusy.value = true
  try {
    const content = await serializeHexdumpSelection(
      bytes.value,
      range.start,
      range.endExclusive,
      format,
      controller.signal,
      props.isBase64 || props.input instanceof Uint8Array ? 'binary' : 'utf8',
    )
    controller.signal.throwIfAborted()
    if (exporting) emit('exportSelection', content)
    else {
      const clipboardContent =
        format === 'text'
          ? await escapeHexdumpClipboardNullBytes(content, controller.signal)
          : content
      controller.signal.throwIfAborted()
      await copyText(clipboardContent)
      if (!controller.signal.aborted) notify.success(t('detail.hex_selection_copied'))
    }
  } catch (error) {
    if (!controller.signal.aborted)
      notify.error(
        t(exporting ? 'detail.body_save_failed' : 'detail.body_copy_failed', {
          error: getErrorMessage(error),
        }),
      )
  } finally {
    if (operationController === controller) {
      operationController = null
      operationBusy.value = false
    }
  }
}

function focusHex() {
  if (canSelect.value) scrollRef.value?.focus({ preventScroll: true })
}

function handleMenuClose(event: Event) {
  event.preventDefault()
  focusHex()
}

function buildRow(sourceBytes: HexdumpBytes, rowIndex: number, bytesPerRow: number): HexDumpLine {
  const offset = rowIndex * bytesPerRow
  const rowBytes: (HexByte | null)[] = []

  for (let index = 0; index < bytesPerRow; index++) {
    const globalIdx = offset + index
    const value = sourceBytes.get(globalIdx)
    rowBytes.push(
      value == null
        ? null
        : {
            hex: formatHexByte(value),
            ascii: formatAsciiByte(value),
            value,
            globalIdx,
          },
    )
  }

  return {
    offset,
    offsetHex: offset.toString(16).toUpperCase().padStart(layout.value.offsetDigits, '0'),
    byteLength: Math.min(bytesPerRow, sourceBytes.length - offset),
    bytes: rowBytes,
  }
}

function formatHexByte(value: number) {
  return value.toString(16).padStart(2, '0').toUpperCase()
}

function formatAsciiByte(value: number) {
  return value >= 0x20 && value < 0x7f ? String.fromCharCode(value) : '.'
}

// both 0 means an ancestor (hidden tab panel) collapsed our layout box, not a real resize
function isElementHidden(element: HTMLElement) {
  return element.clientWidth === 0 && element.clientHeight === 0
}

function updateViewportMetrics() {
  const element = scrollRef.value
  if (!element || isElementHidden(element)) return

  const measureWidth = measureRef.value?.getBoundingClientRect().width ?? 0
  if (measureWidth > 0) {
    charWidth.value = measureWidth / 10
  }

  containerWidth.value = element.clientWidth || WIDTH_FALLBACK
  viewportHeight.value = element.clientHeight || 500
  if (!restoringPosition) scrollTop.value = element.scrollTop || 0

  nextTick(updateScrollbarPresence)
}

function updateScrollbarPresence() {
  const element = scrollRef.value
  if (!element) return

  hasVerticalScrollbar.value = element.scrollHeight > element.clientHeight + 1
}

function observeCurrentScrollElement() {
  updateViewportMetrics()
  if (!scrollRef.value) return

  resizeObserver?.disconnect()
  resizeObserver = new ResizeObserver(() => {
    updateViewportMetrics()
  })
  resizeObserver.observe(scrollRef.value)
  if (measureRef.value) resizeObserver.observe(measureRef.value)
}

function handleHexMouseOver(event: MouseEvent) {
  const target = (event.target as HTMLElement).closest<HTMLElement>('[data-byte-idx]')
  if (!target || !scrollRef.value?.contains(target)) return

  const nextHoveredIdx = Number(target.dataset.byteIdx)
  if (Number.isInteger(nextHoveredIdx) && nextHoveredIdx !== hoveredByteIdx.value) {
    hoveredByteIdx.value = nextHoveredIdx
  }
}

function handlePointerDown(event: PointerEvent) {
  if (event.button !== 0 || !canSelect.value) return
  const element = scrollRef.value
  if (!element) return
  const target = (event.target as HTMLElement).closest<HTMLElement>('[data-byte-idx]')
  focusHex()
  if (!target || !element.contains(target)) {
    clearSelection()
    return
  }
  const index = Number(target.dataset.byteIdx)
  if (!Number.isInteger(index) || index < 0 || index >= bytes.value.length) return
  event.preventDefault()
  stopDrag()
  if (!event.shiftKey || selectionAnchor.value === null) selectionAnchor.value = index
  selectionFocus.value = index
  drag = {
    pointerId: event.pointerId,
    column: target.dataset.byteColumn === 'ascii' ? 'ascii' : 'hex',
    startX: event.clientX,
    startY: event.clientY,
    x: event.clientX,
    y: event.clientY,
    active: false,
  }
  element.setPointerCapture(event.pointerId)
}

function updateDragSelection() {
  const element = scrollRef.value
  if (!element || !drag?.active || restoringPosition) return
  const rect = element.getBoundingClientRect()
  const index = getHexdumpPointerByte(
    drag.x - rect.left - 8 + element.scrollLeft,
    drag.y - rect.top + element.scrollTop - HEX_PADDING_TOP,
    drag.column,
    layout.value,
    charWidth.value,
    props.rowHeight,
    page.value.startRow,
    page.value.rowCount,
    bytes.value.length,
  )
  if (index >= 0) selectionFocus.value = index
}

function handlePointerMove(event: PointerEvent) {
  if (!drag || drag.pointerId !== event.pointerId) return
  if ((event.buttons & 1) === 0) {
    stopDrag()
    return
  }
  drag.x = event.clientX
  drag.y = event.clientY
  // Keep the DOM byte selected on a click. Recomputing its coordinates on a
  // slight move or release can resolve to a different row in the WebView.
  if (!drag.active) {
    if (Math.hypot(drag.x - drag.startX, drag.y - drag.startY) < DRAG_THRESHOLD) return
    drag.active = true
  }
  updateDragSelection()
  if (dragFrame === null) dragFrame = requestAnimationFrame(autoScrollSelection)
}

function stopDrag() {
  const previous = drag
  drag = null
  if (dragFrame !== null) cancelAnimationFrame(dragFrame)
  dragFrame = null
  if (previous && scrollRef.value?.hasPointerCapture(previous.pointerId))
    scrollRef.value.releasePointerCapture(previous.pointerId)
}

function handlePointerUp(event: PointerEvent) {
  if (drag?.pointerId !== event.pointerId) return
  if (drag.active) {
    drag.x = event.clientX
    drag.y = event.clientY
    updateDragSelection()
  }
  stopDrag()
}

function autoScrollSelection() {
  dragFrame = null
  const element = scrollRef.value
  if (!drag?.active || !element || !canSelect.value) {
    stopDrag()
    return
  }
  const rect = element.getBoundingClientRect()
  const delta =
    drag.y < rect.top + 28
      ? -Math.min(24, rect.top + 28 - drag.y)
      : drag.y > rect.bottom - 28
        ? Math.min(24, drag.y - rect.bottom + 28)
        : 0
  if (delta && !restoringPosition) {
    const before = element.scrollTop
    scrollToOffset(before + delta)
    if (before === element.scrollTop) {
      if (delta > 0 && page.value.index < page.value.count - 1) changePage(page.value.index + 1)
      else if (delta < 0 && page.value.index > 0) {
        const previousPage = getHexdumpPage(
          bytes.value.length,
          layout.value.bytesPerRow,
          props.rowHeight,
          page.value.index - 1,
        )
        byteOffset.value =
          (previousPage.startRow + previousPage.rowCount - 1) * layout.value.bytesPerRow
        void restorePosition()
      }
    } else handleHexScroll({ currentTarget: element } as unknown as Event)
    if (!restoringPosition) updateDragSelection()
  }
  dragFrame = requestAnimationFrame(autoScrollSelection)
}

function handleHexScroll(event: Event) {
  if (restoringPosition || decodeState.value !== 'ready') return
  scrollTop.value = (event.currentTarget as HTMLElement).scrollTop
  hoveredByteIdx.value = -1
  const row =
    page.value.startRow +
    Math.floor(Math.max(0, scrollTop.value - HEX_PADDING_TOP) / props.rowHeight)
  byteOffset.value = row * layout.value.bytesPerRow
}

function scrollToOffset(top: number) {
  const element = scrollRef.value
  if (!element) return

  const maxScrollTop = Math.max(0, virtualContentHeight.value - viewportHeight.value)
  const nextScrollTop = Math.min(Math.max(0, top), maxScrollTop)
  element.scrollTop = nextScrollTop
  if (typeof element.scrollTo === 'function') {
    element.scrollTo({ top: nextScrollTop })
  }
  scrollTop.value = nextScrollTop
}

async function restorePosition(offset = byteOffset.value) {
  if (decodeState.value !== 'ready') return
  restoringPosition = true
  const position = getHexdumpPosition(
    offset,
    bytes.value.length,
    layout.value.bytesPerRow,
    props.rowHeight,
  )
  pageIndex.value = position.page
  await nextTick()
  scrollToOffset(position.top)
  restoringPosition = false
}

function changePage(index: number) {
  const nextPage = getHexdumpPage(
    bytes.value.length,
    layout.value.bytesPerRow,
    props.rowHeight,
    index,
  )
  const offset = nextPage.startRow * layout.value.bytesPerRow
  byteOffset.value = offset
  hoveredByteIdx.value = -1
  void restorePosition(offset)
}

watch(
  () => decoder.revision.value,
  () => {
    nextTick(() => {
      observeCurrentScrollElement()
      void restorePosition()
    })
  },
)

watch(
  () => [virtualContentHeight.value, viewportHeight.value] as const,
  () => {
    nextTick(updateScrollbarPresence)
  },
)

watch(
  () => [layout.value.bytesPerRow, props.rowHeight] as const,
  () => {
    void restorePosition()
  },
)

onMounted(() => {
  // Workspace and response panels use v-show. Their descendants remain mounted,
  // so props.active alone does not tell us whether decoding is still useful.
  const root = rootRef.value
  if (root) {
    isVisible.value = root.clientWidth > 0 && root.clientHeight > 0
    if (typeof IntersectionObserver !== 'undefined') {
      visibilityObserver = new IntersectionObserver(([entry]) => {
        if (!entry) return
        isVisible.value =
          entry.isIntersecting &&
          entry.boundingClientRect.width > 0 &&
          entry.boundingClientRect.height > 0
      })
      visibilityObserver.observe(root)
    }
  }
  if (typeof document !== 'undefined')
    document.addEventListener('visibilitychange', updateDocumentVisibility)
  nextTick(() => {
    observeCurrentScrollElement()
    void restorePosition()
  })
})

onUnmounted(() => {
  clearSelection()
  cancelOperation()
  resizeObserver?.disconnect()
  visibilityObserver?.disconnect()
  if (typeof document !== 'undefined')
    document.removeEventListener('visibilitychange', updateDocumentVisibility)
})

watch(canSelect, (active) => {
  if (!active) {
    stopDrag()
    cancelOperation()
  }
})

function updateDocumentVisibility() {
  isDocumentVisible.value = !document.hidden
}
</script>

<template>
  <div
    ref="hexRoot"
    class="relative flex h-full min-h-0 flex-col before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:right-(--hex-frame-gutter) before:border before:border-app-border before:content-['']"
    :style="shellStyle"
  >
    <span
      ref="hexMeasure"
      class="invisible pointer-events-none absolute left-[-9999px] top-0 whitespace-pre text-sm"
      style="font-family: var(--code-font-family)"
      aria-hidden="true"
      >0000000000</span
    >

    <div
      v-if="page.count > 1 && decodeState === 'ready'"
      class="flex shrink-0 items-center justify-end gap-1 border-b border-app-border px-3 py-1 mr-(--hex-frame-gutter)"
    >
      <UButton
        icon="i-lucide-chevrons-left"
        size="xs"
        color="neutral"
        variant="ghost"
        :disabled="page.index === 0"
        :aria-label="t('detail.hex_first_page')"
        @click="changePage(0)"
      />
      <UButton
        icon="i-lucide-chevron-left"
        size="xs"
        color="neutral"
        variant="ghost"
        :disabled="page.index === 0"
        :aria-label="t('detail.hex_previous_page')"
        @click="changePage(page.index - 1)"
      />
      <span class="text-xs tabular-nums text-app-text-muted" role="status">{{
        t('detail.hex_page', { current: page.index + 1, total: page.count })
      }}</span>
      <UButton
        icon="i-lucide-chevron-right"
        size="xs"
        color="neutral"
        variant="ghost"
        :disabled="page.index === page.count - 1"
        :aria-label="t('detail.hex_next_page')"
        @click="changePage(page.index + 1)"
      />
      <UButton
        icon="i-lucide-chevrons-right"
        size="xs"
        color="neutral"
        variant="ghost"
        :disabled="page.index === page.count - 1"
        :aria-label="t('detail.hex_last_page')"
        @click="changePage(page.count - 1)"
      />
    </div>

    <AppLoading v-if="isDecoding" fill :label="t('detail.hex_loading')" />

    <UEmpty
      v-else-if="hasDecodeError"
      icon="i-lucide-circle-alert"
      :title="t('detail.hex_decode_failed')"
      :description="decodeError"
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />

    <UContextMenu
      v-else
      :items="menuItems"
      :content="{ onCloseAutoFocus: handleMenuClose }"
    >
      <div
        ref="hexScroll"
        tabindex="0"
        :aria-label="t('detail.hex')"
        class="min-h-0 flex-1 select-none overflow-auto pl-2 pr-[calc(8px+var(--hex-frame-gutter))] outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-app-accent"
        @scroll="handleHexScroll"
        @mouseover="handleHexMouseOver"
        @mouseleave="hoveredByteIdx = -1"
        @pointerdown="handlePointerDown"
        @pointermove="handlePointerMove"
        @pointerup="handlePointerUp"
        @pointercancel="stopDrag"
        @lostpointercapture="stopDrag"
        @contextmenu="stopDrag"
      >
        <div
          class="relative min-w-0 text-sm"
          :style="{ ...viewerStyle, fontFamily: 'var(--code-font-family)' }"
        >
          <HexDumpRow
            v-for="{ virtualRow, line } in virtualRows"
            :key="virtualRow.key"
            :line="line"
            :top="virtualRow.start"
            :row-height="virtualRow.size"
            :hovered-byte-idx="
              hoveredByteIdx >= line.offset && hoveredByteIdx < line.offset + line.byteLength
                ? hoveredByteIdx
                : -1
            "
            :selection-start="rowSelection(line.offset, line.byteLength).start"
            :selection-end="rowSelection(line.offset, line.byteLength).endExclusive"
          />
        </div>
      </div>
    </UContextMenu>
  </div>
</template>
