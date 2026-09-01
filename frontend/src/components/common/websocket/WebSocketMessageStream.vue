<script setup lang="ts">
import { useVirtualizer } from '@tanstack/vue-virtual'
import type { VirtualItem } from '@tanstack/vue-virtual'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  WebSocketDirectionFilter,
  WebSocketDisplayMessage,
  WebSocketViewMode,
} from '@/types/websocket'
import WebSocketMessageDetailModal from '@/components/modal/WebSocketMessageDetailModal.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'

const props = withDefaults(
  defineProps<{
    messages: WebSocketDisplayMessage[]
    directionFilter: WebSocketDirectionFilter
    viewMode: WebSocketViewMode
    showClearAction?: boolean
  }>(),
  {
    showClearAction: false,
  },
)

const emit = defineEmits<{
  'update:directionFilter': [value: WebSocketDirectionFilter]
  'update:viewMode': [value: WebSocketViewMode]
  clear: []
}>()

const { t } = useI18n()

const ROW_HEIGHT = 44
const BUFFER = 12
const CONVERSATION_ESTIMATED_HEIGHT = 76
const CONVERSATION_OVERSCAN = 8

const detailVisible = ref(false)
const activeMessage = ref<WebSocketDisplayMessage | null>(null)
const listScrollRef = ref<HTMLElement | null>(null)
const conversationScrollRef = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportHeight = ref(480)
const listShouldFollowTail = ref(true)
const conversationShouldFollowTail = ref(true)

let resizeObserver: ResizeObserver | null = null

const filteredMessages = computed(() => {
  if (props.directionFilter === 'all') {
    return props.messages
  }
  return props.messages.filter((item) => item.direction === props.directionFilter)
})

const conversationVirtualizer = useVirtualizer<HTMLElement, HTMLElement>(
  computed(() => ({
    count: filteredMessages.value.length,
    getScrollElement: () => conversationScrollRef.value,
    estimateSize: () => CONVERSATION_ESTIMATED_HEIGHT,
    overscan: CONVERSATION_OVERSCAN,
    getItemKey: (index) => filteredMessages.value[index]?.id ?? index,
    initialRect: {
      width: 0,
      height: 480,
    },
  })),
)

const virtualConversationMessages = computed(() =>
  conversationVirtualizer.value
    .getVirtualItems()
    .map((virtualRow) => ({
      virtualRow,
      message: filteredMessages.value[virtualRow.index],
    }))
    .filter(
      (row): row is { virtualRow: VirtualItem; message: WebSocketDisplayMessage } =>
        row.message !== undefined,
    ),
)
const conversationContentHeight = computed(() => conversationVirtualizer.value.getTotalSize())

const visibleStart = computed(() => Math.max(0, Math.floor(scrollTop.value / ROW_HEIGHT) - BUFFER))
const visibleEnd = computed(() =>
  Math.min(
    filteredMessages.value.length,
    Math.ceil((scrollTop.value + viewportHeight.value) / ROW_HEIGHT) + BUFFER,
  ),
)
const visibleMessages = computed(() =>
  filteredMessages.value.slice(visibleStart.value, visibleEnd.value),
)
const listViewportStyle = computed(() => ({
  paddingTop: `${visibleStart.value * ROW_HEIGHT}px`,
  paddingBottom: `${Math.max(0, filteredMessages.value.length - visibleEnd.value) * ROW_HEIGHT}px`,
}))
const isListContentShort = computed(
  () => filteredMessages.value.length * ROW_HEIGHT < viewportHeight.value,
)

const filterLabel = computed(() => {
  switch (props.directionFilter) {
    case 'send':
      return t('workspace.websocket_client.filter_send')
    case 'receive':
      return t('workspace.websocket_client.filter_receive')
    default:
      return t('workspace.websocket_client.filter_all')
  }
})

const isListMode = computed(() => props.viewMode === 'list')
const hasMessages = computed(() => filteredMessages.value.length > 0)
const clearLabel = computed(() => t('workspace.websocket_client.clear_messages'))
const viewModeLabel = computed(() =>
  props.viewMode === 'list'
    ? t('workspace.websocket_client.view_mode_list')
    : t('workspace.websocket_client.view_mode_conversation'),
)

function cycleDirectionFilter() {
  const nextFilter: Record<WebSocketDirectionFilter, WebSocketDirectionFilter> = {
    all: 'send',
    send: 'receive',
    receive: 'all',
  }
  emit('update:directionFilter', nextFilter[props.directionFilter])
}

function toggleViewMode() {
  emit('update:viewMode', props.viewMode === 'list' ? 'conversation' : 'list')
}

function openMessageDetail(message: WebSocketDisplayMessage) {
  activeMessage.value = message
  detailVisible.value = true
}

