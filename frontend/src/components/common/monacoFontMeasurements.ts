export interface MonacoFontRemeasureApi {
  editor: {
    remeasureFonts(): void
  }
}

export interface MonacoFontFaceSet {
  load(font: string, text?: string): PromiseLike<unknown>
}

export interface MonacoFontMeasurementRequest {
  fontFamily: string
  fontSize: number
  fontWeight?: string
}

const MONACO_FONT_LOAD_SAMPLE = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'

interface PendingFontLoad {
  monacoApis: Set<MonacoFontRemeasureApi>
  promise: Promise<void>
}

const pendingFontLoads = new WeakMap<
  MonacoFontFaceSet,
  Map<string, PendingFontLoad>
>()

function getDocumentFontFaceSet(): MonacoFontFaceSet | null {
  const globalDocument = (
    globalThis as typeof globalThis & {
      document?: { fonts?: MonacoFontFaceSet }
    }
  ).document
  return globalDocument?.fonts ?? null
}

export function remeasureMonacoFontsAfterLoad(
  monaco: MonacoFontRemeasureApi,
  request: MonacoFontMeasurementRequest,
  fontFaceSet: MonacoFontFaceSet | null = getDocumentFontFaceSet(),
): Promise<void> {
  const fontFamily = request.fontFamily.trim()
  if (
    !fontFaceSet ||
    !fontFamily ||
    !Number.isFinite(request.fontSize) ||
    request.fontSize <= 0
  ) {
    monaco.editor.remeasureFonts()
    return Promise.resolve()
  }

  const fontWeight = request.fontWeight?.trim() || 'normal'
  const fontRequest = `${fontWeight} ${request.fontSize}px ${fontFamily}`
  let requests = pendingFontLoads.get(fontFaceSet)
  if (!requests) {
    requests = new Map()
    pendingFontLoads.set(fontFaceSet, requests)
  }

  const existing = requests.get(fontRequest)
  if (existing) {
    existing.monacoApis.add(monaco)
    return existing.promise
  }

  let loadResult: PromiseLike<unknown>
  try {
    // Monaco can cache fallback widths as trusted before a web font swaps in.
    // Explicitly load the selected face before clearing that global cache.
    loadResult = fontFaceSet.load(fontRequest, MONACO_FONT_LOAD_SAMPLE)
  } catch {
    loadResult = Promise.resolve()
  }

  const monacoApis = new Set([monaco])
  const promise = Promise.resolve(loadResult)
    .catch(() => undefined)
    .then(() => {
      for (const api of monacoApis) {
        api.editor.remeasureFonts()
      }
    })
    .finally(() => {
      if (requests.get(fontRequest)?.monacoApis === monacoApis) {
        requests.delete(fontRequest)
      }
      if (requests.size === 0) {
        pendingFontLoads.delete(fontFaceSet)
      }
    })
  requests.set(fontRequest, { monacoApis, promise })
  return promise
}
