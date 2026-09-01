import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type { CategoryContextSnapshot, CategorySectionId } from '@/types/traffic-category'

export const useCategoryContextStore = defineStore('categoryContext', () => {
  const activeContext = ref<CategoryContextSnapshot | null>(null)
  const searchText = ref('')
  const expandedKeys = ref<string[]>([])
  const sectionCollapsed = ref<Record<CategorySectionId, boolean>>({
    host: false,
    process: false,
    structure: false,
  })

  const isEmpty = computed(() => activeContext.value === null)

  function setActiveCaptureContext() {
    activeContext.value = {
      kind: 'capture',
      label: 'capture',
    }
  }

  function setActiveHistoryContext(historyKey: string, label: string) {
    activeContext.value = {
      kind: 'history',
      historyKey,
      label,
    }
  }

  function clearActiveContext() {
    activeContext.value = null
  }

  function clearSearch() {
    searchText.value = ''
  }

  function toggleExpandedKey(key: string) {
    if (expandedKeys.value.includes(key)) {
      expandedKeys.value = expandedKeys.value.filter((item) => item !== key)
      return
    }
    expandedKeys.value = [...expandedKeys.value, key]
  }

  function setExpandedKeys(keys: string[]) {
    expandedKeys.value = [...keys]
  }

  function resetExpandedKeys() {
    expandedKeys.value = []
  }

  function setSectionCollapsed(section: CategorySectionId, collapsed: boolean) {
    sectionCollapsed.value[section] = collapsed
  }

  function toggleSectionCollapsed(section: CategorySectionId) {
    setSectionCollapsed(section, !sectionCollapsed.value[section])
  }

  return {
    activeContext,
    searchText,
    expandedKeys,
    sectionCollapsed,
    isEmpty,
    setActiveCaptureContext,
    setActiveHistoryContext,
    clearActiveContext,
    clearSearch,
    toggleExpandedKey,
    setExpandedKeys,
    resetExpandedKeys,
    setSectionCollapsed,
    toggleSectionCollapsed,
  }
})
