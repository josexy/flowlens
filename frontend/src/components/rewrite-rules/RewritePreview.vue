<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Preview } from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/rewriteservice'
import type {
  Rule,
  PreviewResult,
} from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'

const props = defineProps<{ rule: Rule; validate: () => boolean }>()
const { t } = useI18n()
const method = shallowRef('GET')
const url = shallowRef('https://example.com/example')
const result = shallowRef<PreviewResult | null>(null)
const error = shallowRef('')
const busy = shallowRef(false)
let sequence = 0
watch([() => props.rule, method, url], () => {
  sequence++
  result.value = null
  error.value = ''
  busy.value = false
})
async function preview() {
  const token = ++sequence
  busy.value = false
  error.value = ''
  result.value = null
  if (!props.validate()) {
    error.value = t('rewrite_rules.validation.fix_fields')
    return
  }
  busy.value = true
  try {
    const next = await Preview(props.rule, method.value, url.value)
    if (token === sequence) result.value = next
  } catch (cause) {
    if (token === sequence) error.value = String(cause)
  } finally {
    if (token === sequence) busy.value = false
  }
}
</script>

<template>
  <section class="space-y-3 rounded-md border border-default bg-muted/25 p-3">
    <div>
      <h3 class="text-sm font-semibold text-highlighted">{{ t('rewrite_rules.preview') }}</h3>
      <p class="mt-1 text-xs text-muted">{{ t('rewrite_rules.preview_hint') }}</p>
    </div>
    <div class="flex flex-wrap gap-2">
      <UInput v-model="method" :aria-label="t('rewrite_rules.method')" class="w-24" />
      <UInput
        v-model="url"
        :aria-label="t('rewrite_rules.test_url')"
        :placeholder="t('rewrite_rules.test_url')"
        class="min-w-48 flex-1"
      />
      <UButton :label="t('rewrite_rules.test_match')" :loading="busy" @click="preview" />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error" />
    <div v-if="result" class="space-y-2 text-xs">
      <UBadge :color="result.matched ? 'success' : 'neutral'" variant="subtle">{{
        t(result.matched ? 'rewrite_rules.matched' : 'rewrite_rules.not_matched')
      }}</UBadge>
      <div v-for="(capture, index) in result.captures" :key="index" class="font-mono break-all">
        <span class="text-muted">{{ index + 1 }}:</span> {{ capture }}
      </div>
      <div v-if="result.targetURL" class="space-y-1">
        <p class="text-muted">{{ t('rewrite_rules.expanded_target') }}</p>
        <p class="font-mono break-all">{{ result.targetURL }}</p>
      </div>
    </div>
  </section>
</template>
