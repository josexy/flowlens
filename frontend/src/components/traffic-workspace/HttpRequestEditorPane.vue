<script setup lang="ts">
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { computed, onBeforeUnmount, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import type { TabsItem } from '@nuxt/ui'
import { CancelError, Events } from '@wailsio/runtime'
import { useNotify } from '@/composables/useNotify'
import { SendHTTPRequest } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type {
  SendRequestBody,
  SendRequestBodyType,
  SendRequestProtocol,
  SendRequestProxyMode,
  TLSClientHelloID,
} from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import type { PluginLogEntry } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import type {
  EditableKeyValue,
  HttpRequestProtocol,
  HttpRequestEditorState,
} from '@/types/request-editor'
import AppLoading from '@/components/common/AppLoading.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import CookieTablePane from '@/components/traffic/CookieTablePane.vue'
import BodyViewer from '@/components/traffic/BodyViewer.vue'
import RequestUrlInput from '@/components/traffic-workspace/RequestUrlInput.vue'
import RequestMitmProxyButton from '@/components/traffic-workspace/RequestMitmProxyButton.vue'
import RequestFileBody from '@/components/traffic-workspace/RequestFileBody.vue'
import HttpRequestFormDataBody from '@/components/traffic-workspace/HttpRequestFormDataBody.vue'
import HttpRequestUrlEncodedBody from '@/components/traffic-workspace/HttpRequestUrlEncodedBody.vue'
import HttpRequestPythonConsole from '@/components/traffic-workspace/HttpRequestPythonConsole.vue'
import EditableKeyValueTable from '@/components/traffic-workspace/EditableKeyValueTable.vue'
import RequestCookiePane from '@/components/traffic-workspace/RequestCookiePane.vue'
import RequestQueryPane from '@/components/traffic-workspace/RequestQueryPane.vue'
import { useMitmProxyModeToggle } from '@/composables/useMitmProxyModeToggle'
import { useRequestQuerySync } from '@/composables/useRequestQuerySync'
import { useThemeStore } from '@/stores/theme'
import { useSettingStore } from '@/stores/setting'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import {
  DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED,
  DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT,
} from '@/stores/traffic-workspace/requestEditorState'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'
import {
  editableRowsToHeaderFields,
  convertRequestRouteHeaders,
  findInvalidRequestHeaderName,
  formatHeaderFieldsAsJson,
  formatHeaderFieldsAsText,
  headersRecordToFields,
  synchronizeRequestRouteHeaders,
  type HeaderField,
} from '@/utils/headers'
import { countRequestCookieRows, hasHeader, responseCookiesRecord } from '@/utils/cookies'

type SummaryTagType = 'default' | 'error' | 'primary' | 'info' | 'success' | 'warning'
type ResponseMetaChip = { key: string; text: string; type: SummaryTagType }

const props = defineProps<{
  tabKey: string
}>()
const state = defineModel<HttpRequestEditorState>('state', { required: true })
const requestURLModel = computed({
  get: () => state.value.url,
  set: (value: string) => {
    state.value.url = value
  },
})
const requestQueryRowsModel = computed({
  get: () => state.value.params,
  set: (value: EditableKeyValue[]) => {
    state.value.params = value
  },
})
const { queryCount } = useRequestQuerySync(requestURLModel, requestQueryRowsModel)

const mountedRequestTabs = reactive(new Set<HttpRequestEditorState['activeRequestTab']>())

watch(
  () => state.value.activeRequestTab,
  (tab) => mountedRequestTabs.add(tab),
  { immediate: true },
)

const { t } = useI18n()
const themeStore = useThemeStore()
const settingStore = useSettingStore()
const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const notify = useNotify()
const sendShortcutKbds = useShortcutKbds('requestEditor.send')
let pendingSendCall: ReturnType<typeof SendHTTPRequest> | null = null
let sendOperationGeneration = 0

const PYTHON_PLUGIN_LOG_EVENT = 'python-plugins:log'
const MAX_REQUEST_CONSOLE_ENTRIES = 1000

if (typeof state.value.pluginsEnabled !== 'boolean') {
  state.value.pluginsEnabled = DEFAULT_HTTP_REQUEST_PLUGINS_ENABLED
}
if (typeof state.value.inlineScriptEnabled !== 'boolean') {
  state.value.inlineScriptEnabled = false
}
if (typeof state.value.inlineScriptSource !== 'string') {
  state.value.inlineScriptSource = DEFAULT_HTTP_REQUEST_PYTHON_SCRIPT
}
if (typeof state.value.scriptExecutionId !== 'string') {
  state.value.scriptExecutionId = ''
}
if (!Array.isArray(state.value.scriptConsoleEntries)) {
  state.value.scriptConsoleEntries = []
}

const offPythonPluginLog = Events.On(PYTHON_PLUGIN_LOG_EVENT, (rawEvent) => {
  const entry = rawEvent.data as PluginLogEntry
  if (!entry || !entry.executionId || entry.executionId !== state.value.scriptExecutionId) {
    return
  }
  state.value.scriptConsoleEntries.push(entry)
  const overflow = state.value.scriptConsoleEntries.length - MAX_REQUEST_CONSOLE_ENTRIES
  if (overflow > 0) {
    state.value.scriptConsoleEntries.splice(0, overflow)
  }
})

const methodOptions = [
  { label: 'GET', value: 'GET' },
  { label: 'POST', value: 'POST' },
  { label: 'PUT', value: 'PUT' },
  { label: 'DELETE', value: 'DELETE' },
  { label: 'PATCH', value: 'PATCH' },
  { label: 'HEAD', value: 'HEAD' },
  { label: 'OPTIONS', value: 'OPTIONS' },
]

const bodyTypeOptions = computed(() => [
  { label: t('workspace.http_request.body_type_none'), value: 'none' },
  { label: t('workspace.http_request.body_type_json'), value: 'json' },
  { label: t('workspace.http_request.body_type_text'), value: 'text' },
  { label: t('workspace.http_request.body_type_xml'), value: 'xml' },
  { label: t('workspace.http_request.body_type_file'), value: 'file' },
  { label: t('workspace.http_request.body_type_form_data'), value: 'form-data' },
  { label: t('workspace.http_request.body_type_urlencoded'), value: 'urlencoded' },
])

const proxyModeOptions = computed(() => [
  { label: t('workspace.http_request.proxy_mode_none'), value: 'none' },
  { label: t('workspace.http_request.proxy_mode_system'), value: 'system' },
  { label: t('workspace.http_request.proxy_mode_mitm'), value: 'mitm' },
  { label: t('workspace.http_request.proxy_mode_custom'), value: 'custom' },
])

const protocolOptions = computed(() => [
  { label: t('workspace.http_request.protocol_auto'), value: 'auto' },
  { label: 'HTTP/1.1', value: 'http1' },
  { label: 'HTTP/2', value: 'http2' },
])

const tlsClientHelloOptions = computed(() => [
  { label: t('workspace.http_request.tls_fingerprint_golang'), value: 'golang' },
  { label: t('workspace.http_request.tls_fingerprint_chrome'), value: 'chrome_auto' },
  { label: t('workspace.http_request.tls_fingerprint_firefox'), value: 'firefox_auto' },
  { label: t('workspace.http_request.tls_fingerprint_safari'), value: 'safari_auto' },
  { label: t('workspace.http_request.tls_fingerprint_edge'), value: 'edge_auto' },
  { label: t('workspace.http_request.tls_fingerprint_ios'), value: 'ios_auto' },
  {
    label: t('workspace.http_request.tls_fingerprint_android'),
    value: 'android_11_okhttp',
  },
  { label: t('workspace.http_request.tls_fingerprint_random'), value: 'randomized_alpn' },
])

const { isMitmProxyMode, toggleMitmProxyMode } = useMitmProxyModeToggle(() => state.value.settings)
const mitmProxyToggleLabel = computed(() =>
  isMitmProxyMode.value
    ? t('workspace.http_request.disable_mitm_proxy')
    : t('workspace.http_request.enable_mitm_proxy'),
)

const requestLineThemeVars = computed(() => {
  if (themeStore.isDark) {
    return {
      '--bv-gutter-bg': 'var(--app-shell-bg)',
    }
  }
  return {
    '--bv-gutter-bg': 'var(--app-shell-bg)',
  }
})

const methodColorMap: Record<string, string> = {
  GET: '#16a34a',
  POST: '#2563eb',
  PUT: '#d97706',
  DELETE: '#dc2626',
  PATCH: '#7c3aed',
  HEAD: '#0891b2',
  OPTIONS: '#4f46e5',
}

const methodThemeVars = computed(() => {
  const method = state.value.method.trim().toUpperCase()
  return {
    '--method-accent-color': methodColorMap[method] ?? '#475569',
  }
})

function methodLabelStyle(methodValue: string | number | undefined) {
  const method = String(methodValue ?? '').toUpperCase()
  return {
    color: methodColorMap[method] ?? '#475569',
    fontWeight: '700',
  }
}

const hasResponse = computed(() => state.value.response !== null)
const hasRequestConsole = computed(
  () =>
    hasResponse.value ||
    state.value.inlineScriptEnabled ||
    state.value.scriptExecutionId.length > 0 ||
    state.value.scriptConsoleEntries.length > 0,
)
const hasPluginTerminalResponse = computed(() => state.value.response?.kind === 'plugin')
const hasErrorResponse = computed(
  () => state.value.response?.kind === 'error' || hasPluginTerminalResponse.value,
)
const responseErrorDetailsText = computed(() => state.value.response?.errorMessage || '')
const hasResponseErrorDetails = computed(
  () => hasErrorResponse.value || responseErrorDetailsText.value.length > 0,
)
const responseErrorTitle = computed(() => {
  if (hasPluginTerminalResponse.value) {
    return t('workspace.http_request.plugin_terminal_title')
  }
  if (state.value.response?.outcome === 'completed_with_plugin_error') {
    return t('workspace.http_request.plugin_outcome_completed_with_plugin_error')
  }
  return t('workspace.http_request.response_error_title')
})
const hasCancelledResponse = computed(() => state.value.response?.kind === 'cancelled')
const isHttpOperationActive = computed(() => state.value.isSending || state.value.isStreaming)
const primaryActionTooltip = computed(() => {
  if (state.value.isStreaming) {
    return t('workspace.http_request.stop_stream')
  }
  if (state.value.isSending) {
    return t('workspace.http_request.stop_request')
  }
  return t('workspace.http_request.send_request')
})
const pythonPluginsEnabled = computed(
  () => settingStore.settings?.pythonPluginConfig?.enabled === true,
)
const pluginToggleTooltip = computed(() => {
  if (!pythonPluginsEnabled.value) {
    return t('workspace.http_request.plugins_global_disabled')
  }
  return state.value.pluginsEnabled
    ? t('workspace.http_request.plugins_enabled')
    : t('workspace.http_request.plugins_bypassed')
})
const hasResponseTrailers = computed(() =>
  (state.value.response?.trailers ?? []).some((item) => item.key.trim().length > 0),
)
const visibleResponseTab = computed<HttpRequestEditorState['activeResponseTab']>({
  get() {
    if (state.value.activeResponseTab === 'response-console' && hasRequestConsole.value) {
      return 'response-console'
    }
    if (hasErrorResponse.value) {
      return 'response-error'
    }
    if (!hasResponse.value && hasRequestConsole.value) {
      return 'response-console'
    }

    return (state.value.activeResponseTab === 'response-error' && !hasResponseErrorDetails.value) ||
      (state.value.activeResponseTab === 'response-cookies' && !hasResponseCookies.value) ||
      (state.value.activeResponseTab === 'response-trailers' && !hasResponseTrailers.value)
      ? 'response-headers'
      : state.value.activeResponseTab
  },
  set(value) {
    state.value.activeResponseTab = value
  },
})
const mountedResponseTabs = reactive(new Set<HttpRequestEditorState['activeResponseTab']>())

watch(visibleResponseTab, (tab) => mountedResponseTabs.add(tab), { immediate: true })
const enabledRequestHeaderCount = computed(
  () => state.value.headers.filter((item) => item.enabled).length,
)
const requestCookieCount = computed(() => countRequestCookieRows(state.value.headers))
const showMonacoBody = computed(() => {
  const type = state.value.requestBodyType
  return type === 'json' || type === 'text' || type === 'xml'
})
const showFileBody = computed(() => state.value.requestBodyType === 'file')
const showFormDataBody = computed(() => state.value.requestBodyType === 'form-data')
const showUrlEncodedBody = computed(() => state.value.requestBodyType === 'urlencoded')

function editableRowsToRecord(rows: EditableKeyValue[] | undefined): Record<string, string[]> {
  const record: Record<string, string[]> = {}
  for (const item of rows ?? []) {
    const key = item.key?.trim()
    if (!key) {
      continue
    }
    if (!record[key]) {
      record[key] = []
    }
    record[key]!.push(item.value ?? '')
  }
  return record
}

const responseHeadersRecord = computed<Record<string, string[]>>(() =>
  editableRowsToRecord(state.value.response?.headers),
)

const responseHeaderWarning = computed(() => {
  if (state.value.response?.headersTruncated) {
    return t('workspace.http_request.header_fields_truncated')
  }
  if (state.value.response && !state.value.response.headersHaveWireOrder) {
    return t('workspace.http_request.header_order_unavailable')
  }
  return ''
})

const hasResponseCookies = computed(() => hasHeader(responseHeadersRecord.value, 'set-cookie'))

const responseTrailerWarning = computed(() => {
  if (state.value.response?.trailersTruncated) {
    return t('workspace.http_request.trailer_fields_truncated')
  }
  if (state.value.response && !state.value.response.trailersHaveWireOrder) {
    return t('workspace.http_request.trailer_order_unavailable')
  }
  return ''
})

const responseCookieFields = computed(() => responseCookiesRecord(responseHeadersRecord.value))
const responseCookieCount = computed(() => headersRecordToFields(responseCookieFields.value).length)

const responseContentType = computed(() => {
  const headers = responseHeadersRecord.value
  for (const [key, values] of Object.entries(headers)) {
    if (key.toLowerCase() === 'content-type') {
      return values[0] ?? ''
    }
  }
  return ''
})

const errorCardThemeVars = computed(() => {
  if (themeStore.isDark) {
    return {
      '--error-card-border': 'rgba(248, 113, 113, 0.26)',
      '--error-card-bg-top': 'rgba(127, 29, 29, 0.42)',
      '--error-card-bg-bottom': 'rgba(127, 29, 29, 0.12)',
      '--error-card-icon-bg': 'rgba(248, 113, 113, 0.18)',
      '--error-card-icon-color': '#fda4af',
      '--error-card-summary-border': 'rgba(248, 113, 113, 0.22)',
      '--error-card-summary-bg': 'rgba(127, 29, 29, 0.10)',
    }
  }

  return {
    '--error-card-border': 'rgba(220, 38, 38, 0.16)',
    '--error-card-bg-top': 'rgba(220, 38, 38, 0.08)',
    '--error-card-bg-bottom': 'rgba(220, 38, 38, 0.03)',
    '--error-card-icon-bg': 'rgba(220, 38, 38, 0.14)',
    '--error-card-icon-color': '#dc2626',
    '--error-card-summary-border': 'rgba(220, 38, 38, 0.14)',
    '--error-card-summary-bg': 'rgba(220, 38, 38, 0.06)',
  }
})

const responseBodySourcePath = computed(() => {
  try {
    return new URL(state.value.url).pathname || ''
  } catch {
    return ''
  }
})

const responseStatusTagType = computed<SummaryTagType>(() => {
  if (hasErrorResponse.value) {
    return 'error'
  }
  const statusCode = state.value.response?.statusCode ?? 0
  if (statusCode >= 200 && statusCode < 300) return 'success'
  if (statusCode >= 300 && statusCode < 400) return 'info'
  if (statusCode >= 400 && statusCode < 500) return 'warning'
  if (statusCode >= 500) return 'error'
  return 'default'
})

const chipToneClass: Record<SummaryTagType, string> = {
  default: 'bg-app-control text-app-text-secondary',
  success: 'bg-[color-mix(in_srgb,var(--app-success-color)_14%,transparent)] text-app-success',
  info: 'bg-app-accent-selected text-app-accent',
  primary: 'bg-app-accent-selected text-app-accent',
  warning: 'bg-[color-mix(in_srgb,var(--app-warning-color)_16%,transparent)] text-app-warning',
  error: 'bg-[color-mix(in_srgb,var(--app-error-color)_14%,transparent)] text-app-error',
}

const responseMetaChips = computed<ResponseMetaChip[]>(() => {
  if (!hasResponse.value || hasErrorResponse.value) {
    return []
  }

  const chips: ResponseMetaChip[] = [
    {
      key: 'status',
      text: state.value.response?.statusCode ? String(state.value.response.statusCode) : '-',
      type: responseStatusTagType.value,
    },
    {
      key: 'protocol',
      text: state.value.response?.protocol || '-',
      type: 'default',
    },
  ]

  if (state.value.response?.streamState === 'streaming') {
    chips.push({
      key: 'stream',
      text: t('workspace.http_request.streaming'),
      type: 'info',
    })
  } else if (state.value.response?.streamState === 'stopped') {
    chips.push({
      key: 'stream',
      text: t('workspace.http_request.stream_stopped'),
      type: 'default',
    })
  } else if (state.value.response?.streamState === 'completed') {
    chips.push({
      key: 'stream',
      text: t('workspace.http_request.stream_completed'),
      type: 'success',
    })
  } else if (state.value.response?.streamState === 'error') {
    chips.push({
      key: 'stream',
      text: t('workspace.http_request.stream_error'),
      type: 'error',
    })
  }

  return chips
})
function pluginOutcomeLabel(outcome: string) {
  const supported = new Set([
    'completed',
    'completed_with_plugin_error',
    'blocked_request',
    'blocked_response',
    'plugin_failed',
  ])
  const key = supported.has(outcome) ? outcome : 'plugin_failed'
  return t(`workspace.http_request.plugin_outcome_${key}`)
}

function buildPluginErrorDetails(
  outcome: string,
  execution: NonNullable<HttpRequestEditorState['response']>['pluginExecution'],
) {
  const details = (execution?.diagnostics ?? []).map((diagnostic) => {
    const owner =
      diagnostic.pluginId === 'current-request-script'
        ? t('workspace.http_request.inline_script_name')
        : diagnostic.name || diagnostic.pluginId
    return owner ? `${owner}\n${diagnostic.message}` : diagnostic.message
  })
  return details.length > 0 ? details.join('\n\n') : pluginOutcomeLabel(outcome)
}
const responseTabsUi = { list: 'items-center' }

const monacoLanguage = computed(() => {
  switch (state.value.requestBodyType) {
    case 'json':
      return 'json'
    case 'xml':
      return 'xml'
    default:
      return 'plaintext'
  }
})

const requestTabItems = computed<TabsItem[]>(() => [
  {
    value: 'headers',
    label: `${t('workspace.http_request.request_headers')}(${enabledRequestHeaderCount.value})`,
  },
  {
    value: 'query',
    label: `${t('workspace.http_request.query')}(${queryCount.value})`,
  },
  {
    value: 'cookies',
    label: `${t('workspace.http_request.cookies')}(${requestCookieCount.value})`,
  },
  { value: 'body', label: t('workspace.http_request.request_body') },
  {
    value: 'script',
    label: t('workspace.http_request.inline_script'),
    icon: state.value.inlineScriptEnabled ? 'i-lucide-circle-check' : undefined,
  },
  { value: 'settings', label: t('workspace.http_request.settings') },
])

const responseTabItems = computed<TabsItem[]>(() => {
  if (hasErrorResponse.value) {
    return [
      { value: 'response-error', label: t('workspace.http_request.response_error') },
      { value: 'response-console', label: t('workspace.http_request.console') },
    ]
  }
  if (!hasResponse.value) {
    return [{ value: 'response-console', label: t('workspace.http_request.console') }]
  }
  const items: TabsItem[] = [
    {
      value: 'response-headers',
      label: `${t('workspace.http_request.response_headers')}(${state.value.response?.headers.length || 0})`,
    },
  ]
  if (hasResponseErrorDetails.value) {
    items.unshift({ value: 'response-error', label: t('workspace.http_request.response_error') })
  }
  if (hasResponseCookies.value) {
    items.push({
      value: 'response-cookies',
      label: `${t('workspace.http_request.cookies')}(${responseCookieCount.value})`,
    })
  }
  if (hasResponseTrailers.value) {
    items.push({
      value: 'response-trailers',
      label: `${t('workspace.http_request.response_trailers')}(${state.value.response?.trailers.length || 0})`,
    })
  }
  items.push({ value: 'response-body', label: t('workspace.http_request.response_body') })
  items.push({ value: 'response-console', label: t('workspace.http_request.console') })
  return items
})

watch(
  () => state.value.url,
  (url) => {
    workspaceStore.updateRequestTabTitle(props.tabKey, url)
  },
  { immediate: true },
)

watch([() => state.value.method, () => state.value.url], ([method, url]) => {
  state.value.headers = synchronizeRequestRouteHeaders(
    state.value.headers,
    state.value.settings.protocol,
    method,
    url,
  )
})

function updateRequestProtocol(value: string | number | undefined) {
  if (value !== 'auto' && value !== 'http1' && value !== 'http2') {
    return
  }
  const protocol = value as HttpRequestProtocol
  if (protocol === state.value.settings.protocol) {
    return
  }
  state.value.headers = convertRequestRouteHeaders(
    state.value.headers,
    protocol,
    state.value.method,
    state.value.url,
  )
  state.value.settings.protocol = protocol
}

function addRequestHeader() {
  const emptyRow: EditableKeyValue = {
    key: '',
    value: '',
    enabled: true,
  }
  state.value.headers.push(emptyRow)
}

function buildRequestHeaders(): HeaderField[] {
  return editableRowsToHeaderFields(state.value.headers)
}

function createEmptyHeaderRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

function requestHeadersForCopy(): HeaderField[] {
  return editableRowsToHeaderFields(state.value.headers)
}

async function copyText(text: string, successMessage: string) {
  try {
    await copyTextToClipboard(text)
    notify.success(successMessage)
  } catch (error) {
    notify.error(t('workspace.http_request.headers_copy_failed', { error: String(error) }))
  }
}

async function copyRequestHeadersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(requestHeadersForCopy()),
    t('workspace.http_request.request_headers_copied_json'),
  )
}

