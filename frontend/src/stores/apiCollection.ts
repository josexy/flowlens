import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  CreateFolder,
  DeleteNodes,
  GetRequest,
  ListCollection,
  MoveNode,
  RenameNode,
  SaveHTTPRequest,
  SaveWebSocketRequest,
} from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/apicollectionservice'
import {
  APICollectionNodeType,
  type APICollectionFolder,
  type APICollectionRequest,
  type APICollectionTree,
  type APICollectionEntry as APICollectionEntryModel,
} from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'
import type {
  ApiCollectionEntry,
  ApiCollectionFolderOption,
  ApiCollectionFolderTreeOption,
  ApiCollectionTreeOption,
  ApiCollectionTreeRow,
} from '@/types/api-collection'
import { useTrafficWorkspaceStore } from './trafficWorkspace'

function createEmptyCollection() {
  return { schemaVersion: 2, folders: [] }
}

function isFolderEntry(
  entry: ApiCollectionEntry | null | undefined,
): entry is APICollectionFolder {
  return Boolean(entry && 'folders' in entry && 'requests' in entry)
}

function isRequestEntry(
  entry: ApiCollectionEntry | null | undefined,
): entry is APICollectionRequest {
  return Boolean(entry && 'type' in entry)
}

function getEntryType(entry: ApiCollectionEntry): APICollectionNodeType {
  return isFolderEntry(entry)
    ? APICollectionNodeType.APICollectionNodeTypeFolder
    : entry.type
}

function normalizeNodeName(name: string) {
  return name.trim().toLowerCase()
}

function compareCollectionEntries(left: ApiCollectionEntry, right: ApiCollectionEntry) {
  if (left.sortOrder !== right.sortOrder) {
    return left.sortOrder - right.sortOrder
  }
  const leftType = getEntryType(left)
  const rightType = getEntryType(right)
  if (leftType !== rightType) {
    if (leftType === APICollectionNodeType.APICollectionNodeTypeFolder) {
      return -1
    }
    if (rightType === APICollectionNodeType.APICollectionNodeTypeFolder) {
      return 1
    }
  }
  const nameCompare = normalizeNodeName(left.name).localeCompare(normalizeNodeName(right.name))
  if (nameCompare !== 0) {
    return nameCompare
  }
  return left.id.localeCompare(right.id)
}

function sortCollectionEntries<T extends ApiCollectionEntry>(entries: T[]) {
  return [...entries].sort(compareCollectionEntries)
}

function compactFolders(folders: (APICollectionFolder | null)[] | null | undefined) {
  return (folders ?? []).filter((folder): folder is APICollectionFolder => Boolean(folder))
}

function compactRequests(requests: (APICollectionRequest | null)[] | null | undefined) {
  return (requests ?? []).filter((request): request is APICollectionRequest => Boolean(request))
}

