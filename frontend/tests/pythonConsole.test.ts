import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  appendPythonLogBatch,
  filterPythonLogBatch,
  pythonLogLevelKey,
  pythonLogLineCount,
  pythonLogPreview,
  pythonLogStreamKey,
  pythonLogTone,
} from '../src/utils/pythonConsole.ts'

test('python console maps stream and level metadata', () => {
  const entry = { stream: 'stderr', level: 'warning' }
  assert.equal(pythonLogStreamKey(entry), 'stderr')
  assert.equal(pythonLogLevelKey(entry), 'warning')
  assert.equal(pythonLogTone(entry), 'error')
  assert.equal(pythonLogTone({ stream: 'stdout', level: 'debug' }), 'neutral')
})

test('python console previews multiline output without changing the source', () => {
  assert.equal(pythonLogLineCount('first\r\nsecond\nthird'), 3)
  assert.equal(pythonLogPreview('  first\n  second  '), 'first second')
  assert.equal(pythonLogPreview('abcdefghij', 6), 'abcde…')
  assert.equal(pythonLogPreview(''), '')
})

test('python console batches isolate executions and retain the newest entries', () => {
  const entries = Array.from({ length: 4 }, (_, index) => ({
    eventId: index + 1,
    requestId: 'request',
    executionId: index % 2 === 0 ? 'current' : 'stale',
    pluginId: 'plugin',
    level: 'info',
    stream: 'stdout',
    message: String(index),
    timestamp: index + 1,
  }))
  assert.deepEqual(
    filterPythonLogBatch(entries, 'current').map((entry) => entry.eventId),
    [1, 3],
  )
  const target = entries.slice(0, 2)
  appendPythonLogBatch(target, entries.slice(2), 3)
  assert.deepEqual(
    target.map((entry) => entry.eventId),
    [2, 3, 4],
  )
})
