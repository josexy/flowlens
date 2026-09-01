<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import SettingsInfoGrid from '@/components/settings/SettingsInfoGrid.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import type { SettingsInfoGridItem } from '@/components/settings/SettingsInfoGrid.vue'
import type {
  CacheConfig,
  HistoryRetentionConfig,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import { HistoryRetentionUnit } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

const props = defineProps<{
  dataSizeItems: readonly SettingsInfoGridItem[]
  clearCacheSuccess: boolean
  clearCacheAndHistorySuccess: boolean
  clearPending: boolean
}>()

const cacheConfig = defineModel<CacheConfig>('cacheConfig', { required: true })
const historyRetentionConfig = defineModel<HistoryRetentionConfig>('historyRetentionConfig', {
  required: true,
})

const emit = defineEmits<{
  clearCache: []
  clearCacheAndHistory: []
}>()

const { t } = useI18n()
const clearCacheModalVisible = ref(false)
const clearCacheAndHistoryModalVisible = ref(false)
const clearRequestPending = ref(false)
const clearActionPending = computed(() => props.clearPending || clearRequestPending.value)
const historyRetentionUnitItems = computed(() => [
  {
    label: t('settings.history_retention_unit_hour'),
    value: HistoryRetentionUnit.HistoryRetentionUnitHour,
  },
  {
    label: t('settings.history_retention_unit_day'),
    value: HistoryRetentionUnit.HistoryRetentionUnitDay,
  },
  {
    label: t('settings.history_retention_unit_week'),
    value: HistoryRetentionUnit.HistoryRetentionUnitWeek,
  },
  {
    label: t('settings.history_retention_unit_month'),
    value: HistoryRetentionUnit.HistoryRetentionUnitMonth,
  },
  {
    label: t('settings.history_retention_unit_year'),
    value: HistoryRetentionUnit.HistoryRetentionUnitYear,
  },
])

function handleClearCacheConfirm() {
  if (clearActionPending.value) {
    return
  }
  clearRequestPending.value = true
  emit('clearCache')
}

function handleClearCacheAndHistoryConfirm() {
  if (clearActionPending.value) {
    return
  }
  clearRequestPending.value = true
  emit('clearCacheAndHistory')
}

function setClearCacheModalVisible(visible: boolean) {
  if (!clearActionPending.value) {
    clearCacheModalVisible.value = visible
  }
}

function setClearCacheAndHistoryModalVisible(visible: boolean) {
  if (!clearActionPending.value) {
    clearCacheAndHistoryModalVisible.value = visible
  }
}

watch(
  () => props.clearPending,
  (pending, wasPending) => {
    if (!pending && wasPending) {
      clearRequestPending.value = false
      clearCacheModalVisible.value = false
      clearCacheAndHistoryModalVisible.value = false
    }
  },
)
</script>

<template>
  <div class="min-w-0">
    <SettingsSection :title="t('settings.section_cache')">
      <SettingsRow
        :label="t('settings.cache_threshold_label')"
        :hint="t('settings.cache_threshold_hint')"
        hint-placement="aside"
      >
        <UInputNumber
          v-model="cacheConfig.bodyCacheThresholdBytes"
          orientation="vertical"
          :min="1"
          :step="1"
          class="w-[min(220px,100%)]"
        />
      </SettingsRow>
      <SettingsRow
        :label="t('settings.ws_max_messages_label')"
        :hint="t('settings.ws_max_messages_hint')"
        hint-placement="aside"
      >
        <UInputNumber
          v-model="cacheConfig.maxWsMessages"
          orientation="vertical"
          :min="1"
          :step="1"
          class="w-[min(220px,100%)]"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :title="t('settings.section_history_retention')">
      <SettingsRow
        :label="t('settings.history_retention_enabled_label')"
        :hint="t('settings.history_retention_enabled_hint')"
        hint-placement="aside"
      >
        <USwitch
          v-model="historyRetentionConfig.enabled"
          :aria-label="t('settings.history_retention_enabled_label')"
        />
      </SettingsRow>
      <SettingsRow
        :label="t('settings.history_retention_period_label')"
        :hint="t('settings.history_retention_period_hint')"
        hint-placement="aside"
      >
        <UFieldGroup class="w-[min(320px,100%)]">
          <UInputNumber
            v-model="historyRetentionConfig.value"
            orientation="vertical"
            :min="1"
            :max="9999"
            :step="1"
            :disabled="!historyRetentionConfig.enabled"
            :aria-label="t('settings.history_retention_period_label')"
            class="min-w-0 flex-1"
          />
          <USelect
            v-model="historyRetentionConfig.unit"
            :items="historyRetentionUnitItems"
            :disabled="!historyRetentionConfig.enabled"
            :aria-label="t('settings.history_retention_unit_label')"
            class="w-28"
          />
        </UFieldGroup>
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :title="t('settings.section_danger')" danger>
      <SettingsInfoGrid :items="dataSizeItems" compact class="mx-0 mt-0.5 mb-3" />
      <UAlert
        v-if="clearCacheSuccess"
        color="success"
        variant="subtle"
        :title="t('settings.clear_cache_success')"
        class="my-3"
      />
      <UAlert
        v-if="clearCacheAndHistorySuccess"
        color="success"
        variant="subtle"
        :title="t('settings.clear_all_data_success')"
        class="my-3"
      />
      <div class="mt-3 flex flex-col gap-2">
        <div
          class="flex items-center justify-between gap-4 rounded-md border border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_44%,transparent)] p-3 max-[720px]:flex-col max-[720px]:items-start"
        >
          <div class="min-w-0">
            <div class="text-sm font-semibold text-app-text">
              {{ t('settings.clear_cache_files') }}
            </div>
            <div class="mt-1 text-sm leading-normal text-app-text-muted">
              {{ t('settings.clear_cache_files_desc') }}
            </div>
          </div>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-trash-2"
            :label="t('settings.clear_action')"
            :aria-label="t('settings.clear_cache_files')"
            :disabled="clearActionPending"
            @click="clearCacheModalVisible = true"
          />
        </div>

        <div
          class="flex items-center justify-between gap-4 rounded-md border border-[color-mix(in_srgb,var(--app-error-color)_35%,var(--app-border-color))] bg-[color-mix(in_srgb,var(--app-elevated-bg)_44%,transparent)] p-3 max-[720px]:flex-col max-[720px]:items-start"
        >
          <div class="min-w-0">
            <div class="text-sm font-semibold text-app-text">
              {{ t('settings.clear_all_data') }}
            </div>
            <div class="mt-1 text-sm leading-normal text-app-text-muted">
              {{ t('settings.clear_all_data_desc') }}
            </div>
          </div>
          <UButton
            color="error"
            icon="i-lucide-trash-2"
            :label="t('settings.clear_action')"
            :aria-label="t('settings.clear_all_data')"
            :disabled="clearActionPending"
            @click="clearCacheAndHistoryModalVisible = true"
          />
        </div>
      </div>
    </SettingsSection>

    <ConfirmCardModal
      :show="clearCacheModalVisible"
      :title="t('settings.clear_cache_files')"
      :positive-text="t('settings.clear_action')"
      :negative-text="t('history.cancel')"
      positive-type="warning"
      :positive-disabled="clearActionPending"
      :positive-loading="clearActionPending"
      :negative-disabled="clearActionPending"
      :closable="!clearActionPending"
      :mask-closable="!clearActionPending"
      @update:show="setClearCacheModalVisible"
      @positive-click="handleClearCacheConfirm"
    >
      {{ t('settings.clear_cache_files_confirm') }}
    </ConfirmCardModal>
    <ConfirmCardModal
      :show="clearCacheAndHistoryModalVisible"
      :title="t('settings.clear_all_data')"
      :positive-text="t('settings.clear_action')"
      :negative-text="t('history.cancel')"
      positive-type="error"
      :positive-disabled="clearActionPending"
      :positive-loading="clearActionPending"
      :negative-disabled="clearActionPending"
      :closable="!clearActionPending"
      :mask-closable="!clearActionPending"
      @update:show="setClearCacheAndHistoryModalVisible"
      @positive-click="handleClearCacheAndHistoryConfirm"
    >
      {{ t('settings.clear_all_data_confirm') }}
    </ConfirmCardModal>
  </div>
</template>
