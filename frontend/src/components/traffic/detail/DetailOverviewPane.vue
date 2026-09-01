<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  HTTPMessageState,
  type HTTPMessageMetrics,
  type TrafficEntry,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import {
  UNKNOWN_FORMATTED_VALUE,
  formatDateTimeLocal,
  formatDurationMicros,
  formatFileSize,
  formatUnixMicrosLocal,
  getLogicalHTTPRequestStartLineSize,
  getLogicalHTTPResponseStartLineSize,
  sumKnownByteSizes,
  summarizeHTTPMessageSize,
} from '@/utils/format'
import { parseHighlightedUrl } from '@/utils/urlHighlight'
import DetailTlsSection from './DetailTlsSection.vue'
import DetailCertificateSection from './DetailCertificateSection.vue'
import DetailProcessSection from './DetailProcessSection.vue'

const props = defineProps<{
  selectedEntry: TrafficEntry
}>()

const { t } = useI18n()

interface MetricStateBadge {
  label: string
  color: 'warning' | 'error' | 'neutral'
}

interface MetricRow {
  key: string
  label: string
  value: string
  state?: MetricStateBadge
  indented?: boolean
  prominent?: boolean
}

const methodColorMap: Record<string, string> = {
  GET: '#16a34a',
  POST: '#2563eb',
  PUT: '#d97706',
  DELETE: '#dc2626',
  PATCH: '#7c3aed',
  HEAD: '#0891b2',
  OPTIONS: '#4f46e5',
}

const parsedSelectedEntryUrl = computed(() => parseHighlightedUrl(props.selectedEntry.url ?? ''))
const requestMessage = computed(() => props.selectedEntry.request ?? null)
const responseMessage = computed(() => props.selectedEntry.response ?? null)
const requestMetrics = computed(() => requestMessage.value?.metrics ?? null)
const responseMetrics = computed(() => responseMessage.value?.metrics ?? null)
const hasTrafficMetrics = computed(() => !!requestMetrics.value || !!responseMetrics.value)
const showLegacyMetricsUnavailable = computed(
  () => props.selectedEntry.type !== 'tcp' && !hasTrafficMetrics.value,
)
const showTimingInfo = ref(false)
const showSizeInfo = ref(false)

function isNonZeroTime(t: unknown): boolean {
  return !!t && !String(t).startsWith('0001-01-01')
}

function getMethodColor(method: string) {
  return methodColorMap[method.trim().toUpperCase()] ?? '#475569'
}

function getMethodBadgeStyle(method: string) {
  const methodColor = getMethodColor(method)
  return {
    '--detail-method-color': methodColor,
    '--detail-method-bg': `color-mix(in srgb, ${methodColor} 13%, var(--app-panel-bg))`,
  }
}

function getMetricStateBadge(metrics: HTTPMessageMetrics | null): MetricStateBadge | undefined {
  switch (metrics?.state) {
    case HTTPMessageState.HTTPMessageStatePending:
      return { label: t('detail.metric_state.pending'), color: 'warning' }
    case HTTPMessageState.HTTPMessageStateFailed:
      return { label: t('detail.metric_state.failed'), color: 'error' }
    case HTTPMessageState.HTTPMessageStateCanceled:
      return { label: t('detail.metric_state.canceled'), color: 'neutral' }
    default:
      return undefined
  }
}

function formatKnownSize(size: number | null): string {
  return size === null ? UNKNOWN_FORMATTED_VALUE : formatFileSize(size)
}

function toggleTimingInfo() {
  showTimingInfo.value = !showTimingInfo.value
}

function toggleSizeInfo() {
  showSizeInfo.value = !showSizeInfo.value
}

