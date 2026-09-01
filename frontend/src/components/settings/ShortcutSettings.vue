<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useKbd } from '@nuxt/ui/composables'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import { resolveShortcut, shortcutBindingsConflict, shortcutToUKbdKeys } from '@/shortcuts/binding'
import { shortcutCatalog } from '@/shortcuts/catalog'
import { shortcutRecordingCoordinator } from '@/shortcuts/recording'
import type {
  ShortcutBinding,
  ShortcutCommand,
  ShortcutConfig,
  ShortcutScope,
} from '@/shortcuts/types'
import type * as settingservice from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import { ShortcutScope as SettingShortcutScope } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import type * as shortcutservice from '#bindings/github.com/josexy/flowlens/backend/services/shortcut_service/models'
import { ShortcutRuntimeStatus } from '#bindings/github.com/josexy/flowlens/backend/services/shortcut_service/models'

const shortcuts = defineModel<settingservice.ShortcutConfig>('shortcuts', { required: true })
const props = defineProps<{
  active: boolean
  runtimeState?: shortcutservice.ShortcutRuntimeState | null
  applyResult?: shortcutservice.ShortcutApplyResult | null
}>()

type PendingConflict = {
  command: ShortcutCommand
  binding: ShortcutBinding
  conflicts: ShortcutCommand[]
}

const { t } = useI18n()
const { getKbdKey } = useKbd()
const search = ref('')
const recordingCommandId = ref<string | null>(null)
const pendingConflict = ref<PendingConflict | null>(null)
const resetAllConfirmVisible = ref(false)
let stopRecording: (() => void) | null = null

const displayedRuntimeState = computed(() =>
  props.applyResult && !props.applyResult.applied
    ? props.applyResult.runtimeState
    : props.runtimeState,
)

const runtimeWarnings = computed(() => [
  ...new Set([
    ...(displayedRuntimeState.value?.warnings ?? []),
    ...(props.applyResult?.warnings ?? []),
  ]),
])

const groupedCommands = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const groups = new Map<string, ShortcutCommand[]>()
  for (const command of shortcutCatalog) {
    const category = t(command.categoryKey)
    const label = t(command.labelKey)
    const matches = !query || [category, label, command.id].some((value) =>
      value.toLocaleLowerCase().includes(query),
    )
    if (!matches) continue
    const commands = groups.get(command.categoryKey) ?? []
    commands.push(command)
    groups.set(command.categoryKey, commands)
  }
  return [...groups.entries()].map(([categoryKey, commands]) => ({
    categoryKey,
    label: t(categoryKey),
    commands,
  }))
})

const conflictDescription = computed(() => {
  const conflict = pendingConflict.value
  if (!conflict) return ''
  return t('shortcuts.conflict_message', {
    command: t(conflict.command.labelKey),
    binding: bindingText(conflict.binding),
    conflicts: conflict.conflicts.map((item) => t(item.labelKey)).join(t('settings.field_separator')),
  })
})

function currentOverrides() {
  return shortcuts.value.overrides ?? {}
}

function shortcutConfig(): ShortcutConfig {
  return {
    overrides: currentOverrides() as unknown as ShortcutConfig['overrides'],
  }
}

function resolved(command: ShortcutCommand) {
  return resolveShortcut(command, shortcutConfig())
}

function scopeFor(command: ShortcutCommand): ShortcutScope {
  if (command.id === 'app.showMainWindow') return 'global'
  if (
    command.id === 'capture.toggleProxy' &&
    currentOverrides()[command.id]?.scope === 'global'
  ) {
    return 'global'
  }
  return 'application'
}

function bindingKeys(command: ShortcutCommand) {
  return shortcutToUKbdKeys(resolved(command).binding)
}

function bindingText(binding: ShortcutBinding | null) {
  return shortcutToUKbdKeys(binding)
    .map((key) => getKbdKey(key) ?? key)
    .join('+')
}

function resolvedBindingText(command: ShortcutCommand) {
  return bindingText(resolved(command).binding) || t('shortcuts.not_set')
}

function commandRuntimeState(command: ShortcutCommand) {
  if (scopeFor(command) !== 'global' || !resolved(command).binding) {
    return null
  }
  return displayedRuntimeState.value?.commands?.[command.id] ?? null
}

