import type { ShortcutHandler, ShortcutInvocationContext } from './types'

export interface SelectedShortcutHandler extends ShortcutHandler {
  order: number
}

function isActive(
  value: ShortcutHandler['when'] | ShortcutHandler['enabled'],
  context: ShortcutInvocationContext,
): boolean {
  return typeof value === 'function' ? value(context) : value !== false
}

class ShortcutHandlerRegistry {
  private handlers = new Map<number, SelectedShortcutHandler>()
  private nextOrder = 0

  register(handler: ShortcutHandler): () => void {
    const order = ++this.nextOrder
    this.handlers.set(order, { ...handler, order })
    let active = true
    return () => {
      if (!active) return
      active = false
      this.handlers.delete(order)
    }
  }

  select(
    commandIds: Iterable<string>,
    context: ShortcutInvocationContext = { source: 'keyboard' },
  ): SelectedShortcutHandler | null {
    const ids = new Set(commandIds)
    let selected: SelectedShortcutHandler | null = null
    for (const handler of this.handlers.values()) {
      if (
        !ids.has(handler.commandId) ||
        !isActive(handler.when, context) ||
        !isActive(handler.enabled, context)
      ) {
        continue
      }
      if (
        !selected ||
        (handler.priority ?? 0) > (selected.priority ?? 0) ||
        ((handler.priority ?? 0) === (selected.priority ?? 0) && handler.order > selected.order)
      ) {
        selected = handler
      }
    }
    return selected
  }

  invokeSelected(
    handler: SelectedShortcutHandler | null,
    context: ShortcutInvocationContext = { source: 'keyboard' },
  ): boolean {
    if (!handler) return false
    try {
      Promise.resolve(handler.run(context)).catch((error: unknown) => {
        console.error(`Shortcut handler failed: ${handler.commandId}`, error)
      })
    } catch (error) {
      console.error(`Shortcut handler failed: ${handler.commandId}`, error)
    }
    return true
  }

  invoke(
    commandIds: Iterable<string>,
    context: ShortcutInvocationContext = { source: 'keyboard' },
  ): boolean {
    return this.invokeSelected(this.select(commandIds, context), context)
  }
}

export const shortcutHandlerRegistry = new ShortcutHandlerRegistry()

export function registerShortcutHandler(handler: ShortcutHandler): () => void {
  return shortcutHandlerRegistry.register(handler)
}
