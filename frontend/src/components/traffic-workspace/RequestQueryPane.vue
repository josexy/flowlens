<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import EditableKeyValueTable from '@/components/traffic-workspace/EditableKeyValueTable.vue'
import { useNotify } from '@/composables/useNotify'
import type { EditableKeyValue } from '@/types/request-editor'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import {
  createEmptyRequestQueryRow,
  serializeRequestQueryRows,
} from '@/utils/requestQuery'

const params = defineModel<EditableKeyValue[]>('params', { required: true })
const { t } = useI18n()
const notify = useNotify()

function addQueryParameter() {
  params.value.push(createEmptyRequestQueryRow())
}

function clearQueryParameters() {
  params.value.splice(0, params.value.length, createEmptyRequestQueryRow())
  notify.success(t('workspace.http_request.query_parameters_cleared'))
}

async function copyQuery() {
  try {
    await copyTextToClipboard(serializeRequestQueryRows(params.value))
    notify.success(t('workspace.http_request.query_copied'))
  } catch (error) {
    notify.error(t('workspace.http_request.query_copy_failed', { error: String(error) }))
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between px-2.5 pt-2.5 pb-2">
      <span class="text-sm text-app-text-muted">
        {{ t('workspace.http_request.query_list') }}
      </span>
      <div class="flex gap-1">
        <UTooltip :text="t('workspace.http_request.copy_query')">
          <UButton
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.copy_query')"
            @click="copyQuery"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.clear_query_parameters')">
          <UButton
            icon="i-lucide-trash-2"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.clear_query_parameters')"
            @click="clearQueryParameters"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.add_query_parameter')">
          <UButton
            icon="i-lucide-plus"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.add_query_parameter')"
            @click="addQueryParameter"
          />
        </UTooltip>
      </div>
    </div>
    <EditableKeyValueTable
      v-model="params"
      :key-placeholder="t('workspace.http_request.query_key_placeholder')"
      :value-placeholder="t('workspace.http_request.query_value_placeholder')"
      :show-duplicate-warning="false"
    />
  </div>
</template>
