<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import MemStatsChart from '@/components/memstats/MemStatsChart.vue'
import { useMemStatsStore } from '@/stores/memStats'
import { formatFileSize } from '@/utils/format'

const { t } = useI18n()
const store = useMemStatsStore()

type MetricItem = {
  label: string
  value: string | number
}

// ── Interval selector ────────────────────────────────────────────────────────

const intervalOptions = [
  { label: '500ms', value: 500 },
  { label: '1s', value: 1000 },
  { label: '2s', value: 2000 },
  { label: '5s', value: 5000 },
  { label: '10s', value: 10000 },
]

// Keep interval selector in sync with store; restart monitoring if active.
watch(
  () => store.intervalMs,
  (ms) => {
    if (store.isMonitoring) {
      store.startMonitoring(ms)
    }
  },
)

async function handleStartStop() {
  if (store.isMonitoring) {
    await store.stopMonitoring()
  } else {
    await store.startMonitoring(store.intervalMs)
  }
}

// ── Format helpers ────────────────────────────────────────────────────────────

const formatBytes = (bytes: number | undefined): string =>
  formatFileSize(bytes, { precision: 2, trimTrailingZeros: false, unknownValue: '-' })

function formatNs(ns: number | undefined): string {
  if (!ns) return '0'
  const n = Number(ns)
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(3)} ms`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)} μs`
  return `${n} ns`
}

function formatPct(f: number | undefined): string {
  if (f === undefined || f === null) return '-'
  return `${(Number(f) * 100).toFixed(4)}%`
}

function formatTime(ms: number | undefined): string {
  if (!ms) return t('memstats.no_gc')
  return new Date(ms).toLocaleString()
}

// ── Summary cards ─────────────────────────────────────────────────────────────

const snap = computed(() => store.latestSnapshot)

const summaryCards = computed(() => [
  {
    key: 'heapAlloc',
    label: t('memstats.heap_alloc'),
    value: formatBytes(snap.value?.heapAlloc),
    trend: '',
  },
  {
    key: 'sys',
    label: t('memstats.sys'),
    value: formatBytes(snap.value?.sys),
    trend: '',
  },
  {
    key: 'numGC',
    label: t('memstats.num_gc'),
    value: snap.value?.numGC ?? '-',
    trend: '',
  },
  {
    key: 'goroutines',
    label: t('memstats.num_goroutine'),
    value: snap.value?.numGoroutine ?? '-',
    trend: '',
  },
])

const heapItems = computed<MetricItem[]>(() => [
  { label: t('memstats.heap_alloc_desc'), value: formatBytes(snap.value?.heapAlloc) },
  { label: t('memstats.heap_sys'), value: formatBytes(snap.value?.heapSys) },
  { label: t('memstats.heap_inuse'), value: formatBytes(snap.value?.heapInuse) },
  { label: t('memstats.heap_idle'), value: formatBytes(snap.value?.heapIdle) },
  { label: t('memstats.heap_released'), value: formatBytes(snap.value?.heapReleased) },
  { label: t('memstats.heap_objects'), value: snap.value?.heapObjects ?? '-' },
])

const stackItems = computed<MetricItem[]>(() => [
  { label: t('memstats.stack_inuse'), value: formatBytes(snap.value?.stackInuse) },
  { label: t('memstats.stack_sys'), value: formatBytes(snap.value?.stackSys) },
])

const gcItems = computed<MetricItem[]>(() => [
  { label: t('memstats.gc_count'), value: snap.value?.numGC ?? '-' },
  { label: t('memstats.forced_gc_count'), value: snap.value?.numForcedGC ?? '-' },
  { label: t('memstats.gc_cpu'), value: formatPct(snap.value?.gcCPUFraction) },
  { label: t('memstats.pause_total'), value: formatNs(snap.value?.pauseTotalNs) },
  { label: t('memstats.pause_last'), value: formatNs(snap.value?.pauseNs) },
  { label: t('memstats.next_gc'), value: formatBytes(snap.value?.nextGC) },
  { label: t('memstats.last_gc_time'), value: formatTime(snap.value?.lastGC) },
])

