import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { Events } from '@wailsio/runtime'
import {
  CancelUpdate,
  CheckForUpdates,
  DownloadUpdate,
  GetUpdateSnapshot,
  RestartForUpdate,
} from '#bindings/github.com/josexy/flowlens/backend/services/app_service/appservice'
import {
  UpdateApplyMode,
  UpdatePhase,
  type UpdateProgress,
  type UpdateSnapshot,
} from '#bindings/github.com/josexy/flowlens/backend/services/app_service/models'
import {
  OPEN_UPDATE_WINDOW_EVENT,
  UPDATE_PROGRESS_EVENT,
  UPDATE_STATE_CHANGED_EVENT,
} from '@/runtime/appEvents'

interface UpdateProgressPayload {
  revision: number
  progress: UpdateProgress | null
}

function emptySnapshot(): UpdateSnapshot {
  return {
    revision: 0,
    phase: UpdatePhase.UpdatePhaseIdle,
    currentVersion: '',
    applyMode: UpdateApplyMode.UpdateApplyModeManual,
    release: null,
    progress: null,
    failure: null,
    canCancel: false,
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function isSnapshot(value: unknown): value is UpdateSnapshot {
  if (!isRecord(value)) return false
  return (
    typeof value.revision === 'number' &&
    typeof value.phase === 'string' &&
    typeof value.currentVersion === 'string' &&
    typeof value.applyMode === 'string' &&
    typeof value.canCancel === 'boolean'
  )
}

function isProgressPayload(value: unknown): value is UpdateProgressPayload {
  if (!isRecord(value) || typeof value.revision !== 'number') return false
  if (value.progress === null) return true
  const progress = value.progress
  return (
    isRecord(progress) &&
    typeof progress.written === 'number' &&
    typeof progress.total === 'number' &&
    typeof progress.rate === 'number'
  )
}

export const useUpdaterStore = defineStore('updater', () => {
  const snapshot = ref<UpdateSnapshot>(emptySnapshot())
  const actionPending = ref(false)
  const initialized = ref(false)
  const offs: Array<() => void> = []
  let initializePromise: Promise<void> | null = null

  const phase = computed(() => snapshot.value.phase)
  const release = computed(() => snapshot.value.release ?? null)
  const progress = computed(() => snapshot.value.progress ?? null)
  const failure = computed(() => snapshot.value.failure ?? null)
  const isBusy = computed(() =>
    [
      UpdatePhase.UpdatePhaseChecking,
      UpdatePhase.UpdatePhaseDownloading,
      UpdatePhase.UpdatePhaseVerifying,
      UpdatePhase.UpdatePhasePreparing,
    ].includes(phase.value),
  )
  const isSelfUpdate = computed(
    () => snapshot.value.applyMode === UpdateApplyMode.UpdateApplyModeSelf,
  )

  function applySnapshot(next: UpdateSnapshot) {
    if (next.revision < snapshot.value.revision) return
    snapshot.value = next
  }

  function applyProgress(payload: UpdateProgressPayload) {
    if (payload.revision < snapshot.value.revision) return
    snapshot.value = {
      ...snapshot.value,
      revision: payload.revision,
      progress: payload.progress,
    }
  }

  function subscribe() {
    if (offs.length > 0) return
    offs.push(
      Events.On(UPDATE_STATE_CHANGED_EVENT, (event) => {
        if (isSnapshot(event.data)) applySnapshot(event.data)
      }),
      Events.On(UPDATE_PROGRESS_EVENT, (event) => {
        if (isProgressPayload(event.data)) applyProgress(event.data)
      }),
    )
  }

  async function initialize() {
    if (initialized.value) return
    if (initializePromise) {
      await initializePromise
      return
    }
    subscribe()
    initializePromise = GetUpdateSnapshot()
      .then(applySnapshot)
      .finally(() => {
        initialized.value = true
        initializePromise = null
      })
    await initializePromise
  }

  function cleanup() {
    for (const off of offs) off()
    offs.length = 0
    initialized.value = false
    initializePromise = null
  }

  async function runAction(action: () => Promise<void>) {
    if (actionPending.value) return
    actionPending.value = true
    try {
      await action()
    } finally {
      actionPending.value = false
    }
  }

  async function check() {
    await runAction(() => CheckForUpdates())
  }

  async function download() {
    await runAction(() => DownloadUpdate())
  }

  async function cancel() {
    await runAction(() => CancelUpdate())
  }

  async function restart() {
    await runAction(() => RestartForUpdate())
  }

  async function openWindow() {
    await Events.Emit(OPEN_UPDATE_WINDOW_EVENT)
  }

  return {
    snapshot,
    phase,
    release,
    progress,
    failure,
    actionPending,
    initialized,
    isBusy,
    isSelfUpdate,
    applySnapshot,
    applyProgress,
    initialize,
    cleanup,
    check,
    download,
    cancel,
    restart,
    openWindow,
  }
})
