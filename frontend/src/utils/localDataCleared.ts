export type LocalDataClearedScope = 'cache' | 'cache-and-history'

export interface LocalDataClearedPayload {
  scope: LocalDataClearedScope
  historyCleared: boolean
  requestDraftCacheRoot?: string
}

export interface LocalDataClearedWindowActions {
  clearProcessIconCache: () => void
  resetHistory: () => void
  reloadHistory: () => void
  clearRequestDraftCacheFileReferences: (requestDraftCacheRoot: string) => void
}

function isLocalDataClearedScope(value: unknown): value is LocalDataClearedScope {
  return value === 'cache' || value === 'cache-and-history'
}

export function parseLocalDataClearedPayload(value: unknown): LocalDataClearedPayload | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }

  const payload = value as Record<string, unknown>
  const scope = payload.scope
  if (!isLocalDataClearedScope(scope)) return null

  const historyCleared = payload.historyCleared === true
  const requestDraftCacheRoot =
    typeof payload.requestDraftCacheRoot === 'string' ? payload.requestDraftCacheRoot.trim() : ''
  return requestDraftCacheRoot
    ? { scope, historyCleared, requestDraftCacheRoot }
    : { scope, historyCleared }
}

export function syncLocalDataClearedWindow(
  payload: LocalDataClearedPayload,
  actions: LocalDataClearedWindowActions,
) {
  actions.clearProcessIconCache()
  if (payload.scope === 'cache') {
    // Cache-only cleanup can rotate a flushed current capture into saved
    // history, so every window must refresh its visible history metadata.
    actions.reloadHistory()
  } else {
    if (payload.historyCleared) {
      actions.resetHistory()
    } else {
      actions.reloadHistory()
    }
  }
  if (payload.requestDraftCacheRoot) {
    actions.clearRequestDraftCacheFileReferences(payload.requestDraftCacheRoot)
  }
}
