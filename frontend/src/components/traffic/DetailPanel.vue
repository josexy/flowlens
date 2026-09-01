<script setup lang="ts">
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { computed, inject, onUnmounted, reactive, ref, shallowRef, watch } from 'vue'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import type { TabsItem } from '@nuxt/ui'
import type { TrafficBodyView } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { TRAFFIC_STORE_KEY } from '@/types/inject-keys'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import DetailOverviewPane from './detail/DetailOverviewPane.vue'
import DetailHeadersPane from './detail/DetailHeadersPane.vue'
import DetailBodyPane from './detail/DetailBodyPane.vue'
import DetailRawHTTPPane from './detail/DetailRawHTTPPane.vue'
import DetailWebSocketPane from './detail/DetailWebSocketPane.vue'
import DetailRawTCPPane from './detail/DetailRawTCPPane.vue'
import CookieTablePane from './CookieTablePane.vue'
import QueryTablePane from './QueryTablePane.vue'
import { useI18n } from 'vue-i18n'
import { useNotify } from '@/composables/useNotify'
import type {
  WebSocketDirectionFilter,
  WebSocketDisplayMessage,
  WebSocketViewMode,
} from '@/types/websocket'
import { toWebSocketDisplayMessage, toWebSocketDisplayMessages } from '@/utils/websocket'
import { formatToRFC3339 } from '@/utils/format'
import {
  formatHeaderFieldsAsJson,
  formatHeaderFieldsAsText,
  firstHeaderFieldValue,
  type HeaderSortOrder,
  headersRecordToFields,
  normalizeHeaderFields,
  sortHeaderFields,
} from '@/utils/headers'
import { hasHeader, requestCookiesRecord, responseCookiesRecord } from '@/utils/cookies'
import { isRawTCPTraffic, isWebSocketTraffic } from '@/utils/traffic'
import { formatRawHTTPRequest, formatRawHTTPResponse } from '@/utils/httpRaw'
import { parseUrlQuery } from '@/utils/urlHighlight'

type SummaryTagType = 'default' | 'error' | 'info' | 'success' | 'warning'
type ResponseSummaryChip = { key: string; text: string; type: SummaryTagType }
interface DisplayedBodyViewState {
  entryId: number
  bodyView: TrafficBodyView
  requestContentType?: string
  responseContentType?: string
  sourcePath: string
}

const { t } = useI18n()
const notify = useNotify()
const trafficStore = inject(TRAFFIC_STORE_KEY)!

const selectedEntry = computed(() => trafficStore.selectedEntry)
const selectedEntryBodyView = computed(() => trafficStore.selectedEntryBodyView)
const selectedEntryBodyViewLoading = computed(
  () => trafficStore.selectedEntryBodyViewLoading === true,
)

const requestActiveTab = ref('request-overview')
const responseActiveTab = ref('response-headers')
const mountedRequestTabs = reactive(new Set<string>([requestActiveTab.value]))
const mountedResponseTabs = reactive(new Set<string>([responseActiveTab.value]))
const websocketDirectionFilter = ref<WebSocketDirectionFilter>('all')
const websocketViewMode = ref<WebSocketViewMode>('list')
const requestHeaderSortOrder = ref<HeaderSortOrder>('default')
const responseHeaderSortOrder = ref<HeaderSortOrder>('default')
const responseTrailerSortOrder = ref<HeaderSortOrder>('default')
const responseTabsUi = { list: 'items-center' }

const isWebSocket = computed(
  () => isWebSocketTraffic(selectedEntry.value),
)
const isRawTCP = computed(() => isRawTCPTraffic(selectedEntry.value))

const requestHeadersCount = computed(() => {
  return requestHeaderSource.value.fields.length
})

const responseHeadersCount = computed(() => {
  return responseHeaderSource.value.fields.length
})

const responseTrailersCount = computed(() => {
  return selectedEntry.value?.response?.trailerFields?.length ?? 0
})

const requestQuery = computed(() => parseUrlQuery(selectedEntry.value?.url ?? ''))
const hasRequestQuery = computed(() => requestQuery.value.fields.length > 0)

