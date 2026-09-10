import { computed, shallowRef, watch } from 'vue'
import { estimateDecodedByteLength } from '../utils/hexdump.js'

interface BodyHexSource {
  body: string
  bodyEncoding?: string
  contentType?: string
  active: boolean
}

function isSSE(contentType: string | undefined) {
  return contentType?.split(';')[0]?.trim().toLowerCase() === 'text/event-stream'
}

/** Only scalar reading state survives unmounting the expensive viewer. */
export function useHexdumpViewState(source: BodyHexSource) {
  const requested = shallowRef(false)
  const byteOffset = shallowRef(0)
  const isLarge = computed(
    () => estimateDecodedByteLength(source.body, source.bodyEncoding === 'base64') >= 1024 * 1024,
  )
  const gated = computed(() => isLarge.value && !requested.value)
  const canRender = computed(() => source.active && source.body.length > 0 && !gated.value)

  watch(
    () => [source.body, source.bodyEncoding, source.contentType, source.active] as const,
    (next, previous) => {
      const bodyChanged =
        previous && (next[0] !== previous[0] || next[1] !== previous[1] || next[2] !== previous[2])
      const sameStreamAppend =
        previous &&
        isSSE(next[2]) &&
        isSSE(previous[2]) &&
        next[1] === previous[1] &&
        previous[0].length > 0 &&
        next[0].length > previous[0].length
      if (bodyChanged && !sameStreamAppend) {
        requested.value = false
        byteOffset.value = 0
      }
      // Once opened, crossing the size threshold must not revoke the view.
      if (source.active && source.body.length > 0 && !isLarge.value) requested.value = true
    },
    { immediate: true },
  )

  return {
    byteOffset,
    gated,
    canRender,
    load: () => {
      requested.value = true
    },
  }
}
