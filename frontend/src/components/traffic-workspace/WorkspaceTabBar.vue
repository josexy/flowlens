<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import type { WorkspaceTab } from '@/stores/trafficWorkspace'
import { useApiCollectionStore } from '@/stores/apiCollection'
import { useThemeStore } from '@/stores/theme'
import { useWorkbenchStore } from '@/stores/workbench'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import SaveApiRequestModal from '@/components/modal/SaveApiRequestModal.vue'
import AppTooltip from '@/components/common/AppTooltip.vue'
import type { ContextMenuItem, DropdownMenuItem } from '@nuxt/ui'
import { useNotify } from '@/composables/useNotify'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'

const { t } = useI18n()
const notify = useNotify()
const workspaceStore = useTrafficWorkspaceStore()
const apiCollectionStore = useApiCollectionStore()
const themeStore = useThemeStore()
const workbenchStore = useWorkbenchStore()
const saveShortcutKbds = useShortcutKbds('app.save')
const newHttpRequestShortcutKbds = useShortcutKbds('workspace.newHttpRequest')
const newWebSocketRequestShortcutKbds = useShortcutKbds('workspace.newWebSocketRequest')
const closeTabShortcutKbds = useShortcutKbds('workspace.closeTab')
const contextTabKey = ref<string | null>(null)
const saveModalVisible = ref(false)
const saveModalSaving = ref(false)
const saveModalTabKey = ref<string | null>(null)
const directSavingTabKey = ref<string | null>(null)
const pendingConfirm = ref<{
  title: string
  content: string
  positiveText: string
  onConfirm: () => void
} | null>(null)
const tabsContainerRef = ref<HTMLElement | null>(null)
const canScrollLeft = ref(false)
const canScrollRight = ref(false)
const isDraggingTabs = ref(false)
const SCROLL_STEP = 220
const legacyUntitledTitles = new Set(['\u672a\u547d\u540d'])
const tabFallbackMoveEvents = ['pointermove', 'mousemove', 'touchmove'] as const
const TAB_DRAG_PROJECTED_EVENT_KEY = '__flowLensTabDragProjected'
let tabFallbackLockFrame: number | null = null

type ProjectedDragEvent = MouseEvent & {
  [TAB_DRAG_PROJECTED_EVENT_KEY]?: true
}

const tabbarThemeVars = computed(() => {
  const themeVars = themeStore.isDark
    ? {
        '--tabbar-bg': 'var(--app-shell-bg)',
        '--tab-item-bg': 'var(--app-panel-bg)',
        '--tab-item-hover-bg': 'var(--app-control-bg)',
        '--tab-item-active-bg': 'var(--app-window-bg)',
        '--tab-close-hover-bg': 'var(--app-control-hover-bg)',
        '--tab-active-line': 'var(--app-accent-color)',
        '--tab-divider-color': 'var(--app-border-color)',
        '--tab-scroll-hover-bg': 'var(--app-control-bg)',
        '--tab-add-hover-bg': 'var(--app-control-bg)',
      }
    : {
        '--tabbar-bg': 'var(--app-shell-bg)',
        '--tab-item-bg': 'var(--app-elevated-bg)',
        '--tab-item-hover-bg': 'var(--app-control-bg)',
        '--tab-item-active-bg': 'var(--app-panel-bg)',
        '--tab-close-hover-bg': 'var(--app-control-hover-bg)',
        '--tab-active-line': 'var(--app-accent-color)',
        '--tab-divider-color': 'rgba(0, 0, 0, 0.16)',
        '--tab-scroll-hover-bg': 'var(--app-control-bg)',
        '--tab-add-hover-bg': 'var(--app-control-hover-bg)',
      }

  return themeVars
})

