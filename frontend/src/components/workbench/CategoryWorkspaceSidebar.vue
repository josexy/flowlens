<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficEntry } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/models'
import TrafficContextMenu from '@/components/menu/TrafficContextMenu.vue'
import CategoryNavigationTree from '@/components/traffic-workspace/CategoryNavigationTree.vue'
import CategorySidebarSearch from '@/components/traffic-workspace/CategorySidebarSearch.vue'
import { useCategoryContextStore } from '@/stores/categoryContext'
import { useTrafficStore } from '@/stores/traffic'
import { useHistoryStore } from '@/stores/history'
import { useHistoryTrafficStore } from '@/stores/historyTraffic'
import { useFilterStore } from '@/stores/filter'
import { useHistoryFilterStore } from '@/stores/historyFilter'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import {
  buildCategoryFacetSummaries,
  buildStructureTree,
  filterHostSummary,
  filterProcessSummary,
  filterStructureRootsByHost,
} from '@/utils/traffic-category'
import { trafficMatchesCategoryFilters } from '@/utils/traffic'
import { firstHeaderFieldValue } from '@/utils/headers'
import type {
  CategoryNavigationHostNode,
  CategoryNavigationProcessNode,
  CategoryNavigationSectionConfig,
  StructureTreeNode,
} from '@/types/traffic-category'

const CATEGORY_CAPTURE_SNAPSHOT_MS = 250
const CATEGORY_HOST_NODE_KEY_PREFIX = 'category-host:'
const CATEGORY_PROCESS_NODE_KEY_PREFIX = 'category-process:'

const { t } = useI18n()
const categoryContextStore = useCategoryContextStore()
const trafficStore = useTrafficStore()
const historyStore = useHistoryStore()
const historyTrafficStore = useHistoryTrafficStore()
const filterStore = useFilterStore()
const historyFilterStore = useHistoryFilterStore()
const trafficWorkspaceStore = useTrafficWorkspaceStore()
const activeLeafKey = ref<string | null>(null)
const contextMenuRef = ref<InstanceType<typeof TrafficContextMenu> | null>(null)
const categoryEntries = shallowRef<TrafficEntry[]>([])
let categoryEntriesTimer: ReturnType<typeof setTimeout> | null = null
let activeCategoryContextKey = ''

interface CategoryEntriesSnapshot {
  contextKey: string
  kind: 'capture' | 'history'
  entries: TrafficEntry[]
}

function getEntryContentType(entry: TrafficEntry) {
  return firstHeaderFieldValue(entry.response?.headerFields, 'Content-Type') ?? ''
}

function hasSameCategoryStructure(current: TrafficEntry[], next: TrafficEntry[]) {
  if (current.length !== next.length) {
    return false
  }
  for (let index = 0; index < current.length; index++) {
    const currentEntry = current[index]!
    const nextEntry = next[index]!
    if (
      currentEntry.id !== nextEntry.id ||
      currentEntry.host !== nextEntry.host ||
      currentEntry.url !== nextEntry.url ||
      currentEntry.path !== nextEntry.path ||
      currentEntry.type !== nextEntry.type ||
      currentEntry.rawTcp?.hostPort !== nextEntry.rawTcp?.hostPort ||
      currentEntry.rawTcp?.tls !== nextEntry.rawTcp?.tls ||
      currentEntry.metadata?.process?.status !== nextEntry.metadata?.process?.status ||
      currentEntry.metadata?.process?.appId !== nextEntry.metadata?.process?.appId ||
      currentEntry.metadata?.process?.executablePath !==
        nextEntry.metadata?.process?.executablePath ||
      currentEntry.metadata?.process?.displayName !==
        nextEntry.metadata?.process?.displayName ||
      currentEntry.metadata?.process?.processName !==
        nextEntry.metadata?.process?.processName ||
      currentEntry.metadata?.process?.iconKey !== nextEntry.metadata?.process?.iconKey ||
      getEntryContentType(currentEntry) !== getEntryContentType(nextEntry)
    ) {
      return false
    }
  }
  return true
}

