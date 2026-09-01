import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { Events } from '@wailsio/runtime'
import {
  CreatePlugin,
  CreateRule,
  DeletePlugin,
  DeletePluginFile,
  DeleteRule,
  GetPlugin,
  GetRuntimeStatus,
  ListPluginFiles,
  ListPlugins,
  OpenPluginDirectory,
  OpenPluginsDirectory,
  ReadPluginFile,
  ReloadPlugin,
  RenamePluginFile,
  ReorderPlugins,
  ReorderRules,
  SetPluginEnabled,
  UpdatePlugin,
  UpdatePluginParams,
  UpdateRule,
  ValidatePlugin,
  WritePluginFile,
} from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/pythonpluginservice'
import type {
  CreatePluginInput,
  Plugin,
  Rule,
  RuntimeStatus,
  UpdateRuleInput,
} from '#bindings/github.com/josexy/flowlens/backend/services/python_plugin_service/models'

const REGISTRY_EVENT = 'python-plugins:registry'
const STATUS_EVENT = 'python-plugins:status'

interface RegistryEvent {
  eventId: number
  change: string
  pluginId?: string
}

interface StatusEvent {
  eventId: number
  status: RuntimeStatus
}

interface EditorDraft {
  content: string
  savedContent: string
  error: string
}

function displayError(error: unknown) {
  return error instanceof Error && error.message ? error.message : String(error)
}

function compactRules(rules: (Rule | null)[] | null | undefined) {
  return (rules ?? [])
    .filter((rule): rule is Rule => Boolean(rule))
    .sort((left, right) => left.sortOrder - right.sortOrder || left.id.localeCompare(right.id))
}

function normalizePlugin(plugin: Plugin): Plugin {
  return {
    ...plugin,
    rules: compactRules(plugin.rules),
  }
}

function prettyParams(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function validateParamsObject(value: string) {
  const decoded: unknown = JSON.parse(value)
  if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) {
    throw new Error('plugin params must be a JSON object')
  }
}

function mutationKey(kind: string, ...parts: string[]) {
  return [kind, ...parts].join(':')
}

