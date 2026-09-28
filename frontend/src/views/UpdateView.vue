<script setup lang="ts">
import { Browser, Events, Window as WailsWindow } from '@wailsio/runtime'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { UpdatePhase } from '#bindings/github.com/josexy/flowlens/backend/services/app_service/models'
import iconUrl from '@/assets/images/appicon.png'
import { useNotify } from '@/composables/useNotify'
import {
  UPDATE_WINDOW_CLOSE_BLOCKED_EVENT,
  UPDATE_WINDOW_CLOSE_REQUESTED_EVENT,
  UPDATE_WINDOW_READY_EVENT,
} from '@/runtime/appEvents'
import { useSettingStore } from '@/stores/setting'
import { useThemeStore } from '@/stores/theme'
import { useUpdaterStore } from '@/stores/updater'
import { formatDateTimeLocal, formatFileSize } from '@/utils/format'
import { isSafeUpdateURL, renderUpdateNotes } from '@/utils/updateNotes'

type StatusColor = 'neutral' | 'primary' | 'success' | 'warning' | 'error' | 'info'

interface UpdateStep {
  value: number
  title: string
  icon: string
  ui?: {
    trigger?: string
    title?: string
  }
}

const { locale, t } = useI18n()
const notify = useNotify()
const settingStore = useSettingStore()
const themeStore = useThemeStore()
const updaterStore = useUpdaterStore()
const closeConfirmOpen = ref(false)
const closeBlocked = ref(false)
const closeActionPending = ref(false)
const eventOffs: Array<() => void> = []

function stepIndexForPhase(phase: UpdatePhase | undefined): number {
  switch (phase) {
    case UpdatePhase.UpdatePhaseChecking:
      return 0
    case UpdatePhase.UpdatePhaseAvailable:
    case UpdatePhase.UpdatePhaseDownloading:
      return 1
    case UpdatePhase.UpdatePhaseVerifying:
      return 2
    case UpdatePhase.UpdatePhasePreparing:
      return 3
    case UpdatePhase.UpdatePhaseReady:
      return 4
    default:
      return -1
  }
}

const failureStepIndex = computed(() => stepIndexForPhase(updaterStore.failure?.stage))

const stepperValue = computed(() => {
  switch (updaterStore.phase) {
    case UpdatePhase.UpdatePhaseUpToDate:
      return 0
    case UpdatePhase.UpdatePhaseReady:
      return 4
    case UpdatePhase.UpdatePhaseError:
      return Math.max(failureStepIndex.value, 0)
    default:
      return Math.max(stepIndexForPhase(updaterStore.phase), 0)
  }
})

const activeStepIndex = computed(() => {
  if (
    updaterStore.phase === UpdatePhase.UpdatePhaseUpToDate ||
    updaterStore.phase === UpdatePhase.UpdatePhaseReady
  ) {
    return -1
  }
  if (updaterStore.phase === UpdatePhase.UpdatePhaseError) {
    return failureStepIndex.value >= 4 ? -1 : Math.max(failureStepIndex.value, 0)
  }
  return stepperValue.value
})

const completedStepIndex = computed(() => {
  if (updaterStore.phase === UpdatePhase.UpdatePhaseUpToDate) return 0
  return Math.min(stepperValue.value - 1, 3)
})

const stepperItems = computed<UpdateStep[]>(() => {
  const items: Array<Omit<UpdateStep, 'value'>> = [
    {
      title: t('updater.phase_check'),
      icon: 'i-lucide-search',
    },
    {
      title: t('updater.phase_download'),
      icon: 'i-lucide-download',
    },
    {
      title: t('updater.phase_verify'),
      icon: 'i-lucide-shield-check',
    },
    {
      title: t('updater.phase_prepare'),
      icon: 'i-lucide-package-check',
    },
  ]

  return items.map((item, index) => ({
    ...item,
    value: index,
    ui:
      updaterStore.phase === UpdatePhase.UpdatePhaseError && index === activeStepIndex.value
        ? {
            trigger:
              'group-data-[state=active]:bg-error/10 group-data-[state=active]:text-error group-data-[state=active]:ring-error',
            title: 'group-data-[state=active]:text-error',
          }
        : undefined,
  }))
})

