<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import { useNotify } from '@/composables/useNotify'
import { usePythonPluginsStore } from '@/stores/pythonPlugins'

const { t } = useI18n()
const notify = useNotify()
const store = usePythonPluginsStore()

function updateDraft(value: string) {
  store.paramsDraft = value
  store.paramsError = ''
}

function formatParams() {
  try {
    const value: unknown = JSON.parse(store.paramsDraft)
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      throw new Error(t('python_plugins.params_object_required'))
    }
    store.paramsDraft = JSON.stringify(value, null, 2)
    store.paramsError = ''
  } catch (error) {
    store.paramsError = String(error)
  }
}

async function saveParams() {
  try {
    await store.saveParams()
    notify.success(t('python_plugins.params_saved'))
  } catch {
    // The store keeps the draft and exposes the actionable inline error.
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center justify-between gap-3 px-3 py-2">
      <p class="text-xs text-muted">
        {{ t('python_plugins.params_hint') }}
      </p>
      <div class="flex items-center gap-1.5">
        <UButton
          color="neutral"
          variant="ghost"
          size="xs"
          icon="i-lucide-align-left"
          :label="t('python_plugins.format')"
          @click="formatParams"
        />
        <UButton
          size="xs"
          icon="i-lucide-save"
          :label="t('python_plugins.save_params')"
          :disabled="!store.paramsDirty"
          :loading="store.isMutating(`params:save:${store.selectedPluginId}`)"
          @click="saveParams"
        />
      </div>
    </div>
    <UAlert
      v-if="store.paramsError"
      color="error"
      variant="subtle"
      :description="store.paramsError"
      class="mx-3 mb-2 shrink-0"
    />
    <MonacoBodyEditor
      :value="store.paramsDraft"
      language="json"
      :word-wrap="false"
      :options="{ lineNumbersMinChars: 3, padding: { top: 8, bottom: 8 } }"
      class="min-h-0 flex-1"
      @update:value="updateDraft"
    />
  </div>
</template>
