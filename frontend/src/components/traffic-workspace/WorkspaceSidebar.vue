<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useHistoryStore } from '@/stores/history'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import { useProxyStore } from '@/stores/proxy'
import { useTrafficStore } from '@/stores/traffic'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import HistoryContextMenu from '@/components/menu/HistoryContextMenu.vue'
import CaptureContextMenu from '@/components/menu/CaptureContextMenu.vue'
import AppLoading from '@/components/common/AppLoading.vue'
import AppTooltip from '@/components/common/AppTooltip.vue'
import { useNotify } from '@/composables/useNotify'
import { RestartCapture } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { getErrorMessage } from '@/utils/dialog'

const { t } = useI18n()
const notify = useNotify()
const historyStore = useHistoryStore()
const proxyStore = useProxyStore()
const trafficStore = useTrafficStore()
const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const historyContextMenuRef = ref<InstanceType<typeof HistoryContextMenu> | null>(null)
const restartModalVisible = ref(false)
const restartPendingAction = ref<'save' | 'discard' | null>(null)
const clearAllHistoryModalVisible = ref(false)
const clearAllHistoryPending = ref(false)

function onHistoryContextMenu(event: MouseEvent) {
  // Only history rows open the menu; suppress right-clicks on empty list area.
  if (!(event.target as HTMLElement | null)?.closest('.history-row')) {
    event.stopPropagation()
  }
}

