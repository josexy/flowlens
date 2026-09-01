<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  APICollectionNodeType,
  type APICollectionRequest,
} from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'
import ApiCollectionContextMenu from '@/components/api-collection/ApiCollectionContextMenu.vue'
import ApiCollectionTree from '@/components/api-collection/ApiCollectionTree.vue'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import AppLoading from '@/components/common/AppLoading.vue'
import AppTooltip from '@/components/common/AppTooltip.vue'
import { useNotify } from '@/composables/useNotify'
import { useApiCollectionStore } from '@/stores/apiCollection'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import type { ApiCollectionEntry, ApiCollectionTreeOption } from '@/types/api-collection'

const { t } = useI18n()
const notify = useNotify()
const collectionStore = useApiCollectionStore()
const workspaceStore = useTrafficWorkspaceStore()

interface DeleteTargetSnapshot {
  id: string
  name: string
  isFolder: boolean
}

const creatingFolder = ref(false)
const folderName = ref('')
const folderParentId = ref('')
const multiSelectEnabled = ref(false)
const checkedNodeIds = ref<string[]>([])
const contextMenuNodeType = ref<APICollectionNodeType | null>(null)
const contextMenuNodeId = ref('')
const deleteModalVisible = ref(false)
const deleteTargetSnapshot = ref<DeleteTargetSnapshot[]>([])
const deleteFromMultiSelectToolbar = ref(false)
const deleting = ref(false)
const renameModalVisible = ref(false)
const renameNodeId = ref('')
const renameName = ref('')
const renaming = ref(false)
const copying = ref(false)
let openRequestSequence = 0

const canSubmitRename = computed(() => {
  return renameName.value.trim().length > 0 && !renaming.value
})

const multiSelectLabel = computed(() => {
  return t(
    multiSelectEnabled.value
      ? 'api_collection.exit_multi_select'
      : 'api_collection.multi_select',
  )
})

const expansionToggleLabel = computed(() =>
  t(
    collectionStore.allFoldersExpanded
      ? 'api_collection.collapse_all'
      : 'api_collection.expand_all',
  ),
)

const expansionToggleIcon = computed(() =>
  collectionStore.allFoldersExpanded
    ? 'i-lucide-list-chevrons-down-up'
    : 'i-lucide-list-chevrons-up-down',
)

const effectiveDeleteRootIds = computed(() => {
  const normalizedCheckedNodeIds: string[] = []
  const checkedNodeIdSet = new Set<string>()

  for (const rawNodeId of checkedNodeIds.value) {
    const nodeId = rawNodeId.trim()
    if (!nodeId || checkedNodeIdSet.has(nodeId) || !collectionStore.nodeMap.has(nodeId)) {
      continue
    }
    checkedNodeIdSet.add(nodeId)
    normalizedCheckedNodeIds.push(nodeId)
  }

  return normalizedCheckedNodeIds.filter((nodeId) => {
    const visitedFolderIds = new Set<string>()
    let parentFolderId = collectionStore.parentFolderIdMap.get(nodeId) ?? ''
    while (parentFolderId && !visitedFolderIds.has(parentFolderId)) {
      if (checkedNodeIdSet.has(parentFolderId)) {
        return false
      }
      visitedFolderIds.add(parentFolderId)
      parentFolderId = collectionStore.parentFolderIdMap.get(parentFolderId) ?? ''
    }
    return true
  })
})

const deleteToolbarDisabled = computed(() => {
  if (deleting.value) {
    return true
  }
  return multiSelectEnabled.value
    ? effectiveDeleteRootIds.value.length === 0
    : !collectionStore.selectedNodeId
})

async function refreshCollection() {
  try {
    await collectionStore.loadCollection()
  } catch (error) {
    notify.error(t('api_collection.load_failed', { error: String(error) }))
  }
}

onMounted(() => {
  if (collectionStore.nodes.length === 0 && !collectionStore.loading && !collectionStore.error) {
    void refreshCollection()
  }
})

function updateCheckedNodeIds(nodeIds: string[]) {
  const validNodeIds = new Set(collectionStore.nodes.map((node) => node.id))
  const nextCheckedNodeIds: string[] = []
  const seenNodeIds = new Set<string>()

  for (const rawNodeId of nodeIds) {
    const nodeId = rawNodeId.trim()
    if (!nodeId || seenNodeIds.has(nodeId) || !validNodeIds.has(nodeId)) {
      continue
    }
    seenNodeIds.add(nodeId)
    nextCheckedNodeIds.push(nodeId)
  }

  checkedNodeIds.value = nextCheckedNodeIds
}

