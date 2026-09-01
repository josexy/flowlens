<script setup lang="ts">
import { ref, shallowRef, computed, inject, useAttrs, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContextMenuItem } from '@nuxt/ui'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import type { useHistoryTrafficStore } from '@/stores/historyTraffic'
import type { useTrafficStore } from '@/stores/traffic'
import { TRAFFIC_STORE_KEY } from '@/types/inject-keys'
import {
  getTrafficCapabilities,
  getTrafficMethodLabel,
  getTrafficPathLabel,
  getTrafficProtocol,
  getTrafficTarget,
  getTrafficTypeLabel,
  isHARExportableHistoryFormat,
  isRawTCPTraffic,
  splitHostportToIP,
} from '@/utils/traffic'
import { copyText } from '@/utils/clipboard'
import { ResendRequest as ResendProxyRequest } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { ResendRequest as ResendHistoryRequest } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import ResendNTimesModal from '../modal/ResendNTimesModal.vue'
import ConfirmCardModal from '../modal/ConfirmCardModal.vue'
import { useNotify } from '@/composables/useNotify'
import { useHARExport } from '@/composables/useHARExport'
import { useHistoryStore } from '@/stores/history'

// A superset of Nuxt UI's ContextMenuItem: keeps `key` (for command dispatch and
// tests) and `color` (rendered as a swatch via the #item-leading slot).
type AppMenuItem = ContextMenuItem & {
  key: string
  color?: string
}

const { t } = useI18n()
const notify = useNotify()
const { exporting: exportingHAR, exportHAR } = useHARExport()
const historyStore = useHistoryStore()
type TrafficStoreLike =
  | ReturnType<typeof useTrafficStore>
  | ReturnType<typeof useHistoryTrafficStore>
const props = defineProps<{
  trafficStore?: TrafficStoreLike
}>()
const injectedTrafficStore = inject(TRAFFIC_STORE_KEY, null)
const trafficStore = computed(
  () => (props.trafficStore ?? injectedTrafficStore) as TrafficStoreLike,
)
const workspaceStore = useTrafficWorkspaceStore()
const attrs = useAttrs()

const contextMenuEntries = ref<proxyservice.TrafficEntry[]>([])
const showResendModal = ref(false)
const resendEntryId = ref(0)
const resendHistoryKey = ref<string | null>(null)
const deleteModalVisible = ref(false)
const deleting = ref(false)
const deleteTargetIds = ref<number[]>([])
const deleteHistoryKey = ref<string | null>(null)
const deleteTrafficStore = shallowRef<TrafficStoreLike | null>(null)
let deleteConfirmationVersion = 0

function getHistoryKey(store: TrafficStoreLike): string | null {
  if (!('currentKey' in store)) {
    return null
  }
  const key = store.currentKey
  return typeof key === 'string' && key.length > 0 ? key : null
}

const currentHistoryKey = computed<string | null>(() => getHistoryKey(trafficStore.value))
const currentHistoryMetadata = computed(() =>
  historyStore.metadataList.find((item) => item.key === currentHistoryKey.value),
)
const canExportHAR = computed(
  () =>
    !currentHistoryKey.value ||
    isHARExportableHistoryFormat(currentHistoryMetadata.value?.formatVersion),
)
const singleEntry = computed(() => contextMenuEntries.value[0] ?? null)
const hasMultipleEntries = computed(() => contextMenuEntries.value.length > 1)
const hasHttpEntries = computed(() =>
  contextMenuEntries.value.some((entry) => getTrafficCapabilities(entry).canCopyCurl),
)
const hasOnlyRawTCPEntries = computed(
  () =>
    contextMenuEntries.value.length > 0 && contextMenuEntries.value.every(isRawTCPTraffic),
)
const canEditEntries = computed(
  () =>
    contextMenuEntries.value.length > 0 &&
    contextMenuEntries.value.every((entry) => getTrafficCapabilities(entry).canEditRequest),
)
const canResendSingleEntry = computed(
  () =>
    !hasMultipleEntries.value &&
    !!singleEntry.value &&
    getTrafficCapabilities(singleEntry.value).canResend,
)

