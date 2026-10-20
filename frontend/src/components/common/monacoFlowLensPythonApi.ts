import type * as Monaco from 'monaco-editor'
import {
  API_MEMBERS,
  API_CONSTRUCTORS,
  PYTHON_PLUGIN_HOOKS,
  type ApiTypeName,
  type ApiMemberKind,
  type ApiMember,
} from '../../utils/pythonPluginApiCatalog.js'

type MonacoApi = typeof Monaco

export const FLOWLENS_PYTHON_API_LANGUAGE_ID = 'flowlens-python-api'

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
  const hooks: FlowLensPythonCompletion[] = PYTHON_PLUGIN_HOOKS.map((hook) => ({
    name: hook.name,
    kind: 'snippet',
    detail: `def ${hook.signature}`,
    insertText: hook.insertText,
    documentation: hook.documentation,
    snippet: true,
  }))
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
