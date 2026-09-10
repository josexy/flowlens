// Stay below WebView layout limits regardless of body size or bytes per row.
export const HEX_MAX_PAGE_HEIGHT = 4_000_000
export const HEX_PADDING_TOP = 6
export const HEX_PADDING_BOTTOM = 10

export function getHexdumpPage(
  byteLength: number,
  bytesPerRow: number,
  rowHeight: number,
  requestedPage: number,
) {
  const rowsPerPage = Math.max(
    1,
    Math.floor((HEX_MAX_PAGE_HEIGHT - HEX_PADDING_TOP - HEX_PADDING_BOTTOM) / rowHeight),
  )
  const totalRows = Math.ceil(byteLength / bytesPerRow)
  const count = Math.max(1, Math.ceil(totalRows / rowsPerPage))
  const index = Math.max(0, Math.min(requestedPage, count - 1))
  const startRow = index * rowsPerPage
  const rowCount = Math.min(rowsPerPage, totalRows - startRow)
  return {
    index,
    count,
    startRow,
    rowCount,
    rowsPerPage,
    height: HEX_PADDING_TOP + rowCount * rowHeight + HEX_PADDING_BOTTOM,
  }
}

export function getHexdumpPosition(
  byteOffset: number,
  byteLength: number,
  bytesPerRow: number,
  rowHeight: number,
) {
  const row = Math.floor(Math.max(0, Math.min(byteOffset, byteLength - 1)) / bytesPerRow)
  const { rowsPerPage } = getHexdumpPage(byteLength, bytesPerRow, rowHeight, 0)
  return {
    page: Math.floor(row / rowsPerPage),
    top: row === 0 ? 0 : HEX_PADDING_TOP + (row % rowsPerPage) * rowHeight,
  }
}
