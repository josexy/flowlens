import type { InjectionKey } from 'vue'
import type { useTrafficStore } from '@/stores/traffic'
import type { useFilterStore } from '@/stores/filter'
import type { useHistoryTrafficStore } from '@/stores/historyTraffic'
import type { useHistoryFilterStore } from '@/stores/historyFilter'

export type TrafficStoreInjection =
  | ReturnType<typeof useTrafficStore>
  | ReturnType<typeof useHistoryTrafficStore>

export type FilterStoreInjection =
  | ReturnType<typeof useFilterStore>
  | ReturnType<typeof useHistoryFilterStore>

export const TRAFFIC_STORE_KEY: InjectionKey<TrafficStoreInjection> = Symbol('trafficStore')
export const FILTER_STORE_KEY: InjectionKey<FilterStoreInjection> = Symbol('filterStore')
