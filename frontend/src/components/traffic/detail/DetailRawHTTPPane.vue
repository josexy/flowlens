<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import {
  getMonacoWrappedTextChunk,
  requiresMonacoLargeTextOptimizations,
} from '@/components/common/monacoLargeText'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'

const props = withDefaults(
  defineProps<{
    value: string
    warningMessage?: string
    waiting?: boolean
  }>(),
  {
    warningMessage: '',
    waiting: false,
  },
)

const { t } = useI18n()
const notify = useNotify()
const wordWrap = ref(true)
const largeTextWrapEnabled = ref(false)
const wrappedChunkIndex = ref(0)
const largeTextMode = computed(() => requiresMonacoLargeTextOptimizations(props.value))
const wrappedChunk = computed(() =>
  getMonacoWrappedTextChunk(props.value, wrappedChunkIndex.value),
)
const usesWrappedChunk = computed(
  () => largeTextMode.value && largeTextWrapEnabled.value,
)
const editorValue = computed(() =>
  usesWrappedChunk.value ? wrappedChunk.value.text : props.value,
)
const effectiveWordWrap = computed(() =>
  largeTextMode.value ? largeTextWrapEnabled.value : wordWrap.value,
)
const showWrappedChunkPagination = computed(
  () => usesWrappedChunk.value && wrappedChunk.value.count > 1,
)
const wrappedChunkPageLabel = computed(() =>
  t('detail.large_text_chunk_page', {
    current: wrappedChunk.value.index + 1,
    total: wrappedChunk.value.count,
  }),
)
function toggleWordWrap() {
  if (largeTextMode.value) {
    largeTextWrapEnabled.value = !largeTextWrapEnabled.value
    wrappedChunkIndex.value = 0
    return
  }

  wordWrap.value = !wordWrap.value
}

function showPreviousWrappedChunk() {
  wrappedChunkIndex.value = Math.max(wrappedChunk.value.index - 1, 0)
}

function showNextWrappedChunk() {
  wrappedChunkIndex.value = Math.min(
    wrappedChunk.value.index + 1,
    wrappedChunk.value.count - 1,
  )
}

function resetLargeTextWrap() {
  largeTextWrapEnabled.value = false
  wrappedChunkIndex.value = 0
}

watch(
  () => props.value,
  (nextValue, previousValue) => {
    if (!requiresMonacoLargeTextOptimizations(nextValue)) {
      resetLargeTextWrap()
      return
    }
    if (!largeTextWrapEnabled.value) {
      wrappedChunkIndex.value = 0
      return
    }
    if (!nextValue.startsWith(previousValue)) {
      resetLargeTextWrap()
      return
    }

    const previousChunk = getMonacoWrappedTextChunk(previousValue, wrappedChunkIndex.value)
    const nextChunk = getMonacoWrappedTextChunk(nextValue, wrappedChunkIndex.value)
    wrappedChunkIndex.value =
      previousChunk.index === previousChunk.count - 1
        ? nextChunk.count - 1
        : Math.min(previousChunk.index, nextChunk.count - 1)
  },
)

async function copyRawHTTPMessage() {
  if (!props.value) {
    return
  }
  try {
    await copyText(props.value)
    notify.success(t('detail.raw_http_copied'))
  } catch (error) {
    notify.error(t('detail.raw_http_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div
    class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
    :aria-busy="props.waiting ? 'true' : 'false'"
  >
    <UEmpty
      v-if="props.waiting"
      icon="i-lucide-clock-3"
      :title="t('detail.raw_http_waiting')"
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />
    <template v-else>
      <div class="flex min-h-8.5 shrink-0 items-center justify-end gap-1 px-2.5 pt-1">
        <UTooltip :text="t('detail.wrap_body')">
          <UButton
            icon="i-lucide-corner-down-left"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('detail.wrap_body')"
            :aria-pressed="effectiveWordWrap"
            @click="toggleWordWrap"
          />
        </UTooltip>
        <template v-if="showWrappedChunkPagination">
          <UTooltip :text="t('detail.large_text_previous_chunk')">
            <span class="inline-flex">
              <UButton
                icon="i-lucide-chevron-left"
                color="neutral"
                variant="ghost"
                size="sm"
                square
                :disabled="wrappedChunk.index === 0"
                :aria-label="t('detail.large_text_previous_chunk')"
                @click="showPreviousWrappedChunk"
              />
            </span>
          </UTooltip>
          <span
            class="min-w-10 shrink-0 px-0.5 text-center text-xs tabular-nums text-muted"
            :aria-label="wrappedChunkPageLabel"
            aria-live="polite"
            role="status"
          >
            {{ wrappedChunkPageLabel }}
          </span>
          <UTooltip :text="t('detail.large_text_next_chunk')">
            <span class="inline-flex">
              <UButton
                icon="i-lucide-chevron-right"
                color="neutral"
                variant="ghost"
                size="sm"
                square
                :disabled="wrappedChunk.index === wrappedChunk.count - 1"
                :aria-label="t('detail.large_text_next_chunk')"
                @click="showNextWrappedChunk"
              />
            </span>
          </UTooltip>
        </template>
        <UTooltip :text="t('detail.copy_raw_http')">
          <UButton
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :disabled="!props.value"
            :aria-label="t('detail.copy_raw_http')"
            @click="copyRawHTTPMessage"
          />
        </UTooltip>
      </div>
      <UAlert
        v-if="props.warningMessage"
        icon="i-lucide-triangle-alert"
        color="warning"
        variant="soft"
        :description="props.warningMessage"
        class="mx-2.5 mb-2"
      />
      <div class="flex min-h-0 w-full min-w-0 flex-1 pl-2.5 pb-2.5">
        <MonacoBodyEditor
          :value="editorValue"
          language="http"
          readonly
          :word-wrap="effectiveWordWrap"
          :allow-large-text-word-wrap="usesWrappedChunk"
          follow-tail-on-append
        />
      </div>
    </template>
  </div>
</template>