const stepperUI = computed(() => ({
  root: 'gap-0',
  header: 'w-full',
  item: 'min-w-0',
  trigger: [
    'ring-1 ring-app-border-strong disabled:cursor-default',
    'group-data-[state=completed]:bg-app-accent group-data-[state=completed]:text-inverted group-data-[state=completed]:ring-app-accent',
    'group-data-[state=active]:bg-app-accent-soft group-data-[state=active]:text-app-accent group-data-[state=active]:ring-app-accent',
    updaterStore.phase === UpdatePhase.UpdatePhaseUpToDate
      ? 'group-data-[state=active]:bg-app-accent group-data-[state=active]:text-inverted'
      : '',
  ].join(' '),
  separator: [
    'h-px bg-app-border-strong group-data-[disabled]:opacity-100',
    'group-data-[state=completed]:bg-app-accent',
    updaterStore.phase === UpdatePhase.UpdatePhaseUpToDate
      ? 'group-data-[state=active]:bg-app-accent'
      : '',
  ].join(' '),
  title: [
    'truncate text-xs font-normal text-app-text-muted',
    'group-data-[state=completed]:font-semibold group-data-[state=completed]:text-app-text',
    'group-data-[state=active]:font-semibold group-data-[state=active]:text-app-text',
  ].join(' '),
}))

function stepIcon(item: UpdateStep): string {
  if (item.value <= completedStepIndex.value) return 'i-lucide-check'
  if (updaterStore.phase === UpdatePhase.UpdatePhaseError && item.value === activeStepIndex.value) {
    return 'i-lucide-circle-alert'
  }
  return item.icon
}

const statusPresentation = computed<{
  icon: string
  color: StatusColor
  title: string
  description: string
}>(() => {
  switch (updaterStore.phase) {
    case UpdatePhase.UpdatePhaseChecking:
      return {
        icon: 'i-lucide-radar',
        color: 'info',
        title: t('updater.checking_title'),
        description: t('updater.checking_description'),
      }
    case UpdatePhase.UpdatePhaseUpToDate:
      return {
        icon: 'i-lucide-circle-check-big',
        color: 'success',
        title: t('updater.up_to_date_title'),
        description: t('updater.up_to_date_description'),
      }
    case UpdatePhase.UpdatePhaseAvailable:
      return {
        icon: updaterStore.isSelfUpdate ? 'i-lucide-package-open' : 'i-lucide-external-link',
        color: 'primary',
        title: t('updater.available_title', { version: updaterStore.release?.version ?? '' }),
        description: updaterStore.isSelfUpdate
          ? t('updater.available_self_description')
          : t('updater.available_manual_description'),
      }
    case UpdatePhase.UpdatePhaseDownloading:
      return {
        icon: 'i-lucide-cloud-download',
        color: 'info',
        title: t('updater.downloading_title'),
        description: t('updater.downloading_description'),
      }
    case UpdatePhase.UpdatePhaseVerifying:
      return {
        icon: 'i-lucide-shield-check',
        color: 'warning',
        title: t('updater.verifying_title'),
        description: t('updater.verifying_description'),
      }
    case UpdatePhase.UpdatePhasePreparing:
      return {
        icon: 'i-lucide-package-check',
        color: 'warning',
        title: t('updater.preparing_title'),
        description: t('updater.preparing_description'),
      }
    case UpdatePhase.UpdatePhaseReady:
      return {
        icon: 'i-lucide-power',
        color: 'success',
        title: t('updater.ready_title'),
        description: t('updater.ready_description'),
      }
    case UpdatePhase.UpdatePhaseError:
      return {
        icon: 'i-lucide-circle-alert',
        color: 'error',
        title: t('updater.error_title'),
        description: updaterStore.failure?.message || t('updater.error_description'),
      }
    default:
      return {
        icon: 'i-lucide-refresh-cw',
        color: 'neutral',
        title: t('updater.idle_title'),
        description: t('updater.idle_description'),
      }
  }
})

