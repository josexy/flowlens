import type { EditableKeyValue } from '../types/request-editor.js'

export function createEmptyRequestQueryRow(): EditableKeyValue {
  return {
    key: '',
    value: '',
    enabled: true,
  }
}

function getRawRequestURLQuery(url: string): string {
  const fragmentIndex = url.indexOf('#')
  const queryIndex = url.indexOf('?')
  if (queryIndex < 0 || (fragmentIndex >= 0 && queryIndex > fragmentIndex)) {
    return ''
  }
  const queryEnd = fragmentIndex >= 0 ? fragmentIndex : url.length
  return url.slice(queryIndex + 1, queryEnd)
}

export function hasRequestURLQuery(url: string): boolean {
  const fragmentIndex = url.indexOf('#')
  const queryIndex = url.indexOf('?')
  return queryIndex >= 0 && (fragmentIndex < 0 || queryIndex < fragmentIndex)
}

export function parseRequestQueryRows(url: string): EditableKeyValue[] {
  const rows = Array.from(new URLSearchParams(getRawRequestURLQuery(url)), ([key, value]) => ({
    key,
    value,
    enabled: true,
  }))
  return rows.length > 0 ? rows : [createEmptyRequestQueryRow()]
}

export function serializeRequestQueryRows(rows: EditableKeyValue[]): string {
  const query = new URLSearchParams()
  for (const row of rows) {
    const key = row.key.trim()
    if (!row.enabled || !key) {
      continue
    }
    query.append(key, row.value)
  }
  return query.toString()
}

export function countRequestQueryRows(rows: EditableKeyValue[]): number {
  return rows.filter((row) => row.enabled && row.key.trim().length > 0).length
}

export function replaceRequestURLQuery(url: string, rows: EditableKeyValue[]): string {
  const fragmentIndex = url.indexOf('#')
  const fragment = fragmentIndex >= 0 ? url.slice(fragmentIndex) : ''
  const urlWithoutFragment = fragmentIndex >= 0 ? url.slice(0, fragmentIndex) : url
  const queryIndex = urlWithoutFragment.indexOf('?')
  const baseURL = queryIndex >= 0 ? urlWithoutFragment.slice(0, queryIndex) : urlWithoutFragment
  const query = serializeRequestQueryRows(rows)
  return `${baseURL}${query ? `?${query}` : ''}${fragment}`
}
