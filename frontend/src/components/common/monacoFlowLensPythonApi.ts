import type * as Monaco from 'monaco-editor'

type MonacoApi = typeof Monaco

export const FLOWLENS_PYTHON_API_LANGUAGE_ID = 'flowlens-python-api'

// Keep this catalog aligned with Python SDK API v1 in
// backend/services/python_plugin_service/runtime/flowlens/__init__.py.

type ApiTypeName =
  | 'Body'
  | 'BodySnapshot'
  | 'Context'
  | 'ContextLogger'
  | 'FileDescriptor'
  | 'FileDescriptorClass'
  | 'FileSnapshot'
  | 'FrozenHeaders'
  | 'FrozenQueries'
  | 'HeaderField'
  | 'Headers'
  | 'JsonObject'
  | 'MultipartPart'
  | 'MultipartPartSnapshot'
  | 'Queries'
  | 'QueryField'
  | 'ReadonlyMapping'
  | 'Request'
  | 'RequestSnapshot'
  | 'Response'
  | 'URLEncodedField'
  | 'URLEncodedFieldSnapshot'

type ApiMemberKind = 'method' | 'property'

interface ApiMember {
  name: string
  kind: ApiMemberKind
  signature: string
  insertText: string
  documentation: string
  parameters?: readonly string[]
  resultType?: ApiTypeName
}

interface ApiConstructor {
  name: string
  signature: string
  documentation: string
  parameters: readonly string[]
}

export interface FlowLensPythonCompletion {
  name: string
  kind: ApiMemberKind | 'class' | 'snippet' | 'variable'
  detail: string
  insertText: string
  documentation: string
  snippet: boolean
}

export interface FlowLensPythonCompletionResult {
  replaceLength: number
  items: FlowLensPythonCompletion[]
}

export interface FlowLensPythonSignature {
  label: string
  documentation: string
  parameters: readonly string[]
  activeParameter: number
}

export interface FlowLensPythonHover {
  signature: string
  documentation: string
}

function property(
  name: string,
  type: string,
  documentation: string,
  resultType?: ApiTypeName,
  writable = false,
): ApiMember {
  return {
    name,
    kind: 'property',
    signature: `${name}: ${type}${writable ? '' : '  # read-only'}`,
    insertText: name,
    documentation,
    resultType,
  }
}

function method(
  name: string,
  parameters: readonly string[],
  returnType: string,
  insertText: string,
  documentation: string,
  resultType?: ApiTypeName,
): ApiMember {
  return {
    name,
    kind: 'method',
    signature: `${name}(${parameters.join(', ')}) -> ${returnType}`,
    insertText,
    documentation,
    parameters,
    resultType,
  }
}