function runtimeStatusColor(status: shortcutservice.ShortcutRuntimeStatus) {
  switch (status) {
    case ShortcutRuntimeStatus.ShortcutStatusActive:
      return 'success'
    case ShortcutRuntimeStatus.ShortcutStatusConflict:
      return 'error'
    case ShortcutRuntimeStatus.ShortcutStatusPortalPending:
      return 'warning'
    default:
      return 'neutral'
  }
}

function hasOverride(command: ShortcutCommand) {
  return Object.hasOwn(currentOverrides(), command.id)
}

function makeOverride(
  command: ShortcutCommand,
  binding: ShortcutBinding | null,
  scope = scopeFor(command),
): settingservice.ShortcutOverride {
  const normalizedScope = command.id === 'app.showMainWindow' ? 'global' : scope
  return {
    binding: binding
      ? { modifiers: [...binding.modifiers] as settingservice.ShortcutModifier[], key: binding.key }
      : null,
    scope: normalizedScope === 'global'
      ? SettingShortcutScope.ShortcutScopeGlobal
      : SettingShortcutScope.ShortcutScopeApplication,
  }
}

function applyBinding(command: ShortcutCommand, binding: ShortcutBinding | null) {
  shortcuts.value.overrides = {
    ...currentOverrides(),
    [command.id]: makeOverride(command, binding),
  }
}

function clearBinding(command: ShortcutCommand) {
  applyBinding(command, null)
}

function resetCommand(command: ShortcutCommand) {
  const nextOverrides = { ...currentOverrides() }
  delete nextOverrides[command.id]
  shortcuts.value.overrides = nextOverrides
}

function requestBinding(command: ShortcutCommand, binding: ShortcutBinding) {
  const conflicts = shortcutCatalog.filter(
    (candidate) => candidate.id !== command.id && shortcutBindingsConflict(resolved(candidate).binding, binding),
  )
  if (conflicts.length === 0) {
    applyBinding(command, binding)
    return
  }
  pendingConflict.value = { command, binding, conflicts }
}

function confirmConflictReplacement() {
  const conflict = pendingConflict.value
  if (!conflict) return
  const nextOverrides = {
    ...currentOverrides(),
    [conflict.command.id]: makeOverride(conflict.command, conflict.binding),
  }
  for (const command of conflict.conflicts) {
    nextOverrides[command.id] = makeOverride(command, null)
  }
  shortcuts.value.overrides = nextOverrides
  pendingConflict.value = null
}

function cancelConflictReplacement() {
  pendingConflict.value = null
}

function startRecording(command: ShortcutCommand) {
  stopRecording?.()
  recordingCommandId.value = command.id
  stopRecording = shortcutRecordingCoordinator.start((result) => {
    stopRecording = null
    recordingCommandId.value = null
    if (result.type === 'clear') clearBinding(command)
    else if (result.type === 'binding') requestBinding(command, result.binding)
  })
}

function updateScope(command: ShortcutCommand, scope: ShortcutScope) {
  if (command.id !== 'capture.toggleProxy') return
  shortcuts.value.overrides = {
    ...currentOverrides(),
    [command.id]: makeOverride(command, resolved(command).binding, scope),
  }
}

function confirmResetAll() {
  const nextOverrides = { ...currentOverrides() }
  for (const command of shortcutCatalog) {
    delete nextOverrides[command.id]
  }
  shortcuts.value.overrides = nextOverrides
  resetAllConfirmVisible.value = false
}

function stopShortcutInteractions() {
  stopRecording?.()
  stopRecording = null
  recordingCommandId.value = null
  pendingConflict.value = null
  resetAllConfirmVisible.value = false
}

watch(
  () => props.active,
  (active) => {
    if (!active) {
      stopShortcutInteractions()
    }
  },
)

onBeforeUnmount(() => {
  stopShortcutInteractions()
})
</script>