export const usePythonPluginsStore = defineStore('pythonPlugins', () => {
  const plugins = ref<Plugin[]>([])
  const selectedPluginId = ref('')
  const files = ref<string[]>([])
  const selectedFilePath = ref('')
  const editorContent = ref('')
  const savedEditorContent = ref('')
  const editorError = ref('')
  const editorPluginId = ref('')
  const paramsDraft = ref('{}')
  const savedParamsDraft = ref('{}')
  const paramsError = ref('')
  const runtimeStatus = ref<RuntimeStatus | null>(null)
  const loadingPlugins = ref(false)
  const loadingWorkspace = ref(false)
  const error = ref('')
  const activeMutations = ref<Set<string>>(new Set())

  const selectedPlugin = computed(
    () => plugins.value.find((plugin) => plugin.id === selectedPluginId.value) ?? null,
  )
  const editorDirty = computed(
    () => Boolean(selectedFilePath.value) && editorContent.value !== savedEditorContent.value,
  )
  const paramsDirty = computed(() => paramsDraft.value !== savedParamsDraft.value)

  const selectedFiles = new Map<string, string>()
  const editorDrafts = new Map<string, EditorDraft>()
  const paramsDrafts = new Map<string, EditorDraft>()
  const mutationPromises = new Map<string, Promise<unknown>>()

  let pluginListSequence = 0
  let pluginSequence = 0
  let filesSequence = 0
  let fileSequence = 0
  let statusSequence = 0
  let consumerCount = 0
  let initializationPromise: Promise<void> | null = null
  let registryRefreshPromise: Promise<void> | null = null
  const pendingRegistryEvents: RegistryEvent[] = []
  let offRegistry: (() => void) | null = null
  let offStatus: (() => void) | null = null
  let lastRegistryEventId = 0
  let lastStatusEventId = 0

  function isMutating(key: string) {
    return activeMutations.value.has(key)
  }

  function setMutationActive(key: string, active: boolean) {
    const next = new Set(activeMutations.value)
    if (active) {
      next.add(key)
    } else {
      next.delete(key)
    }
    activeMutations.value = next
  }

  function runMutation<T>(key: string, action: () => Promise<T>): Promise<T> {
    const existing = mutationPromises.get(key)
    if (existing) {
      return existing as Promise<T>
    }
    const promise = (async () => {
      setMutationActive(key, true)
      try {
        return await action()
      } finally {
        mutationPromises.delete(key)
        setMutationActive(key, false)
      }
    })()
    mutationPromises.set(key, promise)
    return promise
  }

  function upsertPlugin(value: Plugin | null | undefined) {
    if (!value) {
      return null
    }
    const plugin = normalizePlugin(value)
    const next = plugins.value.filter((item) => item.id !== plugin.id)
    next.push(plugin)
    plugins.value = next.sort(
      (left, right) => left.sortOrder - right.sortOrder || left.id.localeCompare(right.id),
    )
    return plugin
  }

  function stashEditorDraft() {
    if (!editorPluginId.value || !selectedFilePath.value) {
      return
    }
    editorDrafts.set(`${editorPluginId.value}\0${selectedFilePath.value}`, {
      content: editorContent.value,
      savedContent: savedEditorContent.value,
      error: editorError.value,
    })
  }

  function stashParamsDraft(pluginId = selectedPluginId.value) {
    if (!pluginId) {
      return
    }
    paramsDrafts.set(pluginId, {
      content: paramsDraft.value,
      savedContent: savedParamsDraft.value,
      error: paramsError.value,
    })
  }

  function restoreParamsDraft(plugin: Plugin) {
    const draft = paramsDrafts.get(plugin.id)
    if (draft) {
      paramsDraft.value = draft.content
      savedParamsDraft.value = draft.savedContent
      paramsError.value = draft.error
      return
    }
    const value = prettyParams(plugin.paramsJson)
    paramsDraft.value = value
    savedParamsDraft.value = value
    paramsError.value = ''
  }

  function clearEditor() {
    selectedFilePath.value = ''
    editorPluginId.value = ''
    editorContent.value = ''
    savedEditorContent.value = ''
    editorError.value = ''
  }

  async function loadPlugins() {
    const sequence = ++pluginListSequence
    loadingPlugins.value = true
    error.value = ''
    try {
      const values = (await ListPlugins()) ?? []
      if (sequence !== pluginListSequence) {
        return
      }
      plugins.value = values
        .filter((plugin): plugin is Plugin => Boolean(plugin))
        .map(normalizePlugin)
        .sort(
          (left, right) =>
            left.sortOrder - right.sortOrder || left.id.localeCompare(right.id),
        )
      const currentStillExists = plugins.value.some(
        (plugin) => plugin.id === selectedPluginId.value,
      )
      if (!currentStillExists) {
        stashEditorDraft()
        stashParamsDraft()
        selectedPluginId.value = plugins.value[0]?.id ?? ''
        if (!selectedPluginId.value) {
          files.value = []
          clearEditor()
          paramsDraft.value = '{}'
          savedParamsDraft.value = '{}'
          paramsError.value = ''
        }
      }
    } catch (loadError) {
      if (sequence === pluginListSequence) {
        error.value = displayError(loadError)
      }
      throw loadError
    } finally {
      if (sequence === pluginListSequence) {
        loadingPlugins.value = false
      }
    }
  }

  async function loadPlugin(pluginId = selectedPluginId.value) {
    if (!pluginId) {
      return null
    }
    const sequence = ++pluginSequence
    const value = await GetPlugin(pluginId)
    if (sequence !== pluginSequence || selectedPluginId.value !== pluginId) {
      return null
    }
    const plugin = upsertPlugin(value)
    if (plugin) {
      const currentDraft = paramsDrafts.get(plugin.id)
      const currentParamsAreDirty =
        selectedPluginId.value === plugin.id && paramsDraft.value !== savedParamsDraft.value
      if (
        !currentParamsAreDirty &&
        (!currentDraft || currentDraft.content === currentDraft.savedContent)
      ) {
        const nextValue = prettyParams(plugin.paramsJson)
        paramsDraft.value = nextValue
        savedParamsDraft.value = nextValue
        paramsError.value = ''
        paramsDrafts.delete(plugin.id)
      }
    }
    return plugin
  }

  async function loadFile(pluginId: string, relativePath: string, force = false) {
    const sequence = ++fileSequence
    if (!pluginId || !relativePath) {
      clearEditor()
      return
    }
    const key = `${pluginId}\0${relativePath}`
    const draft = editorDrafts.get(key)
    if (draft && !force) {
      if (sequence === fileSequence && selectedPluginId.value === pluginId) {
        editorPluginId.value = pluginId
        selectedFilePath.value = relativePath
        editorContent.value = draft.content
        savedEditorContent.value = draft.savedContent
        editorError.value = draft.error
      }
      return
    }
    const value = await ReadPluginFile(pluginId, relativePath)
    if (
      sequence !== fileSequence ||
      selectedPluginId.value !== pluginId ||
      selectedFiles.get(pluginId) !== relativePath
    ) {
      return
    }
    if (!value) {
      throw new Error('read plugin file returned no content')
    }
    editorDrafts.delete(key)
    editorPluginId.value = pluginId
    selectedFilePath.value = value.path
    editorContent.value = value.content
    savedEditorContent.value = value.content
    editorError.value = ''
  }

  async function loadFiles(pluginId = selectedPluginId.value, reloadCleanFile = false) {
    if (!pluginId) {
      files.value = []
      clearEditor()
      return
    }
    const sequence = ++filesSequence
    const values = (await ListPluginFiles(pluginId)) ?? []
    if (sequence !== filesSequence || selectedPluginId.value !== pluginId) {
      return
    }
    files.value = [...values].sort((left, right) => left.localeCompare(right))
    let path = selectedFiles.get(pluginId) ?? ''
    if (!files.value.includes(path)) {
      path = files.value.includes('main.py') ? 'main.py' : (files.value[0] ?? '')
      selectedFiles.set(pluginId, path)
    }
    if (!path) {
      clearEditor()
      return
    }
    const currentKey = `${pluginId}\0${path}`
    const currentDraft = editorDrafts.get(currentKey)
    const currentEditorIsDirty =
      editorPluginId.value === pluginId &&
      selectedFilePath.value === path &&
      editorContent.value !== savedEditorContent.value
    const shouldForce =
      reloadCleanFile &&
      !currentEditorIsDirty &&
      (!currentDraft || currentDraft.content === currentDraft.savedContent)
    await loadFile(pluginId, path, shouldForce)
  }

  async function loadWorkspace(pluginId = selectedPluginId.value) {
    if (!pluginId) {
      return
    }
    loadingWorkspace.value = true
    try {
      await Promise.all([loadPlugin(pluginId), loadFiles(pluginId)])
    } finally {
      if (selectedPluginId.value === pluginId) {
        loadingWorkspace.value = false
      }
    }
  }

  async function selectPlugin(pluginId: string) {
    if (!pluginId || pluginId === selectedPluginId.value) {
      return
    }
    stashEditorDraft()
    stashParamsDraft()
    selectedPluginId.value = pluginId
    files.value = []
    clearEditor()
    const plugin = plugins.value.find((item) => item.id === pluginId)
    if (plugin) {
      restoreParamsDraft(plugin)
    }
    await loadWorkspace(pluginId)
  }

  async function selectFile(relativePath: string) {
    const pluginId = selectedPluginId.value
    if (!pluginId || !relativePath || relativePath === selectedFilePath.value) {
      return
    }
    stashEditorDraft()
    selectedFiles.set(pluginId, relativePath)
    await loadFile(pluginId, relativePath)
  }

  async function loadRuntimeStatus() {
    const sequence = ++statusSequence
    const value = await GetRuntimeStatus()
    if (sequence === statusSequence) {
      runtimeStatus.value = value
    }
  }

  async function refreshFromRegistry(event: RegistryEvent) {
    pendingRegistryEvents.push(event)
    if (registryRefreshPromise) {
      await registryRefreshPromise
      return
    }
    registryRefreshPromise = (async () => {
      try {
        while (pendingRegistryEvents.length > 0) {
          const events = pendingRegistryEvents.splice(0)
          await loadPlugins()
          const selectedId = selectedPluginId.value
          if (selectedId && events.some((item) => item.pluginId === selectedId)) {
            if (
              events.some(
                (item) => item.pluginId === selectedId && item.change === 'files_changed',
              )
            ) {
              await loadFiles(selectedId, true)
            } else {
              await loadPlugin(selectedId)
            }
          }
        }
      } catch {
        // The initiating mutation reports its own actionable error.
      } finally {
        registryRefreshPromise = null
      }
    })()
    await registryRefreshPromise
  }

  function registerEvents() {
    if (!offRegistry) {
      offRegistry = Events.On(REGISTRY_EVENT, (rawEvent) => {
        const event = rawEvent.data as RegistryEvent
        if (!event || event.eventId <= lastRegistryEventId) {
          return
        }
        lastRegistryEventId = event.eventId
        void refreshFromRegistry(event)
      })
    }
    if (!offStatus) {
      offStatus = Events.On(STATUS_EVENT, (rawEvent) => {
        const event = rawEvent.data as StatusEvent
        if (!event || event.eventId <= lastStatusEventId) {
          return
        }
        lastStatusEventId = event.eventId
        runtimeStatus.value = event.status
      })
    }
  }

  async function initialize() {
    consumerCount += 1
    registerEvents()
    if (!initializationPromise) {
      initializationPromise = (async () => {
        const results = await Promise.allSettled([loadPlugins(), loadRuntimeStatus()])
        const rejection = results.find(
          (result): result is PromiseRejectedResult => result.status === 'rejected',
        )
        if (rejection) {
          error.value = displayError(rejection.reason)
        }
        if (selectedPluginId.value) {
          await loadWorkspace(selectedPluginId.value).catch((loadError) => {
            error.value = displayError(loadError)
          })
        }
      })().finally(() => {
        initializationPromise = null
      })
    }
    await initializationPromise
  }

  function cleanup() {
    consumerCount = Math.max(0, consumerCount - 1)
    if (consumerCount > 0) {
      return
    }
    offRegistry?.()
    offStatus?.()
    offRegistry = null
    offStatus = null
  }

  async function createPlugin(input: Pick<CreatePluginInput, 'name' | 'description'>) {
    return runMutation('plugin:create', async () => {
      const value = await CreatePlugin({
        id: '',
        name: input.name,
        description: input.description,
        paramsJson: '{}',
      })
      const plugin = upsertPlugin(value)
      if (!plugin) {
        throw new Error('create plugin returned no plugin')
      }
      await selectPlugin(plugin.id)
      return plugin
    })
  }

  async function updatePluginMetadata(pluginId: string, name: string, description: string) {
    return runMutation(mutationKey('plugin:update', pluginId), async () => {
      const current = plugins.value.find((plugin) => plugin.id === pluginId)
      if (!current) {
        throw new Error('plugin not found')
      }
      const plugin = upsertPlugin(
        await UpdatePlugin(pluginId, {
          name,
          description,
          paramsJson: current.paramsJson,
        }),
      )
      if (!plugin) {
        throw new Error('update plugin returned no plugin')
      }
      return plugin
    })
  }

  async function deletePlugin(pluginId: string) {
    return runMutation(mutationKey('plugin:delete', pluginId), async () => {
      await DeletePlugin(pluginId)
      plugins.value = plugins.value.filter((plugin) => plugin.id !== pluginId)
      for (const key of editorDrafts.keys()) {
        if (key.startsWith(`${pluginId}\0`)) {
          editorDrafts.delete(key)
        }
      }
      paramsDrafts.delete(pluginId)
      selectedFiles.delete(pluginId)
      if (selectedPluginId.value === pluginId) {
        selectedPluginId.value = plugins.value[0]?.id ?? ''
        files.value = []
        clearEditor()
        if (selectedPluginId.value) {
          const selected = plugins.value[0]!
          restoreParamsDraft(selected)
          await loadWorkspace(selected.id)
        }
      }
    })
  }

  async function setPluginEnabled(pluginId: string, enabled: boolean) {
    return runMutation(mutationKey('plugin:enabled', pluginId), async () => {
      try {
        const plugin = upsertPlugin(await SetPluginEnabled(pluginId, enabled))
        if (!plugin) {
          throw new Error('update plugin state returned no plugin')
        }
        return plugin
      } catch (toggleError) {
        await loadPlugins().catch(() => {})
        throw toggleError
      }
    })
  }

  function setPluginOrderLocal(ordered: Plugin[]) {
    plugins.value = ordered.map((plugin, sortOrder) => ({ ...plugin, sortOrder }))
  }

  async function reorderPluginList(pluginIds: string[]) {
    const previous = [...plugins.value]
    const byId = new Map(plugins.value.map((plugin) => [plugin.id, plugin]))
    setPluginOrderLocal(
      pluginIds.map((pluginId) => byId.get(pluginId)).filter((plugin): plugin is Plugin => Boolean(plugin)),
    )
    try {
      await runMutation('plugin:reorder', () => ReorderPlugins(pluginIds))
    } catch (reorderError) {
      await loadPlugins().catch(() => {
        plugins.value = previous
      })
      throw reorderError
    }
  }

  async function validatePlugin(pluginId: string) {
    return runMutation(mutationKey('plugin:validate', pluginId), async () => {
      try {
        const plugin = upsertPlugin(await ValidatePlugin(pluginId))
        if (!plugin) {
          throw new Error('validation returned no plugin')
        }
        return plugin
      } catch (validationError) {
        await loadPlugins().catch(() => {})
        throw validationError
      }
    })
  }

  async function reloadPlugin(pluginId: string) {
    return runMutation(mutationKey('plugin:reload', pluginId), async () => {
      try {
        const plugin = upsertPlugin(await ReloadPlugin(pluginId))
        if (!plugin) {
          throw new Error('reload returned no plugin')
        }
        return plugin
      } catch (reloadError) {
        await loadPlugins().catch(() => {})
        throw reloadError
      }
    })
  }

  async function saveCurrentFile() {
    const pluginId = selectedPluginId.value
    const relativePath = selectedFilePath.value
    if (!pluginId || !relativePath) {
      throw new Error('select a plugin file before saving')
    }
    return runMutation(mutationKey('file:save', pluginId, relativePath), async () => {
      const contentToSave = editorContent.value
      const savedContentBeforeSave = savedEditorContent.value
      const draftKey = `${pluginId}\0${relativePath}`
      editorError.value = ''
      try {
        const plugin = upsertPlugin(
          await WritePluginFile(pluginId, relativePath, contentToSave),
        )
        if (!plugin) {
          throw new Error('save returned no plugin')
        }
        if (selectedPluginId.value === pluginId && selectedFilePath.value === relativePath) {
          savedEditorContent.value = contentToSave
          editorDrafts.set(draftKey, {
            content: editorContent.value,
            savedContent: contentToSave,
            error: '',
          })
        } else {
          const draft = editorDrafts.get(draftKey)
          if (draft) {
            editorDrafts.set(draftKey, {
              content: draft.content,
              savedContent: contentToSave,
              error: '',
            })
          }
        }
        return plugin
      } catch (saveError) {
        const message = displayError(saveError)
        if (selectedPluginId.value === pluginId && selectedFilePath.value === relativePath) {
          editorError.value = message
          stashEditorDraft()
        } else {
          const draft = editorDrafts.get(draftKey)
          editorDrafts.set(draftKey, {
            content: draft?.content ?? contentToSave,
            savedContent: draft?.savedContent ?? savedContentBeforeSave,
            error: message,
          })
        }
        throw saveError
      }
    })
  }

  async function createFile(relativePath: string) {
    const pluginId = selectedPluginId.value
    if (!pluginId) {
      throw new Error('select a plugin before creating a file')
    }
    return runMutation(mutationKey('file:create', pluginId, relativePath), async () => {
      const plugin = upsertPlugin(await WritePluginFile(pluginId, relativePath, ''))
      if (!plugin) {
        throw new Error('create file returned no plugin')
      }
      selectedFiles.set(pluginId, relativePath)
      editorDrafts.delete(`${pluginId}\0${relativePath}`)
      await loadFiles(pluginId)
      return plugin
    })
  }

  async function renameCurrentFile(nextRelativePath: string) {
    const pluginId = selectedPluginId.value
    const currentPath = selectedFilePath.value
    if (!pluginId || !currentPath) {
      throw new Error('select a plugin file before renaming')
    }
    return runMutation(mutationKey('file:rename', pluginId, currentPath), async () => {
      stashEditorDraft()
      const plugin = upsertPlugin(
        await RenamePluginFile(pluginId, currentPath, nextRelativePath),
      )
      if (!plugin) {
        throw new Error('rename file returned no plugin')
      }
      const draft = editorDrafts.get(`${pluginId}\0${currentPath}`)
      editorDrafts.delete(`${pluginId}\0${currentPath}`)
      if (draft) {
        editorDrafts.set(`${pluginId}\0${nextRelativePath}`, draft)
      }
      selectedFiles.set(pluginId, nextRelativePath)
      await loadFiles(pluginId)
      return plugin
    })
  }

  async function deleteCurrentFile() {
    const pluginId = selectedPluginId.value
    const relativePath = selectedFilePath.value
    if (!pluginId || !relativePath) {
      throw new Error('select a plugin file before deleting')
    }
    return runMutation(mutationKey('file:delete', pluginId, relativePath), async () => {
      const plugin = upsertPlugin(await DeletePluginFile(pluginId, relativePath))
      if (!plugin) {
        throw new Error('delete file returned no plugin')
      }
      editorDrafts.delete(`${pluginId}\0${relativePath}`)
      selectedFiles.delete(pluginId)
      await loadFiles(pluginId)
      return plugin
    })
  }

  async function saveParams() {
    const pluginId = selectedPluginId.value
    if (!pluginId) {
      throw new Error('select a plugin before saving params')
    }
    return runMutation(mutationKey('params:save', pluginId), async () => {
      const paramsToSave = paramsDraft.value
      const savedParamsBeforeSave = savedParamsDraft.value
      paramsError.value = ''
      try {
        validateParamsObject(paramsToSave)
        const plugin = upsertPlugin(await UpdatePluginParams(pluginId, paramsToSave))
        if (!plugin) {
          throw new Error('save params returned no plugin')
        }
        const value = prettyParams(plugin.paramsJson)
        if (selectedPluginId.value === pluginId) {
          if (paramsDraft.value === paramsToSave) {
            paramsDraft.value = value
          }
          savedParamsDraft.value = value
          if (paramsDraft.value === value) {
            paramsDrafts.delete(pluginId)
          } else {
            stashParamsDraft(pluginId)
          }
        } else {
          const draft = paramsDrafts.get(pluginId)
          if (draft) {
            const content = draft.content === paramsToSave ? value : draft.content
            if (content === value) {
              paramsDrafts.delete(pluginId)
            } else {
              paramsDrafts.set(pluginId, {
                content,
                savedContent: value,
                error: '',
              })
            }
          }
        }
        return plugin
      } catch (saveError) {
        const message = displayError(saveError)
        if (selectedPluginId.value === pluginId) {
          paramsError.value = message
          stashParamsDraft(pluginId)
        } else {
          const draft = paramsDrafts.get(pluginId)
          paramsDrafts.set(pluginId, {
            content: draft?.content ?? paramsToSave,
            savedContent: draft?.savedContent ?? savedParamsBeforeSave,
            error: message,
          })
        }
        throw saveError
      }
    })
  }

  function replaceRules(pluginId: string, rules: Rule[]) {
    const plugin = plugins.value.find((item) => item.id === pluginId)
    if (!plugin) {
      return
    }
    upsertPlugin({ ...plugin, rules })
  }

  async function createRule(pluginId: string) {
    return runMutation(mutationKey('rule:create', pluginId), async () => {
      const rule = await CreateRule(pluginId, {
        id: '',
        enabled: true,
        method: '*',
        urlPattern: '*',
      })
      if (!rule) {
        throw new Error('create rule returned no rule')
      }
      const current = compactRules(
        plugins.value.find((plugin) => plugin.id === pluginId)?.rules,
      )
      replaceRules(pluginId, [...current, rule])
      return rule
    })
  }

  async function updateRule(pluginId: string, ruleId: string, input: UpdateRuleInput) {
    return runMutation(mutationKey('rule:update', pluginId, ruleId), async () => {
      const rule = await UpdateRule(pluginId, ruleId, input)
      if (!rule) {
        throw new Error('update rule returned no rule')
      }
      replaceRules(
        pluginId,
        compactRules(plugins.value.find((plugin) => plugin.id === pluginId)?.rules).map(
          (item) => (item.id === rule.id ? rule : item),
        ),
      )
      return rule
    })
  }

  async function deleteRule(pluginId: string, ruleId: string) {
    return runMutation(mutationKey('rule:delete', pluginId, ruleId), async () => {
      await DeleteRule(pluginId, ruleId)
      replaceRules(
        pluginId,
        compactRules(plugins.value.find((plugin) => plugin.id === pluginId)?.rules).filter(
          (rule) => rule.id !== ruleId,
        ),
      )
    })
  }

  async function reorderRuleList(pluginId: string, ruleIds: string[]) {
    const previous = compactRules(
      plugins.value.find((plugin) => plugin.id === pluginId)?.rules,
    )
    const byId = new Map(previous.map((rule) => [rule.id, rule]))
    replaceRules(
      pluginId,
      ruleIds
        .map((ruleId, sortOrder) => {
          const rule = byId.get(ruleId)
          return rule ? { ...rule, sortOrder } : null
        })
        .filter((rule): rule is Rule => Boolean(rule)),
    )
    try {
      await runMutation(mutationKey('rule:reorder', pluginId), () =>
        ReorderRules(pluginId, ruleIds),
      )
    } catch (reorderError) {
      replaceRules(pluginId, previous)
      throw reorderError
    }
  }

  async function openPluginDirectory(pluginId: string) {
    await OpenPluginDirectory(pluginId)
  }

  async function openPluginsDirectory() {
    await OpenPluginsDirectory()
  }

  return {
    plugins,
    selectedPluginId,
    selectedPlugin,
    files,
    selectedFilePath,
    editorContent,
    savedEditorContent,
    editorError,
    editorDirty,
    paramsDraft,
    savedParamsDraft,
    paramsError,
    paramsDirty,
    runtimeStatus,
    loadingPlugins,
    loadingWorkspace,
    error,
    activeMutations,
    isMutating,
    initialize,
    cleanup,
    loadPlugins,
    loadRuntimeStatus,
    selectPlugin,
    selectFile,
    createPlugin,
    updatePluginMetadata,
    deletePlugin,
    setPluginEnabled,
    setPluginOrderLocal,
    reorderPluginList,
    validatePlugin,
    reloadPlugin,
    saveCurrentFile,
    createFile,
    renameCurrentFile,
    deleteCurrentFile,
    saveParams,
    createRule,
    updateRule,
    deleteRule,
    reorderRuleList,
    openPluginDirectory,
    openPluginsDirectory,
  }
})