async function copyRequestHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(requestHeadersForCopy()),
    t('workspace.http_request.request_headers_copied_text'),
  )
}

async function copyResponseHeadersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(editableRowsToHeaderFields(state.value.response?.headers ?? [])),
    t('workspace.http_request.response_headers_copied_json'),
  )
}

async function copyResponseHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(editableRowsToHeaderFields(state.value.response?.headers ?? [])),
    t('workspace.http_request.response_headers_copied_text'),
  )
}

async function copyResponseTrailersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(editableRowsToHeaderFields(state.value.response?.trailers ?? [])),
    t('workspace.http_request.response_trailers_copied_json'),
  )
}

async function copyResponseTrailersAsText() {
  await copyText(
    formatHeaderFieldsAsText(editableRowsToHeaderFields(state.value.response?.trailers ?? [])),
    t('workspace.http_request.response_trailers_copied_text'),
  )
}

async function copyResponseErrorDetails() {
  await copyText(
    responseErrorDetailsText.value,
    t('workspace.http_request.response_error_details_copied'),
  )
}

function clearRequestHeaders() {
  state.value.headers.splice(0, state.value.headers.length, createEmptyHeaderRow())
  notify.success(t('workspace.http_request.request_headers_cleared'))
}

function mapHeaderFieldsToRows(
  fields: ({ name?: string; value?: string } | null)[] | null | undefined,
): EditableKeyValue[] {
  return (fields ?? [])
    .filter((field): field is { name?: string; value?: string } => field !== null)
    .map((field) => ({
      key: field.name ?? '',
      value: field.value ?? '',
      enabled: true,
    }))
}

