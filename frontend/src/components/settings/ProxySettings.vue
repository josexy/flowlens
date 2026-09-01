<script setup lang="ts">
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
const processAttributionConfig = defineModel<ProcessAttributionConfig>(
  'processAttributionConfig',
  { required: true },
)

const emit = defineEmits<{
  generateCa: [overwrite: boolean]
}>()
</script>

<template>
  <div class="flex min-w-0 flex-col gap-4.5">
    <ProxyConnectionSettings v-model:proxy-config="proxyConfig" />
    <ProcessAttributionSettings
      v-model:process-attribution-config="processAttributionConfig"
    />
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
