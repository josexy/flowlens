<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
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
  return Boolean(
    info?.certExists && info.keyExists && info.validPair && info.isCa && !info.error,
  )
})

const summaryTone = computed<Tone>(() => {
  if (!props.caInfo) return 'neutral'
  return isReady.value ? 'success' : 'warning'
})

const statusSummary = computed(() => {
  if (!props.caInfo) return t('settings.ca_status_unavailable')
  return isReady.value ? t('settings.ca_status_ready') : t('settings.ca_status_incomplete')
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

const panelBorderClass: Record<Tone, string> = {
  success: 'border-l-app-success',
  warning: 'border-l-app-warning',
  neutral: 'border-l-app-border',
}

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
  <section
    class="mt-2 min-w-0 rounded-md border border-l-[3px] border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_46%,transparent)] p-3"
    :class="panelBorderClass[summaryTone]"
  >
    <div class="mb-3 flex items-start justify-between gap-4 max-[760px]:flex-col max-[760px]:items-stretch">
      <div class="flex min-w-0 items-start gap-2.5">
        <div
          class="flex size-7 shrink-0 items-center justify-center rounded-md bg-app-accent-softer text-[18px] text-app-accent"
        >
          <UIcon name="i-lucide-shield-check" class="size-[1em]" />
        </div>
        <div class="min-w-0">
          <div class="text-sm font-bold leading-[1.35] text-app-text">{{ t('settings.ca_current') }}</div>
          <div class="mt-0.5 text-sm leading-[1.45]" :class="summaryTextClass[summaryTone]">
            {{ statusSummary }}
          </div>
        </div>
      </div>

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
        :loading="isGenerating"
        :label="t('settings.generate_ca')"
        @click="emit('generateCa', false)"
      />
    </div>

    <UAlert
      v-if="certGeneratedSuccess"
      color="success"
      variant="subtle"
      :title="t('settings.ca_generated_restart_notice')"
      class="mb-3"
    />
    <UAlert
      v-if="caInfo?.error"
      color="warning"
      variant="subtle"
      :title="caInfo.error"
      class="mb-3"
    />

    <div class="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(142px,1fr))] gap-2">
      <div
        v-for="item in statusItems"
        :key="item.label"
        class="flex min-h-8 min-w-0 items-center justify-between gap-2.5 rounded-md border border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_66%,transparent)] px-2 py-1.5"
      >
        <span class="min-w-0 truncate text-sm font-semibold leading-[1.35] text-app-text-muted">{{ item.label }}</span>
        <span class="shrink-0 text-sm font-bold leading-[1.35]" :class="pillValueClass[item.tone]">{{ item.value }}</span>
      </div>
    </div>

    <dl
      v-if="detailItems.length"
      class="mt-3 grid min-w-0 grid-cols-2 gap-x-4.5 gap-y-0 border-t border-app-border pt-2.5 max-[760px]:grid-cols-1"
    >
      <div
        v-for="item in detailItems"
        :key="item.label"
        class="grid min-w-0 grid-cols-[minmax(74px,108px)_minmax(0,1fr)] gap-2.5 py-1.25 max-[760px]:grid-cols-1 max-[760px]:gap-0.5"
        :class="{ 'col-span-full': item.wide }"
      >
        <dt class="min-w-0 text-sm font-semibold leading-[1.45] text-app-text-muted">{{ item.label }}</dt>
        <dd
          class="m-0 min-w-0 text-sm font-medium leading-[1.45] text-app-text-secondary wrap-anywhere"
          :class="[
            item.wide
              ? 'rounded-md border border-app-border bg-[color-mix(in_srgb,var(--app-elevated-bg)_72%,transparent)] px-2 py-1.5'
              : '',
          ]"
          :style="item.monospace ? 'font-family: var(--code-font-family)' : ''"
        >
          {{ item.value }}
        </dd>
      </div>
    </dl>
  </section>
</template>