const tabItemClass = [
  'tab-item relative flex h-8.5 min-w-31 max-w-55 flex-[0_0_auto] select-none items-center bg-transparent text-app-text-secondary shadow-[inset_-1px_0_0_var(--tab-divider-color)] transition-[background-color,color,border-color,transform,box-shadow] duration-[0.18s] ease-[ease]',
  "before:pointer-events-none before:absolute before:inset-x-0 before:top-0 before:h-0.5 before:bg-transparent before:content-['']",
  'hover:bg-[color-mix(in_srgb,var(--tab-item-hover-bg)_82%,transparent)] hover:text-app-text',
  '[&.active]:z-1 [&.active]:bg-[color-mix(in_srgb,var(--tab-item-active-bg)_92%,transparent)] [&.active]:text-app-text [&.active]:before:bg-(--tab-active-line)',
  '[&.tab-chosen]:bg-(--tab-item-hover-bg) [&.tab-chosen]:opacity-[0.9]',
  '[&.tab-ghost]:bg-transparent [&.tab-ghost]:opacity-0 [&.tab-ghost]:shadow-none',
  '[&.tab-drag]:-translate-y-px [&.tab-drag]:scale-[1.01] [&.tab-drag]:opacity-[0.88] [&.tab-drag]:shadow-[inset_-1px_0_0_transparent,0_10px_24px_-14px_rgba(0,0,0,0.45)]',
  '[&.tab-fallback]:pointer-events-none [&.tab-fallback]:-translate-y-px [&.tab-fallback]:scale-[1.01] [&.tab-fallback]:cursor-grabbing [&.tab-fallback]:bg-(--tab-item-hover-bg) [&.tab-fallback]:opacity-[0.9] [&.tab-fallback]:shadow-[inset_-1px_0_0_transparent,0_12px_26px_-16px_rgba(0,0,0,0.5)] [&.tab-fallback]:transition-none',
].join(' ')

const tabMainClass =
  'tab-main flex h-full min-w-0 flex-1 items-center gap-1.5 px-3 [.tab-item.tab-drag_&]:cursor-grabbing'

const tabIconClass =
  'tab-icon flex shrink-0 items-center text-app-text-muted opacity-[0.72] transition-[transform,color] duration-[0.16s] ease-[ease] [.tab-item:hover_&]:scale-105 [.tab-item.active_&]:text-app-text-secondary'

const tabTitleClass =
  'tab-title min-w-0 flex-1 truncate text-sm font-medium leading-4 [.tab-item.active_&]:text-app-text-secondary'

const tabDirtyIndicatorClass =
  'tab-dirty-indicator mr-1 size-1.5 shrink-0 rounded-full bg-(--tab-active-line) shadow-[0_0_0_1px_color-mix(in_srgb,var(--tabbar-bg)_64%,transparent)]'

const tabCloseButtonClass =
  'tab-close mr-1.5 flex size-4.5 shrink-0 items-center justify-center rounded-full border-0 bg-transparent p-0 text-app-text-muted opacity-0 transition-[opacity,background-color,color] duration-[0.16s] ease-[ease] [.tab-item:hover_&]:opacity-[0.72] [.tab-item.active_&]:opacity-[0.72] hover:bg-(--tab-close-hover-bg) hover:text-app-text [.tab-item:hover_&:hover]:opacity-100'

const tabScrollButtonClass =
  'tab-scroll-button relative z-2 flex h-full w-0 shrink-0 items-center justify-center overflow-hidden border-0 bg-transparent p-0 text-app-text-muted opacity-0 pointer-events-none transition-[width,opacity,color,background-color] duration-[0.16s] ease-[ease] [&.visible]:w-6.5 [&.visible]:opacity-100 [&.visible]:pointer-events-auto hover:bg-(--tab-scroll-hover-bg) hover:text-app-text'

const tabAddButtonClass =
  'tab-add-button flex h-full w-7.5 shrink-0 items-center justify-center border-0 bg-transparent p-0 text-app-text-muted transition-[color,background-color] duration-[0.16s] ease-[ease] hover:bg-(--tab-add-hover-bg) hover:text-app-text focus-visible:bg-(--tab-add-hover-bg) focus-visible:text-app-text'

function getTabIconName(type: WorkspaceTab['type']) {
  switch (type) {
    case 'capture':
      return 'i-lucide-radio'
    case 'history':
      return 'i-lucide-archive'
    case 'http-request':
      return 'i-lucide-braces'
    default:
      return 'i-lucide-messages-square'
  }
}

const tabsContainerStyle = computed(() => {
  let reserved = 30
  if (canScrollLeft.value) reserved += 26
  if (canScrollRight.value) reserved += 26
  return { maxWidth: `calc(100% - ${reserved}px)` }
})

function activateTab(tabKey: string) {
  if (isDraggingTabs.value) {
    return
  }

  workspaceStore.activateTab(tabKey)
}