const API_MEMBERS: Record<ApiTypeName, readonly ApiMember[]> = {
  Context: [
    property('id', 'str', 'Execution ID.'),
    property('timestamp', 'int', 'Execution timestamp.'),
    property(
      'original_url',
      'str',
      'Original URL before the hook chain.',
    ),
    property(
      'original_method',
      'str',
      'Original method before the hook chain.',
    ),
    property('plugin_id', 'str', 'Current plugin ID.'),
    property('plugin_name', 'str', 'Current plugin name.'),
    property(
      'params',
      'Mapping[str, Any]',
      'Deeply read-only parameters for the current plugin.',
      'ReadonlyMapping',
    ),
    property(
      'transport',
      'Mapping[str, Any]',
      'Read-only transport settings for this script execution.',
      'ReadonlyMapping',
    ),
    property(
      'shared',
      'dict[str, Any]',
      'JSON object shared between request and response hooks of this plugin.',
      'JsonObject',
      true,
    ),
    property(
      'log',
      'ContextLogger',
      'Structured logger for the current execution console.',
      'ContextLogger',
    ),
  ],
  ContextLogger: [
    method(
      'debug',
      ['message: Any'],
      'None',
      'debug(${1:message})',
      'Log a debug message.',
    ),
    method(
      'info',
      ['message: Any'],
      'None',
      'info(${1:message})',
      'Log an informational message.',
    ),
    method(
      'warning',
      ['message: Any'],
      'None',
      'warning(${1:message})',
      'Log a warning message.',
    ),
    method(
      'error',
      ['message: Any'],
      'None',
      'error(${1:message})',
      'Log an error message.',
    ),
  ],
  Request: [
    property('method', 'str', 'Mutable request method.', undefined, true),
    property('url', 'str', 'Mutable request URL.', undefined, true),
    property('scheme', 'str', 'Read-only request URL scheme.'),
    property('host', 'str', 'Read-only request URL host.'),
    property('port', 'int | None', 'Read-only request URL port.'),
    property('path', 'str', 'Mutable request URL path.', undefined, true),
    property(
      'queries',
      'Queries',
      'Mutable query parameters preserving order and duplicates.',
      'Queries',
      true,
    ),
    property(
      'content_type',
      'str | None',
      'Read-only Content-Type view; the outgoing value is generated from the final request body kind.',
    ),
    property(
      'headers',
      'Headers',
      'Mutable ordered request headers.',
      'Headers',
      true,
    ),
    property(
      'body',
      'Body',
      'Request body; direct replacement is supported.',
      'Body',
      true,
    ),
  ],
  RequestSnapshot: [
    property(
      'method',
      'str',
      'Read-only request method associated with the response.',
    ),
    property(
      'url',
      'str',
      'Read-only request URL associated with the response.',
    ),
    property('scheme', 'str', 'Read-only request URL scheme.'),
    property('host', 'str', 'Read-only request URL host.'),
    property('port', 'int | None', 'Read-only request URL port.'),
    property('path', 'str', 'Read-only request URL path.'),
    property(
      'queries',
      'FrozenQueries',
      'Read-only query parameters preserving order and duplicates.',
      'FrozenQueries',
    ),
    property(
      'content_type',
      'str | None',
      'Content-Type of the associated request.',
    ),
    property(
      'headers',
      'FrozenHeaders',
      'Read-only request headers associated with the response.',
      'FrozenHeaders',
    ),
    property(
      'body',
      'BodySnapshot',
      'Semantic read-only request body snapshot associated with the response.',
      'BodySnapshot',
    ),
  ],
  Response: [
    property(
      'code',
      'int',
      'Mutable response status code.',
      undefined,
      true,
    ),
    property(
      'protocol',
      'str',
      'Read-only protocol actually used by the upstream response.',
    ),
    property(
      'status_text',
      'str',
      'Read-only status text including the code; updated when code changes.',
    ),
    property(
      'content_type',
      'str | None',
      'Read-only Content-Type view; modify response headers explicitly when needed.',
    ),
    property(
      'headers',
      'Headers',
      'Mutable ordered response headers.',
      'Headers',
      true,
    ),
    property(
      'trailers',
      'Headers',
      'Mutable ordered response trailers.',
      'Headers',
      true,
    ),
    property(
      'body',
      'Body',
      'Response body; unavailable for SSE responses.',
      'Body',
      true,
    ),
    property(
      'request',
      'RequestSnapshot | None',
      'Read-only snapshot of the request that produced this response.',
      'RequestSnapshot',
    ),
  ],
  Headers: [
    method(
      'get',
      ['name: str', 'default: Any = None'],
      'Any',
      "get('${1:name}')",
      'Return the first header with this name.',
    ),
    method(
      'get_all',
      ['name: str'],
      'list[str]',
      "get_all('${1:name}')",
      'Return all matching headers in wire order.',
    ),
    method(
      'set',
      ['name: str', 'value: str'],
      'None',
      "set('${1:name}', '${2:value}')",
      'Replace all matching headers while preserving the first position.',
    ),
    method(
      'add',
      ['name: str', 'value: str'],
      'None',
      "add('${1:name}', '${2:value}')",
      'Append a header field.',
    ),
    method(
      'remove',
      ['name: str'],
      'None',
      "remove('${1:name}')",
      'Remove all matching headers.',
    ),
    method('clear', [], 'None', 'clear()', 'Remove all headers.'),
  ],
  FrozenHeaders: [
    method(
      'get',
      ['name: str', 'default: Any = None'],
      'Any',
      "get('${1:name}')",
      'Return the first header with this name.',
    ),
    method(
      'get_all',
      ['name: str'],
      'list[str]',
      "get_all('${1:name}')",
      'Return all matching headers in wire order.',
    ),
  ],
  Queries: [
    method(
      'get',
      ['name: str', 'default: Any = None'],
      'Any',
      "get('${1:name}')",
      'Return the first query value with this name.',
    ),
    method(
      'get_all',
      ['name: str'],
      'list[str]',
      "get_all('${1:name}')",
      'Return all matching query values in order.',
    ),
    method(
      'set',
      ['name: str', 'value: str'],
      'None',
      "set('${1:name}', '${2:value}')",
      'Replace all matching query values while preserving the first position.',
    ),
    method(
      'add',
      ['name: str', 'value: str'],
      'None',
      "add('${1:name}', '${2:value}')",
      'Append a query field.',
    ),
    method(
      'remove',
      ['name: str'],
      'None',
      "remove('${1:name}')",
      'Remove all matching query fields.',
    ),
    method('clear', [], 'None', 'clear()', 'Remove all query fields.'),
    method(
      'to_string',
      [],
      'str',
      'to_string()',
      'Encode as a URL query string.',
    ),
  ],
  FrozenQueries: [
    method(
      'get',
      ['name: str', 'default: Any = None'],
      'Any',
      "get('${1:name}')",
      'Return the first query value with this name.',
    ),
    method(
      'get_all',
      ['name: str'],
      'list[str]',
      "get_all('${1:name}')",
      'Return all matching query values in order.',
    ),
    method(
      'to_string',
      [],
      'str',
      'to_string()',
      'Return the original URL query string.',
    ),
  ],
  Body: [
    property('kind', 'str', 'Read-only semantic body kind.'),
    property('value', 'Any', 'Read-only materialized body value.'),
    method(
      'write_file',
      ['path: str'],
      'None',
      "write_file('${1:absolute_path}')",
      'Stream a regular body to an absolute path.',
    ),
  ],
  BodySnapshot: [
    property('kind', 'str', 'Read-only semantic body kind.'),
    property(
      'value',
      'Any',
      'Read-only semantic body value; file values do not expose internal paths.',
    ),
    method(
      'write_file',
      ['path: str'],
      'None',
      "write_file('${1:absolute_path}')",
      'Stream a regular body snapshot to an absolute path.',
    ),
  ],
  QueryField: [
    property('name', 'str', 'Query field name.', undefined, true),
    property('value', 'str', 'Query field value.', undefined, true),
  ],
  HeaderField: [
    property('name', 'str', 'Header name.', undefined, true),
    property('value', 'str', 'Header value.', undefined, true),
  ],
  FileDescriptor: [
    property('path', 'str', 'Absolute file path.'),
    property('name', 'str', 'File name.'),
    property('size', 'int', 'File size in bytes.'),
    property('read_only', 'bool', 'Whether the file is read-only.'),
  ],
  FileSnapshot: [
    property('name', 'str', 'Read-only original file name.'),
    property('size', 'int', 'Read-only file size in bytes.'),
  ],
  FileDescriptorClass: [
    method(
      'from_file',
      ['path: str'],
      'FileDescriptor',
      "from_file('${1:absolute_path}')",
      'Create a safe file descriptor from an absolute path.',
      'FileDescriptor',
    ),
  ],
  URLEncodedField: [
    property('enabled', 'bool', 'Whether the field is enabled.', undefined, true),
    property('name', 'str', 'Field name.', undefined, true),
    property('value', 'str', 'Field value.', undefined, true),
  ],
  URLEncodedFieldSnapshot: [
    property('enabled', 'bool', 'Whether the field is enabled.'),
    property('name', 'str', 'Read-only field name.'),
    property('value', 'str', 'Read-only field value.'),
  ],
  MultipartPart: [
    property('enabled', 'bool', 'Whether the part is enabled.', undefined, true),
    property('name', 'str', 'Part name.', undefined, true),
    property('value', 'str', 'Text part value.', undefined, true),
    property(
      'file',
      'FileDescriptor | None',
      'Descriptor for a file part.',
      'FileDescriptor',
      true,
    ),
    property(
      'filename',
      'str',
      'Independent filename used by Content-Disposition.',
      undefined,
      true,
    ),
  ],
  MultipartPartSnapshot: [
    property('enabled', 'bool', 'Whether the part is enabled.'),
    property('name', 'str', 'Read-only part name.'),
    property('value', 'str', 'Read-only text part value.'),
    property(
      'file',
      'FileSnapshot | None',
      'File snapshot without an exposed internal path.',
      'FileSnapshot',
    ),
    property('filename', 'str', 'Read-only part filename.'),
  ],
  ReadonlyMapping: [
    method(
      'get',
      ['key: str', 'default: Any = None'],
      'Any',
      "get('${1:key}')",
      'Read a value by key.',
    ),
    method('keys', [], 'KeysView[str]', 'keys()', 'Return all keys.'),
    method(
      'items',
      [],
      'ItemsView[str, Any]',
      'items()',
      'Return all key-value pairs.',
    ),
    method('values', [], 'ValuesView[Any]', 'values()', 'Return all values.'),
  ],
  JsonObject: [
    method(
      'get',
      ['key: str', 'default: Any = None'],
      'Any',
      "get('${1:key}')",
      'Read a shared value.',
    ),
    method('keys', [], 'dict_keys[str]', 'keys()', 'Return all shared keys.'),
    method(
      'items',
      [],
      'dict_items[str, Any]',
      'items()',
      'Return all shared key-value pairs.',
    ),
    method(
      'values',
      [],
      'dict_values[Any]',
      'values()',
      'Return all shared values.',
    ),
    method(
      'setdefault',
      ['key: str', 'default: Any = None'],
      'Any',
      "setdefault('${1:key}', ${2:None})",
      'Read a key or insert its default value.',
    ),
    method(
      'update',
      ['other: Mapping[str, Any]'],
      'None',
      'update(${1:values})',
      'Update shared values.',
    ),
    method(
      'pop',
      ['key: str', 'default: Any = None'],
      'Any',
      "pop('${1:key}')",
      'Remove and return a shared value.',
    ),
    method('clear', [], 'None', 'clear()', 'Remove all shared values.'),
  ],
}

