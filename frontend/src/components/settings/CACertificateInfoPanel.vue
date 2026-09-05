<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import type { CACertificateInfo } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import { formatUnixMicrosLocal } from '@/utils/format'

type Tone = 'neutral' | 'success' | 'warning'

interface StatusItem {
  label: string
  value: string
  tone: Tone
}

interface DetailItem {
  label: string
  value: string
  monospace?: boolean
  wide?: boolean
}

const props = defineProps<{
  caInfo: CACertificateInfo | null
  caHasExistingFiles: boolean
  isGenerating: boolean
  certGeneratedSuccess: boolean
}>()

const emit = defineEmits<{
  generateCa: [overwrite: boolean]
  requestRegenerate: []
}>()

const { t } = useI18n()

const isReady = computed(() => {
  const info = props.caInfo
  return Boolean(info?.certExists && info.keyExists && info.validPair && info.isCa && !info.error)
})

const summaryTone = computed<Tone>(() => {
  if (!props.caInfo) return 'neutral'
  return isReady.value ? 'success' : 'warning'
})

const statusSummary = computed(() => {
  const info = props.caInfo
  if (!info) return t('settings.ca_status_unavailable')
  if (!info.certExists || !info.keyExists) return t('settings.ca_files_missing')
  if (!info.error && !info.isCa) return t('settings.ca_not_ca')
  return isReady.value ? t('settings.ca_status_ready') : t('settings.ca_status_incomplete')
})

const statusDescription = computed(() => {
  const info = props.caInfo
  return info?.certExists && info.keyExists ? info.error : ''
})

function makeStatusItem(label: string, passed: boolean | undefined): StatusItem {
  if (passed === undefined) {
    return {
      label,
      value: '-',
      tone: 'neutral',
    }
  }

  return {
    label,
    value: passed ? t('settings.yes') : t('settings.no'),
    tone: passed ? 'success' : 'warning',
  }
}

const statusItems = computed<StatusItem[]>(() => {
  const info = props.caInfo
  return [
    makeStatusItem(t('settings.ca_cert_exists'), info?.certExists),
    makeStatusItem(t('settings.ca_key_exists'), info?.keyExists),
    makeStatusItem(t('settings.ca_valid_pair'), info?.validPair),
    makeStatusItem(t('settings.ca_is_ca'), info?.isCa),
  ]
})

const detailItems = computed<DetailItem[]>(() => {
  const info = props.caInfo
  if (!info) return []

  const items: Array<DetailItem | null> = [
    info.subject ? { label: t('settings.ca_subject'), value: info.subject } : null,
    info.issuer ? { label: t('settings.ca_issuer'), value: info.issuer } : null,
    info.notBeforeMicros
      ? {
          label: t('settings.ca_valid_from'),
          value: formatUnixMicrosLocal(info.notBeforeMicros),
          monospace: true,
        }
      : null,
    info.notAfterMicros
      ? {
          label: t('settings.ca_valid_to'),
          value: formatUnixMicrosLocal(info.notAfterMicros),
          monospace: true,
        }
      : null,
    info.sha256Fingerprint
      ? {
          label: t('settings.ca_fingerprint'),
          value: info.sha256Fingerprint,
          monospace: true,
          wide: true,
        }
      : null,
  ]

  return items.filter((item): item is DetailItem => Boolean(item))
})

const summaryTextClass: Record<Tone, string> = {
  success: 'text-app-success',
  warning: 'text-app-warning',
  neutral: 'text-app-text-muted',
}

const pillValueClass: Record<Tone, string> = {
  success: 'text-app-success',
  warning: 'text-app-warning',
  neutral: 'text-app-text-muted',
}
</script>

<template>
  <SettingsSection :title="t('settings.section_certificate')">
    <template #actions>
      <UButton
        v-if="caHasExistingFiles"
        color="neutral"
        variant="outline"
        :loading="isGenerating"
        :label="t('settings.regenerate_ca')"
        @click="emit('requestRegenerate')"
      />
      <UButton
        v-else
        color="neutral"
        variant="outline"
        :loading="isGenerating"
        :label="t('settings.generate_ca')"
        @click="emit('generateCa', false)"
      />
    </template>

    <div
      role="status"
      class="mb-4 flex min-w-0 items-start gap-2 text-sm leading-relaxed"
      :class="summaryTextClass[summaryTone]"
    >
      <UIcon
        :name="isReady ? 'i-lucide-circle-check' : 'i-lucide-info'"
        class="mt-0.5 size-4 shrink-0"
      />
      <div class="min-w-0 wrap-anywhere">
        <div>{{ statusSummary }}</div>
        <div v-if="statusDescription" class="mt-1">{{ statusDescription }}</div>
      </div>
    </div>

    <UAlert
      v-if="certGeneratedSuccess"
      color="success"
      variant="subtle"
      :title="t('settings.ca_generated_restart_notice')"
      class="mb-3"
    />
    <div class="space-y-2">
      <slot />
    </div>

    <UCollapsible v-if="caInfo" class="mt-4" :ui="{ content: 'motion-reduce:animate-none' }">
      <template #default="{ open }">
        <UButton
          color="neutral"
          variant="link"
          :icon="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
          :label="t(open ? 'settings.ca_hide_details' : 'settings.ca_show_details')"
          class="px-0"
        />
      </template>
      <template #content>
        <div class="mt-3 rounded-md border border-app-border bg-app-control/30 p-3">
          <dl class="grid min-w-0 grid-cols-2 gap-x-6 gap-y-2 max-[680px]:grid-cols-1">
            <div
              v-for="item in statusItems"
              :key="item.label"
              class="flex min-w-0 items-center justify-between gap-3 text-sm"
            >
              <dt class="text-app-text-muted">{{ item.label }}</dt>
              <dd class="m-0 shrink-0 font-medium" :class="pillValueClass[item.tone]">
                {{ item.value }}
              </dd>
            </div>
          </dl>
          <dl
            v-if="detailItems.length"
            class="mt-3 grid min-w-0 grid-cols-2 gap-x-6 border-t border-app-border pt-3 max-[760px]:grid-cols-1"
          >
            <div
              v-for="item in detailItems"
              :key="item.label"
              class="min-w-0 py-1.5 text-sm leading-relaxed"
              :class="{ 'col-span-full': item.wide }"
            >
              <dt class="text-app-text-muted">{{ item.label }}</dt>
              <dd
                class="m-0 mt-0.5 text-app-text-secondary wrap-anywhere"
                :style="item.monospace ? 'font-family: var(--code-font-family)' : ''"
              >
                {{ item.value }}
              </dd>
            </div>
          </dl>
        </div>
      </template>
    </UCollapsible>
  </SettingsSection>
</template>
