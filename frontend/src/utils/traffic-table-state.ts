import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { defaultRangeExtractor, type Range } from '@tanstack/vue-virtual'
import type { TrafficTableColumnKey } from './traffic-table-columns.js'
import {
  getTrafficMethodLabel,
  getTrafficPathLabel,
  getTrafficProtocol,
  getTrafficTarget,
  getTrafficTotalDurationMicros,
  getTrafficTotalSizeBytes,
  getTrafficTypeLabel,
} from './traffic.js'

export const TRAFFIC_ROW_HEIGHT = 32
export const TRAFFIC_ROW_OVERSCAN = 6

export function getTrafficVirtualRange(range: Range, viewportHeight: number): number[] {
  // Filtering can shrink the sizer before the browser reports its clamped
  // scroll offset. Include the rows at that new bottom boundary immediately,
  // instead of briefly rendering only the last row and its overscan.
  const maxStartIndex = Math.max(
    0,
    Math.floor((range.count * TRAFFIC_ROW_HEIGHT - viewportHeight) / TRAFFIC_ROW_HEIGHT),
  )
  return defaultRangeExtractor({
    ...range,
    startIndex: Math.min(range.startIndex, maxStartIndex),
  })
}

export interface TrafficTableSort {
  key: TrafficTableColumnKey | null
  order: 'asc' | 'desc' | null
}

export function getTrafficLatestEdge({ key, order }: TrafficTableSort): 'start' | 'end' | null {
  if (!key || !order) return 'end'
  if (key === 'id') return order === 'desc' ? 'start' : 'end'
  // With other sort columns, newly arriving requests can land anywhere.
  return null
}

export function isTrafficAtLatestEdge(
  sort: TrafficTableSort,
  scrollTop: number,
  scrollHeight: number,
  viewportHeight: number,
): boolean {
  if (viewportHeight <= 0) return false
  const edge = getTrafficLatestEdge(sort)
  if (edge === null) return false
  if (edge === 'start') return scrollTop <= 1
  return scrollTop >= Math.max(0, scrollHeight - viewportHeight) - 1
}

type SortValue = string | number | null

function getSortValue(entry: TrafficEntry, key: TrafficTableColumnKey): SortValue {
  switch (key) {
    case 'destination':
      return entry.metadata?.remoteDestinationAddr || ''
    case 'protocol':
      return getTrafficProtocol(entry)
    case 'process':
      return entry.metadata?.process?.displayName ?? ''
    case 'host':
      return getTrafficTarget(entry)
    case 'method':
      return getTrafficMethodLabel(entry)
    case 'path':
      return getTrafficPathLabel(entry)
    case 'type':
      return getTrafficTypeLabel(entry)
    case 'duration':
      return getTrafficTotalDurationMicros(entry)
    case 'size':
      return getTrafficTotalSizeBytes(entry)
    default:
      return entry[key]
  }
}

// Entries are replaced by the traffic store when patched. Cache derived values
// by object identity, and only sort again if membership or a sort value changes.
export function createTrafficTableSorter() {
  let lastKey: TrafficTableColumnKey | null = null
  let lastOrder: TrafficTableSort['order'] = null
  let cachedValues = new WeakMap<TrafficEntry, SortValue>()
  let previousValues = new Map<number, SortValue>()
  let previousSorted: TrafficEntry[] = []

  return (entries: TrafficEntry[], { key, order }: TrafficTableSort): TrafficEntry[] => {
    if (!key || !order) {
      lastKey = null
      lastOrder = null
      cachedValues = new WeakMap()
      previousValues.clear()
      previousSorted = []
      return entries
    }

    if (key !== lastKey) cachedValues = new WeakMap()
    let needsSort = key !== lastKey || order !== lastOrder || entries.length !== previousValues.size
    const values = new Map<number, SortValue>()
    const byId = new Map<number, TrafficEntry>()
    for (const entry of entries) {
      let value = cachedValues.get(entry)
      if (value === undefined) {
        value = getSortValue(entry, key)
        cachedValues.set(entry, value)
      }
      values.set(entry.id, value)
      byId.set(entry.id, entry)
      if (!previousValues.has(entry.id) || previousValues.get(entry.id) !== value) needsSort = true
    }

    const sorted = needsSort
      ? [...entries].sort((a, b) => {
          const valueA = values.get(a.id)!
          const valueB = values.get(b.id)!
          // Incomplete metrics always stay last; ties retain ID order.
          if (valueA === null && valueB === null) return a.id - b.id
          if (valueA === null) return 1
          if (valueB === null) return -1
          const comparison =
            typeof valueA === 'string' && typeof valueB === 'string'
              ? valueA.localeCompare(valueB)
              : valueA < valueB
                ? -1
                : valueA > valueB
                  ? 1
                  : 0
          return comparison === 0 ? a.id - b.id : order === 'asc' ? comparison : -comparison
        })
      : previousSorted.map((entry) => byId.get(entry.id)!)

    lastKey = key
    lastOrder = order
    previousValues = values
    previousSorted = sorted
    return sorted
  }
}

// Only a real store eviction in the same unsorted view may move the viewport.
// Sorting, filtering and ordinary field updates must not trigger compensation.
export function getTrafficEvictionScrollTop(
  previous: readonly { id: number }[],
  next: readonly { id: number }[],
  scrollTop: number,
  viewportHeight: number,
): number | null {
  if (viewportHeight <= 0 || previous.length === 0) return null
  const previousMax = Math.max(0, previous.length * TRAFFIC_ROW_HEIGHT - viewportHeight)
  const nextMax = Math.max(0, next.length * TRAFFIC_ROW_HEIGHT - viewportHeight)
  if (scrollTop >= previousMax - 1) return nextMax

  const index = Math.floor(scrollTop / TRAFFIC_ROW_HEIGHT)
  const anchor = previous[index]
  if (!anchor) return null
  const nextIndex = next.findIndex((entry) => entry.id === anchor.id)
  if (nextIndex === -1) return null
  return Math.min(nextMax, Math.max(0, scrollTop + (nextIndex - index) * TRAFFIC_ROW_HEIGHT))
}
