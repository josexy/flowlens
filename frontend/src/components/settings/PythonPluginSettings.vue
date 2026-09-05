<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import { useI18n } from 'vue-i18n'
import type { PythonPluginConfig } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'
import type {
  InterpreterCandidate,
  RuntimeStatus,
} from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import { useNotify } from '@/composables/useNotify'
import { useSettingStore } from '@/stores/setting'
import { getErrorMessage, isDialogCancelError } from '@/utils/dialog'

const config = defineModel<PythonPluginConfig>({ required: true })
const { t } = useI18n()
const notify = useNotify()
const settingStore = useSettingStore()
const trustedWarningOpen = ref(false)
const trustedWarningAccepted = ref(false)
const selectingInterpreter = ref(false)
const discoveringInterpreters = ref(false)
const testingInterpreter = ref(false)
const testStatus = ref<RuntimeStatus | null>(null)
const testError = ref('')
const interpreterDiscoveryOpen = ref(false)
const discoveredInterpreters = ref<InterpreterCandidate[]>([])
const selectedInterpreterPath = ref('')

type InterpreterOption = InterpreterCandidate & {
  value: string
  label: string
  description: string
  latest: boolean
}

const runtimeStatus = computed(() => settingStore.lastPythonRuntimeStatus)
const runtimeVersion = computed(() => {
  const status = testStatus.value ?? runtimeStatus.value
  if (!status?.ready) return ''
  return `${status.implementation} ${status.pythonMajor}.${status.pythonMinor}.${status.pythonPatch}`
})
const runtimeStateColor = computed<'neutral' | 'success' | 'warning' | 'error'>(() => {
  const status = runtimeStatus.value
  if (!status || !status.enabled) return 'neutral'
  if (status.error) return 'error'
  return status.ready ? 'success' : 'warning'
})
const runtimeStateLabel = computed(() => {
  const status = runtimeStatus.value
  if (!status) return t('settings.python_runtime_unknown')
  if (!status.enabled) return t('settings.python_runtime_disabled')
  if (status.error) return t('settings.python_runtime_error')
  return status.ready
    ? t('settings.python_runtime_ready')
    : t('settings.python_runtime_idle')
})
const interpreterOptions = computed<InterpreterOption[]>(() => {
  const highestVersion = discoveredInterpreters.value[0]
  return discoveredInterpreters.value.map(candidate => ({
    ...candidate,
    value: candidate.interpreterPath,
    label: `${candidate.implementation} ${candidate.pythonMajor}.${candidate.pythonMinor}.${candidate.pythonPatch}`,
    description: candidate.interpreterPath,
    latest: Boolean(
      highestVersion
      && candidate.pythonMajor === highestVersion.pythonMajor
      && candidate.pythonMinor === highestVersion.pythonMinor
      && candidate.pythonPatch === highestVersion.pythonPatch
    ),
  }))
})

function requestEnabledChange(enabled: boolean) {
  if (!enabled) {
    config.value.enabled = false
    return
  }
  if (trustedWarningAccepted.value) {
    config.value.enabled = true
    return
  }
  trustedWarningOpen.value = true
}

function acceptTrustedWarning() {
  trustedWarningAccepted.value = true
  trustedWarningOpen.value = false
  config.value.enabled = true
}

async function selectInterpreter() {
  selectingInterpreter.value = true
  try {
    const selectedPath = await Dialogs.OpenFile({
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: false,
      AllowsOtherFiletypes: true,
      Title: t('settings.python_interpreter_picker_title'),
      Message: t('settings.python_interpreter_picker_message'),
    })
    if (typeof selectedPath === 'string' && selectedPath.trim()) {
      config.value.interpreterPath = selectedPath.trim()
    }
  } catch (error) {
    if (!isDialogCancelError(error)) {
      notify.error(
        t('settings.python_interpreter_picker_failed', { error: getErrorMessage(error) }),
      )
    }
  } finally {
    selectingInterpreter.value = false
  }
}

async function testInterpreter() {
  testingInterpreter.value = true
  testStatus.value = null
  testError.value = ''
  try {
    testStatus.value = await settingStore.testPythonInterpreter(config.value.interpreterPath)
  } catch (error) {
    testError.value = getErrorMessage(error)
  } finally {
    testingInterpreter.value = false
  }
}

async function useDiscoveredInterpreter(interpreterPath: string) {
  if (!interpreterPath) return
  interpreterDiscoveryOpen.value = false
  config.value.interpreterPath = interpreterPath
  await nextTick()
  await testInterpreter()
}

async function detectInterpreters() {
  discoveringInterpreters.value = true
  try {
    const candidates = await settingStore.discoverPythonInterpreters(
      config.value.interpreterPath,
    )
    discoveredInterpreters.value = candidates
    if (candidates.length === 0) {
      notify.warning(t('settings.python_interpreter_detect_none'))
      return
    }
    if (candidates.length === 1) {
      await useDiscoveredInterpreter(candidates[0].interpreterPath)
      return
    }
    selectedInterpreterPath.value =
      candidates.find(candidate => candidate.current)?.interpreterPath
      ?? candidates[0].interpreterPath
    interpreterDiscoveryOpen.value = true
  } catch (error) {
    notify.error(
      t('settings.python_interpreter_detect_failed', { error: getErrorMessage(error) }),
    )
  } finally {
    discoveringInterpreters.value = false
  }
}

watch(
  () => config.value.interpreterPath,
  () => {
    testStatus.value = null
    testError.value = ''
  },
)

onMounted(() => {
  void settingStore.refreshPythonRuntimeStatus().catch(() => {})
})
</script>

