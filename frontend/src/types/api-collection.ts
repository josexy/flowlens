import type {
  APICollectionFolder,
  APICollectionNodeType,
  APICollectionRequest,
} from '#bindings/github.com/josexy/flowlens/backend/services/api_collection_service/models'

export type ApiCollectionEntry = APICollectionFolder | APICollectionRequest

export interface ApiCollectionTreeRow {
  id: string
  parentId: string
  type: APICollectionNodeType
  name: string
  depth: number
  expanded: boolean
  hasChildren: boolean
  node: ApiCollectionEntry
}

export interface ApiCollectionFolderOption {
  label: string
  value: string
  depth: number
}

export interface ApiCollectionFolderTreeOption {
  label: string
  key: string
  value: string
  children?: ApiCollectionFolderTreeOption[]
}

export interface ApiCollectionTreeOption {
  label: string
  key: string
  type: APICollectionNodeType
  folder?: APICollectionFolder
  request?: APICollectionRequest
  isLeaf?: boolean
  children?: ApiCollectionTreeOption[]
}

export interface SaveApiRequestForm {
  parentId: string
  name: string
}
