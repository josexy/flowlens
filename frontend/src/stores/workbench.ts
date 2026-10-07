import { defineStore } from 'pinia'
import { ref } from 'vue'

export type WorkspaceSection =
  | 'capture'
  | 'category'
  | 'apiCollection'
  | 'rewriteRules'
  | 'pythonPlugins'
  | 'memstats'
export type ContentSurface = 'traffic' | 'rewriteRules' | 'pythonPlugins' | 'memstats'

type SectionSelections = {
  capture: string | null
  category: 'category:panel' | null
  apiCollection: 'api-collection:panel' | null
  rewriteRules: 'rewrite-rules:panel' | null
  pythonPlugins: 'python-plugins:panel' | null
  memstats: 'memstats:panel' | null
}

export const useWorkbenchStore = defineStore('workbench', () => {
  const activeSection = ref<WorkspaceSection>('capture')
  const secondarySidebarVisible = ref(true)
  const activeContent = ref<ContentSurface>('traffic')
  const sectionSelections = ref<SectionSelections>({
    capture: 'capture',
    category: 'category:panel',
    apiCollection: 'api-collection:panel',
    rewriteRules: 'rewrite-rules:panel',
    pythonPlugins: 'python-plugins:panel',
    memstats: 'memstats:panel',
  })

  function getSectionContent(section: WorkspaceSection): ContentSurface {
    if (section === 'rewriteRules') return 'rewriteRules'
    if (section === 'pythonPlugins') {
      return 'pythonPlugins'
    }
    if (section === 'memstats') {
      return 'memstats'
    }
    return 'traffic'
  }

  function activateSection(section: WorkspaceSection) {
    const nextContent = getSectionContent(section)

    if (activeSection.value === section) {
      if (activeContent.value !== nextContent) {
        activeContent.value = nextContent
        secondarySidebarVisible.value = true
        return
      }
      secondarySidebarVisible.value = !secondarySidebarVisible.value
      return
    }

    activeSection.value = section
    secondarySidebarVisible.value = true
    activeContent.value = nextContent
  }

  function hideSecondarySidebar() {
    secondarySidebarVisible.value = false
  }

  function showSecondarySidebar() {
    secondarySidebarVisible.value = true
  }

  function selectCaptureItem(itemKey: string) {
    activeSection.value = 'capture'
    secondarySidebarVisible.value = true
    sectionSelections.value.capture = itemKey
    activeContent.value = 'traffic'
  }

  function selectCategoryItem() {
    activeSection.value = 'category'
    secondarySidebarVisible.value = true
    sectionSelections.value.category = 'category:panel'
    activeContent.value = 'traffic'
  }

  function selectRewriteRulesItem() {
    activeSection.value = 'rewriteRules'
    secondarySidebarVisible.value = true
    sectionSelections.value.rewriteRules = 'rewrite-rules:panel'
    activeContent.value = 'rewriteRules'
  }

  function selectPythonPluginsItem() {
    activeSection.value = 'pythonPlugins'
    secondarySidebarVisible.value = true
    sectionSelections.value.pythonPlugins = 'python-plugins:panel'
    activeContent.value = 'pythonPlugins'
  }

  function selectApiCollectionItem() {
    activeSection.value = 'apiCollection'
    secondarySidebarVisible.value = true
    sectionSelections.value.apiCollection = 'api-collection:panel'
    activeContent.value = 'traffic'
  }

  function selectMemStatsItem() {
    activeSection.value = 'memstats'
    secondarySidebarVisible.value = true
    sectionSelections.value.memstats = 'memstats:panel'
    activeContent.value = 'memstats'
  }

  return {
    activeSection,
    secondarySidebarVisible,
    activeContent,
    sectionSelections,
    activateSection,
    hideSecondarySidebar,
    showSecondarySidebar,
    selectCaptureItem,
    selectCategoryItem,
    selectApiCollectionItem,
    selectRewriteRulesItem,
    selectPythonPluginsItem,
    selectMemStatsItem,
  }
})
