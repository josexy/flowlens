export type WebSocketViewMode = 'list' | 'conversation'
export type WebSocketDirectionFilter = 'all' | 'send' | 'receive'

export interface WebSocketDisplayMessage {
  id: string
  direction: 'send' | 'receive'
  msgType: 'text' | 'binary'
  data: string
  dataSize: number
  createdAt: number
}
