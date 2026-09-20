import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { ExportTraffic as ExportCurrentTraffic, GetTrafficExportGeneration } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { ExportTraffic as ExportHistoryTraffic } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import type { TrafficEntry, TrafficExportKind } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { trafficExportExtension, type TrafficExportFormat } from '@/utils/trafficExport'
import { useNotify } from '@/composables/useNotify'
import { makeHARFilename, useHARExport, type HARExportOptions } from '@/composables/useHARExport'
import { trafficExporting as exporting } from '@/composables/trafficExportState'

export interface TrafficExportOptions extends HARExportOptions {
  entry?: TrafficEntry
}

export function useTrafficExport() {
  const { t } = useI18n()
  const notify = useNotify()
  const { exportHAR } = useHARExport()

  async function exportTraffic(format: TrafficExportFormat, options: TrafficExportOptions = {}): Promise<boolean> {
    if (exporting.value || options.trafficIds?.length === 0) return false
    const snapshot = { ...options, trafficIds: options.trafficIds ? [...new Set(options.trafficIds)] : undefined }
    if (format === 'har') return exportHAR(snapshot)
    exporting.value = true
    try {
      const captureGeneration = snapshot.historyKey ? undefined : await GetTrafficExportGeneration()
      const directory = format !== 'csv' && snapshot.trafficIds?.length !== 1
      const extension = trafficExportExtension(format, snapshot.entry)
      const filename = `${makeHARFilename(snapshot.filenameHint).slice(0, -4)}-${format}${extension}`
      const selected = directory
        ? (await Dialogs.OpenFile({
            Title: t('traffic_export.choose_directory'),
            CanChooseDirectories: true,
            CanChooseFiles: false,
            CanCreateDirectories: true,
            AllowsMultipleSelection: false,
          }))[0] ?? ''
        : await Dialogs.SaveFile({
            Filename: filename,
            Filters: [{ DisplayName: t('traffic_export.formats.' + format), Pattern: `*${extension}` }],
          })
      let path = selected.trim()
      if (!path) return false
      if (!directory && !/\.[^./\\]+$/.test(path)) path += extension
      const request = {
        kind: format as TrafficExportKind,
        path,
        targetType: directory ? 'directory' : 'file',
        trafficIds: snapshot.trafficIds ?? [],
        ...(captureGeneration !== undefined ? { captureGeneration } : {}),
      }
      const result = snapshot.historyKey
        ? await ExportHistoryTraffic({ ...request, key: snapshot.historyKey })
        : await ExportCurrentTraffic(request)
      const message = t('traffic_export.result', {
        exported: result.exported,
        skipped: result.skipped,
      })
      const detail = result.path ? `${message}\n${result.path}` : message
      if (result.exported === 0 || result.skipped > 0 || result.headersDegraded) {
        notify.warning(detail)
      } else {
        notify.success(detail)
      }
      return result.exported > 0
    } catch (error) {
      if (!isDialogCancelError(error)) {
        notify.error(t('traffic_export.failed', { error: getErrorMessage(error) }))
      }
      return false
    } finally {
      exporting.value = false
    }
  }

  return { exporting, exportTraffic }
}
