<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import type { Plugin } from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'
import { useNotify } from '@/composables/useNotify'
import { usePythonPluginsStore } from '@/stores/pythonPlugins'
import { useWorkbenchStore } from '@/stores/workbench'

const { t } = useI18n()
const notify = useNotify()
const store = usePythonPluginsStore()
const workbenchStore = useWorkbenchStore()
const search = ref('')
const createOpen = ref(false)
const createName = ref('')
const createDescription = ref('')
const createError = ref('')

const filteredPlugins = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query) {
    return store.plugins
  }
  return store.plugins.filter((plugin) =>
    `${plugin.name}\n${plugin.description}`.toLocaleLowerCase().includes(query),
  )
})

const sortablePlugins = computed({
  get: () => filteredPlugins.value,
  set: (plugins: Plugin[]) => {
    if (!search.value.trim()) {
      store.setPluginOrderLocal(plugins)
    }
  },
})

const runtimeTone = computed<'neutral' | 'success' | 'warning' | 'error'>(() => {
  const status = store.runtimeStatus
  if (!status || !status.enabled) return 'neutral'
  if (status.error) return 'error'
  return status.ready ? 'success' : 'warning'
})

const runtimeLabel = computed(() => {
  const status = store.runtimeStatus
  if (!status) return t('python_plugins.runtime_unknown')
  if (!status.enabled) return t('python_plugins.runtime_disabled')
  if (status.error) return t('python_plugins.runtime_error')
  return status.ready
    ? t('python_plugins.runtime_ready')
    : t('python_plugins.runtime_idle')
})

function validationColor(plugin: Plugin) {
  if (plugin.validationStatus === 'valid') return 'bg-success'
  if (plugin.validationStatus === 'invalid') return 'bg-error'
  return 'bg-warning'
}

function validationLabel(plugin: Plugin) {
  return t(`python_plugins.validation_${plugin.validationStatus || 'unavailable'}`)
}

async function selectPlugin(pluginId: string) {
  workbenchStore.selectPythonPluginsItem()
  try {
    await store.selectPlugin(pluginId)
  } catch (error) {
    notify.error(t('python_plugins.load_failed', { error: String(error) }))
  }
}

async function togglePlugin(plugin: Plugin, enabled: boolean) {
  try {
    await store.setPluginEnabled(plugin.id, enabled)
  } catch (error) {
    notify.error(t('python_plugins.enable_failed', { error: String(error) }))
  }
}

async function handleReorder() {
  if (search.value.trim()) return
  try {
    await store.reorderPluginList(store.plugins.map((plugin) => plugin.id))
  } catch (error) {
    notify.error(t('python_plugins.reorder_failed', { error: String(error) }))
  }
}

function openCreate() {
  createName.value = ''
  createDescription.value = ''
  createError.value = ''
  createOpen.value = true
}

async function submitCreate() {
  if (!createName.value.trim()) {
    createError.value = t('python_plugins.name_required')
    return
  }
  createError.value = ''
  try {
    await store.createPlugin({
      name: createName.value,
      description: createDescription.value,
    })
    createOpen.value = false
    notify.success(t('python_plugins.create_success'))
  } catch (error) {
    createError.value = String(error)
  }
}

async function openRootDirectory() {
  try {
    await store.openPluginsDirectory()
  } catch (error) {
    notify.error(t('python_plugins.open_directory_failed', { error: String(error) }))
  }
}

onMounted(() => {
  void store.initialize()
})

onBeforeUnmount(() => {
  store.cleanup()
})
</script>