<template>
  <div class="min-w-0">
    <SettingsSection :title="t('settings.section_python_plugins')">
      <SettingsRow :label="t('settings.python_plugins_enabled_label')" wide>
        <div class="flex items-center gap-2.5">
          <USwitch
            :model-value="config.enabled"
            :aria-label="t('settings.python_plugins_enabled_label')"
            @update:model-value="requestEnabledChange"
          />
          <UBadge :color="runtimeStateColor" variant="subtle">
            {{ runtimeStateLabel }}
          </UBadge>
        </div>
      </SettingsRow>

      <SettingsRow :label="t('settings.python_interpreter_label')" wide align="start">
        <div class="flex min-w-0 flex-col gap-2">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <UFieldGroup class="min-w-0 flex-1 basis-80">
              <UInput
                v-model="config.interpreterPath"
                class="min-w-0 flex-1"
                :placeholder="t('settings.python_interpreter_placeholder')"
                :aria-label="t('settings.python_interpreter_label')"
              />
              <UTooltip :text="t('settings.python_interpreter_browse')">
                <UButton
                  icon="i-lucide-folder-open"
                  color="neutral"
                  variant="outline"
                  :disabled="discoveringInterpreters || testingInterpreter"
                  :loading="selectingInterpreter"
                  :aria-label="t('settings.python_interpreter_browse')"
                  @click="selectInterpreter"
                />
              </UTooltip>
            </UFieldGroup>
            <div class="flex shrink-0 items-center gap-2">
              <UButton
                icon="i-lucide-scan-search"
                color="neutral"
                variant="outline"
                :label="t('settings.python_interpreter_detect')"
                :disabled="selectingInterpreter || testingInterpreter"
                :loading="discoveringInterpreters"
                @click="detectInterpreters"
              />
              <UButton
                icon="i-lucide-flask-conical"
                color="neutral"
                variant="outline"
                :label="t('settings.python_interpreter_test')"
                :disabled="!config.interpreterPath.trim() || discoveringInterpreters"
                :loading="testingInterpreter"
                @click="testInterpreter"
              />
            </div>
          </div>
          <UAlert
            v-if="testStatus?.ready"
            icon="i-lucide-circle-check"
            color="success"
            variant="subtle"
            :description="
              t('settings.python_interpreter_test_success', { version: runtimeVersion })
            "
          />
          <UAlert
            v-else-if="testError"
            icon="i-lucide-circle-x"
            color="error"
            variant="subtle"
            :description="t('settings.python_interpreter_test_failed', { error: testError })"
          />
        </div>
      </SettingsRow>

      <SettingsRow :label="t('settings.python_hook_timeout_label')" wide>
        <div class="flex min-w-0 items-center gap-2">
          <UInputNumber
            v-model="config.hookTimeoutMs"
            orientation="vertical"
            :min="100"
            :max="60000"
            :step="100"
            :aria-label="t('settings.python_hook_timeout_accessible_label')"
            class="w-full max-w-44"
          />
          <span class="shrink-0 text-sm text-app-text-muted">{{
            t('settings.unit_milliseconds')
          }}</span>
        </div>
      </SettingsRow>

      <UAlert
        v-if="runtimeStatus?.error"
        icon="i-lucide-circle-alert"
        color="error"
        variant="subtle"
        :title="t('settings.python_runtime_error')"
        :description="runtimeStatus.error"
        class="mt-3"
      />
      <p v-else-if="runtimeVersion && !testStatus?.ready" class="mt-3 text-sm text-muted">
        {{ t('settings.python_runtime_version', { version: runtimeVersion }) }}
      </p>
    </SettingsSection>

    <UModal
      v-model:open="interpreterDiscoveryOpen"
      :title="t('settings.python_interpreter_detect_title')"
      :description="t('settings.python_interpreter_detect_description')"
      :ui="{ content: 'max-w-2xl' }"
    >
      <template #body>
        <URadioGroup
          v-model="selectedInterpreterPath"
          :items="interpreterOptions"
          value-key="value"
          variant="card"
          :ui="{ fieldset: 'max-h-[min(420px,55vh)] overflow-y-auto pr-1' }"
        >
          <template #label="{ item }">
            <span class="flex min-w-0 flex-wrap items-center gap-2">
              <span>{{ item.label }}</span>
              <UBadge v-if="item.latest" color="success" variant="subtle" size="sm">
                {{ t('settings.python_interpreter_detect_latest') }}
              </UBadge>
              <UBadge v-if="item.current" color="neutral" variant="subtle" size="sm">
                {{ t('settings.python_interpreter_detect_current') }}
              </UBadge>
            </span>
          </template>
          <template #description="{ item }">
            <span class="block break-all font-(family-name:--code-font-family) text-xs">
              {{ item.description }}
            </span>
          </template>
        </URadioGroup>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton
            color="neutral"
            variant="outline"
            :label="t('settings.python_interpreter_detect_cancel')"
            @click="interpreterDiscoveryOpen = false"
          />
          <UButton
            :label="t('settings.python_interpreter_detect_use')"
            :disabled="!selectedInterpreterPath"
            @click="useDiscoveredInterpreter(selectedInterpreterPath)"
          />
        </div>
      </template>
    </UModal>

    <ConfirmCardModal
      :show="trustedWarningOpen"
      :title="t('settings.python_trusted_code_title')"
      :positive-text="t('settings.python_trusted_code_confirm')"
      :negative-text="t('settings.python_trusted_code_cancel')"
      positive-type="warning"
      @update:show="trustedWarningOpen = $event"
      @positive-click="acceptTrustedWarning"
    >
      {{ t('settings.python_trusted_code_confirm_message') }}
    </ConfirmCardModal>
  </div>
</template>
