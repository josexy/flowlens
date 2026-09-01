<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, shallowRef, useTemplateRef, watch } from 'vue'
import type { CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLoading from '@/components/common/AppLoading.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { decodeHexdumpBytes, estimateDecodedByteLength, type HexByte } from '@/utils/hexdump'

const props = withDefaults(
  defineProps<{
    input: string | Uint8Array
    isBase64?: boolean
    active?: boolean
    rowHeight?: number
    showInfoBar?: boolean
  }>(),
  {
    isBase64: false,
    active: true,
    rowHeight: 22,
    showInfoBar: true,
  },
)

interface HexDumpRow {
  offset: number
  offsetHex: string
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

type DecodeState = 'idle' | 'loading' | 'ready' | 'error'

type HexdumpDecodeResponse =
  | {
      id: number
      ok: true
      buffer: ArrayBuffer
    }
  | {
      id: number
      ok: false
      error: string
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
const VIEWER_PADDING_TOP = 6
const VIEWER_PADDING_BOTTOM = 10
const WORKER_DECODE_THRESHOLD_BYTES = 256 * 1024
const WORKER_CHUNKED_MESSAGE_THRESHOLD_CHARS = 1024 * 1024
const WORKER_MESSAGE_CHUNK_CHARS = 128 * 1024

const { t } = useI18n()
const scrollRef = useTemplateRef<HTMLElement>('hexScroll')
const measureRef = useTemplateRef<HTMLElement>('hexMeasure')
const viewportHeight = shallowRef(500)
const containerWidth = shallowRef(WIDTH_FALLBACK)
const scrollTop = shallowRef(0)
const charWidth = shallowRef(CHAR_WIDTH_FALLBACK)
const hoveredByteIdx = shallowRef(-1)
const hasVerticalScrollbar = shallowRef(false)
const bytes = shallowRef<Uint8Array>(new Uint8Array())
const decodeState = shallowRef<DecodeState>('idle')
const decodeError = shallowRef('')

let resizeObserver: ResizeObserver | null = null
let decodeWorker: Worker | null = null
let decodeRequestId = 0

const layout = computed<HexDumpLayout>(() => chooseLayout(containerWidth.value))
const totalRows = computed(() => Math.ceil(bytes.value.length / layout.value.bytesPerRow))
const frameGutterWidth = computed(() =>
  hasVerticalScrollbar.value ? SCROLLBAR_GUTTER_WIDTH : NO_SCROLL_FRAME_GUTTER_WIDTH,
)
const shellStyle = computed(() => ({
  '--hex-frame-gutter': `${frameGutterWidth.value}px`,
}))

const hoveredByte = computed((): HexByte | null => {
  const value = bytes.value[hoveredByteIdx.value]
  if (value == null) return null

  return {
    hex: formatHexByte(value),
    ascii: formatAsciiByte(value),
    value,
    globalIdx: hoveredByteIdx.value,
  }
})

const visibleRowRange = computed(() => {
  const startIndex = Math.max(
    0,
    Math.floor(Math.max(0, scrollTop.value - VIEWER_PADDING_TOP) / props.rowHeight) - ROW_OVERSCAN,
  )
  const endIndex = Math.min(
    totalRows.value,
    Math.ceil(Math.max(0, scrollTop.value + viewportHeight.value - VIEWER_PADDING_TOP) / props.rowHeight) +
      ROW_OVERSCAN,
  )
  return { startIndex, endIndex }
})

const virtualRows = computed(() => {
  const rows: { virtualRow: HexVirtualRow; line: HexDumpRow }[] = []
  const { startIndex, endIndex } = visibleRowRange.value
  const sourceBytes = bytes.value
  const bytesPerRow = layout.value.bytesPerRow

  for (let index = startIndex; index < endIndex; index++) {
    rows.push({
      virtualRow: {
        index,
        key: index * bytesPerRow,
        size: props.rowHeight,
        start: VIEWER_PADDING_TOP + index * props.rowHeight,
      },
      line: buildRow(sourceBytes, index, bytesPerRow),
    })
  }

  return rows
})

const virtualContentHeight = computed(
  () => VIEWER_PADDING_TOP + totalRows.value * props.rowHeight + VIEWER_PADDING_BOTTOM,
)
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

function buildRow(sourceBytes: Uint8Array, rowIndex: number, bytesPerRow: number): HexDumpRow {
  const offset = rowIndex * bytesPerRow
  const rowBytes: (HexByte | null)[] = []

  for (let index = 0; index < bytesPerRow; index++) {
    const globalIdx = offset + index
    const value = sourceBytes[globalIdx]
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
  scrollTop.value = element.scrollTop || 0

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
  scrollTop.value = (event.currentTarget as HTMLElement).scrollTop
}

function getRowStyle(virtualRow: HexVirtualRow): CSSProperties {
  return {
    height: `${virtualRow.size}px`,
    transform: `translateY(${virtualRow.start}px)`,
  }
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

function resetScroll() {
  hoveredByteIdx.value = -1
  nextTick(() => {
    scrollToOffset(0)
  })
}

function terminateDecodeWorker() {
  decodeWorker?.terminate()
  decodeWorker = null
}

function setDecodedBytes(nextBytes: Uint8Array) {
  bytes.value = nextBytes
  decodeError.value = ''
  decodeState.value = 'ready'
  resetScroll()
}

function setDecodeError(error: string) {
  bytes.value = new Uint8Array()
  decodeError.value = error
  decodeState.value = 'error'
  resetScroll()
}

function pauseDecode() {
  terminateDecodeWorker()
  hoveredByteIdx.value = -1
  bytes.value = new Uint8Array()
  decodeError.value = ''
  decodeState.value = 'idle'
}

function decodeSynchronously(requestId: number) {
  try {
    const decoded = decodeHexdumpBytes(props.input, props.isBase64)
    if (requestId === decodeRequestId) {
      setDecodedBytes(decoded)
    }
  } catch (error) {
    if (requestId === decodeRequestId) {
      setDecodeError(error instanceof Error ? error.message : String(error))
    }
  }
}

function shouldDecodeWithWorker(input: string | Uint8Array, decodedByteLength: number) {
  return (
    typeof Worker !== 'undefined' &&
    typeof input === 'string' &&
    decodedByteLength >= WORKER_DECODE_THRESHOLD_BYTES
  )
}

function waitForBrowserTurn() {
  return new Promise<void>((resolve) => {
    requestAnimationFrame(() => {
      setTimeout(resolve, 0)
    })
  })
}

async function postChunkedDecodeRequest(worker: Worker, input: string, requestId: number) {
  worker.postMessage({
    id: requestId,
    type: 'start',
    isBase64: props.isBase64,
  })

  for (let start = 0; start < input.length; start += WORKER_MESSAGE_CHUNK_CHARS) {
    if (requestId !== decodeRequestId || decodeWorker !== worker) return

    worker.postMessage({
      id: requestId,
      type: 'chunk',
      input: input.slice(start, start + WORKER_MESSAGE_CHUNK_CHARS),
    })

    await waitForBrowserTurn()
  }

  if (requestId !== decodeRequestId || decodeWorker !== worker) return

  worker.postMessage({
    id: requestId,
    type: 'end',
  })
}

function decodeWithWorker(input: string, requestId: number) {
  decodeState.value = 'loading'
  decodeError.value = ''
  bytes.value = new Uint8Array()

  try {
    const worker = new Worker(new URL('../../workers/hexdumpDecoder.worker.ts', import.meta.url), {
      type: 'module',
    })
    decodeWorker = worker

    worker.onmessage = (event: MessageEvent<HexdumpDecodeResponse>) => {
      if (requestId !== decodeRequestId) return

      terminateDecodeWorker()
      const message = event.data
      if (message.ok) {
        setDecodedBytes(new Uint8Array(message.buffer))
        return
      }
      setDecodeError(message.error)
    }

    worker.onerror = (event) => {
      event.preventDefault()
      if (requestId !== decodeRequestId) return

      terminateDecodeWorker()
      setDecodeError(event.message)
    }

    if (input.length >= WORKER_CHUNKED_MESSAGE_THRESHOLD_CHARS) {
      void postChunkedDecodeRequest(worker, input, requestId).catch((error: unknown) => {
        if (requestId !== decodeRequestId) return

        terminateDecodeWorker()
        setDecodeError(error instanceof Error ? error.message : String(error))
      })
      return
    }

    worker.postMessage({
      id: requestId,
      type: 'full',
      input,
      isBase64: props.isBase64,
    })
  } catch {
    terminateDecodeWorker()
    decodeSynchronously(requestId)
  }
}

function startDecode() {
  const requestId = ++decodeRequestId
  terminateDecodeWorker()
  hoveredByteIdx.value = -1

  if (!props.active) {
    pauseDecode()
    return
  }

  const decodedByteLength = estimateDecodedByteLength(props.input, props.isBase64)
  if (!props.input || decodedByteLength === 0) {
    setDecodedBytes(new Uint8Array())
    return
  }

  if (shouldDecodeWithWorker(props.input, decodedByteLength) && typeof props.input === 'string') {
    decodeWithWorker(props.input, requestId)
    return
  }

  decodeState.value = 'idle'
  decodeSynchronously(requestId)
}

watch(() => [props.input, props.isBase64, props.active] as const, startDecode, { immediate: true })

watch(
  () => decodeState.value,
  (state) => {
    if (state === 'ready') {
      nextTick(observeCurrentScrollElement)
    }
  },
)

watch(
  () => [virtualContentHeight.value, viewportHeight.value, layout.value.bytesPerRow] as const,
  () => {
    nextTick(updateScrollbarPresence)
  },
)

watch(
  () => layout.value.bytesPerRow,
  (nextBytesPerRow, previousBytesPerRow) => {
    const currentScrollTop = scrollRef.value?.scrollTop ?? scrollTop.value
    const firstVisibleByte = Math.floor(currentScrollTop / props.rowHeight) * previousBytesPerRow
    const nextScrollTop = Math.floor(firstVisibleByte / nextBytesPerRow) * props.rowHeight
    nextTick(() => {
      scrollToOffset(nextScrollTop)
    })
  },
)

onMounted(() => {
  nextTick(observeCurrentScrollElement)
})

onUnmounted(() => {
  decodeRequestId++
  terminateDecodeWorker()
  resizeObserver?.disconnect()
})
</script>

<template>
  <div
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
      class="flex min-h-6.5 shrink-0 items-center gap-1.5 bg-app-elevated px-3 py-0.75 text-sm mr-(--hex-frame-gutter) [border-bottom:1px_solid_var(--app-border-color)]"
      style="font-family: var(--code-font-family)"
    >
      <template v-if="hoveredByte">
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">Offset</span>
          <span class="font-semibold text-app-text"
            >0x{{ hoveredByte.globalIdx.toString(16).padStart(8, '0') }}</span
          >
        </span>
        <span class="text-app-text-muted">·</span>
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">Hex</span>
          <span class="font-semibold text-app-text">0x{{ hoveredByte.hex }}</span>
        </span>
        <span class="text-app-text-muted">·</span>
        <span class="inline-flex items-center gap-1">
          <span class="text-app-text-muted">Dec</span>
          <span class="font-semibold text-app-text">{{ hoveredByte.value }}</span>
        </span>
        <template v-if="hoveredByte.value >= 0x20 && hoveredByte.value < 0x7f">
          <span class="text-app-text-muted">·</span>
          <span class="inline-flex items-center gap-1">
            <span class="text-app-text-muted">Char</span>
            <span class="font-semibold text-app-text">{{ hoveredByte.ascii }}</span>
          </span>
        </template>
      </template>
      <span v-else class="text-sm text-app-text-muted">Hover over a byte to inspect</span>
    </div>

    <AppLoading
      v-if="isDecoding"
      fill
      :label="t('detail.hex_loading')"
    />

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
      <div class="relative min-w-0 text-sm" :style="{ ...viewerStyle, fontFamily: 'var(--code-font-family)' }">
        <div
          v-for="{ virtualRow, line } in virtualRows"
          :key="String(virtualRow.key)"
          class="absolute left-0 top-0 grid h-(--hex-row-height) w-full min-w-0 grid-cols-[82px_max-content_max-content] items-center gap-x-2.5 leading-(--hex-row-height)"
          :style="getRowStyle(virtualRow)"
        >
          <span class="min-w-0 select-none whitespace-nowrap text-app-text-muted">{{ line.offsetHex }}:</span>
          <span class="grid min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),2ch)] gap-0.5">
            <span
              v-for="(byte, index) in line.bytes"
              :key="byte?.globalIdx ?? `${line.offset}-${index}`"
              class="cursor-default whitespace-pre rounded-[3px] px-px text-center text-app-text transition-[background-color,color] duration-[0.08s]"
              :class="[
                byte !== null && hoveredByteIdx === byte.globalIdx
                  ? 'bg-[color-mix(in_srgb,var(--app-accent-color)_18%,transparent)] text-app-accent'
                  : '',
                byte !== null && byte.value === 0 ? 'opacity-35' : '',
                byte === null ? 'pointer-events-none opacity-0' : '',
              ]"
              :data-byte-idx="byte?.globalIdx"
              >{{ byte?.hex ?? '  ' }}</span
            >
          </span>
          <span class="grid min-w-0 grid-cols-[repeat(var(--hex-bytes-per-row),1ch)] whitespace-pre text-app-text">
            <span
              v-for="(byte, index) in line.bytes"
              :key="`a-${byte?.globalIdx ?? `${line.offset}-${index}`}`"
              class="cursor-default whitespace-pre rounded-[3px] text-center text-app-text transition-[background-color,color] duration-[0.08s]"
              :class="[
                byte !== null && hoveredByteIdx === byte.globalIdx
                  ? 'bg-[color-mix(in_srgb,var(--app-accent-color)_18%,transparent)] text-app-accent'
                  : '',
                byte !== null && (byte.value < 0x20 || byte.value >= 0x7f) ? 'opacity-35' : '',
                byte === null ? 'pointer-events-none opacity-0' : '',
              ]"
              :data-byte-idx="byte?.globalIdx"
              >{{ byte?.ascii ?? ' ' }}</span
            >
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