const hasRequestCookieHeader = computed(() =>
  hasHeader(selectedEntry.value?.request?.headerFields, 'cookie'),
)

const hasResponseCookieHeader = computed(() =>
  hasHeader(selectedEntry.value?.response?.headerFields, 'set-cookie'),
)

const requestContentType = computed(() =>
  firstHeaderFieldValue(selectedEntry.value?.request?.headerFields, 'Content-Type'),
)

const responseContentType = computed(() =>
  firstHeaderFieldValue(selectedEntry.value?.response?.headerFields, 'Content-Type'),
)

const displayedBodyViewState = shallowRef<DisplayedBodyViewState | null>(null)
const bodyLoadingOverlayVisible = shallowRef(false)
const BODY_LOADING_OVERLAY_DELAY_MS = 120
let bodyLoadingOverlayTimer: ReturnType<typeof setTimeout> | null = null

const displayedBodyView = computed(() => displayedBodyViewState.value?.bodyView ?? null)
const displayedBodyViewEntryId = computed(() => displayedBodyViewState.value?.entryId ?? null)
const isDisplayedBodyViewCurrent = computed(
  () => !!selectedEntry.value && displayedBodyViewEntryId.value === selectedEntry.value.id,
)
const currentRawBodyView = computed(() =>
  isDisplayedBodyViewCurrent.value ? displayedBodyView.value : null,
)
const requestRawHTTPMessage = computed(() => {
  const entry = selectedEntry.value
  if (!entry) {
    return ''
  }
  const bodyView = currentRawBodyView.value
  return formatRawHTTPRequest({
    method: entry.method,
    url: entry.url,
    host: entry.host,
    protocol: entry.request?.proto ?? '',
    headerFields: entry.request?.headerFields,
    ...(bodyView
      ? { body: bodyView.reqBody || '', bodyEncoding: bodyView.reqBodyEnc ?? '' }
      : {}),
  })
})
const hasRawHTTPResponse = computed(() => {
  const entry = selectedEntry.value
  const response = entry?.response
  return !!(
    response &&
    (response.proto.trim() ||
      (entry?.statusCode ?? 0) > 0 ||
      firstHeaderFieldValue(response.headerFields, ':status') !== undefined)
  )
})
const responseRawHTTPMessage = computed(() => {
  const entry = selectedEntry.value
  if (!entry || !hasRawHTTPResponse.value) {
    return ''
  }
  const bodyView = currentRawBodyView.value
  return formatRawHTTPResponse({
    status: entry.status,
    statusCode: entry.statusCode,
    protocol: entry.response?.proto ?? '',
    headerFields: entry.response?.headerFields,
    ...(bodyView
      ? { body: bodyView.rspBody || '', bodyEncoding: bodyView.rspBodyEnc ?? '' }
      : {}),
  })
})
const displayedRequestContentType = computed(() =>
  isDisplayedBodyViewCurrent.value
    ? requestContentType.value
    : displayedBodyViewState.value?.requestContentType,
)
const displayedResponseContentType = computed(() =>
  isDisplayedBodyViewCurrent.value
    ? responseContentType.value
    : displayedBodyViewState.value?.responseContentType,
)
const displayedSourcePath = computed(() =>
  isDisplayedBodyViewCurrent.value
    ? selectedEntry.value?.path || ''
    : displayedBodyViewState.value?.sourcePath || '',
)
const hasDisplayedBodyView = computed(() => displayedBodyView.value !== null)
const isBodyViewRefreshing = computed(
  () => selectedEntryBodyViewLoading.value && hasDisplayedBodyView.value,
)
const showBodyLoadingPlaceholder = computed(
  () => selectedEntryBodyViewLoading.value && !hasDisplayedBodyView.value,
)
const showBodyLoadingOverlay = computed(
  () => isBodyViewRefreshing.value && bodyLoadingOverlayVisible.value,
)
const showBodyEmptyFallback = computed(
  () => !selectedEntryBodyViewLoading.value && !hasDisplayedBodyView.value,
)
const shouldRenderRequestBodyPanel = computed(
  () => mountedRequestTabs.has('request-body'),
)
const shouldRenderResponseBodyPanel = computed(
  () => mountedResponseTabs.has('response-body'),
)