function confirmDirtyTabsClose(options: {
  title: string
  content: string
  positiveText: string
  onConfirm: () => void
}) {
  pendingConfirm.value = options
}

function handlePendingConfirm() {
  const action = pendingConfirm.value?.onConfirm
  pendingConfirm.value = null
  action?.()
}

function handlePendingConfirmVisibility(nextShow: boolean) {
  if (!nextShow) {
    pendingConfirm.value = null
  }
}

function closeTabWithPrompt(tabKey: string) {
  if (!workspaceStore.isTabDirty(tabKey)) {
    workspaceStore.closeTab(tabKey)
    return
  }
  confirmDirtyTabsClose({
    title: t('workspace.tab_context.close_confirm_title'),
    content: t('workspace.tab_context.close_confirm_content'),
    positiveText: t('workspace.tab_context.close_current'),
    onConfirm: () => {
      workspaceStore.closeTab(tabKey)
    },
  })
}

function closeTab(event: MouseEvent, tabKey: string) {
  event.stopPropagation()
  closeTabWithPrompt(tabKey)
}

function setContextTab(tabKey: string) {
  // Called on a tab's contextmenu; the wrapping UContextMenu opens at the pointer.
  contextTabKey.value = tabKey
}

function onTabsContextMenu(event: MouseEvent) {
  // Only tabs open the context menu; suppress right-clicks on empty tab-strip area.
  if (!(event.target as HTMLElement | null)?.closest('.tab-item')) {
    event.stopPropagation()
  }
}

const captureTab = computed(() => workspaceStore.tabs.find((tab) => tab.type === 'capture') ?? null)
const movableTabs = computed({
  get: () => workspaceStore.tabs.filter((tab) => tab.type !== 'capture'),
  set: (tabs) => {
    workspaceStore.reorderTabs(tabs)
  },
})

const contextTab = computed(
  () => workspaceStore.tabs.find((tab) => tab.key === contextTabKey.value) ?? null,
)

const contextTabIndex = computed(() => {
  if (!contextTab.value) {
    return -1
  }
  return workspaceStore.tabs.findIndex((tab) => tab.key === contextTab.value?.key)
})

const dirtyTabKeys = computed(() => {
  return new Set(
    workspaceStore.tabs
      .filter((tab) => tab.closable && workspaceStore.isTabDirty(tab.key))
      .map((tab) => tab.key),
  )
})

const allClosableTabKeys = computed(() => {
  return workspaceStore.tabs.filter((tab) => tab.closable).map((tab) => tab.key)
})

const otherClosableTabKeys = computed(() => {
  const tabKey = contextTab.value?.key
  return workspaceStore.tabs
    .filter((tab) => tab.closable && tab.key !== tabKey)
    .map((tab) => tab.key)
})

const rightClosableTabKeys = computed(() => {
  const index = contextTabIndex.value
  if (index === -1) {
    return []
  }
  return workspaceStore.tabs
    .slice(index + 1)
    .filter((tab) => tab.closable)
    .map((tab) => tab.key)
})

const contextOptions = computed(() => {
  const options: ContextMenuItem[] = []
  if (contextTab.value?.type === 'http-request' || contextTab.value?.type === 'websocket-client') {
    options.push({
      key: 'save-api',
      label: t('workspace.tab_context.save_api'),
      kbds: saveShortcutKbds.value,
      onSelect: () => void saveContextTab(),
    })
    options.push({
      key: 'divider-save-api',
      type: 'separator',
    })
  }
  options.push({
    key: 'close-current',
    label: t('workspace.tab_context.close_current'),
    kbds: closeTabShortcutKbds.value,
    disabled: !contextTab.value?.closable,
    onSelect: () => {
      if (contextTab.value?.key) {
        closeTabWithPrompt(contextTab.value.key)
      }
    },
  })
  options.push({
    key: 'force-close-current',
    label: t('workspace.tab_context.force_close_current'),
    disabled: !contextTab.value?.closable,
    onSelect: () => {
      if (contextTab.value?.key) {
        workspaceStore.closeTab(contextTab.value.key)
      }
    },
  })
  options.push({
    key: 'divider-close-current',
    type: 'separator',
  })
  options.push({
    key: 'close-other',
    label: t('workspace.tab_context.close_other'),
    disabled: otherClosableTabKeys.value.length === 0,
    onSelect: closeOtherWithPrompt,
  })
  options.push({
    key: 'force-close-other',
    label: t('workspace.tab_context.force_close_other'),
    disabled: otherClosableTabKeys.value.length === 0,
    onSelect: () => {
      if (contextTab.value?.key) {
        workspaceStore.closeOtherTabs(contextTab.value.key, true)
      }
    },
  })
  options.push({
    key: 'close-right',
    label: t('workspace.tab_context.close_right'),
    disabled: rightClosableTabKeys.value.length === 0,
    onSelect: closeRightWithPrompt,
  })
  options.push({
    key: 'force-close-right',
    label: t('workspace.tab_context.force_close_right'),
    disabled: rightClosableTabKeys.value.length === 0,
    onSelect: () => {
      if (contextTab.value?.key) {
        workspaceStore.closeRightTabs(contextTab.value.key, true)
      }
    },
  })
  options.push({
    key: 'close-all',
    label: t('workspace.tab_context.close_all'),
    disabled: allClosableTabKeys.value.length === 0,
    onSelect: closeAllWithPrompt,
  })
  options.push({
    key: 'force-close-all',
    label: t('workspace.tab_context.force_close_all'),
    disabled: allClosableTabKeys.value.length === 0,
    onSelect: () => workspaceStore.closeAllTabs(true),
  })
  return options
})

