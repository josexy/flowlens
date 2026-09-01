<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CancelError } from '@wailsio/runtime'
import { ResendRequest as ResendProxyRequest } from '#bindings/github.com/josexy/flowlens/backend/services/proxy_service/proxyservice'
import { ResendRequest as ResendHistoryRequest } from '#bindings/github.com/josexy/flowlens/backend/services/history_service/historyservice'
import { useNotify } from '@/composables/useNotify'

const props = defineProps<{
  show: boolean
  entryId: number
  historyKey?: string | null
}>()

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
}>()

const { t } = useI18n()
const notify = useNotify()

const delayMs = ref(0)
const intervalMs = ref(1000)
const count = ref(3)
const useProxy = ref(true)
const upstreamProxy = ref('')
const sending = ref(false)
const cancelled = ref(false)
let pendingResendCall:
  | ReturnType<typeof ResendProxyRequest>
  | ReturnType<typeof ResendHistoryRequest>
  | null = null
const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

watch(
  () => props.show,
  (val) => {
    if (val) {
      if (pendingResendCall) {
        return
      }
      delayMs.value = 0
      intervalMs.value = 1000
      count.value = 3
      useProxy.value = true
      upstreamProxy.value = ''
      sending.value = false
      cancelled.value = false
    } else if (pendingResendCall) {
      void pendingResendCall.cancel()
    }
  },
)

async function handleConfirm() {
  if (sending.value) {
    return
  }
  sending.value = true
  cancelled.value = false
  const cfg = {
    delayMs: delayMs.value,
    intervalMs: intervalMs.value,
    count: count.value,
    useProxy: useProxy.value,
    upstreamProxy: useProxy.value ? '' : upstreamProxy.value,
  }
  const resendCall = props.historyKey
    ? ResendHistoryRequest(props.historyKey, props.entryId, cfg)
    : ResendProxyRequest(props.entryId, cfg)
  pendingResendCall = resendCall
  try {
    const result = await resendCall
    if (pendingResendCall !== resendCall) {
      return
    }
    notify.success(t('resend_modal.success', { success: result.success, failed: result.failed }))
    emit('update:show', false)
  } catch (err: unknown) {
    if (pendingResendCall !== resendCall) {
      return
    }
    if (err instanceof CancelError) {
      cancelled.value = true
      return
    }
    notify.error(t('resend_modal.error', { error: String(err) }))
  } finally {
    if (pendingResendCall === resendCall) {
      pendingResendCall = null
      sending.value = false
    }
  }
}

async function handleStop() {
  const resendCall = pendingResendCall
  if (!resendCall) {
    return
  }
  try {
    await resendCall.cancel()
  } catch (error) {
    console.error('Failed to stop resend operation:', error)
  }
}

function handlePrimaryAction() {
  return sending.value ? handleStop() : handleConfirm()
}

function handleCancel() {
  emit('update:show', false)
}

onBeforeUnmount(() => {
  void pendingResendCall?.cancel()
})
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="t('resend_modal.title')"
    :close="!sending"
    :dismissible="!sending"
  >
    <template #body>
      <div class="flex flex-col gap-3" :class="{ 'opacity-[0.72]': sending }">
        <label class="grid min-w-0 grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5 max-[520px]:grid-cols-1">
          <span class="text-sm font-semibold text-app-text-secondary">{{ t('resend_modal.count') }}</span>
          <UInputNumber v-model="count" orientation="vertical" :min="1" :max="1000" :disabled="sending" class="w-full" />
        </label>
        <label class="grid min-w-0 grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5 max-[520px]:grid-cols-1">
          <span class="text-sm font-semibold text-app-text-secondary">{{ t('resend_modal.delay') }}</span>
          <UInputNumber v-model="delayMs" orientation="vertical" :min="0" :step="100" :disabled="sending" class="w-full" />
        </label>
        <label class="grid min-w-0 grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5 max-[520px]:grid-cols-1">
          <span class="text-sm font-semibold text-app-text-secondary">{{ t('resend_modal.interval') }}</span>
          <UInputNumber v-model="intervalMs" orientation="vertical" :min="0" :step="100" :disabled="sending" class="w-full" />
        </label>
        <label class="grid min-w-0 grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5 max-[520px]:grid-cols-1">
          <span class="text-sm font-semibold text-app-text-secondary">{{ t('resend_modal.use_proxy') }}</span>
          <USwitch v-model="useProxy" :disabled="sending" />
        </label>
        <label v-if="!useProxy" class="grid min-w-0 grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5 max-[520px]:grid-cols-1">
          <span class="text-sm font-semibold text-app-text-secondary">{{ t('resend_modal.upstream_proxy') }}</span>
          <UInput
            v-model="upstreamProxy"
            :disabled="sending"
            :placeholder="t('resend_modal.upstream_proxy_placeholder')"
            class="w-full"
          />
        </label>
        <UAlert
          v-if="cancelled"
          color="neutral"
          variant="subtle"
          icon="i-lucide-circle-stop"
          :title="t('resend_modal.cancelled')"
        />
      </div>
    </template>

    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton
          color="neutral"
          variant="outline"
          :disabled="sending"
          :label="t('resend_modal.cancel')"
          @click="handleCancel"
        />
        <UButton
          :color="sending ? 'error' : 'primary'"
          :icon="sending ? 'i-lucide-square' : undefined"
          :label="sending ? t('resend_modal.stop') : t('resend_modal.confirm')"
          @click="handlePrimaryAction"
        />
      </div>
    </template>
  </UModal>
</template>
