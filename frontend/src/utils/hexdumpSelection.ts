import type { HexdumpBytes } from './hexdump'
import { IncrementalBase64Encoder } from './incrementalBase64'

export type HexdumpCopyFormat = 'text' | 'hex' | 'base64'

/** Windows text clipboard rejects NUL; escape it without altering the other characters. */
export async function escapeHexdumpClipboardNullBytes(
  text: string,
  signal: AbortSignal,
): Promise<string> {
  const parts: string[] = []
  let escaped = false
  signal.throwIfAborted()
  for (let offset = 0; offset < text.length; offset += 0x8000) {
    signal.throwIfAborted()
    const chunk = text.slice(offset, offset + 0x8000)
    if (!escaped && chunk.includes('\u0000')) {
      if (offset > 0) parts.push(text.slice(0, offset))
      escaped = true
    }
    if (escaped) parts.push(chunk.replaceAll('\u0000', '\\x00'))
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
  }
  signal.throwIfAborted()
  return escaped ? parts.join('') : text
}

/** Serialize on demand, without flattening the full byte store or blocking on a large range. */
export async function serializeHexdumpSelection(
  bytes: HexdumpBytes,
  start: number,
  endExclusive: number,
  format: HexdumpCopyFormat,
  signal: AbortSignal,
  textEncoding: 'utf8' | 'binary' = 'binary',
): Promise<string> {
  const parts: string[] = []
  const base64 = format === 'base64' ? new IncrementalBase64Encoder() : null
  const textDecoder =
    format === 'text' && textEncoding === 'utf8'
      ? new TextDecoder('utf-8', { ignoreBOM: true })
      : null
  signal.throwIfAborted()
  for (const chunk of bytes.range(start, endExclusive)) {
    signal.throwIfAborted()
    if (base64) {
      base64.append(chunk)
    } else if (format === 'text') {
      // ASCII dots belong only to the display. Binary text keeps each byte's
      // character value; UTF-8 text uses one decoder across chunk boundaries.
      parts.push(
        textDecoder ? textDecoder.decode(chunk, { stream: true }) : String.fromCharCode(...chunk),
      )
    } else {
      parts.push(
        Array.from(chunk, (value) => value.toString(16).padStart(2, '0').toUpperCase()).join(' '),
      )
    }
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
  }
  signal.throwIfAborted()
  if (textDecoder) parts.push(textDecoder.decode())
  return base64 ? base64.value() : parts.join(format === 'hex' ? ' ' : '')
}
