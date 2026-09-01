<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import ConfirmCardModal from '@/components/modal/ConfirmCardModal.vue'
import PythonPluginParamsPane from '@/components/python-plugins/PythonPluginParamsPane.vue'
import PythonPluginRulesPane from '@/components/python-plugins/PythonPluginRulesPane.vue'
import { useNotify } from '@/composables/useNotify'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'
import { usePythonPluginsStore } from '@/stores/pythonPlugins'
import { useWorkbenchStore } from '@/stores/workbench'

type DetailTab = 'rules' | 'params'
type FileAction = 'create' | 'rename'

interface FileTreeRow {
  key: string
  label: string
  path: string
  depth: number
  directory: boolean
}

const { t } = useI18n()
const notify = useNotify()
const store = usePythonPluginsStore()
const workbenchStore = useWorkbenchStore()
const saveShortcutKbds = useShortcutKbds('app.save')
const activeDetailTab = ref<DetailTab>('rules')
const metadataOpen = ref(false)
const metadataName = ref('')
const metadataDescription = ref('')
const metadataError = ref('')
const fileModalOpen = ref(false)
const fileAction = ref<FileAction>('create')
const filePathDraft = ref('')
const fileActionError = ref('')
const deletePluginOpen = ref(false)
const deleteFileOpen = ref(false)

const detailTabs = computed(() => [
  { label: t('python_plugins.rules'), value: 'rules' },
  { label: t('python_plugins.params'), value: 'params' },
])

const editorLanguage = computed(() => {
  const path = store.selectedFilePath.toLocaleLowerCase()
  if (path.endsWith('.py')) return 'python'
  if (path.endsWith('.json')) return 'json'
  if (path.endsWith('.js')) return 'javascript'
  if (path.endsWith('.html')) return 'html'
  if (path.endsWith('.css')) return 'css'
  if (path.endsWith('.xml')) return 'xml'
  return 'plaintext'
})

const fileTreeRows = computed<FileTreeRow[]>(() => {
  interface Node {
    children: Map<string, Node>
    file: boolean
  }
  const root: Node = { children: new Map(), file: false }
  for (const path of store.files) {
    let node = root
    for (const [index, part] of path.split('/').entries()) {
      let child = node.children.get(part)
      if (!child) {
        child = { children: new Map(), file: false }
        node.children.set(part, child)
      }
      if (index === path.split('/').length - 1) {
        child.file = true
      }
      node = child
    }
  }

  const rows: FileTreeRow[] = []
  const visit = (node: Node, prefix: string, depth: number) => {
    const children = [...node.children.entries()].sort(([leftName, left], [rightName, right]) => {
      const leftDirectory = left.children.size > 0 && !left.file
      const rightDirectory = right.children.size > 0 && !right.file
      if (leftDirectory !== rightDirectory) return leftDirectory ? -1 : 1
      return leftName.localeCompare(rightName)
    })
    for (const [name, child] of children) {
      const path = prefix ? `${prefix}/${name}` : name
      const directory = child.children.size > 0 && !child.file
      rows.push({ key: `${directory ? 'dir' : 'file'}:${path}`, label: name, path, depth, directory })
      if (child.children.size > 0) {
        visit(child, path, depth + 1)
      }
    }
  }
  visit(root, '', 0)
  return rows
})

const validationTone = computed<'neutral' | 'success' | 'warning' | 'error'>(() => {
  switch (store.selectedPlugin?.validationStatus) {
    case 'valid':
      return 'success'
    case 'invalid':
      return 'error'
    case 'unavailable':
      return 'warning'
    default:
      return 'neutral'
  }
})

const validationLabel = computed(() =>
  t(`python_plugins.validation_${store.selectedPlugin?.validationStatus || 'unavailable'}`),
)

const activeRevisionLabel = computed(() => {
  const revision = store.selectedPlugin?.activeRevision ?? ''
  return revision ? revision.slice(0, 12) : t('python_plugins.no_revision')
})

function updateEditor(value: string) {
  store.editorContent = value
  store.editorError = ''
}

async function saveFile(showSuccess = true) {
  try {
    await store.saveCurrentFile()
    if (showSuccess) {
      notify.success(t('python_plugins.file_saved'))
    }
  } catch {
    // The dirty editor and actionable error remain visible in the workspace.
  }
}

async function handleSaveShortcut() {
  if (activeDetailTab.value === 'params' && store.paramsDirty) {
    try {
      await store.saveParams()
      notify.success(t('python_plugins.params_saved'))
    } catch {
      // Params pane shows the validation or persistence error inline.
    }
    return
  }
  if (store.editorDirty) {
    await saveFile()
  }
}

