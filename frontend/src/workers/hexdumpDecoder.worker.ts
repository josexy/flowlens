import { decodeHexdumpBytes } from '@/utils/hexdump'

interface HexdumpDecodeRequest {
  id: number
  type?: 'full'
  input: string
  isBase64: boolean
}

type HexdumpDecodeChunkRequest =
  | {
      id: number
      type: 'start'
      isBase64: boolean
    }
  | {
      id: number
      type: 'chunk'
      input: string
    }
  | {
      id: number
      type: 'end'
    }

type HexdumpWorkerRequest = HexdumpDecodeRequest | HexdumpDecodeChunkRequest

type HexdumpDecodeResponse =
  | {
      id: number
      ok: true
      buffer: ArrayBuffer
    }
  | {
      id: number
      ok: false
      error: string
    }

interface WorkerSelf {
  onmessage: ((event: MessageEvent<HexdumpWorkerRequest>) => void) | null
  postMessage(message: HexdumpDecodeResponse, transfer?: Transferable[]): void
}

const workerSelf = self as unknown as WorkerSelf
let activeChunkedRequestId = 0
let activeChunkedIsBase64 = false
let activeChunks: Uint8Array[] = []
let activeByteLength = 0
let pendingInput = ''
let base64Ended = false

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

function toTransferableBuffer(bytes: Uint8Array): ArrayBuffer {
  if (
    bytes.buffer instanceof ArrayBuffer &&
    bytes.byteOffset === 0 &&
    bytes.byteLength === bytes.buffer.byteLength
  ) {
    return bytes.buffer
  }

  const buffer = new ArrayBuffer(bytes.byteLength)
  new Uint8Array(buffer).set(bytes)
  return buffer
}

function decodeAndPost(id: number, input: string, isBase64: boolean) {
  try {
    const bytes = decodeHexdumpBytes(input, isBase64)
    const buffer = toTransferableBuffer(bytes)
    const response: HexdumpDecodeResponse = {
      id,
      ok: true,
      buffer,
    }
    workerSelf.postMessage(response, [buffer])
  } catch (error) {
    const response: HexdumpDecodeResponse = {
      id,
      ok: false,
      error: getErrorMessage(error),
    }
    workerSelf.postMessage(response)
  }
}

workerSelf.onmessage = (event: MessageEvent<HexdumpWorkerRequest>) => {
  const message = event.data

  if (!message.type || message.type === 'full') {
    decodeAndPost(message.id, message.input, message.isBase64)
    return
  }

  if (message.type === 'start') {
    activeChunkedRequestId = message.id
    activeChunkedIsBase64 = message.isBase64
    activeChunks = []
    activeByteLength = 0
    pendingInput = ''
    base64Ended = false
    return
  }

  if (message.id !== activeChunkedRequestId) return

  try {
    if (message.type === 'chunk') {
      // Decode bounded messages as they arrive, retaining bytes rather than a
      // second complete input string plus atob's full binary-string allocation.
      const chunk = activeChunkedIsBase64
        ? message.input.replace(/[\t\n\f\r ]/g, '')
        : message.input
      if (activeChunkedIsBase64 && base64Ended && chunk.length)
        throw new Error('Invalid Base64 padding')
      const input = pendingInput + chunk
      let end = input.length
      if (activeChunkedIsBase64) end -= end % 4
      else if (end && input.charCodeAt(end - 1) >= 0xd800 && input.charCodeAt(end - 1) <= 0xdbff)
        end--
      pendingInput = input.slice(end)
      if (end) {
        const part = input.slice(0, end)
        const bytes = decodeHexdumpBytes(part, activeChunkedIsBase64)
        activeChunks.push(bytes)
        activeByteLength += bytes.length
        base64Ended = activeChunkedIsBase64 && part.includes('=')
        if (base64Ended && pendingInput.length) throw new Error('Invalid Base64 padding')
      }
      return
    }

    if (pendingInput.length) {
      const tail = decodeHexdumpBytes(pendingInput, activeChunkedIsBase64)
      activeChunks.push(tail)
      activeByteLength += tail.length
    }
    const buffer = new ArrayBuffer(activeByteLength)
    const output = new Uint8Array(buffer)
    let offset = 0
    for (const chunk of activeChunks) {
      output.set(chunk, offset)
      offset += chunk.length
    }
    activeChunks = []
    pendingInput = ''
    workerSelf.postMessage({ id: message.id, ok: true, buffer }, [buffer])
  } catch (error) {
    activeChunks = []
    pendingInput = ''
    activeChunkedRequestId = -1
    workerSelf.postMessage({ id: message.id, ok: false, error: getErrorMessage(error) })
  }
}

export {}
