<script setup lang="ts">
import { computed } from 'vue'
import type { CSSProperties } from 'vue'
import type { TreeItemSelectEvent, TreeItemToggleEvent } from 'reka-ui'
import AppProcessIcon from '@/components/common/AppProcessIcon.vue'
import type {
  CategoryNavigationContentNode,
  CategoryNavigationHostNode,
  CategoryNavigationProcessNode,
  CategoryNavigationSectionConfig,
  CategorySectionId,
  StructureTreeNode,
} from '@/types/traffic-category'

const SECTION_NODE_KEY_PREFIX = 'category-section:'
const STRUCTURE_DEPTH_INDENT = 26
const ROW_INLINE_PADDING = 10

type CategoryNavigationNode = CategorySectionNode | CategoryNavigationContentNode

interface CategorySectionNode {
  nodeKind: 'section'
  sectionId: CategorySectionId
  key: string
  label: string
  children: CategoryNavigationContentNode[]
  collapsed: boolean
  hasPreviousSection: boolean
}

const props = defineProps<{
  navigationLabel: string
  sections: CategoryNavigationSectionConfig[]
  selectedHosts: string[]
  selectedProcessKeys: string[]
  activeLeaf?: StructureTreeNode | null
  expandedKeys?: string[]
}>()

const emit = defineEmits<{
  (event: 'toggle-host', host: string): void
  (event: 'toggle-process', processKey: string): void
  (event: 'update-section-collapsed', section: CategorySectionId, collapsed: boolean): void
  (event: 'update-expanded-keys', keys: string[]): void
  (event: 'select-leaf', node: StructureTreeNode): void
  (event: 'contextmenu-leaf', mouseEvent: MouseEvent, node: StructureTreeNode): void
}>()

const selectedHostSet = computed(() => new Set(props.selectedHosts))
const selectedProcessKeySet = computed(() => new Set(props.selectedProcessKeys))

const navigationItems = computed<CategorySectionNode[]>(() =>
  props.sections.map((section, index) => ({
    nodeKind: 'section',
    sectionId: section.id,
    key: `${SECTION_NODE_KEY_PREFIX}${section.id}`,
    label: section.label,
    children: section.children,
    collapsed: section.collapsed,
    hasPreviousSection: index > 0,
  })),
)

const hostNodes = computed<CategoryNavigationHostNode[]>(() =>
  navigationItems.value.flatMap((section) =>
    section.children.filter(
      (node): node is CategoryNavigationHostNode => isHostNode(node),
    ),
  ),
)

const processNodes = computed<CategoryNavigationProcessNode[]>(() =>
  navigationItems.value.flatMap((section) =>
    section.children.filter(
      (node): node is CategoryNavigationProcessNode => isProcessNode(node),
    ),
  ),
)

const navigationExpandedKeys = computed(() => [
  ...navigationItems.value.filter((section) => !section.collapsed).map((section) => section.key),
  ...(props.expandedKeys ?? []),
])

const selectedNavigationItems = computed<CategoryNavigationNode[]>(() => [
  ...hostNodes.value.filter((node) => selectedHostSet.value.has(node.host)),
  ...processNodes.value.filter((node) =>
    selectedProcessKeySet.value.has(node.processKey),
  ),
  ...(props.activeLeaf ? [props.activeLeaf] : []),
])

function isSectionNode(node: CategoryNavigationNode): node is CategorySectionNode {
  return 'nodeKind' in node && node.nodeKind === 'section'
}

function isHostNode(node: CategoryNavigationNode): node is CategoryNavigationHostNode {
  return 'nodeKind' in node && node.nodeKind === 'host'
}

function isProcessNode(
  node: CategoryNavigationNode,
): node is CategoryNavigationProcessNode {
  return 'nodeKind' in node && node.nodeKind === 'process'
}

function isStructureNode(node: CategoryNavigationNode): node is StructureTreeNode {
  return !('nodeKind' in node)
}

function getNodeKey(node: CategoryNavigationNode) {
  return node.key
}

function getNodeChildren(node: CategoryNavigationNode): CategoryNavigationNode[] | undefined {
  if (isSectionNode(node)) {
    return node.children
  }
  if (isHostNode(node) || isProcessNode(node) || node.children.length === 0) {
    return undefined
  }
  return node.children
}

function getLeafIconName(node: StructureTreeNode): string {
  if (node.trafficKind === 'raw-tcp') {
    return 'i-lucide-plug-zap'
  }

  const contentType = (node.contentType ?? '').toLowerCase().split(';')[0]?.trim() ?? ''
  if (contentType.startsWith('image/')) {
    return 'i-lucide-image'
  }
  if (
    contentType.includes('javascript') ||
    contentType.includes('typescript') ||
    contentType.includes('css') ||
    contentType.startsWith('text/') ||
    contentType.includes('json') ||
    contentType.includes('xml') ||
    contentType.includes('yaml') ||
    contentType.includes('toml')
  ) {
    return 'i-lucide-code'
  }

  return 'i-lucide-file'
}

