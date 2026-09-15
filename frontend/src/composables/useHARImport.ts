import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { ImportHAR } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import type { HistoryMetadata } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import { useHistoryStore } from '@/stores/history'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { useNotify } from '@/composables/useNotify'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { uniqueHARImportPaths } from '@/utils/harImport'
import { inferPlatformFromUserAgent } from '@/runtime/platform'

const importing = ref(false)
const currentFile = ref(0)
const totalFiles = ref(0)

export function useHARImport() {
  const { t } = useI18n()
  const historyStore = useHistoryStore()
  const workspaceStore = useTrafficWorkspaceStore()
  const workbenchStore = useWorkbenchStore()
  const notify = useNotify()
  const importLabel = computed(() => importing.value && totalFiles.value > 0
    ? t('har_import.progress', { current: currentFile.value, total: totalFiles.value })
    : t('har_import.title'))

  async function runImport(paths: string[]) {
    const uniquePaths = uniqueHARImportPaths(paths, inferPlatformFromUserAgent() === 'windows')
    if (!uniquePaths.length) return
    totalFiles.value = uniquePaths.length
    const successes: HistoryMetadata[] = []
    let failures = 0
    let firstFailure = ''
    let imported = 0
    let skipped = 0
    let missing = 0
    for (const [index, path] of uniquePaths.entries()) {
      currentFile.value = index + 1
      const filename = path.split(/[\\/]/).at(-1) || path
      try {
        const result = await ImportHAR({ path })
        imported += result.imported
        skipped += result.skipped
        missing += result.missingBodies
        if (result.metadata) successes.push(result.metadata)
        else {
          failures++
          firstFailure ||= t('har_import.file_failed', {
            file: filename,
            error: t('har_import.no_entries'),
          })
        }
      } catch (error) {
        failures++
        firstFailure ||= t('har_import.file_failed', {
          file: filename,
          error: getErrorMessage(error),
        })
        console.error(`Failed to import HAR file: ${path}`, error)
      }
    }
    if (successes.length) {
      await historyStore.loadList()
      const first = successes.find((item) => historyStore.metadataList.some((metadata) => metadata.key === item.key))
      if (first) {
        workbenchStore.selectCaptureItem(`history:${first.key}`)
        workspaceStore.openHistoryTab(first)
      } else {
        failures++
        firstFailure ||= t('har_import.open_failed')
      }
    }
    const summary = t('har_import.result', { files: successes.length, imported, skipped, missing, failed: failures })
    if (failures && successes.length === 0) {
      notify.error(firstFailure || summary, undefined, 8000)
    } else if (failures) {
      notify.warning(firstFailure || summary, firstFailure ? summary : undefined, 8000)
    } else if (skipped || missing) {
      notify.warning(summary, undefined, 5000)
    } else {
      notify.success(summary, undefined, 5000)
    }
  }

  async function importHARFiles(paths?: string[]) {
    if (importing.value) return
    importing.value = true
    currentFile.value = 0
    totalFiles.value = 0
    try {
      const selectedPaths = paths ?? await Dialogs.OpenFile({
        CanChooseFiles: true,
        CanChooseDirectories: false,
        AllowsMultipleSelection: true,
        AllowsOtherFiletypes: false,
        Filters: [{ DisplayName: t('har_import.file_filter'), Pattern: '*.har' }],
      })
      await runImport(selectedPaths)
    } catch (error) {
      if (!isDialogCancelError(error)) notify.error(t('har_import.failed', { error: getErrorMessage(error) }))
    } finally {
      importing.value = false
      currentFile.value = 0
      totalFiles.value = 0
    }
  }

  return { importing, importLabel, importHARFiles }
}
