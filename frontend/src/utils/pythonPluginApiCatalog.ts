// User-facing SDK API v1. Keep aligned with the Python runtime and technical guides.

export type ApiTypeName =
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

export type ApiMemberKind = 'method' | 'property'

export interface ApiMember {
  name: string
  kind: ApiMemberKind
  signature: string
  insertText: string
  documentation: string
  parameters?: readonly string[]
  resultType?: ApiTypeName
  writable?: boolean
  returnType?: string
}

export interface ApiConstructor {
  name: string
  signature: string
  documentation: string
  parameters: readonly string[]
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
    writable,
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
    returnType,
  }
}

export const API_MEMBERS: Record<ApiTypeName, readonly ApiMember[]> = {
  Context: [
    property('id', 'str', 'Execution ID.'),
    property('timestamp', 'int', 'Send start time in Unix microseconds.'),
    property('original_url', 'str', 'Original URL before the hook chain.'),
    property('original_method', 'str', 'Original method before the hook chain.'),
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
    method('debug', ['message: Any'], 'None', 'debug(${1:message})', 'Log a debug message.'),
    method('info', ['message: Any'], 'None', 'info(${1:message})', 'Log an informational message.'),
    method('warning', ['message: Any'], 'None', 'warning(${1:message})', 'Log a warning message.'),
    method('error', ['message: Any'], 'None', 'error(${1:message})', 'Log an error message.'),
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
    property('headers', 'Headers', 'Mutable ordered request headers.', 'Headers', true),
    property('body', 'Body', 'Request body; direct replacement is supported.', 'Body', true),
  ],
  RequestSnapshot: [
    property('method', 'str', 'Read-only request method associated with the response.'),
    property('url', 'str', 'Read-only request URL associated with the response.'),
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
    property('content_type', 'str | None', 'Content-Type of the associated request.'),
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
    property('code', 'int', 'Mutable response status code.', undefined, true),
    property('protocol', 'str', 'Read-only protocol actually used by the upstream response.'),
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
    property('headers', 'Headers', 'Mutable ordered response headers.', 'Headers', true),
    property('trailers', 'Headers', 'Mutable ordered response trailers.', 'Headers', true),
    property('body', 'Body', 'Response body; unavailable for SSE responses.', 'Body', true),
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
    method('remove', ['name: str'], 'None', "remove('${1:name}')", 'Remove all matching headers.'),
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
    method('to_string', [], 'str', 'to_string()', 'Encode as a URL query string.'),
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
    method('to_string', [], 'str', 'to_string()', 'Return the original URL query string.'),
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
    method('items', [], 'ItemsView[str, Any]', 'items()', 'Return all key-value pairs.'),
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
    method('items', [], 'dict_items[str, Any]', 'items()', 'Return all shared key-value pairs.'),
    method('values', [], 'dict_values[Any]', 'values()', 'Return all shared values.'),
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
      ['key: str', 'default: Any = ...'],
      'Any',
      "pop('${1:key}')",
      'Remove and return a shared value; without a default, a missing key raises KeyError.',
    ),
    method('clear', [], 'None', 'clear()', 'Remove all shared values.'),
  ],
}

export const API_CONSTRUCTORS: readonly ApiConstructor[] = [
  {
    name: 'Body',
    signature: 'Body(kind: str = "none", value: Any = None)',
    documentation: 'Create a body with an explicit semantic kind.',
    parameters: ['kind: str = "none"', 'value: Any = None'],
  },
  {
    name: 'FileDescriptor',
    signature: 'FileDescriptor(path: str, name: str = "", size: int = -1, read_only: bool = True)',
    documentation: 'Describe a FlowLens-managed file; prefer from_file() for user files.',
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
    signature:
      'Queries(fields: Iterable[QueryField | tuple[str, str] | dict[str, str]] | None = None)',
    documentation: 'Create query parameters that preserve order and duplicates.',
    parameters: ['fields: Iterable[QueryField | tuple[str, str] | dict[str, str]] | None = None'],
  },
  {
    name: 'QueryField',
    signature: 'QueryField(name: str, value: str)',
    documentation: 'Create a query field.',
    parameters: ['name: str', 'value: str'],
  },
  {
    name: 'Headers',
    signature: 'Headers(fields: Iterable[HeaderField | dict[str, str]] | None = None)',
    documentation: 'Create headers that preserve order and duplicates.',
    parameters: ['fields: Iterable[HeaderField | dict[str, str]] | None = None'],
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
    documentation: 'Create a request object. Hooks normally modify the provided request.',
    parameters: ['method: str', 'url: str', 'headers: Headers | None = None', 'body: Any = None'],
  },
  {
    name: 'Response',
    signature:
      'Response(code: int, headers: Headers | None = None, trailers: Headers | None = None, body: Any = None, request: RequestSnapshot | None = None, protocol: str = "", status_text: str = "")',
    documentation: 'Create a response object. Hooks normally modify the provided response.',
    parameters: [
      'code: int',
      'headers: Headers | None = None',
      'trailers: Headers | None = None',
      'body: Any = None',
      'request: RequestSnapshot | None = None',
      'protocol: str = ""',
      'status_text: str = ""',
    ],
  },
  {
    name: 'URLEncodedField',
    signature: 'URLEncodedField(name: str, value: str, enabled: bool = True)',
    documentation: 'Create a URL-encoded body field.',
    parameters: ['name: str', 'value: str', 'enabled: bool = True'],
  },
]

export const PYTHON_PLUGIN_HOOKS = [
  {
    name: 'onRequest',
    signature: 'onRequest(context: Context, request: Request) -> Request | None',
    parameters: ['context: Context', 'request: Request'],
    returnType: 'Request | None',
    insertText: 'def onRequest(context, request):\n\t${1:return request}',
    documentation:
      'Modify the provided request and return it, or return None to block before transport.',
  },
  {
    name: 'onResponse',
    signature: 'onResponse(context: Context, response: Response) -> Response | None',
    parameters: ['context: Context', 'response: Response'],
    returnType: 'Response | None',
    insertText: 'def onResponse(context, response):\n\t${1:return response}',
    documentation:
      'Modify the provided response and return it, or return None to block its presentation.',
  },
] as const

export type PythonHelpScope = 'global' | 'inline'

export const PYTHON_PLUGIN_MINIMAL_SCRIPT = `from flowlens import *


def onRequest(context, request):
    return request


def onResponse(context, response):
    return response
`