/** Set the ordered, de-duplicated entry snapshot the menu should act on. */
function setEntries(entries: proxyservice.TrafficEntry[]) {
  const seenIds = new Set<number>()
  contextMenuEntries.value = entries.filter((entry) => {
    if (seenIds.has(entry.id)) {
      return false
    }
    seenIds.add(entry.id)
    return true
  })
}

function getCopyValue(entry: proxyservice.TrafficEntry, key: string): string {
  switch (key) {
    case 'copy-target':
      return getTrafficTarget(entry)
    case 'copy-url':
      return entry.url
    case 'copy-domain':
      return entry.host
    case 'copy-path':
      return entry.path
    case 'copy-server-ip':
      return splitHostportToIP((entry.metadata?.remoteDestinationAddr || '') as string)
    case 'copy-client-ip':
      return splitHostportToIP((entry.metadata?.localSourceAddr || '') as string)
    case 'copy-server-addr':
      return (entry.metadata?.remoteDestinationAddr || '') as string
    case 'copy-client-addr':
      return (entry.metadata?.localSourceAddr || '') as string
    case 'copy-row':
      return [
        entry.id,
        getTrafficMethodLabel(entry),
        getTrafficTarget(entry),
        getTrafficPathLabel(entry),
        entry.statusCode || '',
        getTrafficTypeLabel(entry),
        entry.metadata?.remoteDestinationAddr || '',
        getTrafficProtocol(entry),
      ].join('\t')
    default:
      return ''
  }
}

async function copyToClipboard(text: string) {
  await copyText(text)
  notify.success(t('context_menu.traffic_table.copied'), undefined, 1500)
}