const API_CONSTRUCTORS: readonly ApiConstructor[] = [
  {
    name: 'Body',
    signature: 'Body(kind: str = "none", value: Any = None)',
    documentation: 'Create a body with an explicit semantic kind.',
    parameters: ['kind: str = "none"', 'value: Any = None'],
  },
  {
    name: 'FileDescriptor',
    signature: 'FileDescriptor(path: str, name: str = "", size: int = -1, read_only: bool = True)',
    documentation:
      'Describe a FlowLens-managed file; prefer from_file() for user files.',
    parameters: ['path: str', 'name: str = ""', 'size: int = -1', 'read_only: bool = True'],
  },
  {
    name: 'HeaderField',
    signature: 'HeaderField(name: str, value: str)',
    documentation: 'Create an ordered header field.',
    parameters: ['name: str', 'value: str'],
  },
  {
    name: 'Queries',
    signature: 'Queries(fields: Iterable[QueryField | tuple[str, str]] | None = None)',
    documentation:
      'Create query parameters that preserve order and duplicates.',
    parameters: ['fields: Iterable[QueryField | tuple[str, str]] | None = None'],
  },
  {
    name: 'QueryField',
    signature: 'QueryField(name: str, value: str)',
    documentation: 'Create a query field.',
    parameters: ['name: str', 'value: str'],
  },
  {
    name: 'Headers',
    signature: 'Headers(fields: Iterable[HeaderField] | None = None)',
    documentation:
      'Create headers that preserve order and duplicates.',
    parameters: ['fields: Iterable[HeaderField] | None = None'],
  },
  {
    name: 'MultipartPart',
    signature:
      'MultipartPart(name: str, value: str = "", file: FileDescriptor | None = None, enabled: bool = True, filename: str = "")',
    documentation: 'Create a multipart text or file part.',
    parameters: [
      'name: str',
      'value: str = ""',
      'file: FileDescriptor | None = None',
      'enabled: bool = True',
      'filename: str = ""',
    ],
  },
  {
    name: 'Request',
    signature: 'Request(method: str, url: str, headers: Headers | None = None, body: Any = None)',
    documentation:
      'Create a request object. Hooks normally modify the provided request.',
    parameters: ['method: str', 'url: str', 'headers: Headers | None = None', 'body: Any = None'],
  },
  {
    name: 'Response',
    signature:
      'Response(code: int, headers: Headers | None = None, trailers: Headers | None = None, body: Any = None)',
    documentation:
      'Create a response object. Hooks normally modify the provided response.',
    parameters: [
      'code: int',
      'headers: Headers | None = None',
      'trailers: Headers | None = None',
      'body: Any = None',
    ],
  },
  {
    name: 'URLEncodedField',
    signature: 'URLEncodedField(name: str, value: str, enabled: bool = True)',
    documentation: 'Create a URL-encoded body field.',
    parameters: ['name: str', 'value: str', 'enabled: bool = True'],
  },
]