const dataset = computed(() => {
  const context = categoryContextStore.activeContext
  if (!context) return null

  if (context.kind === 'capture') {
    return {
      kind: 'capture' as const,
      label: t('category.source_capture'),
      contextLabel: context.label,
      entries: filterStore.baseFilteredEntries,
      rawEntries: trafficStore.entries,
      selectedHosts: filterStore.selectedHosts,
      selectedProcessKeys: filterStore.selectedProcessKeys,
      setSelectedHosts: filterStore.setSelectedHosts,
      toggleHost: filterStore.toggleHost,
      toggleProcess: filterStore.toggleProcess,
      focusEntryById: trafficStore.focusEntryById,
    }
  }

  return {
    kind: 'history' as const,
    historyKey: context.historyKey!,
    label: t('category.source_history', { label: context.label }),
    contextLabel: context.label,
    entries: historyFilterStore.baseFilteredEntries,
    rawEntries: historyTrafficStore.entries,
    selectedHosts: historyFilterStore.selectedHosts,
    selectedProcessKeys: historyFilterStore.selectedProcessKeys,
    setSelectedHosts: historyFilterStore.setSelectedHosts,
    toggleHost: historyFilterStore.toggleHost,
    toggleProcess: historyFilterStore.toggleProcess,
    focusEntryById: historyTrafficStore.focusEntryById,
  }
})

const categoryEntriesSource = computed<CategoryEntriesSnapshot | null>(() => {
  const currentDataset = dataset.value
  if (!currentDataset) {
    return null
  }
  return {
    contextKey:
      currentDataset.kind === 'capture'
        ? 'capture'
        : `history:${currentDataset.historyKey}`,
    kind: currentDataset.kind,
    entries: currentDataset.entries,
  }
})

let pendingCategoryEntries: CategoryEntriesSnapshot | null = null

function clearCategoryEntriesTimer() {
  if (categoryEntriesTimer === null) {
    return
  }
  clearTimeout(categoryEntriesTimer)
  categoryEntriesTimer = null
}

function applyCategoryEntries(snapshot: CategoryEntriesSnapshot) {
  activeCategoryContextKey = snapshot.contextKey
  categoryEntries.value = snapshot.entries
}

function flushPendingCategoryEntries() {
  categoryEntriesTimer = null
  const snapshot = pendingCategoryEntries
  pendingCategoryEntries = null
  if (!snapshot || categoryEntriesSource.value?.contextKey !== snapshot.contextKey) {
    return
  }
  applyCategoryEntries(snapshot)
}