const requestTabItems = computed<TabsItem[]>(() => {
  const items: TabsItem[] = [
    { value: 'request-overview', label: t('detail.overview') },
    { value: 'request-raw', label: t('detail.raw') },
    {
      value: 'request-headers',
      label: `${t('detail.request_headers')}(${requestHeadersCount.value})`,
    },
  ]
  if (hasRequestQuery.value) {
    items.push({
      value: 'request-query',
      label: `${t('detail.query')}(${requestQuery.value.fields.length})`,
    })
  }
  if (hasRequestCookieHeader.value) {
    items.push({
      value: 'request-cookies',
      label: `${t('detail.cookies')}(${requestCookiesCount.value})`,
    })
  }
  items.push({ value: 'request-body', label: t('detail.request_body') })
  return items
})

const responseTabItems = computed<TabsItem[]>(() => {
  if (errorMessage.value) {
    return [{ value: 'response-error', label: t('detail.error') }]
  }
  const items: TabsItem[] = []
  if (isWebSocket.value) {
    items.push({ value: 'websocket', label: 'WebSocket' })
  }
  items.push({ value: 'response-raw', label: t('detail.raw') })
  items.push({
    value: 'response-headers',
    label: `${t('detail.response_headers')}(${responseHeadersCount.value})`,
  })
  if (hasResponseCookieHeader.value) {
    items.push({
      value: 'response-cookies',
      label: `${t('detail.cookies')}(${responseCookiesCount.value})`,
    })
  }
  if (responseTrailersCount.value > 0) {
    items.push({
      value: 'response-trailers',
      label: `${t('detail.response_trailers')}(${responseTrailersCount.value})`,
    })
  }
  items.push({ value: 'response-body', label: t('detail.response_body') })
  return items
})

const errorMessage = computed(() => {
  if (!selectedEntry.value) return undefined
  return selectedEntry.value.error?.error
})

const responseStatusCodeText = computed(() => {
  if (!selectedEntry.value?.statusCode) return '-'
  return String(selectedEntry.value.statusCode)
})

const responseStatusTagType = computed(() => {
  const statusCode = selectedEntry.value?.statusCode ?? 0
  if (statusCode >= 200 && statusCode < 300) return 'success'
  if (statusCode >= 300 && statusCode < 400) return 'info'
  if (statusCode >= 400 && statusCode < 500) return 'warning'
  if (statusCode >= 500) return 'error'
  return 'default'
})

const responseProtocolText = computed(() => {
  return selectedEntry.value?.response?.proto || '-'
})

const responseSummaryChips = computed(() => {
  return [
    { key: 'status', text: responseStatusCodeText.value, type: responseStatusTagType.value },
    { key: 'protocol', text: responseProtocolText.value, type: 'default' },
  ] as ResponseSummaryChip[]
})

const summaryChipVariantClass: Record<SummaryTagType, string> = {
  default: 'text-app-text-muted bg-app-control',
  success: 'text-[#16a34a] bg-[rgba(34,197,94,0.14)]',
  info: 'text-[#0284c7] bg-[rgba(14,165,233,0.14)]',
  warning: 'text-[#b45309] bg-[rgba(245,158,11,0.16)]',
  error: 'text-[#dc2626] bg-[rgba(239,68,68,0.14)]',
}

