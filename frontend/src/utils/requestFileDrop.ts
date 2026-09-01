const REQUEST_BODY_FILE_DROP_TARGET = 'request-body-file'
const REQUEST_FORM_DATA_FILE_DROP_TARGET = 'request-form-data-file'

export type RequestFileDropTarget =
  | {
      kind: 'body-file'
      tabKey: string
    }
  | {
      kind: 'form-data-file'
      tabKey: string
      rowId: string
    }

export interface RequestFileDropPayload {
  paths: string[]
  target: RequestFileDropTarget
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function encodePart(value: string): string {
  return encodeURIComponent(value)
}

function decodePart(value: string): string | null {
  try {
    return decodeURIComponent(value) || null
  } catch {
    return null
  }
}

export function buildRequestBodyFileDropTarget(tabKey: string): string {
  return `${REQUEST_BODY_FILE_DROP_TARGET}:${encodePart(tabKey)}`
}

export function buildRequestFormDataFileDropTarget(tabKey: string, rowId: string): string {
  return [REQUEST_FORM_DATA_FILE_DROP_TARGET, encodePart(tabKey), encodePart(rowId)].join(':')
}

function parseTarget(value: unknown): RequestFileDropTarget | null {
  if (typeof value !== 'string') return null

  const parts = value.split(':')
  if (parts[0] === REQUEST_BODY_FILE_DROP_TARGET && parts.length === 2) {
    const tabKey = decodePart(parts[1] ?? '')
    return tabKey ? { kind: 'body-file', tabKey } : null
  }
  if (parts[0] === REQUEST_FORM_DATA_FILE_DROP_TARGET && parts.length === 3) {
    const tabKey = decodePart(parts[1] ?? '')
    const rowId = decodePart(parts[2] ?? '')
    return tabKey && rowId ? { kind: 'form-data-file', tabKey, rowId } : null
  }
  return null
}

export function parseRequestFileDropPayload(value: unknown): RequestFileDropPayload | null {
  const payload = asRecord(value)
  const target = parseTarget(payload?.dataFileDropTarget)
  if (!payload || !target) return null

  return {
    paths: Array.isArray(payload.paths)
      ? payload.paths.filter((path): path is string => typeof path === 'string')
      : [],
    target,
  }
}
