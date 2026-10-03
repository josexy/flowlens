export const HEX_HORIZONTAL_PADDING = 16
export const HEX_SECTION_PADDING = 8

export function getHexdumpLayout(
  width: number,
  charWidth: number,
  byteLength: number,
  gutter: number,
) {
  const offsetDigits = Math.max(5, Math.max(0, byteLength - 1).toString(16).length)
  const offsetWidth = (offsetDigits + 1) * charWidth + 1
  const available =
    width - gutter - HEX_HORIZONTAL_PADDING - offsetWidth - HEX_SECTION_PADDING * 3 - 1
  const bytesPerRow = Math.max(1, Math.min(64, Math.floor(available / (4 * charWidth))))
  const hexStart = offsetWidth + HEX_SECTION_PADDING
  const asciiStart = hexStart + bytesPerRow * 3 * charWidth + HEX_SECTION_PADDING * 2 + 1
  return { bytesPerRow, offsetDigits, offsetWidth, hexStart, asciiStart }
}

/** Hit-test the original drag column, including gaps and off-screen pointer positions. */
export function getHexdumpPointerByte(
  x: number,
  y: number,
  column: 'hex' | 'ascii',
  layout: ReturnType<typeof getHexdumpLayout>,
  charWidth: number,
  rowHeight: number,
  startRow: number,
  rowCount: number,
  byteLength: number,
) {
  if (!rowCount || !byteLength) return -1
  const row = Math.max(0, Math.min(rowCount - 1, Math.floor(y / rowHeight)))
  const start = column === 'hex' ? layout.hexStart : layout.asciiStart
  const cellWidth = charWidth * (column === 'hex' ? 3 : 1)
  const cell = Math.max(0, Math.min(layout.bytesPerRow - 1, Math.floor((x - start) / cellWidth)))
  return Math.min(byteLength - 1, (startRow + row) * layout.bytesPerRow + cell)
}
