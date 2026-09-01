<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContextMenuItem } from '@nuxt/ui'
import { APICollectionNodeType } from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'

const props = defineProps<{
  nodeType: APICollectionNodeType | null
}>()

const emit = defineEmits<{
  createFolder: []
  renameNode: []
  copyNode: []
  deleteNode: []
}>()

const { t } = useI18n()
const preventNextCloseAutoFocus = ref(false)

function item(key: string, label: string): ContextMenuItem {
  return {
    label,
    onSelect: () => handleSelect(key),
  }
}

const menuItems = computed<ContextMenuItem[]>(() => {
  if (props.nodeType === APICollectionNodeType.APICollectionNodeTypeFolder) {
    return [
      item('create-folder', t('api_collection.new_child_folder')),
      { type: 'separator' },
      item('rename', t('api_collection.rename')),
      { type: 'separator' },
      item('delete', t('api_collection.delete')),
    ]
  }
  if (props.nodeType) {
    return [
      item('rename', t('api_collection.rename')),
      item('copy', t('api_collection.copy')),
      { type: 'separator' },
      item('delete', t('api_collection.delete')),
    ]
  }
  return []
})

const contextMenuContent = computed(() => ({
  onCloseAutoFocus: handleCloseAutoFocus,
}))

function handleCloseAutoFocus(event: Event) {
  if (!preventNextCloseAutoFocus.value) {
    return
  }
  event.preventDefault()
  preventNextCloseAutoFocus.value = false
}

function handleSelect(key: string) {
  if (key === 'create-folder') {
    preventNextCloseAutoFocus.value = true
    emit('createFolder')
    return
  }
  if (key === 'rename') {
    emit('renameNode')
    return
  }
  if (key === 'copy') {
    emit('copyNode')
    return
  }
  if (key === 'delete') {
    emit('deleteNode')
  }
}
</script>

<template>
  <UContextMenu :items="menuItems" :content="contextMenuContent">
    <div class="flex h-full min-h-0 w-full">
      <slot />
    </div>
  </UContextMenu>
</template>
