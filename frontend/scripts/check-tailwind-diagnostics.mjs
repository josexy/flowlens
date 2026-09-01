import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { readdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(scriptDir, '..')
const rootUri = pathToFileURL(`${projectRoot}${path.sep}`).href
const serverBin = path.join(
  projectRoot,
  'node_modules',
  '@tailwindcss',
  'language-server',
  'bin',
  'tailwindcss-language-server',
)
const cssEntry = path.join(projectRoot, 'src', 'style.css')

const args = parseArgs(process.argv.slice(2))
const scanRoots = args.paths.length > 0 ? args.paths : ['src']
const timeoutMs = args.timeoutMs
const idleMs = args.idleMs

const supportedExtensions = new Map([
  ['.css', 'css'],
  ['.html', 'html'],
  ['.js', 'javascript'],
  ['.jsx', 'javascriptreact'],
  ['.ts', 'typescript'],
  ['.tsx', 'typescriptreact'],
  ['.vue', 'vue'],
])

const tailwindSettings = {
  includeLanguages: {
    vue: 'html',
    typescript: 'javascript',
    typescriptreact: 'javascript',
  },
  classAttributes: ['class', 'className', 'ngClass', 'class:list'],
  classFunctions: [],
  validate: true,
  lint: {
    cssConflict: 'warning',
    deprecatedAtRule: 'warning',
    invalidApply: 'error',
    invalidConfigPath: 'error',
    invalidScreen: 'error',
    invalidTailwindDirective: 'error',
    invalidVariant: 'error',
    recommendedVariantOrder: 'warning',
    suggestCanonicalClasses: 'warning',
    usedBlocklistedClass: 'warning',
  },
  experimental: {
    configFile: 'src/style.css',
  },
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error))
  process.exitCode = 1
})

async function main() {
  if (!existsSync(serverBin)) {
    throw new Error('Missing @tailwindcss/language-server. Run `npm install` in frontend first.')
  }

  const files = await collectFiles(scanRoots)
  if (files.length === 0) {
    console.log('No supported files found.')
    return
  }

  const client = new LspClient()
  await client.start()

  try {
    await client.request('initialize', {
      processId: null,
      clientInfo: {
        name: 'flowlens-tailwind-check',
      },
      rootPath: projectRoot,
      rootUri,
      workspaceFolders: [
        {
          uri: rootUri,
          name: path.basename(projectRoot),
        },
      ],
      capabilities: {
        general: {
          positionEncodings: ['utf-16'],
        },
        textDocument: {
          synchronization: {
            dynamicRegistration: false,
            didSave: true,
          },
          publishDiagnostics: {
            relatedInformation: true,
            codeDescriptionSupport: true,
            dataSupport: true,
          },
        },
        window: {
          workDoneProgress: true,
        },
        workspace: {
          configuration: true,
          workspaceFolders: true,
          didChangeConfiguration: {
            dynamicRegistration: true,
          },
        },
      },
      initializationOptions: {
        userLanguages: {
          vue: 'html',
        },
      },
    })

    client.notify('initialized', {})
    client.notify('workspace/didChangeConfiguration', {
      settings: {
        tailwindCSS: tailwindSettings,
      },
    })

    await openFileIfPresent(client, cssEntry)
    console.error(`Scanning ${files.length} files with @tailwindcss/language-server...`)

    for (const filePath of files) {
      await openFileIfPresent(client, filePath)
    }

    await client.waitForDiagnosticsIdle({ idleMs, timeoutMs })

    const diagnostics = collectDiagnostics(client.diagnostics, {
      includeAllRules: args.all,
    })

    if (args.fix) {
      const fixResult = await applyFixes(diagnostics)
      console.log(
        `Applied ${fixResult.appliedCount} Tailwind canonical class fix${
          fixResult.appliedCount === 1 ? '' : 'es'
        } across ${fixResult.fileCount} file${fixResult.fileCount === 1 ? '' : 's'}.`,
      )

      if (fixResult.skippedCount > 0) {
        console.log(`Skipped ${fixResult.skippedCount} diagnostic${fixResult.skippedCount === 1 ? '' : 's'} without a canonical replacement.`)
      }

      if (fixResult.skippedCount > 0 && !args.noFail) {
        process.exitCode = 1
      }
      return
    }

    if (args.json) {
      console.log(JSON.stringify(diagnostics, null, 2))
    } else if (diagnostics.length === 0) {
      console.log(
        args.all
          ? 'No Tailwind diagnostics found.'
          : 'No Tailwind suggestCanonicalClasses diagnostics found.',
      )
    } else {
      for (const diagnostic of diagnostics) {
        const rule = diagnostic.code ? ` (${diagnostic.code})` : ''
        console.log(`${diagnostic.file}:${diagnostic.line}:${diagnostic.column} - ${diagnostic.message}${rule}`)
      }

      console.log(
        `\nFound ${diagnostics.length} Tailwind ${
          args.all ? 'diagnostic' : 'suggestCanonicalClasses diagnostic'
        }${diagnostics.length === 1 ? '' : 's'}.`,
      )
    }

    if (diagnostics.length > 0 && !args.noFail) {
      process.exitCode = 1
    }
  } finally {
    await client.stop()
  }
}