function getDefaultApiNameForTab(tab: WorkspaceTab | null | undefined) {
  if (!tab) return t('workspace.tab_bar.untitled')
  const title = getDisplayTitle(tab).trim()
  if (title && title !== t('workspace.tab_bar.untitled')) return title
  const url = tab.httpRequest?.url || tab.webSocketClient?.url || ''
  try {
    const parsed = new URL(url)
    return parsed.pathname && parsed.pathname !== '/'
      ? `${parsed.hostname}${parsed.pathname}`
      : parsed.hostname
  } catch {
    return t('workspace.tab_bar.untitled')
  }
}

const saveModalDefaultName = computed(() => {
  const tab = workspaceStore.tabs.find((item) => item.key === saveModalTabKey.value)
  return getDefaultApiNameForTab(tab)
})

async function saveTab(tab: WorkspaceTab | null | undefined) {
  if (!tab) return
  if (tab.apiId) {
    if (directSavingTabKey.value === tab.key) {
      return
    }
    directSavingTabKey.value = tab.key
    try {
      const request = await workspaceStore.saveExistingApiTab(tab.key)
      apiCollectionStore.upsertNode(request)
      notify.success(t('api_collection.saved'))
    } catch (error) {
      notify.error(t('api_collection.save_failed', { error: String(error) }))
    } finally {
      directSavingTabKey.value = null
    }
    return
  }
  try {
    await apiCollectionStore.ensureCollectionLoaded()
  } catch (error) {
    notify.error(t('api_collection.load_failed', { error: String(error) }))
    return
  }
  if (apiCollectionStore.folderOptions.length === 0) {
    notify.warning(t('api_collection.create_folder_first'))
    return
  }
  saveModalTabKey.value = tab.key
  saveModalVisible.value = true
}

async function saveContextTab() {
  await saveTab(contextTab.value)
}

function closeAllWithPrompt() {
  closeTabsWithPrompt({
    tabKeys: allClosableTabKeys.value,
    title: t('workspace.tab_context.close_all_confirm_title'),
    content: t('workspace.tab_context.close_all_confirm_content'),
    positiveText: t('workspace.tab_context.close_all'),
    onClose: (force) => workspaceStore.closeAllTabs(force),
  })
}

function closeTabsWithPrompt(options: {
  tabKeys: string[]
  title: string
  content: string
  positiveText: string
  onClose: (force: boolean) => void
}) {
  if (options.tabKeys.length === 0) {
    return
  }
  if (!workspaceStore.hasDirtyTabs(options.tabKeys)) {
    options.onClose(false)
    return
  }
  confirmDirtyTabsClose({
    title: options.title,
    content: options.content,
    positiveText: options.positiveText,
    onConfirm: () => {
      options.onClose(true)
    },
  })
}

function closeOtherWithPrompt() {
  const tabKey = contextTab.value?.key
  if (!tabKey) {
    return
  }
  closeTabsWithPrompt({
    tabKeys: otherClosableTabKeys.value,
    title: t('workspace.tab_context.close_other_confirm_title'),
    content: t('workspace.tab_context.close_other_confirm_content'),
    positiveText: t('workspace.tab_context.close_other'),
    onClose: (force) => workspaceStore.closeOtherTabs(tabKey, force),
  })
}

