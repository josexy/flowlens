<script setup lang="ts">
import { Browser } from '@wailsio/runtime'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetEnvironmentInfo } from '#bindings/github.com/josexy/flowlens/backend/services/app_service/appservice'
import type { EnvironmentInfo } from '#bindings/github.com/josexy/flowlens/backend/services/app_service/models'
import { useNotify } from '@/composables/useNotify'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'

const { t } = useI18n()
const notify = useNotify()
const workspaceStore = useTrafficWorkspaceStore()
const githubProjectUrl = 'https://github.com/josexy/flowlens'

const statusActionButtonUi = {
  leadingIcon: 'size-4',
}

const activeStore = computed(() => workspaceStore.activeTrafficStore)
const selectedCount = computed(() => workspaceStore.activeTrafficSelectionCount)

const environmentModalOpen = ref(false)
const environmentInfo = ref<EnvironmentInfo | null>(null)
const environmentLoading = ref(false)
const environmentError = ref('')
const githubOpening = ref(false)

const environmentRows = computed(() => {
  if (!environmentInfo.value) {
    return []
  }
  return [
    { label: t('status.environment_app_version'), value: environmentInfo.value.appVersion },
    { label: t('status.environment_go'), value: environmentInfo.value.goVersion },
    { label: t('status.environment_wails'), value: environmentInfo.value.wailsVersion },
    { label: t('status.environment_build_commit'), value: environmentInfo.value.buildCommit },
    {
      label: t('status.environment_platform'),
      value: `${environmentInfo.value.goos}/${environmentInfo.value.goarch}`,
    },
  ]
})

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

async function loadEnvironmentInfo() {
  if (environmentLoading.value) {
    return
  }
  environmentLoading.value = true
  environmentError.value = ''
  try {
    environmentInfo.value = await GetEnvironmentInfo()
  } catch (error) {
    environmentError.value = t('status.environment_load_failed', {
      error: errorMessage(error),
    })
  } finally {
    environmentLoading.value = false
  }
}

function showEnvironmentInfo() {
  environmentModalOpen.value = true
  if (!environmentInfo.value) {
    void loadEnvironmentInfo()
  }
}

async function openGithubProject() {
  if (githubOpening.value) {
    return
  }
  githubOpening.value = true
  try {
    await Browser.OpenURL(githubProjectUrl)
  } catch (error) {
    notify.error(
      t('status.github_open_failed', {
        error: errorMessage(error),
      }),
    )
  } finally {
    githubOpening.value = false
  }
}
</script>

<template>
  <div
    class="relative z-(--z-sticky,20) flex h-8.5 max-h-8.5 min-h-8.5 shrink-0 items-center justify-end overflow-hidden bg-app-shell px-4 shadow-[0_-10px_24px_-26px_color-mix(in_srgb,var(--app-text-primary)_38%,transparent)] [backdrop-filter:blur(8px)] [-webkit-backdrop-filter:blur(8px)] [border-top:1px_solid_var(--app-border-strong-color)]"
  >
    <div
      class="absolute top-1/2 left-1/2 flex max-w-[calc(100%-12rem)] -translate-x-1/2 -translate-y-1/2 items-center leading-3.5"
    >
      <span
        class="block min-w-0 truncate font-sans text-sm font-medium leading-3.5 text-app-text-muted"
      >
        {{ t('status.total', { count: activeStore.statistics.total }) }}
        <span v-if="selectedCount > 0">{{ t('status.selected', { count: selectedCount }) }}</span>
      </span>
    </div>
    <div class="flex h-full shrink-0 items-center">
      <div class="flex items-center gap-0.5">
        <UTooltip :text="t('status.action_environment')" :content="{ side: 'top' }">
          <UButton
            color="neutral"
            variant="ghost"
            size="xs"
            square
            icon="i-lucide-info"
            :loading="environmentLoading"
            :ui="statusActionButtonUi"
            :aria-label="t('status.action_environment')"
            class="size-6! min-w-6! justify-center! rounded-(--radius-sm,6px)! border-0! bg-transparent! p-0! text-app-text-muted! shadow-none! transition-colors! hover:bg-app-accent-softer! hover:text-app-accent! focus-visible:bg-transparent! focus-visible:text-app-accent! focus-visible:outline-1! focus-visible:outline-offset-1! focus-visible:outline-app-accent! active:bg-app-accent-soft!"
            @click="showEnvironmentInfo"
          />
        </UTooltip>

        <UTooltip :text="t('status.action_github')" :content="{ side: 'top' }">
          <UButton
            color="neutral"
            variant="ghost"
            size="xs"
            square
            icon="i-lucide-github"
            :loading="githubOpening"
            :ui="statusActionButtonUi"
            :aria-label="t('status.action_github')"
            class="size-6! min-w-6! justify-center! rounded-(--radius-sm,6px)! border-0! bg-transparent! p-0! text-app-text-muted! shadow-none! transition-colors! hover:bg-app-accent-softer! hover:text-app-accent! focus-visible:bg-transparent! focus-visible:text-app-accent! focus-visible:outline-1! focus-visible:outline-offset-1! focus-visible:outline-app-accent! active:bg-app-accent-soft!"
            @click="openGithubProject"
          />
        </UTooltip>
      </div>
    </div>
  </div>

  <UModal
    v-model:open="environmentModalOpen"
    :title="t('status.environment_title')"
    :ui="{ content: 'max-w-md' }"
  >
    <template #body>
      <div
        v-if="environmentLoading"
        class="flex min-h-28 items-center justify-center gap-2 text-sm text-muted"
      >
        <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />
        <span>{{ t('common.loading') }}</span>
      </div>

      <div v-else-if="environmentError" class="space-y-3">
        <UAlert
          color="error"
          variant="subtle"
          icon="i-lucide-circle-alert"
          :title="environmentError"
        />
        <div class="flex justify-end">
          <UButton
            color="neutral"
            variant="outline"
            size="sm"
            icon="i-lucide-refresh-cw"
            :label="t('status.environment_retry')"
            @click="loadEnvironmentInfo"
          />
        </div>
      </div>

      <dl v-else class="divide-y divide-default overflow-hidden rounded-lg ring ring-default">
        <div
          v-for="row in environmentRows"
          :key="row.label"
          class="grid grid-cols-[7rem_minmax(0,1fr)] items-center gap-4 px-4 py-3"
        >
          <dt class="text-sm text-muted">{{ row.label }}</dt>
          <dd class="min-w-0 break-all font-mono text-sm text-highlighted">
            {{ row.value || '—' }}
          </dd>
        </div>
      </dl>
    </template>
  </UModal>
</template>
