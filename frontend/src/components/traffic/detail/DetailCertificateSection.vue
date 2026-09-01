<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { formatUnixMicrosLocal } from '@/utils/format'

defineProps<{
  selectedEntry: TrafficEntry
}>()

const { t } = useI18n()
const showCertInfo = ref(false)

function toggleCertInfo() {
  showCertInfo.value = !showCertInfo.value
}
</script>

<template>
  <div class="mt-1 flex flex-col">
    <div
      class="flex items-center justify-between gap-3 rounded-(--radius-sm,6px) px-2 py-1.75 text-sm font-semibold text-app-text-secondary outline-none transition-[background-color,box-shadow,color] duration-200 ease-[ease] select-none hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:text-app-text focus-visible:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] focus-visible:text-app-text focus-visible:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_48%,transparent)]"
      :class="
        showCertInfo &&
        'bg-[color-mix(in_srgb,var(--app-accent-color)_14%,transparent)] shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]'
      "
      role="button"
      tabindex="0"
      @click="toggleCertInfo"
      @keydown.enter.prevent="toggleCertInfo"
      @keydown.space.prevent="toggleCertInfo"
    >
      <span class="min-w-0 truncate" :class="{ 'text-app-accent': showCertInfo }">{{
        t('detail.cert_info')
      }}</span>
      <UIcon
        :name="showCertInfo ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
        class="size-3.75 shrink-0 text-[15px]"
        :class="{ 'text-app-accent': showCertInfo }"
      />
    </div>
    <div v-show="showCertInfo" class="flex flex-col gap-0.5">
      <div
        v-if="selectedEntry.metadata?.certificate?.version"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_version') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.version }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.notBeforeMicros"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_not_before') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ formatUnixMicrosLocal(selectedEntry.metadata.certificate.notBeforeMicros) }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.notAfterMicros"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_not_after') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ formatUnixMicrosLocal(selectedEntry.metadata.certificate.notAfterMicros) }}
        </div>
      </div>
      <!-- Subject -->
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.commonName"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.commonName }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.organization?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_org') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.organization.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.organizationalUnit?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_ou') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.organizationalUnit.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.country?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_country') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.country.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.locality?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_locality') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.locality.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.province?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_province') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.province.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.streetAddress?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_street') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.streetAddress.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.postalCode?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_postal') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.postalCode.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.subject?.serialNumber"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_subject_serial') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.subject!.serialNumber }}
        </div>
      </div>
      <!-- Issuer -->
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.commonName"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.commonName }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.organization?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_org') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.organization.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.organizationalUnit?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_ou') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.organizationalUnit.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.country?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_country') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.country.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.locality?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_locality') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.locality.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.province?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_province') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.province.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.streetAddress?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_street') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.streetAddress.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.postalCode?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_postal') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.postalCode.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.issuer?.serialNumber"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_issuer_serial') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.issuer!.serialNumber }}
        </div>
      </div>
      <!-- Other -->
      <div
        v-if="selectedEntry.metadata?.certificate?.ipAddresses?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_ips') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.ipAddresses!.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.dnsNames?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_dns') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.dnsNames!.join(', ') }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.signatureAlgorithm"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_signature_algorithm') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.signatureAlgorithm }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.serialNumber"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_serial_number') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.serialNumber }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.sha1Fingerprint"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_sha1_fingerprint') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.sha1Fingerprint }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.certificate?.sha256Fingerprint"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cert_sha256_fingerprint') }}
        </div>
        <div class="flex-1 select-text font-mono text-sm break-all text-app-text">
          {{ selectedEntry.metadata.certificate.sha256Fingerprint }}
        </div>
      </div>
    </div>
  </div>
</template>