watch(
  () => collectionStore.nodes.map((node) => node.id),
  () => updateCheckedNodeIds(checkedNodeIds.value),
  { immediate: true },
)

function exitMultiSelect() {
  multiSelectEnabled.value = false
  checkedNodeIds.value = []
}

function toggleMultiSelect() {
  if (multiSelectEnabled.value) {
    exitMultiSelect()
    return
  }
  cancelFolderCreate()
  multiSelectEnabled.value = true
}

function beginFolderCreate(parentId = '') {
  if (multiSelectEnabled.value) {
    exitMultiSelect()
  }
  creatingFolder.value = true
  folderParentId.value = parentId
  folderName.value = ''
  if (parentId && !collectionStore.expandedFolderIds.includes(parentId)) {
    collectionStore.toggleExpandedFolder(parentId)
  }
  collectionStore.selectNode(parentId || null)
}

async function createFolder() {
  const trimmedName = folderName.value.trim()
  if (!trimmedName) {
    notify.warning(t('api_collection.folder_name_required'))
    return
  }
  try {
    await collectionStore.createFolder(folderParentId.value, trimmedName)
    creatingFolder.value = false
    folderName.value = ''
    folderParentId.value = ''
  } catch (error) {
    notify.error(t('api_collection.create_folder_failed', { error: String(error) }))
  }
}

async function moveNode(nodeId: string, newParentId: string) {
  if (collectionStore.mutating) {
    return
  }
  try {
    await collectionStore.moveNode(nodeId, newParentId)
  } catch (error) {
    notify.error(t('api_collection.move_failed', { error: String(error) }))
  }
}

function cancelFolderCreate() {
  creatingFolder.value = false
  folderName.value = ''
  folderParentId.value = ''
}

function isFolderEntry(entry: ApiCollectionEntry | null | undefined) {
  return Boolean(entry && 'folders' in entry && 'requests' in entry)
}

function isRequestEntry(
  entry: ApiCollectionEntry | null | undefined,
): entry is APICollectionRequest {
  return Boolean(entry && 'type' in entry)
}

function getEntryName(entry: ApiCollectionEntry) {
  return entry.name
}

function getTreeNodeId(row: { id?: string; key?: string }) {
  return row.id ?? row.key ?? ''
}

function showNodeContextMenu(_event: MouseEvent, row: ApiCollectionTreeOption) {
  // Select the target node and set the menu node-type before the native
  // contextmenu event bubbles to the wrapping UContextMenu (which opens at the pointer).
  contextMenuNodeId.value = getTreeNodeId(row)
  collectionStore.selectNode(contextMenuNodeId.value)
  contextMenuNodeType.value = row.type
}

function createFolderFromContext() {
  const selectedNode = collectionStore.selectedNode
  if (!isFolderEntry(selectedNode)) {
    return
  }
  beginFolderCreate(selectedNode!.id)
}

function openDeleteConfirmation(nodeIds: string[], fromMultiSelectToolbar: boolean) {
  const targets: DeleteTargetSnapshot[] = []
  const seenNodeIds = new Set<string>()

  for (const rawNodeId of nodeIds) {
    const nodeId = rawNodeId.trim()
    if (!nodeId || seenNodeIds.has(nodeId)) {
      continue
    }
    const node = collectionStore.nodeMap.get(nodeId)
    if (!node) {
      continue
    }
    seenNodeIds.add(nodeId)
    targets.push({
      id: node.id,
      name: getEntryName(node),
      isFolder: isFolderEntry(node),
    })
  }

  if (targets.length === 0) {
    return
  }

  deleteTargetSnapshot.value = targets
  deleteFromMultiSelectToolbar.value = fromMultiSelectToolbar
  deleteModalVisible.value = true
}

function confirmDeleteFromToolbar() {
  if (multiSelectEnabled.value) {
    openDeleteConfirmation(effectiveDeleteRootIds.value, true)
    return
  }

  const selectedNodeId = collectionStore.selectedNodeId
  if (selectedNodeId) {
    openDeleteConfirmation([selectedNodeId], false)
  }
}

function confirmDeleteFromContextMenu() {
  if (contextMenuNodeId.value) {
    openDeleteConfirmation([contextMenuNodeId.value], false)
  }
}

function beginRenameSelected() {
  const node = collectionStore.selectedNode
  if (!node) {
    return
  }
  renameNodeId.value = node.id
  renameName.value = getEntryName(node)
  renameModalVisible.value = true
}