const notesHTML = computed(() => renderUpdateNotes(updaterStore.release?.notes ?? ''))
const publishedAt = computed(() => {
  const value = updaterStore.release?.publishedAt
  return value ? formatDateTimeLocal(value) : ''
})
const progressPercent = computed(() => {
  const progress = updaterStore.progress
  if (!progress || progress.total <= 0) return null
  return Math.min(100, Math.max(0, (progress.written / progress.total) * 100))
})
const progressSummary = computed(() => {
  const progress = updaterStore.progress
  if (!progress) return ''
  const written = formatFileSize(progress.written, { precision: 1 })
  const total =
    progress.total > 0
      ? formatFileSize(progress.total, { precision: 1 })
      : t('updater.unknown_size')
  const rate = progress.rate > 0 ? `${formatFileSize(progress.rate, { precision: 1 })}/s` : ''
  return rate
    ? t('updater.progress_with_rate', { written, total, rate })
    : t('updater.progress', { written, total })
})

const primaryAction = computed(() => {
  switch (updaterStore.phase) {
    case UpdatePhase.UpdatePhaseAvailable:
      return updaterStore.isSelfUpdate
        ? { label: t('updater.action_download'), icon: 'i-lucide-download' }
        : { label: t('updater.action_open_release'), icon: 'i-lucide-external-link' }
    case UpdatePhase.UpdatePhaseReady:
      return { label: t('updater.action_restart'), icon: 'i-lucide-power' }
    case UpdatePhase.UpdatePhaseError:
      return { label: t('updater.action_retry'), icon: 'i-lucide-refresh-cw' }
    case UpdatePhase.UpdatePhaseIdle:
    case UpdatePhase.UpdatePhaseUpToDate:
      return { label: t('updater.action_check'), icon: 'i-lucide-search' }
    default:
      return null
  }
})

const canCloseNormally = computed(
  () =>
    updaterStore.phase !== UpdatePhase.UpdatePhaseChecking &&
    updaterStore.phase !== UpdatePhase.UpdatePhaseDownloading &&
    updaterStore.phase !== UpdatePhase.UpdatePhaseVerifying &&
    updaterStore.phase !== UpdatePhase.UpdatePhasePreparing,
)

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

async function openReleasePage() {
  const url = updaterStore.release?.releaseURL ?? ''
  if (!isSafeUpdateURL(url)) {
    throw new Error(t('updater.invalid_release_url'))
  }
  await Browser.OpenURL(url)
}

async function handlePrimaryAction() {
  try {
    switch (updaterStore.phase) {
      case UpdatePhase.UpdatePhaseAvailable:
        if (updaterStore.isSelfUpdate) await updaterStore.download()
        else await openReleasePage()
        break
      case UpdatePhase.UpdatePhaseReady:
        await updaterStore.restart()
        break
      case UpdatePhase.UpdatePhaseError:
        if (updaterStore.failure?.stage === UpdatePhase.UpdatePhaseChecking) {
          await updaterStore.check()
        } else if (updaterStore.failure?.stage === UpdatePhase.UpdatePhaseReady) {
          await updaterStore.restart()
        } else {
          await updaterStore.download()
        }
        break
      default:
        await updaterStore.check()
    }
  } catch (error) {
    notify.error(t('updater.action_failed', { error: errorMessage(error) }))
  }
}

async function requestClose() {
  await WailsWindow.Close()
}

async function continueInBackground() {
  closeConfirmOpen.value = false
  await WailsWindow.Hide()
}

async function cancelAndClose() {
  if (closeActionPending.value) return
  closeActionPending.value = true
  try {
    await updaterStore.cancel()
    closeConfirmOpen.value = false
    await WailsWindow.Hide()
  } catch (error) {
    notify.error(t('updater.action_failed', { error: errorMessage(error) }))
  } finally {
    closeActionPending.value = false
  }
}

