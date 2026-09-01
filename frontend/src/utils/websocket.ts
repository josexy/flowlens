import type * as proxyservice from '../../bindings/github.com/josexy/flowlens/backend/services/proxy_service/models.js'
import type { WebSocketDisplayMessage } from '../types/websocket.js'

export function normalizeWebSocketDirection(
  direction: string | null | undefined,
): WebSocketDisplayMessage['direction'] {
  return direction?.toLowerCase() === 'receive' ? 'receive' : 'send'
}

export function normalizeWebSocketMsgType(
  msgType: string | null | undefined,
): WebSocketDisplayMessage['msgType'] {
  return msgType === 'binary' ? 'binary' : 'text'
}

export function toWebSocketDisplayMessage(
  item: proxyservice.WebSocketMessage,
  id: string,
  createdAt = Date.now(),
): WebSocketDisplayMessage {
  return {
    id,
    direction: normalizeWebSocketDirection(item.direction),
    msgType: normalizeWebSocketMsgType(item.msgType),
    data: item.data || '',
    dataSize: item.dataSize ?? item.data?.length ?? 0,
    createdAt,
  }
}

export function toWebSocketDisplayMessages(
  items: (proxyservice.WebSocketMessage | null)[] | null | undefined,
  createId: (item: proxyservice.WebSocketMessage, index: number) => string,
): WebSocketDisplayMessage[] {
  if (!items?.length) {
    return []
  }
  const messages: WebSocketDisplayMessage[] = []
  for (let index = 0; index < items.length; index++) {
    const item = items[index]
    if (!item) continue
    messages.push(toWebSocketDisplayMessage(item, createId(item, index)))
  }
  return messages
}
