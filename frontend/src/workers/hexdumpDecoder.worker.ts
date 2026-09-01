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
let activeChunks: string[] = []

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
    return
  }

  if (message.id !== activeChunkedRequestId) return

  if (message.type === 'chunk') {
    activeChunks.push(message.input)
    return
  }

  const input = activeChunks.length === 1 ? (activeChunks[0] ?? '') : activeChunks.join('')
  activeChunks = []
  decodeAndPost(message.id, input, activeChunkedIsBase64)
}

export {}
