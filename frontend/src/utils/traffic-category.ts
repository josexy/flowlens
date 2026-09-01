import type { TrafficEntry } from '../../bindings/github.com/josexy/flowlens/backend/services/proxy_service/models.js'
import type {
  HostSummaryItem,
  ProcessSummaryItem,
  StructureTreeBuildResult,
  StructureTreeNode,
} from '../types/traffic-category.js'
import {
  PROCESS_CATEGORY_UNAVAILABLE_KEY,
  getTrafficCategoryHost,
  getTrafficProcessCategory,
  getTrafficTarget,
  getTrafficTargetPort,
  isRawTCPTraffic,
  trafficMatchesCategoryFilters,
} from './traffic.js'
import { firstHeaderFieldValue } from './headers.js'

const EMPTY_CATEGORY_SELECTION: ReadonlySet<string> = new Set<string>()

function getPathSegments(entry: TrafficEntry): string[] {
  if (isRawTCPTraffic(entry)) {
    const tunnelType = entry.rawTcp?.tls ? 'TCP-TLS' : 'TCP'
    return [tunnelType, getTrafficTargetPort(entry) || getTrafficTarget(entry) || '—']
  }

  const rawPath = (() => {
    try {
      const parsed = new URL(entry.url)
      return parsed.pathname || entry.path || ''
    } catch {
      return entry.path || ''
    }
  })()

  const trimmed = rawPath.trim()
  if (!trimmed || trimmed === '/') {
    return ['/']
  }

  const segments = trimmed
    .split('/')
    .map((segment) => segment.trim())
    .filter((segment) => segment.length > 0)

  return segments.length > 0 ? segments : ['/']
}

function getResponseContentType(entry: TrafficEntry) {
  return firstHeaderFieldValue(entry.response?.headerFields, 'Content-Type')
}

export function buildHostSummary(entries: TrafficEntry[]): HostSummaryItem[] {
  const counts = new Map<string, number>()
  for (const entry of entries) {
    const host = getTrafficCategoryHost(entry)
    if (!host) continue
    counts.set(host, (counts.get(host) ?? 0) + 1)
  }

  return [...counts.entries()]
    .map(([host, count]) => ({ host, count }))
    .sort((a, b) => a.host.localeCompare(b.host))
}

export function buildProcessSummary(
  entries: TrafficEntry[],
  unavailableLabel: string,
  keepUnavailable = false,
): ProcessSummaryItem[] {
  const summaries = new Map<string, ProcessSummaryItem>()
  let unavailableCount = 0

  for (const entry of entries) {
    const process = getTrafficProcessCategory(entry)
    if (process.kind === 'unavailable') {
      unavailableCount++
      continue
    }

    const existing = summaries.get(process.key)
    if (!existing) {
      summaries.set(process.key, {
        kind: 'resolved',
        processKey: process.key,
        label: process.label,
        displayName: process.displayName,
        processName: process.processName,
        iconKey: process.iconKey,
        count: 1,
      })
      continue
    }

    existing.count++
    if (!existing.displayName && process.displayName) {
      existing.displayName = process.displayName
      existing.label = process.displayName
    }
    if (!existing.processName && process.processName) {
      existing.processName = process.processName
      if (!existing.displayName) {
        existing.label = process.processName
      }
    }
    if (!existing.iconKey && process.iconKey) {
      existing.iconKey = process.iconKey
    }
  }

  const result = [...summaries.values()].sort(compareProcessSummaryItems)

  if (unavailableCount > 0 || keepUnavailable) {
    result.push({
      kind: 'unavailable',
      processKey: PROCESS_CATEGORY_UNAVAILABLE_KEY,
      label: unavailableLabel.trim(),
      displayName: '',
      processName: '',
      iconKey: '',
      count: unavailableCount,
    })
  }

  return result
}

function compareProcessSummaryItems(
  left: ProcessSummaryItem,
  right: ProcessSummaryItem,
): number {
  if (left.kind !== right.kind) {
    return left.kind === 'unavailable' ? 1 : -1
  }
  return (
    left.label.localeCompare(right.label) ||
    left.processKey.localeCompare(right.processKey)
  )
}

function retainSelectedHosts(
  visibleItems: HostSummaryItem[],
  allItems: HostSummaryItem[],
  selectedHosts: ReadonlySet<string>,
): HostSummaryItem[] {
  const result = [...visibleItems]
  const visibleHosts = new Set(visibleItems.map((item) => item.host))
  const allByHost = new Map(allItems.map((item) => [item.host, item]))

  for (const host of selectedHosts) {
    if (visibleHosts.has(host)) continue
    result.push({
      ...(allByHost.get(host) ?? { host, count: 0 }),
      count: 0,
    })
  }

  return result.sort((left, right) => left.host.localeCompare(right.host))
}

function retainSelectedProcesses(
  visibleItems: ProcessSummaryItem[],
  allItems: ProcessSummaryItem[],
  selectedProcessKeys: ReadonlySet<string>,
): ProcessSummaryItem[] {
  const result = [...visibleItems]
  const visibleKeys = new Set(visibleItems.map((item) => item.processKey))
  const allByKey = new Map(allItems.map((item) => [item.processKey, item]))

  for (const processKey of selectedProcessKeys) {
    if (visibleKeys.has(processKey)) continue
    const source = allByKey.get(processKey)
    if (source) {
      result.push({ ...source, count: 0 })
    }
  }

  return result.sort(compareProcessSummaryItems)
}