function toDisplayError(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

function extractRenamedEntry(entry: APICollectionEntryModel | null | undefined) {
  return entry?.folder ?? entry?.request ?? null
}

function collectRequestNodeIds(entry: ApiCollectionEntry | null | undefined) {
  const ids: string[] = []
  const visit = (node: ApiCollectionEntry | null | undefined) => {
    if (!node) {
      return
    }
    if (isRequestEntry(node)) {
      ids.push(node.id)
      return
    }
    for (const request of compactRequests(node.requests)) {
      ids.push(request.id)
    }
    for (const folder of compactFolders(node.folders)) {
      visit(folder)
    }
  }
  visit(entry)
  return ids
}

function normalizeNodeIds(nodeIds: string[]) {
  return [...new Set(nodeIds.map((nodeId) => nodeId.trim()).filter(Boolean))]
}

export const useApiCollectionStore = defineStore('apiCollection', () => {
  const collection = ref<APICollectionTree>(createEmptyCollection())
  const loading = ref(false)
  const mutating = ref(false)
  const error = ref('')
  const loaded = ref(false)
  const expandedFolderIds = ref<string[]>([])
  const selectedNodeId = ref<string | null>(null)
  const lastSelectedFolderId = ref('')
  const requestDetailRequests = new Map<string, Promise<APICollectionRequest>>()

  const entries = computed<ApiCollectionEntry[]>(() => {
    const flattened: ApiCollectionEntry[] = []

    const visitFolder = (folder: APICollectionFolder) => {
      flattened.push(folder)
      for (const childFolder of sortCollectionEntries(compactFolders(folder.folders))) {
        visitFolder(childFolder)
      }
      flattened.push(...sortCollectionEntries(compactRequests(folder.requests)))
    }

    for (const folder of sortCollectionEntries(compactFolders(collection.value.folders))) {
      visitFolder(folder)
    }

    return flattened
  })

  const nodes = entries

  const nodeMap = computed(() => {
    return new Map(entries.value.map((entry) => [entry.id, entry]))
  })

  const parentFolderIdMap = computed(() => {
    const parents = new Map<string, string>()

    const visitFolder = (folder: APICollectionFolder, parentId: string) => {
      parents.set(folder.id, parentId)
      for (const childFolder of compactFolders(folder.folders)) {
        visitFolder(childFolder, folder.id)
      }
      for (const request of compactRequests(folder.requests)) {
        parents.set(request.id, folder.id)
      }
    }

    for (const folder of compactFolders(collection.value.folders)) {
      visitFolder(folder, '')
    }

    return parents
  })

  const folderNodeIds = computed(() => {
    return new Set(entries.value.filter(isFolderEntry).map((entry) => entry.id))
  })

  const expandableFolderIds = computed(() => {
    return entries.value
      .filter(
        (entry): entry is APICollectionFolder =>
          isFolderEntry(entry) &&
          (compactFolders(entry.folders).length > 0 || compactRequests(entry.requests).length > 0),
      )
      .map((folder) => folder.id)
  })

  const hasExpandableFolders = computed(() => expandableFolderIds.value.length > 0)

  const allFoldersExpanded = computed(() => {
    if (!hasExpandableFolders.value) {
      return false
    }
    const expandedIds = new Set(expandedFolderIds.value)
    return expandableFolderIds.value.every((folderId) => expandedIds.has(folderId))
  })

  const selectedNode = computed<ApiCollectionEntry | null>(() => {
    return selectedNodeId.value ? nodeMap.value.get(selectedNodeId.value) ?? null : null
  })

  const selectedFolderNode = computed<APICollectionFolder | null>(() => {
    const currentSelectedNode = selectedNode.value
    if (isFolderEntry(currentSelectedNode)) {
      return currentSelectedNode
    }
    const parentId = currentSelectedNode ? parentFolderIdMap.value.get(currentSelectedNode.id) ?? '' : ''
    return parentId ? (nodeMap.value.get(parentId) as APICollectionFolder | undefined) ?? null : null
  })

  const treeRows = computed<ApiCollectionTreeRow[]>(() => {
    const rows: ApiCollectionTreeRow[] = []
    const expandedIds = new Set(expandedFolderIds.value)

    const visitFolder = (folder: APICollectionFolder, parentId: string, depth: number) => {
      const childFolders = sortCollectionEntries(compactFolders(folder.folders))
      const childRequests = sortCollectionEntries(compactRequests(folder.requests))
      const row: ApiCollectionTreeRow = {
        id: folder.id,
        parentId,
        type: APICollectionNodeType.APICollectionNodeTypeFolder,
        name: folder.name,
        depth,
        expanded: expandedIds.has(folder.id),
        hasChildren: childFolders.length > 0 || childRequests.length > 0,
        node: folder,
      }
      rows.push(row)
      if (!row.expanded) {
        return
      }
      for (const childFolder of childFolders) {
        visitFolder(childFolder, folder.id, depth + 1)
      }
      for (const request of childRequests) {
        rows.push({
          id: request.id,
          parentId: folder.id,
          type: request.type,
          name: request.name,
          depth: depth + 1,
          expanded: false,
          hasChildren: false,
          node: request,
        })
      }
    }

    for (const folder of sortCollectionEntries(compactFolders(collection.value.folders))) {
      visitFolder(folder, '', 0)
    }
    return rows
  })

  const folderOptions = computed<ApiCollectionFolderOption[]>(() => {
    const options: ApiCollectionFolderOption[] = []

    const visitFolder = (folder: APICollectionFolder, depth: number, pathPrefix: string) => {
      const label = pathPrefix ? `${pathPrefix} / ${folder.name}` : folder.name
      options.push({
        label,
        value: folder.id,
        depth,
      })
      for (const childFolder of sortCollectionEntries(compactFolders(folder.folders))) {
        visitFolder(childFolder, depth + 1, label)
      }
    }

    for (const folder of sortCollectionEntries(compactFolders(collection.value.folders))) {
      visitFolder(folder, 0, '')
    }
    return options
  })

  const folderTreeOptions = computed<ApiCollectionFolderTreeOption[]>(() => {
    const buildTree = (folders: APICollectionFolder[]): ApiCollectionFolderTreeOption[] => {
      return sortCollectionEntries(folders).map((folder) => ({
        label: folder.name,
        key: folder.id,
        value: folder.id,
        children: buildTree(compactFolders(folder.folders)),
      }))
    }

    return buildTree(compactFolders(collection.value.folders))
  })

  const collectionTreeOptions = computed<ApiCollectionTreeOption[]>(() => {
    const buildFolderOption = (folder: APICollectionFolder): ApiCollectionTreeOption => {
      const childFolders = sortCollectionEntries(compactFolders(folder.folders)).map(buildFolderOption)
      const childRequests = sortCollectionEntries(compactRequests(folder.requests)).map((request) => ({
        label: request.name,
        key: request.id,
        type: request.type,
        request,
        isLeaf: true,
      }))
      const children = [...childFolders, ...childRequests]
      return {
        label: folder.name,
        key: folder.id,
        type: APICollectionNodeType.APICollectionNodeTypeFolder,
        folder,
        isLeaf: children.length === 0,
        children,
      }
    }

    return sortCollectionEntries(compactFolders(collection.value.folders)).map(buildFolderOption)
  })

  function syncSelectionState() {
    const validNodeIds = new Set(entries.value.map((entry) => entry.id))
    const validFolderIds = folderNodeIds.value

    expandedFolderIds.value = expandedFolderIds.value.filter((id) => validFolderIds.has(id))

    if (selectedNodeId.value && !validNodeIds.has(selectedNodeId.value)) {
      selectedNodeId.value = null
    }
    if (lastSelectedFolderId.value && !validFolderIds.has(lastSelectedFolderId.value)) {
      lastSelectedFolderId.value = ''
    }
  }

  function setCollection(nextCollection: APICollectionTree | null | undefined) {
    collection.value = nextCollection ?? createEmptyCollection()
    syncSelectionState()
  }

  function collectAncestorFolderIds(nodeId: string) {
    const ancestors: string[] = []
    const visited = new Set<string>()
    let parentId = parentFolderIdMap.value.get(nodeId) ?? ''

    while (parentId) {
      if (visited.has(parentId)) {
        break
      }
      visited.add(parentId)
      ancestors.push(parentId)
      parentId = parentFolderIdMap.value.get(parentId) ?? ''
    }

    return ancestors
  }

  function ensureAncestorsExpanded(nodeId: string) {
    const expanded = new Set(expandedFolderIds.value)
    for (const ancestorId of collectAncestorFolderIds(nodeId)) {
      expanded.add(ancestorId)
    }
    expandedFolderIds.value = [...expanded]
  }

  function selectNode(nodeId: string | null) {
    selectedNodeId.value = nodeId
    if (!nodeId) {
      return
    }
    const node = nodeMap.value.get(nodeId)
    if (!node) {
      return
    }
    ensureAncestorsExpanded(node.id)
    if (isFolderEntry(node)) {
      lastSelectedFolderId.value = node.id
      return
    }
    const parentFolderId = parentFolderIdMap.value.get(node.id) ?? ''
    if (parentFolderId && folderNodeIds.value.has(parentFolderId)) {
      lastSelectedFolderId.value = parentFolderId
    }
  }

  function toggleExpandedFolder(folderId: string) {
    if (!folderNodeIds.value.has(folderId)) {
      return
    }
    if (expandedFolderIds.value.includes(folderId)) {
      expandedFolderIds.value = expandedFolderIds.value.filter((id) => id !== folderId)
      return
    }
    expandedFolderIds.value = [...expandedFolderIds.value, folderId]
  }

  function toggleAllFoldersExpanded() {
    expandedFolderIds.value = allFoldersExpanded.value ? [] : [...expandableFolderIds.value]
  }

  function reloadCollectionFallback() {
    void loadCollection().catch(() => {})
  }

  function findFolderById(folderId: string) {
    return entries.value.find((entry) => isFolderEntry(entry) && entry.id === folderId) as
      | APICollectionFolder
      | undefined
  }

  function upsertNode(node: ApiCollectionEntry | null | undefined, parentFolderId = '') {
    if (!node) {
      return
    }
    const currentParentId = parentFolderIdMap.value.get(node.id) ?? ''
    removeNodeLocally(node.id)

    if (isFolderEntry(node)) {
      const parentId = currentParentId
      const parentFolder = parentId ? findFolderById(parentId) : null
      if (parentFolder) {
        parentFolder.folders = sortCollectionEntries([...compactFolders(parentFolder.folders), node])
      } else {
        collection.value.folders = sortCollectionEntries([...compactFolders(collection.value.folders), node])
      }
    } else if (isRequestEntry(node)) {
      const requestParentFolderId = parentFolderId || currentParentId || lastSelectedFolderId.value
      const parentFolder = requestParentFolderId ? findFolderById(requestParentFolderId) : null
      if (parentFolder) {
        parentFolder.requests = sortCollectionEntries([...compactRequests(parentFolder.requests), node])
      } else {
        reloadCollectionFallback()
      }
    }

    syncSelectionState()
    ensureAncestorsExpanded(node.id)
  }

  function removeNodesLocally(nodeIds: string[]) {
    const normalizedNodeIds = normalizeNodeIds(nodeIds)
    if (normalizedNodeIds.length === 0) {
      return
    }

    const nodeIdSet = new Set(normalizedNodeIds)
    const removeFromFolders = (folders: APICollectionFolder[]): APICollectionFolder[] => {
      const remainingFolders: APICollectionFolder[] = []

      for (const folder of folders) {
        if (nodeIdSet.has(folder.id)) {
          continue
        }
        folder.folders = removeFromFolders(compactFolders(folder.folders))
        folder.requests = compactRequests(folder.requests).filter(
          (request) => !nodeIdSet.has(request.id),
        )
        remainingFolders.push(folder)
      }

      return remainingFolders
    }

    collection.value.folders = removeFromFolders(compactFolders(collection.value.folders))
    syncSelectionState()
  }

  function removeNodeLocally(nodeId: string) {
    removeNodesLocally([nodeId])
  }

  async function loadCollection() {
    loading.value = true
    error.value = ''
    try {
      setCollection(await ListCollection())
      loaded.value = true
    } catch (loadError) {
      error.value = toDisplayError(loadError)
      throw loadError
    } finally {
      loading.value = false
    }
  }

  async function ensureCollectionLoaded() {
    if (loaded.value || loading.value) {
      return
    }
    await loadCollection()
  }

  async function getRequestNode(nodeId: string) {
    const normalizedNodeId = nodeId.trim()
    const summary = nodeMap.value.get(normalizedNodeId)
    if (!normalizedNodeId || !isRequestEntry(summary)) {
      throw new Error('request node not found')
    }

    const pendingRequest = requestDetailRequests.get(normalizedNodeId)
    if (pendingRequest) {
      return pendingRequest
    }

    const request = GetRequest(normalizedNodeId).then((result) => {
      if (!result || result.id !== normalizedNodeId) {
        throw new Error('request details not found')
      }
      return result
    })
    requestDetailRequests.set(normalizedNodeId, request)
    try {
      return await request
    } finally {
      if (requestDetailRequests.get(normalizedNodeId) === request) {
        requestDetailRequests.delete(normalizedNodeId)
      }
    }
  }

  async function createFolder(parentId: string, name: string) {
    mutating.value = true
    try {
      const folder = await CreateFolder(parentId, name)
      if (!folder) {
        throw new Error('create folder returned no folder')
      }
      await loadCollection()
      selectNode(folder.id)
      return folder
    } finally {
      mutating.value = false
    }
  }

  async function moveCollectionNode(nodeId: string, newParentId: string) {
    const normalizedNodeId = nodeId.trim()
    if (!normalizedNodeId) {
      throw new Error('API collection node id is required')
    }

    mutating.value = true
    try {
      await MoveNode(normalizedNodeId, newParentId.trim())
      await loadCollection()
      selectNode(normalizedNodeId)
    } finally {
      mutating.value = false
    }
  }

  async function deleteNodes(nodeIds: string[]) {
    const normalizedNodeIds = normalizeNodeIds(nodeIds)
    if (normalizedNodeIds.length === 0) {
      throw new Error('at least one API collection node is required')
    }

    mutating.value = true
    try {
      const deletedRequestIds = [
        ...new Set(
          normalizedNodeIds.flatMap((nodeId) =>
            collectRequestNodeIds(nodeMap.value.get(nodeId)),
          ),
        ),
      ]
      await DeleteNodes(normalizedNodeIds)
      removeNodesLocally(normalizedNodeIds)
      useTrafficWorkspaceStore().closeApiTabsByAPIIds(deletedRequestIds)
    } finally {
      mutating.value = false
    }
  }

  async function deleteCollectionNode(nodeId: string) {
    await deleteNodes([nodeId])
  }

  async function renameCollectionNode(nodeId: string, name: string) {
    mutating.value = true
    try {
      const renamedEntry = extractRenamedEntry(await RenameNode(nodeId, name))
      if (!renamedEntry) {
        throw new Error('rename node returned no entry')
      }
      await loadCollection()
      selectNode(renamedEntry.id)
      return renamedEntry
    } finally {
      mutating.value = false
    }
  }

  async function duplicateRequestNode(nodeId: string) {
    mutating.value = true
    try {
      const summary = nodeMap.value.get(nodeId)
      if (!isRequestEntry(summary)) {
        throw new Error('request node not found')
      }

      const parentFolderId = parentFolderIdMap.value.get(summary.id) ?? ''
      if (!parentFolderId) {
        throw new Error('request parent folder not found')
      }

      const node = await getRequestNode(summary.id)

      let copiedRequest: APICollectionRequest | null = null
      if (node.type === APICollectionNodeType.APICollectionNodeTypeHTTP) {
        if (!node.http) {
          throw new Error('http request not found')
        }
        copiedRequest = await SaveHTTPRequest(parentFolderId, node.name, node.http)
      } else if (node.type === APICollectionNodeType.APICollectionNodeTypeWebSocket) {
        if (!node.websocket) {
          throw new Error('websocket request not found')
        }
        copiedRequest = await SaveWebSocketRequest(parentFolderId, node.name, node.websocket)
      } else {
        throw new Error('unsupported request type')
      }

      if (!copiedRequest) {
        throw new Error('copy request returned no request')
      }

      await loadCollection()
      selectNode(copiedRequest.id)
      return copiedRequest
    } finally {
      mutating.value = false
    }
  }

  return {
    collection,
    nodes,
    entries,
    loading,
    mutating,
    error,
    loaded,
    expandedFolderIds,
    selectedNodeId,
    lastSelectedFolderId,
    nodeMap,
    parentFolderIdMap,
    folderNodeIds,
    hasExpandableFolders,
    allFoldersExpanded,
    selectedNode,
    selectedFolderNode,
    treeRows,
    folderOptions,
    folderTreeOptions,
    collectionTreeOptions,
    loadCollection,
    ensureCollectionLoaded,
    getRequestNode,
    createFolder,
    moveNode: moveCollectionNode,
    deleteNode: deleteCollectionNode,
    deleteNodes,
    renameNode: renameCollectionNode,
    duplicateRequestNode,
    selectNode,
    toggleExpandedFolder,
    toggleAllFoldersExpanded,
    upsertNode,
    removeNodeLocally,
    removeNodesLocally,
  }
})
