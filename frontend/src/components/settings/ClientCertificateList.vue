<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import CertificateFileInput from '@/components/settings/CertificateFileInput.vue'
import type { FileFilter } from '@/utils/dialog'
import type { ClientCertConfig } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

const certs = defineModel<ClientCertConfig[]>({ required: true })

const { t } = useI18n()

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

const certificateWarnings = computed(() =>
  certs.value.map((cert) => {
    if (!cert.enabled) {
      return ''
    }

    const missingFields = [
      !cert.hostname.trim() ? t('settings.client_cert_target_host') : '',
      !cert.certPath.trim() ? t('settings.client_cert_file') : '',
      !cert.keyPath.trim() ? t('settings.client_key_file') : '',
    ].filter(Boolean)

    if (missingFields.length === 0) {
      return ''
    }

    return t('settings.client_cert_missing_fields', {
      fields: missingFields.join(t('settings.field_separator')),
    })
  }),
)

function addClientCert() {
  certs.value.push({
    enabled: true,
    hostname: '',
    certPath: '',
    keyPath: '',
  })
}

function removeClientCert(index: number) {
  certs.value.splice(index, 1)
}

function clientCertTitle(cert: ClientCertConfig, index: number) {
  return cert.hostname.trim() || t('settings.client_cert_new_title', { index: index + 1 })
}
</script>

<template>
  <div class="flex min-w-0 flex-col gap-2.5">
    <div class="flex flex-wrap items-center gap-2">
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-plus"
        :label="t('settings.add_client_cert')"
        @click="addClientCert"
      />
    </div>

    <div
      v-if="certs.length === 0"
      class="flex items-start gap-2.5 rounded-md border border-dashed border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_36%,transparent)] p-3"
    >
      <div
        class="flex size-7 shrink-0 items-center justify-center rounded-md bg-app-accent-softer text-[18px] text-app-accent"
      >
        <UIcon name="i-lucide-key-round" class="size-[1em]" />
      </div>
      <div class="min-w-0">
        <div class="text-sm font-semibold leading-[1.35] text-app-text-secondary">{{ t('settings.no_client_certs') }}</div>
        <div class="mt-0.5 text-sm leading-[1.45] text-app-text-muted">
          {{ t('settings.no_client_certs_desc') }}
        </div>
      </div>
    </div>

    <div v-else class="flex flex-col gap-2.5">
      <section
        v-for="(cert, index) in certs"
        :key="index"
        class="flex min-w-0 flex-col gap-2.5 rounded-md border border-app-border p-3"
        :class="
          cert.enabled
            ? 'bg-[color-mix(in_srgb,var(--app-elevated-bg)_30%,transparent)]'
            : 'bg-[color-mix(in_srgb,var(--app-elevated-bg)_14%,transparent)]'
        "
      >
        <div class="flex items-center justify-between gap-3">
          <div class="flex min-w-0 items-center gap-2.5">
            <USwitch v-model="cert.enabled" />
            <div class="min-w-0">
              <div class="truncate text-sm font-bold leading-[1.35] text-app-text">{{ clientCertTitle(cert, index) }}</div>
              <div class="mt-px text-sm leading-[1.35] text-app-text-muted">
                {{
                  cert.enabled
                    ? t('settings.client_cert_enabled')
                    : t('settings.client_cert_disabled')
                }}
              </div>
            </div>
          </div>
          <UTooltip :text="t('settings.remove_client_cert')" :content="{ side: 'top' }">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-trash-2"
              class="shrink-0"
              :aria-label="t('settings.remove_client_cert')"
              @click="removeClientCert(index)"
            />
          </UTooltip>
        </div>

        <div
          class="grid min-w-0 grid-cols-[minmax(180px,0.8fr)_minmax(210px,1fr)_minmax(210px,1fr)] gap-2.5 max-[920px]:grid-cols-1"
        >
          <label class="flex min-w-0 flex-col gap-1.25">
            <span class="text-sm font-semibold leading-[1.35] text-app-text-secondary">
              {{ t('settings.client_cert_target_host') }}
            </span>
            <UInput
              v-model="cert.hostname"
              :placeholder="t('settings.client_cert_target_host_placeholder')"
              class="w-full"
            />
            <span class="text-sm leading-[1.35] text-app-text-muted">
              {{ t('settings.client_cert_target_host_hint') }}
            </span>
          </label>

          <CertificateFileInput
            v-model="cert.certPath"
            :label="t('settings.client_cert_file')"
            :placeholder="t('settings.client_cert_path_placeholder')"
            :dialog-title="t('settings.select_client_cert_file')"
            :dialog-message="t('settings.select_client_cert_file_message')"
            :filters="certificateFilters"
            class="w-full"
          />
          <CertificateFileInput
            v-model="cert.keyPath"
            :label="t('settings.client_key_file')"
            :placeholder="t('settings.client_key_path_placeholder')"
            :dialog-title="t('settings.select_client_key_file')"
            :dialog-message="t('settings.select_client_key_file_message')"
            :filters="keyFilters"
            class="w-full"
          />
        </div>

        <div
          v-if="certificateWarnings[index]"
          class="flex min-w-0 items-center gap-1.5 text-sm leading-[1.4] text-app-warning"
        >
          <UIcon name="i-lucide-info" class="size-[1em]" />
          <span>{{ certificateWarnings[index] }}</span>
        </div>
      </section>
    </div>
  </div>
</template>