<template>
  <div class="min-w-0">
    <SettingsSection :title="t('shortcuts.title')">
      <template #actions>
        <UButton color="neutral" variant="outline" icon="i-lucide-rotate-ccw" :label="t('shortcuts.reset_all')" @click="resetAllConfirmVisible = true" />
      </template>
      <p class="mb-3 text-sm leading-normal text-muted">{{ t('shortcuts.global_note') }}</p>
      <div v-if="runtimeWarnings.length" class="mb-3 flex flex-col gap-2">
        <UAlert
          v-for="warning in runtimeWarnings"
          :key="warning"
          color="warning"
          variant="subtle"
          icon="i-lucide-triangle-alert"
          :title="t(`shortcuts.warnings.${warning}`)"
        />
      </div>
      <UInput v-model="search" icon="i-lucide-search" :placeholder="t('shortcuts.search_placeholder')" :aria-label="t('shortcuts.search_placeholder')" class="mb-4 w-full" />
      <div v-if="groupedCommands.length" class="flex flex-col gap-4">
        <section v-for="group in groupedCommands" :key="group.categoryKey" class="min-w-0">
          <h3 class="mb-2 text-sm font-semibold text-default">{{ group.label }}</h3>
          <div class="overflow-hidden rounded-lg border border-default bg-default">
            <div v-for="command in group.commands" :key="command.id" class="flex min-w-0 items-center gap-3 border-b border-default p-3 last:border-b-0 max-[760px]:flex-wrap" :class="recordingCommandId === command.id ? 'bg-primary/10' : ''">
              <div class="min-w-0 flex-1">
                <div class="truncate text-sm font-medium text-default">{{ t(command.labelKey) }}</div>
              </div>
              <div class="flex shrink-0 flex-wrap items-center justify-end gap-2 max-[760px]:w-full max-[760px]:justify-start">
                <div v-if="command.id === 'capture.toggleProxy'" class="flex items-center gap-1.5 text-xs text-muted">
                  <span>{{ t('shortcuts.scope_global') }}</span>
                  <USwitch
                    :model-value="scopeFor(command) === 'global'"
                    size="sm"
                    :aria-label="t('shortcuts.scope_label')"
                    @update:model-value="updateScope(command, $event ? 'global' : 'application')"
                  />
                </div>
                <UBadge v-else-if="command.id === 'app.showMainWindow'" color="neutral" variant="subtle" size="sm">{{ t('shortcuts.scope_global') }}</UBadge>
                <UBadge
                  v-if="commandRuntimeState(command)"
                  :color="runtimeStatusColor(commandRuntimeState(command)!.status)"
                  variant="subtle"
                  size="sm"
                >
                  {{ t(`shortcuts.runtime.${commandRuntimeState(command)!.status}`) }}
                </UBadge>
                <UButton color="neutral" variant="outline" size="sm" :title="resolvedBindingText(command)" :aria-label="t('shortcuts.record_for', { command: t(command.labelKey) })" :class="recordingCommandId === command.id ? 'ring-2 ring-primary' : ''" @click="startRecording(command)">
                  <span v-if="recordingCommandId === command.id" class="text-primary">{{ t('shortcuts.recording') }}</span>
                  <span v-else-if="bindingKeys(command).length" class="flex items-center gap-1"><UKbd v-for="key in bindingKeys(command)" :key="key" :value="key" size="sm" /></span>
                  <span v-else class="text-muted">{{ t('shortcuts.not_set') }}</span>
                </UButton>
                <UButton color="neutral" variant="ghost" size="sm" icon="i-lucide-eraser" :label="t('shortcuts.clear')" :aria-label="t('shortcuts.clear_for', { command: t(command.labelKey) })" :disabled="!resolved(command).binding" @click="clearBinding(command)" />
                <UButton color="neutral" variant="ghost" size="sm" icon="i-lucide-rotate-ccw" :label="t('shortcuts.reset')" :aria-label="t('shortcuts.reset_for', { command: t(command.labelKey) })" :disabled="!hasOverride(command)" @click="resetCommand(command)" />
              </div>
            </div>
          </div>
        </section>
      </div>
      <div v-else class="rounded-lg border border-dashed border-default p-6 text-center text-sm text-muted">{{ t('shortcuts.empty_search') }}</div>
    </SettingsSection>
    <ConfirmCardModal :show="Boolean(pendingConflict)" :title="t('shortcuts.conflict_title')" :positive-text="t('shortcuts.replace')" :negative-text="t('shortcuts.cancel')" positive-type="warning" @update:show="!$event && cancelConflictReplacement()" @positive-click="confirmConflictReplacement" @negative-click="cancelConflictReplacement">{{ conflictDescription }}</ConfirmCardModal>
    <ConfirmCardModal :show="resetAllConfirmVisible" :title="t('shortcuts.reset_all')" :positive-text="t('shortcuts.reset_all')" :negative-text="t('shortcuts.cancel')" positive-type="warning" @update:show="resetAllConfirmVisible = $event" @positive-click="confirmResetAll">{{ t('shortcuts.reset_all_confirm') }}</ConfirmCardModal>
  </div>
</template>