async function cancelCurrentOperation() {
  try {
    await updaterStore.cancel()
  } catch (error) {
    notify.error(t('updater.action_failed', { error: errorMessage(error) }))
  }
}

function handleNotesClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Element)) return
  const anchor = target.closest('a')
  if (!(anchor instanceof HTMLAnchorElement)) return
  event.preventDefault()
  const url = anchor.getAttribute('href') ?? ''
  if (!isSafeUpdateURL(url)) return
  void Browser.OpenURL(url).catch((error) => {
    notify.error(t('updater.action_failed', { error: errorMessage(error) }))
  })
}

onMounted(async () => {
  eventOffs.push(
    Events.On(UPDATE_WINDOW_CLOSE_REQUESTED_EVENT, () => {
      closeBlocked.value = false
      closeConfirmOpen.value = true
    }),
    Events.On(UPDATE_WINDOW_CLOSE_BLOCKED_EVENT, () => {
      closeBlocked.value = true
    }),
  )
  try {
    await settingStore.load()
    locale.value = settingStore.language
    themeStore.initializeTheme(settingStore.themeMode)
    await updaterStore.initialize()
  } catch (error) {
    notify.error(t('updater.initialization_failed', { error: errorMessage(error) }))
  } finally {
    await nextTick()
    await Events.Emit(UPDATE_WINDOW_READY_EVENT)
  }
})

onBeforeUnmount(() => {
  for (const off of eventOffs) off()
  eventOffs.length = 0
  updaterStore.cleanup()
})
</script>

