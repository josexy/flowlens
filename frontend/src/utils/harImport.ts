export type HARImportSurface = 'capture' | 'history'

export function harImportDropTarget(surface: HARImportSurface): string {
  return `har-import:${surface}`
}

export function parseHARFileDrop(value: unknown): { surface: HARImportSurface; paths: string[] } | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const payload = value as Record<string, unknown>
  const surface = payload.target === 'har-import:capture'
    ? 'capture'
    : payload.target === 'har-import:history' ? 'history' : null
  if (!surface || !Array.isArray(payload.paths)) return null
  return { surface, paths: payload.paths.filter((path): path is string => typeof path === 'string' && path.length > 0) }
}

export function uniqueHARImportPaths(paths: string[], windows: boolean): string[] {
  const seen = new Set<string>()
  return paths.filter((path) => {
    if (!path) return false
    const key = windows ? path.replaceAll('\\', '/').toLowerCase() : path
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export function canHandleHARFileDrop(surface: HARImportSurface, activeContent: string, activeTabType: string, visible: boolean): boolean {
  return visible && activeContent === 'traffic' && activeTabType === surface
}