async function resendRequest(
  entryId: number,
  cfg: proxyservice.ResendConfig,
  historyKey: string | null,
) {
  return historyKey
    ? await ResendHistoryRequest(historyKey, entryId, cfg)
    : await ResendProxyRequest(entryId, cfg)
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`
}

async function buildCurl(
  entry: proxyservice.TrafficEntry,
  store: TrafficStoreLike,
  historyKey: string | null,
): Promise<string> {
  let cmd = 'curl'
  if (entry.method !== 'GET') cmd += ` -X ${entry.method}`
  cmd += ` ${shellQuote(entry.url)}`
  for (const field of entry.request?.headerFields ?? []) {
    if (field === null || field.name.trim().startsWith(':')) {
      continue
    }
    cmd += ` \\\n  -H ${shellQuote(`${field.name}: ${field.value}`)}`
  }

  const bodyView = await store.getBodyView?.(entry.id, historyKey)
  const reqBody = bodyView?.reqBody ?? ''
  if (reqBody && bodyView?.reqBodyEnc !== 'base64') {
    cmd += ` \\\n  --data-raw ${shellQuote(reqBody)}`
  }

  return cmd
}

async function copyCurl() {
  const entries = [...contextMenuEntries.value]
  const store = trafficStore.value
  const historyKey = getHistoryKey(store)
  const commands: string[] = []
  let skipped = 0
  for (const entry of entries) {
    if (!getTrafficCapabilities(entry).canCopyCurl) {
      skipped++
      continue
    }
    commands.push(await buildCurl(entry, store, historyKey))
  }

  if (commands.length === 0) {
    return
  }

  await copyText(commands.join('\n\n'))
  if (skipped > 0) {
    notify.success(
      t('context_menu.traffic_table.curl_copied_skipped', {
        copied: commands.length,
        skipped,
      }),
      undefined,
      1500,
    )
    return
  }
  notify.success(t('context_menu.traffic_table.copied'), undefined, 1500)
}

async function editEntries() {
  const entries = [...contextMenuEntries.value]
  if (entries.some((entry) => !getTrafficCapabilities(entry).canEditRequest)) {
    return
  }
  const store = trafficStore.value
  const historyKey = getHistoryKey(store)
  const maxEntries = 50
  if (entries.length > maxEntries) {
    notify.warning(t('context_menu.traffic_table.edit_limit', { limit: maxEntries }))
    return
  }

  let firstTabKey: string | null = null
  for (const entry of entries) {
    try {
      const bodyView = (await store.getBodyView?.(entry.id, historyKey)) ?? null
      const tabKey = await workspaceStore.openRequestEditorFromTraffic({
        entry,
        bodyView,
        source: historyKey ? 'history-edit' : 'capture-edit',
        sourceHistoryKey: historyKey ?? undefined,
      })
      if (!firstTabKey) {
        firstTabKey = tabKey
      }
    } catch (error) {
      console.error('Failed to open traffic entry in Request Editor:', error)
    }
  }

  if (firstTabKey) {
    workspaceStore.activateTab(firstTabKey)
  }
}

function requestDelete() {
  if (deleting.value) {
    return
  }

  const entries = [...contextMenuEntries.value]
  const store = trafficStore.value
  if (entries.length === 1) {
    void store.deleteEntry(entries[0]!.id)
    return
  }
  deleteTargetIds.value = entries.map((entry) => entry.id)
  deleteHistoryKey.value = getHistoryKey(store)
  deleteTrafficStore.value = store
  deleteConfirmationVersion++
  deleteModalVisible.value = true
}

function closeDeleteConfirmation(expectedVersion?: number) {
  if (expectedVersion !== undefined && expectedVersion !== deleteConfirmationVersion) {
    return
  }

  deleteConfirmationVersion++
  deleteModalVisible.value = false
  deleteTargetIds.value = []
  deleteHistoryKey.value = null
  deleteTrafficStore.value = null
}

function closeResendModal() {
  showResendModal.value = false
  resendEntryId.value = 0
  resendHistoryKey.value = null
}

function handleResendModalVisibleUpdate(value: boolean) {
  if (value) {
    showResendModal.value = true
  } else {
    closeResendModal()
  }
}

function handleDeleteModalVisibleUpdate(value: boolean) {
  if (!deleting.value) {
    deleteModalVisible.value = value
    if (!value) {
      closeDeleteConfirmation()
    }
  }
}

watch([trafficStore, currentHistoryKey], ([store, historyKey]) => {
  if (
    deleteModalVisible.value &&
    (store !== deleteTrafficStore.value || historyKey !== deleteHistoryKey.value)
  ) {
    closeDeleteConfirmation()
  }
})

async function confirmDelete() {
  if (deleting.value || deleteTargetIds.value.length === 0) {
    return
  }
  const store = deleteTrafficStore.value
  if (
    !store ||
    store !== trafficStore.value ||
    deleteHistoryKey.value !== getHistoryKey(store)
  ) {
    closeDeleteConfirmation()
    return
  }

  const ids = [...deleteTargetIds.value]
  const confirmationVersion = deleteConfirmationVersion
  deleting.value = true
  try {
    await store.deleteEntries(ids)
    closeDeleteConfirmation(confirmationVersion)
    notify.success(t('context_menu.traffic_table.delete_multi_success', { count: ids.length }))
  } catch (error) {
    notify.error(t('context_menu.traffic_table.delete_multi_failed', { error: String(error) }))
  } finally {
    deleting.value = false
  }
}

async function handleSelect(key: string) {
  const entries = contextMenuEntries.value
  const entry = singleEntry.value
  if (entries.length === 0 || !entry) return
  const store = trafficStore.value
  const historyKey = getHistoryKey(store)

  switch (key) {
    case 'show-details':
      await store.selectEntry(entry)
      store.showDetailPanel = true
      break
    case 'copy-curl':
      if (!hasHttpEntries.value) return
      await copyCurl()
      break
    case 'copy-target':
    case 'copy-url':
    case 'copy-domain':
    case 'copy-path':
    case 'copy-server-ip':
    case 'copy-client-ip':
    case 'copy-server-addr':
    case 'copy-client-addr':
    case 'copy-row':
      await copyToClipboard(entries.map((current) => getCopyValue(current, key)).join('\n'))
      break
    case 'resend-1':
    case 'resend-2':
    case 'resend-3':
    case 'resend-4':
    case 'resend-5': {
      if (!getTrafficCapabilities(entry).canResend) return
      const n = parseInt(key.split('-')[1] ?? '1', 10)
      try {
        const result = await resendRequest(
          entry.id,
          {
            delayMs: 0,
            intervalMs: 1000,
            count: n,
            useProxy: true,
            upstreamProxy: '',
          },
          historyKey,
        )
        notify.success(
          t('resend_modal.success', { success: result.success, failed: result.failed }),
        )
      } catch (err: unknown) {
        notify.error(t('resend_modal.error', { error: String(err) }))
      }
      break
    }
    case 'resend-times':
      if (!getTrafficCapabilities(entry).canResend) return
      resendEntryId.value = entry.id
      resendHistoryKey.value = historyKey
      showResendModal.value = true
      break
    case 'edit':
      if (!canEditEntries.value) return
      await editEntries()
      break
    case 'export-har':
      if (!canExportHAR.value || hasOnlyRawTCPEntries.value) return
      await exportHAR({
        historyKey,
        trafficIds: entries.map((current) => current.id),
        filenameHint: currentHistoryMetadata.value?.alias
          ? `${currentHistoryMetadata.value.alias}-selection`
          : `flowlens-selection-${entries.length}`,
      })
      break
    case 'highlight-red':
    case 'highlight-orange':
    case 'highlight-yellow':
    case 'highlight-green':
    case 'highlight-blue':
    case 'highlight-purple': {
      const colorMap: Record<string, string> = {
        'highlight-red': '#ef4444',
        'highlight-orange': '#f97316',
        'highlight-yellow': '#eab308',
        'highlight-green': '#22c55e',
        'highlight-blue': '#3b82f6',
        'highlight-purple': '#a855f7',
      }
      for (const current of entries) {
        store.setHighlight(current.id, colorMap[key] ?? null)
      }
      break
    }
    case 'highlight-clear':
      for (const current of entries) {
        store.setHighlight(current.id, null)
      }
      break
    case 'delete':
      requestDelete()
      break
  }
}

function createSelect(key: string) {
  return () => {
    void handleSelect(key)
  }
}

function item(key: string, label: string, extra: Partial<AppMenuItem> = {}): AppMenuItem {
  const disabled = Boolean(extra.disabled)
  return {
    key,
    label,
    ...extra,
    onSelect: extra.children || disabled ? undefined : createSelect(key),
  }
}

function separator(key: string): AppMenuItem {
  return {
    key,
    type: 'separator',
  }
}

const menuItems = computed<AppMenuItem[]>(() => {
  if (contextMenuEntries.value.length === 0) return []
  const items: AppMenuItem[] = [
    item('show-details', t('context_menu.traffic_table.show_details'), {
      disabled: hasMultipleEntries.value,
    }),
    item('export-har', t('har_export.export_selected'), {
      disabled:
        exportingHAR.value || hasOnlyRawTCPEntries.value || !canExportHAR.value,
    }),
  ]

  if (hasHttpEntries.value) {
    items.push(
      separator('d1'),
      item('copy-curl', t('context_menu.traffic_table.copy_curl')),
    )
  }

  const copyChildren = hasOnlyRawTCPEntries.value
    ? [
        item('copy-target', t('context_menu.traffic_table.copy_target')),
        item('copy-server-ip', t('context_menu.traffic_table.copy_serverip')),
        item('copy-client-ip', t('context_menu.traffic_table.copy_clientip')),
        item('copy-server-addr', t('context_menu.traffic_table.copy_serveraddr')),
        item('copy-client-addr', t('context_menu.traffic_table.copy_clientaddr')),
        separator('cd2'),
        item('copy-row', t('context_menu.traffic_table.copy_row')),
      ]
    : [
        item('copy-url', t('context_menu.traffic_table.copy_url')),
        item('copy-domain', t('context_menu.traffic_table.copy_domain')),
        item('copy-path', t('context_menu.traffic_table.copy_path')),
        separator('cd1'),
        item('copy-server-ip', t('context_menu.traffic_table.copy_serverip')),
        item('copy-client-ip', t('context_menu.traffic_table.copy_clientip')),
        item('copy-server-addr', t('context_menu.traffic_table.copy_serveraddr')),
        item('copy-client-addr', t('context_menu.traffic_table.copy_clientaddr')),
        separator('cd2'),
        item('copy-row', t('context_menu.traffic_table.copy_row')),
      ]

  items.push(separator('d2'), item('copy', t('context_menu.traffic_table.copy'), {
    children: copyChildren,
  }))

  if (canEditEntries.value) {
    items.push(separator('d3'), item('edit', t('context_menu.traffic_table.edit')))
  }
  if (canResendSingleEntry.value) {
    items.push(
      item('resend', t('context_menu.traffic_table.resend'), {
        children: [
          item('resend-1', t('context_menu.traffic_table.resend_n', { n: 1 })),
          item('resend-2', t('context_menu.traffic_table.resend_n', { n: 2 })),
          item('resend-3', t('context_menu.traffic_table.resend_n', { n: 3 })),
          item('resend-4', t('context_menu.traffic_table.resend_n', { n: 4 })),
          item('resend-5', t('context_menu.traffic_table.resend_n', { n: 5 })),
          separator('rd1'),
          item('resend-times', t('context_menu.traffic_table.resend_times')),
        ],
      }),
    )
  }

  items.push(
    separator('d4'),
    item('highlight', t('context_menu.traffic_table.highlight'), {
      children: [
        item('highlight-red', t('context_menu.traffic_table.highlight_red'), { color: '#ef4444' }),
        item('highlight-orange', t('context_menu.traffic_table.highlight_orange'), {
          color: '#f97316',
        }),
        item('highlight-yellow', t('context_menu.traffic_table.highlight_yellow'), {
          color: '#eab308',
        }),
        item('highlight-green', t('context_menu.traffic_table.highlight_green'), {
          color: '#22c55e',
        }),
        item('highlight-blue', t('context_menu.traffic_table.highlight_blue'), { color: '#3b82f6' }),
        item('highlight-purple', t('context_menu.traffic_table.highlight_purple'), {
          color: '#a855f7',
        }),
        separator('hd1'),
        item('highlight-clear', t('context_menu.traffic_table.highlight_clear')),
      ],
    }),
    separator('d5'),
    item('delete', t('context_menu.traffic_table.delete')),
  )
  return items
})

defineExpose({ setEntries, menuItems })
</script>

<template>
  <div v-bind="attrs">
    <UContextMenu :items="menuItems">
      <slot />
      <template #item-leading="{ item }">
        <span
          v-if="(item as AppMenuItem).color"
          class="size-3 shrink-0 rounded-full border border-[rgba(0,0,0,0.2)]"
          :style="{ backgroundColor: (item as AppMenuItem).color }"
          aria-hidden="true"
        />
      </template>
    </UContextMenu>
    <ResendNTimesModal
      :show="showResendModal"
      :entry-id="resendEntryId"
      :history-key="resendHistoryKey"
      @update:show="handleResendModalVisibleUpdate"
    />
    <ConfirmCardModal
      :show="deleteModalVisible"
      :title="t('context_menu.traffic_table.delete')"
      :positive-text="t('context_menu.traffic_table.confirm')"
      :negative-text="t('context_menu.traffic_table.cancel')"
      positive-type="error"
      :positive-loading="deleting"
      :positive-disabled="deleting"
      :negative-disabled="deleting"
      :closable="!deleting"
      :mask-closable="!deleting"
      @update:show="handleDeleteModalVisibleUpdate"
      @positive-click="confirmDelete"
    >
      {{ t('context_menu.traffic_table.delete_multi_confirm', { count: deleteTargetIds.length }) }}
    </ConfirmCardModal>
  </div>
</template>
