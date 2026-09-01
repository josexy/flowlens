<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

type HostFilterRow = {
  id: number
  value: string
}

type HostFilterScope = 'include' | 'exclude'

const props = defineProps<{
  show: boolean
  includeHosts: string[]
  excludeHosts: string[]
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  save: [value: { includeHosts: string[]; excludeHosts: string[] }]
}>()

const { t } = useI18n()
const includeHostRows = ref<HostFilterRow[]>([])
const excludeHostRows = ref<HostFilterRow[]>([])
const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})
let nextRowId = 1

function createRows(values: string[]) {
  return values.map((value) => ({
    id: nextRowId++,
    value,
  }))
}

watch(
  () => props.show,
  (show) => {
    if (!show) {
      return
    }
    includeHostRows.value = createRows(props.includeHosts)
    excludeHostRows.value = createRows(props.excludeHosts)
  },
  { immediate: true },
)

function getRows(scope: HostFilterScope) {
  return scope === 'include' ? includeHostRows : excludeHostRows
}

function addHostFilterRow(scope: HostFilterScope) {
  const rows = getRows(scope)
  rows.value = [
    ...rows.value,
    {
      id: nextRowId++,
      value: '',
    },
  ]
}

function removeHostFilterRow(scope: HostFilterScope, rowId: number) {
  const rows = getRows(scope)
  rows.value = rows.value.filter((row) => row.id !== rowId)
}

function normalizeRows(rows: HostFilterRow[]) {
  return rows.map((row) => row.value.trim()).filter(Boolean)
}

function handleCancel() {
  emit('update:show', false)
}

function handleSave() {
  emit('save', {
    includeHosts: normalizeRows(includeHostRows.value),
    excludeHosts: normalizeRows(excludeHostRows.value),
  })
}
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="t('toolbar.host_filter_title')"
    :close="!props.saving"
    :dismissible="!props.saving"
  >
    <template #body>
      <div class="flex max-h-[clamp(320px,calc(100vh-260px),460px)] flex-col gap-4.5 overflow-y-auto pr-1">
        <section class="min-w-0">
          <div class="mb-2 flex items-center justify-between gap-3">
            <div class="flex items-center gap-2 text-sm font-semibold text-app-text">
              {{ t('settings.include_hosts') }}
              <span class="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-app-control px-1.5 text-sm font-medium text-app-text-muted">{{ includeHostRows.length }}</span>
            </div>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-plus"
              :disabled="props.saving"
              :label="t('settings.add_domain_filter')"
              @click="addHostFilterRow('include')"
            />
          </div>

          <div
            v-if="includeHostRows.length > 0"
            class="flex max-h-[clamp(180px,calc(100vh-440px),220px)] flex-col gap-2 overflow-y-auto rounded-lg border border-app-border bg-app-elevated p-2.5 overscroll-contain scrollbar-gutter-stable"
          >
            <div v-for="row in includeHostRows" :key="row.id" class="grid grid-cols-[minmax(0,1fr)_30px] items-center gap-2">
              <UInput
                v-model="row.value"
                :disabled="props.saving"
                :placeholder="t('settings.host_filter_placeholder')"
                @keydown.enter.prevent="addHostFilterRow('include')"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-trash-2"
                :disabled="props.saving"
                :aria-label="t('settings.remove_domain_filter')"
                @click="removeHostFilterRow('include', row.id)"
              />
            </div>
          </div>
        </section>

        <section class="min-w-0">
          <div class="mb-2 flex items-center justify-between gap-3">
            <div class="flex items-center gap-2 text-sm font-semibold text-app-text">
              {{ t('settings.exclude_hosts') }}
              <span class="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-app-control px-1.5 text-sm font-medium text-app-text-muted">{{ excludeHostRows.length }}</span>
            </div>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-plus"
              :disabled="props.saving"
              :label="t('settings.add_domain_filter')"
              @click="addHostFilterRow('exclude')"
            />
          </div>

          <div
            v-if="excludeHostRows.length > 0"
            class="flex max-h-[clamp(180px,calc(100vh-440px),220px)] flex-col gap-2 overflow-y-auto rounded-lg border border-app-border bg-app-elevated p-2.5 overscroll-contain scrollbar-gutter-stable"
          >
            <div v-for="row in excludeHostRows" :key="row.id" class="grid grid-cols-[minmax(0,1fr)_30px] items-center gap-2">
              <UInput
                v-model="row.value"
                :disabled="props.saving"
                :placeholder="t('settings.host_filter_placeholder')"
                @keydown.enter.prevent="addHostFilterRow('exclude')"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-trash-2"
                :disabled="props.saving"
                :aria-label="t('settings.remove_domain_filter')"
                @click="removeHostFilterRow('exclude', row.id)"
              />
            </div>
          </div>
        </section>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton
          color="neutral"
          variant="outline"
          :disabled="props.saving"
          :label="t('toolbar.cancel')"
          @click="handleCancel"
        />
        <UButton :loading="props.saving" :label="t('toolbar.save')" @click="handleSave" />
      </div>
    </template>
  </UModal>
</template>
