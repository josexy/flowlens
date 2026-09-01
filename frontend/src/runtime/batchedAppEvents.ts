import { Events } from '@wailsio/runtime'

const BATCHED_APP_EVENT_NAME = 'app:event-batch'

interface BatchedAppEventItem {
  name: string
  data: unknown
}

interface BatchedAppEventPayload {
  events?: BatchedAppEventItem[]
  dropped?: Record<string, number>
}

interface BatchedAppEventListener {
  onData: (data: unknown) => void
  onDropped?: (count: number) => void
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function createBatchedAppEventRouter() {
  const listeners = new Map<string, Set<BatchedAppEventListener>>()
  let listenerCount = 0

  function subscribe(
    name: string,
    onData: (data: unknown) => void,
    onDropped?: (count: number) => void,
  ) {
    const listener: BatchedAppEventListener = { onData, onDropped }
    let eventListeners = listeners.get(name)
    if (!eventListeners) {
      eventListeners = new Set()
      listeners.set(name, eventListeners)
    }
    eventListeners.add(listener)
    listenerCount++

    let active = true
    return () => {
      if (!active) return
      active = false
      const current = listeners.get(name)
      if (!current?.delete(listener)) return
      listenerCount--
      if (current.size === 0) listeners.delete(name)
    }
  }

  function dispatch(value: unknown) {
    if (!isRecord(value)) return
    const payload = value as BatchedAppEventPayload
    if (Array.isArray(payload.events)) {
      for (const event of payload.events) {
        if (!isRecord(event) || typeof event.name !== 'string') continue
        const eventListeners = listeners.get(event.name)
        if (!eventListeners) continue
        for (const listener of eventListeners) {
          listener.onData(event.data)
        }
      }
    }
    if (!isRecord(payload.dropped)) return
    for (const [name, rawCount] of Object.entries(payload.dropped)) {
      if (typeof rawCount !== 'number' || !Number.isFinite(rawCount) || rawCount <= 0) continue
      const eventListeners = listeners.get(name)
      if (!eventListeners) continue
      const count = Math.floor(rawCount)
      for (const listener of eventListeners) {
        listener.onDropped?.(count)
      }
    }
  }

  return {
    subscribe,
    dispatch,
    get listenerCount() {
      return listenerCount
    },
  }
}

const appEventRouter = createBatchedAppEventRouter()
let offBatchedAppEvent: (() => void) | null = null

export function onBatchedAppEvent(
  name: string,
  onData: (data: unknown) => void,
  onDropped?: (count: number) => void,
) {
  const offListener = appEventRouter.subscribe(name, onData, onDropped)
  if (!offBatchedAppEvent) {
    offBatchedAppEvent = Events.On(BATCHED_APP_EVENT_NAME, (event) => {
      appEventRouter.dispatch(event.data)
    })
  }

  let active = true
  return () => {
    if (!active) return
    active = false
    offListener()
    if (appEventRouter.listenerCount !== 0) return
    offBatchedAppEvent?.()
    offBatchedAppEvent = null
  }
}