function parseArgs(argv) {
  const parsed = {
    all: false,
    fix: false,
    idleMs: 3000,
    json: false,
    noFail: false,
    paths: [],
    timeoutMs: 45000,
  }

  for (const arg of argv) {
    if (arg === '--all') {
      parsed.all = true
      continue
    }

    if (arg === '--fix') {
      parsed.fix = true
      continue
    }

    if (arg === '--json') {
      parsed.json = true
      continue
    }

    if (arg === '--no-fail') {
      parsed.noFail = true
      continue
    }

    if (arg.startsWith('--timeout=')) {
      parsed.timeoutMs = parseDuration(arg.slice('--timeout='.length), parsed.timeoutMs)
      continue
    }

    if (arg.startsWith('--idle=')) {
      parsed.idleMs = parseDuration(arg.slice('--idle='.length), parsed.idleMs)
      continue
    }

    parsed.paths.push(arg)
  }

  return parsed
}

function parseDuration(value, fallback) {
  const numericValue = Number(value)
  if (!Number.isFinite(numericValue) || numericValue <= 0) {
    return fallback
  }
  return numericValue
}

async function collectFiles(roots) {
  const files = new Set()

  for (const root of roots) {
    const absoluteRoot = path.resolve(projectRoot, root)
    const relativeRoot = path.relative(projectRoot, absoluteRoot)
    if (relativeRoot.startsWith('..') || path.isAbsolute(relativeRoot)) {
      throw new Error(`Refusing to scan outside frontend: ${root}`)
    }

    await collectFilesFromPath(absoluteRoot, files)
  }

  return [...files].sort((a, b) => a.localeCompare(b))
}

async function collectFilesFromPath(targetPath, files) {
  const fileExtension = path.extname(targetPath)
  if (supportedExtensions.has(fileExtension)) {
    files.add(targetPath)
    return
  }

  const entries = await readdir(targetPath, { withFileTypes: true })
  for (const entry of entries) {
    if (entry.name.startsWith('.')) {
      continue
    }

    if (['bindings', 'coverage', 'dist', 'dist-ssr', 'node_modules'].includes(entry.name)) {
      continue
    }

    const childPath = path.join(targetPath, entry.name)
    if (entry.isDirectory()) {
      await collectFilesFromPath(childPath, files)
      continue
    }

    if (supportedExtensions.has(path.extname(entry.name))) {
      files.add(childPath)
    }
  }
}

async function openFileIfPresent(client, filePath) {
  if (!existsSync(filePath)) {
    return
  }

  const fileExtension = path.extname(filePath)
  const languageId = supportedExtensions.get(fileExtension)
  if (!languageId) {
    return
  }

  const text = await readFile(filePath, 'utf8')
  client.notify('textDocument/didOpen', {
    textDocument: {
      uri: pathToFileURL(filePath).href,
      languageId,
      version: 1,
      text,
    },
  })
}

