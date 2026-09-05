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
      <SettingsRow :label="t('settings.app_font_label')" wide align="start">
        <div class="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
          <USelectMenu
            v-model="appFontFamilyModel"
            :items="fontOptions"
            value-key="value"
            :search-input="fontSelectSearchInput"
            :virtualize="fontSelectVirtualize"
            :loading="isLoadingFonts"
            :aria-label="t('settings.app_font_label')"
            class="w-full max-w-80"
            @update:open="emit('font-select-open', $event)"
          />
          <span
            class="block max-w-full truncate text-sm text-app-text-muted"
            :style="{ fontFamily: commonConfig.appFontFamily || 'var(--app-font-family)' }"
          >
            {{ t('settings.font_preview_text') }}
          </span>
        </div>
      </SettingsRow>
      <SettingsRow :label="t('settings.code_font_label')" wide align="start">
        <div class="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
          <USelectMenu
            v-model="codeFontFamilyModel"
            :items="fontOptions"
            value-key="value"
            :search-input="fontSelectSearchInput"
            :virtualize="fontSelectVirtualize"
            :loading="isLoadingFonts"
            :aria-label="t('settings.code_font_label')"
            class="w-full max-w-80"
            @update:open="emit('font-select-open', $event)"
          />
          <span
            class="block max-w-full truncate font-(family-name:--code-font-family) text-sm text-app-text-muted"
            :style="{ fontFamily: commonConfig.codeFontFamily || 'var(--code-font-family)' }"
          >
            {{ t('settings.code_font_preview_text') }}
          </span>
        </div>
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :title="t('settings.section_behavior')">
      <SettingsRow :label="t('settings.window_frame_mode_label')" wide>
        <USelect
          v-model="windowConfig.frameMode"
          :items="windowFrameModeOptions"
          :aria-label="t('settings.window_frame_mode_label')"
          class="w-full max-w-80"
        />
      </SettingsRow>
      <SettingsRow :label="t('settings.main_window_close_behavior_label')" wide>
        <USelect
          v-model="windowConfig.mainWindowCloseBehavior"
          :items="mainWindowCloseBehaviorOptions"
          :aria-label="t('settings.main_window_close_behavior_label')"
          class="w-full max-w-80"
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
