<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FieldOperation } from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'

const props = defineProps<{
  operations: FieldOperation[] | null
  title: string
  disabled: boolean
}>()
const emit = defineEmits<{ update: [operations: FieldOperation[]] }>()
const { t } = useI18n()
const items = computed(() =>
  ['add', 'set', 'delete'].map((value) => ({
    value,
    label: t(`rewrite_rules.operation_${value}`),
  })),
)
function update(index: number, field: keyof FieldOperation, value: string) {
  emit(
    'update',
    (props.operations ?? []).map((operation, i) =>
      i === index ? { ...operation, [field]: value } : operation,
    ),
  )
}
function move(index: number, delta: number) {
  const operations = [...(props.operations ?? [])]
  operations.splice(index + delta, 0, operations.splice(index, 1)[0]!)
  emit('update', operations)
}
</script>

<template>
  <section class="space-y-2">
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-1">
        <h3 class="text-sm font-semibold text-highlighted">{{ title }}</h3>
        <UTooltip
          :text="t('rewrite_rules.operations_hint')"
          :ui="{ content: 'h-auto max-w-xs py-2', text: 'whitespace-normal' }"
        >
          <UButton
            icon="i-lucide-circle-help"
            size="xs"
            color="neutral"
            variant="ghost"
            :aria-label="t('rewrite_rules.operations_help')"
          />
        </UTooltip>
      </div>
      <UButton
        size="xs"
        variant="ghost"
        icon="i-lucide-plus"
        :label="t('rewrite_rules.add_operation')"
        :disabled="disabled"
        @click="emit('update', [...(operations ?? []), { operation: 'set', name: '', value: '' }])"
      />
    </div>
    <div
      v-for="(operation, index) in operations"
      :key="index"
      class="flex flex-wrap items-center gap-1.5"
    >
      <USelect
        :model-value="operation.operation"
        :items="items"
        :aria-label="t('rewrite_rules.operation')"
        :disabled="disabled"
        class="w-28"
        @update:model-value="update(index, 'operation', $event)"
      />
      <UInput
        :model-value="operation.name"
        :placeholder="t('rewrite_rules.field_name')"
        :aria-label="t('rewrite_rules.field_name')"
        :disabled="disabled"
        class="min-w-24 flex-1"
        @update:model-value="update(index, 'name', $event)"
      />
      <UInput
        v-if="operation.operation !== 'delete'"
        :model-value="operation.value"
        :placeholder="t('rewrite_rules.field_value')"
        :aria-label="t('rewrite_rules.field_value')"
        :disabled="disabled"
        class="min-w-24 flex-1"
        @update:model-value="update(index, 'value', $event)"
      />
      <UButton
        icon="i-lucide-arrow-up"
        size="xs"
        color="neutral"
        variant="ghost"
        :aria-label="t('rewrite_rules.move_up')"
        :disabled="disabled || index === 0"
        @click="move(index, -1)"
      />
      <UButton
        icon="i-lucide-arrow-down"
        size="xs"
        color="neutral"
        variant="ghost"
        :aria-label="t('rewrite_rules.move_down')"
        :disabled="disabled || index === (operations?.length ?? 0) - 1"
        @click="move(index, 1)"
      />
      <UButton
        icon="i-lucide-x"
        size="xs"
        color="neutral"
        variant="ghost"
        :aria-label="t('rewrite_rules.remove_operation')"
        :disabled="disabled"
        @click="
          emit(
            'update',
            (operations ?? []).filter((_, i) => i !== index),
          )
        "
      />
    </div>
  </section>
</template>