const overallItems = computed<MetricItem[]>(() => [
  { label: t('memstats.total_alloc'), value: formatBytes(snap.value?.totalAlloc) },
  { label: t('memstats.mallocs'), value: snap.value?.mallocs ?? '-' },
  { label: t('memstats.frees'), value: snap.value?.frees ?? '-' },
  {
    label: t('memstats.live_objects'),
    value:
      snap.value && snap.value.mallocs !== undefined
        ? (snap.value.mallocs - snap.value.frees).toLocaleString()
        : '-',
  },
])

const runtimeItems = computed<MetricItem[]>(() => [
  { label: t('memstats.go_version'), value: snap.value?.goVersion ?? '-' },
  {
    label: t('memstats.os_arch'),
    value: snap.value ? `${snap.value.goos} / ${snap.value.goarch}` : '-',
  },
  { label: t('memstats.num_cpu'), value: snap.value?.numCPU ?? '-' },
  { label: t('memstats.num_goroutine'), value: snap.value?.numGoroutine ?? '-' },
])

const metricRowClass = 'grid min-w-0 grid-cols-[minmax(120px,38%)_minmax(0,1fr)]'
const metricLabelClass =
  'm-0 min-w-0 bg-app-elevated px-2.25 py-1.75 text-xs leading-[1.35] text-app-text-muted'
const metricValueClass =
  'm-0 min-w-0 px-2.25 py-1.75 text-xs leading-[1.35] text-app-text tabular-nums wrap-anywhere'
</script>

