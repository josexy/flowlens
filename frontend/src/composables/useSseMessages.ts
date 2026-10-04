import { onScopeDispose, ref, shallowRef, triggerRef, watch } from 'vue'
import { SseParser, type SseEventIndex } from '../utils/sse'

const PARSE_CHUNK_CHARS = 128 * 1024

interface SseSource {
  body: string
  bodyEncoding?: string
  active: boolean
}

export function useSseMessages(source: SseSource) {
  const messages = shallowRef<SseEventIndex[]>([])
  const pending = ref(false)
  const parsing = ref(false)
  const revision = ref(0)
  let parser = new SseParser()
  let observedBody = ''
  let consumed = 0
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | null = null

  function cancel() {
    generation++
    if (timer !== null) clearTimeout(timer)
    timer = null
    parsing.value = false
  }

  function reset() {
    cancel()
    parser = new SseParser()
    consumed = 0
    messages.value = []
    pending.value = false
    revision.value++
  }

  function parseBatch() {
    timer = null
    if (!source.active || source.bodyEncoding === 'base64') return
    const end = Math.min(source.body.length, consumed + PARSE_CHUNK_CHARS)
    const events = parser.append(source.body.slice(consumed, end))
    consumed = end
    for (const event of events) messages.value.push(event)
    // A CRLF split across updates may extend the last event's raw range.
    triggerRef(messages)
    pending.value = parser.hasPendingEvent
    parsing.value = consumed < source.body.length
    if (parsing.value) {
      const currentGeneration = generation
      timer = setTimeout(() => {
        if (generation === currentGeneration) parseBatch()
      }, 0)
    }
  }

  watch(
    () => [source.body, source.bodyEncoding, source.active] as const,
    ([body, encoding, active], previous) => {
      if (encoding !== previous?.[1] || !body.startsWith(observedBody)) reset()
      observedBody = body
      if (!active || encoding === 'base64') {
        cancel()
      } else if (timer === null) {
        parseBatch()
      }
    },
    { immediate: true },
  )

  onScopeDispose(cancel)
  return { messages, pending, parsing, revision }
}
