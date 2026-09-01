<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import type {
  CommonConfig,
  MainWindowCloseBehavior,
  WindowConfig,
  WindowFrameMode,
} from '#bindings/github.com/josexy/flowlens/backend/services/setting_service/models'

type SelectOption<T extends string = string> = {
  label: string
  value: T
}

const DEFAULT_FONT_OPTION_VALUE = '__default_font__'

defineProps<{
  fontOptions: SelectOption[]
  isLoadingFonts: boolean
  windowFrameModeOptions: SelectOption<WindowFrameMode>[]
  mainWindowCloseBehaviorOptions: SelectOption<MainWindowCloseBehavior>[]
  windowFrameModePendingRestart: boolean
}>()
const emit = defineEmits<{
  'font-select-open': [open: boolean]
}>()

const commonConfig = defineModel<CommonConfig>('commonConfig', { required: true })
const windowConfig = defineModel<WindowConfig>('windowConfig', { required: true })

const { t } = useI18n()

const appFontFamilyModel = computed({
  get: () => commonConfig.value.appFontFamily || DEFAULT_FONT_OPTION_VALUE,
  set: (value: string) => {
    commonConfig.value.appFontFamily = value === DEFAULT_FONT_OPTION_VALUE ? '' : value
  },
})

const codeFontFamilyModel = computed({
  get: () => commonConfig.value.codeFontFamily || DEFAULT_FONT_OPTION_VALUE,
  set: (value: string) => {
    commonConfig.value.codeFontFamily = value === DEFAULT_FONT_OPTION_VALUE ? '' : value
  },
})

const fontSelectSearchInput = computed(() => ({
  placeholder: t('settings.font_search_placeholder'),
}))
const fontSelectVirtualize = { estimateSize: 32, overscan: 8 }
</script>

<template>
  <div class="min-w-0">
    <SettingsSection :title="t('settings.section_appearance')">
      <SettingsRow :label="t('settings.app_font_label')">
        <USelectMenu
          v-model="appFontFamilyModel"
          :items="fontOptions"
          value-key="value"
          :search-input="fontSelectSearchInput"
          :virtualize="fontSelectVirtualize"
          :loading="isLoadingFonts"
          class="w-[min(360px,100%)]"
          @update:open="emit('font-select-open', $event)"
        />
        <template #aside>
          <span
            class="block min-w-45 max-w-70 truncate text-sm text-muted"
            :style="{ fontFamily: commonConfig.appFontFamily || 'var(--app-font-family)' }"
          >
            {{ t('settings.font_preview_text') }}
          </span>
        </template>
      </SettingsRow>
      <SettingsRow :label="t('settings.code_font_label')">
        <USelectMenu
          v-model="codeFontFamilyModel"
          :items="fontOptions"
          value-key="value"
          :search-input="fontSelectSearchInput"
          :virtualize="fontSelectVirtualize"
          :loading="isLoadingFonts"
          class="w-[min(360px,100%)]"
          @update:open="emit('font-select-open', $event)"
        />
        <template #aside>
          <span
            class="block min-w-45 max-w-70 truncate font-(family-name:--code-font-family) text-sm text-muted"
            :style="{ fontFamily: commonConfig.codeFontFamily || 'var(--code-font-family)' }"
          >
            {{ t('settings.code_font_preview_text') }}
          </span>
        </template>
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :title="t('settings.section_behavior')">
      <SettingsRow :label="t('settings.window_frame_mode_label')">
        <USelect
          v-model="windowConfig.frameMode"
          :items="windowFrameModeOptions"
          class="w-[min(360px,100%)]"
        />
      </SettingsRow>
      <SettingsRow :label="t('settings.main_window_close_behavior_label')">
        <USelect
          v-model="windowConfig.mainWindowCloseBehavior"
          :items="mainWindowCloseBehaviorOptions"
          class="w-[min(360px,100%)]"
        />
      </SettingsRow>

      <UAlert
        v-if="windowFrameModePendingRestart"
        color="warning"
        variant="subtle"
        :title="t('settings.window_frame_mode_restart_notice')"
        class="mt-3 w-full"
      />
    </SettingsSection>
  </div>
</template>