function shouldShowRowBorder(visibleIndex: number) {
  const messageIndex = visibleStart.value + visibleIndex
  return messageIndex < filteredMessages.value.length - 1 || isListContentShort.value
}

function onListScroll(event: Event) {
  const element = event.target as HTMLElement
  scrollTop.value = element.scrollTop
  listShouldFollowTail.value = isNearBottom(element)
}

function onConversationScroll(event: Event) {
  conversationShouldFollowTail.value = isNearBottom(event.target as HTMLElement)
}

function measureConversationMessage(element: unknown) {
  if (element instanceof HTMLElement) {
    conversationVirtualizer.value.measureElement(element)
  }
}

function isNearBottom(element: HTMLElement) {
  return element.scrollHeight - element.scrollTop - element.clientHeight < 80
}

function maybeTrackViewport() {
  if (!listScrollRef.value) {
    return
  }
  viewportHeight.value = listScrollRef.value.clientHeight || viewportHeight.value
  resizeObserver?.disconnect()
  resizeObserver = new ResizeObserver((entries) => {
    const nextHeight = entries[0]?.contentRect.height
    if (nextHeight) {
      viewportHeight.value = nextHeight
    }
  })
  resizeObserver.observe(listScrollRef.value)
}

watch(
  () => filteredMessages.value.length,
  async () => {
    const shouldFollowTail = isListMode.value
      ? listShouldFollowTail.value
      : conversationShouldFollowTail.value
    await nextTick()
    const element = isListMode.value ? listScrollRef.value : conversationScrollRef.value
    if (!element || !shouldFollowTail) {
      return
    }
    element.scrollTop = element.scrollHeight
  },
)

watch(isListMode, async (enabled) => {
  await nextTick()
  if (enabled) {
    maybeTrackViewport()
    if (listShouldFollowTail.value && listScrollRef.value) {
      listScrollRef.value.scrollTop = listScrollRef.value.scrollHeight
    }
    return
  }
  if (conversationShouldFollowTail.value && conversationScrollRef.value) {
    conversationScrollRef.value.scrollTop = conversationScrollRef.value.scrollHeight
  }
})

