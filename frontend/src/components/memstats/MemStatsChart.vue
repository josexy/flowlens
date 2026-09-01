<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useThemeStore } from '@/stores/theme'
import type { MemSnapshot } from '@/stores/memStats'
import { formatFileSize } from '@/utils/format'

const props = defineProps<{
  snapshots: MemSnapshot[]
}>()

const themeStore = useThemeStore()
const chartRoot = ref<HTMLElement | null>(null)
const plot = shallowRef<uPlot | null>(null)

let resizeObserver: ResizeObserver | null = null

const DEFAULT_SURFACE_LIGHT = '#ffffff'
const DEFAULT_SURFACE_DARK = '#252b34'
const DEFAULT_BORDER_COLOR = 'rgba(32, 42, 35, 0.14)'
const HEAP_SERIES_COLOR = '#10b981'
const HEAP_AREA_FILL = 'rgba(16, 185, 129, 0.11)'
const SYS_SERIES_COLOR = '#2080f0'
const STACK_SERIES_COLOR = '#f0a020'

type ChartThemeColors = {
  surface: string
  borderColor: string
  textColor: string
  mutedTextColor: string
  splitColor: string
}

type TooltipRow = {
  label: string
  color: string
  value: string
}

const seriesMeta = [
  { label: 'HeapAlloc', color: HEAP_SERIES_COLOR },
  { label: 'Sys', color: SYS_SERIES_COLOR },
  { label: 'StackInuse', color: STACK_SERIES_COLOR },
]

const tooltip = ref({
  visible: false,
  x: 0,
  y: 0,
  time: '',
  rows: [] as TooltipRow[],
})

const chartData = computed<uPlot.AlignedData>(() => [
  props.snapshots.map((snapshot) => snapshot.timestamp),
  props.snapshots.map((snapshot) => snapshot.heapAlloc),
  props.snapshots.map((snapshot) => snapshot.sys),
  props.snapshots.map((snapshot) => snapshot.stackInuse),
])

const formatBytes = (bytes: number | undefined): string =>
  formatFileSize(bytes, { precision: 2, trimTrailingZeros: false, unknownValue: '-' })

function readCssVariable(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  return styles.getPropertyValue(name).trim() || fallback
}

function getChartThemeColors(): ChartThemeColors {
  const useDark = themeStore.isDark
  const textColor = useDark ? 'rgba(226, 232, 240, 0.86)' : 'rgba(15, 23, 42, 0.68)'
  const mutedTextColor = useDark ? 'rgba(203, 213, 225, 0.72)' : 'rgba(71, 85, 105, 0.78)'
  const splitColor = useDark ? 'rgba(226, 232, 240, 0.12)' : 'rgba(15, 23, 42, 0.08)'

  if (typeof document === 'undefined') {
    return {
      surface: useDark ? DEFAULT_SURFACE_DARK : DEFAULT_SURFACE_LIGHT,
      borderColor: DEFAULT_BORDER_COLOR,
      textColor,
      mutedTextColor,
      splitColor,
    }
  }

  const styles = getComputedStyle(document.documentElement)

  return {
    surface: readCssVariable(
      styles,
      useDark ? '--ui-bg-elevated' : '--ui-bg',
      useDark ? DEFAULT_SURFACE_DARK : DEFAULT_SURFACE_LIGHT,
    ),
    borderColor: readCssVariable(styles, '--ui-border', DEFAULT_BORDER_COLOR),
    textColor,
    mutedTextColor,
    splitColor,
  }
}

