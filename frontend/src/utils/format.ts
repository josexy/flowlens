export const formatRelativeTime = (timestamp: Date | string): string => {
  const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSecs = Math.floor(diffMs / 1000)
  const diffMins = Math.floor(diffSecs / 60)
  const diffHours = Math.floor(diffMins / 60)
  const diffDays = Math.floor(diffHours / 24)

  if (diffSecs < 60) {
    return 'Just now'
  } else if (diffMins < 60) {
    return `${diffMins} minute${diffMins > 1 ? 's' : ''} ago`
  } else if (diffHours < 24) {
    return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`
  } else if (diffDays < 7) {
    return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`
  } else {
    return date.toLocaleDateString()
  }
}

export const formatToRFC3339 = (dateInput: Date | string | number): string => {
  const date = new Date(dateInput)

  const pad = (n: number) => (n < 10 ? '0' + n : n)

  const year = date.getFullYear()
  const month = pad(date.getMonth() + 1)
  const day = pad(date.getDate())
  const hours = pad(date.getHours())
  const minutes = pad(date.getMinutes())
  const seconds = pad(date.getSeconds())
  const milliseconds = date.getMilliseconds()

  const tzo = -date.getTimezoneOffset()
  const dif = tzo >= 0 ? '+' : '-'
  const padTzo = (n: number) => {
    const abs = Math.abs(n)
    const h = Math.floor(abs / 60)
    const m = abs % 60
    return pad(h) + ':' + pad(m)
  }

  return `${year}-${month}-${day}T${hours}:${minutes}:${seconds}.${milliseconds}${dif}${padTzo(tzo)}`
}

export const UNKNOWN_FORMATTED_VALUE = '—'

const padNumber = (value: number, length: number = 2): string => String(value).padStart(length, '0')

function formatLocalDateTime(date: Date, fractionalMicros: number): string {
  return [
    `${date.getFullYear()}-${padNumber(date.getMonth() + 1)}-${padNumber(date.getDate())}`,
    `${padNumber(date.getHours())}:${padNumber(date.getMinutes())}:${padNumber(date.getSeconds())}.${padNumber(fractionalMicros, 6)}`,
  ].join(' ')
}

export const formatDateTimeLocal = (dateInput: Date | string | number): string => {
  const date = new Date(dateInput)
  if (Number.isNaN(date.getTime())) {
    return UNKNOWN_FORMATTED_VALUE
  }

  const fractionalMatch =
    typeof dateInput === 'string'
      ? dateInput.match(/\.(\d{1,9})(?:Z|[+-]\d{2}:?\d{2})?$/i)
      : null
  const fractionalMicros = fractionalMatch
    ? Number(fractionalMatch[1]!.slice(0, 6).padEnd(6, '0'))
    : date.getMilliseconds() * 1000

  return formatLocalDateTime(date, fractionalMicros)
}

export const formatUnixMicrosLocal = (micros: number): string => {
  if (!Number.isSafeInteger(micros) || micros < 0) {
    return UNKNOWN_FORMATTED_VALUE
  }

  const date = new Date(Math.floor(micros / 1000))
  if (Number.isNaN(date.getTime())) {
    return UNKNOWN_FORMATTED_VALUE
  }

  const fractionalMicros = micros % 1_000_000
  return formatLocalDateTime(date, fractionalMicros)
}

export const formatDurationMicros = (startedAtMicros: number, endedAtMicros: number): string => {
  if (
    !Number.isSafeInteger(startedAtMicros) ||
    !Number.isSafeInteger(endedAtMicros) ||
    startedAtMicros < 0 ||
    endedAtMicros < startedAtMicros
  ) {
    return UNKNOWN_FORMATTED_VALUE
  }

  const durationMicros = endedAtMicros - startedAtMicros
  if (durationMicros < 1000) {
    return `${durationMicros} μs`
  }

  return `${Math.round(durationMicros / 1000)} ms`
}

export interface HTTPMessageSizeSummary {
  header: number | null
  body: number | null
  total: number | null
}

function knownByteSize(value: number | undefined): number | null {
  return value !== undefined && Number.isSafeInteger(value) && value >= 0 ? value : null
}

