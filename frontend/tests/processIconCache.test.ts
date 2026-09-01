import assert from 'node:assert/strict'
import test from 'node:test'

import { ProcessIconCache } from '../src/components/common/processIconCache.js'

test('retries a missing process icon and notifies existing consumers after recovery', async () => {
  const cache = new ProcessIconCache(256)
  const key = 'a'.repeat(64)
  const expectedSource = 'data:image/png;base64,recovered'
  const notifications: Array<[string, string]> = []
  let available = false
  let loadCalls = 0

  cache.onAvailable((availableKey, source) => {
    notifications.push([availableKey, source])
  })
  const loader = async () => {
    loadCalls++
    return available ? expectedSource : null
  }

  assert.equal(await cache.load(key, loader), null)
  available = true
  assert.equal(await cache.load(key, loader), expectedSource)
  assert.equal(loadCalls, 2)
  assert.deepEqual(notifications, [[key, expectedSource]])
})