const CLASS_RECEIVER_TYPES: Readonly<Record<string, ApiTypeName>> = {
  FileDescriptor: 'FileDescriptorClass',
}

const DEFAULT_BINDINGS: Readonly<Record<string, ApiTypeName>> = {
  context: 'Context',
  request: 'Request',
  response: 'Response',
}

const registeredMonacoInstances = new WeakSet<object>()

export function registerFlowLensPythonApi(monaco: MonacoApi) {
  const registrationKey = monaco as unknown as object
  if (registeredMonacoInstances.has(registrationKey)) {
    return
  }
  registeredMonacoInstances.add(registrationKey)

  monaco.languages.registerCompletionItemProvider(FLOWLENS_PYTHON_API_LANGUAGE_ID, {
    triggerCharacters: ['.'],
    provideCompletionItems(model, position) {
      const completions = getFlowLensPythonCompletions(
        model.getValue(),
        model.getOffsetAt(position),
      )
      const range = {
        startLineNumber: position.lineNumber,
        startColumn: Math.max(1, position.column - completions.replaceLength),
        endLineNumber: position.lineNumber,
        endColumn: position.column,
      }
      return {
        suggestions: completions.items.map((item, index) => ({
          label: item.name,
          kind: completionKind(monaco, item.kind),
          detail: item.detail,
          documentation: item.documentation,
          insertText: item.insertText,
          insertTextRules: item.snippet
            ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet
            : undefined,
          range,
          sortText: `${String(index).padStart(3, '0')}-${item.name}`,
        })),
      }
    },
  })

  monaco.languages.registerSignatureHelpProvider(FLOWLENS_PYTHON_API_LANGUAGE_ID, {
    signatureHelpTriggerCharacters: ['(', ','],
    signatureHelpRetriggerCharacters: [','],
    provideSignatureHelp(model, position) {
      const signature = getFlowLensPythonSignature(model.getValue(), model.getOffsetAt(position))
      if (!signature) {
        return null
      }
      return {
        value: {
          signatures: [
            {
              label: signature.label,
              documentation: signature.documentation,
              parameters: signature.parameters.map((label) => ({ label })),
            },
          ],
          activeSignature: 0,
          activeParameter: signature.activeParameter,
        },
        dispose() {},
      }
    },
  })

  monaco.languages.registerHoverProvider(FLOWLENS_PYTHON_API_LANGUAGE_ID, {
    provideHover(model, position) {
      const hover = getFlowLensPythonHover(model.getValue(), model.getOffsetAt(position))
      if (!hover) {
        return null
      }
      return {
        contents: [
          { value: `\`\`\`python\n${hover.signature}\n\`\`\`` },
          { value: hover.documentation },
        ],
      }
    },
  })
}