const requestHeaderSource = computed(() =>
  normalizeHeaderFields(
    selectedEntry.value?.request?.headerFields,
    selectedEntry.value?.request?.headerOrderUnavailable === true,
  ),
)
const responseHeaderSource = computed(() =>
  normalizeHeaderFields(
    selectedEntry.value?.response?.headerFields,
    selectedEntry.value?.response?.headerOrderUnavailable === true,
  ),
)
const responseTrailerSource = computed(() =>
  normalizeHeaderFields(
    selectedEntry.value?.response?.trailerFields,
    selectedEntry.value?.response?.trailerOrderUnavailable === true,
  ),
)
const requestHeaderWarning = computed(() => {
  if (selectedEntry.value?.request?.headersTruncated) {
    return t('detail.header_fields_truncated')
  }
  if (selectedEntry.value?.request && !requestHeaderSource.value.hasWireOrder) {
    return t('detail.header_order_unavailable')
  }
  return ''
})
const responseHeaderWarning = computed(() => {
  if (selectedEntry.value?.response?.headersTruncated) {
    return t('detail.header_fields_truncated')
  }
  if (selectedEntry.value?.response && !responseHeaderSource.value.hasWireOrder) {
    return t('detail.header_order_unavailable')
  }
  return ''
})
const responseTrailerWarning = computed(() => {
  if (selectedEntry.value?.response?.trailersTruncated) {
    return t('detail.trailer_fields_truncated')
  }
  if (selectedEntry.value?.response && !responseTrailerSource.value.hasWireOrder) {
    return t('detail.trailer_order_unavailable')
  }
  return ''
})
const requestCookieFields = computed(() =>
  requestCookiesRecord(selectedEntry.value?.request?.headerFields),
)
const responseCookieFields = computed(() =>
  responseCookiesRecord(selectedEntry.value?.response?.headerFields),
)
const requestCookiesCount = computed(() => headersRecordToFields(requestCookieFields.value).length)
const responseCookiesCount = computed(
  () => headersRecordToFields(responseCookieFields.value).length,
)
const displayRequestHeaders = computed(() =>
  sortHeaderFields(requestHeaderSource.value.fields, requestHeaderSortOrder.value),
)
const displayResponseHeaders = computed(() =>
  sortHeaderFields(responseHeaderSource.value.fields, responseHeaderSortOrder.value),
)
const displayResponseTrailers = computed(() =>
  sortHeaderFields(responseTrailerSource.value.fields, responseTrailerSortOrder.value),
)

const websocketMessages = ref<WebSocketDisplayMessage[]>([])
let mappedWebSocketEntryId: number | null = null
let mappedWebSocketSource: TrafficBodyView['wsMsgs'] = null
let mappedWebSocketSourceLength = 0
watch(
  [
    displayedBodyViewEntryId,
    () => displayedBodyView.value?.wsMsgs ?? null,
    () => displayedBodyView.value?.wsMsgs?.length ?? 0,
  ],
  ([entryId, sourceValue, sourceLength]) => {
    const source = sourceValue ?? []
    if (
      entryId !== mappedWebSocketEntryId ||
      sourceValue !== mappedWebSocketSource ||
      sourceLength < mappedWebSocketSourceLength
    ) {
      websocketMessages.value = toWebSocketDisplayMessages(
        source,
        (_, index) => `detail-ws-msg:${entryId ?? 'unknown'}:${index}`,
      )
      mappedWebSocketEntryId = entryId
      mappedWebSocketSource = sourceValue
      mappedWebSocketSourceLength = sourceLength
      return
    }
    for (let index = mappedWebSocketSourceLength; index < sourceLength; index++) {
      const item = source[index]
      if (!item) continue
      websocketMessages.value.push(
        toWebSocketDisplayMessage(
          item,
          `detail-ws-msg:${entryId ?? 'unknown'}:${index}`,
        ),
      )
    }
    mappedWebSocketEntryId = entryId
    mappedWebSocketSource = sourceValue
    mappedWebSocketSourceLength = sourceLength
  },
  { immediate: true },
)
const wsMsgsTruncated = computed(() => displayedBodyView.value?.wsMsgsTruncated === true)

function clearBodyLoadingOverlayTimer() {
  if (bodyLoadingOverlayTimer === null) {
    return
  }
  clearTimeout(bodyLoadingOverlayTimer)
  bodyLoadingOverlayTimer = null
}

function rememberDisplayedBodyView(bodyView: TrafficBodyView) {
  const entry = selectedEntry.value
  if (!entry) {
    return
  }
  displayedBodyViewState.value = {
    entryId: entry.id,
    bodyView,
    requestContentType: requestContentType.value,
    responseContentType: responseContentType.value,
    sourcePath: entry.path || '',
  }
}

watch(
  selectedEntryBodyView,
  (bodyView) => {
    if (!bodyView) {
      return
    }
    rememberDisplayedBodyView(bodyView)
  },
  { immediate: true },
)

