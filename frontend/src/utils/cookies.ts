import type { EditableKeyValue } from '../types/request-editor.js'
import type { HeaderField } from './headers.js'

export type NullableCookieHeadersRecord =
  | Record<string, (string | null | undefined)[] | null | undefined>
  | null
  | undefined
export type NullableCookieHeaders =
  | NullableCookieHeadersRecord
  | (HeaderField | null)[]
export type CookieHeaderName = 'cookie' | 'set-cookie'

const REQUEST_COOKIE_HEADER = 'cookie'
const RESPONSE_COOKIE_HEADER = 'set-cookie'

function isHeaderName(name: string, expectedName: string): boolean {
  return name.trim().toLowerCase() === expectedName
}

function appendRecordValue(record: Record<string, string[]>, key: string, value: string) {
  if (!Object.hasOwn(record, key)) {
    record[key] = []
  }
  record[key]!.push(value)
}

function forEachHeader(
  headers: NullableCookieHeaders,
  visit: (name: string, values: (string | null | undefined)[]) => void,
) {
  if (Array.isArray(headers)) {
    for (const field of headers) {
      if (field !== null) {
        visit(field.name ?? '', [field.value])
      }
    }
    return
  }
  for (const [name, values] of Object.entries(headers ?? {})) {
    visit(name, Array.isArray(values) ? values : [])
  }
}

function splitCookieField(rawField: string): { key: string; value: string; hasEquals: boolean } {
  const field = rawField.trim()
  const separatorIndex = field.indexOf('=')
  if (separatorIndex < 0) {
    return {
      key: field,
      value: '',
      hasEquals: false,
    }
  }

  return {
    key: field.slice(0, separatorIndex).trim(),
    value: field.slice(separatorIndex + 1).trim(),
    hasEquals: true,
  }
}

function parseRequestCookieValue(value: string, enabled: boolean): EditableKeyValue[] {
  const rows: EditableKeyValue[] = []
  for (const rawField of value.split(';')) {
    if (!rawField.trim()) {
      continue
    }
    const field = splitCookieField(rawField)
    rows.push({
      key: field.key,
      value: field.value,
      enabled,
    })
  }
  return rows
}

export function createEmptyCookieRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

export function hasHeader(
  headers: NullableCookieHeaders,
  expectedName: CookieHeaderName,
): boolean {
  let found = false
  forEachHeader(headers, (name) => {
    found ||= isHeaderName(name, expectedName)
  })
  return found
}

export function collectHeaderValues(
  headers: NullableCookieHeaders,
  expectedName: CookieHeaderName,
): string[] {
  const values: string[] = []

  forEachHeader(headers, (name, headerValues) => {
    if (!isHeaderName(name, expectedName)) {
      return
    }
    for (const value of headerValues) {
      values.push(value ?? '')
    }
  })
  return values
}

export function cookieHeadersRecord(
  headers: NullableCookieHeaders,
  expectedName: CookieHeaderName,
): Record<string, string[]> {
  const record: Record<string, string[]> = Object.create(null)
  forEachHeader(headers, (name, headerValues) => {
    if (!isHeaderName(name, expectedName)) {
      return
    }
    if (headerValues.length === 0) {
      appendRecordValue(record, name, '')
      return
    }
    for (const value of headerValues) {
      appendRecordValue(record, name, value ?? '')
    }
  })
  return record
}

export function requestCookieHeadersRecord(headers: EditableKeyValue[]): Record<string, string[]> {
  const record: Record<string, string[]> = Object.create(null)
  for (const header of headers) {
    if (!header.enabled || !isHeaderName(header.key, REQUEST_COOKIE_HEADER)) {
      continue
    }
    appendRecordValue(record, header.key.trim() || 'Cookie', header.value ?? '')
  }
  return record
}

export function requestCookiesRecord(
  headers: NullableCookieHeaders,
): Record<string, string[]> {
  const record: Record<string, string[]> = Object.create(null)
  for (const value of collectHeaderValues(headers, REQUEST_COOKIE_HEADER)) {
    for (const cookie of parseRequestCookieValue(value, true)) {
      appendRecordValue(record, cookie.key, cookie.value)
    }
  }
  return record
}