function getStructureIconName(node: StructureTreeNode, expanded: boolean): string {
  if (node.type === 'host') {
    return 'i-lucide-globe'
  }
  if (node.type === 'segment') {
    if (node.trafficKind === 'raw-tcp') {
      return 'i-lucide-network'
    }
    return expanded ? 'i-lucide-folder-open' : 'i-lucide-folder'
  }
  return getLeafIconName(node)
}

function getNodeRowStyle(node: CategoryNavigationNode): CSSProperties | undefined {
  if (isSectionNode(node)) {
    return undefined
  }

  const depth = isStructureNode(node) ? node.depth : 0
  return {
    paddingInlineStart: `${ROW_INLINE_PADDING + depth * STRUCTURE_DEPTH_INDENT}px`,
  }
}

function toggleSection(section: CategorySectionNode) {
  emit('update-section-collapsed', section.sectionId, !section.collapsed)
}

function toggleStructureKey(key: string) {
  const expandedKeys = props.expandedKeys ?? []
  if (expandedKeys.includes(key)) {
    emit(
      'update-expanded-keys',
      expandedKeys.filter((item) => item !== key),
    )
    return
  }
  emit('update-expanded-keys', [...expandedKeys, key])
}

function handleNodeSelect(
  event: TreeItemSelectEvent<CategoryNavigationNode>,
  node: CategoryNavigationNode,
) {
  event.preventDefault()

  if (isSectionNode(node)) {
    if (event.detail.originalEvent instanceof KeyboardEvent) {
      toggleSection(node)
    }
    return
  }

  if (isHostNode(node)) {
    emit('toggle-host', node.host)
    return
  }

  if (isProcessNode(node)) {
    emit('toggle-process', node.processKey)
    return
  }

  if (node.type === 'leaf') {
    emit('select-leaf', node)
    return
  }

  if (event.detail.originalEvent instanceof KeyboardEvent) {
    toggleStructureKey(node.key)
  }
}

function handleNodeToggle(
  event: TreeItemToggleEvent<CategoryNavigationNode>,
  node: CategoryNavigationNode,
) {
  if (isSectionNode(node)) {
    return
  }
  if (isHostNode(node) || isProcessNode(node) || node.children.length === 0) {
    event.preventDefault()
  }
}

function hasSameKeys(left: string[], right: string[]) {
  if (left.length !== right.length) {
    return false
  }
  const rightSet = new Set(right)
  return left.every((key) => rightSet.has(key))
}

function handleExpandedKeysUpdate(keys: string[]) {
  const expandedKeySet = new Set(keys)
  const sectionKeySet = new Set<string>()
  for (const section of navigationItems.value) {
    sectionKeySet.add(section.key)
    const collapsed = !expandedKeySet.has(section.key)
    if (collapsed !== section.collapsed) {
      emit('update-section-collapsed', section.sectionId, collapsed)
    }
  }

  const nextStructureKeys = keys.filter((key) => !sectionKeySet.has(key))
  if (!hasSameKeys(nextStructureKeys, props.expandedKeys ?? [])) {
    emit('update-expanded-keys', nextStructureKeys)
  }
}

function canOpenLeafContextMenu(node: CategoryNavigationNode): node is StructureTreeNode {
  return isStructureNode(node) && node.type === 'leaf' && node.entryIds.length > 0
}

function findStructureNode(key: string): StructureTreeNode | null {
  const pending = props.sections.flatMap((section) => section.children)
  while (pending.length > 0) {
    const node = pending.pop()!
    if (!isStructureNode(node)) {
      continue
    }
    if (node.key === key) {
      return node
    }
    pending.push(...node.children)
  }
  return null
}

function handleViewportContextMenu(event: MouseEvent) {
  const leafRowInPath = event
    .composedPath()
    .find(
      (target) =>
        target instanceof HTMLElement && target.dataset.categoryContextMenu === 'traffic',
    )

  if (leafRowInPath) {
    return
  }

  const target = event.target
  const keyboardLeafRow =
    target instanceof HTMLElement && target.matches('[role="treeitem"]')
      ? target.querySelector<HTMLElement>('[data-category-context-menu="traffic"]')
      : null
  const node = keyboardLeafRow?.dataset.key
    ? findStructureNode(keyboardLeafRow.dataset.key)
    : null

  if (!node || !canOpenLeafContextMenu(node)) {
    event.preventDefault()
    return
  }

  emit('contextmenu-leaf', event, node)
}

function handleContextMenu(event: MouseEvent, node: CategoryNavigationNode) {
  if (!canOpenLeafContextMenu(node)) {
    event.preventDefault()
    return
  }

  emit('contextmenu-leaf', event, node)
}

function stopHandledActionKey(event: KeyboardEvent) {
  event.stopImmediatePropagation()
}

