<script setup lang="ts">
import HostFilterModal from '@/components/modal/HostFilterModal.vue'
import PortConfigModal from '@/components/modal/PortConfigModal.vue'
import ProxyToolbarIcon from '@/components/traffic/ProxyToolbarIcon.vue'
import { useProxyStore } from '@/stores/proxy'
import { useSettingStore } from '@/stores/setting'
import { useTrafficStore } from '@/stores/traffic'
import { useTrafficWorkspaceStore } from '@/stores/trafficWorkspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DropdownMenuItem } from '@nuxt/ui'
import { copyText } from '@/utils/clipboard'
import { useNotify } from '@/composables/useNotify'
import { registerShortcutHandler, useShortcutKbds } from '@/shortcuts'
import { ListLocalIPv4Addresses } from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/settingservice'
import {
  ProxyMode,
  UpstreamProxyMode,
  type LocalIPAddress,
  type ProxyConfig,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

defineOptions({
  name: 'ProxyToolbar',
})

const { t } = useI18n()
const proxyStore = useProxyStore()
const settingStore = useSettingStore()
const trafficStore = useTrafficStore()
const workspaceStore = useTrafficWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const notify = useNotify()
const proxyToggleShortcutKbds = useShortcutKbds('capture.toggleProxy')
const systemProxyToggleShortcutKbds = useShortcutKbds('capture.toggleSystemProxy')
const clearTrafficShortcutKbds = useShortcutKbds('capture.clearTraffic')

const applying = ref(false)
const proxyToggleApplying = ref(false)
const systemProxyApplying = ref(false)
const portModalOpen = ref(false)
const hostFilterModalOpen = ref(false)
const localIPOptions = ref<LocalIPAddress[]>([])

const fallbackLocalIPOptions: LocalIPAddress[] = [
  { label: 'ALL', value: '0.0.0.0', interfaceName: '' },
  { label: '127.0.0.1', value: '127.0.0.1', interfaceName: '' },
]

const proxyAddress = computed(() => {
  return proxyStore.status?.address?.trim() ?? ''
})

const proxyConfig = computed(() => {
  if (!settingStore.settings) {
    return null
  }
  return ensureProxyConfig(settingStore.settings)
})

function getProxyScheme(mode: ProxyMode) {
  return mode === ProxyMode.ProxyModeSOCKS5 ? 'socks5' : 'http'
}

const proxyDisplayAddress = computed(() => {
  const address = proxyAddress.value
  if (!address) {
    return ''
  }
  if (/^[a-z][a-z\d+\-.]*:\/\//i.test(address)) {
    return address
  }
  return `${getProxyScheme(proxyConfig.value?.mode ?? ProxyMode.ProxyModeHTTP)}://${address}`
})

const proxyStatusText = computed(() => {
  if (!proxyStore.isRunning) {
    return t('toolbar.proxy_not_running')
  }
  return t('toolbar.proxy_running', { address: proxyDisplayAddress.value })
})

const modeItems = computed<DropdownMenuItem[]>(() => [
  {
    label: 'HTTP',
    active: currentMode.value === ProxyMode.ProxyModeHTTP,
    onSelect: () => void handleModeSelect(ProxyMode.ProxyModeHTTP),
  },
  {
    label: 'SOCKS5',
    active: currentMode.value === ProxyMode.ProxyModeSOCKS5,
    onSelect: () => void handleModeSelect(ProxyMode.ProxyModeSOCKS5),
  },
])

const quickDropdownItemUi = {
  item: 'items-center',
  itemWrapper: 'justify-center',
}

const modeMenuUi = {
  ...quickDropdownItemUi,
  content: 'min-w-29',
}

const hostMenuUi = {
  ...quickDropdownItemUi,
  content: 'min-w-40 max-w-[min(22rem,calc(100vw-24px))]',
}

const hostItems = computed<DropdownMenuItem[]>(() => {
  const options = localIPOptions.value.length
    ? localIPOptions.value
    : fallbackLocalIPOptions
  return options.map((option) => ({
    label: option.label,
    active: option.value === currentHost.value,
    onSelect: () => void handleHostSelect(option.value),
  }))
})

const currentMode = computed(() => proxyConfig.value?.mode ?? ProxyMode.ProxyModeHTTP)
const currentHost = computed(() => proxyConfig.value?.host ?? '127.0.0.1')
const currentPort = computed(() => proxyConfig.value?.port ?? 8080)
const disableProxy = computed(() => proxyConfig.value?.disableProxy ?? false)
const disableHttp2 = computed(() => proxyConfig.value?.disableHttp2 ?? false)
const skipVerifyTls = computed(() => proxyConfig.value?.skipVerifyTls ?? false)
const hostFilterCount = computed(
  () =>
    (proxyConfig.value?.includeHosts?.length ?? 0) + (proxyConfig.value?.excludeHosts?.length ?? 0),
)
const systemProxySupported = computed(() => proxyStore.systemProxyStatus?.supported ?? false)
const systemProxyModeSupported = computed(
  () => proxyStore.systemProxyStatus?.modeSupported ?? false,
)
const systemProxyActive = computed(() => proxyStore.systemProxyStatus?.active ?? false)
const systemProxyTooltip = computed(() => {
	if (systemProxyActive.value) {
		return t('toolbar.system_proxy_on', {
			address: proxyStore.systemProxyStatus?.address ?? '',
		})
	}
	if (!systemProxyModeSupported.value) {
		return t('toolbar.system_proxy_mode_unsupported', {
			mode: currentMode.value.toUpperCase(),
		})
	}
  return t('toolbar.system_proxy_off')
})

onMounted(() => {
  void loadLocalIPOptions()
})

function ensureProxyConfig(settings: NonNullable<typeof settingStore.settings>): ProxyConfig {
  if (!settings.proxyConfig) {
    settings.proxyConfig = {
      mode: ProxyMode.ProxyModeHTTP,
      host: '127.0.0.1',
      port: 8080,
      caCertPath: 'certs/ca.crt',
      caKeyPath: 'certs/ca.key',
      upstreamProxyMode: UpstreamProxyMode.UpstreamProxyModeSystem,
      upstreamProxy: '',
      disableProxy: false,
      disableHttp2: false,
      skipVerifyTls: false,
      includeHosts: [],
      excludeHosts: [],
      rootCAPaths: [],
      clientCerts: [],
    }
  }
  settings.proxyConfig.includeHosts ??= []
  settings.proxyConfig.excludeHosts ??= []
  settings.proxyConfig.rootCAPaths ??= []
  settings.proxyConfig.clientCerts ??= []
  return settings.proxyConfig
}

async function loadLocalIPOptions() {
  try {
    const options = await ListLocalIPv4Addresses()
    localIPOptions.value = options?.length ? options : fallbackLocalIPOptions
  } catch {
    localIPOptions.value = fallbackLocalIPOptions
  }
}

async function saveQuickProxyConfig() {
  if (!settingStore.settings || !proxyConfig.value) {
    await settingStore.load()
  }

  const wasRunning = proxyStore.isRunning
  applying.value = true
  try {
    await settingStore.save()
    const result = settingStore.lastProxyApplyResult
    if (wasRunning && result?.restartRequired) {
      await proxyStore.stop()
      await proxyStore.start()
    }
  } catch (error) {
    notify.error(t('toolbar.quick_settings_failed', { error }))
  } finally {
    applying.value = false
  }
}

async function updateProxyConfig(mutator: (cfg: ProxyConfig) => void) {
  if (!settingStore.settings) {
    await settingStore.load()
  }
  if (!settingStore.settings) {
    notify.error(t('toolbar.quick_settings_failed', { error: 'settings not loaded' }))
    return
  }
  const cfg = ensureProxyConfig(settingStore.settings)
  mutator(cfg)
  await saveQuickProxyConfig()
}

async function handleStartStop() {
  if (proxyToggleApplying.value) return
  proxyToggleApplying.value = true
  try {
    if (proxyStore.isRunning) {
      await proxyStore.stop()
      notify.success(t('toolbar.stopped'))
    } else {
      await proxyStore.start()
      notify.success(t('toolbar.started'))
    }
  } catch (error) {
    notify.error(t('toolbar.operation_failed', { error }))
  } finally {
    proxyToggleApplying.value = false
  }
}

async function handleClear() {
  if (!isCaptureTrafficActive()) return
  try {
    await trafficStore.clearAll()
    notify.success(t('toolbar.cleared'))
  } catch (error) {
    notify.error(t('toolbar.clear_failed', { error }))
  }
}

async function handleModeSelect(key: string | number) {
  const mode = String(key) as ProxyMode
  if (mode === currentMode.value) return
  await updateProxyConfig((cfg) => {
    cfg.mode = mode
  })
}

async function handleHostSelect(key: string | number) {
  const host = String(key)
  if (host === currentHost.value) return
  await updateProxyConfig((cfg) => {
    cfg.host = host
  })
}

function openPortModal() {
  portModalOpen.value = true
}

async function handlePortSave(port: number | null) {
  if (!port || port < 1 || port > 65535) {
    notify.error(t('toolbar.invalid_port'))
    return
  }
  if (port === currentPort.value) {
    portModalOpen.value = false
    return
  }
  await updateProxyConfig((cfg) => {
    cfg.port = port
  })
  portModalOpen.value = false
}

async function toggleBooleanSetting(key: 'disableProxy' | 'disableHttp2' | 'skipVerifyTls') {
  await updateProxyConfig((cfg) => {
    cfg[key] = !cfg[key]
  })
}

async function toggleSystemProxy() {
  if (systemProxyApplying.value) return
  systemProxyApplying.value = true
  try {
    const status = await proxyStore.setSystemProxyEnabled(!systemProxyActive.value)
    if (status.active) {
      notify.success(t('toolbar.system_proxy_enabled', { address: status.address }))
    } else {
      notify.success(t('toolbar.system_proxy_restored'))
    }
  } catch (error) {
    notify.error(t('toolbar.system_proxy_failed', { error }))
  } finally {
    systemProxyApplying.value = false
  }
}

function openHostFilterModal() {
  hostFilterModalOpen.value = true
}

async function handleHostFilterSave(nextValue: { includeHosts: string[]; excludeHosts: string[] }) {
  if (!proxyConfig.value) {
    return
  }
  proxyConfig.value.includeHosts = [...nextValue.includeHosts]
  proxyConfig.value.excludeHosts = [...nextValue.excludeHosts]
  await saveQuickProxyConfig()
  hostFilterModalOpen.value = false
}

async function copyAddress() {
  if (proxyStore.isRunning && proxyDisplayAddress.value) {
    await copyText(proxyDisplayAddress.value)
    notify.success(t('toolbar.copied'))
  }
}

function isTrafficContentActive() {
  return workbenchStore.activeContent === 'traffic'
}

function isCaptureTrafficActive() {
  return isTrafficContentActive() && workspaceStore.activeTab.type === 'capture'
}

const offShortcutHandlers = [
  registerShortcutHandler({
    commandId: 'capture.toggleProxy',
    when: (context) => context.source === 'global' || isTrafficContentActive(),
    enabled: () => !proxyToggleApplying.value,
    run: () => handleStartStop(),
  }),
  registerShortcutHandler({
    commandId: 'capture.toggleSystemProxy',
    when: isTrafficContentActive,
    enabled: () =>
      systemProxySupported.value &&
      !systemProxyApplying.value &&
      (systemProxyModeSupported.value || systemProxyActive.value),
    run: () => toggleSystemProxy(),
  }),
  registerShortcutHandler({
    commandId: 'capture.clearTraffic',
    when: isCaptureTrafficActive,
    enabled: () => trafficStore.entries.length > 0,
    run: () => handleClear(),
  }),
]

onBeforeUnmount(() => {
  offShortcutHandlers.forEach((off) => off())
})
</script>

<template>
  <div
    class="relative flex min-h-11 shrink-0 items-center justify-between gap-3 bg-app-content px-3 py-1.5 [border-bottom:1px_solid_var(--app-border-color)]"
  >
    <div
      class="flex min-h-8 min-w-0 flex-[1_1_auto] items-center justify-between gap-3 rounded-sm border border-app-border bg-app-panel pl-3 pr-2 shadow-(--shadow-sm)"
    >
      <div class="flex h-full min-w-0 flex-[1_1_auto] items-center">
        <div class="flex h-full min-w-0 max-w-full items-center gap-2 leading-none">
          <div
            class="size-1.75 shrink-0 rounded-full transition-colors duration-200"
            :class="proxyStore.isRunning ? 'bg-app-success' : 'bg-app-error'"
          ></div>
          <span
            class="inline-flex min-h-4.5 items-center truncate text-sm leading-4.5 text-app-text-secondary"
            >{{ proxyStatusText }}</span
          >
          <UButton
            v-if="proxyStore.isRunning && proxyDisplayAddress"
            icon="i-lucide-copy"
            color="neutral"
            variant="ghost"
            size="xs"
            square
            :aria-label="t('toolbar.copied')"
            @click="copyAddress"
          />
        </div>
      </div>

      <div class="ml-auto flex flex-[0_0_auto] flex-row items-center gap-1">
        <UDropdownMenu
          :items="modeItems"
          :content="{ side: 'bottom', align: 'end' }"
          :ui="modeMenuUi"
        >
          <UTooltip
            :text="t('toolbar.quick_mode', { mode: currentMode.toUpperCase() })"
            :content="{ side: 'bottom' }"
          >
            <UButton
              class="p-1"
              color="neutral"
              variant="ghost"
              size="md"
              square
              :aria-label="t('toolbar.quick_mode', { mode: currentMode.toUpperCase() })"
            >
              <ProxyToolbarIcon name="mode" />
            </UButton>
          </UTooltip>
          <template #item-trailing="{ item }">
            <UIcon v-if="item.active" name="i-lucide-check" class="size-3.5 shrink-0" />
            <span v-else class="size-3.5 shrink-0" />
          </template>
        </UDropdownMenu>

        <UDropdownMenu
          :items="hostItems"
          :content="{ side: 'bottom', align: 'end' }"
          :ui="hostMenuUi"
        >
          <UTooltip
            :text="t('toolbar.quick_host', { host: currentHost })"
            :content="{ side: 'bottom' }"
          >
            <UButton
              class="p-1"
              color="neutral"
              variant="ghost"
              size="md"
              square
              :aria-label="t('toolbar.quick_host', { host: currentHost })"
            >
              <ProxyToolbarIcon name="listen-host" />
            </UButton>
          </UTooltip>
          <template #item-trailing="{ item }">
            <UIcon v-if="item.active" name="i-lucide-check" class="size-3.5 shrink-0" />
            <span v-else class="size-3.5 shrink-0" />
          </template>
        </UDropdownMenu>

        <UTooltip
          :text="t('toolbar.quick_port', { port: currentPort })"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            color="neutral"
            variant="ghost"
            size="md"
            square
            :aria-label="t('toolbar.quick_port', { port: currentPort })"
            @click="openPortModal"
          >
            <ProxyToolbarIcon name="port" />
          </UButton>
        </UTooltip>

        <UTooltip
          v-if="systemProxySupported"
          :text="systemProxyTooltip"
          :kbds="systemProxyToggleShortcutKbds"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            icon="i-lucide-monitor-up"
            :color="systemProxyActive ? 'primary' : 'neutral'"
            variant="ghost"
            size="md"
            square
            :loading="systemProxyApplying"
			:disabled="systemProxyApplying || (!systemProxyModeSupported && !systemProxyActive)"
            :aria-pressed="systemProxyActive"
            :aria-label="systemProxyTooltip"
            @click="toggleSystemProxy"
          />
        </UTooltip>

        <UTooltip
          :text="disableProxy ? t('toolbar.disable_proxy_on') : t('toolbar.disable_proxy_off')"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            :color="disableProxy ? 'primary' : 'neutral'"
            variant="ghost"
            size="md"
            square
            :aria-pressed="disableProxy"
            :aria-label="t('toolbar.disable_proxy')"
            @click="toggleBooleanSetting('disableProxy')"
          >
            <ProxyToolbarIcon name="upstream-proxy" :active="disableProxy" />
          </UButton>
        </UTooltip>

        <UTooltip
          :text="disableHttp2 ? t('toolbar.disable_http2_on') : t('toolbar.disable_http2_off')"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            :color="disableHttp2 ? 'primary' : 'neutral'"
            variant="ghost"
            size="md"
            square
            :aria-pressed="disableHttp2"
            :aria-label="t('toolbar.disable_http2')"
            @click="toggleBooleanSetting('disableHttp2')"
          >
            <ProxyToolbarIcon name="http2" :active="disableHttp2" />
          </UButton>
        </UTooltip>

        <UTooltip
          :text="skipVerifyTls ? t('toolbar.skip_verify_tls_on') : t('toolbar.skip_verify_tls_off')"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            :color="skipVerifyTls ? 'primary' : 'neutral'"
            variant="ghost"
            size="md"
            square
            :aria-pressed="skipVerifyTls"
            :aria-label="t('toolbar.skip_verify_tls')"
            @click="toggleBooleanSetting('skipVerifyTls')"
          >
            <ProxyToolbarIcon name="tls-verify" :active="skipVerifyTls" />
          </UButton>
        </UTooltip>

        <UTooltip
          :text="t('toolbar.host_filter_tooltip', { count: hostFilterCount })"
          :content="{ side: 'bottom' }"
        >
          <UButton
            class="p-1"
            :color="hostFilterCount > 0 ? 'primary' : 'neutral'"
            variant="ghost"
            size="md"
            square
            :disabled="!proxyConfig"
            :aria-label="t('toolbar.host_filter_tooltip', { count: hostFilterCount })"
            @click="openHostFilterModal"
          >
            <ProxyToolbarIcon name="host-filter" />
          </UButton>
        </UTooltip>
      </div>
    </div>

    <div class="flex h-8 flex-[0_0_auto] items-center">
      <div class="inline-flex min-h-8 items-center gap-2">
        <UTooltip
          :text="proxyStore.isRunning ? t('toolbar.stop') : t('toolbar.start')"
          :kbds="proxyToggleShortcutKbds"
        >
          <UButton
            class="min-w-20 justify-center"
            :color="proxyStore.isRunning ? 'error' : 'primary'"
            :icon="proxyStore.isRunning ? 'i-lucide-square' : 'i-lucide-play'"
            :label="proxyStore.isRunning ? t('toolbar.stop') : t('toolbar.start')"
            :loading="proxyToggleApplying"
            :disabled="proxyToggleApplying"
            @click="handleStartStop"
          />
        </UTooltip>
        <UTooltip :text="t('toolbar.clear')" :kbds="clearTrafficShortcutKbds">
          <UButton
            color="error"
            variant="ghost"
            size="sm"
            square
            icon="i-lucide-trash-2"
            :disabled="!isCaptureTrafficActive() || trafficStore.entries.length === 0"
            :aria-label="t('toolbar.clear')"
            @click="handleClear"
          />
        </UTooltip>
      </div>
    </div>

    <HostFilterModal
      v-if="proxyConfig"
      :show="hostFilterModalOpen"
      :include-hosts="proxyConfig.includeHosts ?? []"
      :exclude-hosts="proxyConfig.excludeHosts ?? []"
      :saving="applying"
      @update:show="hostFilterModalOpen = $event"
      @save="handleHostFilterSave"
    />

    <PortConfigModal
      :show="portModalOpen"
      :port="currentPort"
      :saving="applying"
      @update:show="portModalOpen = $event"
      @save="handlePortSave"
    />
  </div>
</template>
