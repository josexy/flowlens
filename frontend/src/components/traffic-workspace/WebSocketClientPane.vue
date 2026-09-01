<script setup lang="ts">
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { computed, onBeforeUnmount, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { SplitterGroup, SplitterPanel } from 'reka-ui'
import type { TabsItem } from '@nuxt/ui'
import { useNotify } from '@/composables/useNotify'
import type { EditableKeyValue, WebSocketClientState } from '@/types/request-editor'
import AppLoading from '@/components/common/AppLoading.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import AppSplitterResizeHandle from '@/components/common/AppSplitterResizeHandle.vue'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import CookieTablePane from '@/components/traffic/CookieTablePane.vue'
import WebSocketMessageStream from '@/components/common/websocket/WebSocketMessageStream.vue'
import RequestUrlInput from '@/components/traffic-workspace/RequestUrlInput.vue'
import RequestMitmProxyButton from '@/components/traffic-workspace/RequestMitmProxyButton.vue'
import RequestFileBody from '@/components/traffic-workspace/RequestFileBody.vue'
import EditableKeyValueTable from '@/components/traffic-workspace/EditableKeyValueTable.vue'
import RequestCookiePane from '@/components/traffic-workspace/RequestCookiePane.vue'
import RequestQueryPane from '@/components/traffic-workspace/RequestQueryPane.vue'
import { useMitmProxyModeToggle } from '@/composables/useMitmProxyModeToggle'
import { useRequestQuerySync } from '@/composables/useRequestQuerySync'
import { useThemeStore } from '@/stores/theme'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'
import {
  editableRowsToHeaderFields,
  editableRowsToHeadersRecord,
  findInvalidRequestHeaderName,
  formatHeaderFieldsAsJson,
  formatHeaderFieldsAsText,
  headersRecordToFields,
} from '@/utils/headers'
import {
  countRequestCookieRows,
  hasHeader,
  responseCookiesRecord,
} from '@/utils/cookies'

type SummaryTagType = 'default' | 'error' | 'primary' | 'info' | 'success' | 'warning'

const props = defineProps<{
  tabKey: string
}>()
const state = defineModel<WebSocketClientState>('state', { required: true })
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

const mountedLeftTabs = reactive(new Set<WebSocketClientState['activeLeftTab']>())

watch(
  () => state.value.activeLeftTab,
  (tab) => mountedLeftTabs.add(tab),
  { immediate: true },
)

const { t } = useI18n()
const themeStore = useThemeStore()
const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const notify = useNotify()
const sendShortcutKbds = useShortcutKbds('requestEditor.send')

const draftTypeOptions = computed(() => [
  { label: t('workspace.websocket_client.draft_type_text'), value: 'text' },
  { label: t('workspace.websocket_client.draft_type_json'), value: 'json' },
  { label: t('workspace.websocket_client.draft_type_xml'), value: 'xml' },
  { label: t('workspace.websocket_client.draft_type_binary_file'), value: 'binary-file' },
])

const proxyModeOptions = computed(() => [
  { label: t('workspace.http_request.proxy_mode_none'), value: 'none' },
  { label: t('workspace.http_request.proxy_mode_system'), value: 'system' },
  { label: t('workspace.http_request.proxy_mode_mitm'), value: 'mitm' },
  { label: t('workspace.http_request.proxy_mode_custom'), value: 'custom' },
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

const enabledHeaderCount = computed(() => state.value.headers.filter((item) => item.enabled).length)
const requestCookieCount = computed(() => countRequestCookieRows(state.value.headers))
const responseHeaderFields = computed(() =>
  editableRowsToHeaderFields(state.value.responseHeaders),
)
const responseHeadersRecord = computed(() =>
  editableRowsToHeadersRecord(state.value.responseHeaders),
)
const hasResponseHeaders = computed(() => responseHeaderFields.value.length > 0)
const hasResponseCookies = computed(() =>
  hasHeader(responseHeadersRecord.value, 'set-cookie'),
)
const responseCookieFields = computed(() => responseCookiesRecord(responseHeadersRecord.value))
const responseCookieCount = computed(
  () => headersRecordToFields(responseCookieFields.value).length,
)
const hasErrorResponse = computed(() => state.value.connectionStatus === 'error')
const hasCancelledResponse = computed(() => state.value.connectionStatus === 'cancelled')
const isConnecting = computed(() => state.value.connectionStatus === 'connecting')
const connectActionTooltip = computed(() => {
  if (isConnecting.value) {
    return t('workspace.websocket_client.stop_connecting')
  }
  if (state.value.connectionStatus === 'connected') {
    return t('workspace.websocket_client.disconnect')
  }
  return t('workspace.websocket_client.connect')
})
const hasResponseResult = computed(
  () =>
    state.value.connectionStatus !== 'idle' ||
    hasResponseHeaders.value ||
    state.value.messages.length > 0 ||
    state.value.responseError.trim().length > 0,
)
const responseErrorText = computed(
  () => state.value.responseError || t('workspace.websocket_client.connection_status_error'),
)
const visibleRightTab = computed<WebSocketClientState['activeRightTab']>({
  get() {
    if (hasErrorResponse.value) {
      return 'response-error'
    }

    return state.value.activeRightTab === 'response-error' ||
      (state.value.activeRightTab === 'response-cookies' && !hasResponseCookies.value)
      ? 'response-headers'
      : state.value.activeRightTab
  },
  set(value) {
    state.value.activeRightTab = value
  },
})
const mountedRightTabs = reactive(new Set<WebSocketClientState['activeRightTab']>())

watch(
  visibleRightTab,
  (tab) => mountedRightTabs.add(tab),
  { immediate: true },
)
const canSend = computed(() => {
  if (state.value.connectionStatus !== 'connected' || !state.value.sessionId) {
    return false
  }
  if (state.value.draftType === 'binary-file') {
    return !!state.value.draftFile
  }
  return true
})
const monacoLanguage = computed(() => {
  switch (state.value.draftType) {
    case 'json':
      return 'json'
    case 'xml':
      return 'xml'
    default:
      return 'plaintext'
  }
})
const messageEditorOptions = {
  padding: {
    bottom: 56,
  },
}
const statusTagType = computed(() => {
  switch (state.value.connectionStatus) {
    case 'connected':
      return 'success'
    case 'connecting':
      return 'warning'
    case 'error':
      return 'error'
    default:
      return 'default'
  }
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
const chipToneClass: Record<SummaryTagType, string> = {
  default: 'bg-app-control text-app-text-secondary',
  success: 'bg-[color-mix(in_srgb,var(--app-success-color)_14%,transparent)] text-app-success',
  info: 'bg-app-accent-selected text-app-accent',
  primary: 'bg-app-accent-selected text-app-accent',
  warning: 'bg-[color-mix(in_srgb,var(--app-warning-color)_16%,transparent)] text-app-warning',
  error: 'bg-[color-mix(in_srgb,var(--app-error-color)_14%,transparent)] text-app-error',
}

const responseMetaChips = computed<{ key: string; text: string; type: SummaryTagType }[]>(() => [
  {
    key: 'count',
    text: String(state.value.messages.length),
    type: 'default' as const,
  },
  {
    key: 'status',
    text: t(`workspace.websocket_client.connection_status_${state.value.connectionStatus}`),
    type: statusTagType.value,
  },
])
const responseTabsUi = { list: 'items-center' }

const leftTabItems = computed<TabsItem[]>(() => [
  { value: 'message', label: t('workspace.websocket_client.message') },
  {
    value: 'headers',
    label: `${t('workspace.websocket_client.request_headers')}(${enabledHeaderCount.value})`,
  },
  {
    value: 'query',
    label: `${t('workspace.http_request.query')}(${queryCount.value})`,
  },
  {
    value: 'cookies',
    label: `${t('workspace.http_request.cookies')}(${requestCookieCount.value})`,
  },
  { value: 'settings', label: t('workspace.websocket_client.settings') },
])

const rightTabItems = computed<TabsItem[]>(() => {
  if (hasErrorResponse.value) {
    return [{ value: 'response-error', label: t('workspace.websocket_client.response_error') }]
  }
  const items: TabsItem[] = [
    {
      value: 'response-headers',
      label: `${t('workspace.http_request.response_headers')}(${state.value.responseHeaders.length})`,
    },
  ]
  if (hasResponseCookies.value) {
    items.push({
      value: 'response-cookies',
      label: `${t('workspace.http_request.cookies')}(${responseCookieCount.value})`,
    })
  }
  items.push({ value: 'response', label: t('workspace.websocket_client.response') })
  return items
})

watch(
  () => state.value.url,
  (url) => {
    workspaceStore.updateRequestTabTitle(props.tabKey, url)
  },
  { immediate: true },
)

function validateWebSocketURL(rawURL: string): string | null {
  let parsedURL: URL
  try {
    parsedURL = new URL(rawURL)
  } catch {
    return t('workspace.websocket_client.invalid_url')
  }

  if (parsedURL.protocol !== 'ws:' && parsedURL.protocol !== 'wss:') {
    return t('workspace.websocket_client.invalid_url_scheme')
  }
  return null
}

function addRequestHeader() {
  state.value.headers.push({
    key: '',
    value: '',
    enabled: true,
  })
}

function createEmptyHeaderRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
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
    formatHeaderFieldsAsJson(editableRowsToHeaderFields(state.value.headers)),
    t('workspace.http_request.request_headers_copied_json'),
  )
}

async function copyRequestHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(editableRowsToHeaderFields(state.value.headers)),
    t('workspace.http_request.request_headers_copied_text'),
  )
}

async function copyResponseHeadersAsJson() {
  await copyText(
    formatHeaderFieldsAsJson(responseHeaderFields.value),
    t('workspace.http_request.response_headers_copied_json'),
  )
}

async function copyResponseHeadersAsText() {
  await copyText(
    formatHeaderFieldsAsText(responseHeaderFields.value),
    t('workspace.http_request.response_headers_copied_text'),
  )
}

async function copyResponseErrorSummary() {
  await copyText(
    responseErrorText.value,
    t('workspace.websocket_client.response_error_summary_copied'),
  )
}

function clearRequestHeaders() {
  state.value.headers.splice(0, state.value.headers.length, createEmptyHeaderRow())
  notify.success(t('workspace.http_request.request_headers_cleared'))
}

async function handleConnectToggle() {
  if (state.value.connectionStatus === 'connecting') {
    try {
      await workspaceStore.cancelWebSocketConnection(props.tabKey)
    } catch (error) {
      console.error('Failed to stop WebSocket session connection:', error)
    }
    return
  }

  if (state.value.connectionStatus === 'connected') {
    try {
      await workspaceStore.disconnectWebSocketClientTab(props.tabKey)
    } catch (error) {
      notify.error(String(error))
    }
    return
  }

  const errorText = validateWebSocketURL(state.value.url.trim())
  if (errorText) {
    notify.error(errorText)
    return
  }

  const invalidHeaderName = findInvalidRequestHeaderName(state.value.headers)
  if (invalidHeaderName) {
    state.value.activeLeftTab = 'headers'
    notify.error(
      t('workspace.http_request.header_invalid_name', {
        name: invalidHeaderName,
      }),
    )
    return
  }

  try {
    await workspaceStore.connectWebSocketClientTab(props.tabKey)
  } catch {}
}

async function handleSendMessage() {
  if (state.value.isSendingMessage) {
    return
  }
  if (!canSend.value) {
    if (state.value.draftType === 'binary-file' && !state.value.draftFile) {
      notify.error(t('workspace.websocket_client.file_required'))
      return
    }
    notify.error(t('workspace.websocket_client.not_connected'))
    return
  }

  try {
    await workspaceStore.sendWebSocketClientMessage(props.tabKey)
  } catch (error) {
    notify.error(t('workspace.websocket_client.send_failed', { error: String(error) }))
  }
}

const offRequestSendShortcut = registerShortcutHandler({
  commandId: 'requestEditor.send',
  when: () =>
    workbenchStore.activeContent === 'traffic' &&
    workspaceStore.activeTab.type === 'websocket-client' &&
    workspaceStore.activeTab.key === props.tabKey,
  enabled: () => canSend.value && !state.value.isSendingMessage,
  run: () => handleSendMessage(),
})

onBeforeUnmount(() => {
  offRequestSendShortcut()
})
</script>

<template>
  <div class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden bg-app-content">
    <div
      class="grid shrink-0 grid-cols-[minmax(0,1fr)_110px] items-center gap-2 bg-app-content px-2.5 py-2 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div
        class="flex h-9 min-w-0 items-stretch overflow-hidden rounded-sm border border-app-border bg-app-panel"
      >
        <RequestUrlInput
          v-model="state.url"
          class="min-w-0 flex-1 [--request-url-height:100%] [--request-url-border-width:0px] [--request-url-border-color:transparent] [--request-url-bg:var(--app-panel-bg)] [--request-url-padding-x:12px] [--request-url-font-size:15px]"
          :placeholder="t('workspace.websocket_client.url_placeholder')"
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

      <UTooltip :text="connectActionTooltip">
        <UButton
          class="h-9 w-full justify-center rounded-sm"
          :color="isConnecting || state.connectionStatus === 'connected' ? 'error' : 'primary'"
          :icon="
            isConnecting
              ? 'i-lucide-square'
              : state.connectionStatus === 'connected'
                ? 'i-lucide-circle-stop'
                : 'i-lucide-link'
          "
          :label="
            isConnecting
              ? t('workspace.websocket_client.stop')
              : state.connectionStatus === 'connected'
                ? t('workspace.websocket_client.disconnect')
                : t('workspace.websocket_client.connect')
          "
          @click="handleConnectToggle"
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
                  :model-value="state.activeLeftTab"
                  :items="leftTabItems"
                  :content="false"
                  variant="link"
                  class="w-full min-w-0 shrink-0 px-2.5"
                  @update:model-value="
                    state.activeLeftTab = $event as WebSocketClientState['activeLeftTab']
                  "
                />

                <div
                  v-if="mountedLeftTabs.has('message')"
                  v-show="state.activeLeftTab === 'message'"
                  class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
                  data-name="message"
                  role="tabpanel"
                >
                  <div class="flex h-full min-h-0 min-w-0 flex-col p-2.5">
                    <div class="mb-2 flex items-center justify-between gap-2">
                      <div class="flex items-center gap-2">
                        <span class="text-sm text-app-text-muted">{{
                          t('workspace.websocket_client.draft_type')
                        }}</span>
                        <USelect
                          v-model="state.draftType"
                          class="w-32.5"
                          :items="draftTypeOptions"
                        />
                      </div>
                    </div>

                    <div class="relative flex min-h-0 min-w-0 flex-1 items-stretch overflow-hidden">
                      <MonacoBodyEditor
                        v-if="state.draftType !== 'binary-file'"
                        v-model:value="state.draftText"
                        class="h-full min-h-0 min-w-0 flex-1"
                        :language="monacoLanguage"
                        :options="messageEditorOptions"
                      />

                      <RequestFileBody
                        v-else
                        class="h-full min-h-0 flex-1"
                        :tab-key="props.tabKey"
                        :file="state.draftFile"
                        @update:file="state.draftFile = $event"
                      />

                      <div
                        class="pointer-events-none absolute bottom-3 right-5 z-5 flex justify-end"
                      >
                        <UTooltip
                          :text="t('workspace.websocket_client.send_message')"
                          :kbds="sendShortcutKbds"
                          :content="{ side: 'top' }"
                        >
                          <UButton
                            class="pointer-events-auto rounded-full text-white"
                            :class="{ 'send-btn--loading': state.isSendingMessage }"
                            icon="i-lucide-send"
                            size="xl"
                            square
                            :disabled="!canSend || state.isSendingMessage"
                            :loading="state.isSendingMessage"
                            :aria-label="t('workspace.websocket_client.send_message')"
                            @click="handleSendMessage"
                          />
                        </UTooltip>
                      </div>
                    </div>
                  </div>
                </div>

                <div
                  v-if="mountedLeftTabs.has('headers')"
                  v-show="state.activeLeftTab === 'headers'"
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
                      :show-duplicate-warning="false"
                      validate-header-names
                    />
                  </div>
                </div>

                <RequestQueryPane
                  v-if="mountedLeftTabs.has('query')"
                  v-show="state.activeLeftTab === 'query'"
                  v-model:params="state.params"
                  data-name="query"
                  role="tabpanel"
                />

                <RequestCookiePane
                  v-if="mountedLeftTabs.has('cookies')"
                  v-show="state.activeLeftTab === 'cookies'"
                  v-model:headers="state.headers"
                  data-name="cookies"
                  role="tabpanel"
                />

                <div
                  v-if="mountedLeftTabs.has('settings')"
                  v-show="state.activeLeftTab === 'settings'"
                  class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                  data-name="settings"
                  role="tabpanel"
                >
                  <div
                    class="grid grid-cols-[minmax(9.25rem,max-content)_minmax(0,1fr)] items-center gap-2.5 p-2.5"
                  >
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

                    <div
                      v-if="state.settings.proxyMode === 'custom'"
                      class="contents"
                    >
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
            <div class="flex min-h-0 min-w-0 flex-1 flex-col">
              <AppLoading
                v-if="isConnecting"
                fill
                :label="t('workspace.websocket_client.connection_status_connecting')"
              />
              <UEmpty
                v-else-if="!hasResponseResult"
                icon="i-lucide-link-2-off"
                :title="t('workspace.http_request.request_not_started')"
                :size="appEmptyStateSize"
                variant="naked"
                :ui="appEmptyStateUi"
              />
              <UEmpty
                v-else-if="hasCancelledResponse"
                icon="i-lucide-circle-stop"
                :title="t('workspace.websocket_client.connection_cancelled_title')"
                :description="t('workspace.websocket_client.connection_cancelled_description')"
                :size="appEmptyStateSize"
                variant="naked"
                :ui="appEmptyStateUi"
              />
              <div v-else class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                  <div class="flex min-w-0 shrink-0 items-center">
                    <UTabs
                      :model-value="visibleRightTab"
                      :items="rightTabItems"
                      :content="false"
                      variant="link"
                      :ui="responseTabsUi"
                      class="w-full min-w-0 shrink-0 px-2.5"
                      @update:model-value="
                        visibleRightTab = $event as WebSocketClientState['activeRightTab']
                      "
                    >
                      <template #list-trailing>
                        <div
                          class="ml-auto flex min-w-max flex-[0_0_auto] items-center gap-1.5"
                        >
                          <span
                            v-for="chip in responseMetaChips"
                            :key="chip.key"
                            class="inline-flex min-h-5 shrink-0 items-center rounded-full px-2 text-xs font-bold leading-none tabular-nums"
                            :class="chipToneClass[chip.type]"
                          >
                            {{ chip.text }}
                          </span>
                        </div>
                      </template>
                    </UTabs>
                  </div>
                  <div
                    v-if="!hasErrorResponse && mountedRightTabs.has('response-headers')"
                    v-show="visibleRightTab === 'response-headers'"
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
                          <UTooltip :text="t('workspace.http_request.copy_response_headers_json')">
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
                          <UTooltip :text="t('workspace.http_request.copy_response_headers_text')">
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
                      <div
                        class="min-h-0 flex-1 overflow-y-auto px-2.5 pb-2.5"
                      >
                        <HeadersTable
                          v-if="hasResponseHeaders"
                          :fields="responseHeaderFields"
                        />
                        <div v-else class="flex min-h-full items-center justify-center">
                          <UEmpty
                            icon="i-lucide-rows-3"
                            :title="t('detail.no_headers')"
                            :size="appEmptyStateSize"
                            variant="naked"
                            :ui="appEmptyStateUi"
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                  <CookieTablePane
                    v-if="
                      !hasErrorResponse &&
                      hasResponseCookies &&
                      mountedRightTabs.has('response-cookies')
                    "
                    v-show="visibleRightTab === 'response-cookies'"
                    data-name="response-cookies"
                    role="tabpanel"
                    :title="t('detail.cookie_list')"
                    :cookies="responseCookieFields"
                    :empty-title="t('detail.no_cookies')"
                    :raw-headers="responseHeadersRecord"
                    header-name="set-cookie"
                  />
                  <div
                    v-if="!hasErrorResponse && mountedRightTabs.has('response')"
                    v-show="visibleRightTab === 'response'"
                    class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                    data-name="response"
                    role="tabpanel"
                  >
                    <div class="flex h-full min-h-0 min-w-0 flex-col">
                      <WebSocketMessageStream
                        :messages="state.messages"
                        :direction-filter="state.directionFilter"
                        :view-mode="state.viewMode"
                        show-clear-action
                        @update:direction-filter="state.directionFilter = $event"
                        @update:view-mode="state.viewMode = $event"
                        @clear="workspaceStore.clearWebSocketClientMessages(props.tabKey)"
                      />
                    </div>
                  </div>
                  <div
                    v-if="hasErrorResponse"
                    class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
                    data-name="response-error"
                    role="tabpanel"
                  >
                    <div class="flex flex-col overflow-auto p-2.5">
                      <div
                        class="flex flex-col gap-3.5 rounded-lg border border-app-border p-4"
                        :style="errorCardThemeVars"
                      >
                        <div class="flex items-start gap-3">
                          <div
                            class="flex size-10 shrink-0 items-center justify-center rounded-xl bg-(--error-card-icon-bg) text-(--error-card-icon-color)"
                          >
                            <UIcon name="i-lucide-circle-alert" class="flex size-5.5" />
                          </div>
                          <div class="flex min-w-0 flex-col gap-1">
                            <div class="text-[15px] font-bold text-app-text">
                              {{ t('workspace.websocket_client.response_error_title') }}
                            </div>
                            <div class="text-sm leading-normal text-app-text-secondary">
                              {{ t('workspace.websocket_client.response_error_description') }}
                            </div>
                          </div>
                        </div>

                        <div class="flex flex-col gap-2">
                          <div class="flex items-center justify-between gap-2">
                            <div class="text-sm font-semibold text-app-text-secondary">
                              {{ t('workspace.websocket_client.response_error_summary') }}
                            </div>
                            <UTooltip :text="t('workspace.websocket_client.copy_response_error_summary')">
                              <UButton
                                icon="i-lucide-copy"
                                color="neutral"
                                variant="ghost"
                                size="sm"
                                square
                                :aria-label="t('workspace.websocket_client.copy_response_error_summary')"
                                @click="copyResponseErrorSummary"
                              />
                            </UTooltip>
                          </div>
                          <div
                            class="border border-(--error-card-summary-border) bg-(--error-card-summary-bg) px-3 py-2.5 leading-normal text-app-text [word-break:break-word]"
                          >
                            {{ responseErrorText }}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </SplitterPanel>
      </SplitterGroup>
    </div>
  </div>
</template>