function closeRightWithPrompt() {
  const tabKey = contextTab.value?.key
  if (!tabKey) {
    return
  }
  closeTabsWithPrompt({
    tabKeys: rightClosableTabKeys.value,
    title: t('workspace.tab_context.close_right_confirm_title'),
    content: t('workspace.tab_context.close_right_confirm_content'),
    positiveText: t('workspace.tab_context.close_right'),
    onClose: (force) => workspaceStore.closeRightTabs(tabKey, force),
  })
}

async function handleSaveModalSubmit(form: { parentId: string; name: string }) {
  if (!saveModalTabKey.value) return
  saveModalSaving.value = true
  try {
    const request = await workspaceStore.saveRequestTabAsApi(
      saveModalTabKey.value,
      form.parentId,
      form.name,
    )
    apiCollectionStore.upsertNode(request, form.parentId)
    apiCollectionStore.selectNode(request.id)
    saveModalVisible.value = false
    notify.success(t('api_collection.saved'))
  } catch (error) {
    notify.error(t('api_collection.save_failed', { error: String(error) }))
  } finally {
    saveModalSaving.value = false
  }
}

function getDisplayTitle(tab: { type: string; title: string }) {
  if (tab.type === 'capture') {
    return t('menu.capture')
  }
  if (tab.type === 'http-request' || tab.type === 'websocket-client') {
    if (!tab.title || legacyUntitledTitles.has(tab.title)) {
      return t('workspace.tab_bar.untitled')
    }
    return tab.title
  }
  const timeTitlePattern = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/
  if (timeTitlePattern.test(tab.title)) {
    return tab.title.slice(5)
  }
  return tab.title
}

const createOptions = computed<DropdownMenuItem[]>(() => [
  {
    key: 'new-http',
    label: t('workspace.tab_bar.new_http'),
    kbds: newHttpRequestShortcutKbds.value,
    onSelect: () => handleCreateSelect('new-http'),
  },
  {
    key: 'new-ws',
    label: t('workspace.tab_bar.new_ws'),
    kbds: newWebSocketRequestShortcutKbds.value,
    onSelect: () => handleCreateSelect('new-ws'),
  },
])

function handleCreateSelect(key: string) {
  if (key === 'new-http') {
    workspaceStore.createHttpRequestTab('new')
    return
  }
  if (key === 'new-ws') {
    workspaceStore.createWebSocketClientTab('new')
  }
}

function lockTabFallbackYAxis() {
  tabFallbackLockFrame = null

  const fallback = document.querySelector<HTMLElement>('.tab-fallback')
  const transform = fallback?.style.transform
  if (!fallback || !transform || transform === 'none') {
    return
  }

  const matrixMatch = /^matrix\((.+)\)$/.exec(transform)
  const matrixValue = matrixMatch?.[1]
  if (matrixValue) {
    const values = matrixValue.split(',').map((value) => value.trim())
    if (values.length === 6 && values[5] !== '0') {
      values[5] = '0'
      fallback.style.transform = `matrix(${values.join(', ')})`
    }
    return
  }

  const matrix3dMatch = /^matrix3d\((.+)\)$/.exec(transform)
  const matrix3dValue = matrix3dMatch?.[1]
  if (matrix3dValue) {
    const values = matrix3dValue.split(',').map((value) => value.trim())
    if (values.length === 16 && values[13] !== '0') {
      values[13] = '0'
      fallback.style.transform = `matrix3d(${values.join(', ')})`
    }
  }
}

function getTabDragTrackClientY() {
  const track = tabsContainerRef.value?.querySelector<HTMLElement>('.draggable-tabs')
  const rect = (track ?? tabsContainerRef.value)?.getBoundingClientRect()
  if (!rect) {
    return null
  }

  return rect.top + rect.height / 2
}

function markProjectedDragEvent<T extends MouseEvent>(event: T) {
  Object.defineProperty(event, TAB_DRAG_PROJECTED_EVENT_KEY, {
    value: true,
  })
  return event as T & ProjectedDragEvent
}

