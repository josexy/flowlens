<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'

const props = withDefaults(
  defineProps<{
    value: string
    warningMessage?: string
    waiting?: boolean
    bodyUnavailable?: boolean
    copyDisabled?: boolean
  }>(),
  {
    warningMessage: '',
    waiting: false,
    bodyUnavailable: false,
    copyDisabled: false,
  },
)

const { t } = useI18n()
const notify = useNotify()
const wordWrap = ref(false)

function toggleWordWrap() {
  wordWrap.value = !wordWrap.value
}

async function copyRawHTTPMessage() {
  if (!props.value || props.bodyUnavailable || props.copyDisabled) {
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
            :aria-pressed="wordWrap"
            @click="toggleWordWrap"
          />
        </UTooltip>
        <UTooltip :text="t('detail.copy_raw_http')">
          <UButton
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :disabled="!props.value || props.bodyUnavailable || props.copyDisabled"
            :aria-label="t('detail.copy_raw_http')"
            @click="copyRawHTTPMessage"
          />
        </UTooltip>
      </div>
      <UAlert
        v-if="props.bodyUnavailable"
        icon="i-lucide-triangle-alert"
        color="warning"
        variant="soft"
        :description="t('detail.body_unavailable')"
        class="mx-2.5 mb-2"
      />
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
          :value="props.value"
          language="http"
          readonly
          :word-wrap="wordWrap"
          follow-tail-on-append
        />
      </div>
    </template>
  </div>
</template>
