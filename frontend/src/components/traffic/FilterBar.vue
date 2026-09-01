<script setup lang="ts">
import { computed, inject, onBeforeUnmount, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TabsItem } from '@nuxt/ui'
import { FILTER_STORE_KEY } from '@/types/inject-keys'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { registerShortcutHandler } from '@/shortcuts'

const props = defineProps<{
  context: 'capture' | 'history'
}>()

const { t } = useI18n()
const filterStore = inject(FILTER_STORE_KEY)!
const filterTabsRef = useTemplateRef<HTMLElement>('filterTabsScroller')
const filterInputRef = useTemplateRef<{ inputRef: HTMLInputElement | null }>('filterInput')
const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()

const filterTabs = computed<TabsItem[]>(() => [
  { value: 'all', label: t('filter.all') },
  { value: 'http', label: t('filter.http') },
  { value: 'https', label: t('filter.https') },
  { value: 'websocket', label: t('filter.websocket') },
  { value: 'tcp', label: t('filter.tcp') },
  { value: 'http1', label: t('filter.http1') },
  { value: 'http2', label: t('filter.http2') },
  { value: 'json', label: t('filter.json') },
  { value: 'xml', label: t('filter.xml') },
  { value: 'text', label: t('filter.text') },
  { value: 'html', label: t('filter.html') },
  { value: 'js', label: t('filter.js') },
  { value: 'image', label: t('filter.image') },
  { value: 'media', label: t('filter.media') },
  { value: 'binary', label: t('filter.binary') },
  { value: '1xx', label: '1xx' },
  { value: '2xx', label: '2xx' },
  { value: '3xx', label: '3xx' },
  { value: '4xx', label: '4xx' },
  { value: '5xx', label: '5xx' },
])

const activeTab = computed<string | number>({
  get: () => filterStore.activeFilterTab || 'all',
  set: (value) => {
    filterStore.setActiveTab(String(value))
  },
})

function clearSearch() {
  filterStore.searchText = ''
}

function handleFilterTabsWheel(event: WheelEvent) {
  const element = filterTabsRef.value
  if (!element) return

  const maxScrollLeft = element.scrollWidth - element.clientWidth
  if (maxScrollLeft <= 0) return

  const scrollDelta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
  if (scrollDelta === 0) return

  const nextScrollLeft = Math.min(maxScrollLeft, Math.max(0, element.scrollLeft + scrollDelta))
  if (nextScrollLeft === element.scrollLeft) return

  event.preventDefault()
  element.scrollLeft = nextScrollLeft
}

const offFocusFilterShortcut = registerShortcutHandler({
  commandId: 'capture.focusFilter',
  when: () =>
    workbenchStore.activeContent === 'traffic' && workspaceStore.activeTab.type === props.context,
  enabled: () => Boolean(filterInputRef.value?.inputRef),
  run: () => filterInputRef.value?.inputRef?.focus(),
})

onBeforeUnmount(() => {
  offFocusFilterShortcut()
})
</script>

<template>
  <div class="relative shrink-0 bg-app-panel [border-bottom:1px_solid_var(--app-border-color)]">
    <div class="flex min-h-9 items-center justify-between gap-2 px-2.5 py-1">
      <div
        ref="filterTabsScroller"
        class="relative min-w-0 flex-1 overflow-x-auto overflow-y-hidden scrollbar-none [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden"
        @wheel="handleFilterTabsWheel"
      >
        <UTabs
          v-model="activeTab"
          class="w-max"
          :items="filterTabs"
          :content="false"
          variant="pill"
          size="xs"
          :ui="{ list: 'w-max' }"
        />
      </div>
      <div class="shrink-0 px-1">
        <UInput
          ref="filterInput"
          v-model="filterStore.searchText"
          leading-icon="i-lucide-search"
          :placeholder="t('filter.search_placeholder')"
          :aria-label="t('filter.search_placeholder')"
          size="sm"
          class="w-[clamp(150px,13vw,200px)]"
          :ui="{ base: '' }"
        >
          <template #trailing>
            <UButton
              v-if="filterStore.searchText"
              icon="i-lucide-circle-x"
              color="neutral"
              variant="link"
              size="sm"
              :aria-label="t('toolbar.clear')"
              @click="clearSearch"
            />
          </template>
        </UInput>
      </div>
    </div>
  </div>
</template>
