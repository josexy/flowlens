<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import ResponseCookieTable from '@/components/traffic/ResponseCookieTable.vue'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { formatHeaderFieldsAsText, headersRecordToFields } from '@/utils/headers'
import {
  cookieHeaderFields,
  type ResponseCookie,
  type CookieHeaderName,
  type NullableCookieHeaders,
} from '@/utils/cookies'
import { useNotify } from '@/composables/useNotify'

const props = defineProps<{
  title: string
  cookies?: Record<string, string[]>
  responseCookies?: ResponseCookie[]
  emptyTitle: string
  rawHeaders: NullableCookieHeaders
  headerName: CookieHeaderName
  warningMessage?: string
}>()

const { t } = useI18n()
const notify = useNotify()
const cookieRows = computed(() =>
  props.headerName === 'set-cookie'
    ? (props.responseCookies ?? [])
    : headersRecordToFields(props.cookies),
)
const rawCookieHeaders = computed(() => cookieHeaderFields(props.rawHeaders, props.headerName))
const rawCookieHeaderText = computed(() => formatHeaderFieldsAsText(rawCookieHeaders.value))
const copyLabel = computed(() =>
  props.headerName === 'cookie'
    ? t('detail.copy_cookie_header')
    : t('detail.copy_set_cookie_header'),
)
const copiedMessage = computed(() =>
  props.headerName === 'cookie'
    ? t('detail.cookie_header_copied')
    : t('detail.set_cookie_header_copied'),
)

async function copyRawCookieHeaders() {
  try {
    await copyTextToClipboard(rawCookieHeaderText.value)
    notify.success(copiedMessage.value)
  } catch (error) {
    notify.error(t('detail.cookie_header_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
      <span class="text-sm text-app-text-muted">
        {{ props.title }}
      </span>
      <UTooltip :text="copyLabel">
        <UButton
          icon="i-lucide-copy"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          :aria-label="copyLabel"
          @click="copyRawCookieHeaders"
        />
      </UTooltip>
    </div>
    <UAlert
      v-if="props.warningMessage"
      icon="i-lucide-triangle-alert"
      color="warning"
      variant="soft"
      :description="props.warningMessage"
      class="mx-2.5 mb-2 shrink-0"
    />
    <div class="relative min-h-0 flex-1">
      <div class="h-full min-h-0 overflow-y-auto px-2.5 pb-2.5">
        <ResponseCookieTable
          v-if="cookieRows.length && props.headerName === 'set-cookie'"
          :cookies="props.responseCookies ?? []"
        />
        <HeadersTable v-else-if="cookieRows.length" :fields="cookieRows" />
        <div v-else class="flex min-h-full items-center justify-center">
          <UEmpty
            icon="i-lucide-cookie"
            :title="props.emptyTitle"
            :size="appEmptyStateSize"
            variant="naked"
            :ui="appEmptyStateUi"
          />
        </div>
      </div>
    </div>
  </div>
</template>