<template>
  <main class="flex h-full min-h-0 flex-col overflow-hidden bg-app-content">
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div class="mx-auto flex min-h-full w-full max-w-3xl flex-col px-7 py-6 max-[620px]:px-5">
        <header class="flex items-start gap-4">
          <div
            class="relative flex size-13 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-app-elevated ring-1 ring-app-border-strong shadow-(--app-panel-shadow)"
          >
            <div class="absolute inset-x-1.5 bottom-1 h-1 rounded-full bg-app-accent-soft" />
            <img :src="iconUrl" alt="" class="relative size-8 object-contain" />
          </div>
          <div class="min-w-0 flex-1 pt-0.5">
            <p class="text-xs font-semibold tracking-[0.14em] text-app-text-muted uppercase">
              {{ t('updater.eyebrow') }}
            </p>
            <div class="mt-1 flex flex-wrap items-center gap-2">
              <h1 class="text-xl font-bold tracking-[-0.02em] text-app-text">
                {{ t('updater.window_title') }}
              </h1>
              <UBadge color="neutral" variant="subtle" size="sm">
                v{{ updaterStore.snapshot.currentVersion }}
              </UBadge>
            </div>
          </div>
        </header>

        <section class="mt-6" :aria-label="t('updater.progress_label')">
          <UStepper
            :model-value="stepperValue"
            :items="stepperItems"
            :ui="stepperUI"
            class="w-full"
            color="primary"
            size="sm"
            disabled
          >
            <template #indicator="{ item }">
              <UIcon
                :name="stepIcon(item)"
                class="size-4"
                :class="
                  item.value === activeStepIndex && updaterStore.isBusy ? 'animate-pulse' : ''
                "
              />
            </template>
          </UStepper>
        </section>

        <section
          class="mt-6 overflow-hidden rounded-xl bg-app-panel ring-1 ring-app-border-strong shadow-(--app-panel-shadow)"
        >
          <div class="flex items-start gap-4 px-5 py-5">
            <div
              class="flex size-11 shrink-0 items-center justify-center rounded-lg"
              :class="{
                'bg-success/10 text-success': statusPresentation.color === 'success',
                'bg-error/10 text-error': statusPresentation.color === 'error',
                'bg-warning/10 text-warning': statusPresentation.color === 'warning',
                'bg-info/10 text-info': statusPresentation.color === 'info',
                'bg-app-accent-soft text-app-accent': statusPresentation.color === 'primary',
                'bg-app-control text-app-text-muted': statusPresentation.color === 'neutral',
              }"
            >
              <UIcon
                :name="statusPresentation.icon"
                class="size-6"
                :class="
                  updaterStore.phase === UpdatePhase.UpdatePhaseChecking ? 'animate-pulse' : ''
                "
              />
            </div>
            <div class="min-w-0 flex-1">
              <h2 class="text-lg font-bold leading-6 text-app-text">
                {{ statusPresentation.title }}
              </h2>
              <p class="mt-1 text-sm leading-5 text-app-text-muted">
                {{ statusPresentation.description }}
              </p>
            </div>
          </div>

          <div
            v-if="updaterStore.phase === UpdatePhase.UpdatePhaseDownloading"
            class="border-t border-app-border px-5 py-4"
          >
            <UProgress
              :model-value="progressPercent"
              :max="100"
              color="primary"
              size="sm"
              :aria-label="t('updater.downloading_title')"
            />
            <div
              class="mt-2 flex items-center justify-between gap-4 font-mono text-xs text-app-text-muted"
            >
              <span>{{ progressSummary }}</span>
              <span v-if="progressPercent !== null">{{ Math.round(progressPercent) }}%</span>
            </div>
          </div>

          <div v-if="updaterStore.release" class="border-t border-app-border px-5 py-4">
            <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-5 gap-y-2 text-sm">
              <dt class="text-app-text-muted">{{ t('updater.release_version') }}</dt>
              <dd class="font-mono font-semibold text-app-text">
                v{{ updaterStore.release.version }}
              </dd>
              <template v-if="publishedAt">
                <dt class="text-app-text-muted">{{ t('updater.published_at') }}</dt>
                <dd class="font-mono text-app-text-secondary">{{ publishedAt }}</dd>
              </template>
              <dt class="text-app-text-muted">{{ t('updater.artifact') }}</dt>
              <dd class="min-w-0 truncate font-mono text-app-text-secondary">
                {{ updaterStore.release.artifactName }}
                <span v-if="updaterStore.release.artifactSize > 0">
                  · {{ formatFileSize(updaterStore.release.artifactSize, { precision: 1 }) }}
                </span>
              </dd>
            </dl>
          </div>
        </section>

        <UAlert
          v-if="updaterStore.release && !updaterStore.isSelfUpdate"
          class="mt-4"
          color="info"
          variant="subtle"
          icon="i-lucide-external-link"
          :title="t('updater.manual_notice_title')"
          :description="t('updater.manual_notice_description')"
        />

        <UAlert
          v-if="closeBlocked"
          class="mt-4"
          color="warning"
          variant="subtle"
          icon="i-lucide-lock-keyhole"
          :title="t('updater.close_blocked_title')"
          :description="t('updater.close_blocked_description')"
          :actions="[
            { label: t('updater.action_understood'), onClick: () => (closeBlocked = false) },
          ]"
        />

        <section v-if="updaterStore.release" class="mt-5 min-h-0">
          <div class="mb-2 flex items-center justify-between gap-3">
            <h2 class="text-sm font-bold text-app-text">{{ t('updater.release_notes') }}</h2>
            <UBadge
              :color="updaterStore.isSelfUpdate ? 'success' : 'info'"
              variant="subtle"
              size="sm"
            >
              {{ updaterStore.isSelfUpdate ? t('updater.mode_self') : t('updater.mode_manual') }}
            </UBadge>
          </div>
          <div
            class="update-notes max-h-56 min-h-24 select-text overflow-y-auto rounded-lg bg-app-panel px-4 py-3 text-sm text-app-text-secondary ring-1 ring-app-border"
            @click="handleNotesClick"
          >
            <div v-if="notesHTML" v-html="notesHTML" />
            <p v-else class="text-app-text-muted">{{ t('updater.release_notes_empty') }}</p>
          </div>
          <p class="mt-2 flex items-center gap-1.5 text-xs text-app-text-muted">
            <UIcon name="i-lucide-shield-check" class="size-3.5 shrink-0" />
            {{ t('updater.integrity_note') }}
          </p>
        </section>
      </div>
    </div>

    <footer
      class="shrink-0 border-t border-app-border bg-app-sidebar-header px-7 py-3 max-[620px]:px-5"
    >
      <div class="mx-auto flex w-full max-w-3xl items-center justify-end gap-2.5">
        <UButton
          v-if="updaterStore.snapshot.canCancel"
          color="neutral"
          variant="outline"
          icon="i-lucide-x"
          :label="t('updater.action_cancel')"
          :loading="updaterStore.actionPending"
          @click="cancelCurrentOperation"
        />
        <UButton
          v-else-if="canCloseNormally"
          color="neutral"
          variant="outline"
          :label="
            updaterStore.phase === UpdatePhase.UpdatePhaseReady
              ? t('updater.action_later')
              : t('updater.action_close')
          "
          @click="requestClose"
        />
        <UButton
          v-if="primaryAction"
          :icon="primaryAction.icon"
          :label="primaryAction.label"
          :loading="updaterStore.actionPending"
          @click="handlePrimaryAction"
        />
      </div>
    </footer>

    <UModal
      v-model:open="closeConfirmOpen"
      :dismissible="!closeActionPending"
      :title="t('updater.close_confirm_title')"
      :description="t('updater.close_confirm_description')"
      :ui="{ content: 'max-w-md', footer: 'justify-end' }"
    >
      <template #footer>
        <UButton
          color="neutral"
          variant="ghost"
          :disabled="closeActionPending"
          :label="t('updater.action_return')"
          @click="closeConfirmOpen = false"
        />
        <UButton
          color="error"
          variant="outline"
          :loading="closeActionPending"
          :label="t('updater.action_cancel_and_close')"
          @click="cancelAndClose"
        />
        <UButton
          icon="i-lucide-minimize-2"
          :disabled="closeActionPending"
          :label="t('updater.action_background')"
          @click="continueInBackground"
        />
      </template>
    </UModal>
  </main>
