import {
  getLogicalHTTPRequestStartLineSize,
  getLogicalHTTPResponseStartLineSize,
  sumKnownByteSizes,
  summarizeHTTPMessageSize,
} from './format.js'

export const PROCESS_CATEGORY_UNAVAILABLE_KEY = 'process:unavailable'

const HAR_EXPORTABLE_HBIN_VERSION = 1

export function isHARExportableHistoryFormat(formatVersion: number | null | undefined): boolean {
  return formatVersion === HAR_EXPORTABLE_HBIN_VERSION
}

export interface TrafficProcessLike {
  status?: string
  pid?: number
  displayName?: string
  processName?: string
  executablePath?: string
  appId?: string
  iconKey?: string
}

export interface TrafficHTTPMessageMetricsLike {
  startedAtMicros?: number
  endedAtMicros?: number
  headerSize?: number
  bodySize?: number
}

export interface TrafficHTTPMessageLike {
  proto?: string
  headersTruncated?: boolean
  metrics?: TrafficHTTPMessageMetricsLike | null
}

export type TrafficProcessCategoryKind = 'resolved' | 'unavailable'

export interface TrafficProcessCategory {
  kind: TrafficProcessCategoryKind
  key: string
  label: string
  displayName: string
  processName: string
  executablePath: string
  appId: string
  iconKey: string
}

export interface TrafficEntryLike {
  type: string
  method?: string
  url?: string
  host?: string
  path?: string
  status?: string
  rawTcp?: {
    source?: string
    hostPort?: string
    tls?: boolean
  } | null
  metadata?: {
    tls?: {
      selectedAlpn?: string
    } | null
    process?: TrafficProcessLike | null
  } | null
  request?: TrafficHTTPMessageLike | null
  response?: TrafficHTTPMessageLike | null
}

export interface ParsedHostPort {
  host: string
  port: string
}

export interface TrafficCapabilities {
  canLoadBody: boolean
  canSubscribeLiveDetail: boolean
  canEditRequest: boolean
  canResend: boolean
  canCopyCurl: boolean
  canSaveToCollection: boolean
}

export function advanceTrafficEvictionWatermark(
  currentWatermark: number,
  evictedEntryIds: Iterable<number>,
): number {
  let nextWatermark = Number.isFinite(currentWatermark) ? Math.max(0, currentWatermark) : 0
  for (const id of evictedEntryIds) {
    if (Number.isFinite(id) && id > nextWatermark) {
      nextWatermark = id
    }
  }
  return nextWatermark
}

export function isTrafficEntryEvicted(id: number, evictionWatermark: number): boolean {
  return id > 0 && evictionWatermark > 0 && id <= evictionWatermark
}

export function parseHostPort(address: string): ParsedHostPort {
  const normalizedAddress = address.trim()
  if (!normalizedAddress) {
    return { host: '', port: '' }
  }

  const bracketedIPv6Match = normalizedAddress.match(/^\[([^\]]+)\](?::([^:]+))?$/)
  if (bracketedIPv6Match?.[1]) {
    return {
      host: bracketedIPv6Match[1],
      port: bracketedIPv6Match[2] ?? '',
    }
  }

  const lastColonIndex = normalizedAddress.lastIndexOf(':')
  if (
    lastColonIndex > 0 &&
    normalizedAddress.indexOf(':') === lastColonIndex &&
    /^\d+$/.test(normalizedAddress.slice(lastColonIndex + 1))
  ) {
    return {
      host: normalizedAddress.slice(0, lastColonIndex),
      port: normalizedAddress.slice(lastColonIndex + 1),
    }
  }

  return { host: normalizedAddress, port: '' }
}

export function splitHostportToIP(address: string): string {
  return parseHostPort(address).host
}

export function getProtocol(proto: string): string {
  if (proto === 'HTTP/2.0') return 'h2'
  return proto
}

export function isRawTCPTraffic(entry: Pick<TrafficEntryLike, 'type'> | null | undefined) {
  return entry?.type === 'tcp'
}

export function isWebSocketTraffic(entry: Pick<TrafficEntryLike, 'type'> | null | undefined) {
  return entry?.type === 'ws' || entry?.type === 'wss'
}

export function isHTTPTraffic(entry: Pick<TrafficEntryLike, 'type'> | null | undefined) {
  return entry?.type === 'http' || entry?.type === 'https'
}

export function getTrafficTarget(entry: TrafficEntryLike): string {
  if (isRawTCPTraffic(entry)) {
    return entry.rawTcp?.hostPort?.trim() || entry.host?.trim() || ''
  }
  return entry.host?.trim() || ''
}

export function getTrafficCategoryHost(entry: TrafficEntryLike): string {
  const target = getTrafficTarget(entry)
  return isRawTCPTraffic(entry) ? parseHostPort(target).host || target : target
}

function trimProcessField(value: string | undefined): string {
  return value?.trim() ?? ''
}