export function buildCategoryFacetSummaries(
  entries: TrafficEntry[],
  selectedHosts: ReadonlySet<string>,
  selectedProcessKeys: ReadonlySet<string>,
  unavailableLabel: string,
): { hosts: HostSummaryItem[]; processes: ProcessSummaryItem[] } {
  const hostEntries =
    selectedProcessKeys.size === 0
      ? entries
      : entries.filter((entry) =>
          trafficMatchesCategoryFilters(
            entry,
            EMPTY_CATEGORY_SELECTION,
            selectedProcessKeys,
          ),
        )
  const processEntries =
    selectedHosts.size === 0
      ? entries
      : entries.filter((entry) =>
          trafficMatchesCategoryFilters(
            entry,
            selectedHosts,
            EMPTY_CATEGORY_SELECTION,
          ),
        )
  const keepUnavailable = selectedProcessKeys.has(PROCESS_CATEGORY_UNAVAILABLE_KEY)
  let hosts = buildHostSummary(hostEntries)
  let processes = buildProcessSummary(processEntries, unavailableLabel, keepUnavailable)

  if (selectedHosts.size > 0 && selectedProcessKeys.size > 0) {
    hosts = retainSelectedHosts(hosts, buildHostSummary(entries), selectedHosts)
    processes = retainSelectedProcesses(
      processes,
      buildProcessSummary(entries, unavailableLabel, keepUnavailable),
      selectedProcessKeys,
    )
  }

  return { hosts, processes }
}

export function buildStructureTree(entries: TrafficEntry[]): StructureTreeBuildResult {
  const hosts = new Map<string, StructureTreeNode>()
  const nodeMap = new Map<string, StructureTreeNode>()
  const childMaps = new WeakMap<StructureTreeNode, Map<string, StructureTreeNode>>()

  const registerNode = (node: StructureTreeNode) => {
    nodeMap.set(node.key, node)
    return node
  }

  const getChildMap = (node: StructureTreeNode) => {
    let childMap = childMaps.get(node)
    if (!childMap) {
      childMap = new Map<string, StructureTreeNode>()
      childMaps.set(node, childMap)
    }
    return childMap
  }

  for (const entry of entries) {
    const host = getTrafficCategoryHost(entry)
    if (!host) continue

    const trafficKind = isRawTCPTraffic(entry) ? 'raw-tcp' : 'http'

    let hostNode = hosts.get(host)
    if (!hostNode) {
      hostNode = registerNode({
        key: `host:${host}`,
        label: host,
        type: 'host',
        host,
        depth: 0,
        entryIds: [],
        children: [],
      })
      hosts.set(host, hostNode)
    }

    hostNode.entryIds.push(entry.id)
    let current = hostNode
    const segments = getPathSegments(entry)

    segments.forEach((segment, index) => {
      const isLeaf = index === segments.length - 1
      const segmentIdentity = `${trafficKind}:${segment}`
      const key = `${current.key}/${segmentIdentity}:${entry.id}:${index}`
      const type = isLeaf ? 'leaf' : 'segment'

      const childMap = getChildMap(current)
      let nextNode = isLeaf ? undefined : childMap.get(segmentIdentity)

      if (!nextNode) {
        nextNode = registerNode({
          key,
          label: segment,
          type,
          host,
          depth: current.depth + 1,
          entryIds: [],
          contentType: isLeaf ? getResponseContentType(entry) : undefined,
          trafficKind,
          children: [],
        })
        current.children.push(nextNode)
        if (!isLeaf) {
          childMap.set(segmentIdentity, nextNode)
        }
      }

      nextNode.entryIds.push(entry.id)
      current = nextNode
    })
  }

  const sortNodes = (nodes: StructureTreeNode[]) => {
    nodes.sort((a, b) => a.label.localeCompare(b.label))
    for (const node of nodes) {
      sortNodes(node.children)
    }
  }

  const rootNodes = [...hosts.values()].sort((a, b) => a.label.localeCompare(b.label))
  for (const node of rootNodes) {
    sortNodes(node.children)
  }
  return {
    roots: rootNodes,
    nodeMap,
  }
}

export function filterHostSummary(items: HostSummaryItem[], query: string): HostSummaryItem[] {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return items
  return items.filter((item) => item.host.toLowerCase().includes(normalized))
}

export function filterProcessSummary(
  items: ProcessSummaryItem[],
  query: string,
): ProcessSummaryItem[] {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return items
  return items.filter((item) =>
    [item.label, item.displayName, item.processName].some((value) =>
      value.toLowerCase().includes(normalized),
    ),
  )
}

export function filterStructureRootsByHost(
  roots: StructureTreeNode[],
  query: string,
): StructureTreeNode[] {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return roots
  return roots.filter((node) => node.host.toLowerCase().includes(normalized))
}
