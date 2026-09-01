import { bindingFromKeyboardEvent, isModifierOnlyKey } from './binding'
import type { ShortcutRecordingResult } from './types'

type RecordingListener = (result: ShortcutRecordingResult) => void

interface RecordingSession {
  id: number
  listener: RecordingListener
}

class ShortcutRecordingCoordinator {
  private session: RecordingSession | null = null
  private nextSessionId = 0

  get isRecording(): boolean {
    return this.session !== null
  }

  start(listener: RecordingListener): () => void {
    const previous = this.session
    const session: RecordingSession = { id: ++this.nextSessionId, listener }
    this.session = session
    previous?.listener({ type: 'cancel' })
    return () => this.cancelSession(session.id)
  }

  cancel(): void {
    if (this.session) this.cancelSession(this.session.id)
  }

  private cancelSession(sessionId: number): void {
    if (this.session?.id !== sessionId) return
    const session = this.session
    this.session = null
    session.listener({ type: 'cancel' })
  }

  private complete(result: ShortcutRecordingResult): void {
    const session = this.session
    this.session = null
    session?.listener(result)
  }

  consume(event: KeyboardEvent): boolean {
    if (!this.session) return false
    if (event.key === 'Escape') {
      this.cancel()
      return true
    }
    if (!event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) {
      if (event.key === 'Backspace' || event.key === 'Delete') {
        this.complete({ type: 'clear' })
        return true
      }
    }
    if (isModifierOnlyKey(event.key)) return true
    const binding = bindingFromKeyboardEvent(event)
    if (!binding) return true
    this.complete({ type: 'binding', binding })
    return true
  }
}

export const shortcutRecordingCoordinator = new ShortcutRecordingCoordinator()
