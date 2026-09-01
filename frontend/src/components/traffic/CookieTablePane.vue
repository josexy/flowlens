<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { formatHeadersAsText } from '@/utils/headers'
import {
  cookieHeadersRecord,
  type CookieHeaderName,
  type NullableCookieHeaders,
} from '@/utils/cookies'
import { useNotify } from '@/composables/useNotify'

const props = defineProps<{
  title: string
  cookies: Record<string, string[]>
  emptyTitle: string
  rawHeaders: NullableCookieHeaders
  headerName: CookieHeaderName
}>()

const { t } = useI18n()
const notify = useNotify()
const hasCookies = computed(() => Object.keys(props.cookies).length > 0)
const rawCookieHeaders = computed(() => cookieHeadersRecord(props.rawHeaders, props.headerName))
const rawCookieHeaderText = computed(() => formatHeadersAsText(rawCookieHeaders.value))
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
    <div class="relative min-h-0 flex-1">
      <div class="h-full min-h-0 overflow-y-auto px-2.5 pb-2.5">
        <HeadersTable v-if="hasCookies" :headers="props.cookies" />
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
