import type * as proxyservice from '../../bindings/github.com/josexy/flowlens/backend/services/proxy_service/models.js'

export interface TrafficResponseHeadersPatchPayload {
  statusCode: number
  status: string
  proto: string
  headerFields: proxyservice.HTTPHeaderField[] | null
  headersTruncated: boolean
  headerOrderUnavailable: boolean
}

export interface TrafficResponseTrailersPatchPayload {
  trailerFields: proxyservice.HTTPHeaderField[] | null
  trailersTruncated: boolean
  trailerOrderUnavailable: boolean
}

export interface TrafficMetricsPatchPayload {
  request?: proxyservice.HTTPMessageMetrics | null
  response?: proxyservice.HTTPMessageMetrics | null
}

export interface TrafficEntryPatchPayload {
  trafficId: number
  revision: number
  responseHeaders?: TrafficResponseHeadersPatchPayload | null
  responseTrailers?: TrafficResponseTrailersPatchPayload | null
  metrics?: TrafficMetricsPatchPayload | null
  process?: proxyservice.ProcessInfo | null
  error?: proxyservice.TrafficError | null
}

export type HTTPMessageSide = 'request' | 'response'

const TERMINAL_HTTP_MESSAGE_STATES = new Set(['completed', 'failed', 'canceled'])

function isTerminalHTTPMessage(
  message: proxyservice.HTTPMessage | null | undefined,
): boolean {
  return TERMINAL_HTTP_MESSAGE_STATES.has(message?.metrics?.state ?? '')
}

export function getNewTerminalHTTPMessageSides(
  previous: proxyservice.TrafficEntry,
  next: proxyservice.TrafficEntry,
): HTTPMessageSide[] {
  const sides: HTTPMessageSide[] = []
  if (!isTerminalHTTPMessage(previous.request) && isTerminalHTTPMessage(next.request)) {
    sides.push('request')
  }
  if (!isTerminalHTTPMessage(previous.response) && isTerminalHTTPMessage(next.response)) {
    sides.push('response')
  }
  return sides
}

export class TerminalBodyRefreshQueue {
  private entryId: number | null = null
  private readonly observedSides = new Set<HTTPMessageSide>()
  private pending = false

  activate(entryId: number) {
    if (this.entryId === entryId) {
      return
    }
    this.entryId = entryId
    this.observedSides.clear()
    this.pending = false
  }

  cancel() {
    this.entryId = null
    this.observedSides.clear()
    this.pending = false
  }

  request(entryId: number, sides: HTTPMessageSide[], bodyViewLoading: boolean): boolean {
    if (entryId !== this.entryId) {
      return false
    }

    let hasNewSide = false
    for (const side of sides) {
      if (this.observedSides.has(side)) {
        continue
      }
      this.observedSides.add(side)
      hasNewSide = true
    }
    if (!hasNewSide) {
      return false
    }
    if (bodyViewLoading) {
      this.pending = true
      return false
    }
    return true
  }

  completeLoad(entryId: number): boolean {
    if (entryId !== this.entryId || !this.pending) {
      return false
    }
    this.pending = false
    return true
  }
}

function emptyHTTPMessage(): proxyservice.HTTPMessage {
  return {
    proto: '',
    headerFields: [],
    trailerFields: [],
  }
}

function applyMessageMetrics(
  message: proxyservice.HTTPMessage | null | undefined,
  metrics: proxyservice.HTTPMessageMetrics | null | undefined,
): proxyservice.HTTPMessage | null | undefined {
  if (metrics === undefined || metrics === null) {
    return message
  }
  return {
    ...(message ?? emptyHTTPMessage()),
    metrics,
  }
}

export function applyTrafficEntryPatch(
  entry: proxyservice.TrafficEntry,
  patch: TrafficEntryPatchPayload,
): proxyservice.TrafficEntry {
  if (
    entry.id !== patch.trafficId ||
    !Number.isSafeInteger(patch.revision) ||
    patch.revision <= (entry.revision ?? 0)
  ) {
    return entry
  }

  let request = entry.request
  let response = entry.response

  if (patch.responseHeaders) {
    response = {
      ...(response ?? emptyHTTPMessage()),
      proto: patch.responseHeaders.proto,
      headerFields: patch.responseHeaders.headerFields,
      headersTruncated: patch.responseHeaders.headersTruncated,
      headerOrderUnavailable: patch.responseHeaders.headerOrderUnavailable,
    }
  }

  if (patch.responseTrailers) {
    response = {
      ...(response ?? emptyHTTPMessage()),
      trailerFields: patch.responseTrailers.trailerFields,
      trailersTruncated: patch.responseTrailers.trailersTruncated,
      trailerOrderUnavailable: patch.responseTrailers.trailerOrderUnavailable,
    }
  }

  if (patch.metrics) {
    request = applyMessageMetrics(request, patch.metrics.request)
    response = applyMessageMetrics(response, patch.metrics.response)
  }

  let metadata = entry.metadata
  if (patch.process !== undefined && patch.process !== null) {
    metadata = {
      ...(metadata ?? ({} as proxyservice.Metadata)),
      process: patch.process,
    }
  }

  return {
    ...entry,
    revision: patch.revision,
    statusCode: patch.responseHeaders?.statusCode ?? entry.statusCode,
    status: patch.responseHeaders?.status ?? entry.status,
    metadata,
    request,
    response,
    error: patch.error ?? entry.error,
  }
}
