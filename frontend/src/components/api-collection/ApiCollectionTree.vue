<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { TreeItem } from '@nuxt/ui'
import { useI18n } from 'vue-i18n'
import { APICollectionNodeType } from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'
import type { ApiCollectionTreeOption } from '@/types/api-collection'

const CREATE_NODE_KEY = '__api_collection_create_folder__'
const DRAG_EXPAND_DELAY_MS = 600
const AUTO_SCROLL_EDGE_SIZE = 32
const AUTO_SCROLL_MAX_STEP = 12

const methodColorMap: Record<string, string> = {
  GET: '#16a34a',
  POST: '#2563eb',
  PUT: '#d97706',
  DELETE: '#dc2626',
  PATCH: '#7c3aed',
  HEAD: '#0891b2',
  OPTIONS: '#4f46e5',
  WS: '#0891b2',
}

interface CollectionTreeNode extends TreeItem {
  key: string
  label: string
  type: ApiCollectionTreeOption['type'] | 'create-folder'
  depth?: number
  folder?: ApiCollectionTreeOption['folder']
  request?: ApiCollectionTreeOption['request']
  source?: ApiCollectionTreeOption
  children?: CollectionTreeNode[]
}

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    options: ApiCollectionTreeOption[]
    expandedFolderIds: string[]
    selectedNodeId?: string | null
    multiSelectEnabled?: boolean
    checkedNodeIds?: string[]
    dragDisabled?: boolean
    creatingFolderParentId?: string | null
    creatingFolderName?: string
    creatingFolderPlaceholder?: string
  }>(),
  {
    multiSelectEnabled: false,
    checkedNodeIds: () => [],
    dragDisabled: false,
  },
)

const emit = defineEmits<{
  toggleFolder: [folderId: string]
  openRequest: [option: ApiCollectionTreeOption]
  contextMenuNode: [event: MouseEvent, option: ApiCollectionTreeOption]
  'update:checkedNodeIds': [value: string[]]
  'update:creatingFolderName': [value: string]
  createFolder: []
  cancelFolderCreate: []
  moveNode: [nodeId: string, newParentId: string]
}>()

type CreateFolderInputRef = {
  inputRef?: HTMLInputElement | null
}

type TreeComponentRef = {
  $el?: HTMLElement | null
}

const createFolderInputRef = ref<CreateFolderInputRef | null>(null)
const treeRef = ref<TreeComponentRef | null>(null)
const draggedNodeId = ref('')
const dropTargetFolderId = ref('')
const rootDropActive = ref(false)
const suppressNextNodeClick = ref(false)
let hoverExpandFolderId = ''
let hoverExpandTimerId = 0
let autoScrollFrameId = 0
let autoScrollStep = 0
let clickUnlockFrameId = 0

function getNodeKey(node: CollectionTreeNode) {
  return node.key
}

const treeOptions = computed<CollectionTreeNode[]>(() => {
  const createOption: CollectionTreeNode = {
    key: CREATE_NODE_KEY,
    label: '',
    type: 'create-folder',
  }

  const attachCreateOption = (
    options: ApiCollectionTreeOption[],
    depth = 0,
  ): CollectionTreeNode[] => {
    return options.map((option) => {
      const children = attachCreateOption(option.children ?? [], depth + 1)
      const shouldAttachCreateRow =
        props.creatingFolderParentId &&
        option.key === props.creatingFolderParentId &&
        option.type === APICollectionNodeType.APICollectionNodeTypeFolder

      const nestedChildren = shouldAttachCreateRow ? [createOption, ...children] : children
      const treeOption: CollectionTreeNode = {
        key: option.key,
        label: option.label,
        type: option.type,
        depth,
        folder: option.folder,
        request: option.request,
        source: option,
      }
      if (nestedChildren.length > 0) {
        treeOption.children = nestedChildren
      }
      return treeOption
    })
  }

  const options = attachCreateOption(props.options)
  if (props.creatingFolderParentId === '') {
    return [createOption, ...options]
  }
  return options
})

const parentFolderIdMap = computed(() => {
  const parents = new Map<string, string>()
  const visit = (options: ApiCollectionTreeOption[], parentId: string) => {
    for (const option of options) {
      parents.set(option.key, parentId)
      visit(option.children ?? [], option.key)
    }
  }
  visit(props.options, '')
  return parents
})

