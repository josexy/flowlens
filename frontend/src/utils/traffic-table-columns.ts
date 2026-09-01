// Keep in sync with backend/services/setting_service/setting_service.go.
export const TRAFFIC_TABLE_COLUMN_KEYS = [
  'id',
  'method',
  'host',
  'path',
  'process',
  'statusCode',
  'type',
  'destination',
  'protocol',
  'duration',
  'size',
] as const

export type TrafficTableColumnKey = (typeof TRAFFIC_TABLE_COLUMN_KEYS)[number]

export interface TrafficTableColumn {
  key: TrafficTableColumnKey
  title: string
  width: number
  minWidth: number
  isFlex?: boolean
}

const TRAFFIC_TABLE_COLUMN_DEFINITIONS: readonly TrafficTableColumn[] = [
  { key: 'id', title: 'traffic.id', width: 80, minWidth: 60 },
  { key: 'method', title: 'traffic.method', width: 80, minWidth: 70 },
  { key: 'host', title: 'traffic.host', width: 200, minWidth: 120 },
  { key: 'path', title: 'traffic.path', width: 300, minWidth: 120, isFlex: true },
  { key: 'process', title: 'traffic.process', width: 160, minWidth: 120 },
  { key: 'statusCode', title: 'traffic.status', width: 80, minWidth: 70 },
  { key: 'type', title: 'traffic.type', width: 80, minWidth: 70 },
  { key: 'destination', title: 'traffic.destination', width: 150, minWidth: 120 },
  { key: 'protocol', title: 'traffic.protocol', width: 100, minWidth: 80 },
  { key: 'duration', title: 'traffic.duration', width: 100, minWidth: 80 },
  { key: 'size', title: 'traffic.size', width: 100, minWidth: 80 },
]

const TRAFFIC_TABLE_COLUMN_KEY_SET = new Set<string>(TRAFFIC_TABLE_COLUMN_KEYS)

export function createTrafficTableColumns(): TrafficTableColumn[] {
  return TRAFFIC_TABLE_COLUMN_DEFINITIONS.map((column) => ({ ...column }))
}

export function normalizeHiddenTrafficColumnKeys(
  values: readonly string[] | null | undefined,
): TrafficTableColumnKey[] {
  const hidden = new Set<TrafficTableColumnKey>()
  for (const value of values ?? []) {
    const key = value.trim()
    if (TRAFFIC_TABLE_COLUMN_KEY_SET.has(key)) {
      hidden.add(key as TrafficTableColumnKey)
    }
  }
  if (hidden.size === TRAFFIC_TABLE_COLUMN_KEYS.length) {
    hidden.delete('id')
  }
  return TRAFFIC_TABLE_COLUMN_KEYS.filter((key) => hidden.has(key))
}

export function getVisibleTrafficColumns(
  columns: readonly TrafficTableColumn[],
  hiddenValues: readonly string[] | null | undefined,
): TrafficTableColumn[] {
  const hidden = new Set(normalizeHiddenTrafficColumnKeys(hiddenValues))
  return columns.filter((column) => !hidden.has(column.key))
}

export function reorderVisibleTrafficColumns(
  columns: readonly TrafficTableColumn[],
  hiddenValues: readonly string[] | null | undefined,
  draggedKey: TrafficTableColumnKey,
  targetVisibleIndex: number,
): TrafficTableColumn[] {
  const hidden = new Set(normalizeHiddenTrafficColumnKeys(hiddenValues))
  const visible = columns.filter((column) => !hidden.has(column.key))
  const draggedIndex = visible.findIndex((column) => column.key === draggedKey)
  if (
    draggedIndex < 0 ||
    targetVisibleIndex < 0 ||
    targetVisibleIndex >= visible.length ||
    draggedIndex === targetVisibleIndex
  ) {
    return [...columns]
  }

  const reorderedVisible = [...visible]
  const [dragged] = reorderedVisible.splice(draggedIndex, 1)
  if (!dragged) {
    return [...columns]
  }
  reorderedVisible.splice(targetVisibleIndex, 0, dragged)

  let visibleIndex = 0
  return columns.map((column) => {
    if (hidden.has(column.key)) {
      return column
    }
    return reorderedVisible[visibleIndex++]!
  })
}