function buildProjectedMouseEvent(event: MouseEvent, clientY: number) {
  return markProjectedDragEvent(
    new MouseEvent(event.type, {
      bubbles: true,
      cancelable: true,
      view: window,
      detail: event.detail,
      screenX: event.screenX,
      screenY: event.screenY + clientY - event.clientY,
      clientX: event.clientX,
      clientY,
      ctrlKey: event.ctrlKey,
      altKey: event.altKey,
      shiftKey: event.shiftKey,
      metaKey: event.metaKey,
      button: event.button,
      buttons: event.buttons,
      relatedTarget: event.relatedTarget,
    }),
  )
}

function buildProjectedPointerEvent(event: PointerEvent, clientY: number) {
  return markProjectedDragEvent(
    new PointerEvent(event.type, {
      bubbles: true,
      cancelable: true,
      view: window,
      detail: event.detail,
      screenX: event.screenX,
      screenY: event.screenY + clientY - event.clientY,
      clientX: event.clientX,
      clientY,
      ctrlKey: event.ctrlKey,
      altKey: event.altKey,
      shiftKey: event.shiftKey,
      metaKey: event.metaKey,
      button: event.button,
      buttons: event.buttons,
      pointerId: event.pointerId,
      width: event.width,
      height: event.height,
      pressure: event.pressure,
      tangentialPressure: event.tangentialPressure,
      tiltX: event.tiltX,
      tiltY: event.tiltY,
      twist: event.twist,
      pointerType: event.pointerType,
      isPrimary: event.isPrimary,
    }),
  )
}

function projectTabDragMoveEvent(event: MouseEvent | PointerEvent) {
  if (!isDraggingTabs.value || (event as ProjectedDragEvent)[TAB_DRAG_PROJECTED_EVENT_KEY]) {
    return
  }

  const trackY = getTabDragTrackClientY()
  if (trackY === null || Math.abs(event.clientY - trackY) < 1) {
    return
  }

  event.stopImmediatePropagation()
  if (event.cancelable) {
    event.preventDefault()
  }

  const projectedEvent =
    event instanceof PointerEvent
      ? buildProjectedPointerEvent(event, trackY)
      : buildProjectedMouseEvent(event, trackY)
  document.dispatchEvent(projectedEvent)
  scheduleTabFallbackYAxisLock()
}

function scheduleTabFallbackYAxisLock() {
  if (tabFallbackLockFrame !== null) {
    return
  }

  tabFallbackLockFrame = window.requestAnimationFrame(lockTabFallbackYAxis)
}

function startTabFallbackYAxisLock() {
  document.addEventListener('pointermove', projectTabDragMoveEvent, true)
  document.addEventListener('mousemove', projectTabDragMoveEvent, true)
  tabFallbackMoveEvents.forEach((eventName) => {
    document.addEventListener(eventName, scheduleTabFallbackYAxisLock, { passive: true })
  })
  scheduleTabFallbackYAxisLock()
}

function stopTabFallbackYAxisLock() {
  document.removeEventListener('pointermove', projectTabDragMoveEvent, true)
  document.removeEventListener('mousemove', projectTabDragMoveEvent, true)
  tabFallbackMoveEvents.forEach((eventName) => {
    document.removeEventListener(eventName, scheduleTabFallbackYAxisLock)
  })
  if (tabFallbackLockFrame !== null) {
    window.cancelAnimationFrame(tabFallbackLockFrame)
    tabFallbackLockFrame = null
  }
}

function handleTabDragStart() {
  isDraggingTabs.value = true
  startTabFallbackYAxisLock()
}

function handleTabDragEnd() {
  stopTabFallbackYAxisLock()
  nextTick(() => {
    updateScrollState()
  })
  window.setTimeout(() => {
    isDraggingTabs.value = false
  }, 0)
}

function updateScrollState() {
  const element = tabsContainerRef.value
  if (!element) {
    canScrollLeft.value = false
    canScrollRight.value = false
    return
  }

  const maxScrollLeft = element.scrollWidth - element.clientWidth
  canScrollLeft.value = element.scrollLeft > 2
  canScrollRight.value = element.scrollLeft < maxScrollLeft - 2
}

function scrollTabs(direction: 'left' | 'right') {
  const element = tabsContainerRef.value
  if (!element) {
    return
  }

  const offset = direction === 'left' ? -SCROLL_STEP : SCROLL_STEP
  element.scrollBy({ left: offset, behavior: 'smooth' })
}

