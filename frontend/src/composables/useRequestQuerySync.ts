import { computed, nextTick, watch, type Ref } from 'vue'
import type { EditableKeyValue } from '../types/request-editor.js'
import {
  countRequestQueryRows,
  hasRequestURLQuery,
  parseRequestQueryRows,
  replaceRequestURLQuery,
  serializeRequestQueryRows,
} from '../utils/requestQuery.js'

export function useRequestQuerySync(
  url: Ref<string>,
  rows: Ref<EditableKeyValue[]>,
) {
  let syncingRowsFromURL = false
  let syncGeneration = 0

  function syncRowsFromURL(nextURL: string) {
    const nextRows = parseRequestQueryRows(nextURL)
    if (serializeRequestQueryRows(nextRows) === serializeRequestQueryRows(rows.value)) {
      return
    }
    syncingRowsFromURL = true
    syncGeneration += 1
    const currentGeneration = syncGeneration
    rows.value = nextRows
    void nextTick(() => {
      if (syncGeneration === currentGeneration) {
        syncingRowsFromURL = false
      }
    })
  }

  if (hasRequestURLQuery(url.value)) {
    syncRowsFromURL(url.value)
  } else {
    const nextURL = replaceRequestURLQuery(url.value, rows.value)
    if (nextURL !== url.value) {
      url.value = nextURL
    }
  }

  watch(url, syncRowsFromURL)
  watch(
    rows,
    (nextRows) => {
      if (syncingRowsFromURL) {
        return
      }
      const nextURL = replaceRequestURLQuery(url.value, nextRows)
      if (nextURL !== url.value) {
        url.value = nextURL
      }
    },
    { deep: true },
  )

  return {
    queryCount: computed(() => countRequestQueryRows(rows.value)),
  }
}
