<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, shallowRef, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLoading from '@/components/common/AppLoading.vue'
import HexDumpRow from '@/components/common/HexDumpRow.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { type HexByte, type HexdumpBytes } from '@/utils/hexdump'
import { useHexdumpDecoder } from '@/composables/useHexdumpDecoder'
import { getHexdumpPage, getHexdumpPosition, HEX_PADDING_TOP } from '@/utils/hexdumpViewport'

const props = withDefaults(
  defineProps<{
    input: string | Uint8Array
    isBase64?: boolean
    active?: boolean
    appendOnly?: boolean
    rowHeight?: number
    showInfoBar?: boolean
  }>(),
  {
    isBase64: false,
    active: true,
    appendOnly: false,
    rowHeight: 22,
    showInfoBar: true,
  },
)

const byteOffset = defineModel<number>('byteOffset', { default: 0 })

watch(
  () => [props.input, props.isBase64] as const,
  (next, previous) => {
    // Standalone users (WebSocket message details) do not own a position model.
    // Only an append within the same stream may inherit another input's offset.
    if (props.appendOnly && !next[1] && !previous[1] && next[0].length > previous[0].length) return
    byteOffset.value = 0
  },
)

interface HexDumpLine {
  offset: number
  offsetHex: string
  byteLength: number
  bytes: (HexByte | null)[]
}

interface HexDumpLayout {
  bytesPerRow: number
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
  rows: { virtualRow: HexVirtualRow; line: HexDumpLine }[]
}

const WIDTH_FALLBACK = 720
const VIEWER_HORIZONTAL_PADDING = 24
const SCROLLBAR_GUTTER_WIDTH = 14
const NO_SCROLL_FRAME_GUTTER_WIDTH = 10
const OFFSET_COLUMN_WIDTH = 82
const SECTION_GAP = 10
const HEX_BYTE_GAP = 2
const CHAR_WIDTH_FALLBACK = 7.5
const ROW_OVERSCAN = 10
const MAX_BYTES_PER_ROW = 64
const { t } = useI18n()
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

const layout = computed<HexDumpLayout>(() => chooseLayout(containerWidth.value))
const page = computed(() =>
  getHexdumpPage(bytes.value.length, layout.value.bytesPerRow, props.rowHeight, pageIndex.value),
)
const frameGutterWidth = computed(() =>
  hasVerticalScrollbar.value ? SCROLLBAR_GUTTER_WIDTH : NO_SCROLL_FRAME_GUTTER_WIDTH,
)
const shellStyle = computed(() => ({
  '--hex-frame-gutter': `${frameGutterWidth.value}px`,
}))

const hoveredByte = computed((): HexByte | null => {
  const value = bytes.value.get(hoveredByteIdx.value)
  if (value == null) return null

  return {
    hex: formatHexByte(value),
    ascii: formatAsciiByte(value),
    value,
    globalIdx: hoveredByteIdx.value,
  }
})

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
  const canReuse = previous?.revision === revision && previous.bytesPerRow === bytesPerRow
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
    const line = oldRow?.line.byteLength === byteLength
      ? oldRow.line
      : buildRow(sourceBytes, globalRow, bytesPerRow)
    if (oldRow?.line === line && oldRow.virtualRow.start === start && oldRow.virtualRow.size === rowHeight) {
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

  if (canReuse && rows.length === previous.rows.length && rows.every((row, index) => row === previous.rows[index])) {
    return previous
  }
  return { revision, bytesPerRow, rows }
})
const virtualRows = computed(() => renderedRows.value.rows)

const virtualContentHeight = computed(() => page.value.height)
const isDecoding = computed(() => decodeState.value === 'loading')
const hasDecodeError = computed(() => decodeState.value === 'error')

const viewerStyle = computed(() => ({
  height: `${virtualContentHeight.value}px`,
  '--hex-row-height': `${props.rowHeight}px`,
  '--hex-bytes-per-row': String(layout.value.bytesPerRow),
}))

function chooseLayout(width: number): HexDumpLayout {
  const usableWidth = Math.max(
    0,
    width -
      frameGutterWidth.value -
      VIEWER_HORIZONTAL_PADDING -
      OFFSET_COLUMN_WIDTH -
      SECTION_GAP * 2,
  )

  for (let candidate = MAX_BYTES_PER_ROW; candidate >= 1; candidate--) {
    const neededWidth =
      candidate * 2 * charWidth.value +
      Math.max(0, candidate - 1) * HEX_BYTE_GAP +
      candidate * charWidth.value
    if (usableWidth >= neededWidth) {
      return { bytesPerRow: candidate }
    }
  }

  return { bytesPerRow: 1 }
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
    offsetHex: offset.toString(16).padStart(8, '0'),
    byteLength: Math.min(bytesPerRow, sourceBytes.length - offset),
    bytes: rowBytes,
  }
}

function formatHexByte(value: number) {
  return value.toString(16).padStart(2, '0')
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
  resizeObserver?.disconnect()
  visibilityObserver?.disconnect()
  if (typeof document !== 'undefined')
    document.removeEventListener('visibilitychange', updateDocumentVisibility)
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
      v-if="props.showInfoBar"
      class="flex min-h-6.5 shrink-0 flex-wrap items-center gap-1.5 bg-app-elevated px-3 py-0.75 text-sm mr-(--hex-frame-gutter) [border-bottom:1px_solid_var(--app-border-color)]"
      style="font-family: var(--code-font-family)"
    >
      <template v-if="hoveredByte">
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">{{ t('detail.hex_offset') }}</span>
          <span class="font-semibold text-app-text"
            >0x{{ hoveredByte.globalIdx.toString(16).padStart(8, '0') }}</span
          >
        </span>
        <span class="text-app-text-muted">·</span>
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">{{ t('detail.hex_value') }}</span>
          <span class="font-semibold text-app-text">0x{{ hoveredByte.hex }}</span>
        </span>
        <span class="text-app-text-muted">·</span>
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">{{ t('detail.hex_decimal') }}</span>
          <span class="font-semibold text-app-text">{{ hoveredByte.value }}</span>
        </span>
        <template v-if="hoveredByte.value >= 0x20 && hoveredByte.value < 0x7f">
          <span class="text-app-text-muted">·</span>
          <span class="inline-flex items-center gap-1">
            <span class="text-app-text-muted">{{ t('detail.hex_character') }}</span>
            <span class="font-semibold text-app-text">{{ hoveredByte.ascii }}</span>
          </span>
        </template>
      </template>
      <span v-else class="text-sm text-app-text-muted">{{ t('detail.hex_hover_hint') }}</span>
    </div>

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

    <div
      v-else
      ref="hexScroll"
      class="min-h-0 flex-1 overflow-x-hidden overflow-y-auto pl-3 pr-[calc(12px+var(--hex-frame-gutter))]"
      @scroll="handleHexScroll"
      @mouseover="handleHexMouseOver"
      @mouseleave="hoveredByteIdx = -1"
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
          :hovered-byte-idx="hoveredByteIdx >= line.offset && hoveredByteIdx < line.offset + line.byteLength ? hoveredByteIdx : -1"
        />
      </div>
    </div>
  </div>
</template>
