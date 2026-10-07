<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import type { BodyAction } from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'

const props = defineProps<{ body: BodyAction; disabled: boolean }>()
const emit = defineEmits<{ update: [body: BodyAction] }>()
const { t } = useI18n()
const modes = computed(() =>
  ['none', 'replace', 'regex'].map((value) => ({ value, label: t(`rewrite_rules.body_${value}`) })),
)
function update(field: keyof BodyAction, value: string) {
  emit('update', { ...props.body, [field]: value })
}
</script>

<template>
  <section class="space-y-3">
    <UFormField :label="t('rewrite_rules.body')">
      <USelect
        :model-value="body.mode"
        :items="modes"
        :disabled="disabled"
        class="w-52"
        @update:model-value="update('mode', $event)"
      />
    </UFormField>
    <p v-if="body.mode !== 'none'" class="text-xs text-muted">
      {{ t('rewrite_rules.body_hint') }}
    </p>
    <template v-if="body.mode === 'regex'">
      <UFormField :label="t('rewrite_rules.regex_pattern')">
        <UInput
          :model-value="body.pattern"
          :disabled="disabled"
          class="w-full font-mono"
          @update:model-value="update('pattern', $event)"
        />
      </UFormField>
      <p class="text-xs text-muted">{{ t('rewrite_rules.regex_hint') }}</p>
    </template>
    <div v-if="body.mode !== 'none'" class="space-y-1.5">
      <p class="text-sm font-medium text-highlighted">
        {{
          t(
            body.mode === 'regex'
              ? 'rewrite_rules.regex_replacement'
              : 'rewrite_rules.replacement_text',
          )
        }}
      </p>
      <div class="h-56 overflow-hidden rounded-md border border-default">
        <MonacoBodyEditor
          :value="body.mode === 'regex' ? body.replacement : body.text"
          :readonly="disabled"
          language="plaintext"
          :word-wrap="true"
          class="h-full"
          @update:value="update(body.mode === 'regex' ? 'replacement' : 'text', $event)"
        />
      </div>
    </div>
  </section>
</template>