const draggingEnabled = computed(
  () =>
    !props.dragDisabled &&
    !props.multiSelectEnabled &&
    (props.creatingFolderParentId === null || props.creatingFolderParentId === undefined),
)

const treeNodeMap = computed(() => {
  const nodeMap = new Map<string, CollectionTreeNode>()
  const visit = (nodes: CollectionTreeNode[]) => {
    for (const node of nodes) {
      nodeMap.set(node.key, node)
      if (node.children) {
        visit(node.children)
      }
    }
  }
  visit(treeOptions.value)
  return nodeMap
})

const draggedTreeNode = computed(() =>
  draggedNodeId.value ? treeNodeMap.value.get(draggedNodeId.value) : undefined,
)

const draggingFolder = computed(() =>
  Boolean(draggedTreeNode.value && isFolderNode(draggedTreeNode.value)),
)

const showRootDropZone = computed(
  () => draggingFolder.value && Boolean(parentFolderIdMap.value.get(draggedNodeId.value)),
)

const selectedTreeNode = computed(() => {
  const key = props.selectedNodeId
  if (!key) {
    return undefined
  }
  return treeNodeMap.value.get(key)
})

function toCollectionOption(node: CollectionTreeNode): ApiCollectionTreeOption | null {
  const option = node as CollectionTreeNode
  if (option.type === 'create-folder') {
    return null
  }
  if (option.source) {
    return option.source
  }
  if (option.folder || option.request) {
    return option as unknown as ApiCollectionTreeOption
  }
  return null
}

function isFolderNode(node: CollectionTreeNode) {
  return node.type === APICollectionNodeType.APICollectionNodeTypeFolder
}

function isCreateFolderNode(node: CollectionTreeNode) {
  return node.type === 'create-folder'
}

function clearHoverExpand() {
  if (hoverExpandTimerId) {
    window.clearTimeout(hoverExpandTimerId)
    hoverExpandTimerId = 0
  }
  hoverExpandFolderId = ''
}

function stopAutoScroll() {
  autoScrollStep = 0
  if (autoScrollFrameId) {
    window.cancelAnimationFrame(autoScrollFrameId)
    autoScrollFrameId = 0
  }
}

function runAutoScroll() {
  autoScrollFrameId = 0
  const element = treeRef.value?.$el
  if (!element || !draggedNodeId.value || autoScrollStep === 0) {
    return
  }

  const previousScrollTop = element.scrollTop
  element.scrollTop += autoScrollStep
  if (element.scrollTop !== previousScrollTop) {
    autoScrollFrameId = window.requestAnimationFrame(runAutoScroll)
  }
}

function updateAutoScroll(clientY: number) {
  const element = treeRef.value?.$el
  if (!element) {
    stopAutoScroll()
    return
  }

  const bounds = element.getBoundingClientRect()
  let nextStep = 0
  if (clientY < bounds.top + AUTO_SCROLL_EDGE_SIZE) {
    const ratio = Math.min(
      1,
      (bounds.top + AUTO_SCROLL_EDGE_SIZE - clientY) / AUTO_SCROLL_EDGE_SIZE,
    )
    nextStep = -Math.max(2, Math.ceil(AUTO_SCROLL_MAX_STEP * ratio))
  } else if (clientY > bounds.bottom - AUTO_SCROLL_EDGE_SIZE) {
    const ratio = Math.min(
      1,
      (clientY - (bounds.bottom - AUTO_SCROLL_EDGE_SIZE)) / AUTO_SCROLL_EDGE_SIZE,
    )
    nextStep = Math.max(2, Math.ceil(AUTO_SCROLL_MAX_STEP * ratio))
  }

  autoScrollStep = nextStep
  if (nextStep === 0) {
    stopAutoScroll()
  } else if (!autoScrollFrameId) {
    autoScrollFrameId = window.requestAnimationFrame(runAutoScroll)
  }
}

function releaseNodeClickSuppression() {
  if (clickUnlockFrameId) {
    window.cancelAnimationFrame(clickUnlockFrameId)
  }
  clickUnlockFrameId = window.requestAnimationFrame(() => {
    suppressNextNodeClick.value = false
    clickUnlockFrameId = 0
  })
}

