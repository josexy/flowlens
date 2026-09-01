import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { ExportHAR as ExportCurrentHAR } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { ExportHAR as ExportHistoryHAR } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import type * as proxyservice from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { useNotify } from '@/composables/useNotify'

const exporting = ref(false)

export interface HARExportOptions {
  historyKey?: string | null
  trafficIds?: number[]
  filenameHint?: string
}

function timestampFilenamePart(now = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return [
    now.getFullYear(),
    pad(now.getMonth() + 1),
    pad(now.getDate()),
    '-',
    pad(now.getHours()),
    pad(now.getMinutes()),
    pad(now.getSeconds()),
  ].join('')
}

export function makeHARFilename(hint?: string, now = new Date()): string {
  const safeHint = (hint ?? '')
    .trim()
    .replace(/[<>:"/\\|?*\u0000-\u001f]/g, '-')
    .replace(/[. ]+$/g, '')
    .slice(0, 80)
  return `${safeHint || `flowlens-${timestampFilenamePart(now)}`}.har`
}

function ensureHARExtension(path: string): string {
  return /\.har$/i.test(path) ? path : `${path}.har`
}

export function useHARExport() {
  const { t } = useI18n()
  const notify = useNotify()
  async function exportHAR(options: HARExportOptions = {}): Promise<boolean> {
    if (exporting.value) {
      return false
    }

    exporting.value = true
    try {
      let selectedPath: string
      try {
        selectedPath = await Dialogs.SaveFile({
          Filename: makeHARFilename(options.filenameHint),
          Filters: [
            {
              DisplayName: t('har_export.file_filter'),
              Pattern: '*.har',
            },
          ],
        })
      } catch (error) {
        if (isDialogCancelError(error)) {
          return false
        }
        notify.error(t('har_export.failed', { error: getErrorMessage(error) }))
        return false
      }

      const path = selectedPath.trim()
      if (!path) {
        return false
      }

      const trafficIds = options.trafficIds ?? []
      const result: proxyservice.HARWriteResult = options.historyKey
        ? await ExportHistoryHAR({
            key: options.historyKey,
            path: ensureHARExtension(path),
            trafficIds,
          })
        : await ExportCurrentHAR({
            path: ensureHARExtension(path),
            trafficIds,
          })

      const message = t('har_export.success', {
        exported: result.exported,
        skipped: result.skipped,
        missing: result.missingBodies,
      })
      if (result.exported === 0 || result.skipped > 0 || result.missingBodies > 0) {
        notify.warning(message)
      } else {
        notify.success(message)
      }
      return true
    } catch (error) {
      notify.error(t('har_export.failed', { error: getErrorMessage(error) }))
      return false
    } finally {
      exporting.value = false
    }
  }

  return {
    exporting,
    exportHAR,
  }
}
