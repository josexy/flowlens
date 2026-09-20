import type { ContextMenuItem } from '@nuxt/ui'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { firstHeaderFieldValue } from './headers.js'

export const TRAFFIC_EXPORT_GROUPS = [
  ['request', 'request-headers', 'request-body'],
  ['response', 'response-headers', 'response-body'],
  ['request-response'],
  ['csv', 'har'],
] as const

export type TrafficExportFormat = (typeof TRAFFIC_EXPORT_GROUPS)[number][number]

export function canExportTraffic(entry: TrafficEntry, format: TrafficExportFormat): boolean {
  if (format === 'csv') return true
  if (!['http', 'https', 'ws', 'wss'].includes(entry.type)) return false
  if (format === 'har') return true
  const request = format.startsWith('request')
  const response = format.startsWith('response') || format === 'request-response'
  const body = !format.endsWith('-headers')
  if (request && (!entry.request?.proto.trim() || !entry.method.trim())) return false
  if (response && (!entry.response?.proto.trim() || !(entry.statusCode > 0 || entry.status.trim()))) return false
  if (body && request && entry.request?.metrics?.state !== 'completed') return false
  if (body && response && entry.response?.metrics?.state !== 'completed') return false
  return true
}

export function createTrafficExportMenu(
  translate: (key: string) => string,
  select: (format: TrafficExportFormat) => void,
  disabled: boolean,
  entries?: readonly TrafficEntry[],
): ContextMenuItem[] {
  return TRAFFIC_EXPORT_GROUPS.flatMap((group, index) => [
    ...(index ? [{ key: `export-separator-${index}`, type: 'separator' as const }] : []),
    ...group.map((format) => {
      const unavailable = disabled || (entries !== undefined && !entries.some((entry) => canExportTraffic(entry, format)))
      return {
        key: `export-${format}`,
        label: translate(`traffic_export.formats.${format}`),
        disabled: unavailable,
        onSelect: unavailable ? undefined : () => select(format),
      }
    }),
  ])
}

export function trafficExportExtension(format: TrafficExportFormat, entry?: TrafficEntry): string {
  if (format === 'csv' || format === 'har') return `.${format}`
  if (format.endsWith('-headers')) return '.txt'
  if (!format.endsWith('-body')) return '.http'
  const message = format === 'request-body' ? entry?.request : entry?.response
  const contentType = firstHeaderFieldValue(message?.headerFields, 'Content-Type')?.split(';')[0]?.trim().toLowerCase() ?? ''
  if (contentType === 'application/json' || contentType.endsWith('+json')) return '.json'
  if (contentType === 'image/svg+xml') return '.svg'
  if (contentType === 'text/xml' || contentType === 'application/xml' || contentType.endsWith('+xml')) return '.xml'
  const extensions: Record<string, string> = {
    'text/plain': '.txt',
    'text/html': '.html',
    'image/jpeg': '.jpg',
    'image/png': '.png',
    'image/gif': '.gif',
    'image/webp': '.webp',
    'application/pdf': '.pdf',
    'application/zip': '.zip',
  }
  return extensions[contentType] ?? '.bin'
}
