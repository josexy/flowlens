<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { LogLevel } from '#bindings/github.com/josexy/flowlens/backend/pkg/logger/models'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import { useNotify } from '@/composables/useNotify'
import { useLoggingStore } from '@/stores/logging'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const notify = useNotify()
const loggingStore = useLoggingStore()
const clearLogsModalVisible = ref(false)
const enabledModel = ref(false)
const levelModel = ref<LogLevel>(LogLevel.LogLevelInfo)

const levelOptions = computed(() => [
  { label: t('settings.log_level_trace'), value: LogLevel.LogLevelTrace },
  { label: t('settings.log_level_debug'), value: LogLevel.LogLevelDebug },
  { label: t('settings.log_level_info'), value: LogLevel.LogLevelInfo },
  { label: t('settings.log_level_warn'), value: LogLevel.LogLevelWarn },
  { label: t('settings.log_level_error'), value: LogLevel.LogLevelError },
])

function formatError(error: unknown) {
  if (error instanceof Error && error.message) {
    return error.message
  }
  return String(error)
}

watch(
  () => loggingStore.status?.enabled,
  (enabled) => {
    if (enabled !== undefined) {
      enabledModel.value = enabled
    }
  },
  { immediate: true },
)

watch(
  () => loggingStore.status?.level,
  (level) => {
    if (level) {
      levelModel.value = level
    }
  },
  { immediate: true },
)

async function runAction(action: () => Promise<unknown>, errorKey: string, successKey?: string) {
  try {
    await action()
    if (successKey) {
      notify.success(t(successKey))
    }
    return true
  } catch (error) {
    notify.error(
      t(errorKey, {
        error: formatError(error),
      }),
    )
    return false
  }
}

async function handleEnabledUpdate(enabled: boolean) {
  const previousValue = loggingStore.status?.enabled ?? enabledModel.value
  enabledModel.value = enabled
  const success = await runAction(() => loggingStore.setEnabled(enabled), 'settings.log_error_toggle')
  if (!success) {
    enabledModel.value = loggingStore.status?.enabled ?? previousValue
  }
}

async function handleLevelUpdate(value: LogLevel) {
  const previousValue = loggingStore.status?.level ?? levelModel.value
  levelModel.value = value
  const success = await runAction(() => loggingStore.setLevel(value), 'settings.log_error_level')
  if (!success) {
    levelModel.value = loggingStore.status?.level ?? previousValue
  }
}

async function handleOpenDir() {
  await runAction(() => loggingStore.openDir(), 'settings.log_error_open_dir')
}

function handleRequestClearLogs() {
  clearLogsModalVisible.value = true
}

async function handleConfirmClearLogs() {
  try {
    await loggingStore.clearLogs()
    notify.success(t('settings.log_success_cleared_logs'))
    clearLogsModalVisible.value = false
  } catch (error) {
    notify.error(
      t('settings.log_error_clear_logs', {
        error: formatError(error),
      }),
    )
  }
}
</script>

<template>
  <SettingsSection :title="t('settings.section_logs')">
    <div v-if="loggingStore.status" class="min-w-0">
      <SettingsRow :label="t('settings.log_enabled_label')">
        <USwitch
          :model-value="enabledModel"
          :disabled="loggingStore.isUpdatingEnabled"
          @update:model-value="handleEnabledUpdate"
        />
      </SettingsRow>

      <SettingsRow :label="t('settings.log_level_label')">
        <USelect
          :model-value="levelModel"
          :items="levelOptions"
          class="w-[min(220px,100%)]"
          :disabled="loggingStore.isUpdatingLevel"
          @update:model-value="handleLevelUpdate"
        />
      </SettingsRow>

      <SettingsRow :label="t('settings.log_maintenance_label')" class="mt-2">
        <div class="flex flex-wrap items-center gap-2">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-folder-open"
            :loading="loggingStore.isOpeningDir"
            :label="t('settings.log_open_dir')"
            @click="handleOpenDir"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-trash-2"
            :loading="loggingStore.isClearingLogs"
            :label="t('settings.log_clear_logs')"
            @click="handleRequestClearLogs"
          />
        </div>
      </SettingsRow>
    </div>
    <ConfirmCardModal
      :show="clearLogsModalVisible"
      :title="t('settings.log_clear_logs')"
      :positive-text="t('settings.log_clear_logs')"
      :negative-text="t('history.cancel')"
      positive-type="warning"
      :positive-loading="loggingStore.isClearingLogs"
      @update:show="clearLogsModalVisible = $event"
      @positive-click="handleConfirmClearLogs"
    >
      {{ t('settings.log_clear_logs_confirm') }}
    </ConfirmCardModal>
  </SettingsSection>
</template>
