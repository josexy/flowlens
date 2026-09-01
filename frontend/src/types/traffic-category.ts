export type CategoryContextKind = 'capture' | 'history'

export interface CategoryContextSnapshot {
  kind: CategoryContextKind
  historyKey?: string
  label: string
}

export interface HostSummaryItem {
  host: string
  count: number
}

export interface ProcessSummaryItem {
  kind: 'resolved' | 'unavailable'
  processKey: string
  label: string
  displayName: string
  processName: string
  iconKey: string
  count: number
}

export type CategorySectionId = 'host' | 'process' | 'structure'

export type StructureNodeType = 'host' | 'segment' | 'leaf'

export interface StructureTreeNode {
  key: string
  label: string
  type: StructureNodeType
  host: string
  depth: number
  entryIds: number[]
  contentType?: string
  trafficKind?: 'http' | 'raw-tcp'
  children: StructureTreeNode[]
}

export interface CategoryNavigationHostNode {
  nodeKind: 'host'
  key: string
  label: string
  host: string
  count: number
}

export interface CategoryNavigationProcessNode extends ProcessSummaryItem {
  nodeKind: 'process'
  key: string
}

export type CategoryNavigationContentNode =
  | CategoryNavigationHostNode
  | CategoryNavigationProcessNode
  | StructureTreeNode

export interface CategoryNavigationSectionConfig {
  id: CategorySectionId
  label: string
  children: CategoryNavigationContentNode[]
  collapsed: boolean
}

export interface StructureTreeBuildResult {
  roots: StructureTreeNode[]
  nodeMap: Map<string, StructureTreeNode>
}
