export type ProcessIconLoader = (key: string) => Promise<string | null>
export type ProcessIconAvailableListener = (key: string, source: string) => void

export class ProcessIconCache {
  // Successful data URLs remain for the window lifetime unless LRU eviction or
  // clear() removes them. Missing results are deliberately not stored, allowing
  // a later component mount to retry the backend's read-triggered recovery.
  private readonly values = new Map<string, string>()
  private readonly pending = new Map<string, Promise<string | null>>()
  private readonly availableListeners = new Set<ProcessIconAvailableListener>()
  private generation = 0

  constructor(private readonly maxSize: number) {}

  async load(key: string, loader: ProcessIconLoader): Promise<string | null> {
    const cached = this.values.get(key)
    if (cached !== undefined) {
      this.setValue(key, cached)
      return cached
    }

    const pending = this.pending.get(key)
    if (pending) {
      return pending
    }

    const requestGeneration = this.generation
    const request = loader(key)
      .then((source) => {
        if (requestGeneration !== this.generation) {
          return null
        }
        if (source !== null) {
          this.setValue(key, source)
          this.notifyAvailable(key, source)
        }
        return source
      })
      .catch(() => null)
      .finally(() => {
        if (requestGeneration === this.generation) {
          this.pending.delete(key)
        }
      })

    this.pending.set(key, request)
    return request
  }

  delete(key: string) {
    this.values.delete(key)
  }

  clear() {
    this.generation++
    this.values.clear()
    this.pending.clear()
  }

  onAvailable(listener: ProcessIconAvailableListener): () => void {
    this.availableListeners.add(listener)
    return () => {
      this.availableListeners.delete(listener)
    }
  }

  private setValue(key: string, value: string) {
    this.values.delete(key)
    this.values.set(key, value)

    while (this.values.size > this.maxSize) {
      const oldestKey = this.values.keys().next().value
      if (oldestKey === undefined) {
        break
      }
      this.values.delete(oldestKey)
    }
  }

  private notifyAvailable(key: string, source: string) {
    for (const listener of this.availableListeners) {
      listener(key, source)
    }
  }
}
