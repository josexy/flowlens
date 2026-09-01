<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { ProcessStatus } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import AppProcessIcon from '@/components/common/AppProcessIcon.vue'
import { copyText } from '@/utils/clipboard'
import { useNotify } from '@/composables/useNotify'

const props = defineProps<{
  selectedEntry: TrafficEntry
}>()

const { t } = useI18n()
const notify = useNotify()
const showProcessInfo = ref(false)

const process = computed(() => props.selectedEntry.metadata?.process ?? null)
const isResolved = computed(
  () => process.value?.status === ProcessStatus.ProcessStatusResolved,
)
const displayName = computed(
  () =>
    process.value?.displayName ||
    process.value?.processName ||
    (process.value?.pid
      ? t('traffic.process_pid', { pid: process.value.pid })
      : t('detail.process_unknown')),
)

const reasonKeyByCode: Record<string, string> = {
  remote_client: 'detail.process_reason.remote_client',
  queue_full: 'detail.process_reason.queue_full',
  lookup_timeout: 'detail.process_reason.lookup_timeout',
  socket_owner_not_found: 'detail.process_reason.socket_owner_not_found',
  socket_scan_restricted: 'detail.process_reason.permission_denied',
  process_scan_restricted: 'detail.process_reason.permission_denied',
  multiple_socket_owners: 'detail.process_reason.multiple_socket_owners',
  metadata_denied: 'detail.process_reason.metadata_denied',
}

const statusMessage = computed(() => {
  const value = process.value
  if (!value) return ''

  const reasonKey = reasonKeyByCode[value.unavailableReason || '']
  if (reasonKey) {
    return t(reasonKey)
  }

  switch (value.status) {
    case ProcessStatus.ProcessStatusPending:
      return t('detail.process_status.pending')
    case ProcessStatus.ProcessStatusResolved:
      return value.unavailableReason ? t('detail.process_reason.unavailable') : ''
    case ProcessStatus.ProcessStatusRemote:
      return t('detail.process_status.remote')
    case ProcessStatus.ProcessStatusNotFound:
      return t('detail.process_status.not_found')
    case ProcessStatus.ProcessStatusPermissionDenied:
      return t('detail.process_status.permission_denied')
    case ProcessStatus.ProcessStatusUnsupported:
      return t('detail.process_status.unsupported')
    case ProcessStatus.ProcessStatusAmbiguous:
      return t('detail.process_status.ambiguous')
    default:
      return t('detail.process_status.unavailable')
  }
})

const statusIcon = computed(() => {
  switch (process.value?.status) {
    case ProcessStatus.ProcessStatusPending:
      return 'i-lucide-loader-circle'
    case ProcessStatus.ProcessStatusRemote:
      return 'i-lucide-monitor-up'
    case ProcessStatus.ProcessStatusPermissionDenied:
      return 'i-lucide-shield-alert'
    case ProcessStatus.ProcessStatusUnsupported:
      return 'i-lucide-circle-slash-2'
    case ProcessStatus.ProcessStatusAmbiguous:
      return 'i-lucide-git-fork'
    default:
      return 'i-lucide-search-x'
  }
})

function toggleProcessInfo() {
  showProcessInfo.value = !showProcessInfo.value
}

async function copyExecutablePath() {
  const path = process.value?.executablePath
  if (!path) return

  try {
    await copyText(path)
    notify.success(t('detail.process_path_copied'))
  } catch (error) {
    notify.error(t('detail.process_path_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div v-if="process" class="mt-1 flex flex-col" data-process-detail>
    <div
      class="flex items-center justify-between gap-3 rounded-(--radius-sm,6px) px-2 py-1.75 text-sm font-semibold text-app-text-secondary outline-none transition-[background-color,box-shadow,color] duration-200 ease-[ease] select-none hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:text-app-text focus-visible:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] focus-visible:text-app-text focus-visible:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_48%,transparent)]"
      :class="
        showProcessInfo &&
        'bg-[color-mix(in_srgb,var(--app-accent-color)_14%,transparent)] shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]'
      "
      role="button"
      tabindex="0"
      :aria-expanded="showProcessInfo"
      @click="toggleProcessInfo"
      @keydown.enter.prevent="toggleProcessInfo"
      @keydown.space.prevent="toggleProcessInfo"
    >
      <span class="min-w-0 truncate" :class="{ 'text-app-accent': showProcessInfo }">{{
        t('detail.process')
      }}</span>
      <UIcon
        :name="showProcessInfo ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
        class="size-3.75 shrink-0 text-[15px]"
        :class="{ 'text-app-accent': showProcessInfo }"
      />
    </div>

    <div v-show="showProcessInfo" class="flex flex-col gap-0.5">
      <div
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.process_application_name') }}
        </div>
        <div v-if="isResolved" class="flex min-w-0 flex-1 items-center gap-2.5">
          <AppProcessIcon :icon-key="process.iconKey" :alt="displayName" :size="24" />
          <div class="min-w-0 flex-1">
            <div class="select-text break-normal font-semibold text-app-text wrap-anywhere">
              {{ displayName }}
            </div>
            <div v-if="statusMessage" class="mt-0.5 text-sm leading-[1.4] text-warning">
              {{ statusMessage }}
            </div>
          </div>
        </div>
        <div v-else class="flex min-w-0 flex-1 items-center gap-2 text-muted">
          <UIcon
            :name="statusIcon"
            class="size-4 shrink-0"
            :class="{
              'animate-spin': process.status === ProcessStatus.ProcessStatusPending,
              'text-warning':
                process.status === ProcessStatus.ProcessStatusPermissionDenied ||
                process.status === ProcessStatus.ProcessStatusAmbiguous,
            }"
          />
          <span class="select-text break-normal wrap-anywhere">{{ statusMessage }}</span>
        </div>
      </div>

      <template v-if="isResolved">
        <div
          class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
        >
          <div
            class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
          >
            {{ t('detail.process_name') }}
          </div>
          <div class="min-w-0 flex-1 select-text font-sans text-sm break-all text-app-text">
            {{ process.processName || '—' }}
          </div>
        </div>
        <div
          class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
        >
          <div
            class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
          >
            {{ t('detail.process_pid') }}
          </div>
          <div class="min-w-0 flex-1 select-text font-mono text-sm break-all text-app-text">
            {{ process.pid || '—' }}
          </div>
        </div>
        <div
          v-if="process.appId"
          class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
        >
          <div
            class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
          >
            {{ t('detail.process_app_id') }}
          </div>
          <div class="min-w-0 flex-1 select-text font-mono text-sm break-all text-app-text">
            {{ process.appId }}
          </div>
        </div>
        <div
          class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
        >
          <div
            class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
          >
            {{ t('detail.process_executable_path') }}
          </div>
          <div class="flex min-w-0 flex-1 items-start gap-1.5">
            <span class="min-w-0 flex-1 select-text font-mono text-sm break-all text-app-text">
              {{ process.executablePath || '—' }}
            </span>
            <UTooltip
              v-if="process.executablePath"
              :text="t('detail.copy_process_path')"
              :content="{ side: 'top' }"
            >
              <UButton
                color="neutral"
                variant="ghost"
                size="xs"
                icon="i-lucide-copy"
                class="-my-1 shrink-0"
                :aria-label="t('detail.copy_process_path')"
                @click.stop="copyExecutablePath"
              />
            </UTooltip>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>
