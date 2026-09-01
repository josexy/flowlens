export type ShortcutModifier = 'primary' | 'control' | 'alt' | 'shift' | 'super'
export type ShortcutScope = 'application' | 'global'

export interface ShortcutBinding {
  modifiers: ShortcutModifier[]
  key: string
}

export interface ShortcutOverride {
  binding: ShortcutBinding | null
  scope: ShortcutScope
}

export interface ShortcutConfig {
  overrides: Record<string, ShortcutOverride | undefined>
}

export type ShortcutModalPolicy = 'allow' | 'block'
export type ShortcutEditablePolicy = 'allow' | 'block'

export interface ShortcutCommand {
  id: string
  categoryKey: string
  labelKey: string
  defaultBinding: ShortcutBinding | null
  defaultScope: ShortcutScope
  modalPolicy: ShortcutModalPolicy
  editablePolicy: ShortcutEditablePolicy
  globalCapable: boolean
}

export interface ResolvedShortcutBinding {
  key: string
  modifiers: Array<'control' | 'alt' | 'shift' | 'super'>
}

export interface ResolvedShortcut {
  command: ShortcutCommand
  binding: ShortcutBinding | null
  scope: ShortcutScope
}

export type ShortcutInvocationSource = 'keyboard' | 'global'

export interface ShortcutInvocationContext {
  source: ShortcutInvocationSource
}

export interface ShortcutHandler {
  commandId: string
  when?: boolean | ((context: ShortcutInvocationContext) => boolean)
  enabled?: boolean | ((context: ShortcutInvocationContext) => boolean)
  run: (context: ShortcutInvocationContext) => void | Promise<void>
  priority?: number
}

export type ShortcutRecordingResult =
  | { type: 'binding'; binding: ShortcutBinding }
  | { type: 'clear' }
  | { type: 'cancel' }
