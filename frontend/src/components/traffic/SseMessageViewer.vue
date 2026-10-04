<script setup lang="ts">
import { useVirtualizer } from '@tanstack/vue-virtual'
import { computed, nextTick, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import type { TabsItem } from '@nuxt/ui'
import { SaveBodyToFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { MONACO_LARGE_TEXT_THRESHOLD_CHARS } from '@/components/common/monacoLargeText'
import { useSseMessages } from '@/composables/useSseMessages'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { getSseMessageDetail, type SseEventIndex, type SseMessageDetail } from '@/utils/sse'

const props = defineProps<{
  body: string
  bodyEncoding?: string
  active: boolean
}>()

type DetailTab = 'data' | 'formatted' | 'raw'
const ROW_HEIGHT = 44
const TABLE_MIN_WIDTH = '480px'
const GRID_COLUMNS = '6rem 6rem minmax(10rem, 1fr)'
const { t } = useI18n()
const notify = useNotify()
const { messages, pending, parsing, revision } = useSseMessages(props)
const unavailable = computed(() => props.bodyEncoding === 'base64')
const scrollRef = ref<HTMLElement | null>(null)
const headerTrackRef = ref<HTMLElement | null>(null)
const shouldFollowTail = ref(true)
const detailVisible = ref(false)
const selectedMessage = shallowRef<SseMessageDetail | null>(null)
const detailTab = ref<DetailTab>('data')
const wordWrap = ref(false)
const exporting = ref(false)
const columns = ['id', 'event_type', 'preview'] as const

const virtualizer = useVirtualizer<HTMLElement, HTMLElement>(
  computed(() => ({
    count: messages.value.length,
    getScrollElement: () => scrollRef.value,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
    getItemKey: (index) => messages.value[index]?.sequence ?? index,
    initialRect: { width: 0, height: 480 },
  })),
)
const virtualRows = computed(() =>
  virtualizer.value.getVirtualItems().map((row) => ({
    row,
    message: messages.value[row.index],
  })),
)
const virtualContentHeight = computed(() => virtualizer.value.getTotalSize())

const formattedData = computed(() => {
  const data = selectedMessage.value?.data
  if (data === undefined || data.length >= MONACO_LARGE_TEXT_THRESHOLD_CHARS) return null
  try {
    return JSON.stringify(JSON.parse(data), null, 2)
  } catch {
    return null
  }
})
const detailTabs = computed<TabsItem[]>(() => {
  const tabs: TabsItem[] = [{ label: t('detail.sse.data'), value: 'data' }]
  if (formattedData.value !== null) tabs.push({ label: t('detail.formatted'), value: 'formatted' })
  tabs.push({ label: t('detail.sse.raw_event'), value: 'raw' })
  return tabs
})
const detailContent = computed(() => {
  if (!selectedMessage.value) return ''
  if (detailTab.value === 'raw') return selectedMessage.value.raw
  if (detailTab.value === 'formatted') return formattedData.value ?? selectedMessage.value.data
  return selectedMessage.value.data
})

function jumpToLatest() {
  shouldFollowTail.value = true
  const element = scrollRef.value
  if (element) element.scrollTop = element.scrollHeight
}

function onScroll(event: Event) {
  const element = event.target as HTMLElement
  shouldFollowTail.value = element.scrollHeight - element.scrollTop - element.clientHeight < 80
  if (headerTrackRef.value) {
    headerTrackRef.value.style.transform = `translateX(${-element.scrollLeft}px)`
  }
}

function openDetail(message: SseEventIndex) {
  selectedMessage.value = getSseMessageDetail(props.body, message)
  detailTab.value = 'data'
  wordWrap.value = false
  detailVisible.value = true
}

async function copyMessage() {
  if (!selectedMessage.value) return
  try {
    await copyText(detailContent.value)
    notify.success(t('detail.body_copied'))
  } catch (error) {
    notify.error(t('detail.body_copy_failed', { error: getErrorMessage(error) }))
  }
}

async function saveMessage() {
  if (!selectedMessage.value || exporting.value) return
  // Snapshot the selected view before the native dialog yields to new events.
  const body = detailContent.value
  const formatted = detailTab.value === 'formatted'
  const filename =
    detailTab.value === 'raw' ? 'sse-event.txt' : formatted ? 'sse-message.json' : 'sse-message.txt'
  exporting.value = true
  try {
    const path = (await Dialogs.SaveFile({ Filename: filename })).trim()
    if (!path) return
    await SaveBodyToFile({
      path,
      body,
      bodyEncoding: '',
      contentType: formatted ? 'application/json' : 'text/plain',
    })
  } catch (error) {
    if (!isDialogCancelError(error)) {
      notify.error(t('detail.body_save_failed', { error: getErrorMessage(error) }))
    }
  } finally {
    exporting.value = false
  }
}

watch(
  () => messages.value.length,
  async () => {
    const follow = shouldFollowTail.value
    await nextTick()
    if (props.active && follow) jumpToLatest()
  },
)
watch(revision, () => {
  detailVisible.value = false
  shouldFollowTail.value = true
  if (scrollRef.value) scrollRef.value.scrollTop = 0
})
watch(
  () => props.active,
  async (active) => {
    if (!active) {
      detailVisible.value = false
      return
    }
    await nextTick()
    virtualizer.value.measure()
    if (shouldFollowTail.value) jumpToLatest()
  },
)
watch(detailVisible, (visible) => {
  if (!visible) selectedMessage.value = null
})
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden pr-2.5 pb-2.5" role="tabpanel">
    <div class="flex min-h-8 shrink-0 items-center justify-between gap-2 pb-1.5">
      <div class="flex min-w-0 items-center gap-2 text-xs text-muted" role="status">
        <span v-if="!unavailable" class="truncate tabular-nums">{{
          t('detail.sse.message_count', { count: messages.length })
        }}</span>
        <span v-if="parsing" class="flex shrink-0 items-center gap-1">
          <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" aria-hidden="true" />
          {{ t('detail.sse.parsing') }}
        </span>
      </div>
      <UTooltip v-if="!unavailable" :text="t('detail.sse.jump_to_latest')">
        <UButton
          icon="i-lucide-arrow-down-to-line"
          size="sm"
          color="neutral"
          variant="ghost"
          square
          :aria-label="t('detail.sse.jump_to_latest')"
          :disabled="!messages.length"
          @click="jumpToLatest"
        />
      </UTooltip>
    </div>
    <UEmpty
      v-if="unavailable || (!messages.length && !parsing)"
      icon="i-lucide-radio"
      :title="t(unavailable ? 'detail.sse.unavailable' : 'detail.sse.empty')"
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />
    <div v-else class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <div
        class="shrink-0 overflow-hidden border border-app-border bg-app-elevated text-xs font-semibold text-app-text-secondary"
      >
        <div
          ref="headerTrackRef"
          class="grid h-8 w-full items-center gap-2 px-3"
          :style="{ gridTemplateColumns: GRID_COLUMNS, minWidth: TABLE_MIN_WIDTH }"
        >
          <span v-for="column in columns" :key="column" class="truncate">{{
            t(`detail.sse.${column}`)
          }}</span>
        </div>
      </div>
      <div
        ref="scrollRef"
        class="min-h-0 flex-1 overflow-auto border-x border-b border-app-border [overflow-anchor:none]"
        @scroll="onScroll"
      >
        <div
          class="relative min-h-full w-full"
          :style="{ height: `${virtualContentHeight}px`, minWidth: TABLE_MIN_WIDTH }"
        >
          <template v-for="{ row, message } in virtualRows" :key="row.key">
            <button
              v-if="message"
              type="button"
              class="absolute left-0 grid h-11 w-full items-center gap-2 border-b border-app-border bg-transparent px-3 text-left text-sm text-app-text hover:bg-app-hover focus-visible:z-10 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-app-accent"
              :style="{
                transform: `translateY(${row.start}px)`,
                gridTemplateColumns: GRID_COLUMNS,
              }"
              :aria-label="t('detail.sse.open_message', { sequence: message.sequence })"
              @click="openDetail(message)"
            >
              <span class="truncate tabular-nums text-muted">{{ message.lastEventId || '—' }}</span>
              <span class="truncate">{{ message.eventType }}</span>
              <span class="truncate whitespace-pre">{{
                message.preview || t('detail.sse.empty_data')
              }}</span>
            </button>
          </template>
        </div>
      </div>
    </div>
    <p v-if="pending && !parsing && !unavailable" class="shrink-0 pt-1.5 text-xs text-muted" role="status">
      {{ t('detail.sse.incomplete') }}
    </p>

    <UModal
      v-model:open="detailVisible"
      :title="t('detail.sse.message_detail')"
      :ui="{ content: 'max-w-[min(960px,92vw)]' }"
    >
      <template #body>
        <template v-if="selectedMessage && detailVisible">
          <div class="mb-3 flex flex-wrap items-center gap-2 text-sm">
            <UBadge color="neutral">#{{ selectedMessage.sequence }}</UBadge>
            <UBadge color="info" class="max-w-full truncate">{{
              selectedMessage.eventType
            }}</UBadge>
            <span class="min-w-0 break-all text-muted"
              >{{ t('detail.sse.id') }}: {{ selectedMessage.lastEventId || '—' }}</span
            >
          </div>
          <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
            <UTabs
              v-model="detailTab"
              :items="detailTabs"
              :content="false"
              variant="pill"
              size="xs"
            />
            <div class="flex shrink-0 items-center gap-1">
              <UTooltip :text="t('detail.wrap_body')">
                <UButton
                  icon="i-lucide-corner-down-left"
                  color="neutral"
                  variant="ghost"
                  size="sm"
                  square
                  :aria-label="t('detail.wrap_body')"
                  :aria-pressed="wordWrap"
                  @click="wordWrap = !wordWrap"
                />
              </UTooltip>
              <UTooltip :text="t('detail.sse.copy_message')">
                <UButton
                  icon="i-lucide-copy"
                  color="neutral"
                  variant="ghost"
                  size="sm"
                  square
                  :aria-label="t('detail.sse.copy_message')"
                  @click="copyMessage"
                />
              </UTooltip>
              <UTooltip :text="t('detail.sse.save_message')">
                <UButton
                  icon="i-lucide-download"
                  color="neutral"
                  variant="ghost"
                  size="sm"
                  square
                  :aria-label="t('detail.sse.save_message')"
                  :disabled="exporting"
                  @click="saveMessage"
                />
              </UTooltip>
            </div>
          </div>
          <div
            class="flex h-[min(70vh,720px)] min-h-0 overflow-hidden border border-app-border bg-app-elevated"
          >
            <MonacoBodyEditor
              :value="detailContent"
              :language="detailTab === 'formatted' ? 'json' : 'plaintext'"
              readonly
              :word-wrap="wordWrap"
            />
          </div>
        </template>
      </template>
    </UModal>
  </div>
</template>
