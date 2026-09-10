import { onScopeDispose, shallowRef, triggerRef, watch } from 'vue'
import { decodeHexdumpBytes, estimateDecodedByteLength, HexdumpBytes } from '../utils/hexdump.js'

const WORKER_THRESHOLD = 256 * 1024
const MESSAGE_CHUNK_CHARS = 128 * 1024
const APPEND_INTERVAL_MS = 100

interface HexdumpSource {
  input: string | Uint8Array
  isBase64: boolean
  active: boolean
  // Only complete Unicode text appended within one SSE stream may use this path.
  appendOnly: boolean
}

export type HexdumpDecodeResponse =
  { id: number; ok: true; buffer: ArrayBuffer } | { id: number; ok: false; error: string }

export interface HexdumpWorker {
  postMessage(message: unknown): void
  terminate(): void
  onmessage: ((event: MessageEvent<HexdumpDecodeResponse>) => void) | null
  onerror: ((event: ErrorEvent) => void) | null
}

export function useHexdumpDecoder(source: HexdumpSource, createWorker: () => HexdumpWorker) {
  const bytes = shallowRef(new HexdumpBytes())
  const state = shallowRef<'idle' | 'loading' | 'ready' | 'error'>('idle')
  const error = shallowRef('')
  const revision = shallowRef(0)
  let worker: HexdumpWorker | null = null
  let sendTimer: ReturnType<typeof setTimeout> | null = null
  let appendTimer: ReturnType<typeof setTimeout> | null = null
  let generation = 0
  let completedChars = 0
  let busy = false

  function stopWorker() {
    if (sendTimer !== null) clearTimeout(sendTimer)
    sendTimer = null
    worker?.terminate()
    worker = null
  }

  function cancel() {
    generation++
    stopWorker()
    if (appendTimer !== null) clearTimeout(appendTimer)
    appendTimer = null
    busy = false
  }

  function scheduleAppend() {
    if (busy || appendTimer !== null || !source.active || state.value === 'error') return
    appendTimer = setTimeout(() => {
      appendTimer = null
      decode()
    }, APPEND_INTERVAL_MS)
  }

  function decode() {
    if (!source.active || busy) return
    const id = ++generation
    const end = source.input.length
    const first = completedChars === 0
    const input =
      typeof source.input === 'string' ? source.input.slice(completedChars) : source.input
    busy = true
    if (first) state.value = 'loading'

    function fail(message: string) {
      if (id !== generation) return
      stopWorker()
      busy = false
      state.value = 'error'
      error.value = message
    }

    function complete(decoded: Uint8Array) {
      if (id !== generation) return
      stopWorker()
      bytes.value.append(decoded)
      triggerRef(bytes)
      completedChars = end
      busy = false
      state.value = 'ready'
      if (first) revision.value++
      if (source.appendOnly && !source.isBase64 && source.input.length > end) scheduleAppend()
    }

    if (
      input instanceof Uint8Array ||
      estimateDecodedByteLength(input, source.isBase64) < WORKER_THRESHOLD
    ) {
      try {
        complete(decodeHexdumpBytes(input, source.isBase64))
      } catch (cause) {
        fail(cause instanceof Error ? cause.message : String(cause))
      }
      return
    }

    try {
      const currentWorker = createWorker()
      worker = currentWorker
      currentWorker.onmessage = (event) => {
        if (id !== generation || event.data.id !== id || worker !== currentWorker) return
        if (event.data.ok) complete(new Uint8Array(event.data.buffer))
        else fail(event.data.error)
      }
      currentWorker.onerror = (event) => {
        event.preventDefault()
        fail(event.message)
      }
      currentWorker.postMessage({ id, type: 'start', isBase64: source.isBase64 })
      let start = 0
      const sendNext = () => {
        sendTimer = null
        if (id !== generation || worker !== currentWorker) return
        try {
          if (start < input.length) {
            currentWorker.postMessage({
              id,
              type: 'chunk',
              input: input.slice(start, start + MESSAGE_CHUNK_CHARS),
            })
            start += MESSAGE_CHUNK_CHARS
            // Yield between bounded messages without waiting for an animation
            // frame (hidden/minimized windows can stop producing frames).
            sendTimer = setTimeout(sendNext, 0)
          } else currentWorker.postMessage({ id, type: 'end' })
        } catch (cause) {
          fail(cause instanceof Error ? cause.message : String(cause))
        }
      }
      sendNext()
    } catch (cause) {
      // Never fall back to an unbounded decode on the UI thread.
      fail(cause instanceof Error ? cause.message : String(cause))
    }
  }

  watch(
    () => [source.input, source.isBase64, source.active, source.appendOnly] as const,
    (next, previous) => {
      const isAppend =
        previous &&
        next[2] &&
        previous[2] &&
        next[3] &&
        previous[3] &&
        !next[1] &&
        !previous[1] &&
        typeof next[0] === 'string' &&
        next[0].length > previous[0].length
      if (isAppend) {
        scheduleAppend()
        return
      }
      cancel()
      bytes.value = new HexdumpBytes()
      completedChars = 0
      error.value = ''
      state.value = 'idle'
      if (source.active) decode()
    },
    { immediate: true },
  )

  onScopeDispose(() => {
    cancel()
    bytes.value = new HexdumpBytes()
  })
  return { bytes, state, error, revision }
}
