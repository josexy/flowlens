<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const model = defineModel<string>({ required: true })
const { t } = useI18n()

function clearSearch() {
  model.value = ''
}
</script>

<template>
  <div class="bg-app-sidebar-header p-2.5 [border-top:1px_solid_var(--app-border-color)]">
    <UInput
      v-model="model"
      leading-icon="i-lucide-search"
      :placeholder="t('category.search_placeholder')"
      class="w-full min-w-0"
      :ui="{ base: 'category-search-input pe-8' }"
    >
      <template #trailing>
        <UButton
          v-if="model"
          color="neutral"
          variant="link"
          size="xs"
          icon="i-lucide-circle-x"
          :aria-label="t('toolbar.clear')"
          @click="clearSearch"
        />
      </template>
    </UInput>
  </div>
</template>
