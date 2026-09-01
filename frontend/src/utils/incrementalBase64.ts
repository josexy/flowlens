function encodeBase64Bytes(bytes: Uint8Array): string {
  if (bytes.byteLength === 0) return ''
  const parts: string[] = []
  const chunkSize = 0x8000
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    parts.push(String.fromCharCode(...bytes.subarray(offset, offset + chunkSize)))
  }
  return btoa(parts.join(''))
}

// Keeps only the incomplete 1-2 byte base64 group between appends. Previously
// each stream chunk copied and re-encoded every byte received so far.
export class IncrementalBase64Encoder {
  private encoded = ''
  private remainder = new Uint8Array()
  private totalBytes = 0

  constructor(initial?: Uint8Array) {
    if (initial?.byteLength) this.append(initial)
  }

  get byteLength() {
    return this.totalBytes
  }

  append(bytes: Uint8Array) {
    if (bytes.byteLength === 0) return
    this.totalBytes += bytes.byteLength
    let offset = 0

    if (this.remainder.byteLength > 0) {
      const needed = 3 - this.remainder.byteLength
      const consumed = Math.min(needed, bytes.byteLength)
      const prefix = new Uint8Array(this.remainder.byteLength + consumed)
      prefix.set(this.remainder)
      prefix.set(bytes.subarray(0, consumed), this.remainder.byteLength)
      offset = consumed
      if (prefix.byteLength < 3) {
        this.remainder = prefix
        return
      }
      this.encoded += encodeBase64Bytes(prefix)
      this.remainder = new Uint8Array()
    }

    const completeLength = Math.floor((bytes.byteLength - offset) / 3) * 3
    if (completeLength > 0) {
      this.encoded += encodeBase64Bytes(bytes.subarray(offset, offset + completeLength))
      offset += completeLength
    }
    this.remainder = bytes.slice(offset)
  }

  value() {
    return this.encoded + encodeBase64Bytes(this.remainder)
  }
}
