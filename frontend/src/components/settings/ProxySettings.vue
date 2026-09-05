<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import ProxyCertificateSettings from '@/components/settings/ProxyCertificateSettings.vue'
import ProxyConnectionSettings from '@/components/settings/ProxyConnectionSettings.vue'
import ProcessAttributionSettings from '@/components/settings/ProcessAttributionSettings.vue'
import type {
  CACertificateInfo,
  ProcessAttributionConfig,
  ProxyConfig,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

defineProps<{
  caInfo: CACertificateInfo | null
  caHasExistingFiles: boolean
  isGenerating: boolean
  certGeneratedSuccess: boolean
}>()

const proxyConfig = defineModel<ProxyConfig>('proxyConfig', { required: true })
const processAttributionConfig = defineModel<ProcessAttributionConfig>('processAttributionConfig', {
  required: true,
})

const emit = defineEmits<{
  generateCa: [overwrite: boolean]
}>()

const { t } = useI18n()
</script>

<template>
  <div class="flex min-w-0 flex-col gap-6">
    <SettingsSection :title="t('settings.section_connection')">
      <div class="space-y-2">
        <ProxyConnectionSettings v-model:proxy-config="proxyConfig" />
        <ProcessAttributionSettings v-model:process-attribution-config="processAttributionConfig" />
      </div>
    </SettingsSection>
    <ProxyCertificateSettings
      v-model:proxy-config="proxyConfig"
      :ca-info="caInfo"
      :ca-has-existing-files="caHasExistingFiles"
      :is-generating="isGenerating"
      :cert-generated-success="certGeneratedSuccess"
      @generate-ca="emit('generateCa', $event)"
    />
  </div>
</template>
