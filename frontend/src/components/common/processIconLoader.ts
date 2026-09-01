import { GetProcessIcon } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'

import { ProcessIconCache, type ProcessIconAvailableListener } from './processIconCache'

// This loader is intentionally scoped to the current Wails WebView. A cache
// hit skips GetProcessIcon, so clearing backend files must be paired with
// clearProcessIconCache() in every frontend window.
const windowProcessIconCache = new ProcessIconCache(256)
const processIconCacheResetListeners = new Set<() => void>()

export const PROCESS_ICON_KEY_PATTERN = /^[0-9a-f]{64}$/

export async function loadProcessIcon(key: string): Promise<string | null> {
  if (!PROCESS_ICON_KEY_PATTERN.test(key)) {
    return null
  }

  return windowProcessIconCache.load(key, async () => {
    const data = await GetProcessIcon(key)
    return data?.mimeType === 'image/png' && data.dataBase64
      ? `data:image/png;base64,${data.dataBase64}`
      : null
  })
}

export function deleteProcessIconCacheEntry(key: string) {
  windowProcessIconCache.delete(key)
}

export function clearProcessIconCache() {
  windowProcessIconCache.clear()
  for (const listener of processIconCacheResetListeners) {
    listener()
  }
}

export function onProcessIconCacheReset(listener: () => void): () => void {
  processIconCacheResetListeners.add(listener)
  return () => processIconCacheResetListeners.delete(listener)
}

export function onProcessIconAvailable(listener: ProcessIconAvailableListener): () => void {
  return windowProcessIconCache.onAvailable(listener)
}