export function getTrafficProcessCategory(entry: TrafficEntryLike): TrafficProcessCategory {
  const process = entry.metadata?.process
  if (process?.status !== 'resolved') {
    return {
      kind: 'unavailable',
      key: PROCESS_CATEGORY_UNAVAILABLE_KEY,
      label: '',
      displayName: '',
      processName: '',
      executablePath: '',
      appId: '',
      iconKey: '',
    }
  }

  const appId = trimProcessField(process.appId)
  const executablePath = trimProcessField(process.executablePath)
  const processName = trimProcessField(process.processName)
  const displayName = trimProcessField(process.displayName)
  const key = appId
    ? `app:${appId}`
    : executablePath
      ? `exe:${executablePath}`
      : processName
        ? `name:${processName}`
        : ''
  const label = displayName || processName

  if (!key || !label) {
    return {
      kind: 'unavailable',
      key: PROCESS_CATEGORY_UNAVAILABLE_KEY,
      label: '',
      displayName: '',
      processName: '',
      executablePath: '',
      appId: '',
      iconKey: '',
    }
  }

  return {
    kind: 'resolved',
    key,
    label,
    displayName,
    processName,
    executablePath,
    appId,
    iconKey: trimProcessField(process.iconKey),
  }
}

export function trafficMatchesCategoryFilters(
  entry: TrafficEntryLike,
  selectedHosts: ReadonlySet<string>,
  selectedProcessKeys: ReadonlySet<string>,
): boolean {
  if (selectedHosts.size > 0 && !selectedHosts.has(getTrafficCategoryHost(entry))) {
    return false
  }
  if (selectedProcessKeys.size === 0) {
    return true
  }
  return selectedProcessKeys.has(getTrafficProcessCategory(entry).key)
}

export function getTrafficTargetPort(entry: TrafficEntryLike): string {
  if (!isRawTCPTraffic(entry)) return ''
  return parseHostPort(getTrafficTarget(entry)).port
}

export function getTrafficProtocol(entry: TrafficEntryLike): string {
  if (isRawTCPTraffic(entry)) {
    return entry.metadata?.tls?.selectedAlpn?.trim() || (entry.rawTcp?.tls ? 'TLS' : 'TCP')
  }
  return getProtocol(entry.response?.proto || entry.request?.proto || '')
}

export function getTrafficTypeLabel(entry: TrafficEntryLike): string {
  if (isRawTCPTraffic(entry)) {
    return entry.rawTcp?.tls ? 'TCP/TLS' : 'TCP'
  }
  return entry.type.toUpperCase()
}

export function getTrafficMethodLabel(entry: TrafficEntryLike): string {
  return entry.method?.trim() || '—'
}

export function getTrafficPathLabel(entry: TrafficEntryLike): string {
  return isRawTCPTraffic(entry) ? '—' : entry.path?.trim() || '—'
}

export function getTrafficTotalDurationMicros(entry: TrafficEntryLike): number | null {
  const startedAtMicros = entry.request?.metrics?.startedAtMicros
  const endedAtMicros = entry.response?.metrics?.endedAtMicros
  if (
    startedAtMicros === undefined ||
    endedAtMicros === undefined ||
    !Number.isSafeInteger(startedAtMicros) ||
    !Number.isSafeInteger(endedAtMicros) ||
    startedAtMicros < 0 ||
    endedAtMicros < startedAtMicros
  ) {
    return null
  }
  return endedAtMicros - startedAtMicros
}

export function getTrafficTotalSizeBytes(entry: TrafficEntryLike): number | null {
  const request = summarizeHTTPMessageSize(
    entry.request?.metrics?.headerSize,
    entry.request?.metrics?.bodySize,
    entry.request?.headersTruncated,
    getLogicalHTTPRequestStartLineSize(
      entry.method ?? '',
      entry.url ?? '',
      entry.request?.proto ?? '',
    ),
  )
  const response = summarizeHTTPMessageSize(
    entry.response?.metrics?.headerSize,
    entry.response?.metrics?.bodySize,
    entry.response?.headersTruncated,
    getLogicalHTTPResponseStartLineSize(entry.status ?? '', entry.response?.proto ?? ''),
  )
  return sumKnownByteSizes(request.total, response.total)
}

export function trafficMatchesSearch(entry: TrafficEntryLike, query: string): boolean {
  const normalizedQuery = query.trim().toLowerCase()
  if (!normalizedQuery) return true

  const values = [entry.url, entry.host, entry.path]
  if (isRawTCPTraffic(entry)) {
    const target = getTrafficTarget(entry)
    const parsedTarget = parseHostPort(target)
    values.push(target, parsedTarget.host, parsedTarget.port, entry.rawTcp?.source)
  }

  return values.some((value) => value?.toLowerCase().includes(normalizedQuery))
}

export function getTrafficCapabilities(
  entry: Pick<TrafficEntryLike, 'type'> | null | undefined,
): TrafficCapabilities {
  if (isRawTCPTraffic(entry)) {
    return {
      canLoadBody: false,
      canSubscribeLiveDetail: false,
      canEditRequest: false,
      canResend: false,
      canCopyCurl: false,
      canSaveToCollection: false,
    }
  }

  const isHTTP = isHTTPTraffic(entry)
  const isWebSocket = isWebSocketTraffic(entry)
  return {
    canLoadBody: isHTTP || isWebSocket,
    canSubscribeLiveDetail: isHTTP || isWebSocket,
    canEditRequest: isHTTP || isWebSocket,
    canResend: isHTTP,
    canCopyCurl: isHTTP,
    canSaveToCollection: isHTTP || isWebSocket,
  }
}