function formatTimeAxis(value: number): string {
  return new Date(value).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function getAppFontFamily(): string {
  if (typeof document === 'undefined') {
    return '"DM Sans", system-ui, sans-serif'
  }

  return getComputedStyle(document.body).fontFamily || '"DM Sans", system-ui, sans-serif'
}

function getPlotSize(): { width: number; height: number } | null {
  const root = chartRoot.value
  if (!root) {
    return null
  }

  const width = Math.floor(root.clientWidth)
  const height = Math.floor(root.clientHeight)

  if (width < 40 || height < 40) {
    return null
  }

  return { width, height }
}

function resetZoom(self: uPlot) {
  const xValues = chartData.value[0]
  const first = xValues[0]
  const last = xValues[xValues.length - 1]

  if (typeof first === 'number' && typeof last === 'number') {
    self.setScale('x', { min: first, max: last })
  }
}

function updateTooltip(self: uPlot) {
  const root = chartRoot.value
  const idx = self.cursor.idx

  if (!root || idx === null || idx === undefined || idx < 0 || idx >= props.snapshots.length) {
    tooltip.value.visible = false
    return
  }

  const cursorLeft = self.cursor.left ?? 0
  const plotLeft = self.over.offsetLeft
  const plotTop = self.over.offsetTop
  const tooltipWidth = 180
  const preferredX = plotLeft + cursorLeft + 12
  const x =
    preferredX + tooltipWidth > root.clientWidth
      ? Math.max(8, plotLeft + cursorLeft - tooltipWidth - 12)
      : preferredX

  const rows = seriesMeta.map((series, seriesIndex) => {
    const value = chartData.value[seriesIndex + 1]?.[idx]
    return {
      label: series.label,
      color: series.color,
      value: typeof value === 'number' ? formatBytes(value) : '-',
    }
  })

  tooltip.value = {
    visible: true,
    x,
    y: plotTop + 8,
    time: new Date(props.snapshots[idx].timestamp).toLocaleTimeString(),
    rows,
  }
}

function createOptions(width: number, height: number): uPlot.Options {
  const colors = getChartThemeColors()
  const axisFont = `11px ${getAppFontFamily()}`

  return {
    width,
    height,
    ms: 1,
    class: 'memstats-uplot',
    legend: {
      show: false,
    },
    scales: {
      x: {
        time: true,
      },
      y: {
        range: (_self, min, max) => {
          if (!Number.isFinite(min) || !Number.isFinite(max)) {
            return [0, 1]
          }
          if (min === max) {
            const pad = Math.max(max * 0.1, 1024)
            return [Math.max(0, min - pad), max + pad]
          }
          return [Math.max(0, min - (max - min) * 0.08), max + (max - min) * 0.12]
        },
      },
    },
    axes: [
      {
        stroke: colors.mutedTextColor,
        font: axisFont,
        size: 32,
        gap: 6,
        space: 72,
        values: (_self, values) => values.map(formatTimeAxis),
        grid: { stroke: colors.splitColor, width: 1 },
        ticks: { stroke: colors.splitColor, width: 1, size: 4 },
        border: { stroke: colors.splitColor, width: 1 },
      },
      {
        stroke: colors.mutedTextColor,
        font: axisFont,
        size: 76,
        gap: 6,
        space: 48,
        values: (_self, values) => values.map((value) => formatBytes(value)),
        grid: { stroke: colors.splitColor, width: 1 },
        ticks: { stroke: colors.splitColor, width: 1, size: 4 },
        border: { stroke: colors.splitColor, width: 1 },
      },
    ],
    cursor: {
      x: true,
      y: false,
      drag: {
        x: true,
        y: false,
        setScale: true,
        dist: 4,
      },
      points: {
        size: 6,
        width: 1.5,
        stroke: colors.surface,
        fill: (_self, seriesIdx) => seriesMeta[seriesIdx - 1]?.color ?? colors.surface,
      },
      bind: {
        dblclick: (self) => (event) => {
          event.preventDefault()
          resetZoom(self)
          return null
        },
        mouseleave: (_self, _target, handler) => (event) => {
          tooltip.value.visible = false
          return handler(event)
        },
      },
    },
    series: [
      {},
      {
        label: seriesMeta[0].label,
        stroke: seriesMeta[0].color,
        fill: HEAP_AREA_FILL,
        width: 2,
        points: { show: false },
      },
      {
        label: seriesMeta[1].label,
        stroke: seriesMeta[1].color,
        width: 2,
        dash: [8, 5],
        points: { show: false },
      },
      {
        label: seriesMeta[2].label,
        stroke: seriesMeta[2].color,
        width: 1.5,
        points: { show: false },
      },
    ],
    hooks: {
      setCursor: [updateTooltip],
      setData: [updateTooltip],
    },
  }
}

function createPlot() {
  const root = chartRoot.value
  const size = getPlotSize()

  if (!root || !size) {
    return
  }

  plot.value?.destroy()
  tooltip.value.visible = false
  plot.value = new uPlot(createOptions(size.width, size.height), chartData.value, root)
}

function resizePlot() {
  const size = getPlotSize()
  const instance = plot.value

  if (!size) {
    return
  }

  if (!instance) {
    createPlot()
    return
  }

  if (instance.width !== size.width || instance.height !== size.height) {
    instance.setSize(size)
  }
}

watch(chartData, (data) => {
  const instance = plot.value
  if (!instance) {
    nextTick(createPlot)
    return
  }

  instance.setData(data, true)
})

watch(
  () => themeStore.isDark,
  () => {
    nextTick(createPlot)
  },
)

onMounted(() => {
  nextTick(() => {
    createPlot()

    if (chartRoot.value) {
      resizeObserver = new ResizeObserver(resizePlot)
      resizeObserver.observe(chartRoot.value)
    }
  })
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
  plot.value?.destroy()
  plot.value = null
})
</script>

<template>
  <div class="relative h-full min-h-0 w-full overflow-hidden">
    <div ref="chartRoot" class="memstats-uplot-host h-full w-full min-w-0" />

    <div class="pointer-events-none absolute right-2 top-1 z-10 flex flex-wrap justify-end gap-x-3 gap-y-1">
      <span
        v-for="series in seriesMeta"
        :key="series.label"
        class="inline-flex items-center gap-1.5 text-xs font-medium text-app-text-muted"
      >
        <span
          class="h-1.5 w-3.5 rounded-full"
          :style="{ backgroundColor: series.color }"
        />
        {{ series.label }}
      </span>
    </div>

    <div
      v-if="tooltip.visible"
      class="pointer-events-none absolute z-20 min-w-45 rounded-md border border-app-border bg-app-panel px-2.5 py-2 text-xs shadow-(--app-panel-shadow)"
      :style="{ transform: `translate(${tooltip.x}px, ${tooltip.y}px)` }"
    >
      <div class="mb-1 text-app-text-muted">{{ tooltip.time }}</div>
      <div
        v-for="row in tooltip.rows"
        :key="row.label"
        class="flex items-center justify-between gap-4 leading-5"
      >
        <span class="inline-flex items-center gap-1.5 text-app-text-muted">
          <span class="size-2 rounded-full" :style="{ backgroundColor: row.color }" />
          {{ row.label }}
        </span>
        <span class="font-semibold tabular-nums text-app-text">{{ row.value }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.memstats-uplot-host :deep(.uplot) {
  width: 100%;
  height: 100%;
  background: transparent;
  font-family: var(--app-font-family, system-ui, sans-serif);
}

.memstats-uplot-host :deep(.u-wrap) {
  width: 100%;
  height: 100%;
}

.memstats-uplot-host :deep(.u-over) {
  cursor: crosshair;
}

.memstats-uplot-host :deep(.u-select) {
  border: 1px solid rgba(32, 128, 240, 0.35);
  background: rgba(32, 128, 240, 0.12);
}

.memstats-uplot-host :deep(.u-cursor-x) {
  border-right-color: rgba(100, 116, 139, 0.55);
}
</style>