watch(
  () => selectedEntry.value?.id ?? null,
  (entryId) => {
    if (entryId !== null) {
      return
    }
    displayedBodyViewState.value = null
    bodyLoadingOverlayVisible.value = false
    clearBodyLoadingOverlayTimer()
  },
)

watch(
  selectedEntryBodyViewLoading,
  (isLoading) => {
    clearBodyLoadingOverlayTimer()
    bodyLoadingOverlayVisible.value = false
    if (!isLoading) {
      if (!selectedEntryBodyView.value && !isDisplayedBodyViewCurrent.value) {
        displayedBodyViewState.value = null
      }
      return
    }
    if (!hasDisplayedBodyView.value) {
      return
    }
    bodyLoadingOverlayTimer = setTimeout(() => {
      bodyLoadingOverlayTimer = null
      if (selectedEntryBodyViewLoading.value && hasDisplayedBodyView.value) {
        bodyLoadingOverlayVisible.value = true
      }
    }, BODY_LOADING_OVERLAY_DELAY_MS)
  },
  { immediate: true },
)

watch(
  () => selectedEntry.value,
  (entry, previousEntry) => {
    const entryChanged = entry?.id !== previousEntry?.id
    if (entryChanged) {
      requestHeaderSortOrder.value = 'default'
      responseHeaderSortOrder.value = 'default'
      responseTrailerSortOrder.value = 'default'
    }
    if (requestActiveTab.value === 'request-query' && !hasRequestQuery.value) {
      requestActiveTab.value = 'request-headers'
    }
    if (requestActiveTab.value === 'request-cookies' && !hasRequestCookieHeader.value) {
      requestActiveTab.value = 'request-headers'
    }
    if (errorMessage.value) {
      responseActiveTab.value = 'response-error'
    } else if (entryChanged && isWebSocket.value) {
      responseActiveTab.value = 'websocket'
    } else if (
      responseActiveTab.value === 'response-cookies' &&
      !hasResponseCookieHeader.value
    ) {
      responseActiveTab.value = 'response-headers'
    } else if (
      (responseActiveTab.value === 'response-error' && !isWebSocket.value) ||
      (responseActiveTab.value === 'websocket' && !isWebSocket.value) ||
      (responseActiveTab.value === 'response-trailers' && responseTrailersCount.value === 0)
    ) {
      responseActiveTab.value = 'response-headers'
    } else if (responseActiveTab.value === 'response-error' && isWebSocket.value) {
      responseActiveTab.value = 'websocket'
    }

    if (entryChanged) {
      mountedRequestTabs.clear()
      mountedRequestTabs.add(requestActiveTab.value)
      mountedResponseTabs.clear()
      mountedResponseTabs.add(responseActiveTab.value)
    }
  },
  { immediate: true },
)

watch(
  requestActiveTab,
  (tab) => mountedRequestTabs.add(tab),
  { immediate: true },
)

watch(
  responseActiveTab,
  (tab) => mountedResponseTabs.add(tab),
  { immediate: true },
)

onUnmounted(() => {
  clearBodyLoadingOverlayTimer()
})

function nextSortOrder(order: HeaderSortOrder): HeaderSortOrder {
  if (order === 'default') return 'asc'
  if (order === 'asc') return 'desc'
  return 'default'
}

function sortOrderLabel(order: HeaderSortOrder): string {
  return t(`detail.header_sort_${order}`)
}

async function copyText(text: string, successMessage: string) {
  try {
    await copyTextToClipboard(text)
    notify.success(successMessage)
  } catch (error) {
    notify.error(t('detail.headers_copy_failed', { error: String(error) }))
  }
}

async function copyRequestHeadersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(displayRequestHeaders.value),
    t('detail.request_headers_copied_json'),
  )
}

async function copyRequestHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(displayRequestHeaders.value),
    t('detail.request_headers_copied_text'),
  )
}

async function copyResponseHeadersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(displayResponseHeaders.value),
    t('detail.response_headers_copied_json'),
  )
}

async function copyResponseHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(displayResponseHeaders.value),
    t('detail.response_headers_copied_text'),
  )
}

async function copyResponseTrailersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(displayResponseTrailers.value),
    t('detail.response_trailers_copied_json'),
  )
}

