<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import type { Rule } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import { usePythonPluginsStore } from '@/stores/pythonPlugins'

interface RuleDraft {
  id: string
  enabled: boolean
  method: string
  urlPattern: string
}

const { t } = useI18n()
const store = usePythonPluginsStore()
const drafts = ref<RuleDraft[]>([])
const error = ref('')

const sourceRules = computed(() =>
  (store.selectedPlugin?.rules ?? []).filter((rule): rule is Rule => Boolean(rule)),
)

watch(
  [
    () => store.selectedPluginId,
    () => sourceRules.value.map((rule) => `${rule.id}:${rule.updatedAt}`).join('|'),
  ],
  () => {
    drafts.value = sourceRules.value.map((rule) => ({
      id: rule.id,
      enabled: rule.enabled,
      method: rule.method,
      urlPattern: rule.urlPattern,
    }))
    error.value = ''
  },
  { immediate: true },
)

async function addRule() {
  if (!store.selectedPluginId) return
  error.value = ''
  try {
    await store.createRule(store.selectedPluginId)
  } catch (createError) {
    error.value = String(createError)
  }
}

async function saveRule(rule: RuleDraft) {
  if (!store.selectedPluginId) return
  error.value = ''
  try {
    await store.updateRule(store.selectedPluginId, rule.id, {
      enabled: rule.enabled,
      method: rule.method,
      urlPattern: rule.urlPattern,
    })
  } catch (updateError) {
    error.value = String(updateError)
  }
}

function updateMethodDraft(rule: RuleDraft, method: string) {
  rule.method = method.toUpperCase()
}

async function updateEnabled(rule: RuleDraft, enabled: boolean) {
  rule.enabled = enabled
  await saveRule(rule)
}

async function removeRule(rule: RuleDraft) {
  if (!store.selectedPluginId) return
  error.value = ''
  try {
    await store.deleteRule(store.selectedPluginId, rule.id)
  } catch (deleteError) {
    error.value = String(deleteError)
  }
}

async function handleReorder() {
  if (!store.selectedPluginId) return
  error.value = ''
  try {
    await store.reorderRuleList(
      store.selectedPluginId,
      drafts.value.map((rule) => rule.id),
    )
  } catch (reorderError) {
    error.value = String(reorderError)
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between gap-3 px-3 py-2">
      <p class="text-xs text-muted">
        {{ t('python_plugins.rules_hint') }}
      </p>
      <UButton
        icon="i-lucide-plus"
        size="xs"
        color="neutral"
        variant="outline"
        :label="t('python_plugins.add_rule')"
        :loading="store.isMutating(`rule:create:${store.selectedPluginId}`)"
        @click="addRule"
      />
    </div>
    <UAlert
      v-if="error"
      color="error"
      variant="subtle"
      :description="error"
      class="mx-3 mb-2 shrink-0"
    />
    <UEmpty
      v-if="drafts.length === 0"
      icon="i-lucide-route"
      :title="t('python_plugins.no_rules')"
      class="min-h-0 flex-1"
    />
    <div v-else class="min-h-0 flex-1 overflow-y-auto px-3 pb-3">
      <VueDraggable
        v-model="drafts"
        tag="div"
        handle=".python-rule-drag-handle"
        :animation="150"
        class="space-y-1.5"
        @end="handleReorder"
      >
        <div
          v-for="rule in drafts"
          :key="rule.id"
          class="grid grid-cols-[18px_88px_minmax(160px,1fr)_auto_28px] items-center gap-2 rounded-md border border-default bg-elevated/60 px-2 py-1.5"
        >
          <UIcon
            name="i-lucide-grip-vertical"
            class="python-rule-drag-handle size-4 cursor-grab text-dimmed"
          />
          <UInput
            :model-value="rule.method"
            size="sm"
            :disabled="store.isMutating(`rule:update:${store.selectedPluginId}:${rule.id}`)"
            @update:model-value="updateMethodDraft(rule, String($event))"
            @blur="saveRule(rule)"
            @keydown.enter.prevent="saveRule(rule)"
          />
          <UInput
            v-model="rule.urlPattern"
            size="sm"
            :placeholder="t('python_plugins.rule_pattern_placeholder')"
            :disabled="store.isMutating(`rule:update:${store.selectedPluginId}:${rule.id}`)"
            @blur="saveRule(rule)"
            @keydown.enter.prevent="saveRule(rule)"
          />
          <USwitch
            :model-value="rule.enabled"
            size="sm"
            :aria-label="t('python_plugins.enabled')"
            :disabled="store.isMutating(`rule:update:${store.selectedPluginId}:${rule.id}`)"
            @update:model-value="updateEnabled(rule, $event)"
          />
          <UTooltip :text="t('python_plugins.delete_rule')">
            <UButton
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              size="xs"
              :aria-label="t('python_plugins.delete_rule')"
              :loading="store.isMutating(`rule:delete:${store.selectedPluginId}:${rule.id}`)"
              @click="removeRule(rule)"
            />
          </UTooltip>
        </div>
      </VueDraggable>
    </div>
  </div>
</template>
