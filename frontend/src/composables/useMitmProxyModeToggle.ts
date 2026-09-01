import { computed, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type { RequestProxyMode } from '@/types/request-editor'

type RestorableProxyMode = Exclude<RequestProxyMode, 'mitm'>
type RequestProxyModeSettings = {
  proxyMode: RequestProxyMode
}

const DEFAULT_RESTORED_PROXY_MODE: RestorableProxyMode = 'none'
const rememberedProxyModes = new WeakMap<RequestProxyModeSettings, RestorableProxyMode>()

function isRestorableProxyMode(mode: RequestProxyMode): mode is RestorableProxyMode {
  return mode !== 'mitm'
}

export function useMitmProxyModeToggle(
  settingsSource: MaybeRefOrGetter<RequestProxyModeSettings>,
) {
  const settings = computed(() => toValue(settingsSource))
  const lastNonMitmProxyMode = shallowRef<RestorableProxyMode>(
    rememberedProxyModes.get(settings.value) ??
      (isRestorableProxyMode(settings.value.proxyMode)
        ? settings.value.proxyMode
        : DEFAULT_RESTORED_PROXY_MODE),
  )

  watch(
    settings,
    (nextSettings) => {
      const rememberedMode = rememberedProxyModes.get(nextSettings)
      if (rememberedMode) {
        lastNonMitmProxyMode.value = rememberedMode
        return
      }

      if (isRestorableProxyMode(nextSettings.proxyMode)) {
        lastNonMitmProxyMode.value = nextSettings.proxyMode
        rememberedProxyModes.set(nextSettings, nextSettings.proxyMode)
        return
      }

      lastNonMitmProxyMode.value = DEFAULT_RESTORED_PROXY_MODE
    },
    { immediate: true },
  )

  watch(
    () => settings.value.proxyMode,
    (proxyMode) => {
      if (!isRestorableProxyMode(proxyMode)) {
        return
      }
      lastNonMitmProxyMode.value = proxyMode
      rememberedProxyModes.set(settings.value, proxyMode)
    },
    { immediate: true },
  )

  const isMitmProxyMode = computed(() => settings.value.proxyMode === 'mitm')

  function toggleMitmProxyMode() {
    if (settings.value.proxyMode === 'mitm') {
      settings.value.proxyMode = lastNonMitmProxyMode.value
      return
    }

    if (isRestorableProxyMode(settings.value.proxyMode)) {
      lastNonMitmProxyMode.value = settings.value.proxyMode
      rememberedProxyModes.set(settings.value, settings.value.proxyMode)
    }
    settings.value.proxyMode = 'mitm'
  }

  return {
    isMitmProxyMode,
    toggleMitmProxyMode,
  }
}