onMounted(() => {
  maybeTrackViewport()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
    <div
      class="flex items-center justify-between gap-3 px-2.5 py-1 [border-bottom:1px_solid_var(--app-border-color)]"
    >
      <div class="flex min-w-7 items-center">
        <span
          class="inline-flex h-5.5 min-w-7 items-center justify-center rounded-full px-2"
          :class="
            hasMessages
              ? 'border border-[color-mix(in_srgb,var(--app-accent-color)_18%,transparent)] bg-[color-mix(in_srgb,var(--app-accent-color)_8%,transparent)]'
              : 'border border-[rgba(148,163,184,0.16)] bg-[rgba(148,163,184,0.08)]'
          "
        >
          <span
            class="text-sm font-semibold leading-none tabular-nums"
            :class="hasMessages ? 'text-app-accent' : 'text-app-text-muted'"
            >{{ filteredMessages.length }}</span
          >
        </span>
      </div>

      <div class="flex items-center gap-1">
        <UTooltip :text="filterLabel" :content="{ side: 'top' }">
          <UButton
            size="sm"
            color="neutral"
            variant="ghost"
            icon="i-lucide-arrow-up-down"
            :aria-label="filterLabel"
            @click="cycleDirectionFilter"
          />
        </UTooltip>

        <UTooltip v-if="props.showClearAction" :text="clearLabel" :content="{ side: 'top' }">
          <UButton
            size="sm"
            color="neutral"
            variant="ghost"
            icon="i-lucide-trash-2"
            :aria-label="clearLabel"
            @click="emit('clear')"
          />
        </UTooltip>

        <UTooltip :text="viewModeLabel" :content="{ side: 'top' }">
          <UButton
            size="sm"
            color="neutral"
            variant="ghost"
            :icon="props.viewMode === 'list' ? 'i-lucide-list' : 'i-lucide-messages-square'"
            :aria-label="viewModeLabel"
            @click="toggleViewMode"
          />
        </UTooltip>
      </div>
    </div>

    <div v-if="filteredMessages.length === 0" class="flex flex-1 items-center justify-center">
      <UEmpty
        icon="i-lucide-messages-square"
        :title="t('workspace.websocket_client.no_messages')"
        :size="appEmptyStateSize"
        variant="naked"
        :ui="appEmptyStateUi"
      />
    </div>

    <div
      v-else-if="props.viewMode === 'list'"
      ref="listScrollRef"
      class="min-h-0 flex-1 overflow-auto p-2.5"
      @scroll="onListScroll"
    >
      <div class="min-h-full border border-app-border" :style="listViewportStyle">
        <button
          v-for="(message, visibleIndex) in visibleMessages"
          :key="message.id"
          type="button"
          class="grid h-11 w-full grid-cols-[28px_88px_minmax(0,1fr)] items-center gap-2.5 border-none bg-transparent px-3 text-left text-app-text hover:bg-[color-mix(in_srgb,var(--app-panel-bg)_82%,#0f172a_4%)]"
          :class="
            shouldShowRowBorder(visibleIndex)
              ? '[border-bottom:1px_solid_var(--app-border-color)]'
              : ''
          "
          @click="openMessageDetail(message)"
        >
          <span
            class="inline-flex items-center justify-center text-base"
            :class="
              message.direction === 'send'
                ? 'text-(--app-ws-send-color,#d97706)'
                : 'text-(--app-ws-receive-color,#2563eb)'
            "
          >
            <UIcon
              :name="message.direction === 'send' ? 'i-lucide-arrow-up' : 'i-lucide-arrow-down'"
              class="size-4 shrink-0"
              aria-hidden="true"
            />
          </span>
          <span>
            <span
              class="inline-flex min-h-5 items-center justify-center whitespace-nowrap rounded-full px-2 text-xs font-bold leading-none"
              :class="
                message.direction === 'send'
                  ? 'bg-[color-mix(in_srgb,var(--app-ws-send-color,#d97706)_12%,transparent)] text-(--app-ws-send-tag-color,#b45309)'
                  : 'bg-[color-mix(in_srgb,var(--app-ws-receive-color,#2563eb)_12%,transparent)] text-(--app-ws-receive-tag-color,#1d4ed8)'
              "
            >
              {{
                message.direction === 'send'
                  ? t('workspace.websocket_client.filter_send')
                  : t('workspace.websocket_client.filter_receive')
              }}
            </span>
          </span>
          <span class="min-w-0 truncate text-sm text-app-text">
            <template v-if="message.msgType === 'text'">{{ message.data }}</template>
            <template v-else>{{
              t('workspace.websocket_client.binary_size', { size: message.dataSize })
            }}</template>
          </span>
        </button>
      </div>
    </div>

    <div
      v-else
      ref="conversationScrollRef"
      class="min-h-0 flex-1 overflow-auto p-3.5"
      @scroll="onConversationScroll"
    >
      <div
        class="relative w-full"
        :style="{ height: `${conversationContentHeight}px` }"
      >
        <div
          v-for="{ virtualRow, message } in virtualConversationMessages"
          :key="String(virtualRow.key)"
          :ref="measureConversationMessage"
          :data-index="virtualRow.index"
          class="absolute left-0 top-0 flex w-full pb-2.5"
          :class="message.direction === 'send' ? 'justify-end' : 'justify-start'"
          :style="{ transform: `translateY(${virtualRow.start}px)` }"
        >
          <div
            class="max-w-[min(76%,720px)] rounded-2xl border border-app-border px-3 py-2.5 shadow-[0_6px_16px_rgba(15,23,42,0.04)]"
            :class="
              message.direction === 'send'
                ? 'bg-[rgba(245,158,11,0.08)]'
                : 'bg-[rgba(37,99,235,0.08)]'
            "
          >
            <div class="mb-1.5">
              <span
                class="inline-flex min-h-5 items-center justify-center whitespace-nowrap rounded-full px-2 text-xs font-bold leading-none"
                :class="
                  message.direction === 'send'
                    ? 'bg-[color-mix(in_srgb,var(--app-ws-send-color,#d97706)_12%,transparent)] text-(--app-ws-send-tag-color,#b45309)'
                    : 'bg-[color-mix(in_srgb,var(--app-ws-receive-color,#2563eb)_12%,transparent)] text-(--app-ws-receive-tag-color,#1d4ed8)'
                "
              >
                {{
                  message.direction === 'send'
                    ? t('workspace.websocket_client.filter_send')
                    : t('workspace.websocket_client.filter_receive')
                }}
              </span>
            </div>

            <pre
              v-if="message.msgType === 'text'"
              class="m-0 whitespace-pre-wrap wrap-break-word text-sm leading-[1.6]"
              style="font-family: var(--app-font-family)"
              >{{ message.data }}</pre
            >
            <button
              v-else
              type="button"
              class="rounded-full border-none bg-[rgba(15,23,42,0.08)] px-3 py-2 text-sm text-app-text"
              @click="openMessageDetail(message)"
            >
              {{ t('workspace.websocket_client.binary_size', { size: message.dataSize }) }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <WebSocketMessageDetailModal
      :show="detailVisible"
      :message="activeMessage"
      @update:show="detailVisible = $event"
    />
  </div>
</template>