<template>
  <div class="flex h-full w-full flex-col overflow-hidden">
    <!-- ── Header ─────────────────────────────────────────────────────────── -->
    <div
      class="flex shrink-0 flex-wrap items-center justify-between gap-2 bg-app-panel px-3.5 py-2.5 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div class="flex min-w-0 items-center gap-2">
        <span class="whitespace-nowrap text-[0.95rem] font-semibold text-app-text">{{
          t('memstats.title')
        }}</span>
        <UBadge
          color="info"
          variant="subtle"
          class="shrink-0 px-2 py-0.5 text-sm font-semibold"
        >
          {{ t('memstats.subtitle') }}
        </UBadge>
      </div>
      <div class="flex min-w-0 flex-[0_1_auto] flex-nowrap items-center justify-end gap-2">
        <UButton
          class="shrink-0"
          :color="store.isMonitoring ? 'warning' : 'primary'"
          size="sm"
          :icon="store.isMonitoring ? 'i-lucide-pause' : 'i-lucide-play'"
          :label="store.isMonitoring ? t('memstats.stop') : t('memstats.start')"
          @click="handleStartStop"
        />
        <USelect
          v-model="store.intervalMs"
          :items="intervalOptions"
          size="sm"
          class="w-27.5 flex-[0_0_90px]"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          icon="i-lucide-refresh-cw"
          :label="t('memstats.refresh')"
          @click="store.fetchOnce()"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          icon="i-lucide-trash-2"
          :label="t('memstats.clear')"
          @click="store.clearHistory()"
        />
      </div>
    </div>

    <div
      class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3"
    >
      <!-- ── Summary cards ────────────────────────────────────────────────── -->
      <div class="grid shrink-0 grid-cols-2 gap-3 sm:grid-cols-4">
        <section
          v-for="card in summaryCards"
          :key="card.key"
          class="min-w-0 rounded-lg border border-app-border bg-app-panel p-3 text-center shadow-(--app-panel-shadow)"
        >
          <span class="mb-1.5 block text-sm text-app-text-muted">{{ card.label }}</span>
          <span class="block text-[1.1rem] font-semibold tabular-nums text-app-accent">{{
            card.value
          }}</span>
        </section>
      </div>

      <!-- ── Chart ────────────────────────────────────────────────────────── -->
      <section
        class="min-w-0 shrink-0 rounded-lg border border-app-border bg-app-panel p-3 shadow-(--app-panel-shadow)"
      >
        <h3 class="mb-3 text-sm font-semibold text-app-text">
          {{ t('memstats.chart_title') }}
        </h3>
        <UEmpty
          v-if="store.snapshots.length === 0"
          icon="i-lucide-chart-line"
          :title="t('memstats.no_data')"
          class="min-h-30"
          :size="appEmptyStateSize"
          variant="naked"
          :ui="appEmptyStateUi"
        />
        <div v-else class="h-55 min-h-55 w-full min-w-0">
          <MemStatsChart :snapshots="store.snapshots" class="h-full w-full min-w-0" />
        </div>
      </section>

      <!-- ── Detailed metrics ──────────────────────────────────────────────── -->
      <div class="grid grid-cols-[minmax(0,1fr)] gap-3 lg:grid-cols-2">
        <!-- Heap + Stack -->
        <section
          class="min-w-0 rounded-lg border border-app-border bg-app-panel p-3 shadow-(--app-panel-shadow)"
        >
          <h3 class="mb-3 text-sm font-semibold text-app-text">
            {{ t('memstats.section_heap') }}
          </h3>
          <dl class="grid overflow-hidden rounded-md border border-app-border">
            <div
              v-for="(item, index) in heapItems"
              :key="item.label"
              :class="[metricRowClass, index === 0 ? '' : 'border-t border-app-border']"
            >
              <dt :class="metricLabelClass">{{ item.label }}</dt>
              <dd :class="metricValueClass">{{ item.value }}</dd>
            </div>
          </dl>

          <div
            class="my-3 flex items-center gap-2 text-sm font-semibold text-app-text-muted after:h-px after:flex-1 after:bg-app-border"
          >
            <span>{{ t('memstats.section_stack') }}</span>
          </div>

          <dl class="grid overflow-hidden rounded-md border border-app-border">
            <div
              v-for="(item, index) in stackItems"
              :key="item.label"
              :class="[metricRowClass, index === 0 ? '' : 'border-t border-app-border']"
            >
              <dt :class="metricLabelClass">{{ item.label }}</dt>
              <dd :class="metricValueClass">{{ item.value }}</dd>
            </div>
          </dl>
        </section>

        <!-- GC + Runtime -->
        <section
          class="min-w-0 rounded-lg border border-app-border bg-app-panel p-3 shadow-(--app-panel-shadow)"
        >
          <h3 class="mb-3 text-sm font-semibold text-app-text">
            {{ t('memstats.section_gc') }}
          </h3>
          <dl class="grid overflow-hidden rounded-md border border-app-border">
            <div
              v-for="(item, index) in gcItems"
              :key="item.label"
              :class="[metricRowClass, index === 0 ? '' : 'border-t border-app-border']"
            >
              <dt :class="metricLabelClass">{{ item.label }}</dt>
              <dd :class="metricValueClass">{{ item.value }}</dd>
            </div>
          </dl>

          <div
            class="my-3 flex items-center gap-2 text-sm font-semibold text-app-text-muted after:h-px after:flex-1 after:bg-app-border"
          >
            <span>{{ t('memstats.section_overall') }}</span>
          </div>

          <dl class="grid overflow-hidden rounded-md border border-app-border">
            <div
              v-for="(item, index) in overallItems"
              :key="item.label"
              :class="[metricRowClass, index === 0 ? '' : 'border-t border-app-border']"
            >
              <dt :class="metricLabelClass">{{ item.label }}</dt>
              <dd :class="metricValueClass">{{ item.value }}</dd>
            </div>
          </dl>

          <div
            class="my-3 flex items-center gap-2 text-sm font-semibold text-app-text-muted after:h-px after:flex-1 after:bg-app-border"
          >
            <span>{{ t('memstats.section_runtime') }}</span>
          </div>

          <dl class="grid overflow-hidden rounded-md border border-app-border">
            <div
              v-for="(item, index) in runtimeItems"
              :key="item.label"
              :class="[metricRowClass, index === 0 ? '' : 'border-t border-app-border']"
            >
              <dt :class="metricLabelClass">{{ item.label }}</dt>
              <dd :class="metricValueClass">{{ item.value }}</dd>
            </div>
          </dl>
        </section>
      </div>
    </div>
  </div>
</template>
