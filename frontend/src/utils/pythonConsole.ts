import type { PluginLogEntry } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'

export type PythonLogTone = 'neutral' | 'info' | 'warning' | 'error'

export function pythonLogStreamKey(entry: Pick<PluginLogEntry, 'stream'>) {
  return entry.stream.trim().toLowerCase() === 'stderr' ? 'stderr' : 'stdout'
}

export function pythonLogLevelKey(entry: Pick<PluginLogEntry, 'level'>) {
  const level = entry.level.trim().toLowerCase()
  if (level === 'warn') {
    return 'warning'
  }
  return ['debug', 'info', 'warning', 'error'].includes(level) ? level : 'info'
}

export function pythonLogTone(entry: Pick<PluginLogEntry, 'level' | 'stream'>): PythonLogTone {
  if (pythonLogStreamKey(entry) === 'stderr' || pythonLogLevelKey(entry) === 'error') {
    return 'error'
  }
  if (pythonLogLevelKey(entry) === 'warning') {
    return 'warning'
  }
  if (pythonLogLevelKey(entry) === 'debug') {
    return 'neutral'
  }
  return 'info'
}

export function pythonLogPreview(message: string, maxLength = 240) {
  const normalized = message
    .replace(/\r?\n|\r/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  if (normalized.length <= maxLength) {
    return normalized
  }
  return `${normalized.slice(0, Math.max(0, maxLength - 1)).trimEnd()}…`
}

export function pythonLogLineCount(message: string) {
  return message.length === 0 ? 0 : message.split(/\r\n|\r|\n/).length
}

export function filterPythonLogBatch(entries: PluginLogEntry[], executionId: string) {
  return entries.filter((entry) => entry.executionId === executionId)
}

export function appendPythonLogBatch(
  target: PluginLogEntry[],
  batch: PluginLogEntry[],
  maxEntries = 1000,
) {
  target.push(...batch)
  const overflow = target.length - maxEntries
  if (overflow > 0) {
    target.splice(0, overflow)
  }
}