function isHTTP1Protocol(protocol: string): boolean {
  return /^HTTP\/1(?:\.\d+)?$/i.test(protocol.trim())
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

export function requestTargetFromURL(url: string): string | null {
  const trimmedURL = url.trim()
  if (trimmedURL === '*') return '*'

  const fragmentIndex = trimmedURL.indexOf('#')
  const withoutFragment =
    fragmentIndex >= 0 ? trimmedURL.slice(0, fragmentIndex) : trimmedURL
  const schemeSeparator = withoutFragment.indexOf('://')
  if (schemeSeparator >= 0) {
    const authorityStart = schemeSeparator + 3
    const pathStart = withoutFragment.indexOf('/', authorityStart)
    const queryStart = withoutFragment.indexOf('?', authorityStart)

    if (pathStart >= 0 && (queryStart < 0 || pathStart < queryStart)) {
      return withoutFragment.slice(pathStart)
    }
    if (queryStart >= 0) {
      return `/${withoutFragment.slice(queryStart)}`
    }
    return '/'
  }

  return withoutFragment.startsWith('/') ? withoutFragment : null
}

// The detail view includes a logical HTTP/1 start line, while HAR headersSize
// remains the field-line total persisted by FlowLens.
export const getLogicalHTTPRequestStartLineSize = (
  method: string,
  url: string,
  protocol: string,
): number => {
  const normalizedProtocol = protocol.trim()
  if (!isHTTP1Protocol(normalizedProtocol)) return 0

  const normalizedMethod = method.trim()
  if (normalizedMethod.toUpperCase() === 'CONNECT') return -1
  const requestTarget = requestTargetFromURL(url)
  if (!normalizedMethod || requestTarget === null) return -1

  return utf8ByteLength(`${normalizedMethod} ${requestTarget} ${normalizedProtocol}\r\n`)
}

export const getLogicalHTTPResponseStartLineSize = (status: string, protocol: string): number => {
  const normalizedProtocol = protocol.trim()
  if (!isHTTP1Protocol(normalizedProtocol)) return 0

  if (!status.trim()) return -1

  return utf8ByteLength(`${normalizedProtocol} ${status}\r\n`)
}

export const sumKnownByteSizes = (...sizes: Array<number | null>): number | null => {
  let total = 0
  for (const size of sizes) {
    if (size === null) return null
    total += size
    if (!Number.isSafeInteger(total)) return null
  }
  return total
}

export const summarizeHTTPMessageSize = (
  headerSize: number | undefined,
  bodySize: number | undefined,
  headersTruncated: boolean = false,
  startLineSize: number | undefined = 0,
): HTTPMessageSizeSummary => {
  const rawHeader = headersTruncated ? null : knownByteSize(headerSize)
  const startLine = knownByteSize(startLineSize)
  const header =
    rawHeader === null || startLine === null ? null : knownByteSize(rawHeader + startLine + 2)
  const body = knownByteSize(bodySize)
  return {
    header,
    body,
    total: sumKnownByteSizes(header, body),
  }
}

export const truncateText = (text: string, maxLength: number = 100): string => {
  if (text.length <= maxLength) {
    return text
  }
  return text.substring(0, maxLength) + '...'
}

export interface FormatFileSizeOptions {
  precision?: number
  trimTrailingZeros?: boolean
  unknownValue?: string
}

export const formatFileSize = (
  bytes: number | null | undefined,
  options: FormatFileSizeOptions = {},
): string => {
  const unknownValue = options.unknownValue ?? UNKNOWN_FORMATTED_VALUE
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes < 0) return unknownValue
  if (bytes === 0) return '0 B'

  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.max(Math.floor(Math.log(bytes) / Math.log(k)), 0), sizes.length - 1)
  if (i === 0) return `${bytes} ${sizes[i]}`

  const requestedPrecision = options.precision ?? 2
  const precision = Number.isFinite(requestedPrecision)
    ? Math.min(Math.max(Math.trunc(requestedPrecision), 0), 6)
    : 2
  const formatted = (bytes / Math.pow(k, i)).toFixed(precision)
  const value = options.trimTrailingZeros === false ? formatted : String(Number(formatted))
  return `${value} ${sizes[i]}`
}
