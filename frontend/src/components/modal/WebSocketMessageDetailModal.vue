<script setup lang="ts">
import { copyText } from '@/utils/clipboard'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import { SaveBodyToFile } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import type { WebSocketDisplayMessage } from '@/types/websocket'
import HexDumpViewer from '@/components/common/HexDumpViewer.vue'
import AppTooltip from '@/components/common/AppTooltip.vue'
import { useNotify } from '@/composables/useNotify'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'

const props = defineProps<{
  show: boolean
  message: WebSocketDisplayMessage | null
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
}>()

const { t } = useI18n()
const notification = useNotify()
const wrapText = ref(false)
const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

function toggleWrapText() {
  wrapText.value = !wrapText.value
}

const title = computed(() => {
  if (!props.message) {
    return t('workspace.websocket_client.message_detail')
  }
  return props.message.direction === 'send'
    ? t('workspace.websocket_client.message_detail_send')
    : t('workspace.websocket_client.message_detail_receive')
})

const textContent = computed(() => {
  if (!props.show || !props.message || props.message.msgType !== 'text') {
    return ''
  }
  return props.message.data
})

const isTextMessage = computed(() => props.message?.msgType === 'text')
const isBinaryMessage = computed(() => props.message?.msgType === 'binary')

async function copyTextMessage() {
  if (!props.message || props.message.msgType !== 'text') {
    return
  }

  try {
    await copyText(textContent.value)
    notification.success(t('detail.body_copied'))
  } catch (error) {
    notification.error(t('detail.body_copy_failed', { error: getErrorMessage(error) }))
  }
}

async function exportMessage() {
  if (!props.message) {
    return
  }

  try {
    const isBinary = props.message.msgType === 'binary'
    const filename = isBinary ? 'websocket-message.bin' : 'websocket-message.txt'
    const selectedPath = await Dialogs.SaveFile({
      Filename: filename,
    })
    const savePath = selectedPath.trim()
    if (!savePath) {
      return
    }
    await SaveBodyToFile({
      path: savePath,
      body: props.message.data,
      bodyEncoding: isBinary ? 'base64' : '',
      contentType: isBinary ? 'application/octet-stream' : 'text/plain',
    })
  } catch (error) {
    if (isDialogCancelError(error)) {
      return
    }
    notification.error(t('detail.body_save_failed', { error: getErrorMessage(error) }))
  }
}
</script>

<template>
  <UModal v-model:open="visible" :title="title" :ui="{ content: 'max-w-[min(960px,92vw)]' }">
    <template #body>
      <div v-if="!props.message" class="flex min-h-45 items-center justify-center">
        <div class="text-sm text-app-text-muted" role="status">
          {{ t('common.no_content') }}
        </div>
      </div>

      <template v-else>
        <div class="mb-3 flex items-center justify-between gap-3">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <UBadge
              class="px-2 py-0.75 text-sm font-semibold"
              :color="props.message.direction === 'send' ? 'warning' : 'info'"
            >
              {{
                props.message.direction === 'send'
                  ? t('workspace.websocket_client.filter_send')
                  : t('workspace.websocket_client.filter_receive')
              }}
            </UBadge>
            <UBadge class="px-2 py-0.75 text-sm font-semibold" color="neutral">
              {{
                props.message.msgType === 'binary'
                  ? t('workspace.websocket_client.draft_type_binary_file')
                  : t('workspace.websocket_client.draft_type_text')
              }}
            </UBadge>
            <span class="text-sm text-app-text-muted">{{
              t('workspace.websocket_client.binary_size', { size: props.message.dataSize })
            }}</span>
          </div>

          <div class="flex shrink-0 items-center gap-2">
            <AppTooltip v-if="isTextMessage" :text="t('detail.wrap_body')">
              <template #trigger>
                <UButton
                  size="sm"
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-corner-down-left"
                  :aria-label="t('detail.wrap_body')"
                  @click="toggleWrapText"
                />
              </template>
            </AppTooltip>
            <AppTooltip v-if="isTextMessage" :text="t('detail.copy_body')">
              <template #trigger>
                <UButton
                  size="sm"
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-copy"
                  :aria-label="t('detail.copy_body')"
                  @click="copyTextMessage"
                />
              </template>
            </AppTooltip>
            <AppTooltip v-if="isTextMessage || isBinaryMessage" :text="t('detail.save_body')">
              <template #trigger>
                <UButton
                  size="sm"
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-download"
                  :aria-label="t('detail.save_body')"
                  @click="exportMessage"
                />
              </template>
            </AppTooltip>
          </div>
        </div>

        <div class="h-[min(70vh,720px)] overflow-hidden rounded-lg border border-app-border bg-app-elevated">
          <div class="h-full min-h-0">
            <textarea
              v-if="props.message.msgType === 'text'"
              class="text-detail-input size-full resize-none overflow-auto border-none bg-transparent p-3 text-sm leading-[1.6] text-app-text outline-none"
              style="font-family: var(--app-font-family)"
              :class="wrapText ? 'text-detail-input--wrapped whitespace-pre-wrap wrap-break-word' : 'whitespace-pre'"
              :value="textContent"
              readonly
              spellcheck="false"
              :wrap="wrapText ? 'soft' : 'off'"
            />
            <HexDumpViewer
              v-else
              :input="props.message.data"
              is-base64
              :row-height="24"
              :show-info-bar="false"
            />
          </div>
        </div>
      </template>
    </template>
  </UModal>
</template>
