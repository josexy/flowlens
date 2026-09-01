<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import type { ProcessAttributionConfig } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

const processAttributionConfig = defineModel<ProcessAttributionConfig>(
  'processAttributionConfig',
  { required: true },
)

const { t } = useI18n()
</script>

<template>
  <SettingsSection>
    <SettingsRow
      :label="t('settings.process_attribution_label')"
    >
      <USwitch
        v-model="processAttributionConfig.enabled"
        :aria-label="t('settings.process_attribution_label')"
      />
    </SettingsRow>
  </SettingsSection>
</template>
