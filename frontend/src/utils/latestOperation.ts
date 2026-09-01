export interface LatestOperationGuard {
  begin: () => number
  invalidate: () => void
  isCurrent: (token: number) => boolean
}

export function createLatestOperationGuard(): LatestOperationGuard {
  let generation = 0

  return {
    begin() {
      generation++
      return generation
    },
    invalidate() {
      generation++
    },
    isCurrent(token) {
      return token === generation
    },
  }
}

export interface OperationGenerationGuard {
  capture: () => number
  invalidate: () => void
  isCurrent: (token: number) => boolean
}

// Unlike createLatestOperationGuard(), capture() does not supersede other
// operations. Every operation in the same generation remains valid until the
// shared resource boundary is invalidated.
export function createOperationGenerationGuard(): OperationGenerationGuard {
  let generation = 0

  return {
    capture() {
      return generation
    },
    invalidate() {
      generation++
    },
    isCurrent(token) {
      return token === generation
    },
  }
}

export interface KeyedLatestOperationToken<Key> {
  key: Key
  generation: number
  scopeGeneration: number
}

export interface KeyedLatestOperationGuard<Key> {
  begin: (key: Key) => KeyedLatestOperationToken<Key>
  invalidate: (key: Key) => void
  invalidateAll: () => void
  isCurrent: (token: KeyedLatestOperationToken<Key>) => boolean
}

export function createKeyedLatestOperationGuard<Key>(): KeyedLatestOperationGuard<Key> {
  let scopeGeneration = 0
  const generations = new Map<Key, number>()

  return {
    begin(key) {
      const generation = (generations.get(key) ?? 0) + 1
      generations.set(key, generation)
      return { key, generation, scopeGeneration }
    },
    invalidate(key) {
      generations.set(key, (generations.get(key) ?? 0) + 1)
    },
    invalidateAll() {
      scopeGeneration++
      generations.clear()
    },
    isCurrent(token) {
      return (
        token.scopeGeneration === scopeGeneration &&
        generations.get(token.key) === token.generation
      )
    },
  }
}
