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
    // Keep threshold checks bounded even for a growing multi-megabyte SSE body.
    // Beyond this limit return a UTF-8 upper bound, not a UTF-16 byte count.
    if (input.length > 64 * 1024) return input.length * 3
    let size = 0
    for (let i = 0; i < input.length; i++) {
      const code = input.charCodeAt(i)
      if (code < 0x80) size++
      else if (code < 0x800) size += 2
      else if (
        code >= 0xd800 &&
        code <= 0xdbff &&
        input.charCodeAt(i + 1) >= 0xdc00 &&
        input.charCodeAt(i + 1) <= 0xdfff
      ) {
        size += 4
        i++
      } else size += 3
    }
    return size
  }

  return estimateBase64DecodedByteLength(input)
}

function estimateBase64DecodedByteLength(input: string): number {
  // Backend input is canonical Base64 (not a data URL). Whitespace, if supplied,
  // only overestimates the size; never scan the entire payload on the UI thread.
  const end = input.length
  let padding = 0
  if (input.charCodeAt(end - 1) === 0x3d) padding++
  if (input.charCodeAt(end - 2) === 0x3d) padding++
  return Math.max(0, Math.floor((end * 3) / 4) - padding)
}

/** Active-view storage: append without copying the previously decoded body. */
export class HexdumpBytes {
  private chunks: { start: number; data: Uint8Array; used: number }[] = []
  length = 0

  append(data: Uint8Array) {
    if (!data.length) return
    const tail = this.chunks.at(-1)
    const copied = tail ? Math.min(data.length, tail.data.length - tail.used) : 0
    if (tail && copied) {
      tail.data.set(data.subarray(0, copied), tail.used)
      tail.used += copied
      this.length += copied
    }
    if (copied === data.length) return
    const rest = data.subarray(copied)
    // Amortize tiny stream updates, with at most 64 KiB of unused capacity.
    const storage = this.length > 0 && rest.length < 64 * 1024 ? new Uint8Array(64 * 1024) : rest
    if (storage !== rest) storage.set(rest)
    this.chunks.push({ start: this.length, data: storage, used: rest.length })
    this.length += rest.length
  }

  get(index: number): number | undefined {
    if (index < 0 || index >= this.length) return undefined
    let low = 0
    let high = this.chunks.length - 1
    while (low <= high) {
      const middle = (low + high) >>> 1
      const chunk = this.chunks[middle]!
      if (index < chunk.start) high = middle - 1
      else if (index >= chunk.start + chunk.used) low = middle + 1
      else return chunk.data[index - chunk.start]
    }
    return undefined
  }
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
