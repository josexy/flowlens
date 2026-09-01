<script setup lang="ts">
import { computed, ref, useAttrs } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContextMenuItem } from '@nuxt/ui'
import { useHistoryStore } from '@/stores/history'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import { useNotify } from '@/composables/useNotify'
import { useHARExport } from '@/composables/useHARExport'
import { getErrorMessage } from '@/utils/dialog'
import { isHARExportableHistoryFormat } from '@/utils/traffic'

const { t } = useI18n()
const notify = useNotify()
const { exporting, exportHAR } = useHARExport()
const historyStore = useHistoryStore()
const attrs = useAttrs()

const contextMenuKey = ref<string | null>(null)
const deleteModalVisible = ref(false)
const deleting = ref(false)
const contextMetadata = computed(() =>
  historyStore.metadataList.find((item) => item.key === contextMenuKey.value),
)
const canExportHAR = computed(() =>
  isHARExportableHistoryFormat(contextMetadata.value?.formatVersion),
)

/** Set the history key the menu should act on (called on a row's contextmenu). */
function setKey(key: string) {
  contextMenuKey.value = key
}

function setDeleteModalVisible(visible: boolean) {
  if (!deleting.value) {
    deleteModalVisible.value = visible
  }
}

function handleSelect(key: string) {
  if (deleting.value) {
    return
  }
  const historyKey = contextMenuKey.value
  if (!historyKey) return

  if (key === 'export-har') {
    if (!canExportHAR.value) {
      return
    }
    void exportHAR({
      historyKey,
      filenameHint: contextMetadata.value?.alias || historyKey,
    })
  } else if (key === 'delete') {
    deleteModalVisible.value = true
  }
}

async function handleDeleteHistory() {
  if (deleting.value) {
    return
  }
  const historyKey = contextMenuKey.value
  if (!historyKey) {
    return
  }

  try {
    deleting.value = true
    await historyStore.deleteHistory(historyKey)
    deleteModalVisible.value = false
    contextMenuKey.value = null
    notify.success(t('history.deleted'))
  } catch (error) {
    notify.error(getErrorMessage(error))
  } finally {
    deleting.value = false
  }
}

const menuItems = computed<ContextMenuItem[]>(() => [
  {
    label: t('har_export.export_session'),
    icon: 'i-lucide-file-down',
    disabled: exporting.value || !canExportHAR.value,
    onSelect: () => handleSelect('export-har'),
  },
  {
    label: t('history.delete'),
    icon: 'i-lucide-trash-2',
    disabled: deleting.value,
    onSelect: () => handleSelect('delete'),
  },
])

defineExpose({ setKey })
</script>

<template>
  <div v-bind="attrs">
    <UContextMenu :items="menuItems">
      <slot />
    </UContextMenu>
    <ConfirmCardModal
      :show="deleteModalVisible"
      :title="t('history.delete')"
      :positive-text="t('history.delete')"
      :negative-text="t('history.cancel')"
      positive-type="error"
      :positive-disabled="deleting"
      :positive-loading="deleting"
      :negative-disabled="deleting"
      :closable="!deleting"
      :mask-closable="!deleting"
      @update:show="setDeleteModalVisible"
      @positive-click="handleDeleteHistory"
    >
      {{ t('history.deleteConfirm') }}
    </ConfirmCardModal>
  </div>
</template>
