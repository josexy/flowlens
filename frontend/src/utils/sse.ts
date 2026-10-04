export const SSE_PREVIEW_LENGTH = 160
const PREVIEW_SOURCE_LENGTH = SSE_PREVIEW_LENGTH * 2 + 2

/** Character ranges refer to the unchanged, decoded response body. */
export interface SseEventIndex {
  sequence: number
  eventType: string
  lastEventId: string
  rawStart: number
  rawEnd: number
  preview: string
}

export interface SseMessageDetail extends SseEventIndex {
  data: string
  raw: string
}

function fieldValue(line: string): [string, string] {
  const colon = line.indexOf(':')
  if (colon === -1) return [line, '']
  const start = colon + (line.charAt(colon + 1) === ' ' ? 2 : 1)
  return [line.slice(0, colon), line.slice(start)]
}

function messagePreview(prefix: string, dataLength: number): string {
  const characters = Array.from(prefix.slice(0, dataLength).replaceAll('\n', ' ↵ '))
  if (dataLength > prefix.length || characters.length > SSE_PREVIEW_LENGTH) {
    return characters.slice(0, SSE_PREVIEW_LENGTH - 1).join('') + '…'
  }
  return characters.join('')
}

/** Incremental WHATWG event-stream parser. Payloads are indexed, not retained. */
export class SseParser {
  private offset = 0
  private rawStart = 0
  private lineParts: string[] = []
  private skipLF = false
  private crBoundary = false
  private crEvent: SseEventIndex | null = null
  private blockHasContent = false
  private eventType = ''
  private lastEventId = ''
  private hasData = false
  private dataLength = 0
  private previewPrefix = ''
  private sequence = 0

  get hasPendingEvent(): boolean {
    return this.blockHasContent
  }

  append(chunk: string): SseEventIndex[] {
    const events: SseEventIndex[] = []
    let segmentStart = 0
    for (let index = 0; index < chunk.length; index++, this.offset++) {
      const code = chunk.charCodeAt(index)
      if (this.skipLF) {
        this.skipLF = false
        if (code === 10) {
          // CR dispatches immediately; a later LF still belongs to its raw block.
          if (this.crBoundary) this.rawStart = this.offset + 1
          if (this.crEvent) this.crEvent.rawEnd = this.offset + 1
          this.crEvent = null
          segmentStart = index + 1
          continue
        }
        this.crEvent = null
      }
      if (this.offset === 0 && code === 0xfeff) {
        segmentStart = index + 1
        continue
      }
      if (code !== 10 && code !== 13) {
        this.blockHasContent = true
        continue
      }
      this.lineParts.push(chunk.slice(segmentStart, index))
      const line = this.lineParts.join('')
      this.lineParts = []
      segmentStart = index + 1
      const event = this.processLine(line, this.offset + 1)
      if (event) events.push(event)
      this.skipLF = code === 13
      this.crBoundary = line.length === 0
      this.crEvent = this.skipLF ? event : null
    }
    if (segmentStart < chunk.length) this.lineParts.push(chunk.slice(segmentStart))
    return events
  }

  private processLine(line: string, rawEnd: number): SseEventIndex | null {
    if (line.length === 0) {
      const event: SseEventIndex | null = this.hasData
        ? {
            sequence: ++this.sequence,
            eventType: this.eventType || 'message',
            lastEventId: this.lastEventId,
            rawStart: this.rawStart,
            rawEnd,
            preview: messagePreview(this.previewPrefix, this.dataLength - 1),
          }
        : null
      this.rawStart = rawEnd
      this.blockHasContent = false
      this.eventType = ''
      this.hasData = false
      this.dataLength = 0
      this.previewPrefix = ''
      return event
    }
    if (line.startsWith(':')) return null
    const [field, value] = fieldValue(line)
    if (field === 'data') {
      this.hasData = true
      this.dataLength += value.length + 1
      const remaining = PREVIEW_SOURCE_LENGTH - this.previewPrefix.length
      if (remaining > 0) {
        this.previewPrefix += value.slice(0, remaining)
        if (value.length < remaining) this.previewPrefix += '\n'
      }
    } else if (field === 'event') {
      this.eventType = value
    } else if (field === 'id' && !value.includes('\0')) {
      this.lastEventId = value
    }
    // retry does not dispatch data or reconnect this response viewer.
    return null
  }
}

/** Materialize just the selected event, preserving its original wire text. */
export function getSseMessageDetail(body: string, event: SseEventIndex): SseMessageDetail {
  const raw = body.slice(event.rawStart, event.rawEnd)
  const data: string[] = []
  let start = raw.charCodeAt(0) === 0xfeff ? 1 : 0
  for (let index = start; index <= raw.length; index++) {
    const code = raw.charCodeAt(index)
    if (index !== raw.length && code !== 10 && code !== 13) continue
    const [field, value] = fieldValue(raw.slice(start, index))
    if (field === 'data') data.push(value)
    if (code === 13 && raw.charCodeAt(index + 1) === 10) index++
    start = index + 1
  }
  return { ...event, data: data.join('\n'), raw }
}
