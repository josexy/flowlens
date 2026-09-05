<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import { useI18n } from 'vue-i18n'
import {
  UpstreamProxyMode,
  type ProxyConfig,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

type UpstreamProxyModeValue =
  | UpstreamProxyMode.UpstreamProxyModeNone
  | UpstreamProxyMode.UpstreamProxyModeSystem
  | UpstreamProxyMode.UpstreamProxyModeCustom

const proxyConfig = defineModel<ProxyConfig>('proxyConfig', { required: true })

const { t } = useI18n()

const upstreamProxyModeOptions = computed(() => [
  { label: t('settings.upstream_proxy_mode_none'), value: UpstreamProxyMode.UpstreamProxyModeNone },
  {
    label: t('settings.upstream_proxy_mode_system'),
    value: UpstreamProxyMode.UpstreamProxyModeSystem,
  },
  {
    label: t('settings.upstream_proxy_mode_custom'),
    value: UpstreamProxyMode.UpstreamProxyModeCustom,
  },
])

function isUpstreamProxyMode(value: string | undefined): value is UpstreamProxyModeValue {
  return (
    value === UpstreamProxyMode.UpstreamProxyModeNone ||
    value === UpstreamProxyMode.UpstreamProxyModeSystem ||
    value === UpstreamProxyMode.UpstreamProxyModeCustom
  )
}

function resolveUpstreamProxyMode(): UpstreamProxyModeValue {
  if (isUpstreamProxyMode(proxyConfig.value.upstreamProxyMode)) {
    return proxyConfig.value.upstreamProxyMode
  }
  if (proxyConfig.value.upstreamProxy?.trim()) {
    return UpstreamProxyMode.UpstreamProxyModeCustom
  }
  if (proxyConfig.value.disableProxy) {
    return UpstreamProxyMode.UpstreamProxyModeNone
  }
  return UpstreamProxyMode.UpstreamProxyModeSystem
}

const upstreamProxyMode = shallowRef<UpstreamProxyModeValue>(resolveUpstreamProxyMode())
const showCustomUpstreamProxy = computed(
  () => upstreamProxyMode.value === UpstreamProxyMode.UpstreamProxyModeCustom,
)

watch(
  () => [proxyConfig.value.upstreamProxyMode, proxyConfig.value.upstreamProxy] as const,
  () => {
    const resolvedMode = resolveUpstreamProxyMode()
    if (
      upstreamProxyMode.value === UpstreamProxyMode.UpstreamProxyModeCustom &&
      !proxyConfig.value.upstreamProxy?.trim()
    ) {
      return
    }
    upstreamProxyMode.value = resolvedMode
  },
)

function handleUpstreamProxyModeUpdate(value: unknown) {
  if (typeof value !== 'string' || !isUpstreamProxyMode(value)) {
    return
  }

  const nextMode = value
  upstreamProxyMode.value = nextMode
  proxyConfig.value.upstreamProxyMode = nextMode

  if (nextMode !== UpstreamProxyMode.UpstreamProxyModeCustom) {
    proxyConfig.value.upstreamProxy = ''
  }
}
</script>

<template>
  <SettingsRow :label="t('toolbar.upstream_proxy')" wide>
    <div class="flex min-w-0 flex-col gap-2">
      <USelect
        :model-value="upstreamProxyMode"
        :items="upstreamProxyModeOptions"
        :aria-label="t('toolbar.upstream_proxy')"
        class="w-full max-w-80"
        @update:model-value="handleUpstreamProxyModeUpdate"
      />
      <UInput
        v-if="showCustomUpstreamProxy"
        v-model="proxyConfig.upstreamProxy"
        :placeholder="t('settings.upstream_proxy_custom_placeholder')"
        :aria-label="t('settings.upstream_proxy_mode_custom')"
        class="w-full"
      />
    </div>
  </SettingsRow>
</template>