async function validateSelected() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  try {
    if (store.editorDirty) {
      await store.saveCurrentFile()
    }
    await store.validatePlugin(plugin.id)
    notify.success(t('python_plugins.validation_success'))
  } catch (error) {
    notify.error(t('python_plugins.validation_failed', { error: String(error) }))
  }
}

async function reloadSelected() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  try {
    await store.reloadPlugin(plugin.id)
    notify.success(t('python_plugins.reload_success'))
  } catch (error) {
    notify.error(t('python_plugins.reload_failed', { error: String(error) }))
  }
}

async function openPluginDirectory() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  try {
    await store.openPluginDirectory(plugin.id)
  } catch (error) {
    notify.error(t('python_plugins.open_directory_failed', { error: String(error) }))
  }
}

function openMetadata() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  metadataName.value = plugin.name
  metadataDescription.value = plugin.description
  metadataError.value = ''
  metadataOpen.value = true
}

async function saveMetadata() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  if (!metadataName.value.trim()) {
    metadataError.value = t('python_plugins.name_required')
    return
  }
  metadataError.value = ''
  try {
    await store.updatePluginMetadata(
      plugin.id,
      metadataName.value,
      metadataDescription.value,
    )
    metadataOpen.value = false
    notify.success(t('python_plugins.metadata_saved'))
  } catch (error) {
    metadataError.value = String(error)
  }
}

function openFileAction(action: FileAction) {
  fileAction.value = action
  filePathDraft.value = action === 'rename' ? store.selectedFilePath : ''
  fileActionError.value = ''
  fileModalOpen.value = true
}

async function submitFileAction() {
  const path = filePathDraft.value.trim()
  if (!path) {
    fileActionError.value = t('python_plugins.file_path_required')
    return
  }
  fileActionError.value = ''
  try {
    if (fileAction.value === 'create') {
      await store.createFile(path)
    } else {
      await store.renameCurrentFile(path)
    }
    fileModalOpen.value = false
  } catch (error) {
    fileActionError.value = String(error)
  }
}

async function confirmDeleteFile() {
  try {
    await store.deleteCurrentFile()
    deleteFileOpen.value = false
    notify.success(t('python_plugins.file_deleted'))
  } catch (error) {
    notify.error(t('python_plugins.delete_file_failed', { error: String(error) }))
  }
}

async function confirmDeletePlugin() {
  const plugin = store.selectedPlugin
  if (!plugin) return
  try {
    await store.deletePlugin(plugin.id)
    deletePluginOpen.value = false
    notify.success(t('python_plugins.delete_success'))
  } catch (error) {
    notify.error(t('python_plugins.delete_failed', { error: String(error) }))
  }
}

const offSaveShortcut = registerShortcutHandler({
  commandId: 'app.save',
  when: () => workbenchStore.activeContent === 'pythonPlugins',
  enabled: () =>
    (activeDetailTab.value === 'params' && store.paramsDirty) || store.editorDirty,
  run: () => handleSaveShortcut(),
  priority: 20,
})

onMounted(() => {
  void store.initialize()
})

onBeforeUnmount(() => {
  offSaveShortcut()
  store.cleanup()
})
</script>

