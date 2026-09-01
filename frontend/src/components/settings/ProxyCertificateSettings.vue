<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CACertificateInfoPanel from '@/components/settings/CACertificateInfoPanel.vue'
import CertificateFileInput from '@/components/settings/CertificateFileInput.vue'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import ClientCertificateList from '@/components/settings/ClientCertificateList.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import RootCAPathList from '@/components/settings/RootCAPathList.vue'
import type { FileFilter } from '@/utils/dialog'
import type {
  CACertificateInfo,
  ClientCertConfig,
  ProxyConfig,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

defineProps<{
  caInfo: CACertificateInfo | null
  caHasExistingFiles: boolean
  isGenerating: boolean
  certGeneratedSuccess: boolean
}>()
const proxyConfig = defineModel<ProxyConfig>('proxyConfig', { required: true })

const emit = defineEmits<{
  generateCa: [overwrite: boolean]
}>()

const { t } = useI18n()

function ensureCertificateConfig() {
  proxyConfig.value.rootCAPaths ??= []
  proxyConfig.value.clientCerts ??= []
}

watch(() => proxyConfig.value, ensureCertificateConfig, { immediate: true })

const regenerateModalVisible = ref(false)
const rootCAPaths = computed<string[]>({
  get: () => proxyConfig.value.rootCAPaths ?? [],
  set: (value) => {
    proxyConfig.value.rootCAPaths = value
  },
})
const clientCerts = computed<ClientCertConfig[]>({
  get: () => proxyConfig.value.clientCerts ?? [],
  set: (value) => {
    proxyConfig.value.clientCerts = value
  },
})
const certificateFilters = computed<FileFilter[]>(() => [
  {
    DisplayName: t('settings.certificate_file_filter'),
    Pattern: '*.crt;*.cer;*.pem',
  },
])
const keyFilters = computed<FileFilter[]>(() => [
  {
    DisplayName: t('settings.private_key_file_filter'),
    Pattern: '*.key;*.pem',
  },
])
function handleRegenerateConfirm() {
  emit('generateCa', true)
  regenerateModalVisible.value = false
}
</script>

<template>
  <div class="flex min-w-0 flex-col gap-4.5">
    <SettingsSection :title="t('settings.section_certificate')">
      <SettingsRow :label="t('toolbar.ca_cert_path')">
        <CertificateFileInput
          v-model="proxyConfig.caCertPath"
          :button-title="t('settings.select_ca_cert_file')"
          :dialog-title="t('settings.select_ca_cert_file')"
          :dialog-message="t('settings.select_ca_cert_file_message')"
          :filters="certificateFilters"
          class="w-[min(520px,100%)]"
        />
      </SettingsRow>
      <SettingsRow :label="t('toolbar.ca_key_path')">
        <CertificateFileInput
          v-model="proxyConfig.caKeyPath"
          :button-title="t('settings.select_ca_key_file')"
          :dialog-title="t('settings.select_ca_key_file')"
          :dialog-message="t('settings.select_ca_key_file_message')"
          :filters="keyFilters"
          class="w-[min(520px,100%)]"
        />
      </SettingsRow>

      <CACertificateInfoPanel
        :ca-info="caInfo"
        :ca-has-existing-files="caHasExistingFiles"
        :is-generating="isGenerating"
        :cert-generated-success="certGeneratedSuccess"
        @generate-ca="emit('generateCa', $event)"
        @request-regenerate="regenerateModalVisible = true"
      />
    </SettingsSection>

    <SettingsSection :title="t('settings.section_root_cas')" :separated="false">
      <RootCAPathList v-model="rootCAPaths" />
    </SettingsSection>

    <SettingsSection :title="t('settings.section_client_certs')" :separated="false">
      <ClientCertificateList v-model="clientCerts" />
    </SettingsSection>

    <ConfirmCardModal
      :show="regenerateModalVisible"
      :title="t('settings.regenerate_ca')"
      :positive-text="t('settings.regenerate_ca')"
      :negative-text="t('history.cancel')"
      positive-type="warning"
      @update:show="regenerateModalVisible = $event"
      @positive-click="handleRegenerateConfirm"
    >
      {{ t('settings.regenerate_ca_confirm') }}
    </ConfirmCardModal>
  </div>
</template>