function createUniqueCookiePrefix(baseName: string, usedPrefixes: Set<string>): string {
  const base = baseName || 'Cookie'
  if (!usedPrefixes.has(base)) {
    usedPrefixes.add(base)
    return base
  }

  let suffix = 2
  let candidate = `${base}#${suffix}`
  while (usedPrefixes.has(candidate)) {
    suffix += 1
    candidate = `${base}#${suffix}`
  }
  usedPrefixes.add(candidate)
  return candidate
}

export function responseCookiesRecord(
  headers: NullableCookieHeaders,
): Record<string, string[]> {
  const record: Record<string, string[]> = Object.create(null)
  const usedPrefixes = new Set<string>()

  for (const headerValue of collectHeaderValues(headers, RESPONSE_COOKIE_HEADER)) {
    const fields = headerValue.split(';')
    const cookie = splitCookieField(fields.shift() ?? '')
    const prefix = createUniqueCookiePrefix(cookie.key, usedPrefixes)
    appendRecordValue(record, `${prefix}.Value`, cookie.value)

    for (const rawAttribute of fields) {
      if (!rawAttribute.trim()) {
        continue
      }
      const attribute = splitCookieField(rawAttribute)
      if (!attribute.key) {
        continue
      }
      appendRecordValue(
        record,
        `${prefix}.${attribute.key}`,
        attribute.hasEquals ? attribute.value : 'true',
      )
    }
  }

  return record
}

export function requestCookieRows(headers: EditableKeyValue[]): EditableKeyValue[] {
  const rows: EditableKeyValue[] = []
  for (const header of headers) {
    if (!isHeaderName(header.key, REQUEST_COOKIE_HEADER)) {
      continue
    }
    rows.push(...parseRequestCookieValue(header.value ?? '', header.enabled))
  }
  return rows.length > 0 ? rows : [createEmptyCookieRow()]
}

export function countRequestCookieRows(headers: EditableKeyValue[]): number {
  return requestCookieRows(headers).filter(
    (cookie) => cookie.enabled && cookie.key.trim().length > 0,
  ).length
}

export function replaceRequestCookieHeaders(
  headers: EditableKeyValue[],
  cookies: EditableKeyValue[],
): EditableKeyValue[] {
  const firstCookieIndex = headers.findIndex((header) =>
    isHeaderName(header.key, REQUEST_COOKIE_HEADER),
  )
  const firstCookieHeader = firstCookieIndex >= 0 ? headers[firstCookieIndex] : undefined
  const headerName = firstCookieHeader?.key.trim() || 'Cookie'
  const nonCookieHeaders = headers.filter(
    (header) => !isHeaderName(header.key, REQUEST_COOKIE_HEADER),
  )
  const insertionIndex =
    firstCookieIndex < 0
      ? nonCookieHeaders.length
      : headers
          .slice(0, firstCookieIndex)
          .filter((header) => !isHeaderName(header.key, REQUEST_COOKIE_HEADER)).length

  const enabledCookies: string[] = []
  const disabledCookies: string[] = []
  for (const cookie of cookies) {
    const name = cookie.key.trim()
    if (!name) {
      continue
    }
    const serialized = `${name}=${cookie.value ?? ''}`
    if (cookie.enabled) {
      enabledCookies.push(serialized)
    } else {
      disabledCookies.push(serialized)
    }
  }

  const replacements: EditableKeyValue[] = []
  if (enabledCookies.length > 0) {
    replacements.push({
      key: headerName,
      value: enabledCookies.join('; '),
      enabled: true,
    })
  }
  if (disabledCookies.length > 0) {
    replacements.push({
      key: headerName,
      value: disabledCookies.join('; '),
      enabled: false,
    })
  }

  nonCookieHeaders.splice(insertionIndex, 0, ...replacements)
  return nonCookieHeaders
}

export function requestCookieHeaderSignature(headers: EditableKeyValue[]): string {
  return JSON.stringify(
    headers
      .filter((header) => isHeaderName(header.key, REQUEST_COOKIE_HEADER))
      .map((header) => ({
        key: header.key,
        value: header.value,
        enabled: header.enabled,
      })),
  )
}