function formatDate(unixMs: number): string {
  if (!unixMs) return '-'
  const date = new Date(unixMs)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function formatTime(unixMs: number): string {
  if (!unixMs) return '-'
  const date = new Date(unixMs)
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')
  return `${hours}:${minutes}:${seconds}`
}

function getHistoryLabel(alias: string, createdAt: number): string {
  return alias || formatDate(createdAt)
}

const activeCapture = computed(() => workbenchStore.sectionSelections.capture === 'capture')
const captureCount = computed(() => trafficStore.statistics.total)
const captureAlias = computed(() => proxyStore.status?.captureAlias?.trim() ?? '')
const captureLabel = computed(() => captureAlias.value || t('menu.capture'))

function selectCapture() {
  workbenchStore.selectCaptureItem('capture')
  workspaceStore.activateCapture()
}

function selectHistory(key: string) {
  const metadata = historyStore.metadataList.find((item) => item.key === key)
  if (!metadata) {
    return
  }
  workbenchStore.selectCaptureItem(`history:${key}`)
  workspaceStore.openHistoryTab(metadata)
}

async function executeRestartCapture(saveCurrent: boolean) {
  await RestartCapture(saveCurrent)
  workspaceStore.activateCapture()
  workbenchStore.selectCaptureItem('capture')
  await historyStore.loadList()
}

function handleRestartCapture() {
  if (captureCount.value === 0) {
    void executeRestartCapture(false)
      .then(() => {
        notify.success(t('capture.restart_success'))
      })
      .catch((error) => {
        notify.error(t('capture.restart_failed', { error }))
      })
    return
  }

  restartModalVisible.value = true
}

async function restartCaptureAndClose(saveCurrent: boolean) {
  if (restartPendingAction.value !== null) {
    return
  }

  restartPendingAction.value = saveCurrent ? 'save' : 'discard'
  try {
    await executeRestartCapture(saveCurrent)
    restartModalVisible.value = false
    notify.success(
      saveCurrent ? t('capture.restart_saved_success') : t('capture.restart_discarded_success'),
    )
  } catch (error) {
    notify.error(t('capture.restart_failed', { error }))
  } finally {
    restartPendingAction.value = null
  }
}

function handleClearAllHistory() {
  if (clearAllHistoryPending.value) {
    return
  }
  clearAllHistoryModalVisible.value = true
}

async function confirmClearAllHistory() {
  if (clearAllHistoryPending.value) {
    return
  }

  clearAllHistoryPending.value = true
  try {
    await historyStore.clearAll()
    clearAllHistoryModalVisible.value = false
  } catch (error) {
    notify.error(getErrorMessage(error))
  } finally {
    clearAllHistoryPending.value = false
  }
}

function updateClearAllHistoryModalVisible(value: boolean) {
  if (!clearAllHistoryPending.value) {
    clearAllHistoryModalVisible.value = value
  }
}
</script>

<template>
  <div class="flex h-full w-full min-w-0 shrink-0 flex-col overflow-hidden bg-app-sidebar">
    <CaptureContextMenu :current-alias="captureAlias">
      <div
        class="group/item mx-2 mt-1 flex h-10 min-h-10 items-center gap-2 rounded-[10px] border px-2.5 py-2 transition-colors duration-150"
        :class="
          activeCapture
            ? 'border-app-border-strong bg-app-accent-selected'
            : 'border-transparent bg-[color-mix(in_srgb,var(--app-sidebar-header-bg)_88%,var(--app-sidebar-bg))] hover:bg-app-control'
        "
        @click="selectCapture"
      >
        <div
          class="flex shrink-0 items-center"
          :class="activeCapture ? 'text-app-accent opacity-100' : 'text-app-text-muted opacity-70'"
        >
          <UIcon name="i-lucide-radio" class="size-4 shrink-0" />
        </div>
        <div class="flex min-w-0 flex-1 flex-col gap-0.5">
          <span class="truncate text-sm font-semibold text-app-text">{{ captureLabel }}</span>
        </div>
        <div class="flex shrink-0 items-center">
          <AppTooltip :text="t('capture.restart')" placement="bottom" :delay="500">
            <template #trigger>
              <UButton
                size="sm"
                color="neutral"
                variant="ghost"
                icon="i-lucide-skip-forward"
                class="mr-1"
                :aria-label="t('capture.restart')"
                @click.stop="handleRestartCapture"
              />
            </template>
          </AppTooltip>
          <span
            class="inline-block h-5 min-w-5.5 rounded-full bg-[color-mix(in_srgb,var(--app-control-bg)_88%,transparent)] px-1.75 text-sm leading-5 text-app-text-muted"
            >{{ captureCount }}</span
          >
        </div>
      </div>
    </CaptureContextMenu>

    <div
      class="mt-1.5 flex min-h-9.5 items-center justify-between bg-app-sidebar-header pl-2.5 pr-2 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div class="flex items-center gap-1.5 text-sm font-semibold text-app-text-muted">
        <span>{{ t('history.title') }}</span>
        <span
          class="rounded-lg bg-[color-mix(in_srgb,var(--app-control-bg)_82%,var(--app-sidebar-bg))] px-1.5 py-px text-sm leading-[1.6]"
          >{{ workspaceStore.historyCount }}</span
        >
      </div>
      <div class="flex items-center gap-0.5">
        <AppTooltip :text="t('history.refresh')" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-refresh-cw"
              :disabled="historyStore.loading"
              :aria-label="t('history.refresh')"
              @click="historyStore.loadList()"
            />
          </template>
        </AppTooltip>
        <AppTooltip :text="t('history.clearAll')" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-trash-2"
              :disabled="historyStore.metadataList.length === 0 || clearAllHistoryPending"
              :aria-label="t('history.clearAll')"
              @click="handleClearAllHistory"
            />
          </template>
        </AppTooltip>
      </div>
    </div>

    <HistoryContextMenu
      ref="historyContextMenuRef"
      class="flex min-h-0 flex-1 flex-col overflow-hidden"
    >
      <div class="flex min-h-0 flex-1 flex-col overflow-y-auto" @contextmenu="onHistoryContextMenu">
        <AppLoading
          v-if="historyStore.loading"
          fill
          size="md"
          class="p-4"
        />
        <div
          v-else-if="historyStore.metadataList.length === 0"
          class="flex flex-1 items-center justify-center p-4 text-sm text-app-text-muted"
          role="status"
        >
          {{ t('history.empty') }}
        </div>
        <div v-else class="min-h-0 flex-1 overflow-y-auto">
          <div
            v-for="item in historyStore.metadataList"
            :key="item.key"
            class="history-row mx-2 mt-1 flex min-h-8.5 items-center gap-2 rounded-[10px] border px-2.5 py-1 transition-colors duration-150"
            :class="
              workbenchStore.sectionSelections.capture === `history:${item.key}`
                ? 'border-app-border-strong bg-app-accent-selected'
                : 'border-transparent hover:bg-app-control'
            "
            @click="selectHistory(item.key)"
            @contextmenu="historyContextMenuRef?.setKey(item.key)"
          >
            <div
              class="flex shrink-0 items-center"
              :class="
                workbenchStore.sectionSelections.capture === `history:${item.key}`
                  ? 'text-app-accent opacity-100'
                  : 'text-app-text-muted opacity-70'
              "
            >
              <UIcon name="i-lucide-archive" class="size-3.75 shrink-0" />
            </div>
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="truncate text-sm font-semibold text-app-text">{{
                getHistoryLabel(item.alias, item.createdAt)
              }}</span>
              <span class="text-xs leading-4 text-app-text-muted">{{ formatTime(item.createdAt) }}</span>
            </div>
            <div class="flex shrink-0 items-center">
              <span
                class="inline-block h-5 min-w-5.5 rounded-full px-1.75 text-sm leading-5"
                :class="
                  workbenchStore.sectionSelections.capture === `history:${item.key}`
                    ? 'bg-(--app-accent-strong-bg) text-app-accent'
                    : 'bg-[color-mix(in_srgb,var(--app-control-bg)_88%,transparent)] text-app-text-muted'
                "
                >{{ item.total }}</span
              >
            </div>
          </div>
        </div>
      </div>
    </HistoryContextMenu>

    <ConfirmCardModal
      :show="clearAllHistoryModalVisible"
      :title="t('history.clearAll')"
      :positive-text="t('history.clearAll')"
      :negative-text="t('history.cancel')"
      positive-type="error"
      :positive-disabled="clearAllHistoryPending"
      :positive-loading="clearAllHistoryPending"
      :negative-disabled="clearAllHistoryPending"
      :closable="!clearAllHistoryPending"
      :mask-closable="!clearAllHistoryPending"
      @update:show="updateClearAllHistoryModalVisible"
      @positive-click="confirmClearAllHistory"
    >
      {{ t('history.clearAllConfirm') }}
    </ConfirmCardModal>
    <ConfirmCardModal
      :show="restartModalVisible"
      :title="t('capture.restart_title')"
      :positive-text="t('capture.restart_save')"
      :negative-text="''"
      :closable="restartPendingAction === null"
      :mask-closable="false"
      @update:show="restartModalVisible = $event"
    >
      {{ t('capture.restart_confirm') }}

      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton
            color="neutral"
            variant="outline"
            :disabled="restartPendingAction !== null"
            :label="t('history.cancel')"
            @click="restartModalVisible = false"
          />
          <UButton
            color="neutral"
            variant="outline"
            :disabled="restartPendingAction !== null"
            :loading="restartPendingAction === 'discard'"
            :label="t('capture.restart_discard')"
            @click="restartCaptureAndClose(false)"
          />
          <UButton
            :disabled="restartPendingAction !== null"
            :loading="restartPendingAction === 'save'"
            :label="t('capture.restart_save')"
            @click="restartCaptureAndClose(true)"
          />
        </div>
      </template>
    </ConfirmCardModal>
  </div>
</template>
