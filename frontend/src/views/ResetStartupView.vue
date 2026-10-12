<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Application } from '@wailsio/runtime'
import { Exit, GetState, Ready, Retry, Show } from '#bindings/github.com/josexy/flowlens/backend/services/app_service/resetstartupservice'
import type { ResetStartupState } from '#bindings/github.com/josexy/flowlens/backend/services/app_service/models'

const { t, locale } = useI18n()
const state = ref<ResetStartupState | null>(null)
const actionPending = ref(false)
const frontendReady = ref(false)
const failure = ref('')
let timer: ReturnType<typeof setTimeout> | null = null
let disposed = false
const stageKey = computed(() => {
  const stage = state.value?.stage
  return stage === 'storage' || stage === 'webview' || stage === 'temporary' || stage === 'initializing'
    ? `settings.reset_startup_${stage}`
    : 'settings.reset_application_progress_description'
})
const stages = computed(() => {
  const snapshot = state.value
  if (!snapshot) return []
  const steps = snapshot.stages ?? []
  const current = steps.indexOf(snapshot.stage)
  return steps.map((stage, index) => ({
    stage,
    completed: current > index,
    active: current === index,
    failed: current === index && !!snapshot.error,
  }))
})

async function refresh() {
  try {
    const snapshot = await GetState()
    if (disposed) return
    state.value = snapshot
    locale.value = snapshot.language === 'en' ? 'en' : 'zh'
    failure.value = ''
    if (snapshot.close) {
      await Application.Quit()
      return
    }
  } catch (error) {
    if (disposed) return
    failure.value = String(error)
  }
  if (!disposed) timer = setTimeout(() => void refresh(), 250)
}

async function retry() {
  if (actionPending.value) return
  actionPending.value = true
  try {
    await Retry()
  } catch (error) {
    failure.value = String(error)
  } finally {
    actionPending.value = false
  }
}

async function exit() {
  if (actionPending.value) return
  actionPending.value = true
  try {
    await Exit()
  } catch (error) {
    failure.value = String(error)
    actionPending.value = false
  }
}

onMounted(async () => {
  await refresh()
  await nextTick()
  if (!disposed) {
    try {
      await Show()
      // RAF is throttled while the WebView is hidden. Show first, then allow
      // a painted frame before authorising the parent to delete any data.
      await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
      if (disposed) return
      await Ready()
      frontendReady.value = true
    } catch (error) {
      failure.value = String(error)
    }
  }
})

onBeforeUnmount(() => {
  disposed = true
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <UApp>
    <main class="flex min-h-screen flex-col justify-center gap-5 bg-default px-7 py-6 text-default">
      <div>
        <h1 class="text-lg font-semibold">{{ t(state?.error || failure ? 'settings.reset_recovery_title' : 'settings.reset_startup_title') }}</h1>
        <p v-if="state?.error || failure" class="mt-2 text-sm text-muted">{{ t('settings.reset_recovery_description') }}</p>
      </div>
      <ol v-if="stages.length" class="space-y-3 text-sm" role="status" aria-live="polite" :aria-label="t(stageKey)" :aria-busy="state?.busy">
        <li v-for="step in stages" :key="step.stage" class="flex items-center gap-3" :class="step.failed ? 'text-error' : step.completed ? 'text-success' : step.active ? 'text-default' : 'text-muted'">
          <UIcon :name="step.failed ? 'i-lucide-circle-x' : step.completed ? 'i-lucide-circle-check' : step.active ? 'i-lucide-loader-circle' : 'i-lucide-circle'" class="size-5 shrink-0" :class="{ 'animate-spin': step.active && !step.failed }" aria-hidden="true" />
          <span>{{ t(`settings.reset_startup_${step.stage}`) }}</span>
        </li>
      </ol>
      <p v-if="state?.error || failure" class="max-h-24 overflow-auto wrap-break-word text-sm text-error" role="alert">{{ state?.error || failure }}</p>
      <div class="flex justify-end gap-2">
        <UButton color="neutral" variant="outline" :label="t('settings.reset_recovery_exit')" :disabled="actionPending" @click="exit" />
        <UButton v-if="state?.error" :label="t('settings.reset_recovery_retry')" :disabled="!frontendReady || state.busy || actionPending" @click="retry" />
      </div>
    </main>
  </UApp>
</template>
