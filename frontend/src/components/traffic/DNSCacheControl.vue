<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useProxyStore } from '@/stores/proxy'
import { useSettingStore } from '@/stores/setting'
import { useNotify } from '@/composables/useNotify'

const applying = defineModel<boolean>('applying', { default: false })
const { t } = useI18n()
const proxyStore = useProxyStore()
const settingStore = useSettingStore()
const notify = useNotify()
const open = ref(false)
const loading = ref(false)
const loaded = ref(false)
const draftEnabled = ref(false)
const draftTTL = ref<number | null>(60)
const error = ref('')
const enabled = computed(() => proxyStore.dnsCacheStatus?.enabled ?? false)
const busy = computed(() => applying.value || loading.value || settingStore.isSaving)
const validTTL = computed(
  () =>
    typeof draftTTL.value === 'number' &&
    Number.isInteger(draftTTL.value) &&
    draftTTL.value >= 1 &&
    draftTTL.value <= 3600,
)
// These brief operations keep the controls locked without fading or resizing them.
const buttonUI = { base: 'disabled:opacity-100 aria-disabled:opacity-100' }

watch(open, async (isOpen) => {
  if (!isOpen) return
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    const status = await proxyStore.refreshDNSCacheStatus()
    draftEnabled.value = status.enabled
    draftTTL.value = status.ttlSeconds
    loaded.value = true
  } catch (cause) {
    error.value = t('toolbar.dns_cache_failed', { error: String(cause) })
  } finally {
    loading.value = false
  }
})

async function applySettings() {
  if (busy.value || !loaded.value) return
  if (!validTTL.value) {
    error.value = t('toolbar.dns_cache_invalid_ttl')
    return
  }
  applying.value = true
  error.value = ''
  try {
    if (!settingStore.settings) await settingStore.load()
    const config = settingStore.settings?.proxyConfig
    if (!config) throw new Error('proxy settings are not available')
    config.dnsCacheEnabled = draftEnabled.value
    config.dnsCacheTTLSeconds = draftTTL.value as number
    const result = await settingStore.save({ dirtySections: ['proxy'] })
    if (!result?.complete) throw new Error('proxy settings were not saved')
    await proxyStore.refreshDNSCacheStatus()
    open.value = false
  } catch (cause) {
    // Retain the draft for retry; the icon represents the backend configuration.
    const status = await proxyStore.refreshDNSCacheStatus().catch(() => null)
    const config = settingStore.settings?.proxyConfig
    if (status && config) {
      config.dnsCacheEnabled = status.enabled
      config.dnsCacheTTLSeconds = status.ttlSeconds
    }
    error.value = t('toolbar.dns_cache_failed', { error: String(cause) })
  } finally {
    applying.value = false
  }
}

async function clearCache() {
  if (busy.value) return
  applying.value = true
  error.value = ''
  try {
    await proxyStore.clearDNSCache()
    notify.success(t('toolbar.dns_cache_cleared'))
  } catch (cause) {
    error.value = t('toolbar.dns_cache_failed', { error: String(cause) })
  } finally {
    applying.value = false
  }
}
</script>

<template>
  <UPopover v-model:open="open" :dismissible="!applying" :content="{ side: 'bottom' }">
    <UTooltip :text="enabled ? t('toolbar.dns_cache_on') : t('toolbar.dns_cache_off')">
      <UButton
        class="p-1"
        :color="enabled ? 'primary' : 'neutral'"
        variant="ghost"
        size="md"
        square
        icon="i-lucide-globe"
        :disabled="applying"
        :ui="buttonUI"
        :aria-label="t('toolbar.dns_cache')"
      />
    </UTooltip>
    <template #content>
      <div class="w-72 space-y-3 p-4" :aria-busy="busy">
        <USwitch
          v-model="draftEnabled"
          :disabled="busy"
          :ui="{ root: 'opacity-100' }"
          :label="t('toolbar.dns_cache_enabled')"
        />
        <UFormField
          :label="t('toolbar.dns_cache_ttl')"
          :error="!validTTL ? t('toolbar.dns_cache_invalid_ttl') : undefined"
        >
          <UInputNumber
            v-model="draftTTL"
            :min="1"
            :max="3600"
            :step="1"
            :disabled="busy"
            :ui="{ base: 'disabled:opacity-100' }"
            :increment="{ ui: buttonUI }"
            :decrement="{ ui: buttonUI }"
            class="w-full"
          />
        </UFormField>
        <p v-if="error" role="alert" class="text-xs text-error">{{ error }}</p>
        <div class="flex justify-end gap-2">
          <UButton
            color="neutral"
            variant="outline"
            :disabled="busy"
            :ui="buttonUI"
            class="disabled:hover:bg-elevated"
            :label="t('toolbar.dns_cache_clear')"
            @click="clearCache"
          />
          <UButton
            :disabled="busy || !loaded || !validTTL"
            :ui="buttonUI"
            class="disabled:hover:bg-primary/75"
            :label="t('toolbar.dns_cache_apply')"
            @click="applySettings"
          />
        </div>
      </div>
    </template>
  </UPopover>
</template>