function handleTabsWheel(event: WheelEvent) {
  const element = tabsContainerRef.value
  if (!element) {
    return
  }

  const maxScrollLeft = element.scrollWidth - element.clientWidth
  if (maxScrollLeft <= 0) {
    return
  }

  const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
  if (delta === 0) {
    return
  }

  const nextScrollLeft = Math.min(Math.max(element.scrollLeft + delta, 0), maxScrollLeft)
  if (Math.abs(nextScrollLeft - element.scrollLeft) < 1) {
    return
  }

  event.preventDefault()
  element.scrollLeft = nextScrollLeft
}

function scrollActiveTabIntoView() {
  const element = tabsContainerRef.value
  if (!element) {
    return
  }

  const activeElement = element.querySelector<HTMLElement>('.tab-item.active')
  if (!activeElement) {
    return
  }

  activeElement.scrollIntoView({
    behavior: 'smooth',
    block: 'nearest',
    inline: 'nearest',
  })
}

function isTrafficContentActive() {
  return workbenchStore.activeContent === 'traffic'
}

function isActiveRequestTabDirty() {
  const tab = workspaceStore.activeTab
  return (
    (tab.type === 'http-request' || tab.type === 'websocket-client') && workspaceStore.isTabDirty(tab.key)
  )
}

const offShortcutHandlers = [
  registerShortcutHandler({
    commandId: 'app.save',
    when: () => isTrafficContentActive() && isActiveRequestTabDirty(),
    run: () => saveTab(workspaceStore.activeTab),
  }),
  registerShortcutHandler({
    commandId: 'workspace.newHttpRequest',
    when: isTrafficContentActive,
    run: () => {
      workspaceStore.createHttpRequestTab('new')
    },
  }),
  registerShortcutHandler({
    commandId: 'workspace.newWebSocketRequest',
    when: isTrafficContentActive,
    run: () => {
      workspaceStore.createWebSocketClientTab('new')
    },
  }),
  registerShortcutHandler({
    commandId: 'workspace.closeTab',
    when: () => isTrafficContentActive() && workspaceStore.activeTab.closable,
    run: () => closeTabWithPrompt(workspaceStore.activeTab.key),
  }),
  registerShortcutHandler({
    commandId: 'workspace.nextTab',
    when: isTrafficContentActive,
    enabled: () => workspaceStore.tabs.length > 1,
    run: () => workspaceStore.activateNextTab(),
  }),
  registerShortcutHandler({
    commandId: 'workspace.previousTab',
    when: isTrafficContentActive,
    enabled: () => workspaceStore.tabs.length > 1,
    run: () => workspaceStore.activatePreviousTab(),
  }),
]

let resizeObserver: ResizeObserver | null = null

onMounted(() => {
  const element = tabsContainerRef.value
  if (!element) {
    return
  }

  updateScrollState()
  element.addEventListener('scroll', updateScrollState, { passive: true })
  resizeObserver = new ResizeObserver(() => {
    updateScrollState()
  })
  resizeObserver.observe(element)
})

onBeforeUnmount(() => {
  offShortcutHandlers.forEach((off) => off())
  tabsContainerRef.value?.removeEventListener('scroll', updateScrollState)
  resizeObserver?.disconnect()
  resizeObserver = null
  stopTabFallbackYAxisLock()
})

watch(
  () => workspaceStore.activeTabKey,
  () => {
    nextTick(() => {
      scrollActiveTabIntoView()
      updateScrollState()
    })
  },
)

watch(
  () => workspaceStore.tabs.length,
  () => {
    nextTick(() => {
      updateScrollState()
    })
  },
)

watch(
  () => workspaceStore.tabs.map((tab) => tab.key).join('|'),
  () => {
    nextTick(() => {
      updateScrollState()
    })
  },
)
</script>

