<script setup lang="ts">
import { copyText } from '@/utils/clipboard'
import { computed, ref, shallowRef, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import type { ContextMenuItem, TabsItem } from '@nuxt/ui'
import { SaveBodyToFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import AppLoading from '@/components/common/AppLoading.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import HexDumpViewer from '@/components/common/HexDumpViewer.vue'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import {
  MONACO_LARGE_TEXT_THRESHOLD_CHARS,
  getMonacoWrappedTextChunk,
  requiresMonacoLargeTextOptimizations,
} from '@/components/common/monacoLargeText'
import { useNotify } from '@/composables/useNotify'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { estimateDecodedByteLength } from '@/utils/hexdump'
import { formatFileSize } from '@/utils/format'

const { t } = useI18n()
const notify = useNotify()

const props = defineProps<{
  body: string
  contentType?: string
  bodyEncoding?: string
  sourcePath?: string
}>()

type BodyCategory = 'json' | 'xml' | 'html' | 'js' | 'css' | 'text' | 'image' | 'svg' | 'binary'
type TabKey = 'formatted' | 'image' | 'raw' | 'hex'
type ToolbarAction = 'wrap' | 'copy' | 'save'

const IMAGE_SCALE_MIN = 0.2
const IMAGE_SCALE_MAX = 5
const IMAGE_SCALE_STEP = 0.1
const IMAGE_SCROLLBAR_GUTTER_WIDTH = 14
const IMAGE_NO_SCROLL_FRAME_GUTTER_WIDTH = 10
const HEX_MANUAL_LOAD_THRESHOLD_BYTES = 1 * 1024 * 1024
const FORMATTED_LARGE_BODY_THRESHOLD_BYTES = MONACO_LARGE_TEXT_THRESHOLD_CHARS

// ── Content-type categorisation ──────────────────────────────────────────────

const bodyCategory = computed((): BodyCategory => {
  const ct = ((props.contentType ?? '').toLowerCase().split(';')[0] ?? '').trim()
  if (ct.includes('json')) return 'json'
  if (ct === 'image/svg+xml') return 'svg'
  if (ct.startsWith('image/')) return 'image'
  if (ct.includes('xml')) return 'xml'
  if (ct.includes('html')) return 'html'
  if (ct.includes('javascript')) return 'js'
  if (ct.includes('css')) return 'css'
  if (ct.startsWith('text/')) return 'text'
  if (props.bodyEncoding === 'base64') return 'binary'
  return 'text'
})

const isServerSentEvents = computed(() => {
  const mediaType = ((props.contentType ?? '').toLowerCase().split(';')[0] ?? '').trim()
  return mediaType === 'text/event-stream'
})

const monacoLanguage = computed(() => {
  switch (bodyCategory.value) {
    case 'json':
      return 'json'
    case 'xml':
    case 'svg':
      return 'xml'
    case 'html':
      return 'html'
    case 'js':
      return 'javascript'
    case 'css':
      return 'css'
    default:
      return 'plaintext'
  }
})

const hasFormatted = computed(
  () =>
    ['json', 'xml', 'html', 'js', 'css', 'svg'].includes(bodyCategory.value) &&
    estimateDecodedByteLength(props.body, props.bodyEncoding === 'base64') <
      FORMATTED_LARGE_BODY_THRESHOLD_BYTES,
)
const hasImage = computed(() => bodyCategory.value === 'image' || bodyCategory.value === 'svg')

// ── Image preview state ──────────────────────────────────────────────────────

const imageScale = ref(1)
const imageOffsetX = ref(0)
const imageOffsetY = ref(0)
const isImageDragging = ref(false)
const dragStartMouseX = ref(0)
const dragStartMouseY = ref(0)
const dragStartOffsetX = ref(0)
const dragStartOffsetY = ref(0)
const imageNaturalWidth = ref<number | null>(null)
const imageNaturalHeight = ref<number | null>(null)
const imageContainerRef = ref<HTMLElement | null>(null)
const hasImageVerticalScrollbar = ref(false)
const hasImageHorizontalScrollbar = ref(false)
const formattedWordWrap = ref(true)
const rawWordWrap = ref(true)
const largeTextWrapEnabled = ref(false)
const wrappedChunkIndex = ref(0)

let imageResizeObserver: ResizeObserver | null = null

const imageScalePercent = computed(() => `${Math.round(imageScale.value * 100)}%`)
const imageCanZoomIn = computed(() => imageScale.value < IMAGE_SCALE_MAX)
const imageCanZoomOut = computed(() => imageScale.value > IMAGE_SCALE_MIN)
const imageCanReset = computed(
  () => imageScale.value !== 1 || imageOffsetX.value !== 0 || imageOffsetY.value !== 0,
)

const imageFormatLabel = computed(() => inferImageFormatLabel(props.contentType))
const imageByteSize = computed(() => getBodyByteLength(props.body, props.bodyEncoding))
const imageSizeLabel = computed(() => formatFileSize(imageByteSize.value))
const imageDimensionsLabel = computed(() => {
  if (!imageNaturalWidth.value || !imageNaturalHeight.value) return ''
  return `${imageNaturalWidth.value} x ${imageNaturalHeight.value}`
})
const imageInfoText = computed(() =>
  [imageFormatLabel.value, imageDimensionsLabel.value, imageSizeLabel.value]
    .filter(Boolean)
    .join(' | '),
)

const imageTransformStyle = computed(() => ({
  transform: `translate(${imageOffsetX.value}px, ${imageOffsetY.value}px) scale(${imageScale.value})`,
  transition: isImageDragging.value ? 'none' : 'transform 0.08s ease-out',
}))

const imageFrameRightGutter = computed(() =>
  hasImageVerticalScrollbar.value
    ? IMAGE_SCROLLBAR_GUTTER_WIDTH
    : IMAGE_NO_SCROLL_FRAME_GUTTER_WIDTH,
)
const imageFrameBottomGutter = computed(() =>
  hasImageHorizontalScrollbar.value ? IMAGE_SCROLLBAR_GUTTER_WIDTH : 0,
)
const imageShellStyle = computed(() => ({
  '--image-frame-right-gutter': `${imageFrameRightGutter.value}px`,
  '--image-frame-bottom-gutter': `${imageFrameBottomGutter.value}px`,
}))

const imageContextMenuOptions = computed<ContextMenuItem[]>(() => [
  {
    label: t('detail.image_zoom_in'),
    disabled: !imageCanZoomIn.value,
    onSelect: () => zoomImageBy(IMAGE_SCALE_STEP),
  },
  {
    label: t('detail.image_zoom_out'),
    disabled: !imageCanZoomOut.value,
    onSelect: () => zoomImageBy(-IMAGE_SCALE_STEP),
  },
  {
    label: t('detail.image_reset'),
    disabled: !imageCanReset.value,
    onSelect: () => resetImagePreview(),
  },
  { type: 'separator' },
  {
    label: t('detail.image_save'),
    onSelect: () => void saveCurrentBodyContent(),
  },
])

function clampImageScale(next: number) {
  return Math.min(IMAGE_SCALE_MAX, Math.max(IMAGE_SCALE_MIN, Number(next.toFixed(2))))
}

function setImageScale(next: number) {
  const scale = clampImageScale(next)
  imageScale.value = scale
  if (scale <= 1) {
    imageOffsetX.value = 0
    imageOffsetY.value = 0
    isImageDragging.value = false
  }
}

function resetImagePreview() {
  imageScale.value = 1
  imageOffsetX.value = 0
  imageOffsetY.value = 0
  isImageDragging.value = false
}

function resetImageMetadata() {
  imageNaturalWidth.value = null
  imageNaturalHeight.value = null
}

function zoomImageBy(delta: number) {
  setImageScale(imageScale.value + delta)
}

function handleImageWheel(event: WheelEvent) {
  if (!event.ctrlKey) return
  event.preventDefault()
  zoomImageBy(event.deltaY < 0 ? IMAGE_SCALE_STEP : -IMAGE_SCALE_STEP)
}

function handleImageMouseDown(event: MouseEvent) {
  if (event.button !== 0 || imageScale.value <= 1) return
  event.preventDefault()
  isImageDragging.value = true
  dragStartMouseX.value = event.clientX
  dragStartMouseY.value = event.clientY
  dragStartOffsetX.value = imageOffsetX.value
  dragStartOffsetY.value = imageOffsetY.value
}

function handleWindowMouseMove(event: MouseEvent) {
  if (!isImageDragging.value) return
  imageOffsetX.value = dragStartOffsetX.value + (event.clientX - dragStartMouseX.value)
  imageOffsetY.value = dragStartOffsetY.value + (event.clientY - dragStartMouseY.value)
}

function handleWindowMouseUp() {
  isImageDragging.value = false
}

function handleImageContextMenu() {
  // Stop any in-progress drag; UContextMenu opens itself at the pointer.
  handleWindowMouseUp()
}

function handleImageLoad(event: Event) {
  const image = event.target as HTMLImageElement
  imageNaturalWidth.value = image.naturalWidth || null
  imageNaturalHeight.value = image.naturalHeight || null
  nextTick(updateImageScrollbarPresence)
}

function updateImageScrollbarPresence() {
  const element = imageContainerRef.value
  if (!element) return

  hasImageVerticalScrollbar.value = element.scrollHeight > element.clientHeight + 1
  hasImageHorizontalScrollbar.value = element.scrollWidth > element.clientWidth + 1
}

async function saveBodyToFile(body: string, bodyEncoding: string) {
  const selectedPath = await Dialogs.SaveFile({
    Filename: suggestedFilename.value,
  })
  const savePath = selectedPath.trim()
  if (!savePath) {
    return
  }
  await SaveBodyToFile({
    path: savePath,
    body,
    bodyEncoding,
    contentType: props.contentType ?? '',
  })
}

// ── Available tabs and default active tab ────────────────────────────────────

const availableTabs = computed((): TabKey[] => {
  const tabs: TabKey[] = []
  if (hasFormatted.value) tabs.push('formatted')
  if (hasImage.value) tabs.push('image')
  tabs.push('raw')
  tabs.push('hex')
  return tabs
})

const activeTab = ref<TabKey>('raw')

watch(
  availableTabs,
  (tabs) => {
    if (tabs.length > 0 && !tabs.includes(activeTab.value)) {
      activeTab.value = tabs[0]!
    }
  },
  { immediate: true },
)

const bodyTabOptions = computed<TabsItem[]>(() =>
  availableTabs.value.map((tab) => ({
    label: tabLabel(tab),
    value: tab,
  })),
)

// ── Body content ─────────────────────────────────────────────────────────────

const hasBody = computed(() => props.body.length > 0)
const isBinaryEncoded = computed(() => props.bodyEncoding === 'base64')

const rawBody = computed(() => {
  if (!hasBody.value) return ''
  if (isBinaryEncoded.value) {
    return `[Binary data - view in Hex or Image tab]`
  }
  return props.body
})

const formattedBody = computed(() => {
  if (!hasBody.value) return ''
  if (bodyCategory.value === 'json') {
    try {
      return JSON.stringify(JSON.parse(props.body), null, 2)
    } catch {
      return props.body
    }
  }
  return props.body
})

const activeTextTab = computed<'formatted' | 'raw'>(() =>
  activeTab.value === 'formatted' && hasFormatted.value ? 'formatted' : 'raw',
)
const showTextPanel = computed(
  () => activeTab.value === 'raw' || (activeTab.value === 'formatted' && hasFormatted.value),
)
const textEditorBody = computed(() =>
  activeTextTab.value === 'formatted' ? formattedBody.value : rawBody.value,
)
const textEditorLanguage = computed(() =>
  activeTextTab.value === 'formatted' ? monacoLanguage.value : 'plaintext',
)
const textEditorOptions = computed(() =>
  activeTextTab.value === 'raw'
    ? {
        bracketPairColorization: { enabled: false },
        matchBrackets: 'never' as const,
      }
    : {},
)
const textEditorLargeTextMode = computed(() =>
  requiresMonacoLargeTextOptimizations(textEditorBody.value),
)
const textEditorWrappedChunk = computed(() =>
  getMonacoWrappedTextChunk(textEditorBody.value, wrappedChunkIndex.value),
)
const textEditorUsesWrappedChunk = computed(
  () => textEditorLargeTextMode.value && largeTextWrapEnabled.value,
)
const textEditorValue = computed(() =>
  textEditorUsesWrappedChunk.value
    ? textEditorWrappedChunk.value.text
    : textEditorBody.value,
)
const requestedTextEditorWordWrap = computed(() =>
  activeTextTab.value === 'formatted' ? formattedWordWrap.value : rawWordWrap.value,
)
const textEditorWordWrap = computed(
  () =>
    textEditorLargeTextMode.value
      ? largeTextWrapEnabled.value
      : requestedTextEditorWordWrap.value,
)
const showWrappedChunkPagination = computed(
  () =>
    showTextPanel.value &&
    textEditorUsesWrappedChunk.value &&
    textEditorWrappedChunk.value.count > 1,
)
const wrappedChunkPageLabel = computed(() =>
  t('detail.large_text_chunk_page', {
    current: textEditorWrappedChunk.value.index + 1,
    total: textEditorWrappedChunk.value.count,
  }),
)
const shouldRenderHexPanel = shallowRef(false)
const hexViewerBody = shallowRef('')
const hexViewerIsBase64 = shallowRef(false)
const hexLoadRequested = shallowRef(false)
const hexViewerPending = shallowRef(false)
let hexActivationRequestId = 0
let hexActivationTimer: ReturnType<typeof setTimeout> | null = null

const hexEstimatedByteSize = computed(() =>
  estimateDecodedByteLength(props.body, props.bodyEncoding === 'base64'),
)
const isLargeHexBody = computed(
  () => hexEstimatedByteSize.value >= HEX_MANUAL_LOAD_THRESHOLD_BYTES,
)
const shouldGateHexViewer = computed(() => isLargeHexBody.value && !hexLoadRequested.value)
const canRenderHexViewer = computed(
  () =>
    shouldRenderHexPanel.value &&
    !shouldGateHexViewer.value &&
    !hexViewerPending.value &&
    hexViewerBody.value.length > 0,
)
const showHexLoadGate = computed(
  () => activeTab.value === 'hex' && shouldRenderHexPanel.value && shouldGateHexViewer.value,
)
const showHexLoadingState = computed(
  () => activeTab.value === 'hex' && shouldRenderHexPanel.value && hexViewerPending.value,
)
const hexLargeBodyDescription = computed(() =>
  t('detail.hex_large_body_description', { size: formatFileSize(hexEstimatedByteSize.value) }),
)

function clearHexActivationTimer() {
  if (hexActivationTimer === null) return

  clearTimeout(hexActivationTimer)
  hexActivationTimer = null
}

function updateHexViewerSource() {
  shouldRenderHexPanel.value = true
  clearHexActivationTimer()
  hexViewerIsBase64.value = props.bodyEncoding === 'base64'

  if (shouldGateHexViewer.value) {
    hexActivationRequestId++
    hexViewerPending.value = false
    hexViewerBody.value = ''
    return
  }

  const requestId = ++hexActivationRequestId
  hexViewerPending.value = true
  hexViewerBody.value = ''

  hexActivationTimer = setTimeout(() => {
    hexActivationTimer = null
    if (requestId !== hexActivationRequestId || activeTab.value !== 'hex') return

    hexViewerBody.value = props.body
    hexViewerIsBase64.value = props.bodyEncoding === 'base64'
    hexViewerPending.value = false
  }, 0)
}

function resetHexViewerSource() {
  hexActivationRequestId++
  clearHexActivationTimer()
  shouldRenderHexPanel.value = false
  hexViewerBody.value = ''
  hexViewerIsBase64.value = false
  hexViewerPending.value = false
}

function loadLargeHexViewer() {
  hexLoadRequested.value = true
  updateHexViewerSource()
}

function inferExtensionFromContentType(contentType?: string): string {
  const mediaType = ((contentType ?? '').toLowerCase().split(';')[0] ?? '').trim()
  switch (mediaType) {
    case 'application/json':
      return '.json'
    case 'application/xml':
    case 'text/xml':
      return '.xml'
    case 'text/html':
      return '.html'
    case 'application/javascript':
    case 'text/javascript':
      return '.js'
    case 'text/css':
      return '.css'
    case 'image/svg+xml':
      return '.svg'
    case 'image/png':
      return '.png'
    case 'image/jpeg':
      return '.jpg'
    case 'image/gif':
      return '.gif'
    case 'image/webp':
      return '.webp'
    case 'image/avif':
      return '.avif'
    default:
      return bodyCategory.value === 'image' || bodyCategory.value === 'binary' ? '.bin' : '.txt'
  }
}

function inferImageFormatLabel(contentType?: string): string {
  const mediaType = ((contentType ?? '').toLowerCase().split(';')[0] ?? '').trim()
  switch (mediaType) {
    case 'image/svg+xml':
      return 'SVG'
    case 'image/png':
      return 'PNG'
    case 'image/jpeg':
      return 'JPEG'
    case 'image/gif':
      return 'GIF'
    case 'image/webp':
      return 'WEBP'
    case 'image/avif':
      return 'AVIF'
    case 'image/bmp':
      return 'BMP'
    case 'image/x-icon':
    case 'image/vnd.microsoft.icon':
      return 'ICO'
    default:
      return mediaType.startsWith('image/')
        ? mediaType.slice('image/'.length).split('+')[0]!.toUpperCase()
        : ''
  }
}

function getBodyByteLength(body: string, bodyEncoding?: string): number {
  if (!body) return 0

  if (bodyEncoding === 'base64') {
    return estimateDecodedByteLength(body, true)
  }

  return new TextEncoder().encode(body).length
}

function splitFilenameParts(name: string) {
  const lastDot = name.lastIndexOf('.')
  if (lastDot <= 0 || lastDot === name.length - 1) {
    return { stem: name, ext: '' }
  }
  return {
    stem: name.slice(0, lastDot),
    ext: name.slice(lastDot).toLowerCase(),
  }
}

function matchesContentTypeExtension(ext: string, contentType?: string): boolean {
  const mediaType = ((contentType ?? '').toLowerCase().split(';')[0] ?? '').trim()
  switch (mediaType) {
    case 'application/json':
      return ext === '.json'
    case 'application/xml':
    case 'text/xml':
      return ext === '.xml'
    case 'text/html':
      return ext === '.html' || ext === '.htm'
    case 'application/javascript':
    case 'text/javascript':
      return ext === '.js' || ext === '.mjs' || ext === '.cjs'
    case 'text/css':
      return ext === '.css'
    case 'image/svg+xml':
      return ext === '.svg'
    case 'image/png':
      return ext === '.png'
    case 'image/jpeg':
      return ext === '.jpg' || ext === '.jpeg'
    case 'image/gif':
      return ext === '.gif'
    case 'image/webp':
      return ext === '.webp'
    case 'image/avif':
      return ext === '.avif'
    default:
      return true
  }
}

function inferFilenameFromSourcePath(
  sourcePath: string | undefined,
  contentType: string | undefined,
  fallbackBase: string,
): string {
  const cleanedPath = (sourcePath ?? '').split('?')[0]?.split('#')[0] ?? ''
  const segments = cleanedPath.split('/').filter(Boolean)
  const lastSegment = segments.length > 0 ? segments[segments.length - 1]! : ''
  const safeBase = lastSegment || fallbackBase
  const inferredExt = inferExtensionFromContentType(contentType)
  const sanitizedBase = safeBase
    .replace(/[<>:"/\\|?*\u0000-\u001f]/g, '-')
    .trim()
    .replace(/[.\s]+$/g, '')
  const finalBase = sanitizedBase || fallbackBase
  const { stem, ext } = splitFilenameParts(finalBase)

  if (!ext) {
    return inferredExt ? `${finalBase}${inferredExt}` : finalBase
  }

  if (!inferredExt || matchesContentTypeExtension(ext, contentType)) {
    return finalBase
  }

  return `${stem}${inferredExt}`
}

const suggestedFilename = computed(() => {
  const fallbackBase = activeTab.value === 'image' ? 'response-image' : 'response-body'
  return inferFilenameFromSourcePath(props.sourcePath, props.contentType, fallbackBase)
})

const copyableBodyContent = computed(() => {
  if (!hasBody.value) return ''
  if (activeTab.value === 'formatted') return formattedBody.value
  if (activeTab.value === 'raw' && !isBinaryEncoded.value) return props.body
  return ''
})

const currentSaveTarget = computed((): { body: string; bodyEncoding: string } | null => {
  if (!hasBody.value) return null

  switch (activeTab.value) {
    case 'formatted':
      return {
        body: formattedBody.value,
        bodyEncoding: '',
      }
    case 'raw':
      return {
        body: props.body,
        bodyEncoding: isBinaryEncoded.value ? (props.bodyEncoding ?? '') : '',
      }
    case 'image':
      return {
        body: props.body,
        bodyEncoding: props.bodyEncoding ?? '',
      }
    case 'hex':
      return {
        body: props.body,
        bodyEncoding: isBinaryEncoded.value ? (props.bodyEncoding ?? '') : '',
      }
    default:
      return null
  }
})

const tabToolbarActions = computed((): ToolbarAction[] => {
  if (!hasBody.value) return []

  const actions: ToolbarAction[] = []
  if (activeTab.value === 'formatted' || activeTab.value === 'raw') {
    actions.push('wrap')
  }
  if (copyableBodyContent.value) {
    actions.push('copy')
  }
  if (currentSaveTarget.value) {
    actions.push('save')
  }

  return actions
})

function toggleCurrentEditorWordWrap() {
  if (textEditorLargeTextMode.value) {
    largeTextWrapEnabled.value = !largeTextWrapEnabled.value
    wrappedChunkIndex.value = 0
    return
  }

  if (activeTab.value === 'raw') {
    rawWordWrap.value = !rawWordWrap.value
    return
  }

  if (activeTab.value === 'formatted') {
    formattedWordWrap.value = !formattedWordWrap.value
  }
}

function showPreviousWrappedChunk() {
  wrappedChunkIndex.value = Math.max(textEditorWrappedChunk.value.index - 1, 0)
}

function showNextWrappedChunk() {
  wrappedChunkIndex.value = Math.min(
    textEditorWrappedChunk.value.index + 1,
    textEditorWrappedChunk.value.count - 1,
  )
}

function resetLargeTextWrap() {
  largeTextWrapEnabled.value = false
  wrappedChunkIndex.value = 0
}

async function copyBodyContent() {
  if (!copyableBodyContent.value) return
  try {
    await copyText(copyableBodyContent.value)
    notify.success(t('detail.body_copied'))
  } catch (error) {
    notify.error(t('detail.body_copy_failed', { error: getErrorMessage(error) }))
  }
}

async function saveCurrentBodyContent() {
  const saveTarget = currentSaveTarget.value
  if (!saveTarget) return

  try {
    await saveBodyToFile(saveTarget.body, saveTarget.bodyEncoding)
  } catch (error) {
    if (isDialogCancelError(error)) {
      return
    }
    notify.error(t('detail.body_save_failed', { error: getErrorMessage(error) }))
  }
}

onMounted(() => {
  window.addEventListener('mousemove', handleWindowMouseMove)
  window.addEventListener('mouseup', handleWindowMouseUp)

  nextTick(() => {
    if (!imageContainerRef.value) return

    imageResizeObserver = new ResizeObserver(() => {
      updateImageScrollbarPresence()
    })
    imageResizeObserver.observe(imageContainerRef.value)
    updateImageScrollbarPresence()
  })
})

onUnmounted(() => {
  window.removeEventListener('mousemove', handleWindowMouseMove)
  window.removeEventListener('mouseup', handleWindowMouseUp)
  imageResizeObserver?.disconnect()
  resetHexViewerSource()
})

watch(activeTextTab, () => {
  resetLargeTextWrap()
})

watch(textEditorBody, (nextValue, previousValue) => {
  if (!requiresMonacoLargeTextOptimizations(nextValue)) {
    resetLargeTextWrap()
    return
  }
  if (!largeTextWrapEnabled.value) {
    wrappedChunkIndex.value = 0
    return
  }
  if (!nextValue.startsWith(previousValue)) {
    resetLargeTextWrap()
    return
  }

  const previousChunk = getMonacoWrappedTextChunk(previousValue, wrappedChunkIndex.value)
  const nextChunk = getMonacoWrappedTextChunk(nextValue, wrappedChunkIndex.value)
  wrappedChunkIndex.value =
    previousChunk.index === previousChunk.count - 1
      ? nextChunk.count - 1
      : Math.min(previousChunk.index, nextChunk.count - 1)
})

watch(
  () => [props.body, props.bodyEncoding, props.contentType] as const,
  () => {
    hexLoadRequested.value = false
    if (activeTab.value === 'hex') {
      updateHexViewerSource()
    } else {
      resetHexViewerSource()
    }
    resetImagePreview()
    resetImageMetadata()
    nextTick(updateImageScrollbarPresence)
  },
)

watch(
  () => activeTab.value,
  (tab) => {
    if (tab === 'hex') {
      updateHexViewerSource()
      return
    }
    hexActivationRequestId++
    clearHexActivationTimer()
    hexViewerPending.value = false
  },
  { immediate: true },
)

watch(
  () =>
    [
      activeTab.value,
      imageScale.value,
      imageOffsetX.value,
      imageOffsetY.value,
      hasImage.value,
    ] as const,
  () => {
    nextTick(updateImageScrollbarPresence)
  },
)

const imageDataUrl = computed(() => {
  if (!hasBody.value) return ''
  const ct = ((props.contentType ?? '').split(';')[0] ?? '').trim()
  if (props.bodyEncoding === 'base64') {
    return `data:${ct};base64,${props.body}`
  }
  const encoded = encodeURIComponent(props.body)
  return `data:${ct},${encoded}`
})

// ── Monaco editor options ────────────────────────────────────────────────────

// ── Tab bar helpers ───────────────────────────────────────────────────────────

function tabLabel(tab: TabKey): string {
  switch (tab) {
    case 'formatted':
      return t('detail.formatted')
    case 'image':
      return t('detail.preview')
    case 'raw':
      return t('detail.raw')
    case 'hex':
      return t('detail.hex')
  }
}
</script>

<template>
  <div class="body-viewer flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden">
    <UEmpty
      v-if="!hasBody"
      icon="i-lucide-file-x-2"
      :title="t('common.no_content')"
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />
    <div v-else class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden">
      <div class="flex min-h-8.5 shrink-0 items-center justify-between gap-2 bg-app-panel pl-0 pr-2.5 pt-1">
        <UTabs
          :model-value="activeTab"
          :items="bodyTabOptions"
          :content="false"
          variant="pill"
          size="xs"
          :ui="{ list: 'min-w-0' }"
          @update:model-value="activeTab = $event as TabKey"
        />
        <div v-if="tabToolbarActions.length" class="flex shrink-0 items-center gap-1">
          <UTooltip v-if="tabToolbarActions.includes('wrap')" :text="t('detail.wrap_body')">
            <UButton
              icon="i-lucide-corner-down-left"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="t('detail.wrap_body')"
              :aria-pressed="textEditorWordWrap"
              @click="toggleCurrentEditorWordWrap"
            />
          </UTooltip>
          <template v-if="showWrappedChunkPagination">
            <UTooltip :text="t('detail.large_text_previous_chunk')">
              <span class="inline-flex">
                <UButton
                  icon="i-lucide-chevron-left"
                  color="neutral"
                  variant="ghost"
                  size="sm"
                  square
                  :disabled="textEditorWrappedChunk.index === 0"
                  :aria-label="t('detail.large_text_previous_chunk')"
                  @click="showPreviousWrappedChunk"
                />
              </span>
            </UTooltip>
            <span
              class="min-w-10 shrink-0 px-0.5 text-center text-xs tabular-nums text-muted"
              :aria-label="wrappedChunkPageLabel"
              aria-live="polite"
              role="status"
            >
              {{ wrappedChunkPageLabel }}
            </span>
            <UTooltip :text="t('detail.large_text_next_chunk')">
              <span class="inline-flex">
                <UButton
                  icon="i-lucide-chevron-right"
                  color="neutral"
                  variant="ghost"
                  size="sm"
                  square
                  :disabled="textEditorWrappedChunk.index === textEditorWrappedChunk.count - 1"
                  :aria-label="t('detail.large_text_next_chunk')"
                  @click="showNextWrappedChunk"
                />
              </span>
            </UTooltip>
          </template>
          <UTooltip v-if="tabToolbarActions.includes('copy')" :text="t('detail.copy_body')">
            <UButton
              icon="i-lucide-copy"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="t('detail.copy_body')"
              @click="copyBodyContent"
            />
          </UTooltip>
          <UTooltip v-if="tabToolbarActions.includes('save')" :text="t('detail.save_body')">
            <UButton
              icon="i-lucide-download"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="t('detail.save_body')"
              @click="saveCurrentBodyContent"
            />
          </UTooltip>
        </div>
      </div>

      <div class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden pt-1.5">
        <div v-show="showTextPanel" class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden" role="tabpanel">
          <MonacoBodyEditor
            :value="textEditorValue"
            :language="textEditorLanguage"
            :options="textEditorOptions"
            readonly
            :word-wrap="textEditorWordWrap"
            :allow-large-text-word-wrap="textEditorUsesWrappedChunk"
            :follow-tail-on-append="isServerSentEvents"
          />
        </div>

        <template v-if="hasImage">
          <div v-show="activeTab === 'image'" class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden" role="tabpanel">
            <div
              class="relative flex min-h-0 flex-1 flex-col bg-app-panel before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:right-(--image-frame-right-gutter) before:bottom-(--image-frame-bottom-gutter) before:top-0 before:border before:border-app-border before:content-['']"
              :style="imageShellStyle"
            >
              <div class="flex min-h-7 min-w-0 items-center gap-2.5 bg-app-control px-2.5 text-sm mr-(--image-frame-right-gutter) [border-bottom:1px_solid_var(--app-border-color)]">
                <span class="shrink-0 font-semibold tabular-nums text-app-text-secondary">{{ imageScalePercent }}</span>
                <span v-if="imageInfoText" class="min-w-0 truncate tabular-nums text-app-text-secondary">{{ imageInfoText }}</span>
                <span class="ml-auto shrink-0 text-app-text-muted">{{ t('detail.image_zoom_hint') }}</span>
              </div>

              <div class="flex min-h-0 flex-1 flex-col">
                <UContextMenu :items="imageContextMenuOptions">
                  <div
                    ref="imageContainerRef"
                    class="flex flex-1 items-center justify-center overflow-auto py-2.5 pl-2.5 pr-[calc(10px+var(--image-frame-right-gutter))] pb-[calc(10px+var(--image-frame-bottom-gutter))]"
                    @wheel="handleImageWheel"
                    @mousedown="handleImageMouseDown"
                    @contextmenu="handleImageContextMenu"
                  >
                    <div
                      class="flex min-h-full min-w-full items-center justify-center"
                      :class="
                        isImageDragging ? 'cursor-grabbing' : imageScale > 1 ? 'cursor-grab' : 'cursor-default'
                      "
                    >
                      <img
                        :src="imageDataUrl"
                        class="max-h-full max-w-full select-none object-contain origin-[center_center]"
                        :style="imageTransformStyle"
                        alt="Response image"
                        draggable="false"
                        @load="handleImageLoad"
                        @dragstart.prevent
                      />
                    </div>
                  </div>
                </UContextMenu>
              </div>
            </div>
          </div>
        </template>

        <div
          v-if="shouldRenderHexPanel"
          v-show="activeTab === 'hex'"
          class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
          role="tabpanel"
        >
          <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
            <UEmpty
              v-if="showHexLoadGate"
              icon="i-lucide-binary"
              :title="t('detail.hex_large_body_title')"
              :description="hexLargeBodyDescription"
              :size="appEmptyStateSize"
              variant="naked"
              :ui="appEmptyStateUi"
            >
              <template #actions>
                <UButton size="sm" @click="loadLargeHexViewer">
                  {{ t('detail.load_hex_dump') }}
                </UButton>
              </template>
            </UEmpty>
            <AppLoading
              v-else-if="showHexLoadingState"
              fill
              :label="t('detail.hex_loading')"
            />
            <HexDumpViewer
              v-else-if="canRenderHexViewer"
              :input="hexViewerBody"
              :is-base64="hexViewerIsBase64"
              :active="activeTab === 'hex'"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
