import type * as Monaco from 'monaco-editor'
import {
  FLOWLENS_PYTHON_API_LANGUAGE_ID,
  registerFlowLensPythonApi,
} from './monacoFlowLensPythonApi.js'

type MonacoApi = typeof Monaco
type MonacoLanguage = Monaco.languages.IMonarchLanguage
type MonacoLanguageConfiguration = Monaco.languages.LanguageConfiguration

const LANGUAGE_MAP: Record<string, string> = {
  css: 'flowlens-css',
  html: 'flowlens-html',
  http: 'flowlens-http',
  javascript: 'flowlens-javascript',
  json: 'flowlens-json',
  python: 'flowlens-python',
  xml: 'flowlens-xml',
}

const BRACKET_CONFIGURATION: MonacoLanguageConfiguration = {
  brackets: [
    ['{', '}'],
    ['[', ']'],
    ['(', ')'],
  ],
  autoClosingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
  surroundingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
}

const JSON_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  tokenPostfix: '.json',
  tokenizer: {
    root: [
      [/[{}[\]]/, '@brackets'],
      [/:|,/, 'delimiter'],
      [/"([^"\\]|\\.)*"(?=\s*:)/, 'type.identifier'],
      [/"([^"\\]|\\.)*"/, 'string'],
      [/-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/, 'number'],
      [/\b(?:true|false|null)\b/, 'keyword'],
      [/\s+/, ''],
    ],
  },
}

