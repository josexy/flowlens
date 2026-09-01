import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import type { HttpRequestEditorState, WebSocketClientState } from '@/types/request-editor'

export type WorkspaceTabType = 'capture' | 'history' | 'http-request' | 'websocket-client'

export interface WorkspaceTab {
  key: string
  type: WorkspaceTabType
  title: string
  closable: boolean
  historyKey?: string
  apiId?: string
  apiUpdatedAt?: number
  savedSnapshot?: string
  httpRequest?: HttpRequestEditorState
  webSocketClient?: WebSocketClientState
}

export const CAPTURE_TAB_KEY = 'capture'
export const HISTORY_TAB_KEY = 'history'
export const HTTP_REQUEST_TAB_PREFIX = 'http-request:'
export const WEBSOCKET_CLIENT_TAB_PREFIX = 'websocket-client:'
export const DEFAULT_HTTP_TAB_TITLE = ''
export const DEFAULT_WS_TAB_TITLE = ''
export const HTTP_REQUEST_EVENT_NAME = 'http-request:event'
export const MAX_PENDING_HTTP_REQUEST_EVENT_SESSIONS = 100
export const WEBSOCKET_SESSION_EVENT_NAME = 'websocket-session:event'
export const MAX_PENDING_WEBSOCKET_SESSION_EVENT_SESSIONS = 100

export function buildCaptureTab(): WorkspaceTab {
  return {
    key: CAPTURE_TAB_KEY,
    type: 'capture',
    title: CAPTURE_TAB_KEY,
    closable: false,
  }
}

export function deriveRequestTabTitle(rawUrl: string): string {
  const trimmedUrl = rawUrl.trim()
  if (!trimmedUrl) {
    return ''
  }

  try {
    const parsedUrl = new URL(trimmedUrl)
    if (parsedUrl.hostname) {
      return parsedUrl.hostname
    }
  } catch {
    // Fall through to partial-input parsing.
  }

  const leadingSegment = trimmedUrl.split(/[/?#]/, 1)[0] ?? ''
  if (!leadingSegment) {
    return ''
  }

  try {
    const parsedHost = new URL(`http://${leadingSegment}`)
    if (parsedHost.hostname) {
      return parsedHost.hostname
    }
  } catch {
    // Keep the original leading segment for invalid partial input.
  }

  return leadingSegment
}

export function formatHistoryTitle(metadata: proxyservice.HistoryMetadata): string {
  if (metadata.alias?.trim()) {
    return metadata.alias.trim()
  }
  if (!metadata.createdAt) {
    return metadata.key
  }
  const date = new Date(metadata.createdAt)
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  return `${y}-${m}-${d} ${hh}:${mm}`
}
