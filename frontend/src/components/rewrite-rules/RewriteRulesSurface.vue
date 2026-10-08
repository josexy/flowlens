<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRewriteRulesStore } from '@/stores/rewriteRules'
import { useWorkbenchStore } from '@/stores/workbench'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'
import RewriteRuleForm from './RewriteRuleForm.vue'

const { t } = useI18n()
const store = useRewriteRulesStore()
const workbench = useWorkbenchStore()
onMounted(() => void store.initialize())
const saveKbds = useShortcutKbds('app.save')
const deleteOpen = shallowRef(false)
const deleteId = shallowRef('')
const deleteName = shallowRef('')
const deleteDirty = shallowRef(false)
const deleteFieldError = computed(() => Object.values(store.fieldErrors(deleteId.value))[0])
const saving = computed(
  () => store.pendingMutation?.kind === 'save' && store.pendingMutation.id === store.selectedId,
)
const canRevert = computed(
  () => !!store.selectedId && (store.isDirty(store.selectedId) || !!store.selectedDraft?.conflict),
)
const canSave = computed(
  () =>
    !!store.selectedId &&
    store.isDirty(store.selectedId) &&
    !store.selectedDraft?.conflict &&
    !store.selectedRule?.unavailableReason,
)
function save() {
  return store.save()
}
function openDelete() {
  deleteId.value = store.selectedId
  deleteName.value = store.selectedRule?.name ?? ''
  deleteDirty.value = store.isDirty(deleteId.value)
  deleteOpen.value = true
}
async function remove(saveFirst: boolean) {
  let id = deleteId.value
  if (saveFirst) {
    const result = await store.save(id)
    if (!result.saved) return
    // Retain the saved identity even if refresh changed the active selection.
    id = result.ruleId
  }
  if (await store.remove(id)) deleteOpen.value = false
}
const offSave = registerShortcutHandler({
  commandId: 'app.save',
  when: () => workbench.activeContent === 'rewriteRules',
  enabled: () => !store.busy && !!store.selectedRule && !store.selectedDraft?.conflict,
  run: async () => {
    await save()
  },
})
onBeforeUnmount(offSave)
</script>

<template>
  <div class="flex h-full min-w-0 flex-col overflow-hidden bg-default">
    <div class="flex h-12 shrink-0 items-center gap-2 border-b border-default px-3">
      <UIcon name="i-lucide-repeat-2" class="size-4 text-primary" />
      <h2 class="min-w-0 flex-1 truncate text-sm font-semibold text-highlighted">
        {{ store.selectedRule?.name || t('rewrite_rules.title') }}
      </h2>
      <UBadge
        v-if="store.selectedId && store.isDirty(store.selectedId)"
        color="warning"
        variant="subtle"
        >{{ t('rewrite_rules.unsaved') }}</UBadge
      >
      <UButton
        color="neutral"
        variant="ghost"
        :label="t('rewrite_rules.revert')"
        :disabled="store.busy || !canRevert"
        :ui="{ base: canRevert ? 'disabled:opacity-100' : undefined }"
        @click="store.revert()"
      />
      <UTooltip :text="t('rewrite_rules.save')" :kbds="saveKbds"
        ><UButton
          icon="i-lucide-save"
          :label="t('rewrite_rules.save')"
          :loading="saving"
          :disabled="store.busy || !canSave"
          :ui="{ base: canSave ? 'disabled:opacity-100' : undefined }"
          @click="save"
      /></UTooltip>
      <UButton
        icon="i-lucide-trash-2"
        color="error"
        variant="ghost"
        :aria-label="t('rewrite_rules.delete')"
        :disabled="store.busy || !store.selectedRule"
        :ui="{ base: store.selectedRule ? 'disabled:opacity-100' : undefined }"
        @click="openDelete"
      />
    </div>
    <UAlert
      v-if="store.error"
      color="error"
      variant="subtle"
      :description="store.error"
      class="shrink-0 rounded-none"
    />
    <div
      v-if="store.selectedDraft?.conflict"
      class="shrink-0 space-y-2 border-b border-warning/30 bg-warning/10 p-3"
    >
      <p class="text-sm text-warning">{{ t('rewrite_rules.conflict') }}</p>
      <div class="flex gap-2">
        <UButton
          size="xs"
          color="warning"
          variant="outline"
          :label="t('rewrite_rules.keep_draft')"
          :disabled="store.busy"
          @click="store.acceptConflict()"
        /><UButton
          size="xs"
          color="neutral"
          variant="outline"
          :label="t('rewrite_rules.load_saved')"
          :disabled="store.busy"
          @click="store.revert()"
        />
      </div>
    </div>
    <UAlert
      v-if="store.selectedRule?.unavailableReason"
      color="error"
      variant="subtle"
      :description="store.selectedRule.unavailableReason"
      class="shrink-0 rounded-none"
    />
    <div v-if="store.selectedRule" class="min-h-0 flex-1 overflow-y-auto">
      <RewriteRuleForm
        :key="store.selectedDraft?.editorKey"
        :rule="store.selectedRule"
        :errors="store.selectedFieldErrors"
        :validate="() => store.validate()"
        :disabled="!!store.selectedRule.unavailableReason"
        @update="store.update"
      />
    </div>
    <div
      v-else-if="store.loading"
      role="status"
      class="flex flex-1 items-center justify-center gap-2 text-sm text-muted"
    >
      <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />
      {{ t('rewrite_rules.loading') }}
    </div>
    <UEmpty
      v-else
      icon="i-lucide-repeat-2"
      :title="t('rewrite_rules.empty_title')"
      :description="t('rewrite_rules.empty_description')"
      class="flex-1"
      ><template #actions
        ><UButton
          icon="i-lucide-plus"
          :label="t('rewrite_rules.new_rule')"
          :disabled="store.busy || store.state.revision < 0"
          @click="store.create(t('rewrite_rules.new_name'))" /></template
    ></UEmpty>
    <UModal
      v-model:open="deleteOpen"
      :title="t('rewrite_rules.delete')"
      :close="!store.busy"
      :dismissible="!store.busy"
    >
      <template #body
        ><p class="text-sm">
          {{
            t(deleteDirty ? 'rewrite_rules.delete_dirty' : 'rewrite_rules.delete_confirm', {
              name: deleteName,
            })
          }}
        </p>
        <UAlert
          v-if="deleteFieldError"
          color="error"
          variant="subtle"
          :description="t(deleteFieldError)"
          class="mt-3" />
        <UAlert
          v-if="store.error"
          color="error"
          variant="subtle"
          :description="store.error"
          class="mt-3" /><UAlert
          v-if="store.selectedDraft?.conflict"
          color="warning"
          variant="subtle"
          :description="t('rewrite_rules.conflict')"
          class="mt-3"
      /></template>
      <template #footer
        ><div class="flex w-full flex-wrap justify-end gap-2">
          <UButton
            color="neutral"
            variant="outline"
            :label="t('rewrite_rules.cancel')"
            :disabled="store.busy"
            @click="deleteOpen = false"
          /><UButton
            color="error"
            variant="outline"
            :label="t(deleteDirty ? 'rewrite_rules.discard_delete' : 'rewrite_rules.delete')"
            :disabled="store.busy"
            @click="remove(false)"
          /><UButton
            v-if="deleteDirty"
            :label="t('rewrite_rules.save_delete')"
            :disabled="store.busy || !!store.selectedDraft?.conflict"
            @click="remove(true)"
          /></div
      ></template>
    </UModal>
  </div>
</template>