const MARKUP_CONFIGURATION: MonacoLanguageConfiguration = {
  comments: {
    blockComment: ['<!--', '-->'],
  },
  brackets: [
    ['<', '>'],
    ['{', '}'],
    ['[', ']'],
    ['(', ')'],
  ],
  autoClosingPairs: [
    { open: '<', close: '>' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
  surroundingPairs: [
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
}

const MARKUP_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  ignoreCase: true,
  tokenPostfix: '.xml',
  tokenizer: {
    root: [
      [/<!\[CDATA\[/, 'string', '@cdata'],
      [/<!--/, 'comment', '@comment'],
      [/<!DOCTYPE/, 'metatag', '@doctype'],
      [/<\?/, 'metatag', '@processing'],
      [/<\/?/, 'delimiter', '@tagName'],
      [/[^<]+/, ''],
    ],
    cdata: [
      [/[^\]]+/, 'string'],
      [/\]\]>/, 'string', '@pop'],
      [/./, 'string'],
    ],
    comment: [
      [/[^-]+/, 'comment'],
      [/-->/, 'comment', '@pop'],
      [/-/, 'comment'],
    ],
    doctype: [
      [/[^>]+/, 'metatag'],
      [/>/, 'metatag', '@pop'],
    ],
    processing: [
      [/[^?]+/, 'metatag'],
      [/\?>/, 'metatag', '@pop'],
      [/\?/, 'metatag'],
    ],
    tagName: [
      [/[a-zA-Z_][\w:.-]*/, 'tag', '@tag'],
      [/>/, 'delimiter', '@pop'],
    ],
    tag: [
      [/[a-zA-Z_][\w:.-]*(?=\s*=)/, 'attribute.name'],
      [/=/, 'delimiter'],
      [/"[^"]*"/, 'attribute.value'],
      [/'[^']*'/, 'attribute.value'],
      [/\/?>/, 'delimiter', '@pop'],
      [/\s+/, ''],
      [/[^=\s/>]+/, 'attribute.name'],
    ],
  },
}

const CSS_CONFIGURATION: MonacoLanguageConfiguration = {
  comments: {
    blockComment: ['/*', '*/'],
  },
  brackets: [
    ['{', '}'],
    ['[', ']'],
    ['(', ')'],
  ],
  autoClosingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
  surroundingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
}

const CSS_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  tokenPostfix: '.css',
  tokenizer: {
    root: [
      [/\/\*/, 'comment', '@comment'],
      [/[{}()[\]:;,]/, 'delimiter'],
      [/@-?[\w-]+/, 'keyword'],
      [/#[\da-fA-F]{3,8}\b/, 'number.hex'],
      [/-?\d*\.?\d+(?:%|[a-zA-Z]+)?/, 'number'],
      [/"([^"\\]|\\.)*"/, 'string'],
      [/'([^'\\]|\\.)*'/, 'string'],
      [/--[\w-]+(?=\s*:)/, 'variable'],
      [/[a-zA-Z-]+(?=\s*:)/, 'attribute.name'],
      [/[.#]?-?[_a-zA-Z][\w-]*/, 'identifier'],
      [/[>+~*=^$|!]+/, 'operator'],
      [/\s+/, ''],
    ],
    comment: [
      [/[^*]+/, 'comment'],
      [/\*\//, 'comment', '@pop'],
      [/\*/, 'comment'],
    ],
  },
}

const HTTP_CONFIGURATION: MonacoLanguageConfiguration = {}

export const HTTP_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  tokenPostfix: '.http',
  tokenizer: {
    root: [
      [
        /^(\S+)(\s+)(\S+)(\s+)(HTTP\/\d(?:\.\d+)?)\s*$/,
        [
          'keyword',
          '',
          'string',
          '',
          { token: 'type.identifier', next: '@headers' },
        ],
      ],
      [
        /^(HTTP\/\d(?:\.\d+)?)(\s+)(\d{3})(.*)$/,
        [
          'type.identifier',
          '',
          { token: 'number', next: '@headers' },
          'string',
        ],
      ],
      [/.*$/, { token: '', next: '@headers' }],
    ],
    headers: [
      [/^$/, { token: '', next: '@body' }],
      [/^([^:\s][^:]*)(:)([ \t]*)(.*)$/, ['attribute.name', 'delimiter', '', '']],
      [/.*$/, ''],
    ],
    body: [[/.+/, '']],
  },
}

const JAVASCRIPT_CONFIGURATION: MonacoLanguageConfiguration = {
  comments: {
    lineComment: '//',
    blockComment: ['/*', '*/'],
  },
  ...BRACKET_CONFIGURATION,
  autoClosingPairs: [...(BRACKET_CONFIGURATION.autoClosingPairs ?? []), { open: '`', close: '`' }],
  surroundingPairs: [...(BRACKET_CONFIGURATION.surroundingPairs ?? []), { open: '`', close: '`' }],
}

const JAVASCRIPT_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  tokenPostfix: '.js',
  keywords: [
    'async',
    'await',
    'break',
    'case',
    'catch',
    'class',
    'const',
    'constructor',
    'continue',
    'debugger',
    'default',
    'delete',
    'do',
    'else',
    'export',
    'extends',
    'false',
    'finally',
    'for',
    'from',
    'function',
    'get',
    'if',
    'import',
    'in',
    'instanceof',
    'let',
    'new',
    'null',
    'of',
    'return',
    'set',
    'static',
    'super',
    'switch',
    'symbol',
    'this',
    'throw',
    'true',
    'try',
    'typeof',
    'undefined',
    'var',
    'void',
    'while',
    'with',
    'yield',
  ],
  operators: [
    '<=',
    '>=',
    '==',
    '!=',
    '===',
    '!==',
    '=>',
    '+',
    '-',
    '**',
    '*',
    '/',
    '%',
    '++',
    '--',
    '<<',
    '</',
    '>>',
    '>>>',
    '&',
    '|',
    '^',
    '!',
    '~',
    '&&',
    '||',
    '??',
    '?',
    ':',
    '=',
    '+=',
    '-=',
    '*=',
    '**=',
    '/=',
    '%=',
    '<<=',
    '>>=',
    '>>>=',
    '&=',
    '|=',
    '^=',
    '@',
  ],
  symbols: /[=><!~?:&|+\-*/^%@]+/,
  escapes: /\\(?:[abfnrtv\\"'`]|x[\da-fA-F]{2}|u[\da-fA-F]{4}|u\{[\da-fA-F]+\})/,
  tokenizer: {
    root: [
      [/[{}()[\]]/, '@brackets'],
      [/[a-zA-Z_$][\w$]*/, { cases: { '@keywords': 'keyword', '@default': 'identifier' } }],
      { include: '@whitespace' },
      [/\d*\.\d+(?:[eE][+-]?\d+)?/, 'number.float'],
      [/0[xX][\da-fA-F]+/, 'number.hex'],
      [/0[bB][01]+/, 'number.binary'],
      [/0[oO][0-7]+/, 'number.octal'],
      [/\d+/, 'number'],
      [/[;,.]/, 'delimiter'],
      [/@symbols/, { cases: { '@operators': 'operator', '@default': '' } }],
      [/"([^"\\]|\\.)*$/, 'string.invalid'],
      [/'([^'\\]|\\.)*$/, 'string.invalid'],
      [/"/, 'string', '@string_double'],
      [/'/, 'string', '@string_single'],
      [/`/, 'string', '@string_backtick'],
    ],
    whitespace: [
      [/[ \t\r\n]+/, ''],
      [/\/\*/, 'comment', '@comment'],
      [/\/\/.*$/, 'comment'],
    ],
    comment: [
      [/[^/*]+/, 'comment'],
      [/\*\//, 'comment', '@pop'],
      [/[/*]/, 'comment'],
    ],
    string_double: [
      [/[^\\"]+/, 'string'],
      [/@escapes/, 'string.escape'],
      [/\\./, 'string.escape.invalid'],
      [/"/, 'string', '@pop'],
    ],
    string_single: [
      [/[^\\']+/, 'string'],
      [/@escapes/, 'string.escape'],
      [/\\./, 'string.escape.invalid'],
      [/'/, 'string', '@pop'],
    ],
    string_backtick: [
      [/[^\\`$]+/, 'string'],
      [/@escapes/, 'string.escape'],
      [/\\./, 'string.escape.invalid'],
      [/\$\{/, { token: 'delimiter.bracket', next: '@bracketCounting' }],
      [/`/, 'string', '@pop'],
    ],
    bracketCounting: [
      [/\{/, 'delimiter.bracket', '@bracketCounting'],
      [/\}/, 'delimiter.bracket', '@pop'],
      { include: 'root' },
    ],
  },
}

const PYTHON_CONFIGURATION: MonacoLanguageConfiguration = {
  comments: {
    lineComment: '#',
  },
  ...BRACKET_CONFIGURATION,
  indentationRules: {
    increaseIndentPattern: /^\s*(?:async\s+)?(?:def|class|if|elif|else|for|while|try|except|finally|with|match|case)\b.*:\s*(?:#.*)?$/,
    decreaseIndentPattern: /^\s*(?:elif|else|except|finally|case)\b/,
  },
}

const PYTHON_LANGUAGE: MonacoLanguage = {
  defaultToken: '',
  tokenPostfix: '.py',
  keywords: [
    'False',
    'None',
    'True',
    'and',
    'as',
    'assert',
    'async',
    'await',
    'break',
    'case',
    'class',
    'continue',
    'def',
    'del',
    'elif',
    'else',
    'except',
    'finally',
    'for',
    'from',
    'global',
    'if',
    'import',
    'in',
    'is',
    'lambda',
    'match',
    'nonlocal',
    'not',
    'or',
    'pass',
    'raise',
    'return',
    'try',
    'while',
    'with',
    'yield',
  ],
  builtins: [
    'bool',
    'bytes',
    'dict',
    'enumerate',
    'float',
    'int',
    'len',
    'list',
    'object',
    'print',
    'range',
    'set',
    'str',
    'super',
    'tuple',
    'type',
    'zip',
  ],
  tokenizer: {
    root: [
      [/\b[_a-zA-Z]\w*\b/, { cases: { '@keywords': 'keyword', '@builtins': 'type.identifier', '@default': 'identifier' } }],
      [/@[_a-zA-Z]\w*/, 'annotation'],
      [/0[xX][\da-fA-F](?:_?[\da-fA-F])*/, 'number.hex'],
      [/0[bB][01](?:_?[01])*/, 'number.binary'],
      [/0[oO][0-7](?:_?[0-7])*/, 'number.octal'],
      [/(?:\d(?:_?\d)*)?\.\d(?:_?\d)*(?:[eE][+-]?\d(?:_?\d)*)?[jJ]?/, 'number.float'],
      [/\d(?:_?\d)*(?:[eE][+-]?\d(?:_?\d)*)?[jJ]?/, 'number'],
      [/"""/, 'string', '@tripleDoubleString'],
      [/'''/, 'string', '@tripleSingleString'],
      [/"/, 'string', '@doubleString'],
      [/'/, 'string', '@singleString'],
      [/#.*$/, 'comment'],
      [/[{}()[\]]/, '@brackets'],
      [/[+\-*/%&|^~<>!=:@]+/, 'operator'],
      [/[;,.]/, 'delimiter'],
      [/\s+/, ''],
    ],
    doubleString: [
      [/[^\\"]+/, 'string'],
      [/\\./, 'string.escape'],
      [/"/, 'string', '@pop'],
    ],
    singleString: [
      [/[^\\']+/, 'string'],
      [/\\./, 'string.escape'],
      [/'/, 'string', '@pop'],
    ],
    tripleDoubleString: [
      [/[^\\"]+/, 'string'],
      [/\\./, 'string.escape'],
      [/"""/, 'string', '@pop'],
      [/"/, 'string'],
    ],
    tripleSingleString: [
      [/[^\\']+/, 'string'],
      [/\\./, 'string.escape'],
      [/'''/, 'string', '@pop'],
      [/'/, 'string'],
    ],
  },
}

export function resolveMonacoLightLanguage(
  language: string | undefined,
  flowLensPythonApi = false,
) {
  if (language === 'python' && flowLensPythonApi) {
    return FLOWLENS_PYTHON_API_LANGUAGE_ID
  }
  return LANGUAGE_MAP[language ?? ''] ?? language ?? 'plaintext'
}

export function registerMonacoLightLanguages(monaco: MonacoApi) {
  registerLanguage(monaco, LANGUAGE_MAP.json, JSON_LANGUAGE, BRACKET_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.xml, MARKUP_LANGUAGE, MARKUP_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.html, MARKUP_LANGUAGE, MARKUP_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.css, CSS_LANGUAGE, CSS_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.http, HTTP_LANGUAGE, HTTP_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.javascript, JAVASCRIPT_LANGUAGE, JAVASCRIPT_CONFIGURATION)
  registerLanguage(monaco, LANGUAGE_MAP.python, PYTHON_LANGUAGE, PYTHON_CONFIGURATION)
  registerLanguage(
    monaco,
    FLOWLENS_PYTHON_API_LANGUAGE_ID,
    PYTHON_LANGUAGE,
    PYTHON_CONFIGURATION,
  )
  registerFlowLensPythonApi(monaco)
}

function registerLanguage(
  monaco: MonacoApi,
  id: string,
  language: MonacoLanguage,
  configuration: MonacoLanguageConfiguration,
) {
  if (monaco.languages.getLanguages().some((item) => item.id === id)) {
    return
  }

  monaco.languages.register({ id })
  monaco.languages.setLanguageConfiguration(id, configuration)
  monaco.languages.setMonarchTokensProvider(id, language)
}
