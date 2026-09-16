import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { useHARImport } from './useHARImport'
import { HAR_FILE_DROP_EVENT } from '@/runtime/appEvents'
import { canHandleHARFileDrop, harImportDropTarget, parseHARFileDrop, type HARImportSurface } from '@/utils/harImport'

export function useHARImportDrop(surface: HARImportSurface) {
  const workspace = useTrafficWorkspaceStore()
  const workbench = useWorkbenchStore()
  const { importing, importHARFiles } = useHARImport()
  const dragDepth = ref(0)
  const enabled = computed(() => canHandleHARFileDrop(surface, workbench.activeContent, workspace.activeTab.type, true) && !importing.value)
  const dropActive = computed(() => enabled.value && dragDepth.value > 0)
  const dropTarget = computed(() => enabled.value ? harImportDropTarget(surface) : undefined)
  const resetDrop = () => { dragDepth.value = 0 }
  watch(enabled, resetDrop)

  function accepts(event: DragEvent): boolean {
    return enabled.value && !!event.dataTransfer?.types.includes('Files')
  }
  function onDragEnter(event: DragEvent) {
    if (!accepts(event)) return
    event.preventDefault()
    dragDepth.value++
  }
  function onDragOver(event: DragEvent) {
    if (!accepts(event)) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy'
    dragDepth.value = Math.max(dragDepth.value, 1)
  }
  function onDragLeave(event: DragEvent) {
    if (!event.dataTransfer?.types.includes('Files')) return
    dragDepth.value = Math.max(0, dragDepth.value - 1)
  }

  const off = Events.On(HAR_FILE_DROP_EVENT, (event) => {
    const payload = parseHARFileDrop(event.data)
    if (!payload || payload.surface !== surface) return
    resetDrop()
    if (!enabled.value || document.visibilityState === 'hidden') return
    void importHARFiles(payload.paths)
  })
  window.addEventListener('blur', resetDrop)
  window.addEventListener('dragend', resetDrop)
  window.addEventListener('drop', resetDrop)
  onBeforeUnmount(() => {
    off()
    window.removeEventListener('blur', resetDrop)
    window.removeEventListener('dragend', resetDrop)
    window.removeEventListener('drop', resetDrop)
  })
  return { dropActive, dropTarget, onDragEnter, onDragOver, onDragLeave, resetDrop }
}