function collectDiagnostics(diagnosticsByUri, { includeAllRules }) {
  const diagnostics = []

  for (const [uri, fileDiagnostics] of diagnosticsByUri.entries()) {
    const filePath = fileURLToPath(uri)
    const relativeFile = path.relative(projectRoot, filePath).replaceAll(path.sep, '/')

    for (const diagnostic of fileDiagnostics) {
      const code = diagnostic.code == null ? '' : String(diagnostic.code)
      if (!includeAllRules && code !== 'suggestCanonicalClasses') {
        continue
      }

      diagnostics.push({
        file: relativeFile,
        filePath,
        line: diagnostic.range.start.line + 1,
        column: diagnostic.range.start.character + 1,
        range: diagnostic.range,
        replacement: extractCanonicalReplacement(diagnostic.message),
        code,
        severity: diagnostic.severity,
        message: diagnostic.message,
      })
    }
  }

  diagnostics.sort(
    (a, b) =>
      a.file.localeCompare(b.file) ||
      a.line - b.line ||
      a.column - b.column ||
      a.message.localeCompare(b.message),
  )

  return diagnostics
}

async function applyFixes(diagnostics) {
  const fixesByFile = new Map()
  let skippedCount = 0

  for (const diagnostic of diagnostics) {
    if (diagnostic.code !== 'suggestCanonicalClasses' || !diagnostic.replacement) {
      skippedCount += 1
      continue
    }

    const fixes = fixesByFile.get(diagnostic.filePath) ?? []
    fixes.push({
      range: diagnostic.range,
      replacement: diagnostic.replacement,
    })
    fixesByFile.set(diagnostic.filePath, fixes)
  }

  let appliedCount = 0

  for (const [filePath, fixes] of fixesByFile.entries()) {
    const text = await readFile(filePath, 'utf8')
    const lineOffsets = getLineOffsets(text)
    const sortedFixes = fixes
      .map((fix) => ({
        start: offsetAt(lineOffsets, text.length, fix.range.start),
        end: offsetAt(lineOffsets, text.length, fix.range.end),
        replacement: fix.replacement,
      }))
      .sort((a, b) => b.start - a.start || b.end - a.end)

    let nextText = text
    let lastStart = Number.POSITIVE_INFINITY
    for (const fix of sortedFixes) {
      if (fix.end > lastStart) {
        skippedCount += 1
        continue
      }

      nextText = `${nextText.slice(0, fix.start)}${fix.replacement}${nextText.slice(fix.end)}`
      lastStart = fix.start
      appliedCount += 1
    }

    if (nextText !== text) {
      await writeFile(filePath, nextText)
    }
  }

  return {
    appliedCount,
    fileCount: fixesByFile.size,
    skippedCount,
  }
}