<template>
  <div class="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-default">
    <UEmpty
      v-if="!store.selectedPlugin"
      icon="i-lucide-file-code-2"
      :title="t('python_plugins.empty')"
      :description="t('python_plugins.empty_hint')"
      class="min-h-0 flex-1"
    />
    <template v-else>
      <div class="flex min-h-11 shrink-0 items-center gap-2 border-b border-default px-3 py-1.5">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <h2 class="truncate text-sm font-semibold text-highlighted">
              {{ store.selectedPlugin.name }}
            </h2>
            <span v-if="store.editorDirty" class="size-1.5 shrink-0 rounded-full bg-primary" />
          </div>
          <p v-if="store.selectedPlugin.description" class="truncate text-xs text-muted">
            {{ store.selectedPlugin.description }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <UTooltip :text="t('python_plugins.save_file')" :kbds="saveShortcutKbds">
            <UButton
              icon="i-lucide-save"
              size="sm"
              :label="t('python_plugins.save')"
              :disabled="!store.editorDirty"
              :loading="store.isMutating(`file:save:${store.selectedPluginId}:${store.selectedFilePath}`)"
              @click="saveFile()"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.validate_hint')">
            <UButton
              icon="i-lucide-circle-check-big"
              color="neutral"
              variant="outline"
              size="sm"
              :label="t('python_plugins.validate')"
              :loading="store.isMutating(`plugin:validate:${store.selectedPluginId}`)"
              @click="validateSelected"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.reload_hint')">
            <UButton
              icon="i-lucide-refresh-cw"
              color="neutral"
              variant="ghost"
              size="sm"
              :aria-label="t('python_plugins.reload')"
              :loading="store.isMutating(`plugin:reload:${store.selectedPluginId}`)"
              @click="reloadSelected"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.rename_plugin')">
            <UButton
              icon="i-lucide-pencil"
              color="neutral"
              variant="ghost"
              size="sm"
              :aria-label="t('python_plugins.rename_plugin')"
              @click="openMetadata"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.open_directory')">
            <UButton
              icon="i-lucide-folder-open"
              color="neutral"
              variant="ghost"
              size="sm"
              :aria-label="t('python_plugins.open_directory')"
              @click="openPluginDirectory"
            />
          </UTooltip>
          <UTooltip :text="t('python_plugins.delete_plugin')">
            <UButton
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              size="sm"
              :aria-label="t('python_plugins.delete_plugin')"
              @click="deletePluginOpen = true"
            />
          </UTooltip>
        </div>
      </div>

      <div class="flex min-h-0 flex-1 overflow-hidden">
        <aside class="flex w-48 shrink-0 flex-col overflow-hidden border-r border-default bg-muted/25">
          <div class="flex h-9 shrink-0 items-center justify-between border-b border-default px-2.5">
            <span class="text-xs font-semibold tracking-wide text-muted uppercase">
              {{ t('python_plugins.files') }}
            </span>
            <div class="flex items-center gap-0.5">
              <UTooltip :text="t('python_plugins.new_file')">
                <UButton
                  icon="i-lucide-file-plus-2"
                  color="neutral"
                  variant="ghost"
                  size="xs"
                  :aria-label="t('python_plugins.new_file')"
                  @click="openFileAction('create')"
                />
              </UTooltip>
              <UTooltip :text="t('python_plugins.rename_file')">
                <UButton
                  icon="i-lucide-pencil"
                  color="neutral"
                  variant="ghost"
                  size="xs"
                  :disabled="!store.selectedFilePath"
                  :aria-label="t('python_plugins.rename_file')"
                  @click="openFileAction('rename')"
                />
              </UTooltip>
              <UTooltip :text="t('python_plugins.delete_file')">
                <UButton
                  icon="i-lucide-trash-2"
                  color="error"
                  variant="ghost"
                  size="xs"
                  :disabled="!store.selectedFilePath"
                  :aria-label="t('python_plugins.delete_file')"
                  @click="deleteFileOpen = true"
                />
              </UTooltip>
            </div>
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto py-1">
            <button
              v-for="row in fileTreeRows"
              :key="row.key"
              type="button"
              class="flex h-7 w-full min-w-0 items-center gap-1.5 pr-2 text-left text-xs"
              :class="[
                row.directory
                  ? 'cursor-default text-muted'
                  : store.selectedFilePath === row.path
                    ? 'bg-primary/10 text-primary'
                    : 'text-highlighted hover:bg-elevated',
              ]"
              :style="{ paddingLeft: `${8 + row.depth * 14}px` }"
              :disabled="row.directory"
              @click="store.selectFile(row.path)"
            >
              <UIcon
                :name="row.directory ? 'i-lucide-folder' : 'i-lucide-file-code-2'"
                class="size-3.5 shrink-0"
              />
              <span class="truncate">{{ row.label }}</span>
              <span
                v-if="!row.directory && row.path === store.selectedFilePath && store.editorDirty"
                class="ml-auto size-1.5 shrink-0 rounded-full bg-primary"
              />
            </button>
          </div>
        </aside>

        <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <div class="relative flex min-h-0 flex-1 overflow-hidden">
            <MonacoBodyEditor
              v-if="store.selectedFilePath"
              :value="store.editorContent"
              :language="editorLanguage"
              :flow-lens-python-api="editorLanguage === 'python'"
              :word-wrap="false"
              :options="{ lineNumbersMinChars: 3, padding: { top: 8, bottom: 8 } }"
              class="min-h-0 flex-1"
              @update:value="updateEditor"
            />
            <UEmpty
              v-else
              icon="i-lucide-file-code-2"
              :title="t('python_plugins.no_file_selected')"
              class="min-h-0 flex-1"
            />
            <div
              v-if="store.editorError"
              class="absolute inset-x-3 bottom-3 z-5 flex items-start gap-2 rounded-md border border-error/30 bg-error/10 px-3 py-2 text-xs text-error shadow-sm"
            >
              <UIcon name="i-lucide-circle-alert" class="mt-0.5 size-3.5 shrink-0" />
              <span class="min-w-0 flex-1 wrap-break-word">{{ store.editorError }}</span>
            </div>
          </div>

          <div class="flex h-[36%] min-h-52 max-h-80 shrink-0 flex-col border-t border-default bg-default">
            <UTabs
              v-model="activeDetailTab"
              :items="detailTabs"
              :content="false"
              variant="link"
              size="sm"
              class="shrink-0 px-2"
            />
            <div class="min-h-0 flex-1 overflow-hidden">
              <PythonPluginRulesPane v-show="activeDetailTab === 'rules'" class="h-full" />
              <PythonPluginParamsPane v-show="activeDetailTab === 'params'" class="h-full" />
            </div>
          </div>
        </div>
      </div>

      <div class="flex h-7 shrink-0 items-center gap-3 border-t border-default bg-muted/30 px-3 text-xs text-muted">
        <UBadge :color="validationTone" variant="subtle" size="sm">
          {{ validationLabel }}
        </UBadge>
        <UTooltip :text="store.selectedPlugin.activeRevision || t('python_plugins.no_revision')">
          <span class="font-mono">
            {{ t('python_plugins.revision', { revision: activeRevisionLabel }) }}
          </span>
        </UTooltip>
        <UTooltip v-if="store.selectedPlugin.validationError" :text="store.selectedPlugin.validationError">
          <span class="min-w-0 flex-1 truncate text-error">
            {{ store.selectedPlugin.validationError }}
          </span>
        </UTooltip>
        <span v-else class="min-w-0 flex-1" />
        <span class="shrink-0">
          {{ store.selectedPlugin.enabled ? t('python_plugins.enabled') : t('python_plugins.disabled') }}
        </span>
      </div>
    </template>

    <UModal
      v-model:open="metadataOpen"
      :title="t('python_plugins.edit_plugin')"
      :close="!store.isMutating(`plugin:update:${store.selectedPluginId}`)"
      :dismissible="!store.isMutating(`plugin:update:${store.selectedPluginId}`)"
    >
      <template #body>
        <div class="space-y-4">
          <UFormField :label="t('python_plugins.name')" required>
            <UInput v-model="metadataName" autofocus class="w-full" @keydown.enter.prevent="saveMetadata" />
          </UFormField>
          <UFormField :label="t('python_plugins.description')">
            <UTextarea v-model="metadataDescription" class="w-full" :rows="3" />
          </UFormField>
          <UAlert v-if="metadataError" color="error" variant="subtle" :description="metadataError" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="outline" :label="t('python_plugins.cancel')" @click="metadataOpen = false" />
          <UButton
            :label="t('python_plugins.save')"
            :loading="store.isMutating(`plugin:update:${store.selectedPluginId}`)"
            @click="saveMetadata"
          />
        </div>
      </template>
    </UModal>

    <UModal
      v-model:open="fileModalOpen"
      :title="fileAction === 'create' ? t('python_plugins.new_file') : t('python_plugins.rename_file')"
    >
      <template #body>
        <div class="space-y-3">
          <UFormField :label="t('python_plugins.file_path')" required :hint="t('python_plugins.file_path_hint')">
            <UInput v-model="filePathDraft" autofocus class="w-full" @keydown.enter.prevent="submitFileAction" />
          </UFormField>
          <UAlert v-if="fileActionError" color="error" variant="subtle" :description="fileActionError" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="outline" :label="t('python_plugins.cancel')" @click="fileModalOpen = false" />
          <UButton :label="t('python_plugins.confirm')" @click="submitFileAction" />
        </div>
      </template>
    </UModal>

    <ConfirmCardModal
      :show="deleteFileOpen"
      :title="t('python_plugins.delete_file')"
      :positive-text="t('python_plugins.delete')"
      :negative-text="t('python_plugins.cancel')"
      positive-type="error"
      :positive-loading="store.isMutating(`file:delete:${store.selectedPluginId}:${store.selectedFilePath}`)"
      @update:show="deleteFileOpen = $event"
      @positive-click="confirmDeleteFile"
    >
      {{ t('python_plugins.delete_file_confirm', { path: store.selectedFilePath }) }}
    </ConfirmCardModal>

    <ConfirmCardModal
      :show="deletePluginOpen"
      :title="t('python_plugins.delete_plugin')"
      :positive-text="t('python_plugins.delete')"
      :negative-text="t('python_plugins.cancel')"
      positive-type="error"
      :positive-loading="store.isMutating(`plugin:delete:${store.selectedPluginId}`)"
      @update:show="deletePluginOpen = $event"
      @positive-click="confirmDeletePlugin"
    >
      {{ t('python_plugins.delete_plugin_confirm', { name: store.selectedPlugin?.name ?? '' }) }}
    </ConfirmCardModal>
  </div>
</template>