function stopContextMenuKeyFromTypeahead(event: KeyboardEvent) {
  // Reka's virtual tree treats the standalone Shift from Shift+F10 as typeahead input.
  if (
    event.key === 'Shift' ||
    event.key === 'ContextMenu' ||
    (event.shiftKey && event.key === 'F10')
  ) {
    event.stopImmediatePropagation()
  }
}
</script>

<template>
  <UTree
    class="h-full min-h-0 w-full overflow-x-hidden"
    :aria-label="navigationLabel"
    :items="navigationItems"
    :expanded="navigationExpandedKeys"
    :virtualize="{ estimateSize: 32, overscan: 12 }"
    :model-value="selectedNavigationItems"
    :get-key="getNodeKey"
    :get-children="getNodeChildren"
    :on-select="handleNodeSelect"
    :on-toggle="handleNodeToggle"
    :ui="{ item: 'w-full', link: 'gap-0 p-0! text-left', linkLabel: 'min-w-0 flex-1' }"
    multiple
    @update:expanded="handleExpandedKeysUpdate"
    @keydown.capture="stopContextMenuKeyFromTypeahead"
    @keydown.enter.space="stopHandledActionKey"
    @contextmenu.capture="handleViewportContextMenu"
  >
    <template #item="{ item, expanded }">
      <div
        class="flex h-8 w-full min-w-0 items-center gap-1.5 text-left text-sm"
        :class="
          isSectionNode(item as CategoryNavigationNode)
            ? [
                'bg-[color-mix(in_srgb,var(--app-sidebar-header-bg)_88%,var(--app-sidebar-bg))] px-3 font-semibold text-app-text-muted [border-bottom:1px_solid_var(--app-border-color)] hover:bg-[rgba(128,128,128,0.06)]',
                (item as CategorySectionNode).hasPreviousSection &&
                  '[border-top:1px_solid_var(--app-border-color)]',
              ]
            : 'rounded-md pe-2.5'
        "
        :style="getNodeRowStyle(item as CategoryNavigationNode)"
        :data-key="(item as CategoryNavigationNode).key"
        :data-category-context-menu="
          canOpenLeafContextMenu(item as CategoryNavigationNode) ? 'traffic' : undefined
        "
        :data-node-kind="
          isSectionNode(item as CategoryNavigationNode)
            ? `section-${(item as CategorySectionNode).sectionId}`
            : isHostNode(item as CategoryNavigationNode)
              ? 'host'
              : isProcessNode(item as CategoryNavigationNode)
                ? 'process'
                : `structure-${(item as StructureTreeNode).type}`
        "
        @contextmenu="handleContextMenu($event, item as CategoryNavigationNode)"
      >
        <template v-if="isSectionNode(item as CategoryNavigationNode)">
          <span class="min-w-0 flex-1 truncate">
            {{ (item as CategorySectionNode).label }}
          </span>
          <UIcon
            :name="expanded ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
            class="size-3 shrink-0"
            aria-hidden="true"
          />
        </template>

        <template v-else-if="isHostNode(item as CategoryNavigationNode)">
          <UIcon
            name="i-lucide-globe"
            class="size-5 shrink-0 text-app-text-muted"
            aria-hidden="true"
          />
          <span class="block min-w-0 flex-1 truncate text-left text-sm leading-5">
            {{ (item as CategoryNavigationHostNode).label }}
          </span>
          <UIcon
            v-if="selectedHostSet.has((item as CategoryNavigationHostNode).host)"
            name="i-lucide-check"
            class="size-5 shrink-0 text-app-accent"
            aria-hidden="true"
          />
        </template>

        <template v-else-if="isProcessNode(item as CategoryNavigationNode)">
          <AppProcessIcon
            :icon-key="(item as CategoryNavigationProcessNode).iconKey"
            alt=""
            :size="20"
          />
          <span class="block min-w-0 flex-1 truncate text-left text-sm leading-5">
            {{ (item as CategoryNavigationProcessNode).label }}
          </span>
          <UIcon
            v-if="
              selectedProcessKeySet.has(
                (item as CategoryNavigationProcessNode).processKey,
              )
            "
            name="i-lucide-check"
            class="size-5 shrink-0 text-app-accent"
            aria-hidden="true"
          />
        </template>

        <template v-else>
          <UIcon
            :name="getStructureIconName(item as StructureTreeNode, expanded)"
            class="size-5 shrink-0 text-app-text-muted"
            aria-hidden="true"
          />
          <span class="block min-w-0 flex-1 truncate text-left text-sm leading-5">
            {{ (item as StructureTreeNode).label }}
          </span>
          <UIcon
            v-if="(item as StructureTreeNode).children.length"
            name="i-lucide-chevron-down"
            class="ms-auto size-5 shrink-0 text-app-text-muted transition-transform duration-200"
            :class="expanded && 'rotate-180'"
            aria-hidden="true"
          />
        </template>
      </div>
    </template>
  </UTree>
</template>