<template>
  <div class="flex h-full w-full min-w-0 flex-col overflow-hidden bg-default">
    <div class="shrink-0 border-b border-default p-2.5">
      <div class="mb-2 flex items-center justify-between gap-2">
        <div class="flex min-w-0 items-center gap-2">
          <span class="text-sm font-semibold text-highlighted">
            {{ t('python_plugins.title') }}
          </span>
          <UBadge :color="runtimeTone" variant="subtle" size="sm">
            {{ runtimeLabel }}
          </UBadge>
        </div>
        <div class="flex shrink-0 items-center gap-0.5">
          <UTooltip :text="t('python_plugins.open_root_directory')">
            <UButton
              icon="i-lucide-folder-open"
              color="neutral"
              variant="ghost"
              size="xs"
              :aria-label="t('python_plugins.open_root_directory')"
              @click="openRootDirectory"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.create')">
            <UButton
              icon="i-lucide-plus"
              color="primary"
              variant="soft"
              size="xs"
              :aria-label="t('python_plugins.create')"
              @click="openCreate"
            />
          </UTooltip>
        </div>
      </div>
      <UInput
        v-model="search"
        icon="i-lucide-search"
        size="sm"
        :placeholder="t('python_plugins.search_placeholder')"
        :aria-label="t('python_plugins.search_placeholder')"
        class="w-full"
      />
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto py-1.5">
      <div
        v-if="store.loadingPlugins && store.plugins.length === 0"
        class="flex h-24 items-center justify-center text-sm text-muted"
      >
        {{ t('app.loading') }}
      </div>
      <UEmpty
        v-else-if="sortablePlugins.length === 0"
        icon="i-lucide-file-code-2"
        :title="search ? t('python_plugins.no_search_results') : t('python_plugins.empty')"
        class="py-8"
      />
      <VueDraggable
        v-else
        v-model="sortablePlugins"
        tag="div"
        handle=".python-plugin-drag-handle"
        :disabled="Boolean(search.trim())"
        :animation="160"
        ghost-class="opacity-30"
        chosen-class="bg-elevated"
        @end="handleReorder"
      >
        <div
          v-for="plugin in sortablePlugins"
          :key="plugin.id"
          class="group flex w-full min-w-0 items-center gap-1.5 border-l-3 px-2 py-2 text-left transition-colors"
          :class="
            store.selectedPluginId === plugin.id
              ? 'border-primary bg-primary/10'
              : 'border-transparent hover:bg-elevated'
          "
        >
          <UIcon
            name="i-lucide-grip-vertical"
            class="python-plugin-drag-handle size-3.5 shrink-0 cursor-grab text-dimmed opacity-0 group-hover:opacity-100"
            :class="search.trim() ? 'invisible' : ''"
          />
          <button
            type="button"
            class="flex min-w-0 flex-1 items-center gap-1.5 text-left"
            @click="selectPlugin(plugin.id)"
          >
            <span class="relative flex size-4 shrink-0 items-center justify-center">
              <UIcon name="i-lucide-file-code-2" class="size-4 text-muted" />
              <UTooltip :text="validationLabel(plugin)">
                <span
                  class="absolute -right-0.5 -bottom-0.5 size-1.75 rounded-full ring-2 ring-default"
                  :class="validationColor(plugin)"
                />
              </UTooltip>
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium text-highlighted">
                {{ plugin.name }}
              </span>
              <span v-if="plugin.description" class="mt-0.5 block truncate text-xs text-muted">
                {{ plugin.description }}
              </span>
            </span>
          </button>
          <USwitch
            :model-value="plugin.enabled"
            size="sm"
            :disabled="store.isMutating(`plugin:enabled:${plugin.id}`)"
            :loading="store.isMutating(`plugin:enabled:${plugin.id}`)"
            :aria-label="t('python_plugins.enabled')"
            @click.stop
            @update:model-value="togglePlugin(plugin, $event)"
          />
        </div>
      </VueDraggable>
      <p v-if="store.error" class="px-3 py-2 text-xs text-error">
        {{ store.error }}
      </p>
    </div>

    <UModal
      v-model:open="createOpen"
      :title="t('python_plugins.create_title')"
      :close="!store.isMutating('plugin:create')"
      :dismissible="!store.isMutating('plugin:create')"
    >
      <template #body>
        <div class="space-y-4">
          <UFormField :label="t('python_plugins.name')" required>
            <UInput
              v-model="createName"
              autofocus
              class="w-full"
              :disabled="store.isMutating('plugin:create')"
              @keydown.enter.prevent="submitCreate"
            />
          </UFormField>
          <UFormField :label="t('python_plugins.description')">
            <UTextarea
              v-model="createDescription"
              class="w-full"
              :rows="3"
              :disabled="store.isMutating('plugin:create')"
            />
          </UFormField>
          <UAlert v-if="createError" color="error" variant="subtle" :description="createError" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton
            color="neutral"
            variant="outline"
            :label="t('python_plugins.cancel')"
            :disabled="store.isMutating('plugin:create')"
            @click="createOpen = false"
          />
          <UButton
            :label="t('python_plugins.create')"
            :loading="store.isMutating('plugin:create')"
            @click="submitCreate"
          />
        </div>
      </template>
    </UModal>
  </div>
</template>
