// Monaco only enables its built-in large-file tokenization guard above 20 MB.
// FlowLens bodies need an earlier cutoff because wrapped capture payloads are
// laid out synchronously whenever a hidden editor becomes visible.
export const MONACO_LARGE_TEXT_THRESHOLD_CHARS = 512 * 1024
// Protect sub-threshold single-line payloads without treating Monaco's much
// lower tokenization cutoff as a wrapped-layout performance boundary.
export const MONACO_LONG_LINE_THRESHOLD_CHARS = 128 * 1024

export function requiresMonacoLargeTextOptimizations(value: string): boolean {
  if (value.length >= MONACO_LARGE_TEXT_THRESHOLD_CHARS) {
    return true
  }
  if (value.length < MONACO_LONG_LINE_THRESHOLD_CHARS) {
    return false
  }

  let lineStart = 0
  while (lineStart < value.length) {
    const lineFeed = value.indexOf('\n', lineStart)
    if (lineFeed === -1) {
      return value.length - lineStart >= MONACO_LONG_LINE_THRESHOLD_CHARS
    }

    const lineEnd =
      lineFeed > lineStart && value.charCodeAt(lineFeed - 1) === 0x0d ? lineFeed - 1 : lineFeed
    if (lineEnd - lineStart >= MONACO_LONG_LINE_THRESHOLD_CHARS) {
      return true
    }
    lineStart = lineFeed + 1
  }

  return false
}
