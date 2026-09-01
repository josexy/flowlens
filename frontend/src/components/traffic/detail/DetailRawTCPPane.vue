<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { formatDateTimeLocal } from '@/utils/format'
import { getTrafficProtocol, getTrafficTarget } from '@/utils/traffic'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import DetailCertificateSection from './DetailCertificateSection.vue'
import DetailProcessSection from './DetailProcessSection.vue'
import DetailTlsSection from './DetailTlsSection.vue'

const props = defineProps<{
  selectedEntry: TrafficEntry
}>()

const { t } = useI18n()

const target = computed(() => getTrafficTarget(props.selectedEntry))
const tunnelSource = computed(() => props.selectedEntry.rawTcp?.source || 'unknown')
const protocol = computed(() => getTrafficProtocol(props.selectedEntry))
const isTLS = computed(() => props.selectedEntry.rawTcp?.tls === true)
const outerConnectHeaders = computed(() =>
  (props.selectedEntry.request?.headerFields ?? []).filter((field) => field !== null),
)
const hasOuterConnectHeaders = computed(() => outerConnectHeaders.value.length > 0)
const isHTTPConnect = computed(() => tunnelSource.value === 'http_connect')

const sourceLabel = computed(() => {
  const translationKeys: Record<string, string> = {
    direct: 'detail.raw_tcp_source_direct',
    http_connect: 'detail.raw_tcp_source_http_connect',
    socks5: 'detail.raw_tcp_source_socks5',
    unknown: 'detail.raw_tcp_source_unknown',
  }
  return t(translationKeys[tunnelSource.value] ?? translationKeys.unknown!)
})

function isNonZeroTime(value: unknown): boolean {
  return !!value && !String(value).startsWith('0001-01-01')
}

function formatTimestamp(value: string | number | Date): string {
  return formatDateTimeLocal(value)
}

const detailRows = computed(() => {
  const entry = props.selectedEntry
  const metadata = entry.metadata
  const rows: Array<{ key: string; label: string; value: string }> = [
    {
      key: 'target',
      label: t('detail.raw_tcp_target'),
      value: target.value || '—',
    },
    {
      key: 'source',
      label: t('detail.raw_tcp_source'),
      value: sourceLabel.value,
    },
    {
      key: 'protocol',
      label: t('detail.protocol'),
      value: protocol.value || '—',
    },
  ]

  if (isHTTPConnect.value && entry.request?.proto) {
    rows.push({
      key: 'outer-protocol',
      label: t('detail.raw_tcp_outer_connect_protocol'),
      value: entry.request.proto,
    })
  }

  const addressRows: Array<[string, string, string | undefined]> = [
    ['local-source', t('detail.local_source_address'), metadata?.localSourceAddr],
    ['local-destination', t('detail.local_destination_address'), metadata?.localDestinationAddr],
    ['remote-source', t('detail.remote_source_address'), metadata?.remoteSourceAddr],
    ['remote-destination', t('detail.remote_destination_address'), metadata?.remoteDestinationAddr],
  ]
  for (const [key, label, value] of addressRows) {
    if (value) rows.push({ key, label, value })
  }

  const timeRows: Array<[string, string, unknown]> = [
    ['observed', t('detail.raw_tcp_observed_at'), entry.startedAt],
    [
      'local-connected',
      t('detail.local_connection_time'),
      metadata?.localConnectionEstablishedAt,
    ],
    [
      'remote-connected',
      t('detail.remote_connection_time'),
      metadata?.remoteConnectionEstablishedAt,
    ],
    ['tls-handshake', t('detail.ssl_handshake'), metadata?.sslHandshakeCompletedAt],
  ]
  for (const [key, label, value] of timeRows) {
    if (isNonZeroTime(value)) {
      rows.push({ key, label, value: formatTimestamp(value as string) })
    }
  }

  return rows
})
</script>

<template>
  <div
    class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden bg-app-panel"
    data-name="raw-tcp-detail"
  >
    <div class="min-h-0 flex-1 overflow-y-auto px-3 py-3.5">
      <div class="mx-auto flex w-full max-w-5xl flex-col gap-3">
        <section class="flex flex-col gap-0.5" :aria-label="t('detail.raw_tcp_tunnel_info')">
          <div class="flex items-center gap-2 px-2 pb-1 pt-0.5 text-sm font-semibold text-app-text">
            <UIcon name="i-lucide-network" class="size-4 text-app-accent" />
            <span>{{ t('detail.raw_tcp_tunnel_info') }}</span>
            <UBadge
              :label="isTLS ? 'TCP/TLS' : 'TCP'"
              :color="isTLS ? 'info' : 'neutral'"
              variant="subtle"
              size="sm"
            />
          </div>

          <div
            v-for="row in detailRows"
            :key="row.key"
            class="flex gap-2.5 rounded-(--radius-sm,6px) px-2 py-1 text-sm transition-[background-color,box-shadow] duration-200 ease-[ease] hover:bg-[color-mix(in_srgb,var(--app-accent-color)_12%,transparent)] hover:shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--app-accent-color)_22%,transparent)]"
          >
            <div
              class="w-36 shrink-0 select-text text-sm font-semibold tracking-[0.5px] text-app-text-secondary uppercase"
            >
              {{ row.label }}
            </div>
            <div
              class="min-w-0 flex-1 select-text font-sans text-sm break-all text-app-text"
            >
              {{ row.value }}
            </div>
          </div>

          <DetailProcessSection
            v-if="selectedEntry.metadata?.process"
            :selected-entry="selectedEntry"
          />
          <DetailTlsSection v-if="selectedEntry.metadata?.tls" :selected-entry="selectedEntry" />
          <DetailCertificateSection
            v-if="selectedEntry.metadata?.certificate"
            :selected-entry="selectedEntry"
          />
        </section>

        <section
          v-if="isHTTPConnect"
          class="flex min-w-0 flex-col gap-2 pt-1"
          :aria-label="t('detail.raw_tcp_outer_connect_headers')"
        >
          <div class="flex items-center gap-2 px-2 text-sm font-semibold text-app-text">
            <UIcon name="i-lucide-rows-3" class="size-4 text-app-accent" />
            <span>{{ t('detail.raw_tcp_outer_connect_headers') }}</span>
          </div>
          <HeadersTable v-if="hasOuterConnectHeaders" :fields="outerConnectHeaders" />
          <div
            v-else
            class="rounded-(--radius-sm,6px) border border-app-border px-3 py-4 text-center text-sm text-muted"
          >
            {{ t('detail.no_headers') }}
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
