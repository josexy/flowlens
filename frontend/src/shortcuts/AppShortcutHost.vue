<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { Events } from '@wailsio/runtime'
import { shortcutCatalog } from './catalog'
import { bindingMatchesEvent, isModifierOnlyKey, resolveShortcut } from './binding'
import { shortcutRecordingCoordinator } from './recording'
import { shortcutHandlerRegistry } from './registry'
import { useSettingStore } from '@/stores/setting'
import {
  SHORTCUT_INVOKE_EVENT,
  parseShortcutInvokePayload,
} from '@/runtime/appEvents'

const settingStore = useSettingStore()
let offShortcutInvoke: (() => void) | null = null

function hasOpenModal() {
  return Boolean(
    document.querySelector(
      '[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"], dialog[open], [aria-modal="true"]:not([data-state="closed"])',
    ),
  )
}

function isEditableTarget(target: EventTarget | null) {
  if (!(target instanceof Element)) return false
  return Boolean(
    target.closest(
      'input, textarea, select, [contenteditable]:not([contenteditable="false"]), .monaco-editor',
    ),
  )
}

function consumeEvent(event: KeyboardEvent) {
  event.preventDefault()
  event.stopPropagation()
}

function handleKeydown(event: KeyboardEvent) {
  if (event.repeat || event.isComposing || ['Dead', 'Process', 'Unidentified'].includes(event.key)) {
    return
  }

  if (shortcutRecordingCoordinator.consume(event)) {
    consumeEvent(event)
    return
  }

  if (isModifierOnlyKey(event.key)) return

  const modalOpen = hasOpenModal()
  const editable = isEditableTarget(event.target)
  const commandIds: string[] = []
  for (const command of shortcutCatalog) {
    const resolved = resolveShortcut(command, settingStore.shortcutConfig)
    // Global shortcuts are registered with the OS and must not also run locally.
    if (resolved.scope === 'global' || !resolved.binding) {
      continue
    }
    if (modalOpen && command.modalPolicy === 'block') continue
    if (editable && command.editablePolicy === 'block') continue
    if (bindingMatchesEvent(resolved.binding, event)) commandIds.push(command.id)
  }

  if (commandIds.length === 0) return
  const context = { source: 'keyboard' } as const
  const handler = shortcutHandlerRegistry.select(commandIds, context)
  if (!handler) return
  consumeEvent(event)
  shortcutHandlerRegistry.invokeSelected(handler, context)
}

function listenShortcutInvocations() {
  if (offShortcutInvoke) return
  offShortcutInvoke = Events.On(SHORTCUT_INVOKE_EVENT, (event) => {
    const payload = parseShortcutInvokePayload(event.data)
    if (!payload) return
    const command = shortcutCatalog.find((item) => item.id === payload.commandId)
    if (hasOpenModal() && command?.modalPolicy === 'block') return
    shortcutHandlerRegistry.invoke([payload.commandId], { source: 'global' })
  })
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown, { capture: true })
  listenShortcutInvocations()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeydown, { capture: true })
  offShortcutInvoke?.()
  offShortcutInvoke = null
})
</script>

<template><slot /></template>
