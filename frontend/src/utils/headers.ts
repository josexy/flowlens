export type HeaderSortOrder = 'default' | 'asc' | 'desc'
export type NullableHeadersRecord = Record<string, string[] | null | undefined> | null | undefined
export interface HeaderField {
  name: string
  value: string
}
export type NullableHeaderFields = (HeaderField | null)[] | null | undefined
export type RequestHTTPProtocol = 'auto' | 'http1' | 'http2'
export interface EditableHeaderRow {
  key: string
  value: string
  enabled: boolean
}
export interface HeaderOrderHTTPMessage {
  proto?: string
  headerFields?: unknown[] | null
  headersTruncated?: boolean
  headerOrderUnavailable?: boolean
}

const HTTP_HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/
const HTTP2_CONNECTION_SPECIFIC_HEADER_NAMES = new Set([
  'connection',
  'keep-alive',
  'proxy-connection',
  'transfer-encoding',
  'upgrade',
])

export function normalizeRequestProtocol(value: unknown): RequestHTTPProtocol {
  return value === 'http1' || value === 'http2' ? value : 'auto'
}

export function inferRequestProtocolFromHTTPMessage(
  message: HeaderOrderHTTPMessage | null | undefined,
): RequestHTTPProtocol {
  if (
    message?.headerFields === null ||
    message?.headerFields === undefined ||
    message.headersTruncated ||
    message.headerOrderUnavailable
  ) {
    return 'auto'
  }
  if (message.proto?.startsWith('HTTP/2')) {
    return 'http2'
  }
  if (message.proto?.startsWith('HTTP/1')) {
    return 'http1'
  }
  return 'auto'
}

export function normalizeHeadersRecord(
  headers: NullableHeadersRecord,
): Record<string, string[]> {
  const result = Object.create(null) as Record<string, string[]>
  for (const [key, values] of Object.entries(headers ?? {})) {
    result[key] = values ?? []
  }
  return result
}

export function sortHeadersRecord(
  headers: Record<string, string[]>,
  order: HeaderSortOrder,
): Record<string, string[]> {
  if (order === 'default') {
    return { ...headers }
  }

  const entries = Object.entries(headers).sort(([left], [right]) =>
    order === 'asc' ? left.localeCompare(right) : right.localeCompare(left),
  )

  return Object.fromEntries(entries)
}

export function headersRecordToFields(headers: NullableHeadersRecord): HeaderField[] {
  const fields: HeaderField[] = []
  for (const [name, values] of Object.entries(headers ?? {})) {
    if (!values?.length) {
      fields.push({ name, value: '' })
      continue
    }
    for (const value of values) {
      fields.push({ name, value: value ?? '' })
    }
  }
  return fields
}

export function firstHeaderFieldValue(
  fields: NullableHeaderFields,
  expectedName: string,
): string | undefined {
  const normalizedName = expectedName.trim().toLocaleLowerCase()
  return (fields ?? []).find(
    (field): field is HeaderField =>
      field !== null && field.name.trim().toLocaleLowerCase() === normalizedName,
  )?.value
}

export function normalizeHeaderFields(
  fields: NullableHeaderFields,
  headerOrderUnavailable = false,
): { fields: HeaderField[]; hasWireOrder: boolean } {
  const normalizedFields = (fields ?? [])
    .filter((field): field is HeaderField => field !== null)
    .map((field) => ({ name: field.name ?? '', value: field.value ?? '' }))
  return {
    fields: normalizedFields,
    hasWireOrder: fields !== null && fields !== undefined && !headerOrderUnavailable,
  }
}

export function sortHeaderFields(fields: HeaderField[], order: HeaderSortOrder): HeaderField[] {
  const result = fields.map((field) => ({ ...field }))
  if (order === 'default') {
    return result
  }
  return result.sort((left, right) => {
    const compared = left.name.toLocaleLowerCase().localeCompare(right.name.toLocaleLowerCase())
    return order === 'asc' ? compared : -compared
  })
}

function headerFieldsToJsonRecord(fields: HeaderField[]): Record<string, string | string[]> {
  const grouped = Object.create(null) as Record<string, string[]>
  for (const field of fields) {
    if (!Object.hasOwn(grouped, field.name)) {
      grouped[field.name] = []
    }
    grouped[field.name]!.push(field.value)
  }
  return Object.fromEntries(
    Object.entries(grouped).map(([name, values]) => [
      name,
      values.length === 1 ? (values[0] ?? '') : values,
    ]),
  )
}

export function formatHeaderFieldsAsJson(fields: HeaderField[]): string {
  return JSON.stringify(headerFieldsToJsonRecord(fields), null, 2)
}

export function formatHeaderFieldsAsText(fields: HeaderField[]): string {
  return fields.map((field) => `${field.name}: ${field.value}`).join('\n')
}

function toJsonHeaderRecord(
  headers: Record<string, string[]>,
): Record<string, string | string[]> {
  return Object.fromEntries(
    Object.entries(headers).map(([key, values]) => {
      if (values.length === 1) {
        return [key, values[0] ?? '']
      }
      return [key, values]
    }),
  )
}

export function formatHeadersAsJson(headers: Record<string, string[]>): string {
  return JSON.stringify(toJsonHeaderRecord(headers), null, 2)
}

export function formatHeadersAsText(headers: Record<string, string[]>): string {
  const lines: string[] = []

  for (const [key, values] of Object.entries(headers)) {
    for (const value of values) {
      lines.push(`${key}: ${value}`)
    }
  }

  return lines.join('\n')
}

