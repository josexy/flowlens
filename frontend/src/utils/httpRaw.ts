import { requestTargetFromURL } from './format.js'
import type { HeaderField, NullableHeaderFields } from './headers.js'

export const RAW_HTTP_BINARY_BODY = '<binary body>'

interface RawHTTPBodyInput {
  body?: string
  bodyEncoding?: string
}

export interface RawHTTPRequestInput extends RawHTTPBodyInput {
  method: string
  url: string
  host?: string
  protocol: string
  headerFields?: NullableHeaderFields
}

export interface RawHTTPResponseInput extends RawHTTPBodyInput {
  status: string
  statusCode?: number
  protocol: string
  headerFields?: NullableHeaderFields
}

function normalizedHeaderFields(fields: NullableHeaderFields): HeaderField[] {
  return (fields ?? [])
    .filter((field): field is HeaderField => field !== null)
    .map((field) => ({ name: field.name ?? '', value: field.value ?? '' }))
}

function normalizeHTTPProtocol(protocol: string): string {
  const normalized = protocol.trim()
  return /^HTTP\/2(?:\.0)?$/i.test(normalized) ? 'HTTP/2.0' : normalized
}

function isHTTP2Protocol(protocol: string): boolean {
  return normalizeHTTPProtocol(protocol) === 'HTTP/2.0'
}

function firstPseudoHeaderValue(fields: HeaderField[], name: string): string | undefined {
  const normalizedName = name.toLocaleLowerCase()
  return fields.find((field) => field.name.toLocaleLowerCase() === normalizedName)?.value
}

function authorityFormRequestTarget(url: string, fallbackHost = ''): string {
  const trimmedURL = url.trim()
  const fragmentIndex = trimmedURL.indexOf('#')
  const withoutFragment = fragmentIndex >= 0 ? trimmedURL.slice(0, fragmentIndex) : trimmedURL
  const schemeSeparator = withoutFragment.indexOf('://')

  if (schemeSeparator >= 0) {
    const authorityStart = schemeSeparator + 3
    const authorityEnd = withoutFragment.slice(authorityStart).search(/[/?]/)
    return authorityEnd >= 0
      ? withoutFragment.slice(authorityStart, authorityStart + authorityEnd)
      : withoutFragment.slice(authorityStart)
  }

  if (withoutFragment.startsWith('//')) {
    const authority = withoutFragment.slice(2)
    const authorityEnd = authority.search(/[/?]/)
    return authorityEnd >= 0 ? authority.slice(0, authorityEnd) : authority
  }

  if (withoutFragment && !withoutFragment.startsWith('/')) {
    const authorityEnd = withoutFragment.search(/[/?]/)
    return authorityEnd >= 0 ? withoutFragment.slice(0, authorityEnd) : withoutFragment
  }

  return fallbackHost.trim()
}

function requestTarget(method: string, url: string, host = ''): string {
  if (method.toLocaleUpperCase() === 'CONNECT') {
    return authorityFormRequestTarget(url, host) || host.trim() || '/'
  }
  return requestTargetFromURL(url) ?? '/'
}

function transformHTTP2HeaderFields(
  fields: HeaderField[],
  consumedPseudoHeaders: ReadonlySet<string>,
): HeaderField[] {
  const authorityFields: HeaderField[] = []
  const remainingFields: HeaderField[] = []

  for (const field of fields) {
    const normalizedName = field.name.toLocaleLowerCase()
    if (normalizedName === ':authority') {
      authorityFields.push({ name: 'host', value: field.value })
      continue
    }
    if (normalizedName === ':scheme' || consumedPseudoHeaders.has(normalizedName)) {
      continue
    }
    if (field.name.startsWith(':')) {
      remainingFields.push({ name: field.name.slice(1), value: field.value })
      continue
    }
    remainingFields.push(field)
  }

  return [...authorityFields, ...remainingFields]
}

function formatMessage(startLine: string, fields: HeaderField[], body?: string, encoding = '') {
  const headerLines = fields.map((field) => `${field.name}: ${field.value}`)
  const head = [startLine, ...headerLines, '', ''].join('\r\n')
  if (body === undefined) {
    return head
  }
  if (encoding === 'base64') {
    return `${head}${RAW_HTTP_BINARY_BODY}`
  }
  return `${head}${body}`
}

export function formatRawHTTPRequest(input: RawHTTPRequestInput): string {
  const protocol = normalizeHTTPProtocol(input.protocol)
  const capturedFields = normalizedHeaderFields(input.headerFields)
  let method = input.method.trim()
  let target = requestTarget(method, input.url, input.host)
  let displayFields = capturedFields

  if (isHTTP2Protocol(protocol)) {
    method = (firstPseudoHeaderValue(capturedFields, ':method') ?? method).trim()
    const pseudoPath = firstPseudoHeaderValue(capturedFields, ':path')
    const authority = firstPseudoHeaderValue(capturedFields, ':authority') ?? input.host ?? ''
    target = pseudoPath ?? requestTarget(method, input.url, authority)
    displayFields = transformHTTP2HeaderFields(
      capturedFields,
      new Set([':method', ':path']),
    )
  }

  return formatMessage(
    `${method} ${target} ${protocol}`.trim(),
    displayFields,
    input.body,
    input.bodyEncoding,
  )
}

export function formatRawHTTPResponse(input: RawHTTPResponseInput): string {
  const protocol = normalizeHTTPProtocol(input.protocol)
  const capturedFields = normalizedHeaderFields(input.headerFields)
  let status = input.status.trim() || (input.statusCode ? String(input.statusCode) : '')
  let displayFields = capturedFields

  if (isHTTP2Protocol(protocol)) {
    status =
      (firstPseudoHeaderValue(capturedFields, ':status') ?? '').trim() ||
      (input.statusCode ? String(input.statusCode) : status.match(/^\d{3}\b/)?.[0] ?? status)
    displayFields = transformHTTP2HeaderFields(capturedFields, new Set([':status']))
  }

  return formatMessage(
    `${protocol} ${status}`.trim(),
    displayFields,
    input.body,
    input.bodyEncoding,
  )
}