function finishNodeDrag() {
  const wasDragging = Boolean(draggedNodeId.value)
  draggedNodeId.value = ''
  dropTargetFolderId.value = ''
  rootDropActive.value = false
  clearHoverExpand()
  stopAutoScroll()
  if (wasDragging) {
    suppressNextNodeClick.value = true
    releaseNodeClickSuppression()
  }
}

function isDescendantFolder(folderId: string, ancestorFolderId: string) {
  const visited = new Set<string>()
  let parentId = parentFolderIdMap.value.get(folderId) ?? ''
  while (parentId && !visited.has(parentId)) {
    if (parentId === ancestorFolderId) {
      return true
    }
    visited.add(parentId)
    parentId = parentFolderIdMap.value.get(parentId) ?? ''
  }
  return false
}

function canDropOnFolder(node: CollectionTreeNode) {
  const source = draggedTreeNode.value
  if (!source || !isFolderNode(node) || node.key === source.key) {
    return false
  }
  if ((parentFolderIdMap.value.get(source.key) ?? '') === node.key) {
    return false
  }
  return !isFolderNode(source) || !isDescendantFolder(node.key, source.key)
}

function scheduleFolderExpansion(node: CollectionTreeNode) {
  if (
    !node.children?.length ||
    props.expandedFolderIds.includes(node.key) ||
    hoverExpandFolderId === node.key
  ) {
    return
  }

  clearHoverExpand()
  hoverExpandFolderId = node.key
  hoverExpandTimerId = window.setTimeout(() => {
    hoverExpandTimerId = 0
    const target = treeNodeMap.value.get(node.key)
    if (
      dropTargetFolderId.value === node.key &&
      target &&
      canDropOnFolder(target) &&
      !props.expandedFolderIds.includes(node.key)
    ) {
      emit('toggleFolder', node.key)
    }
    hoverExpandFolderId = ''
  }, DRAG_EXPAND_DELAY_MS)
}

function handleNodeDragStart(event: DragEvent, node: CollectionTreeNode) {
  const option = toCollectionOption(node)
  if (!draggingEnabled.value || !option) {
    event.preventDefault()
    return
  }

  draggedNodeId.value = option.key
  dropTargetFolderId.value = ''
  rootDropActive.value = false
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', option.key)
    const handle = event.currentTarget as HTMLElement | null
    const row = handle?.closest<HTMLElement>('[data-api-collection-node]')
    if (row) {
      event.dataTransfer.setDragImage(row, 16, Math.min(16, row.offsetHeight / 2))
    }
  }
}

function handleNodeDragOver(event: DragEvent, node: CollectionTreeNode) {
  if (!draggedNodeId.value) {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  updateAutoScroll(event.clientY)

  if (!canDropOnFolder(node)) {
    dropTargetFolderId.value = ''
    rootDropActive.value = false
    clearHoverExpand()
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'none'
    }
    return
  }

  rootDropActive.value = false
  if (dropTargetFolderId.value !== node.key) {
    clearHoverExpand()
    dropTargetFolderId.value = node.key
    scheduleFolderExpansion(node)
  }
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
}

function eventLeftElement(event: DragEvent) {
  const currentTarget = event.currentTarget as HTMLElement | null
  const relatedTarget = event.relatedTarget
  if (!currentTarget) {
    return true
  }
  if (relatedTarget instanceof Node) {
    return !currentTarget.contains(relatedTarget)
  }
  const pointerTarget = document.elementFromPoint(event.clientX, event.clientY)
  return !(pointerTarget && currentTarget.contains(pointerTarget))
}

function handleNodeDragLeave(event: DragEvent, node: CollectionTreeNode) {
  if (!eventLeftElement(event) || dropTargetFolderId.value !== node.key) {
    return
  }
  dropTargetFolderId.value = ''
  clearHoverExpand()
}