function validateRequestURL(rawURL: string): string | null {
  let parsedURL: URL
  try {
    parsedURL = new URL(rawURL)
  } catch {
    return t('workspace.http_request.invalid_url')
  }
  if (parsedURL.protocol !== 'http:' && parsedURL.protocol !== 'https:') {
    return t('workspace.http_request.invalid_url_scheme')
  }
  return null
}

function createPluginExecutionId() {
  const cryptoApi = globalThis.crypto
  if (cryptoApi && typeof cryptoApi.randomUUID === 'function') {
    return cryptoApi.randomUUID()
  }
  return `request-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function buildRequestBodyPayload(): SendRequestBody {
  const bodyType = state.value.requestBodyType
  if (bodyType === 'none') {
    return {
      bodyType: bodyType as SendRequestBodyType,
      text: '',
    }
  }

  if (bodyType === 'json' || bodyType === 'text' || bodyType === 'xml') {
    return {
      bodyType: bodyType as SendRequestBodyType,
      text: state.value.requestBodyText,
    }
  }

  if (bodyType === 'file') {
    const file = state.value.requestBodyFile
    if (!file?.path) {
      throw new Error(t('workspace.http_request.file_required'))
    }
    return {
      bodyType: bodyType as SendRequestBodyType,
      text: '',
      file: {
        path: file.path,
        name: file.name,
        size: file.size,
      },
    }
  }

  if (bodyType === 'urlencoded') {
    const urlEncoded = state.value.requestBodyUrlEncoded
      .filter((item) => item.enabled && item.key.trim())
      .map((item) => ({
        enabled: true,
        name: item.key.trim(),
        value: item.value,
      }))

    return {
      bodyType: bodyType as SendRequestBodyType,
      text: '',
      urlEncoded,
    }
  }

  const formData: NonNullable<SendRequestBody['formData']> = []
  for (const item of state.value.requestBodyFormData) {
    if (!item.enabled) {
      continue
    }
    const name = item.name.trim()
    if (!name) {
      continue
    }
    if (item.itemType === 'file') {
      if (!item.file?.path) {
        throw new Error(t('workspace.http_request.form_data_file_required', { name }))
      }
      formData.push({
        enabled: true,
        name,
        itemType: 'file',
        value: '',
        file: {
          path: item.file.path,
          name: item.file.name,
          size: item.file.size,
        },
      })
      continue
    }
    formData.push({
      enabled: true,
      name,
      itemType: 'text',
      value: item.value,
    })
  }

  return {
    bodyType: bodyType as SendRequestBodyType,
    text: '',
    formData,
  }
}

async function handleSend() {
  if (isHttpOperationActive.value) {
    return
  }

  const urlError = validateRequestURL(state.value.url)
  if (urlError) {
    notify.error(urlError)
    return
  }

  if (!state.value.method.trim()) {
    notify.error(t('workspace.http_request.invalid_method'))
    return
  }

  if (state.value.inlineScriptEnabled && !pythonPluginsEnabled.value) {
    state.value.activeRequestTab = 'script'
    notify.error(t('workspace.http_request.inline_script_runtime_disabled'))
    return
  }

  if (state.value.inlineScriptEnabled && !state.value.inlineScriptSource.trim()) {
    state.value.activeRequestTab = 'script'
    notify.error(t('workspace.http_request.inline_script_empty'))
    return
  }

  const invalidHeaderName = findInvalidRequestHeaderName(
    state.value.headers,
    state.value.settings.protocol,
  )
  if (invalidHeaderName) {
    state.value.activeRequestTab = 'headers'
    notify.error(
      t('workspace.http_request.header_invalid_name', {
        name: invalidHeaderName,
      }),
    )
    return
  }

  let bodyPayload: ReturnType<typeof buildRequestBodyPayload>
  try {
    bodyPayload = buildRequestBodyPayload()
  } catch (error) {
    notify.error(String(error))
    return
  }

  const pluginExecutionRequested =
    pythonPluginsEnabled.value &&
    (state.value.pluginsEnabled || state.value.inlineScriptEnabled)
  const pluginExecutionId = pluginExecutionRequested ? createPluginExecutionId() : ''
  state.value.scriptExecutionId = pluginExecutionId
  state.value.scriptConsoleEntries.splice(0, state.value.scriptConsoleEntries.length)
  if (pluginExecutionRequested) {
    state.value.activeResponseTab = 'response-console'
  }

  state.value.isSending = true
  sendOperationGeneration += 1
  const sendCall = SendHTTPRequest(
    {
      proxyMode: state.value.settings.proxyMode as SendRequestProxyMode,
      protocol: state.value.settings.protocol as SendRequestProtocol,
      customProxy:
        state.value.settings.proxyMode === 'custom' ? state.value.settings.customProxy : '',
      timeoutMs: state.value.settings.timeoutMs > 0 ? state.value.settings.timeoutMs : 0,
      tlsClientHelloId: state.value.settings.tlsClientHelloId as TLSClientHelloID,
      http2Fingerprint: state.value.settings.http2Fingerprint,
      disablePlugins: !state.value.pluginsEnabled,
      pluginExecutionId,
      inlinePythonScript: state.value.inlineScriptEnabled
        ? {
            enabled: true,
            source: state.value.inlineScriptSource,
          }
        : undefined,
    },
    state.value.method,
    state.value.url,
    buildRequestHeaders(),
    bodyPayload,
  )
  pendingSendCall = sendCall
  try {
    const response = await sendCall
    if (pendingSendCall !== sendCall) {
      return
    }

    const streamSessionID = response.streamSessionId?.trim() ?? ''
    const isPluginTerminal =
      response.outcome === 'blocked_request' ||
      response.outcome === 'blocked_response' ||
      response.outcome === 'plugin_failed'
    const hasPluginErrorDetails =
      isPluginTerminal || (response.pluginExecution?.diagnostics?.length ?? 0) > 0
    const isStreaming =
      !isPluginTerminal && response.streaming === true && streamSessionID.length > 0
    const headersHaveWireOrder =
      response.headerFields !== null &&
      response.headerFields !== undefined &&
      response.headerOrderUnavailable !== true
    state.value.response = {
      kind: isPluginTerminal ? 'plugin' : 'success',
      outcome: response.outcome || 'completed',
      pluginExecution: response.pluginExecution ?? null,
      streamState: isStreaming ? 'streaming' : '',
      statusCode: response.statusCode,
      statusText: response.statusText,
      protocol: response.protocol,
      headers: mapHeaderFieldsToRows(response.headerFields),
      headersHaveWireOrder,
      headersTruncated: response.headersTruncated === true,
      trailers: mapHeaderFieldsToRows(response.trailerFields),
      trailersHaveWireOrder:
        response.trailerFields !== null &&
        response.trailerFields !== undefined &&
        response.trailerOrderUnavailable !== true,
      trailersTruncated: response.trailersTruncated === true,
      body: response.body ?? '',
      bodyEncoding: response.bodyEncoding === 'base64' ? 'base64' : '',
      errorMessage: hasPluginErrorDetails
        ? buildPluginErrorDetails(response.outcome, response.pluginExecution ?? null)
        : '',
    }
    state.value.isStreaming = isStreaming
    state.value.streamSessionId = isStreaming ? streamSessionID : ''
    state.value.scriptExecutionId =
      response.pluginExecution?.executionId || state.value.scriptExecutionId
    const streamRegistered = isStreaming
      ? workspaceStore.registerHTTPRequestStream(props.tabKey, streamSessionID)
      : false
    if (hasPluginErrorDetails) {
      state.value.activeResponseTab = 'response-error'
    } else if (isStreaming) {
      state.value.activeResponseTab = streamRegistered ? 'response-body' : 'response-error'
    } else if (state.value.inlineScriptEnabled) {
      state.value.activeResponseTab = 'response-console'
    } else {
      state.value.activeResponseTab = 'response-headers'
    }
  } catch (error) {
    if (pendingSendCall !== sendCall) {
      return
    }
    if (error instanceof CancelError) {
      state.value.isStreaming = false
      state.value.streamSessionId = ''
      state.value.response = {
        kind: 'cancelled',
        outcome: '',
        pluginExecution: null,
        streamState: '',
        statusCode: 0,
        statusText: '',
        protocol: '',
        headers: [],
        headersHaveWireOrder: false,
        headersTruncated: false,
        trailers: [],
        trailersHaveWireOrder: false,
        trailersTruncated: false,
        body: '',
        bodyEncoding: '',
        errorMessage: '',
      }
      return
    }
    const errorMessage = String(error)
    state.value.isStreaming = false
    state.value.streamSessionId = ''
    state.value.response = {
      kind: 'error',
      outcome: '',
      pluginExecution: null,
      streamState: 'error',
      statusCode: 0,
      statusText: '',
      protocol: '',
      headers: [],
      headersHaveWireOrder: false,
      headersTruncated: false,
      trailers: [],
      trailersHaveWireOrder: false,
      trailersTruncated: false,
      body: '',
      bodyEncoding: '',
      errorMessage,
    }
    state.value.activeResponseTab = 'response-error'
  } finally {
    if (pendingSendCall === sendCall) {
      pendingSendCall = null
      state.value.isSending = false
    }
  }
}

async function handleStop() {
  const sendCall = pendingSendCall
  const operationGeneration = sendOperationGeneration
  if (sendCall) {
    await sendCall.cancel()
  }

  if (sendOperationGeneration !== operationGeneration) {
    return
  }
  if (state.value.isStreaming) {
    try {
      await workspaceStore.disconnectHTTPRequestTab(props.tabKey)
    } catch (error) {
      console.error('Failed to stop HTTP request stream:', error)
    }
  }
}

function handlePrimaryAction() {
  return isHttpOperationActive.value ? handleStop() : handleSend()
}

function clearScriptConsole() {
  state.value.scriptConsoleEntries.splice(0, state.value.scriptConsoleEntries.length)
}

const offRequestSendShortcut = registerShortcutHandler({
  commandId: 'requestEditor.send',
  when: () =>
    workbenchStore.activeContent === 'traffic' &&
    workspaceStore.activeTab.type === 'http-request' &&
    workspaceStore.activeTab.key === props.tabKey,
  enabled: () => !state.value.isSending && !state.value.isStreaming,
  run: () => handleSend(),
})

onBeforeUnmount(() => {
  void pendingSendCall?.cancel()
  offRequestSendShortcut()
  offPythonPluginLog()
})
</script>

<template>
  <div
    class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden bg-app-content"
    :style="requestLineThemeVars"
  >
    <div
      class="grid shrink-0 grid-cols-[minmax(0,1fr)_auto_110px] items-center gap-2 bg-app-content px-2.5 py-2 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div
        class="flex h-9 min-w-0 items-stretch overflow-hidden rounded-sm border border-app-border bg-app-panel"
        :style="methodThemeVars"
      >
        <div
          class="flex w-28 shrink-0 items-stretch border-r border-r-[color-mix(in_srgb,var(--method-accent-color)_22%,var(--app-border-color))] bg-[color-mix(in_srgb,var(--method-accent-color)_5%,var(--app-panel-bg))]"
        >
          <USelect
            v-model="state.method"
            class="h-full w-full"
            :items="methodOptions"
            variant="none"
            :ui="{
              base: 'h-full w-full rounded-none border-0 bg-transparent px-0 shadow-none hover:bg-transparent focus:bg-transparent',
              value: 'ps-3 text-sm font-bold',
              trailing: 'pe-2',
              trailingIcon: 'size-4 text-[color:var(--method-accent-color)]',
            }"
          >
            <template #default="{ modelValue }">
              <span :style="methodLabelStyle(modelValue)">{{ modelValue }}</span>
            </template>
            <template #item-label="{ item }">
              <span :style="methodLabelStyle(item.value)">{{ item.label }}</span>
            </template>
          </USelect>
        </div>
        <RequestUrlInput
          v-model="state.url"
          class="min-w-0 flex-1 [--request-url-height:100%] [--request-url-border-width:0px] [--request-url-border-color:transparent] [--request-url-bg:var(--app-panel-bg)] [--request-url-padding-x:12px] [--request-url-font-size:15px]"
          :placeholder="t('workspace.http_request.url_placeholder')"
        >
          <template #suffix>
            <RequestMitmProxyButton
              :active="isMitmProxyMode"
              :label="mitmProxyToggleLabel"
              @toggle="toggleMitmProxyMode"
            />
          </template>
        </RequestUrlInput>
      </div>
      <UTooltip :text="pluginToggleTooltip">
        <div
          class="flex h-9 shrink-0 items-center gap-2 rounded-sm border border-app-border bg-app-panel px-2.5"
          :class="
            pythonPluginsEnabled && state.pluginsEnabled
              ? 'text-app-accent'
              : 'text-app-text-muted'
          "
        >
          <UIcon name="i-lucide-file-code-2" class="size-4 shrink-0" />
          <USwitch
            v-model="state.pluginsEnabled"
            size="sm"
            :disabled="!pythonPluginsEnabled || isHttpOperationActive"
            :aria-label="t('workspace.http_request.plugins_toggle')"
          />
        </div>
      </UTooltip>
      <UTooltip
        :text="primaryActionTooltip"
        :kbds="isHttpOperationActive ? undefined : sendShortcutKbds"
      >
        <UButton
          class="h-9 w-full justify-center rounded-sm"
          :color="isHttpOperationActive ? 'error' : 'primary'"
          :icon="isHttpOperationActive ? 'i-lucide-square' : 'i-lucide-send'"
          :label="
            isHttpOperationActive
              ? t('workspace.http_request.stop')
              : t('workspace.http_request.send')
          "
          @click="handlePrimaryAction"
        />
      </UTooltip>
    </div>

    <div class="min-h-0 min-w-0 flex-1 overflow-hidden">
      <SplitterGroup direction="horizontal" class="flex h-full w-full min-w-0 bg-transparent">
        <SplitterPanel
          :default-size="50"
          :min-size="30"
          class="flex min-h-0 min-w-0 flex-col overflow-hidden!"
        >
          <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-app-panel">
            <div class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
              <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                <UTabs
                  :model-value="state.activeRequestTab"
                  :items="requestTabItems"
                  :content="false"
                  variant="link"
                  class="w-full min-w-0 shrink-0 px-2.5"
                  @update:model-value="
                    state.activeRequestTab = $event as HttpRequestEditorState['activeRequestTab']
                  "
                />

                <div
                  v-if="mountedRequestTabs.has('headers')"
                  v-show="state.activeRequestTab === 'headers'"
                  class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                  data-name="headers"
                  role="tabpanel"
                >
                  <div class="flex h-full min-h-0 flex-col overflow-hidden">
                    <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
                      <span class="text-sm text-app-text-muted">
                        {{ t('workspace.http_request.request_header_list') }}
                      </span>
                      <div class="flex gap-1">
                        <UTooltip :text="t('workspace.http_request.copy_request_headers_json')">
                          <UButton
                            icon="i-lucide-code"
                            color="neutral"
                            variant="ghost"
                            size="sm"
                            square
                            :aria-label="t('workspace.http_request.copy_request_headers_json')"
                            @click="copyRequestHeadersAsJson"
                          />
                        </UTooltip>
                        <UTooltip :text="t('workspace.http_request.copy_request_headers_text')">
                          <UButton
                            icon="i-lucide-copy"
                            color="neutral"
                            variant="ghost"
                            size="sm"
                            square
                            :aria-label="t('workspace.http_request.copy_request_headers_text')"
                            @click="copyRequestHeadersAsText"
                          />
                        </UTooltip>
                        <UTooltip :text="t('workspace.http_request.clear_request_headers')">
                          <UButton
                            icon="i-lucide-trash-2"
                            color="neutral"
                            variant="ghost"
                            size="sm"
                            square
                            :aria-label="t('workspace.http_request.clear_request_headers')"
                            @click="clearRequestHeaders"
                          />
                        </UTooltip>
                        <UTooltip :text="t('workspace.http_request.add_request_header')">
                          <UButton
                            icon="i-lucide-plus"
                            color="neutral"
                            variant="ghost"
                            size="sm"
                            square
                            :aria-label="t('workspace.http_request.add_request_header')"
                            @click="addRequestHeader"
                          />
                        </UTooltip>
                      </div>
                    </div>
                    <EditableKeyValueTable
                      v-model="state.headers"
                      :protocol="state.settings.protocol"
                      :show-duplicate-warning="false"
                      validate-header-names
                    />
                  </div>
                </div>

                <RequestQueryPane
                  v-if="mountedRequestTabs.has('query')"
                  v-show="state.activeRequestTab === 'query'"
                  v-model:params="state.params"
                  data-name="query"
                  role="tabpanel"
                />

                <RequestCookiePane
                  v-if="mountedRequestTabs.has('cookies')"
                  v-show="state.activeRequestTab === 'cookies'"
                  v-model:headers="state.headers"
                  data-name="cookies"
                  role="tabpanel"
                />

                <div
                  v-if="mountedRequestTabs.has('body')"
                  v-show="state.activeRequestTab === 'body'"
                  class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                  data-name="body"
                  role="tabpanel"
                >
                  <div class="flex h-full min-h-0 min-w-0 flex-col p-2.5">
                    <div class="mb-2 flex items-center justify-between gap-2">
                      <div class="flex items-center gap-2">
                        <span class="text-sm text-app-text-muted">{{
                          t('workspace.http_request.body_type')
                        }}</span>
                        <USelect
                          v-model="state.requestBodyType"
                          class="w-35"
                          :items="bodyTypeOptions"
                        />
                      </div>
                    </div>

                    <div class="flex min-h-0 min-w-0 flex-1 items-stretch overflow-hidden">
                      <MonacoBodyEditor
                        v-if="showMonacoBody"
                        v-model:value="state.requestBodyText"
                        class="h-full min-h-0 min-w-0 flex-1"
                        :language="monacoLanguage"
                      />

                      <RequestFileBody
                        v-else-if="showFileBody"
                        class="h-full min-h-0 flex-1"
                        :tab-key="props.tabKey"
                        :file="state.requestBodyFile"
                        @update:file="state.requestBodyFile = $event"
                      />

                      <HttpRequestFormDataBody
                        v-else-if="showFormDataBody"
                        class="h-full min-h-0 flex-1"
                        :tab-key="props.tabKey"
                        :items="state.requestBodyFormData"
                        @update:items="state.requestBodyFormData = $event"
                      />

                      <HttpRequestUrlEncodedBody
                        v-else-if="showUrlEncodedBody"
                        class="h-full min-h-0 flex-1"
                        :items="state.requestBodyUrlEncoded"
                        @update:items="state.requestBodyUrlEncoded = $event"
                      />
                    </div>
                  </div>
                </div>

                <div
                  v-if="mountedRequestTabs.has('script')"
                  v-show="state.activeRequestTab === 'script'"
                  class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                  data-name="script"
                  role="tabpanel"
                >
                  <div class="flex shrink-0 items-center justify-start px-2.5 py-2">
                    <div class="flex shrink-0 items-center gap-2">
                      <span class="text-sm text-muted">
                        {{ t('workspace.http_request.inline_script_enable') }}
                      </span>
                      <USwitch
                        v-model="state.inlineScriptEnabled"
                        size="sm"
                        :disabled="isHttpOperationActive"
                        :aria-label="t('workspace.http_request.inline_script_enable')"
                      />
                    </div>
                  </div>
                  <UAlert
                    v-if="!pythonPluginsEnabled"
                    icon="i-lucide-triangle-alert"
                    color="warning"
                    variant="soft"
                    :description="t('workspace.http_request.inline_script_runtime_disabled')"
                    class="mx-2.5 mb-2"
                  />
                  <MonacoBodyEditor
                    v-model:value="state.inlineScriptSource"
                    class="min-h-0 min-w-0 flex-1"
                    language="python"
                    flow-lens-python-api
                    :readonly="isHttpOperationActive"
                    :word-wrap="false"
                    :options="{ tabSize: 4, insertSpaces: true }"
                  />
                </div>

                <div
                  v-if="mountedRequestTabs.has('settings')"
                  v-show="state.activeRequestTab === 'settings'"
                  class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                  data-name="settings"
                  role="tabpanel"
                >
                  <div
                    class="grid grid-cols-[minmax(9.25rem,max-content)_minmax(0,1fr)] items-center gap-2.5 p-2.5"
                  >
                    <div class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.protocol') }}
                      </span>
                      <USelect
                        :model-value="state.settings.protocol"
                        :items="protocolOptions"
                        @update:model-value="updateRequestProtocol"
                      />
                    </div>
                    <div class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.proxy_mode') }}
                      </span>
                      <USelect v-model="state.settings.proxyMode" :items="proxyModeOptions" />
                    </div>
                    <div class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.tls_fingerprint') }}
                      </span>
                      <USelect
                        v-model="state.settings.tlsClientHelloId"
                        :items="tlsClientHelloOptions"
                      />
                    </div>
                    <div class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.http2_fingerprint') }}
                      </span>
                      <UInput
                        v-model="state.settings.http2Fingerprint"
                        :disabled="state.settings.protocol === 'http1'"
                      />
                    </div>
                    <div v-if="state.settings.proxyMode === 'custom'" class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.custom_proxy') }}
                      </span>
                      <UInput
                        v-model="state.settings.customProxy"
                        :placeholder="t('workspace.http_request.custom_proxy_placeholder')"
                      />
                    </div>
                    <div class="contents">
                      <span class="whitespace-nowrap text-sm leading-[1.35] text-app-text-muted">
                        {{ t('workspace.http_request.request_timeout') }}
                      </span>
                      <UInputNumber
                        orientation="vertical"
                        v-model="state.settings.timeoutMs"
                        :min="0"
                        :step="100"
                        :format-options="{ minimumFractionDigits: 0, maximumFractionDigits: 0 }"
                        :placeholder="t('workspace.http_request.request_timeout_placeholder')"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </SplitterPanel>

        <AppSplitterResizeHandle />
        <SplitterPanel
          :default-size="50"
          :min-size="18"
          class="flex min-h-0 min-w-0 flex-col overflow-hidden!"
        >
          <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-app-panel">
            <AppLoading
              v-if="state.isSending && visibleResponseTab !== 'response-console'"
              fill
              :label="t('workspace.http_request.response_sending_title')"
            />
            <UEmpty
              v-else-if="!hasResponse && !hasRequestConsole"
              icon="i-lucide-send"
              :title="t('workspace.http_request.request_not_started')"
              :size="appEmptyStateSize"
              variant="naked"
              :ui="appEmptyStateUi"
            />
            <UEmpty
              v-else-if="hasCancelledResponse && visibleResponseTab !== 'response-console'"
              icon="i-lucide-circle-stop"
              :title="t('workspace.http_request.request_cancelled_title')"
              :description="t('workspace.http_request.request_cancelled_description')"
              :size="appEmptyStateSize"
              variant="naked"
              :ui="appEmptyStateUi"
            />
            <template v-else>
              <div class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                  <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                    <div class="flex min-w-0 shrink-0 items-center">
                      <UTabs
                        :model-value="visibleResponseTab"
                        :items="responseTabItems"
                        :content="false"
                        variant="link"
                        :ui="responseTabsUi"
                        class="w-full min-w-0 shrink-0 px-2.5"
                        @update:model-value="
                          visibleResponseTab = $event as HttpRequestEditorState['activeResponseTab']
                        "
                      >
                        <template #list-trailing>
                          <div
                            v-if="responseMetaChips.length > 0"
                            class="ml-auto flex min-w-max flex-[0_0_auto] items-center gap-1.5"
                          >
                            <span
                              v-for="chip in responseMetaChips"
                              :key="chip.key"
                              class="inline-flex min-h-5 shrink-0 items-center rounded-full px-2 text-sm font-bold leading-none tabular-nums"
                              :class="chipToneClass[chip.type]"
                            >
                              {{ chip.text }}
                            </span>
                          </div>
                        </template>
                      </UTabs>
                    </div>
                    <div
                      v-if="
                        hasResponse &&
                        !hasErrorResponse &&
                        mountedResponseTabs.has('response-headers')
                      "
                      v-show="visibleResponseTab === 'response-headers'"
                      class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                      data-name="response-headers"
                      role="tabpanel"
                    >
                      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
                        <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
                          <span class="text-sm text-app-text-muted">
                            {{ t('workspace.http_request.response_header_list') }}
                          </span>
                          <div class="flex gap-1">
                            <UTooltip
                              :text="t('workspace.http_request.copy_response_headers_json')"
                            >
                              <UButton
                                icon="i-lucide-code"
                                color="neutral"
                                variant="ghost"
                                size="sm"
                                square
                                :aria-label="t('workspace.http_request.copy_response_headers_json')"
                                @click="copyResponseHeadersAsJson"
                              />
                            </UTooltip>
                            <UTooltip
                              :text="t('workspace.http_request.copy_response_headers_text')"
                            >
                              <UButton
                                icon="i-lucide-copy"
                                color="neutral"
                                variant="ghost"
                                size="sm"
                                square
                                :aria-label="t('workspace.http_request.copy_response_headers_text')"
                                @click="copyResponseHeadersAsText"
                              />
                            </UTooltip>
                          </div>
                        </div>
                        <div class="min-h-0 flex-1 overflow-y-auto px-2.5 pb-2.5">
                          <UAlert
                            v-if="responseHeaderWarning"
                            icon="i-lucide-triangle-alert"
                            color="warning"
                            variant="soft"
                            :description="responseHeaderWarning"
                            class="mb-2"
                          />
                          <HeadersTable
                            :fields="
                              state.response?.headers.map((item) => ({
                                name: item.key,
                                value: item.value,
                              })) ?? []
                            "
                            class="flex-[0_0_auto]"
                          />
                        </div>
                      </div>
                    </div>
                    <CookieTablePane
                      v-if="
                        !hasErrorResponse &&
                        hasResponseCookies &&
                        mountedResponseTabs.has('response-cookies')
                      "
                      v-show="visibleResponseTab === 'response-cookies'"
                      data-name="response-cookies"
                      role="tabpanel"
                      :title="t('detail.cookie_list')"
                      :cookies="responseCookieFields"
                      :empty-title="t('detail.no_cookies')"
                      :raw-headers="responseHeadersRecord"
                      header-name="set-cookie"
                    />
                    <div
                      v-if="
                        !hasErrorResponse &&
                        hasResponseTrailers &&
                        mountedResponseTabs.has('response-trailers')
                      "
                      v-show="visibleResponseTab === 'response-trailers'"
                      class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                      data-name="response-trailers"
                      role="tabpanel"
                    >
                      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
                        <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
                          <span class="text-sm text-app-text-muted">
                            {{ t('workspace.http_request.response_trailer_list') }}
                          </span>
                          <div class="flex gap-1">
                            <UTooltip
                              :text="t('workspace.http_request.copy_response_trailers_json')"
                            >
                              <UButton
                                icon="i-lucide-code"
                                color="neutral"
                                variant="ghost"
                                size="sm"
                                square
                                :aria-label="
                                  t('workspace.http_request.copy_response_trailers_json')
                                "
                                @click="copyResponseTrailersAsJson"
                              />
                            </UTooltip>
                            <UTooltip
                              :text="t('workspace.http_request.copy_response_trailers_text')"
                            >
                              <UButton
                                icon="i-lucide-copy"
                                color="neutral"
                                variant="ghost"
                                size="sm"
                                square
                                :aria-label="
                                  t('workspace.http_request.copy_response_trailers_text')
                                "
                                @click="copyResponseTrailersAsText"
                              />
                            </UTooltip>
                          </div>
                        </div>
                        <div class="min-h-0 flex-1 overflow-y-auto px-2.5 pb-2.5">
                          <UAlert
                            v-if="responseTrailerWarning"
                            icon="i-lucide-triangle-alert"
                            color="warning"
                            variant="soft"
                            :description="responseTrailerWarning"
                            class="mb-2"
                          />
                          <HeadersTable
                            :fields="
                              state.response?.trailers.map((item) => ({
                                name: item.key,
                                value: item.value,
                              })) ?? []
                            "
                            class="flex-[0_0_auto]"
                          />
                        </div>
                      </div>
                    </div>
                    <div
                      v-if="
                        hasResponse && !hasErrorResponse && mountedResponseTabs.has('response-body')
                      "
                      v-show="visibleResponseTab === 'response-body'"
                      class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                      data-name="response-body"
                      role="tabpanel"
                    >
                      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
                        <BodyViewer
                          class="py-2.5 pl-2.5"
                          :body="state.response?.body || ''"
                          :content-type="responseContentType"
                          :body-encoding="state.response?.bodyEncoding || ''"
                          :source-path="responseBodySourcePath"
                        />
                      </div>
                    </div>
                    <div
                      v-if="mountedResponseTabs.has('response-console')"
                      v-show="visibleResponseTab === 'response-console'"
                      class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                      data-name="response-console"
                    >
                      <HttpRequestPythonConsole
                        :entries="state.scriptConsoleEntries"
                        :running="state.isSending"
                        @clear="clearScriptConsole"
                      />
                    </div>
                    <div
                      v-if="hasResponseErrorDetails && mountedResponseTabs.has('response-error')"
                      v-show="visibleResponseTab === 'response-error'"
                      class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                      data-name="response-error"
                      role="tabpanel"
                    >
                      <div class="flex flex-col overflow-auto p-2.5">
                        <div
                          class="flex flex-col gap-3.5 rounded-lg border border-app-border p-4"
                          :style="errorCardThemeVars"
                        >
                          <div class="flex items-center gap-3">
                            <div
                              class="flex size-10 shrink-0 items-center justify-center rounded-xl bg-(--error-card-icon-bg) text-(--error-card-icon-color)"
                            >
                              <UIcon name="i-lucide-circle-alert" class="flex size-5.5" />
                            </div>
                            <div class="flex min-w-0 flex-col gap-1">
                              <div class="text-[15px] font-bold text-app-text">
                                {{ responseErrorTitle }}
                              </div>
                            </div>
                          </div>

                          <div class="flex flex-col gap-2">
                            <div class="flex items-center justify-between gap-2">
                              <div class="text-sm font-semibold text-app-text-secondary">
                                {{ t('workspace.http_request.response_error_details') }}
                              </div>
                              <UTooltip
                                :text="t('workspace.http_request.copy_response_error_details')"
                              >
                                <UButton
                                  icon="i-lucide-copy"
                                  color="neutral"
                                  variant="ghost"
                                  size="sm"
                                  square
                                  :aria-label="
                                    t('workspace.http_request.copy_response_error_details')
                                  "
                                  @click="copyResponseErrorDetails"
                                />
                              </UTooltip>
                            </div>
                            <div
                              class="overflow-x-auto border border-(--error-card-summary-border) bg-(--error-card-summary-bg) px-3 py-2.5 font-mono text-sm leading-5 whitespace-pre-wrap text-app-text [word-break:break-word]"
                            >
                              {{ responseErrorDetailsText }}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </SplitterPanel>
      </SplitterGroup>
    </div>
  </div>
</template>
