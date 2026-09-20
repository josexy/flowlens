<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import { copyText } from '@/utils/clipboard'
import { formatHeaderFieldsAsText } from '@/utils/headers'
import type { ResponseCookie } from '@/utils/cookies'
import { useNotify } from '@/composables/useNotify'

const props = defineProps<{ cookies: ResponseCookie[] }>()
const { t } = useI18n()
const notify = useNotify()
const openedCookie = shallowRef<ResponseCookie | null>(null)

watch(
  () => props.cookies,
  () => {
    openedCookie.value = null
  },
)

const popoverContent = {
  align: 'end' as const,
  side: 'bottom' as const,
  collisionPadding: 12,
  hideWhenDetached: true,
}
const popoverUi = {
  content:
    'flex max-h-(--reka-popover-content-available-height) w-96 max-w-[calc(100vw-1.5rem)] flex-col overflow-hidden bg-app-panel text-app-text',
}

function setOpen(cookie: ResponseCookie, open: boolean) {
  if (open) {
    openedCookie.value = cookie
  } else if (openedCookie.value === cookie) {
    openedCookie.value = null
  }
}

async function copyCookieHeader(cookie: ResponseCookie) {
  try {
    await copyText(formatHeaderFieldsAsText([cookie.header]))
    notify.success(t('detail.set_cookie_header_copied'))
  } catch (error) {
    notify.error(t('detail.cookie_header_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <HeadersTable :fields="props.cookies">
    <template #value-trailing="{ index }">
      <UPopover
        v-if="props.cookies[index]"
        :open="openedCookie === props.cookies[index]"
        :content="{
          ...popoverContent,
          'aria-label': t('detail.cookie_view_attributes', {
            name: props.cookies[index].name,
            count: props.cookies[index].attributes.length,
          }),
        }"
        :ui="popoverUi"
        @update:open="setOpen(props.cookies[index]!, $event)"
      >
        <UTooltip :text="t('detail.cookie_attributes')">
          <UButton
            color="neutral"
            variant="ghost"
            size="xs"
            trailing-icon="i-lucide-chevron-down"
            :label="String(props.cookies[index].attributes.length)"
            :aria-label="
              t('detail.cookie_view_attributes', {
                name: props.cookies[index].name,
                count: props.cookies[index].attributes.length,
              })
            "
            class="group shrink-0 tabular-nums"
            :ui="{ trailingIcon: 'size-3 transition-transform group-data-[state=open]:rotate-180' }"
          />
        </UTooltip>
        <template #content>
          <div
            class="flex shrink-0 items-center justify-between gap-2 border-b border-app-border px-3 py-2.5"
          >
            <span class="min-w-0 text-sm font-medium break-all">{{
              props.cookies[index].name || t('detail.cookie_unnamed')
            }}</span>
            <span class="shrink-0 text-xs text-app-text-muted">{{
              t('detail.cookie_attribute_count', { count: props.cookies[index].attributes.length })
            }}</span>
          </div>
          <div class="max-h-80 min-h-0 overflow-y-auto p-3">
            <dl
              v-if="props.cookies[index].attributes.length"
              class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-x-3 gap-y-2 text-xs select-text"
            >
              <template
                v-for="(attribute, attributeIndex) in props.cookies[index].attributes"
                :key="attributeIndex"
              >
                <dt class="text-app-text-muted break-all">
                  {{ attribute.name || t('detail.cookie_unnamed') }}
                </dt>
                <dd class="min-w-0 font-mono whitespace-pre-wrap break-all">
                  <template v-if="attribute.hasEquals">
                    <span v-if="attribute.value">{{ attribute.value }}</span>
                    <span v-else class="font-sans text-app-text-muted">{{
                      t('detail.cookie_empty_value')
                    }}</span>
                  </template>
                  <span v-else class="inline-flex align-middle">
                    <UIcon name="i-lucide-check" class="size-3.5 text-app-text" aria-hidden="true" />
                  </span>
                </dd>
              </template>
            </dl>
            <p v-else class="text-xs text-app-text-muted">
              {{ t('detail.cookie_no_attributes') }}
            </p>
            <div class="mt-3 border-t border-app-border pt-2">
              <div class="mb-1 flex items-center justify-between gap-2">
                <span class="text-xs text-app-text-muted">{{ t('detail.cookie_raw_header') }}</span>
                <UTooltip :text="t('detail.copy_set_cookie_header')">
                  <UButton
                    icon="i-lucide-copy"
                    color="neutral"
                    variant="ghost"
                    size="xs"
                    square
                    :aria-label="t('detail.copy_set_cookie_header')"
                    @click="copyCookieHeader(props.cookies[index]!)"
                  />
                </UTooltip>
              </div>
              <pre class="font-mono text-xs whitespace-pre-wrap break-all select-text">{{
                formatHeaderFieldsAsText([props.cookies[index].header])
              }}</pre>
            </div>
          </div>
        </template>
      </UPopover>
    </template>
  </HeadersTable>
</template>
