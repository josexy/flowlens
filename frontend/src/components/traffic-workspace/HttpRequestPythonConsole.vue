<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { SaveBodyToFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type { PluginLogEntry } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import { copyText as copyTextToClipboard } from '@/utils/clipboard'
import { formatUnixMicrosLocal } from '@/utils/format'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'
import { useNotify } from '@/composables/useNotify'
import { appEmptyStateSize, appEmptyStateUi } from '@/components/common/emptyState'

const props = defineProps<{
  entries: PluginLogEntry[]
  running: boolean
}>()

const emit = defineEmits<{
  clear: []
}>()

const { t } = useI18n()
const notify = useNotify()
const wordWrap = ref(true)

const consoleEditorOptions = {
  renderLineHighlight: 'none',
  overviewRulerLanes: 0,
  hideCursorInOverviewRuler: true,
  bracketPairColorization: { enabled: false },
  matchBrackets: 'never',
  wrappingIndent: 'none',
  padding: { top: 6, bottom: 6 },
} as const

const consoleText = computed(() =>
  props.entries
    .map((entry) => {
      const owner = pluginLabel(entry)
      const stream = entry.stream || entry.level || 'log'
      return `[${formatUnixMicrosLocal(entry.timestamp)}] [${owner}] [${stream}] ${entry.message}`
    })
    .join('\n'),
)

function pluginLabel(entry: PluginLogEntry) {
  return entry.pluginId === 'current-request-script'
    ? t('workspace.http_request.inline_script_name')
    : entry.pluginId || t('workspace.http_request.console_unknown_plugin')
}

async function copyAll() {
  if (!consoleText.value) {
    return
  }
  try {
    await copyTextToClipboard(consoleText.value)
    notify.success(t('workspace.http_request.console_copied'))
  } catch (error) {
    notify.error(
      t('workspace.http_request.console_copy_failed', { error: getErrorMessage(error) }),
    )
  }
}

async function saveAll() {
  if (!consoleText.value) {
    return
  }
  try {
    const selectedPath = await Dialogs.SaveFile({
      Filename: 'flowlens-python-console.log',
    })
    const savePath = selectedPath.trim()
    if (!savePath) {
      return
    }
    await SaveBodyToFile({
      path: savePath,
      body: consoleText.value,
      bodyEncoding: '',
      contentType: 'text/plain; charset=utf-8',
    })
  } catch (error) {
    if (isDialogCancelError(error)) {
      return
    }
    notify.error(
      t('workspace.http_request.console_save_failed', { error: getErrorMessage(error) }),
    )
  }
}

function clear() {
  emit('clear')
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden" role="tabpanel">
    <div class="flex shrink-0 items-center justify-between gap-2 px-2.5 py-2">
      <div class="flex min-w-0 items-center gap-2 text-sm text-muted">
        <span
          class="size-2 shrink-0 rounded-full"
          :class="running ? 'animate-pulse bg-primary' : 'bg-muted'"
          aria-hidden="true"
        />
        <span>
          {{
            running
              ? t('workspace.http_request.console_running')
              : t('workspace.http_request.console_entries', { count: entries.length })
          }}
        </span>
      </div>
      <div class="flex shrink-0 items-center gap-1">
        <UTooltip :text="t('workspace.http_request.console_wrap')">
          <UButton
            icon="i-lucide-corner-down-left"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :aria-label="t('workspace.http_request.console_wrap')"
            :aria-pressed="wordWrap"
            @click="wordWrap = !wordWrap"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.console_copy_all')">
          <UButton
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :disabled="entries.length === 0"
            :aria-label="t('workspace.http_request.console_copy_all')"
            @click="copyAll"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.console_save')">
          <UButton
            icon="i-lucide-download"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :disabled="entries.length === 0"
            :aria-label="t('workspace.http_request.console_save')"
            @click="saveAll"
          />
        </UTooltip>
        <UTooltip :text="t('workspace.http_request.console_clear')">
          <UButton
            icon="i-lucide-trash-2"
            color="neutral"
            variant="ghost"
            size="sm"
            square
            :disabled="entries.length === 0"
            :aria-label="t('workspace.http_request.console_clear')"
            @click="clear"
          />
        </UTooltip>
      </div>
    </div>

    <UEmpty
      v-if="entries.length === 0"
      icon="i-lucide-terminal"
      :title="
        running
          ? t('workspace.http_request.console_waiting_output')
          : t('workspace.http_request.console_empty')
      "
      :size="appEmptyStateSize"
      variant="naked"
      :ui="appEmptyStateUi"
    />
    <div
      v-else
      class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden pb-2.5 pl-2.5"
    >
      <MonacoBodyEditor
        :value="consoleText"
        class="min-h-0 flex-1"
        language="plaintext"
        readonly
        :word-wrap="wordWrap"
        follow-tail-on-append
        :options="consoleEditorOptions"
      />
    </div>
  </div>
</template>
