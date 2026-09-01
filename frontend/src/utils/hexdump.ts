export interface HexByte {
  hex: string
  ascii: string
  value: number
  globalIdx: number
}

export interface HexLine {
  offsetHex: string
  bytes: (HexByte | null)[] // always 16 elements; null = padding on last line
}

export function decodeHexdumpBytes(input: string | Uint8Array, isBase64 = false): Uint8Array {
  if (input instanceof Uint8Array) {
    return input
  }

  if (isBase64) {
    const binary = atob(input)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
    return bytes
  }

  return new TextEncoder().encode(input)
}

export function estimateDecodedByteLength(input: string | Uint8Array, isBase64 = false): number {
  if (input instanceof Uint8Array) {
    return input.length
  }

  if (!isBase64) {
    return input.length
  }

  return estimateBase64DecodedByteLength(input)
}

function estimateBase64DecodedByteLength(input: string): number {
  const start = input.lastIndexOf(',') + 1
  let end = input.length
  while (end > start && isAsciiWhitespace(input.charCodeAt(end - 1))) {
    end--
  }

  let padding = 0
  if (end > start && input.charCodeAt(end - 1) === 0x3d) padding++
  if (end > start + 1 && input.charCodeAt(end - 2) === 0x3d) padding++

  const dataLength = end - start
  return Math.max(0, Math.floor((dataLength * 3) / 4) - Math.min(padding, 2))
}

function isAsciiWhitespace(code: number) {
  return code === 0x20 || code === 0x09 || code === 0x0a || code === 0x0d || code === 0x0c
}

/**
 * Return structured hexdump data for interactive rendering.
 * Each HexLine always has exactly 16 entries in `bytes`; trailing entries are
 * null when the last line has fewer than 16 bytes.
 */
export function hexdumpStructured(input: string | Uint8Array, isBase64 = false): HexLine[] {
  const bytes = decodeHexdumpBytes(input, isBase64)

  if (bytes.length === 0) return []

  const BYTES_PER_LINE = 16
  const lines: HexLine[] = []
  for (let offset = 0; offset < bytes.length; offset += BYTES_PER_LINE) {
    const chunk = bytes.slice(offset, offset + BYTES_PER_LINE)
    const lineBytes: (HexByte | null)[] = []
    for (let i = 0; i < BYTES_PER_LINE; i++) {
      if (i < chunk.length) {
        const b = chunk[i] ?? 0
        lineBytes.push({
          hex: b.toString(16).padStart(2, '0'),
          ascii: b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.',
          value: b,
          globalIdx: offset + i,
        })
      } else {
        lineBytes.push(null)
      }
    }
    lines.push({ offsetHex: offset.toString(16).padStart(8, '0'), bytes: lineBytes })
  }
  return lines
}

/**
 * Generate an xxd-style hexdump string.
 *
 * Format per line:
 *   00000000: 4865 6c6c 6f20 776f  726c 6421 0a        Hello world!.
 *
 * @param input    - Raw string or Uint8Array
 * @param isBase64 - When true, `input` must be a string and is decoded via atob() first
 */
export function hexdump(input: string | Uint8Array, isBase64 = false): string {
  const bytes = decodeHexdumpBytes(input, isBase64)

  if (bytes.length === 0) return ''

  const BYTES_PER_LINE = 16
  const lines: string[] = []

  for (let offset = 0; offset < bytes.length; offset += BYTES_PER_LINE) {
    const chunk = bytes.slice(offset, offset + BYTES_PER_LINE)
    const offsetHex = offset.toString(16).padStart(8, '0')
    const hexStr = buildHexString(chunk, BYTES_PER_LINE)
    let ascii = ''
    for (let i = 0; i < chunk.length; i++) {
      const b = chunk[i] ?? 0
      ascii += b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.'
    }
    lines.push(`${offsetHex}: ${hexStr}  ${ascii}`)
  }

  return lines.join('\n')
}

function buildHexString(chunk: Uint8Array, lineWidth: number): string {
  const parts: string[] = []
  for (let i = 0; i < lineWidth; i++) {
    if (i === 8) {
      parts.push(' ') // extra separator between two 8-byte groups
    }
    if (i < chunk.length) {
      const b = chunk[i] ?? 0
      const byteHex = b.toString(16).padStart(2, '0')
      parts.push(i % 2 === 0 ? byteHex : byteHex + ' ')
    } else {
      // pad for shorter last line
      parts.push(i % 2 === 0 ? '  ' : '   ')
    }
  }
  return parts.join('').trimEnd()
}