function resetRenameForm() {
  renameNodeId.value = ''
  renameName.value = ''
}

function handleRenameModalVisibleUpdate(value: boolean) {
  renameModalVisible.value = value
  if (!value) {
    resetRenameForm()
  }
}

const deleteModalContent = computed(() => {
  const targets = deleteTargetSnapshot.value
  if (targets.length === 0) {
    return ''
  }
  if (targets.length > 1) {
    return t('api_collection.delete_multi_confirm', { count: targets.length })
  }
  const target = targets[0]!
  return target.isFolder
    ? t('api_collection.delete_folder_confirm', { name: target.name })
    : t('api_collection.delete_api_confirm', { name: target.name })
})

function resetDeleteConfirmation() {
  deleteTargetSnapshot.value = []
  deleteFromMultiSelectToolbar.value = false
}

function handleDeleteModalVisibleUpdate(value: boolean) {
  if (deleting.value) {
    return
  }
  deleteModalVisible.value = value
  if (!value) {
    resetDeleteConfirmation()
  }
}

async function handleDeleteSelected() {
  if (deleting.value || deleteTargetSnapshot.value.length === 0) {
    return
  }

  const targetIds = deleteTargetSnapshot.value.map((target) => target.id)
  const shouldClearCheckedNodeIds = deleteFromMultiSelectToolbar.value

  deleting.value = true
  try {
    await collectionStore.deleteNodes(targetIds)
    deleteModalVisible.value = false
    if (shouldClearCheckedNodeIds) {
      checkedNodeIds.value = []
    }
    resetDeleteConfirmation()
    notify.success(t('api_collection.deleted'))
  } catch (error) {
    notify.error(String(error))
  } finally {
    deleting.value = false
  }
}

async function handleRenameSelected() {
  const nodeId = renameNodeId.value
  const trimmedName = renameName.value.trim()
  if (renaming.value) {
    return
  }
  if (!nodeId) {
    return
  }
  if (!trimmedName) {
    notify.warning(t('api_collection.rename_name_required'))
    return
  }

  renaming.value = true
  try {
    await collectionStore.renameNode(nodeId, trimmedName)
    renameModalVisible.value = false
    resetRenameForm()
    notify.success(t('api_collection.renamed'))
  } catch (error) {
    notify.error(t('api_collection.rename_failed', { error: String(error) }))
  } finally {
    renaming.value = false
  }
}

async function handleCopySelected() {
  if (copying.value) {
    return
  }
  const node = collectionStore.selectedNode
  if (!isRequestEntry(node)) {
    return
  }

  copying.value = true
  try {
    await collectionStore.duplicateRequestNode(node.id)
    notify.success(t('api_collection.copied'))
  } catch (error) {
    notify.error(t('api_collection.copy_failed', { error: String(error) }))
  } finally {
    copying.value = false
  }
}

async function openRequest(row: { id?: string; key?: string }) {
  const requestSequence = ++openRequestSequence
  const nodeId = getTreeNodeId(row)
  collectionStore.selectNode(nodeId)
  const entry = collectionStore.nodeMap.get(nodeId)
  if (!isRequestEntry(entry)) {
    notify.error(t('api_collection.open_failed', { error: 'request not found' }))
    return
  }
  try {
    if (workspaceStore.activateSavedApiTab(nodeId)) {
      return
    }
    const request = await collectionStore.getRequestNode(nodeId)
    if (requestSequence !== openRequestSequence) {
      return
    }
    if (!workspaceStore.openSavedApi(request)) {
      throw new Error('request details are incomplete')
    }
  } catch (error) {
    if (requestSequence === openRequestSequence) {
      notify.error(t('api_collection.open_failed', { error: String(error) }))
    }
  }
}
</script>