async function copyResponseTrailersAsText() {
  await copyText(
    formatHeaderFieldsAsText(displayResponseTrailers.value),
    t('detail.response_trailers_copied_text'),
  )
}

function cycleRequestHeaderSortOrder() {
  requestHeaderSortOrder.value = nextSortOrder(requestHeaderSortOrder.value)
  notify.success(
    t('detail.request_headers_sorted_state', {
      state: sortOrderLabel(requestHeaderSortOrder.value),
    }),
  )
}

function cycleResponseHeaderSortOrder() {
  responseHeaderSortOrder.value = nextSortOrder(responseHeaderSortOrder.value)
  notify.success(
    t('detail.response_headers_sorted_state', {
      state: sortOrderLabel(responseHeaderSortOrder.value),
    }),
  )
}

function cycleResponseTrailerSortOrder() {
  responseTrailerSortOrder.value = nextSortOrder(responseTrailerSortOrder.value)
  notify.success(
    t('detail.response_trailers_sorted_state', {
      state: sortOrderLabel(responseTrailerSortOrder.value),
    }),
  )
}

const formatTimestamp = formatToRFC3339
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden bg-app-panel">
    <UEmpty
      v-if="!selectedEntry"
      icon="i-lucide-mouse-pointer-click"
      :title="t('detail.select_request')"
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />
    <DetailRawTCPPane
      v-else-if="isRawTCP"
      :selected-entry="selectedEntry"
    />
    <div v-else class="flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden">
      <!-- 请求和响应部分的分割 -->
      <SplitterGroup direction="vertical" class="flex h-full min-h-0 flex-col bg-transparent">
        <SplitterPanel
          :default-size="50"
          :min-size="30"
          class="flex min-h-0 flex-[1_1_0] flex-col overflow-hidden"
        >
          <!-- 总览+Request 部分 -->
          <div
            class="relative flex h-full min-h-0 flex-col overflow-hidden rounded-none bg-app-panel"
          >
            <div class="flex h-full min-h-0 flex-col">
              <div class="flex min-h-9.5 min-w-0 shrink-0 items-center gap-2 bg-app-panel px-2.5">
                <UTabs
                  :model-value="requestActiveTab"
                  :items="requestTabItems"
                  :content="false"
                  variant="link"
                  class="min-w-0 flex-1"
                  @update:model-value="requestActiveTab = String($event)"
                />
              </div>
              <div class="flex min-h-0 w-full min-w-0 flex-1 overflow-hidden bg-app-panel p-0">
                <DetailOverviewPane
                  v-if="mountedRequestTabs.has('request-overview')"
                  v-show="requestActiveTab === 'request-overview'"
                  :selected-entry="selectedEntry"
                />
                <DetailRawHTTPPane
                  v-if="mountedRequestTabs.has('request-raw')"
                  v-show="requestActiveTab === 'request-raw'"
                  data-name="request-raw"
                  role="tabpanel"
                  :value="requestRawHTTPMessage"
                  :warning-message="requestHeaderWarning"
                />
                <DetailHeadersPane
                  v-if="mountedRequestTabs.has('request-headers')"
                  v-show="requestActiveTab === 'request-headers'"
                  data-name="request-headers"
                  role="tabpanel"
                  :title="t('workspace.http_request.request_header_list')"
                  :fields="displayRequestHeaders"
                  :has-headers="displayRequestHeaders.length > 0"
                  :empty-title="t('detail.no_headers')"
                  :warning-message="requestHeaderWarning"
                  :copy-json-label="t('detail.copy_request_headers_json')"
                  :sort-label="t('detail.sort_request_headers')"
                  :copy-text-label="t('detail.copy_request_headers_text')"
                  @copy-json="copyRequestHeadersAsJson"
                  @sort="cycleRequestHeaderSortOrder"
                  @copy-text="copyRequestHeadersAsText"
                />
                <QueryTablePane
                  v-if="mountedRequestTabs.has('request-query') && hasRequestQuery"
                  v-show="requestActiveTab === 'request-query'"
                  data-name="request-query"
                  role="tabpanel"
                  :title="t('detail.query_list')"
                  :fields="requestQuery.fields"
                  :raw-query="requestQuery.rawQuery"
                  :empty-title="t('detail.no_query_parameters')"
                />
                <CookieTablePane
                  v-if="mountedRequestTabs.has('request-cookies') && hasRequestCookieHeader"
                  v-show="requestActiveTab === 'request-cookies'"
                  data-name="request-cookies"
                  role="tabpanel"
                  :title="t('detail.cookie_list')"
                  :cookies="requestCookieFields"
                  :empty-title="t('detail.no_cookies')"
                  :raw-headers="selectedEntry.request?.headerFields"
                  header-name="cookie"
                />
                <div
                  v-if="shouldRenderRequestBodyPanel"
                  v-show="requestActiveTab === 'request-body'"
                  class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                  data-name="request-body"
                  role="tabpanel"
                >
                  <DetailBodyPane
                    v-if="shouldRenderRequestBodyPanel"
                    body-side="request"
                    :body-identity="displayedBodyViewEntryId"
                    :body-view="displayedBodyView"
                    :content-type="displayedRequestContentType"
                    :fallback-content-type="requestContentType"
                    :source-path="displayedSourcePath"
                    :fallback-source-path="selectedEntry?.path || ''"
                    :is-loading="selectedEntryBodyViewLoading"
                    :is-refreshing="isBodyViewRefreshing"
                    :show-empty-fallback="showBodyEmptyFallback"
                    :show-loading-placeholder="showBodyLoadingPlaceholder"
                    :show-loading-overlay="showBodyLoadingOverlay"
                  />
                </div>
              </div>
            </div>
          </div>
        </SplitterPanel>
        <AppSplitterResizeHandle />
        <SplitterPanel
          :default-size="50"
          :min-size="30"
          class="flex min-h-0 flex-[1_1_0] flex-col overflow-hidden"
        >
          <!-- Response 部分 -->
          <div
            class="relative flex h-full min-h-0 flex-col overflow-hidden rounded-none bg-app-panel"
          >
            <div class="flex h-full min-h-0 flex-col">
              <div class="flex min-h-9.5 min-w-0 shrink-0 items-center bg-app-panel px-2.5">
                <UTabs
                  :model-value="responseActiveTab"
                  :items="responseTabItems"
                  :content="false"
                  variant="link"
                  :ui="responseTabsUi"
                  class="min-w-0 flex-1"
                  @update:model-value="responseActiveTab = String($event)"
                >
                  <template #list-trailing>
                    <div
                      v-if="!errorMessage"
                      class="ml-auto flex min-w-max flex-[0_0_auto] flex-nowrap items-center gap-1.5"
                    >
                      <span
                        v-for="chip in responseSummaryChips"
                        :key="chip.key"
                        class="inline-flex min-h-5 shrink-0 items-center justify-center rounded-(--radius-sm,6px) px-2 text-sm leading-none font-bold tabular-nums"
                        :class="summaryChipVariantClass[chip.type]"
                      >
                        {{ chip.text }}
                      </span>
                    </div>
                  </template>
                </UTabs>
              </div>
              <div class="flex min-h-0 w-full min-w-0 flex-1 overflow-hidden bg-app-panel p-0">
                <div
                  v-if="responseActiveTab === 'response-error'"
                  class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                  data-name="response-error"
                  role="tabpanel"
                >
                  <div
                    class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-4"
                  >
                    <div class="text-sm text-app-text-muted">
                      {{ formatTimestamp(selectedEntry.error!.timestamp) }}
                    </div>
                    <div class="whitespace-pre-wrap break-all select-text text-sm text-[#d03050]">
                      {{ selectedEntry.error!.error }}
                    </div>
                  </div>
                </div>
                <template v-else>
                  <DetailWebSocketPane
                    v-if="mountedResponseTabs.has('websocket')"
                    v-show="responseActiveTab === 'websocket'"
                    data-name="websocket"
                    role="tabpanel"
                    :messages="websocketMessages"
                    :direction-filter="websocketDirectionFilter"
                    :view-mode="websocketViewMode"
                    :is-loading="selectedEntryBodyViewLoading"
                    :is-refreshing="isBodyViewRefreshing"
                    :has-body-view="hasDisplayedBodyView"
                    :show-empty-fallback="showBodyEmptyFallback"
                    :show-loading-placeholder="showBodyLoadingPlaceholder"
                    :show-loading-overlay="showBodyLoadingOverlay"
                    :messages-truncated="wsMsgsTruncated"
                    :truncated-title="t('traffic.ws_msgs_truncated')"
                    @update:direction-filter="websocketDirectionFilter = $event"
                    @update:view-mode="websocketViewMode = $event"
                  />
                  <DetailRawHTTPPane
                    v-if="mountedResponseTabs.has('response-raw')"
                    v-show="responseActiveTab === 'response-raw'"
                    data-name="response-raw"
                    role="tabpanel"
                    :value="responseRawHTTPMessage"
                    :warning-message="responseHeaderWarning"
                    :waiting="!hasRawHTTPResponse"
                  />
                  <DetailHeadersPane
                    v-if="mountedResponseTabs.has('response-headers')"
                    v-show="responseActiveTab === 'response-headers'"
                    data-name="response-headers"
                    role="tabpanel"
                    :title="t('workspace.http_request.response_header_list')"
                    :fields="displayResponseHeaders"
                    :has-headers="displayResponseHeaders.length > 0"
                    :empty-title="t('detail.no_headers')"
                    :warning-message="responseHeaderWarning"
                    :copy-json-label="t('detail.copy_response_headers_json')"
                    :sort-label="t('detail.sort_response_headers')"
                    :copy-text-label="t('detail.copy_response_headers_text')"
                    @copy-json="copyResponseHeadersAsJson"
                    @sort="cycleResponseHeaderSortOrder"
                    @copy-text="copyResponseHeadersAsText"
                  />
                  <CookieTablePane
                    v-if="mountedResponseTabs.has('response-cookies') && hasResponseCookieHeader"
                    v-show="responseActiveTab === 'response-cookies'"
                    data-name="response-cookies"
                    role="tabpanel"
                    :title="t('detail.cookie_list')"
                    :cookies="responseCookieFields"
                    :empty-title="t('detail.no_cookies')"
                    :raw-headers="selectedEntry.response?.headerFields"
                    header-name="set-cookie"
                  />
                  <DetailHeadersPane
                    v-if="
                      mountedResponseTabs.has('response-trailers') && responseTrailersCount > 0
                    "
                    v-show="responseActiveTab === 'response-trailers'"
                    data-name="response-trailers"
                    role="tabpanel"
                    :title="t('detail.response_trailer_list')"
                    :fields="displayResponseTrailers"
                    :has-headers="displayResponseTrailers.length > 0"
                    :empty-title="t('detail.no_headers')"
                    :warning-message="responseTrailerWarning"
                    :copy-json-label="t('detail.copy_response_trailers_json')"
                    :sort-label="t('detail.sort_response_trailers')"
                    :copy-text-label="t('detail.copy_response_trailers_text')"
                    @copy-json="copyResponseTrailersAsJson"
                    @sort="cycleResponseTrailerSortOrder"
                    @copy-text="copyResponseTrailersAsText"
                  />
                  <div
                    v-if="shouldRenderResponseBodyPanel"
                    v-show="responseActiveTab === 'response-body'"
                    class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                    data-name="response-body"
                    role="tabpanel"
                  >
                    <DetailBodyPane
                      v-if="shouldRenderResponseBodyPanel"
                      body-side="response"
                      :body-identity="displayedBodyViewEntryId"
                      :body-view="displayedBodyView"
                      :content-type="displayedResponseContentType"
                      :fallback-content-type="responseContentType"
                      :source-path="displayedSourcePath"
                      :fallback-source-path="selectedEntry?.path || ''"
                      :is-loading="selectedEntryBodyViewLoading"
                      :is-refreshing="isBodyViewRefreshing"
                      :show-empty-fallback="showBodyEmptyFallback"
                      :show-loading-placeholder="showBodyLoadingPlaceholder"
                      :show-loading-overlay="showBodyLoadingOverlay"
                    />
                  </div>
                </template>
              </div>
            </div>
          </div>
        </SplitterPanel>
      </SplitterGroup>
    </div>
  </div>
</template>
