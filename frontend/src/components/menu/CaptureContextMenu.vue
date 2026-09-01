<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContextMenuItem } from '@nuxt/ui'
import { UpdateHistoryAlias } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import CaptureAliasModal from '@/components/modal/CaptureAliasModal.vue'
import { useNotify } from '@/composables/useNotify'
import { useHARExport } from '@/composables/useHARExport'

const props = defineProps<{
  currentAlias: string
}>()

const emit = defineEmits<{
  aliasUpdated: [alias: string]
}>()

const { t } = useI18n()
const notify = useNotify()
const { exporting, exportHAR } = useHARExport()

const aliasModalVisible = ref(false)
const savingAlias = ref(false)

function handleSelect(key: string) {
  if (key === 'export-har') {
    void exportHAR({ filenameHint: props.currentAlias || undefined })
  } else if (key === 'set-alias') {
    aliasModalVisible.value = true
  }
}

async function handleAliasSave(aliasValue: string) {
  savingAlias.value = true
  try {
    const alias = aliasValue.trim()
    await UpdateHistoryAlias(alias)
    emit('aliasUpdated', alias)
    aliasModalVisible.value = false
    notify.success(alias ? t('capture.alias_saved') : t('capture.alias_cleared'))
  } catch (error) {
    notify.error(t('capture.alias_save_failed', { error }))
  } finally {
    savingAlias.value = false
  }
}

const menuItems = computed<ContextMenuItem[]>(() => [
  {
    label: t('har_export.export_session'),
    icon: 'i-lucide-file-down',
    disabled: exporting.value,
    onSelect: () => handleSelect('export-har'),
  },
  {
    label: t('capture.set_alias'),
    icon: 'i-lucide-tag',
    onSelect: () => handleSelect('set-alias'),
  },
])
</script>

<template>
  <UContextMenu :items="menuItems">
    <slot />
  </UContextMenu>
  <CaptureAliasModal
    :show="aliasModalVisible"
    :current-alias="props.currentAlias"
    :saving="savingAlias"
    @update:show="aliasModalVisible = $event"
    @save="handleAliasSave"
  />
</template>
