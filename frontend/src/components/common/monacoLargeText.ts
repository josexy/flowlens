// Monaco only enables its built-in large-file tokenization guard above 20 MB.
// FlowLens bodies need an earlier cutoff because wrapped capture payloads are
// laid out synchronously whenever a hidden editor becomes visible.
export const MONACO_LARGE_TEXT_THRESHOLD_CHARS = 512 * 1024
// Keep wrapped large-text models small enough that Monaco can lay them out
// without blocking the UI, while still providing a useful reading window.
export const MONACO_WRAPPED_CHUNK_SIZE_CHARS = 128 * 1024
// Protect sub-threshold single-line payloads without treating Monaco's much
// lower tokenization cutoff as a wrapped-layout performance boundary.
export const MONACO_LONG_LINE_THRESHOLD_CHARS = MONACO_WRAPPED_CHUNK_SIZE_CHARS

export interface MonacoWrappedTextChunk {
  text: string
  index: number
  count: number
  start: number
  end: number
}

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

function normalizeMonacoWrappedChunkBoundary(value: string, boundary: number): number {
  if (boundary <= 0 || boundary >= value.length) {
    return Math.min(Math.max(boundary, 0), value.length)
  }

  const previousCodeUnit = value.charCodeAt(boundary - 1)
  const nextCodeUnit = value.charCodeAt(boundary)
  const splitsCrLf = previousCodeUnit === 0x0d && nextCodeUnit === 0x0a
  const splitsSurrogatePair =
    previousCodeUnit >= 0xd800 &&
    previousCodeUnit <= 0xdbff &&
    nextCodeUnit >= 0xdc00 &&
    nextCodeUnit <= 0xdfff

  return splitsCrLf || splitsSurrogatePair ? boundary - 1 : boundary
}

export function getMonacoWrappedTextChunk(
  value: string,
  requestedIndex: number,
): MonacoWrappedTextChunk {
  const count = Math.max(1, Math.ceil(value.length / MONACO_WRAPPED_CHUNK_SIZE_CHARS))
  const finiteIndex = Number.isFinite(requestedIndex)
    ? Math.trunc(requestedIndex)
    : requestedIndex > 0
      ? count - 1
      : 0
  const index = Math.min(Math.max(finiteIndex, 0), count - 1)
  const start = normalizeMonacoWrappedChunkBoundary(
    value,
    index * MONACO_WRAPPED_CHUNK_SIZE_CHARS,
  )
  const end = normalizeMonacoWrappedChunkBoundary(
    value,
    Math.min((index + 1) * MONACO_WRAPPED_CHUNK_SIZE_CHARS, value.length),
  )

  return {
    text: value.slice(start, end),
    index,
    count,
    start,
    end,
  }
}
