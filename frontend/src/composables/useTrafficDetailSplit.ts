import { computed, nextTick, reactive, ref, watch, type Ref } from 'vue'
import { storeToRefs } from 'pinia'
import type { SplitterPanel } from 'reka-ui'
import { TrafficDetailLayout } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import { useSettingStore } from '@/stores/setting'

// Remember each orientation only while its owner is mounted. Resize the existing
// panels so changing orientation preserves the table and detail pane instances.
export function useTrafficDetailSplit(defaultSize: number, visible: Ref<boolean>) {
  const { trafficDetailLayout: layout } = storeToRefs(useSettingStore())
  const firstPanel = ref<InstanceType<typeof SplitterPanel> | null>(null)
  const sizes = reactive({
    [TrafficDetailLayout.TrafficDetailLayoutVertical]: defaultSize,
    [TrafficDetailLayout.TrafficDetailLayoutHorizontal]: defaultSize,
  })
  const firstPanelSize = computed(() => sizes[layout.value])
  let restoring = false

  function handleLayout(nextSizes: number[]) {
    const size = nextSizes[0]
    if (
      !restoring && visible.value && nextSizes.length === 2
      && typeof size === 'number' && Number.isFinite(size)
    ) {
      sizes[layout.value] = size
    }
  }

  watch([layout, visible], async ([, isVisible], _, onCleanup) => {
    let cancelled = false
    onCleanup(() => {
      cancelled = true
    })
    restoring = true
    await nextTick()
    if (cancelled) return
    if (isVisible) firstPanel.value?.resize(firstPanelSize.value)
    restoring = false
  })

  return { layout, firstPanel, firstPanelSize, handleLayout }
}