export function getFlowLensPythonCompletions(
  source: string,
  offset: number,
): FlowLensPythonCompletionResult {
  const safeOffset = clampOffset(source, offset)
  const lineStart = source.lastIndexOf('\n', safeOffset - 1) + 1
  const linePrefix = source.slice(lineStart, safeOffset)
  const memberMatch = linePrefix.match(
    /([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\.\s*([A-Za-z_]\w*)?$/,
  )
  if (memberMatch) {
    const expression = memberMatch[1].replace(/\s+/g, '')
    const partial = memberMatch[2] ?? ''
    const bindings = collectBindings(source)
    const ownerType = resolveExpressionType(expression, bindings)
    const items = ownerType
      ? API_MEMBERS[ownerType]
          .filter((item) => item.name.toLocaleLowerCase().startsWith(partial.toLocaleLowerCase()))
          .map((item) => memberCompletion(ownerType, item))
      : []
    return { replaceLength: partial.length, items }
  }

  const wordMatch = linePrefix.match(/([A-Za-z_]\w*)$/)
  const partial = wordMatch?.[1] ?? ''
  return {
    replaceLength: partial.length,
    items: topLevelCompletions(source).filter((item) =>
      item.name.toLocaleLowerCase().startsWith(partial.toLocaleLowerCase()),
    ),
  }
}

export function getFlowLensPythonSignature(
  source: string,
  offset: number,
): FlowLensPythonSignature | null {
  const prefix = source.slice(0, clampOffset(source, offset))
  const openParen = findActiveCallOpenParen(prefix)
  if (openParen < 0) {
    return null
  }
  const callableMatch = prefix
    .slice(0, openParen)
    .match(/([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*$/)
  if (!callableMatch) {
    return null
  }

  const callable = callableMatch[1].replace(/\s+/g, '')
  const parts = callable.split('.')
  let label = ''
  let documentation = ''
  let parameters: readonly string[] = []
  if (parts.length === 1) {
    const constructor = API_CONSTRUCTORS.find((item) => item.name === parts[0])
    if (!constructor) {
      return null
    }
    label = constructor.signature
    documentation = constructor.documentation
    parameters = constructor.parameters
  } else {
    const memberName = parts.at(-1)!
    const ownerExpression = parts.slice(0, -1).join('.')
    const ownerType = resolveExpressionType(ownerExpression, collectBindings(source))
    const member = ownerType
      ? API_MEMBERS[ownerType].find((item) => item.kind === 'method' && item.name === memberName)
      : undefined
    if (!ownerType || !member) {
      return null
    }
    label = `${ownerType}.${member.signature}`
    documentation = member.documentation
    parameters = member.parameters ?? []
  }

  const activeParameter = countTopLevelCommas(prefix.slice(openParen + 1))
  return {
    label,
    documentation,
    parameters,
    activeParameter: parameters.length > 0 ? Math.min(activeParameter, parameters.length - 1) : 0,
  }
}

export function getFlowLensPythonHover(source: string, offset: number): FlowLensPythonHover | null {
  if (!source) {
    return null
  }
  let cursor = Math.min(clampOffset(source, offset), source.length - 1)
  if (
    !isIdentifierCharacter(source[cursor]) &&
    cursor > 0 &&
    isIdentifierCharacter(source[cursor - 1])
  ) {
    cursor -= 1
  }
  if (!isIdentifierCharacter(source[cursor])) {
    return null
  }

  let wordStart = cursor
  while (wordStart > 0 && isIdentifierCharacter(source[wordStart - 1])) {
    wordStart -= 1
  }
  let expressionStart = wordStart
  while (expressionStart > 0) {
    let dot = expressionStart - 1
    while (dot >= 0 && /\s/.test(source[dot])) dot -= 1
    if (dot < 0 || source[dot] !== '.') break
    let previousEnd = dot - 1
    while (previousEnd >= 0 && /\s/.test(source[previousEnd])) previousEnd -= 1
    if (previousEnd < 0 || !isIdentifierCharacter(source[previousEnd])) break
    let previousStart = previousEnd
    while (previousStart > 0 && isIdentifierCharacter(source[previousStart - 1])) previousStart -= 1
    expressionStart = previousStart
  }
  let wordEnd = cursor + 1
  while (wordEnd < source.length && isIdentifierCharacter(source[wordEnd])) {
    wordEnd += 1
  }
  const expression = source.slice(expressionStart, wordEnd).replace(/\s+/g, '')
  const parts = expression.split('.')
  const bindings = collectBindings(source)
  if (parts.length === 1) {
    const type = bindings.get(parts[0])
    if (type) {
      return {
        signature: `${parts[0]}: ${type}`,
        documentation: `FlowLens ${type} API object.`,
      }
    }
    const constructor = API_CONSTRUCTORS.find((item) => item.name === parts[0])
    return constructor
      ? { signature: constructor.signature, documentation: constructor.documentation }
      : null
  }

  const memberName = parts.at(-1)!
  const ownerExpression = parts.slice(0, -1).join('.')
  const ownerType = resolveExpressionType(ownerExpression, bindings)
  const member = ownerType
    ? API_MEMBERS[ownerType].find((item) => item.name === memberName)
    : undefined
  if (!ownerType || !member) {
    return null
  }
  return {
    signature: `${member.kind === 'method' ? '(method)' : '(property)'} ${ownerType}.${member.signature}`,
    documentation: member.documentation,
  }
}

function collectBindings(source: string) {
  const bindings = new Map<string, ApiTypeName>(Object.entries(DEFAULT_BINDINGS))
  const hookPattern = /^\s*(?:async\s+)?def\s+(onRequest|onResponse)\s*\(([^)]*)\)/gm
  for (const match of source.matchAll(hookPattern)) {
    const parameters = match[2].split(',').map(parameterName).filter(Boolean)
    if (parameters[0]) bindings.set(parameters[0], 'Context')
    if (parameters[1])
      bindings.set(parameters[1], match[1] === 'onRequest' ? 'Request' : 'Response')
  }

  const annotationPattern =
    /\b([A-Za-z_]\w*)\s*:\s*(Context|Request|Response|RequestSnapshot|Headers|FrozenHeaders|Queries|FrozenQueries|Body|BodySnapshot|HeaderField|QueryField|FileDescriptor|FileSnapshot|URLEncodedField|URLEncodedFieldSnapshot|MultipartPart|MultipartPartSnapshot)\b/g
  for (const match of source.matchAll(annotationPattern)) {
    bindings.set(match[1], match[2] as ApiTypeName)
  }

  const assignmentPattern =
    /^\s*([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*(?:#.*)?$/gm
  const assignments = [...source.matchAll(assignmentPattern)]
  for (let pass = 0; pass < 3; pass += 1) {
    for (const match of assignments) {
      const type = resolveExpressionType(match[2].replace(/\s+/g, ''), bindings)
      if (type) bindings.set(match[1], type)
    }
  }
  return bindings
}

function resolveExpressionType(
  expression: string,
  bindings: ReadonlyMap<string, ApiTypeName>,
): ApiTypeName | undefined {
  const parts = expression.split('.')
  let type: ApiTypeName | undefined = bindings.get(parts[0]) ?? CLASS_RECEIVER_TYPES[parts[0]]
  for (const name of parts.slice(1)) {
    if (!type) return undefined
    type = API_MEMBERS[type].find((item) => item.name === name)?.resultType
  }
  return type
}

function memberCompletion(ownerType: ApiTypeName, member: ApiMember): FlowLensPythonCompletion {
  return {
    name: member.name,
    kind: member.kind,
    detail: `${ownerType}.${member.signature}`,
    insertText: member.insertText,
    documentation: member.documentation,
    snippet: member.kind === 'method',
  }
}

function topLevelCompletions(source: string): FlowLensPythonCompletion[] {
  const variables = [...collectBindings(source)].map(([name, type]) => ({
    name,
    kind: 'variable' as const,
    detail: `${name}: ${type}`,
    insertText: name,
    documentation: `FlowLens ${type} API object.`,
    snippet: false,
  }))
  const classes = API_CONSTRUCTORS.map((item) => ({
    name: item.name,
    kind: 'class' as const,
    detail: item.signature,
    insertText: item.name,
    documentation: item.documentation,
    snippet: false,
  }))
  const hooks: FlowLensPythonCompletion[] = [
    {
      name: 'onRequest',
      kind: 'snippet',
      detail: 'def onRequest(context: Context, request: Request) -> Request',
      insertText: 'def onRequest(context, request):\n\t${1:return request}',
      documentation: 'Define the request hook.',
      snippet: true,
    },
    {
      name: 'onResponse',
      kind: 'snippet',
      detail: 'def onResponse(context: Context, response: Response) -> Response',
      insertText: 'def onResponse(context, response):\n\t${1:return response}',
      documentation: 'Define the response hook.',
      snippet: true,
    },
  ]
  return [...variables, ...classes, ...hooks]
}

function completionKind(monaco: MonacoApi, kind: FlowLensPythonCompletion['kind']) {
  switch (kind) {
    case 'method':
      return monaco.languages.CompletionItemKind.Method
    case 'property':
      return monaco.languages.CompletionItemKind.Property
    case 'class':
      return monaco.languages.CompletionItemKind.Class
    case 'snippet':
      return monaco.languages.CompletionItemKind.Snippet
    case 'variable':
      return monaco.languages.CompletionItemKind.Variable
  }
}

function parameterName(value: string) {
  return (
    value
      .trim()
      .replace(/^\*+/, '')
      .match(/^([A-Za-z_]\w*)/)?.[1] ?? ''
  )
}

function findActiveCallOpenParen(source: string) {
  const stack: Array<{ character: string; index: number }> = []
  scanPython(source, (character, index) => {
    if ('([{'.includes(character)) {
      stack.push({ character, index })
      return
    }
    const expected =
      character === ')' ? '(' : character === ']' ? '[' : character === '}' ? '{' : ''
    if (!expected) return
    for (let i = stack.length - 1; i >= 0; i -= 1) {
      if (stack[i].character === expected) {
        stack.splice(i)
        return
      }
    }
  })
  for (let index = stack.length - 1; index >= 0; index -= 1) {
    if (stack[index].character === '(') return stack[index].index
  }
  return -1
}

function countTopLevelCommas(value: string) {
  let depth = 0
  let count = 0
  scanPython(value, (character) => {
    if ('([{'.includes(character)) depth += 1
    else if (')]}'.includes(character)) depth = Math.max(0, depth - 1)
    else if (character === ',' && depth === 0) count += 1
  })
  return count
}

function scanPython(source: string, visit: (character: string, index: number) => void) {
  let quote = ''
  let triple = false
  let escaped = false
  let comment = false
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index]
    if (comment) {
      if (character === '\n') comment = false
      continue
    }
    if (quote) {
      if (escaped) {
        escaped = false
        continue
      }
      if (character === '\\') {
        escaped = true
        continue
      }
      if (triple && source.slice(index, index + 3) === quote.repeat(3)) {
        quote = ''
        triple = false
        index += 2
      } else if (!triple && character === quote) {
        quote = ''
      }
      continue
    }
    if (character === '#') {
      comment = true
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      triple = source.slice(index, index + 3) === character.repeat(3)
      if (triple) index += 2
      continue
    }
    visit(character, index)
  }
}

function isIdentifierCharacter(value: string | undefined) {
  return !!value && /[A-Za-z0-9_]/.test(value)
}

function clampOffset(source: string, offset: number) {
  return Math.max(0, Math.min(Number.isFinite(offset) ? Math.trunc(offset) : 0, source.length))
}
