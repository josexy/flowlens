<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RuleFieldErrors } from '@/stores/rewriteRules'
import type {
  Rule,
  Action,
} from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'
import RewriteFieldOperations from './RewriteFieldOperations.vue'
import RewriteBodyAction from './RewriteBodyAction.vue'
import RewritePreview from './RewritePreview.vue'
import RewriteBodyPreview from './RewriteBodyPreview.vue'

const props = defineProps<{
  rule: Rule
  disabled: boolean
  errors: RuleFieldErrors
  validate: () => boolean
}>()
const emit = defineEmits<{ update: [rule: Rule] }>()
const { t } = useI18n()
const methods = ['ALL', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'TRACE']
const actions = computed(() =>
  ['redirect', 'request', 'response'].map((value) => ({
    value,
    label: t(`rewrite_rules.action_${value}`),
  })),
)
const policies = computed(() =>
  ['target', 'preserve'].map((value) => ({ value, label: t(`rewrite_rules.host_${value}`) })),
)
const editsMessage = computed(
  () => props.rule.action.type === 'request' || props.rule.action.type === 'response',
)
function update(field: keyof Rule, value: Rule[keyof Rule]) {
  emit('update', { ...props.rule, [field]: value })
}
function updateAction(patch: Partial<Action>) {
  update('action', { ...props.rule.action, ...patch })
}
function changeAction(type: string) {
  // Hidden controls must not retain operations unsupported by the chosen action.
  updateAction({
    type,
    targetURL: type === 'redirect' ? props.rule.action.targetURL : '',
    headers: type === 'request' || type === 'response' ? props.rule.action.headers : [],
    query: type === 'request' ? props.rule.action.query : [],
    body:
      type === 'request' || type === 'response'
        ? props.rule.action.body
        : { mode: 'none', text: '', pattern: '', replacement: '' },
  })
}
</script>

<template>
  <div class="mx-auto w-full max-w-4xl space-y-5 p-4">
    <div class="grid grid-cols-[1fr_8rem] gap-3">
      <UFormField
        name="name"
        :label="t('rewrite_rules.name')"
        :error="errors.name ? t(errors.name) : undefined"
        required
        ><UInput
          :model-value="rule.name"
          :disabled="disabled"
          class="w-full"
          @update:model-value="update('name', $event)"
      /></UFormField>
      <UFormField :label="t('rewrite_rules.method')"
        ><USelect
          :model-value="rule.method"
          :items="methods"
          :disabled="disabled"
          class="w-full"
          @update:model-value="update('method', $event)"
      /></UFormField>
    </div>
    <UFormField
      name="urlPattern"
      :label="t('rewrite_rules.url_pattern')"
      :error="errors.urlPattern ? t(errors.urlPattern) : undefined"
      required
    >
      <UInput
        :model-value="rule.urlPattern"
        :disabled="disabled"
        class="w-full font-mono"
        @update:model-value="update('urlPattern', $event)"
      />
      <p v-if="!errors.urlPattern" class="mt-1.5 text-xs text-muted">{{ t('rewrite_rules.pattern_hint') }}</p>
    </UFormField>
    <UFormField :label="t('rewrite_rules.action')"
      ><USelect
        :model-value="rule.unavailableReason ? undefined : rule.action.type"
        :items="actions"
        :placeholder="t('rewrite_rules.unavailable')"
        :disabled="disabled"
        class="w-64"
        @update:model-value="changeAction"
    /></UFormField>
    <template v-if="rule.action.type === 'redirect'">
      <UFormField :label="t('rewrite_rules.target_url')" required
        ><UInput
          :model-value="rule.action.targetURL"
          :disabled="disabled"
          class="w-full font-mono"
          @update:model-value="updateAction({ targetURL: $event })"
      /></UFormField>
      <p class="text-xs text-muted">
        {{ t('rewrite_rules.target_hint', { capture: '${1}', capture2: '${2}' }) }}
      </p>
      <UFormField :label="t('rewrite_rules.host_policy')"
        ><USelect
          :model-value="rule.action.hostPolicy"
          :items="policies"
          :disabled="disabled"
          class="w-64"
          @update:model-value="updateAction({ hostPolicy: $event })"
      /></UFormField>
    </template>
    <template v-if="editsMessage">
      <RewriteFieldOperations
        :operations="rule.action.headers"
        :title="
          t(
            rule.action.type === 'request'
              ? 'rewrite_rules.request_headers'
              : 'rewrite_rules.response_headers',
          )
        "
        :disabled="disabled"
        @update="updateAction({ headers: $event })"
      />
      <RewriteFieldOperations
        v-if="rule.action.type === 'request'"
        :operations="rule.action.query"
        :title="t('rewrite_rules.query')"
        :disabled="disabled"
        @update="updateAction({ query: $event })"
      />
      <RewriteBodyAction
        :body="rule.action.body"
        :disabled="disabled"
        @update="updateAction({ body: $event })"
      />
    </template>
    <RewritePreview :rule="rule" :validate="validate" />
    <RewriteBodyPreview
      v-if="editsMessage && rule.action.body.mode === 'regex'"
      :body="rule.action.body"
    />
  </div>
</template>