const timingRows = computed<MetricRow[]>(() => {
  const request = requestMetrics.value
  const response = responseMetrics.value

  return [
    {
      key: 'request-start',
      label: t('detail.request_start'),
      value: formatUnixMicrosLocal(request?.startedAtMicros ?? -1),
    },
    {
      key: 'request-end',
      label: t('detail.request_end'),
      value: formatUnixMicrosLocal(request?.endedAtMicros ?? -1),
    },
    {
      key: 'request-duration',
      label: t('detail.request_duration'),
      value: formatDurationMicros(request?.startedAtMicros ?? -1, request?.endedAtMicros ?? -1),
      state: getMetricStateBadge(request),
    },
    {
      key: 'response-start',
      label: t('detail.response_start'),
      value: formatUnixMicrosLocal(response?.startedAtMicros ?? -1),
    },
    {
      key: 'response-end',
      label: t('detail.response_end'),
      value: formatUnixMicrosLocal(response?.endedAtMicros ?? -1),
    },
    {
      key: 'response-duration',
      label: t('detail.response_duration'),
      value: formatDurationMicros(response?.startedAtMicros ?? -1, response?.endedAtMicros ?? -1),
      state: getMetricStateBadge(response),
    },
    {
      key: 'total-duration',
      label: t('detail.total_duration'),
      value: formatDurationMicros(request?.startedAtMicros ?? -1, response?.endedAtMicros ?? -1),
      prominent: true,
    },
  ]
})

const sizeRows = computed<MetricRow[]>(() => {
  const request = summarizeHTTPMessageSize(
    requestMetrics.value?.headerSize,
    requestMetrics.value?.bodySize,
    requestMessage.value?.headersTruncated,
    getLogicalHTTPRequestStartLineSize(
      props.selectedEntry.method,
      props.selectedEntry.url,
      requestMessage.value?.proto ?? '',
    ),
  )
  const response = summarizeHTTPMessageSize(
    responseMetrics.value?.headerSize,
    responseMetrics.value?.bodySize,
    responseMessage.value?.headersTruncated,
    getLogicalHTTPResponseStartLineSize(
      props.selectedEntry.status,
      responseMessage.value?.proto ?? '',
    ),
  )

  return [
    {
      key: 'request-total',
      label: t('detail.request_size'),
      value: formatKnownSize(request.total),
      prominent: true,
    },
    {
      key: 'request-header',
      label: t('detail.request_header_size'),
      value: formatKnownSize(request.header),
      indented: true,
    },
    {
      key: 'request-body',
      label: t('detail.request_body_size'),
      value: formatKnownSize(request.body),
      indented: true,
    },
    {
      key: 'response-total',
      label: t('detail.response_size'),
      value: formatKnownSize(response.total),
      prominent: true,
    },
    {
      key: 'response-header',
      label: t('detail.response_header_size'),
      value: formatKnownSize(response.header),
      indented: true,
    },
    {
      key: 'response-body',
      label: t('detail.response_body_size'),
      value: formatKnownSize(response.body),
      indented: true,
    },
    {
      key: 'total',
      label: t('detail.total_size'),
      value: formatKnownSize(sumKnownByteSizes(request.total, response.total)),
      prominent: true,
    },
  ]
})

const formatTimestamp = formatDateTimeLocal
</script>

