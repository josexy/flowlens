<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Events } from '@wailsio/runtime'
import { useRewriteRulesStore } from '@/stores/rewriteRules'
import { useWorkbenchStore } from '@/stores/workbench'
import {
  REWRITE_DRAFTS_DIRTY_CHANGED_EVENT,
  CONFIRM_REWRITE_QUIT_REQUEST_EVENT,
  REWRITE_QUIT_CONFIRMED_EVENT,
} from '@/runtime/appEvents'

const { t } = useI18n()
const store = useRewriteRulesStore()
const workbench = useWorkbenchStore()
const pending = shallowRef<{ requestId: string; reason: 'quit' | 'update' } | null>(null)
const deliveryError = shallowRef('')
const fieldError = computed(() => Object.values(store.selectedFieldErrors)[0])
let offQuit: (() => void) | null = null

watch(
  () => store.hasDirtyDrafts,
  (dirty) => {
    void Events.Emit(REWRITE_DRAFTS_DIRTY_CHANGED_EVENT, dirty).catch((cause) => {
      deliveryError.value = String(cause)
    })
  },
  { immediate: true, flush: 'sync' },
)

async function reply(decision: 'save' | 'discard' | 'cancel') {
  const request = pending.value
  if (!request || store.busy) return
  if (decision === 'save' && !(await store.saveAll())) {
    workbench.selectRewriteRulesItem()
    return
  }
  try {
    await Events.Emit(REWRITE_QUIT_CONFIRMED_EVENT, { ...request, decision })
    if (decision === 'discard') store.discardAll()
    pending.value = null
    deliveryError.value = ''
  } catch (cause) {
    deliveryError.value = String(cause)
  }
}

onMounted(() => {
  offQuit = Events.On(CONFIRM_REWRITE_QUIT_REQUEST_EVENT, (event) => {
    const data = event.data as { requestId?: unknown; reason?: unknown } | null
    if (typeof data?.requestId !== 'string' || (data.reason !== 'quit' && data.reason !== 'update'))
      return
    pending.value = { requestId: data.requestId, reason: data.reason }
    if (!store.hasDirtyDrafts) void reply('save')
  })
})
onBeforeUnmount(() => {
  offQuit?.()
  store.cleanup()
})
</script>

<template>
  <UModal
    :open="!!pending"
    :close="false"
    :dismissible="false"
    :title="t('rewrite_rules.quit_title')"
  >
    <template #body>
      <p class="text-sm">
        {{
          t(
            pending?.reason === 'update'
              ? 'rewrite_rules.update_dirty'
              : 'rewrite_rules.quit_dirty',
            { count: store.dirtyIds.length },
          )
        }}
      </p>
      <UAlert
        v-if="fieldError"
        color="error"
        variant="subtle"
        :description="t(fieldError)"
        class="mt-3"
      />
      <UAlert
        v-if="store.error || deliveryError"
        color="error"
        variant="subtle"
        :description="store.error || deliveryError"
        class="mt-3"
      />
      <UAlert
        v-if="store.selectedDraft?.conflict"
        color="warning"
        variant="subtle"
        :description="t('rewrite_rules.conflict')"
        class="mt-3"
      />
    </template>
    <template #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <UButton
          color="neutral"
          variant="outline"
          :label="t('rewrite_rules.cancel')"
          :disabled="store.busy"
          @click="reply('cancel')"
        />
        <UButton
          color="error"
          variant="outline"
          :label="t('rewrite_rules.discard_continue')"
          :disabled="store.busy"
          @click="reply('discard')"
        />
        <UButton
          :label="t('rewrite_rules.save_continue')"
          :loading="store.busy"
          @click="reply('save')"
        />
      </div>
    </template>
  </UModal>
</template>
