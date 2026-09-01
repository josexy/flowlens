<script setup lang="ts">
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'
import type { HeaderField } from '@/utils/headers'
import HeadersTable from '../HeadersTable.vue'

const props = withDefaults(
  defineProps<{
    title: string
    fields?: HeaderField[]
    headers?: Record<string, string[]>
    hasHeaders?: boolean
    emptyTitle?: string
    warningMessage?: string
    copyJsonLabel: string
    sortLabel: string
    copyTextLabel: string
  }>(),
  {
    hasHeaders: true,
    emptyTitle: '',
    warningMessage: '',
  },
)

const emit = defineEmits<{
  copyJson: []
  sort: []
  copyText: []
}>()
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden">
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
        <span class="text-sm text-app-text-muted">
          {{ props.title }}
        </span>
        <div class="flex gap-1">
          <UTooltip :text="props.copyJsonLabel">
            <UButton
              icon="i-lucide-code"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="props.copyJsonLabel"
              @click="emit('copyJson')"
            />
          </UTooltip>
          <UTooltip :text="props.sortLabel">
            <UButton
              icon="i-lucide-arrow-up-down"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="props.sortLabel"
              @click="emit('sort')"
            />
          </UTooltip>
          <UTooltip :text="props.copyTextLabel">
            <UButton
              icon="i-lucide-copy"
              color="neutral"
              variant="ghost"
              size="sm"
              square
              :aria-label="props.copyTextLabel"
              @click="emit('copyText')"
            />
          </UTooltip>
        </div>
      </div>
      <UAlert
        v-if="props.warningMessage"
        icon="i-lucide-triangle-alert"
        color="warning"
        variant="soft"
        :description="props.warningMessage"
        class="mx-2.5 mb-2"
      />
      <div class="relative min-h-0 flex-1">
        <div class="h-full min-h-0 overflow-y-auto px-2.5 pb-2.5">
          <HeadersTable
            v-if="props.hasHeaders"
            :fields="props.fields"
            :headers="props.headers"
          />
          <div v-else class="flex min-h-full items-center justify-center">
            <UEmpty
              icon="i-lucide-rows-3"
              :title="props.emptyTitle"
              :size="appEmptyStateSize"
              variant="naked"
              :ui="appEmptyStateUi"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
