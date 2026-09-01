export interface QueryItem {
  key: string
  hasEquals: boolean
  value: string
}

export interface ParsedHighlightedUrl {
  scheme: string
  host: string
  path: string
  queryItems: QueryItem[]
  hasQuery: boolean
  hash: string
}

export interface QueryParameterField {
  name: string
  value: string
}

export interface ParsedUrlQuery {
  rawQuery: string
  fields: QueryParameterField[]
}

function parsePathQueryHash(input: string) {
  const hashIndex = input.indexOf('#')
  const beforeHash = hashIndex >= 0 ? input.slice(0, hashIndex) : input
  const hash = hashIndex >= 0 ? input.slice(hashIndex) : ''

  const queryIndex = beforeHash.indexOf('?')
  const path = queryIndex >= 0 ? beforeHash.slice(0, queryIndex) : beforeHash
  const queryText = queryIndex >= 0 ? beforeHash.slice(queryIndex + 1) : ''

  const queryItems: QueryItem[] = queryText
    ? queryText.split('&').map((item) => {
        const eqIndex = item.indexOf('=')
        if (eqIndex < 0) {
          return {
            key: item,
            hasEquals: false,
            value: '',
          }
        }
        return {
          key: item.slice(0, eqIndex),
          hasEquals: true,
          value: item.slice(eqIndex + 1),
        }
      })
    : []

  return {
    path,
    queryItems,
    hasQuery: queryIndex >= 0,
    hash,
  }
}

export function parseHighlightedUrl(input: string): ParsedHighlightedUrl {
  const value = input.trim()
  if (!value) {
    return {
      scheme: '',
      host: '',
      path: '',
      queryItems: [],
      hasQuery: false,
      hash: '',
    }
  }

  const match = value.match(/^([a-zA-Z][a-zA-Z\d+\-.]*:\/\/)([^/?#]*)(.*)$/)
  if (!match) {
    const rest = parsePathQueryHash(value)
    return {
      scheme: '',
      host: '',
      path: rest.path,
      queryItems: rest.queryItems,
      hasQuery: rest.hasQuery,
      hash: rest.hash,
    }
  }

  const rest = parsePathQueryHash(match[3] || '')
  return {
    scheme: match[1] || '',
    host: match[2] || '',
    path: rest.path,
    queryItems: rest.queryItems,
    hasQuery: rest.hasQuery,
    hash: rest.hash,
  }
}

export function parseUrlQuery(input: string): ParsedUrlQuery {
  const rawQuery = parseHighlightedUrl(input).queryItems
    .map((item) => `${item.key}${item.hasEquals ? `=${item.value}` : ''}`)
    .join('&')

  return {
    rawQuery,
    fields: Array.from(new URLSearchParams(rawQuery), ([name, value]) => ({ name, value })),
  }
}
