<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'

defineProps<{
  selectedEntry: TrafficEntry
}>()

const { t } = useI18n()
const showTlsInfo = ref(false)

function toggleTlsInfo() {
  showTlsInfo.value = !showTlsInfo.value
}
</script>

<template>
  <div class="mt-1 flex flex-col">
    <div
      class="flex items-center justify-between gap-3 rounded-(--radius-sm,6px) px-2 py-1.75 text-sm font-semibold text-app-text-secondary outline-none transition-[background-color,box-shadow,color] duration-200 ease-[ease] select-none hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:text-app-text focus-visible:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] focus-visible:text-app-text focus-visible:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_48%,transparent)]"
      :class="
        showTlsInfo &&
        'bg-[color-mix(in_srgb,var(--app-accent-color)_14%,transparent)] shadow-[inset_3px_0_0_var(--app-accent-color),inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]'
      "
      role="button"
      tabindex="0"
      @click="toggleTlsInfo"
      @keydown.enter.prevent="toggleTlsInfo"
      @keydown.space.prevent="toggleTlsInfo"
    >
      <span class="min-w-0 truncate" :class="{ 'text-app-accent': showTlsInfo }">{{
        t('detail.tls_info')
      }}</span>
      <UIcon
        :name="showTlsInfo ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
        class="size-3.75 shrink-0 text-[15px]"
        :class="{ 'text-app-accent': showTlsInfo }"
      />
    </div>
    <div v-show="showTlsInfo" class="flex flex-col gap-0.5">
      <div
        v-if="selectedEntry.metadata?.tls?.serverName"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.tls_server_name') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.tls.serverName }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.selectedVersion"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.tls_version') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.tls.selectedVersion }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.selectedAlpn"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.alpn') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.tls.selectedAlpn }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.selectedCipherSuite"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.cipher_suite') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          {{ selectedEntry.metadata.tls.selectedCipherSuite }}
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.supportedVersion?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.supported_tls_versions') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          <ul class="m-0 flex list-none flex-col gap-0.75 p-0">
            <li
              v-for="(version, index) in selectedEntry.metadata.tls.supportedVersion"
              :key="`tls-version-${index}-${version}`"
              class="leading-[1.45] break-normal wrap-anywhere"
            >
              {{ version }}
            </li>
          </ul>
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.supportedAlpn?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.supported_alpns') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          <ul class="m-0 flex list-none flex-col gap-0.75 p-0">
            <li
              v-for="(alpn, index) in selectedEntry.metadata.tls.supportedAlpn"
              :key="`tls-alpn-${index}-${alpn}`"
              class="leading-[1.45] break-normal wrap-anywhere"
            >
              {{ alpn }}
            </li>
          </ul>
        </div>
      </div>
      <div
        v-if="selectedEntry.metadata?.tls?.supportedCipherSuites?.length"
        class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
      >
        <div
          class="w-30 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
        >
          {{ t('detail.supported_cipher_suites') }}
        </div>
        <div class="flex-1 select-text font-sans text-sm break-all text-app-text">
          <ul class="m-0 flex list-none flex-col gap-0.75 p-0">
            <li
              v-for="(cipherSuite, index) in selectedEntry.metadata.tls.supportedCipherSuites"
              :key="`tls-cipher-suite-${index}-${cipherSuite}`"
              class="leading-[1.45] break-normal wrap-anywhere"
            >
              {{ cipherSuite }}
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