<template>
  <div
    class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
    data-name="request-overview"
    role="tabpanel"
  >
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <!-- 总览信息 -->
      <div class="min-h-0 flex-1 overflow-y-auto px-2.5 pt-2.5 pb-3.5">
        <div class="flex flex-col gap-0.5 pb-2">
          <!-- 基本信息 -->
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.method') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              <span
                class="inline-flex min-h-5 items-center justify-center rounded-(--radius-sm,6px) bg-(--detail-method-bg) px-2 text-sm leading-none font-bold text-(--detail-method-color)"
                :style="getMethodBadgeStyle(selectedEntry.method)"
              >
                {{ selectedEntry.method }}
              </span>
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.url') }}
            </div>
            <div
              class="flex-1 select-text font-sans text-sm break-normal text-app-text wrap-anywhere"
            >
              <span class="text-app-text-muted">{{ parsedSelectedEntryUrl.scheme }}</span>
              <span class="font-semibold text-app-url-host">{{ parsedSelectedEntryUrl.host }}</span>
              <span class="text-app-url-path">{{ parsedSelectedEntryUrl.path }}</span>
              <template v-if="parsedSelectedEntryUrl.hasQuery">
                <span class="text-app-url-query-punct">?</span>
                <template
                  v-for="(item, index) in parsedSelectedEntryUrl.queryItems"
                  :key="`detail-url-query-${index}`"
                >
                  <span v-if="index > 0" class="text-app-url-query-punct">&</span>
                  <span class="text-app-url-query-key">{{ item.key }}</span>
                  <span v-if="item.hasEquals" class="text-app-url-query-punct">=</span>
                  <span v-if="item.hasEquals" class="text-app-url-query-value">{{
                    item.value
                  }}</span>
                </template>
              </template>
              <span class="text-app-url-hash">{{ parsedSelectedEntryUrl.hash }}</span>
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.path') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.path || '' }}
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.status_code') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.statusCode ? `${selectedEntry.statusCode}` : '-' }}
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.protocol') }} (Req)
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.request ? `${selectedEntry.request.proto}` : '-' }}
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.protocol') }} (Res)
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.response ? `${selectedEntry.response.proto}` : '-' }}
            </div>
          </div>
          <div
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.server_address') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.host }}
            </div>
          </div>

          <!-- 连接信息 -->
          <div
            v-if="selectedEntry.metadata?.localSourceAddr"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.local_source_address') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.metadata.localSourceAddr }}
            </div>
          </div>
          <div
            v-if="selectedEntry.metadata?.localDestinationAddr"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.local_destination_address') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.metadata.localDestinationAddr }}
            </div>
          </div>
          <div
            v-if="selectedEntry.metadata?.remoteSourceAddr"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.remote_source_address') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.metadata.remoteSourceAddr }}
            </div>
          </div>
          <div
            v-if="selectedEntry.metadata?.remoteDestinationAddr"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.remote_destination_address') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ selectedEntry.metadata.remoteDestinationAddr }}
            </div>
          </div>

          <!-- 时间信息 -->
          <div
            v-if="isNonZeroTime(selectedEntry.metadata?.localConnectionEstablishedAt)"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.local_connection_time') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ formatTimestamp(selectedEntry.metadata!.localConnectionEstablishedAt) }}
            </div>
          </div>
          <div
            v-if="isNonZeroTime(selectedEntry.metadata?.remoteConnectionEstablishedAt)"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.remote_connection_time') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ formatTimestamp(selectedEntry.metadata!.remoteConnectionEstablishedAt) }}
            </div>
          </div>
          <div
            v-if="isNonZeroTime(selectedEntry.metadata?.requestProcessedAt)"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.request_time') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ formatTimestamp(selectedEntry.metadata!.requestProcessedAt) }}
            </div>
          </div>
          <div
            v-if="isNonZeroTime(selectedEntry.metadata?.sslHandshakeCompletedAt)"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ t('detail.ssl_handshake') }}
            </div>
            <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
              {{ formatTimestamp(selectedEntry.metadata!.sslHandshakeCompletedAt) }}
            </div>
          </div>

          <DetailProcessSection
            v-if="selectedEntry.metadata?.process"
            :selected-entry="selectedEntry"
          />
          <DetailTlsSection v-if="selectedEntry.metadata?.tls" :selected-entry="selectedEntry" />
          <DetailCertificateSection
            v-if="selectedEntry.metadata?.certificate"
            :selected-entry="selectedEntry"
          />

          <div
            v-if="showLegacyMetricsUnavailable"
            class="mt-1.5 flex items-start gap-2 rounded-(--radius-sm,6px) border border-default bg-elevated px-2.5 py-2 font-sans text-sm text-muted"
          >
            <UIcon name="i-lucide-info" class="mt-0.5 size-3.5 shrink-0" />
            <span>{{ t('detail.legacy_metrics_unavailable') }}</span>
          </div>

          <section v-if="hasTrafficMetrics" class="mt-1 flex flex-col" data-metrics-section="timing">
            <button
              type="button"
              class="flex w-full items-center justify-between gap-3 rounded-(--radius-sm,6px) px-2 py-1.75 text-left font-sans text-sm font-semibold text-app-text-secondary outline-none transition-[background-color,box-shadow,color] duration-200 ease-[ease] select-none hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:text-app-text focus-visible:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] focus-visible:text-app-text focus-visible:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_48%,transparent)]"
              :class="
                showTimingInfo &&
                'bg-[color-mix(in_srgb,var(--app-accent-color)_14%,transparent)] shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]'
              "
              :aria-expanded="showTimingInfo"
              @click="toggleTimingInfo"
            >
              <span class="min-w-0 truncate" :class="{ 'text-app-accent': showTimingInfo }">
                {{ t('detail.timing') }}
              </span>
              <UIcon
                :name="showTimingInfo ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                class="size-3.75 shrink-0 text-[15px]"
                :class="{ 'text-app-accent': showTimingInfo }"
                aria-hidden="true"
              />
            </button>
            <dl v-show="showTimingInfo" class="m-0 flex flex-col gap-0.5">
              <div
                v-for="row in timingRows"
                :key="row.key"
                class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
              >
                <dt
                  class="w-30 shrink-0 select-text font-sans text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
                >
                  {{ row.label }}
                </dt>
                <dd
                  class="m-0 flex min-w-0 flex-1 items-center gap-1.5 select-text font-sans text-sm break-all text-app-text"
                  :class="row.prominent && 'font-semibold'"
                >
                  <span v-if="row.value !== UNKNOWN_FORMATTED_VALUE || !row.state">
                    {{ row.value }}
                  </span>
                  <UBadge v-if="row.state" :color="row.state.color" variant="subtle" size="sm">
                    {{ row.state.label }}
                  </UBadge>
                </dd>
              </div>
            </dl>
          </section>

          <section v-if="hasTrafficMetrics" class="mt-1 flex flex-col" data-metrics-section="size">
            <button
              type="button"
              class="flex w-full items-center justify-between gap-3 rounded-(--radius-sm,6px) px-2 py-1.75 text-left font-sans text-sm font-semibold text-app-text-secondary outline-none transition-[background-color,box-shadow,color] duration-200 ease-[ease] select-none hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:text-app-text focus-visible:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] focus-visible:text-app-text focus-visible:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_48%,transparent)]"
              :class="
                showSizeInfo &&
                'bg-[color-mix(in_srgb,var(--app-accent-color)_14%,transparent)] shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]'
              "
              :aria-expanded="showSizeInfo"
              @click="toggleSizeInfo"
            >
              <span class="min-w-0 truncate" :class="{ 'text-app-accent': showSizeInfo }">
                {{ t('detail.message_size') }}
              </span>
              <UIcon
                :name="showSizeInfo ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                class="size-3.75 shrink-0 text-[15px]"
                :class="{ 'text-app-accent': showSizeInfo }"
                aria-hidden="true"
              />
            </button>
            <dl v-show="showSizeInfo" class="m-0 flex flex-col gap-0.5">
              <div
                v-for="row in sizeRows"
                :key="row.key"
                class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
              >
                <dt
                  class="flex w-30 shrink-0 select-text items-center font-sans text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
                  :class="row.indented && 'pl-2'"
                >
                  <span v-if="row.indented" class="mr-1 text-app-text-muted">–</span>
                  {{ row.label }}
                </dt>
                <dd
                  class="m-0 min-w-0 flex-1 select-text font-sans text-sm break-all text-app-text"
                  :class="row.prominent && 'font-semibold'"
                >
                  {{ row.value }}
                </dd>
              </div>
            </dl>
          </section>
        </div>
      </div>
    </div>
  </div>
</template>
