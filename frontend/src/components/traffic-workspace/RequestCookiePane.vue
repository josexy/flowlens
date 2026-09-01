<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EditableKeyValue } from '@/types/request-editor'
import EditableKeyValueTable from '@/components/traffic-workspace/EditableKeyValueTable.vue'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { formatHeadersAsText } from '@/utils/headers'
import { useNotify } from '@/composables/useNotify'
import {
  requestCookieHeaderSignature,
  requestCookieHeadersRecord,
  requestCookieRows,
  createEmptyCookieRow,
  replaceRequestCookieHeaders,
} from '@/utils/cookies'

const headers = defineModel<EditableKeyValue[]>('headers', { required: true })
const { t } = useI18n()
const notify = useNotify()

const cookieRows = ref<EditableKeyValue[]>(requestCookieRows(headers.value))
const rawCookieHeaderText = computed(() =>
  formatHeadersAsText(requestCookieHeadersRecord(headers.value)),
)
let syncingFromHeaders = false
let lastWrittenHeaderSignature: string | null = null

watch(
  cookieRows,
  (rows) => {
    if (syncingFromHeaders) {
      return
    }
    const nextHeaders = replaceRequestCookieHeaders(headers.value, rows)
    lastWrittenHeaderSignature = requestCookieHeaderSignature(nextHeaders)
    headers.value = nextHeaders
  },
  { deep: true },
)

watch(
  () => requestCookieHeaderSignature(headers.value),
  (signature) => {
    if (signature === lastWrittenHeaderSignature) {
      lastWrittenHeaderSignature = null
      return
    }

    syncingFromHeaders = true
    cookieRows.value = requestCookieRows(headers.value)
    void nextTick(() => {
      syncingFromHeaders = false
    })
  },
)

function addCookie() {
  cookieRows.value.push(createEmptyCookieRow())
}

function clearCookies() {
  cookieRows.value.splice(0, cookieRows.value.length, createEmptyCookieRow())
  notify.success(t('workspace.http_request.cookies_cleared'))
}

async function copyRawCookieHeaders() {
  try {
    await copyTextToClipboard(rawCookieHeaderText.value)
    notify.success(t('detail.cookie_header_copied'))
  } catch (error) {
    notify.error(t('detail.cookie_header_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
      <span class="text-sm text-app-text-muted">
        {{ t('workspace.http_request.cookie_list') }}
      </span>
      <div class="flex gap-1">
        <UTooltip :text="t('detail.copy_cookie_header')">
          <UButton
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('detail.copy_cookie_header')"
            @click="copyRawCookieHeaders"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.clear_cookies')">
          <UButton
            icon="i-lucide-trash-2"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.clear_cookies')"
            @click="clearCookies"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.add_cookie')">
          <UButton
            icon="i-lucide-plus"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.add_cookie')"
            @click="addCookie"
          />
        </UTooltip>
      </div>
    </div>
    <EditableKeyValueTable
      v-model="cookieRows"
      :key-placeholder="t('workspace.http_request.cookie_name_placeholder')"
      :value-placeholder="t('workspace.http_request.cookie_value_placeholder')"
      :show-duplicate-warning="false"
    />
  </div>
</template>