function handleNodeDrop(event: DragEvent, node: CollectionTreeNode) {
  if (!draggedNodeId.value) {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  const sourceNodeId = draggedNodeId.value
  const canMove = canDropOnFolder(node)
  finishNodeDrag()
  if (canMove) {
    emit('moveNode', sourceNodeId, node.key)
  }
}

function handleRootDragOver(event: DragEvent) {
  if (!showRootDropZone.value) {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  dropTargetFolderId.value = ''
  rootDropActive.value = true
  clearHoverExpand()
  updateAutoScroll(event.clientY)
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
}

function handleRootDragLeave(event: DragEvent) {
  if (eventLeftElement(event)) {
    rootDropActive.value = false
  }
}

function handleRootDrop(event: DragEvent) {
  if (!showRootDropZone.value || !draggedNodeId.value) {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  const sourceNodeId = draggedNodeId.value
  finishNodeDrag()
  emit('moveNode', sourceNodeId, '')
}

function handleTreeDragOver(event: DragEvent) {
  if (!draggedNodeId.value) {
    return
  }
  event.preventDefault()
  dropTargetFolderId.value = ''
  rootDropActive.value = false
  clearHoverExpand()
  updateAutoScroll(event.clientY)
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'none'
  }
}

function handleTreeDrop(event: DragEvent) {
  if (!draggedNodeId.value) {
    return
  }
  event.preventDefault()
  finishNodeDrag()
}

function handleTreeDragLeave(event: DragEvent) {
  if (!draggedNodeId.value || !eventLeftElement(event)) {
    return
  }
  dropTargetFolderId.value = ''
  rootDropActive.value = false
  clearHoverExpand()
  stopAutoScroll()
}

const checkedTreeNodes = computed(() => {
  const nodes: CollectionTreeNode[] = []
  const seenNodeIds = new Set<string>()

  for (const rawNodeId of props.checkedNodeIds) {
    const nodeId = rawNodeId.trim()
    if (!nodeId || nodeId === CREATE_NODE_KEY || seenNodeIds.has(nodeId)) {
      continue
    }
    const node = treeNodeMap.value.get(nodeId)
    if (!node || !toCollectionOption(node)) {
      continue
    }
    seenNodeIds.add(nodeId)
    nodes.push(node)
  }

  return nodes
})

const treeModelValue = computed(() => {
  return props.multiSelectEnabled ? checkedTreeNodes.value : selectedTreeNode.value
})

function handleTreeModelValueUpdate(value: CollectionTreeNode | CollectionTreeNode[] | undefined) {
  if (!props.multiSelectEnabled) {
    return
  }

  const nodeIds: string[] = []
  const seenNodeIds = new Set<string>()
  const nodes = Array.isArray(value) ? value : value ? [value] : []
  for (const node of nodes) {
    const collectionOption = toCollectionOption(node)
    const nodeId = collectionOption?.key.trim() ?? ''
    if (!nodeId || nodeId === CREATE_NODE_KEY || seenNodeIds.has(nodeId)) {
      continue
    }
    seenNodeIds.add(nodeId)
    nodeIds.push(nodeId)
  }
  emit('update:checkedNodeIds', nodeIds)
}

function handleNodeClick(node: CollectionTreeNode) {
  if (suppressNextNodeClick.value) {
    return
  }
  const collectionOption = toCollectionOption(node)
  if (!collectionOption) {
    return
  }
  if (collectionOption.type === APICollectionNodeType.APICollectionNodeTypeFolder) {
    emit('toggleFolder', collectionOption.key)
    return
  }
  emit('openRequest', collectionOption)
}

function handleContextMenu(event: MouseEvent, node: CollectionTreeNode) {
  const collectionOption = toCollectionOption(node)
  if (!collectionOption) {
    event.preventDefault()
    return
  }
  // Let the event bubble unprevented so the wrapping UContextMenu can open.
  emit('contextMenuNode', event, collectionOption)
}

function getNodeIconName(node: CollectionTreeNode, expanded: boolean): string {
  if (isCreateFolderNode(node)) {
    return 'i-lucide-folder'
  }
  if (isFolderNode(node)) {
    return expanded ? 'i-lucide-folder-open' : 'i-lucide-folder'
  }
  return node.type === APICollectionNodeType.APICollectionNodeTypeWebSocket
    ? 'i-lucide-radio'
    : 'i-lucide-file-text'
}

function getNodeMethod(node: CollectionTreeNode) {
  if (node.type === APICollectionNodeType.APICollectionNodeTypeHTTP) {
    const collectionOption = toCollectionOption(node)
    const method = collectionOption?.request?.http?.method?.trim().toUpperCase()
    return method || 'GET'
  }
  if (node.type === APICollectionNodeType.APICollectionNodeTypeWebSocket) {
    return 'WS'
  }
  return ''
}

function getChildCount(node: CollectionTreeNode) {
  if (!isFolderNode(node)) {
    return 0
  }
  return node.source?.children?.length ?? 0
}

function submitCreateFolder() {
  if ((props.creatingFolderName ?? '').trim()) {
    emit('createFolder')
  }
}

function handleCreateKeyup(event: KeyboardEvent) {
  if (event.key === 'Enter') {
    emit('createFolder')
  }
  if (event.key === 'Escape') {
    emit('cancelFolderCreate')
  }
}

function focusCreateFolderInput() {
  void nextTick(() => {
    if (props.creatingFolderParentId === '') {
      treeRef.value?.$el?.scrollTo({ top: 0 })
    }
    window.setTimeout(() => {
      createFolderInputRef.value?.inputRef?.focus({ preventScroll: true })
    }, 0)
  })
}

watch(
  () => props.creatingFolderParentId,
  (parentId) => {
    if (parentId !== null && parentId !== undefined) {
      focusCreateFolderInput()
    }
  },
  { flush: 'post' },
)

watch(draggingEnabled, (enabled) => {
  if (!enabled && draggedNodeId.value) {
    finishNodeDrag()
  }
})

onBeforeUnmount(() => {
  clearHoverExpand()
  stopAutoScroll()
  if (clickUnlockFrameId) {
    window.cancelAnimationFrame(clickUnlockFrameId)
    clickUnlockFrameId = 0
  }
})
</script>

<template>
  <div
    class="relative h-full min-h-0 w-full"
    @dragover="handleTreeDragOver"
    @dragleave="handleTreeDragLeave"
    @drop="handleTreeDrop"
  >
    <div
      v-if="showRootDropZone"
      class="absolute inset-x-2 top-1 z-20 flex h-8 items-center justify-center gap-1.5 rounded-md border border-dashed border-primary bg-default/95 px-2 text-xs font-medium text-primary shadow-sm backdrop-blur transition-colors"
      :class="rootDropActive && 'bg-primary/15 ring-2 ring-primary/25'"
      aria-hidden="true"
      @dragover="handleRootDragOver"
      @dragleave="handleRootDragLeave"
      @drop="handleRootDrop"
    >
      <UIcon name="i-lucide-folder-tree" class="size-4 shrink-0" aria-hidden="true" />
      <span>{{ t('api_collection.move_to_root') }}</span>
    </div>

    <UTree
      ref="treeRef"
      class="h-full min-h-0 w-full pt-0.5"
      :as="{ link: 'div' }"
      :items="treeOptions"
      :expanded="props.expandedFolderIds"
      :virtualize="{ estimateSize: 32, overscan: 12 }"
      :model-value="treeModelValue"
      :multiple="props.multiSelectEnabled"
      :propagate-select="props.multiSelectEnabled"
      :bubble-select="props.multiSelectEnabled"
      :get-key="getNodeKey"
      :ui="{ item: 'w-full', link: 'gap-1.5 p-0 text-left', linkLabel: 'min-w-0 flex-1' }"
      @update:model-value="handleTreeModelValueUpdate"
    >
      <template #item="{ item, expanded, selected, indeterminate, handleSelect }">
        <template v-if="(item as CollectionTreeNode).type === 'create-folder'">
          <div
            class="flex min-h-8 w-full min-w-0 items-center gap-1.5 rounded-md px-2.5 text-left text-sm"
            :data-key="(item as CollectionTreeNode).key"
            @click.stop
            @contextmenu.stop
            @keydown.stop
          >
            <UIcon
              :name="getNodeIconName(item as CollectionTreeNode, expanded)"
              class="size-5 shrink-0 text-app-text-muted"
              aria-hidden="true"
            />
            <UInput
              ref="createFolderInputRef"
              :model-value="creatingFolderName ?? ''"
              autofocus
              :placeholder="creatingFolderPlaceholder ?? ''"
              size="xs"
              class="min-w-0 flex-[1_1_auto]"
              @update:model-value="emit('update:creatingFolderName', $event)"
              @keydown.stop
              @keyup.stop="handleCreateKeyup"
            />
            <UButton
              icon="i-lucide-plus"
              color="primary"
              variant="ghost"
              size="xs"
              square
              class="flex-[0_0_auto]"
              :disabled="!(creatingFolderName ?? '').trim()"
              @keydown.stop
              @click.stop="submitCreateFolder"
            />
          </div>
        </template>
        <div
          v-else
          class="group flex min-h-8 w-full min-w-0 items-center gap-1.5 rounded-md px-2.5 text-left text-sm transition-colors"
          :data-key="(item as CollectionTreeNode).key"
          data-api-collection-node
          :class="[
            draggedNodeId === (item as CollectionTreeNode).key && 'opacity-50',
            dropTargetFolderId === (item as CollectionTreeNode).key &&
              'bg-primary/10 ring-1 ring-inset ring-primary',
          ]"
          @click.stop="handleNodeClick(item as CollectionTreeNode)"
          @contextmenu="handleContextMenu($event, item as CollectionTreeNode)"
          @dragover="handleNodeDragOver($event, item as CollectionTreeNode)"
          @dragleave="handleNodeDragLeave($event, item as CollectionTreeNode)"
          @drop="handleNodeDrop($event, item as CollectionTreeNode)"
        >
          <UCheckbox
            v-if="props.multiSelectEnabled"
            size="sm"
            class="shrink-0"
            :model-value="indeterminate ? 'indeterminate' : selected"
            :aria-label="t('api_collection.select_item', { name: String(item.label ?? '') })"
            @update:model-value="handleSelect()"
            @pointerdown.stop
            @pointerup.stop
            @click.stop
            @keydown.stop
            @keyup.stop
          />
          <span
            v-if="draggingEnabled"
            class="inline-flex size-3.5 shrink-0 cursor-grab items-center justify-center text-dimmed opacity-0 transition-opacity group-hover:opacity-100 active:cursor-grabbing"
            draggable="true"
            aria-hidden="true"
            @pointerdown.stop
            @click.stop
            @dragstart.stop="handleNodeDragStart($event, item as CollectionTreeNode)"
            @dragend.stop="finishNodeDrag"
          >
            <UIcon name="i-lucide-grip-vertical" class="size-3.5" aria-hidden="true" />
          </span>
          <UIcon
            :name="getNodeIconName(item as CollectionTreeNode, expanded)"
            class="size-5 shrink-0 text-app-text-muted"
            aria-hidden="true"
          />
          <UTooltip :text="String(item.label ?? '')" :content="{ side: 'right' }">
            <span class="inline-flex w-full min-w-0 items-center gap-1.5 text-left text-inherit">
              <span
                v-if="getNodeMethod(item as CollectionTreeNode)"
                class="-mr-0.5 flex-[0_0_auto] text-sm font-semibold leading-none"
                :style="{
                  color: methodColorMap[getNodeMethod(item as CollectionTreeNode)] ?? '#64748b',
                }"
              >
                {{ getNodeMethod(item as CollectionTreeNode) }}
              </span>
              <span class="min-w-0 truncate text-left text-sm leading-5">{{
                String(item.label ?? '')
              }}</span>
              <span
                v-if="isFolderNode(item as CollectionTreeNode)"
                class="inline-flex h-5 min-w-5 flex-[0_0_auto] items-center justify-center rounded-full bg-[color-mix(in_srgb,var(--app-control-bg)_92%,transparent)] px-1.5 text-sm text-app-text-muted"
              >
                {{ getChildCount(item as CollectionTreeNode) }}
              </span>
            </span>
          </UTooltip>
          <UIcon
            v-if="
              isFolderNode(item as CollectionTreeNode) &&
              getChildCount(item as CollectionTreeNode) > 0
            "
            name="i-lucide-chevron-down"
            class="ms-auto size-5 shrink-0 text-app-text-muted transition-transform duration-200"
            :class="expanded && 'rotate-180'"
            aria-hidden="true"
          />
        </div>
      </template>
    </UTree>
  </div>
</template>
