<script setup lang="ts">
import { computed, onMounted, shallowRef, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import { useRewriteRulesStore } from '@/stores/rewriteRules'
import { useWorkbenchStore } from '@/stores/workbench'

const { t } = useI18n()
const store = useRewriteRulesStore()
const workbench = useWorkbenchStore()
onMounted(() => void store.initialize())
const search = shallowRef('')
const dragging = shallowRef(false)
const sortableRows = shallowRef<typeof store.rows>([])
let dragRevision = -1
const filteredRows = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return store.rows.filter(({ rule }) =>
    `${rule.name}\n${rule.method}\n${rule.urlPattern}`.toLocaleLowerCase().includes(query),
  )
})
watch(
  filteredRows,
  (rows) => {
    if (!dragging.value) sortableRows.value = rows
  },
  { immediate: true },
)

function startReorder() {
  dragging.value = true
  dragRevision = store.state.revision
}
async function finishReorder() {
  dragging.value = false
  try {
    if (search.value.trim() || store.busy || dragRevision !== store.state.revision) return
    const ids = sortableRows.value.filter((row) => row.saved).map((row) => row.key)
    if (ids.every((id, index) => id === store.rules[index]?.id)) return
    await store.reorder(ids)
  } finally {
    sortableRows.value = filteredRows.value
  }
}
function select(id: string) {
  store.select(id)
  workbench.selectRewriteRulesItem()
}
function create() {
  store.create(t('rewrite_rules.new_name'))
  workbench.selectRewriteRulesItem()
}
</script>

<template>
  <div class="flex h-full min-w-0 flex-col overflow-hidden bg-default">
    <div class="shrink-0 space-y-3 border-b border-default p-2.5">
      <div class="flex items-center justify-between gap-2">
        <span class="text-sm font-semibold text-highlighted">{{ t('rewrite_rules.title') }}</span>
        <div class="flex items-center gap-1">
          <UTooltip :text="t('rewrite_rules.refresh')">
            <UButton
              icon="i-lucide-refresh-cw"
              size="xs"
              color="neutral"
              variant="ghost"
              :aria-label="t('rewrite_rules.refresh')"
              :loading="store.loading"
              :disabled="store.busy"
              class="disabled:opacity-100"
              @click="store.refresh()"
            />
          </UTooltip>
          <UButton
            icon="i-lucide-plus"
            size="xs"
            variant="ghost"
            :aria-label="t('rewrite_rules.new_rule')"
            :disabled="store.busy || store.state.revision < 0"
            :ui="{ base: store.state.revision >= 0 ? 'disabled:opacity-100' : undefined }"
            @click="create"
          />
        </div>
      </div>
      <USwitch
        :model-value="store.state.enabled"
        :disabled="store.busy || store.state.revision < 0"
        :loading="store.pendingMutation?.kind === 'enabled'"
        :ui="{ root: store.state.revision >= 0 ? 'opacity-100' : undefined }"
        :label="t('rewrite_rules.enable_all')"
        @update:model-value="store.setEnabled"
      />
      <UInput
        v-model="search"
        icon="i-lucide-search"
        :placeholder="t('rewrite_rules.search')"
        :aria-label="t('rewrite_rules.search')"
        class="w-full"
      />
      <p class="text-xs text-muted">{{ t('rewrite_rules.order_hint') }}</p>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
      <VueDraggable
        v-model="sortableRows"
        tag="div"
        handle=".rewrite-rule-drag-handle"
        draggable=".rewrite-rule-saved"
        :disabled="Boolean(search.trim()) || store.busy"
        :animation="160"
        :force-fallback="true"
        :fallback-tolerance="3"
        ghost-class="opacity-30"
        chosen-class="bg-elevated"
        @start="startReorder"
        @end="finishReorder"
      >
        <div
          v-for="row in sortableRows"
          :key="row.key"
          data-clickable
          class="mb-1 rounded-md border p-2"
          :class="[
            row.saved && 'rewrite-rule-saved',
            store.selectedId === row.key
              ? 'border-primary/40 bg-primary/10'
              : 'border-transparent hover:bg-elevated',
          ]"
          @click="select(row.key)"
        >
          <div class="flex items-center gap-1.5">
            <UIcon
              name="i-lucide-grip-vertical"
              class="size-3.5 shrink-0 text-dimmed"
              :class="
                row.saved && !search.trim() && !store.busy
                  ? 'rewrite-rule-drag-handle cursor-grab'
                  : ''
              "
              @click.stop
            />
            <button
              type="button"
              class="min-w-0 flex-1 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              :aria-current="store.selectedId === row.key ? 'true' : undefined"
              @click.stop="select(row.key)"
            >
              <span class="flex items-center gap-1.5 text-sm font-medium text-highlighted"
                ><span class="truncate">{{ row.rule.name || t('rewrite_rules.new_name') }}</span
                ><span
                  v-if="store.isDirty(row.key)"
                  class="size-1.5 shrink-0 rounded-full bg-primary"
                  :title="t('rewrite_rules.unsaved')"
              /></span>
            </button>
            <USwitch
              size="xs"
              :model-value="row.saved?.enabled ?? false"
              :disabled="!row.saved || !!row.saved.unavailableReason || store.busy"
              :loading="
                store.pendingMutation?.kind === 'ruleEnabled' &&
                store.pendingMutation.id === row.key
              "
              :ui="{ root: row.saved && !row.saved.unavailableReason ? 'opacity-100' : undefined }"
              :aria-label="t('rewrite_rules.enable_rule', { name: row.rule.name })"
              @click.stop
              @update:model-value="store.setRuleEnabled(row.key, $event)"
            />
          </div>
          <p class="mt-1 truncate font-mono text-xs text-muted">
            {{ row.rule.urlPattern }}
          </p>
          <div class="mt-1.5 flex flex-wrap items-center gap-1">
            <UBadge color="neutral" variant="subtle" size="sm">{{ row.rule.method }}</UBadge>
            <UBadge
              :color="row.rule.unavailableReason ? 'error' : 'neutral'"
              variant="subtle"
              size="sm"
              >{{
                t(
                  row.rule.unavailableReason
                    ? 'rewrite_rules.unavailable'
                    : `rewrite_rules.action_${row.rule.action.type}`,
                )
              }}</UBadge
            >
            <span v-if="!row.saved" class="text-xs text-muted">{{ t('rewrite_rules.draft') }}</span>
          </div>
        </div>
      </VueDraggable>
      <p v-if="!filteredRows.length" class="p-3 text-center text-xs text-muted">
        {{ t(store.loading ? 'rewrite_rules.loading' : 'rewrite_rules.no_rules') }}
      </p>
    </div>
  </div>
</template>