<template>
  <div class="flex h-full w-full flex-col overflow-hidden bg-app-sidebar">
    <div
      class="flex min-h-10 items-center justify-between bg-app-sidebar-header pl-3 pr-2.5 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div class="flex items-center gap-1.5 text-sm font-semibold text-app-text-muted">
        <span>{{ t('api_collection.title') }}</span>
      </div>
      <div class="flex items-center gap-1.5">
        <AppTooltip :text="t('api_collection.refresh')" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-refresh-cw"
              :disabled="collectionStore.loading"
              :aria-label="t('api_collection.refresh')"
              @click="refreshCollection"
            />
          </template>
        </AppTooltip>
        <AppTooltip :text="t('api_collection.new_folder')" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-plus"
              :aria-label="t('api_collection.new_folder')"
              @click="beginFolderCreate()"
            />
          </template>
        </AppTooltip>
        <AppTooltip :text="expansionToggleLabel" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              :icon="expansionToggleIcon"
              :disabled="collectionStore.loading || !collectionStore.hasExpandableFolders"
              :aria-label="expansionToggleLabel"
              @click="collectionStore.toggleAllFoldersExpanded"
            />
          </template>
        </AppTooltip>
        <AppTooltip :text="multiSelectLabel" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              :color="multiSelectEnabled ? 'primary' : 'neutral'"
              :variant="multiSelectEnabled ? 'soft' : 'ghost'"
              icon="i-lucide-list-checks"
              :disabled="!multiSelectEnabled && collectionStore.nodes.length === 0"
              :aria-label="multiSelectLabel"
              :aria-pressed="multiSelectEnabled"
              @click="toggleMultiSelect"
            />
          </template>
        </AppTooltip>
        <AppTooltip :text="t('api_collection.delete')" placement="bottom" :delay="500">
          <template #trigger>
            <UButton
              size="sm"
              color="neutral"
              variant="ghost"
              icon="i-lucide-trash-2"
              :disabled="deleteToolbarDisabled"
              :aria-label="t('api_collection.delete')"
              @click="confirmDeleteFromToolbar"
            />
          </template>
        </AppTooltip>
      </div>
    </div>

    <div class="flex min-h-0 flex-1 flex-col">
      <AppLoading
        v-if="collectionStore.loading"
        fill
        size="md"
        class="p-4"
      />
      <div
        v-else-if="collectionStore.error"
        class="flex flex-1 items-center justify-center p-4 text-center text-sm text-app-text-muted"
        role="status"
      >
        {{ collectionStore.error }}
      </div>
      <div
        v-else-if="collectionStore.nodes.length === 0 && !creatingFolder"
        class="flex flex-1 items-center justify-center p-4 text-center text-sm text-app-text-muted"
        role="status"
      >
        {{ t('api_collection.empty') }}
      </div>
      <div v-else class="min-h-0 flex-1 overflow-hidden">
        <ApiCollectionContextMenu
          :node-type="contextMenuNodeType"
          @create-folder="createFolderFromContext"
          @rename-node="beginRenameSelected"
          @copy-node="handleCopySelected"
          @delete-node="confirmDeleteFromContextMenu"
        >
          <ApiCollectionTree
            v-model:checked-node-ids="checkedNodeIds"
            v-model:creating-folder-name="folderName"
            :creating-folder-parent-id="creatingFolder ? folderParentId : null"
            :expanded-folder-ids="collectionStore.expandedFolderIds"
            :creating-folder-placeholder="t('api_collection.folder_name_placeholder')"
            :drag-disabled="collectionStore.loading || collectionStore.mutating || creatingFolder"
            :multi-select-enabled="multiSelectEnabled"
            :options="collectionStore.collectionTreeOptions"
            :selected-node-id="collectionStore.selectedNodeId"
            @toggle-folder="collectionStore.toggleExpandedFolder"
            @open-request="openRequest"
            @context-menu-node="showNodeContextMenu"
            @create-folder="createFolder"
            @cancel-folder-create="cancelFolderCreate"
            @move-node="moveNode"
          />
        </ApiCollectionContextMenu>
      </div>
    </div>

    <ConfirmCardModal
      :show="deleteModalVisible"
      :title="t('api_collection.delete_title')"
      :positive-text="t('api_collection.delete')"
      :negative-text="t('api_collection.cancel')"
      positive-type="error"
      :positive-disabled="deleting || deleteTargetSnapshot.length === 0"
      :positive-loading="deleting"
      :negative-disabled="deleting"
      :closable="!deleting"
      :mask-closable="!deleting"
      @update:show="handleDeleteModalVisibleUpdate"
      @positive-click="handleDeleteSelected"
    >
      {{ deleteModalContent }}
    </ConfirmCardModal>
    <ConfirmCardModal
      :show="renameModalVisible"
      :title="t('api_collection.rename_title')"
      :positive-text="t('api_collection.rename')"
      :negative-text="t('api_collection.cancel')"
      :positive-disabled="!canSubmitRename"
      :positive-loading="renaming"
      @update:show="handleRenameModalVisibleUpdate"
      @positive-click="handleRenameSelected"
    >
      <UInput
        v-model="renameName"
        autofocus
        class="w-full"
        :placeholder="t('api_collection.rename_name_placeholder')"
        :disabled="renaming || copying"
        @keyup.enter="handleRenameSelected"
      />
    </ConfirmCardModal>
  </div>
</template>