</template>

<style scoped>
.update-notes :deep(:first-child) {
  margin-top: 0;
}

.update-notes :deep(:last-child) {
  margin-bottom: 0;
}

.update-notes :deep(p),
.update-notes :deep(ul),
.update-notes :deep(ol),
.update-notes :deep(pre),
.update-notes :deep(blockquote),
.update-notes :deep(table) {
  margin-block: 0.65rem;
}

.update-notes :deep(h1),
.update-notes :deep(h2),
.update-notes :deep(h3) {
  margin-block: 0.9rem 0.45rem;
  color: var(--app-text-primary);
  font-weight: 700;
  line-height: 1.35;
}

.update-notes :deep(h1) {
  font-size: 1.05rem;
}

.update-notes :deep(h2),
.update-notes :deep(h3) {
  font-size: 0.95rem;
}

.update-notes :deep(ul),
.update-notes :deep(ol) {
  padding-left: 1.3rem;
}

.update-notes :deep(ul) {
  list-style: disc;
}

.update-notes :deep(ol) {
  list-style: decimal;
}

.update-notes :deep(a) {
  color: var(--app-accent-color);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.update-notes :deep(code) {
  border-radius: 4px;
  background: var(--app-control-bg);
  padding: 0.08rem 0.3rem;
  font-family: var(--code-font-family);
  font-size: 0.82rem;
}

.update-notes :deep(pre) {
  overflow-x: auto;
  border: 1px solid var(--app-border-color);
  border-radius: 6px;
  background: var(--app-content-bg);
  padding: 0.75rem;
}

.update-notes :deep(pre code) {
  background: transparent;
  padding: 0;
}

.update-notes :deep(blockquote) {
  border-left: 3px solid var(--app-accent-color);
  padding-left: 0.75rem;
  color: var(--app-text-muted);
}

.update-notes :deep(table) {
  width: 100%;
  border-collapse: collapse;
}

.update-notes :deep(th),
.update-notes :deep(td) {
  border: 1px solid var(--app-border-color);
  padding: 0.4rem 0.55rem;
  text-align: left;
}
</style>