export function editableRowsToHeadersRecord(
  rows: EditableHeaderRow[],
  options: { dedupe?: boolean } = {},
): Record<string, string[]> {
  const result = Object.create(null) as Record<string, string[]>
  const seenKeys = new Set<string>()

  for (const row of rows) {
    if (!row.enabled) {
      continue
    }

    const key = row.key.trim()
    if (!key) {
      continue
    }

    const normalizedKey = key.toLowerCase()
    if (options.dedupe && seenKeys.has(normalizedKey)) {
      continue
    }

    seenKeys.add(normalizedKey)

    if (!Object.hasOwn(result, key)) {
      result[key] = []
    }

    result[key]!.push(row.value ?? '')
  }

  return result
}

export function editableRowsToHeaderFields(rows: EditableHeaderRow[]): HeaderField[] {
  const fields: HeaderField[] = []
  for (const row of rows) {
    const name = row.key.trim()
    if (!row.enabled || !name) {
      continue
    }
    fields.push({ name, value: row.value ?? '' })
  }
  return fields
}

export function headerFieldsToEditableRows(fields: NullableHeaderFields): EditableHeaderRow[] {
  return (fields ?? [])
    .filter((field): field is HeaderField => field !== null)
    .map((field) => ({
      key: field.name ?? '',
      value: field.value ?? '',
      enabled: true,
    }))
}

export function editableHeaderFieldsToRows(
  fields: NullableHeaderFields,
): EditableHeaderRow[] {
  return headerFieldsToEditableRows(fields).filter((row) => !row.key.trim().startsWith(':'))
}

export function isValidHTTPHeaderName(name: string): boolean {
  const normalizedName = name.trim()
  return normalizedName.length > 0 && HTTP_HEADER_NAME_PATTERN.test(normalizedName)
}

export function isValidRequestHeaderName(
  name: string,
  protocol: RequestHTTPProtocol = 'auto',
): boolean {
  if (!isValidHTTPHeaderName(name)) {
    return false
  }
  return !(
    protocol === 'http2' &&
    HTTP2_CONNECTION_SPECIFIC_HEADER_NAMES.has(name.trim().toLocaleLowerCase())
  )
}

export function findInvalidRequestHeaderName(
  rows: EditableHeaderRow[],
  protocol: RequestHTTPProtocol = 'auto',
): string | null {
  for (const row of rows) {
    const name = row.key.trim()
    if (row.enabled && name && !isValidRequestHeaderName(name, protocol)) {
      return name
    }
  }
  return null
}

interface RequestRouteValues {
  authority: string
}

function parseRequestRouteValues(rawURL: string): RequestRouteValues | null {
  let parsedURL: URL
  try {
    parsedURL = new URL(rawURL)
  } catch {
    return null
  }
  if (parsedURL.protocol !== 'http:' && parsedURL.protocol !== 'https:') {
    return null
  }
  return {
    authority: parsedURL.host,
  }
}

function isRequestRouteField(name: string): boolean {
  const normalizedName = name.trim().toLocaleLowerCase()
  return normalizedName === 'host' || normalizedName.startsWith(':')
}

/** Remove backend-owned pseudo-headers and synchronize Host when the protocol changes. */
export function convertRequestRouteHeaders(
  rows: EditableHeaderRow[],
  protocol: RequestHTTPProtocol,
  _method: string,
  rawURL: string,
): EditableHeaderRow[] {
  const route = parseRequestRouteValues(rawURL) ?? {
    authority:
      rows.find((row) => [':authority', 'host'].includes(row.key.trim().toLocaleLowerCase()))
        ?.value ?? '',
  }
  const retainedRows: EditableHeaderRow[] = []
  let firstRouteInsertIndex = -1
  let hostName = 'Host'
  for (const row of rows) {
    if (!isRequestRouteField(row.key)) {
      retainedRows.push({ ...row })
      continue
    }
    if (firstRouteInsertIndex < 0) {
      firstRouteInsertIndex = retainedRows.length
    }
    if (row.key.trim().toLocaleLowerCase() === 'host') {
      hostName = row.key.trim() || 'Host'
    }
  }

  if (protocol === 'http2') {
    return retainedRows
  }

  const insertAt = firstRouteInsertIndex < 0 ? 0 : firstRouteInsertIndex
  retainedRows.splice(insertAt, 0, {
    key: hostName,
    value: route.authority,
    enabled: true,
  })
  return retainedRows
}

/** Keep URL as routing truth while leaving all pseudo-headers backend-owned. */
export function synchronizeRequestRouteHeaders(
  rows: EditableHeaderRow[],
  protocol: RequestHTTPProtocol,
  _method: string,
  rawURL: string,
): EditableHeaderRow[] {
  const route = parseRequestRouteValues(rawURL)
  const sanitizedRows = rows.filter((row) => !row.key.trim().startsWith(':'))

  if (protocol === 'http2') {
    return sanitizedRows.filter((row) => row.key.trim().toLocaleLowerCase() !== 'host')
  }

  let hostRow: EditableHeaderRow | null = null
  let hostInsertAt = -1
  const result: EditableHeaderRow[] = []
  for (const row of sanitizedRows) {
    if (row.key.trim().toLocaleLowerCase() !== 'host') {
      result.push(row)
      continue
    }
    if (hostInsertAt < 0) {
      hostInsertAt = result.length
    }
    if (hostRow === null) {
      hostRow = {
        ...row,
        value: route?.authority ?? row.value,
      }
    }
  }

  if (hostRow === null) {
    hostRow = {
      key: 'Host',
      value: route?.authority ?? '',
      enabled: true,
    }
  }
  result.splice(hostInsertAt < 0 ? 0 : hostInsertAt, 0, hostRow)
  return result
}