function extractCanonicalReplacement(message) {
  const match = /can be written as `([^`]+)`/.exec(message)
  return match?.[1] ?? ''
}

function getLineOffsets(text) {
  const lineOffsets = [0]

  for (let index = 0; index < text.length; index += 1) {
    if (text.charCodeAt(index) === 10) {
      lineOffsets.push(index + 1)
    }
  }

  return lineOffsets
}

function offsetAt(lineOffsets, textLength, position) {
  if (position.line >= lineOffsets.length) {
    return textLength
  }

  if (position.line < 0) {
    return 0
  }

  const lineStart = lineOffsets[position.line]
  const nextLineStart =
    position.line + 1 < lineOffsets.length ? lineOffsets[position.line + 1] : textLength
  return Math.max(Math.min(lineStart + position.character, nextLineStart), lineStart)
}

class LspClient {
  constructor() {
    this.nextId = 1
    this.pending = new Map()
    this.diagnostics = new Map()
    this.buffer = Buffer.alloc(0)
    this.lastDiagnosticAt = Date.now()
  }

  async start() {
    this.server = spawn(process.execPath, [serverBin, '--stdio'], {
      cwd: projectRoot,
      stdio: ['pipe', 'pipe', 'pipe'],
    })

    this.server.stdout.on('data', (chunk) => {
      this.buffer = Buffer.concat([this.buffer, chunk])
      this.readMessages()
    })

    this.server.stderr.on('data', (chunk) => {
      const text = chunk.toString('utf8').trim()
      if (text) {
        console.error(text)
      }
    })

    this.server.on('exit', (code, signal) => {
      const error = new Error(`Tailwind language server exited (${signal ?? code ?? 'unknown'}).`)
      for (const { reject } of this.pending.values()) {
        reject(error)
      }
      this.pending.clear()
    })
  }

  request(method, params) {
    const id = this.nextId++
    const message = { jsonrpc: '2.0', id, method, params }

    const promise = new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
    })

    this.send(message)
    return promise
  }

  notify(method, params) {
    this.send({ jsonrpc: '2.0', method, params })
  }

  send(message) {
    const payload = JSON.stringify(message)
    const header = `Content-Length: ${Buffer.byteLength(payload, 'utf8')}\r\n\r\n`
    this.server.stdin.write(header + payload)
  }

  readMessages() {
    while (true) {
      const headerEnd = this.buffer.indexOf('\r\n\r\n')
      if (headerEnd === -1) {
        return
      }

      const header = this.buffer.slice(0, headerEnd).toString('ascii')
      const contentLengthMatch = /Content-Length: (\d+)/i.exec(header)
      if (!contentLengthMatch) {
        throw new Error(`Invalid LSP header: ${header}`)
      }

      const contentLength = Number(contentLengthMatch[1])
      const messageStart = headerEnd + 4
      const messageEnd = messageStart + contentLength
      if (this.buffer.length < messageEnd) {
        return
      }

      const rawMessage = this.buffer.slice(messageStart, messageEnd).toString('utf8')
      this.buffer = this.buffer.slice(messageEnd)
      this.handleMessage(JSON.parse(rawMessage))
    }
  }

  handleMessage(message) {
    if (message.id != null && (Object.hasOwn(message, 'result') || Object.hasOwn(message, 'error'))) {
      const pendingRequest = this.pending.get(message.id)
      if (!pendingRequest) {
        return
      }

      this.pending.delete(message.id)
      if (message.error) {
        pendingRequest.reject(new Error(message.error.message ?? JSON.stringify(message.error)))
        return
      }

      pendingRequest.resolve(message.result)
      return
    }

    if (message.method === 'textDocument/publishDiagnostics') {
      this.diagnostics.set(message.params.uri, message.params.diagnostics ?? [])
      this.lastDiagnosticAt = Date.now()
      return
    }

    if (message.id != null && message.method) {
      this.respondToServerRequest(message)
    }
  }

  respondToServerRequest(message) {
    if (message.method === 'workspace/configuration') {
      this.send({
        jsonrpc: '2.0',
        id: message.id,
        result: message.params.items.map((item) => resolveConfigurationSection(item.section)),
      })
      return
    }

    if (message.method === 'workspace/workspaceFolders') {
      this.send({
        jsonrpc: '2.0',
        id: message.id,
        result: [
          {
            uri: rootUri,
            name: path.basename(projectRoot),
          },
        ],
      })
      return
    }

    if (
      message.method === 'client/registerCapability' ||
      message.method === 'window/workDoneProgress/create'
    ) {
      this.send({ jsonrpc: '2.0', id: message.id, result: null })
      return
    }

    this.send({ jsonrpc: '2.0', id: message.id, result: null })
  }

  async waitForDiagnosticsIdle({ idleMs, timeoutMs }) {
    const start = Date.now()

    while (Date.now() - start < timeoutMs) {
      if (Date.now() - this.lastDiagnosticAt >= idleMs) {
        return
      }

      await delay(100)
    }

    throw new Error(`Timed out after ${timeoutMs}ms while waiting for Tailwind diagnostics.`)
  }

  async stop() {
    if (!this.server || this.server.exitCode != null) {
      return
    }

    try {
      await this.request('shutdown', null)
    } catch {
      // Ignore shutdown errors; the process is about to be terminated.
    }

    this.notify('exit', undefined)

    await Promise.race([
      new Promise((resolve) => this.server.once('exit', resolve)),
      delay(1000).then(() => {
        this.server.kill()
      }),
    ])
  }
}

function resolveConfigurationSection(section) {
  if (!section || section === 'tailwindCSS') {
    return tailwindSettings
  }

  if (!section.startsWith('tailwindCSS.')) {
    return null
  }

  return section
    .slice('tailwindCSS.'.length)
    .split('.')
    .reduce((value, key) => (value == null ? undefined : value[key]), tailwindSettings)
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
