import type {
  ResolvedShortcut,
  ResolvedShortcutBinding,
  ShortcutBinding,
  ShortcutCommand,
  ShortcutConfig,
  ShortcutModifier,
} from './types'

const modifierOrder: ShortcutModifier[] = ['primary', 'control', 'alt', 'shift', 'super']
const modifierKeys = new Set([
  'Control',
  'Alt',
  'Shift',
  'Meta',
  'OS',
  'Super',
  'Hyper',
  'AltGraph',
  'CapsLock',
  'NumLock',
  'ScrollLock',
  'Fn',
  'FnLock',
  'Symbol',
  'SymbolLock',
])
const namedKeyAliases: Record<string, string> = {
  Esc: 'Escape',
  Spacebar: ' ',
}

export function isMacOS(): boolean {
  if (typeof navigator === 'undefined') return false
  return /Mac|iPhone|iPad|iPod/i.test(navigator.userAgent ?? navigator.platform ?? '')
}

export function normalizeShortcutKey(key: string | null | undefined): string {
  if (key === ' ') return ' '
  const value = key?.trim()
  if (!value) return ''
  const aliased = namedKeyAliases[value] ?? value
  return /^[A-Z]$/i.test(aliased) ? aliased.toLowerCase() : aliased
}

export function isModifierOnlyKey(key: string | null | undefined): boolean {
  return modifierKeys.has(key ?? '')
}

export function normalizeShortcutBinding(
  binding: ShortcutBinding | null | undefined,
): ShortcutBinding | null {
  if (!binding) return null
  const key = normalizeShortcutKey(binding.key)
  if (!key) return null
  const modifiers = modifierOrder.filter((modifier) => binding.modifiers?.includes(modifier))
  return { key, modifiers }
}

function resolvedModifier(modifier: ShortcutModifier, macOS: boolean) {
  if (modifier === 'primary') return macOS ? 'super' : 'control'
  if (modifier === 'super') return 'super'
  return modifier
}

export function resolveShortcutBinding(
  binding: ShortcutBinding | null | undefined,
  macOS = isMacOS(),
): ResolvedShortcutBinding | null {
  const normalized = normalizeShortcutBinding(binding)
  if (!normalized) return null
  const modifiers = new Set<ResolvedShortcutBinding['modifiers'][number]>()
  normalized.modifiers.forEach((modifier) => modifiers.add(resolvedModifier(modifier, macOS)))
  return {
    key: normalized.key,
    modifiers: (['control', 'alt', 'shift', 'super'] as const).filter((modifier) =>
      modifiers.has(modifier),
    ),
  }
}

export function bindingMatchesEvent(
  binding: ShortcutBinding | null | undefined,
  event: KeyboardEvent,
  macOS = isMacOS(),
): boolean {
  const resolved = resolveShortcutBinding(binding, macOS)
  if (!resolved || normalizeShortcutKey(event.key) !== resolved.key) return false
  return (
    event.ctrlKey === resolved.modifiers.includes('control') &&
    event.altKey === resolved.modifiers.includes('alt') &&
    event.shiftKey === resolved.modifiers.includes('shift') &&
    event.metaKey === resolved.modifiers.includes('super')
  )
}

export function shortcutBindingsEqual(
  left: ShortcutBinding | null | undefined,
  right: ShortcutBinding | null | undefined,
  macOS = isMacOS(),
): boolean {
  const resolvedLeft = resolveShortcutBinding(left, macOS)
  const resolvedRight = resolveShortcutBinding(right, macOS)
  if (!resolvedLeft || !resolvedRight) return resolvedLeft === resolvedRight
  return (
    resolvedLeft.key === resolvedRight.key &&
    resolvedLeft.modifiers.length === resolvedRight.modifiers.length &&
    resolvedLeft.modifiers.every((modifier, index) => modifier === resolvedRight.modifiers[index])
  )
}

export function shortcutBindingsConflict(
  left: ShortcutBinding | null | undefined,
  right: ShortcutBinding | null | undefined,
  macOS = isMacOS(),
): boolean {
  return Boolean(
    normalizeShortcutBinding(left) &&
      normalizeShortcutBinding(right) &&
      shortcutBindingsEqual(left, right, macOS),
  )
}

export function bindingFromKeyboardEvent(event: KeyboardEvent, macOS = isMacOS()): ShortcutBinding | null {
  const key = normalizeShortcutKey(event.key)
  if (!key || isModifierOnlyKey(key)) return null
  const modifiers: ShortcutModifier[] = []
  if (macOS) {
    if (event.metaKey) modifiers.push('primary')
    if (event.ctrlKey) modifiers.push('control')
  } else {
    if (event.ctrlKey) modifiers.push('primary')
    if (event.metaKey) modifiers.push('super')
  }
  if (event.altKey) modifiers.push('alt')
  if (event.shiftKey) modifiers.push('shift')
  return normalizeShortcutBinding({ key, modifiers })
}

export function resolveShortcut(command: ShortcutCommand, config: ShortcutConfig | null | undefined): ResolvedShortcut {
  const override = config?.overrides?.[command.id]
  const scope =
    override?.scope === 'global' && command.globalCapable ? 'global' : command.defaultScope
  return {
    command,
    binding: override ? normalizeShortcutBinding(override.binding) : command.defaultBinding,
    scope,
  }
}

export function shortcutToUKbdKeys(
  binding: ShortcutBinding | null | undefined,
  macOS = isMacOS(),
): string[] {
  const resolved = resolveShortcutBinding(binding, macOS)
  if (!resolved) return []
  const modifiers = resolved.modifiers.map((modifier) => {
    if (modifier === 'control') return 'ctrl'
    if (modifier === 'super') return macOS ? 'meta' : 'win'
    return modifier
  })
  const key = resolved.key === ' ' ? 'space' : resolved.key.toLowerCase()
  return [...modifiers, key]
}

export function shortcutDisplay(
  binding: ShortcutBinding | null | undefined,
  macOS = isMacOS(),
): string {
  const resolved = resolveShortcutBinding(binding, macOS)
  if (!resolved) return ''
  const labels = resolved.modifiers.map((modifier) => {
    if (modifier === 'control') return macOS ? '⌃' : 'Ctrl'
    if (modifier === 'alt') return macOS ? '⌥' : 'Alt'
    if (modifier === 'shift') return macOS ? '⇧' : 'Shift'
    return macOS ? '⌘' : 'Super'
  })
  const key = resolved.key === ' ' ? 'Space' : resolved.key.length === 1 ? resolved.key.toUpperCase() : resolved.key
  return [...labels, key].join(macOS ? '' : '+')
}