<template>
  <div
    class="relative flex h-8.5 min-h-8.5 shrink-0 items-stretch bg-(--tabbar-bg) [border-bottom:1px_solid_var(--app-border-color)]"
    :style="tabbarThemeVars"
  >
    <button
      v-if="canScrollLeft"
      class="border-r border-app-border"
      :class="[tabScrollButtonClass, { visible: canScrollLeft }]"
      type="button"
      @click="scrollTabs('left')"
    >
      <UIcon name="i-lucide-chevron-left" class="size-3.5 shrink-0" />
    </button>
    <UContextMenu :items="contextOptions">
      <div
        ref="tabsContainerRef"
        class="min-w-0 flex-[0_1_auto] overflow-x-auto overflow-y-hidden scrollbar-none [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden"
        :style="tabsContainerStyle"
        @wheel="handleTabsWheel"
        @contextmenu="onTabsContextMenu"
      >
      <div class="flex w-max min-w-full items-stretch">
        <div
          v-if="captureTab"
          :class="[tabItemClass, { active: workspaceStore.activeTabKey === captureTab.key }]"
          @click="activateTab(captureTab.key)"
          @contextmenu="setContextTab(captureTab.key)"
        >
            <span :class="tabMainClass">
              <span :class="tabIconClass">
                <UIcon :name="getTabIconName(captureTab.type)" class="size-3.5 shrink-0" />
              </span>
              <span :class="tabTitleClass">{{ getDisplayTitle(captureTab) }}</span>
            </span>
        </div>
        <VueDraggable
          v-model="movableTabs"
          class="flex flex-[0_0_auto] items-stretch"
          tag="div"
          :animation="170"
          easing="cubic-bezier(0.2, 0, 0, 1)"
          direction="horizontal"
          handle=".tab-drag-handle"
          ghost-class="tab-ghost"
          chosen-class="tab-chosen"
          drag-class="tab-drag"
          fallback-class="tab-fallback"
          :swap-threshold="1"
          :force-fallback="true"
          :fallback-tolerance="3"
          :scroll="true"
          :scroll-sensitivity="60"
          :scroll-speed="14"
          @start="handleTabDragStart"
          @end="handleTabDragEnd"
        >
          <div
            v-for="tab in movableTabs"
            :key="tab.key"
            :class="[tabItemClass, { active: workspaceStore.activeTabKey === tab.key }]"
            @click="activateTab(tab.key)"
            @contextmenu="setContextTab(tab.key)"
          >
            <span class="tab-drag-handle" :class="tabMainClass">
              <span :class="tabIconClass">
                <UIcon :name="getTabIconName(tab.type)" class="size-3.5 shrink-0" />
              </span>
              <span :class="tabTitleClass">{{ getDisplayTitle(tab) }}</span>
            </span>
            <AppTooltip
              v-if="dirtyTabKeys.has(tab.key)"
              :text="t('workspace.tab_bar.unsaved_changes')"
            >
              <template #trigger>
                <span :class="tabDirtyIndicatorClass" aria-hidden="true" />
              </template>
            </AppTooltip>
            <AppTooltip v-if="tab.closable" :text="t('workspace.close')">
              <template #trigger>
                <button :class="tabCloseButtonClass" type="button" @click="closeTab($event, tab.key)">
                  <UIcon name="i-lucide-x" class="size-3 shrink-0" />
                </button>
              </template>
            </AppTooltip>
          </div>
        </VueDraggable>
      </div>
      </div>
    </UContextMenu>
    <button
      v-if="canScrollRight"
      type="button"
      class="border-x border-app-border"
      :class="[tabScrollButtonClass, { visible: canScrollRight }]"
      @click="scrollTabs('right')"
    >
      <UIcon name="i-lucide-chevron-right" class="size-3.5 shrink-0" />
    </button>
    <UDropdownMenu :items="createOptions" :content="{ side: 'bottom', align: 'end' }">
      <button :class="tabAddButtonClass" type="button">
        <UIcon name="i-lucide-plus" class="size-3.5 shrink-0" />
      </button>
    </UDropdownMenu>
  </div>
  <SaveApiRequestModal
    v-model:show="saveModalVisible"
    :default-name="saveModalDefaultName"
    :default-parent-id="apiCollectionStore.lastSelectedFolderId"
    :folder-options="apiCollectionStore.folderTreeOptions"
    :saving="saveModalSaving"
    @save="handleSaveModalSubmit"
  />
  <ConfirmCardModal
    :show="Boolean(pendingConfirm)"
    :title="pendingConfirm?.title ?? ''"
    :positive-text="pendingConfirm?.positiveText ?? ''"
    :negative-text="t('api_collection.cancel')"
    positive-type="warning"
    @update:show="handlePendingConfirmVisibility"
    @positive-click="handlePendingConfirm"
  >
    {{ pendingConfirm?.content ?? '' }}
  </ConfirmCardModal>
</template>
