<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import HeadersTable from '@/components/traffic/HeadersTable.vue'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'
import type { QueryParameterField } from '@/utils/urlHighlight'

const props = defineProps<{
  title: string
  fields: QueryParameterField[]
  rawQuery: string
  emptyTitle: string
}>()

const { t } = useI18n()
const notify = useNotify()
const hasParameters = computed(() => props.fields.length > 0)

async function copyRawQuery() {
  try {
    await copyText(props.rawQuery)
    notify.success(t('detail.query_copied'))
  } catch (error) {
    notify.error(t('detail.query_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
      <span class="text-sm text-app-text-muted">{{ props.title }}</span>
      <UTooltip :text="t('detail.copy_query')">
        <UButton
          icon="i-lucide-copy"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          :aria-label="t('detail.copy_query')"
          @click="copyRawQuery"
        />
      </UTooltip>
    </div>
    <div class="relative min-h-0 flex-1">
      <div class="h-full min-h-0 overflow-y-auto px-2.5 pb-2.5">
        <HeadersTable v-if="hasParameters" :fields="props.fields" />
        <div v-else class="flex min-h-full items-center justify-center">
          <UEmpty
            icon="i-lucide-list-filter"
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