watch(
  categoryEntriesSource,
  (snapshot) => {
    if (!snapshot) {
      clearCategoryEntriesTimer()
      pendingCategoryEntries = null
      activeCategoryContextKey = ''
      categoryEntries.value = []
      return
    }

    const applyImmediately =
      snapshot.kind === 'history' ||
      snapshot.contextKey !== activeCategoryContextKey ||
      categoryEntries.value.length === 0 ||
      snapshot.entries.length === 0
    if (applyImmediately) {
      clearCategoryEntriesTimer()
      pendingCategoryEntries = null
      applyCategoryEntries(snapshot)
      return
    }

    if (hasSameCategoryStructure(categoryEntries.value, snapshot.entries)) {
      clearCategoryEntriesTimer()
      pendingCategoryEntries = null
      return
    }

    pendingCategoryEntries = snapshot
    if (categoryEntriesTimer === null) {
      categoryEntriesTimer = setTimeout(
        flushPendingCategoryEntries,
        CATEGORY_CAPTURE_SNAPSHOT_MS,
      )
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  clearCategoryEntriesTimer()
  pendingCategoryEntries = null
})

watch(
  () => categoryContextStore.activeContext,
  (context) => {
    if (!context || context.kind !== 'history' || !context.historyKey) {
      return
    }
    if (historyStore.selectedKey === context.historyKey) {
      return
    }
    void historyStore.selectHistory(context.historyKey)
  },
  { immediate: true },
)

watch(
  () => categoryContextStore.activeContext,
  () => {
    activeLeafKey.value = null
  },
  { deep: true },
)

const selectedHostSet = computed(() => new Set(dataset.value?.selectedHosts ?? []))

const selectedProcessKeySet = computed(
  () => new Set(dataset.value?.selectedProcessKeys ?? []),
)

const facetSummaries = computed(() =>
  buildCategoryFacetSummaries(
    categoryEntries.value,
    selectedHostSet.value,
    selectedProcessKeySet.value,
    t('category.process_unidentified'),
  ),
)

const filteredHostSummary = computed(() =>
  filterHostSummary(facetSummaries.value.hosts, categoryContextStore.searchText),
)

const categoryHostNodes = computed<CategoryNavigationHostNode[]>(() =>
  filteredHostSummary.value.map((item) => ({
    nodeKind: 'host',
    key: `${CATEGORY_HOST_NODE_KEY_PREFIX}${item.host}`,
    label: item.host,
    host: item.host,
    count: item.count,
  })),
)

const filteredProcessSummary = computed(() =>
  filterProcessSummary(facetSummaries.value.processes, categoryContextStore.searchText),
)

const categoryProcessNodes = computed<CategoryNavigationProcessNode[]>(() =>
  filteredProcessSummary.value.map((item) => ({
    ...item,
    nodeKind: 'process',
    key: `${CATEGORY_PROCESS_NODE_KEY_PREFIX}${item.processKey}`,
  })),
)

const structureEntries = computed(() => {
  const hostSet = selectedHostSet.value
  const processKeySet = selectedProcessKeySet.value
  if (hostSet.size === 0 && processKeySet.size === 0) {
    return categoryEntries.value
  }
  return categoryEntries.value.filter((entry) =>
    trafficMatchesCategoryFilters(entry, hostSet, processKeySet),
  )
})

const structureTree = computed(() => {
  return buildStructureTree(structureEntries.value)
})

const filteredStructureRoots = computed(() => {
  return filterStructureRootsByHost(
    structureTree.value.roots,
    categoryContextStore.searchText,
  )
})

const categoryNavigationSections = computed<CategoryNavigationSectionConfig[]>(() => [
  {
    id: 'process',
    label: t('category.process'),
    children: categoryProcessNodes.value,
    collapsed: categoryContextStore.sectionCollapsed.process,
  },
  {
    id: 'host',
    label: t('category.host'),
    children: categoryHostNodes.value,
    collapsed: categoryContextStore.sectionCollapsed.host,
  },
  {
    id: 'structure',
    label: t('category.structure'),
    children: filteredStructureRoots.value,
    collapsed: categoryContextStore.sectionCollapsed.structure,
  },
])

const activeLeafNode = computed(() => {
  const key = activeLeafKey.value
  return key ? structureTree.value.nodeMap.get(key) : undefined
})

const hasContext = computed(() => dataset.value !== null)
const hasEntries = computed(() => (dataset.value?.rawEntries.length ?? 0) > 0)
const hasMatches = computed(() =>
  categoryNavigationSections.value.some((section) => section.children.length > 0),
)

function toggleHost(host: string) {
  dataset.value?.toggleHost(host)
}

function toggleProcess(processKey: string) {
  dataset.value?.toggleProcess(processKey)
}

function resolveEntryForLeaf(node: StructureTreeNode): TrafficEntry | null {
  const currentDataset = dataset.value
  if (!currentDataset || node.entryIds.length === 0) {
    return null
  }

  const entryIdSet = new Set(node.entryIds)
  return (
    currentDataset.entries.find((entry) => entryIdSet.has(entry.id)) ??
    currentDataset.rawEntries.find((entry) => entryIdSet.has(entry.id)) ??
    null
  )
}

async function selectLeaf(node: StructureTreeNode) {
  const currentDataset = dataset.value
  const context = categoryContextStore.activeContext
  if (!currentDataset || !context || node.entryIds.length === 0) {
    return
  }

  activeLeafKey.value = node.key

  if (currentDataset.kind === 'capture') {
    trafficWorkspaceStore.ensureCategoryTargetTab({ kind: 'capture' })
  } else {
    trafficWorkspaceStore.ensureCategoryTargetTab({
      kind: 'history',
      historyKey: currentDataset.historyKey,
      title: currentDataset.contextLabel,
    })
    if (historyStore.selectedKey !== currentDataset.historyKey) {
      await historyStore.selectHistory(currentDataset.historyKey)
    }
  }

  if (
    currentDataset.selectedHosts.length > 0 &&
    !currentDataset.selectedHosts.includes(node.host)
  ) {
    currentDataset.setSelectedHosts([...currentDataset.selectedHosts, node.host])
  }

  currentDataset.focusEntryById(node.entryIds[0]!)
}

async function showLeafContextMenu(event: MouseEvent, node: StructureTreeNode) {
  const currentDataset = dataset.value
  const context = categoryContextStore.activeContext
  if (!currentDataset || !context || node.entryIds.length === 0) {
    return
  }

  let entry = resolveEntryForLeaf(node)

  if (
    !entry &&
    currentDataset.kind === 'history' &&
    historyStore.selectedKey !== currentDataset.historyKey
  ) {
    await historyStore.selectHistory(currentDataset.historyKey)
    entry = resolveEntryForLeaf(node)
  }

  if (!entry) {
    return
  }

  activeLeafKey.value = node.key
  // The wrapping UContextMenu opens itself at the pointer; we only set the entry.
  void event
  contextMenuRef.value?.setEntries([entry])
}

const contextMenuStore = computed(() =>
  dataset.value?.kind === 'history' ? historyTrafficStore : trafficStore,
)
</script>

<template>
  <div class="flex h-full min-h-0 w-full flex-col overflow-hidden bg-app-sidebar">
    <div class="flex min-h-10 items-center bg-app-sidebar-header px-3 [border-bottom:1px_solid_var(--app-border-color)]">
      <div class="text-sm font-semibold text-app-text-muted">
        {{ dataset?.label }}
      </div>
    </div>

    <div
      v-if="!hasContext"
      class="flex flex-1 items-center justify-center p-4 text-center text-sm text-app-text-muted"
      role="status"
    >
      {{ t('category.unsupported') }}
    </div>
    <div
      v-else-if="!hasEntries"
      class="flex flex-1 items-center justify-center p-4 text-center text-sm text-app-text-muted"
      role="status"
    >
      {{ t('category.empty') }}
    </div>
    <div
      v-else-if="!hasMatches"
      class="flex flex-1 items-center justify-center p-4 text-center text-sm text-app-text-muted"
      role="status"
    >
      {{ t('category.empty_filtered') }}
    </div>
    <div v-else class="min-h-0 flex-1 overflow-hidden">
      <TrafficContextMenu
        ref="contextMenuRef"
        :traffic-store="contextMenuStore"
        class="h-full min-h-0 w-full"
      >
        <CategoryNavigationTree
          :navigation-label="t('category.title')"
          :sections="categoryNavigationSections"
          :selected-hosts="dataset?.selectedHosts ?? []"
          :selected-process-keys="dataset?.selectedProcessKeys ?? []"
          :active-leaf="activeLeafNode"
          :expanded-keys="categoryContextStore.expandedKeys"
          @toggle-host="toggleHost"
          @toggle-process="toggleProcess"
          @update-section-collapsed="categoryContextStore.setSectionCollapsed"
          @update-expanded-keys="categoryContextStore.setExpandedKeys"
          @select-leaf="selectLeaf"
          @contextmenu-leaf="showLeafContextMenu"
        />
      </TrafficContextMenu>
    </div>

    <CategorySidebarSearch v-model="categoryContextStore.searchText" />
  </div>
</template>
