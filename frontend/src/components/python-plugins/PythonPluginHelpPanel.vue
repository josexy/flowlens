<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'
import { PYTHON_PLUGIN_MINIMAL_SCRIPT, type PythonHelpScope } from '@/utils/pythonPluginApiCatalog'
import {
  buildPythonPluginAIContext,
  getPythonPluginApiGroups,
  getPythonPluginHelpMessages,
} from '@/utils/pythonPluginHelp'

const props = defineProps<{ scope: PythonHelpScope }>()
const emit = defineEmits<{ close: [] }>()
const activeTab = defineModel<string>('tab', { default: 'quickstart' })
const query = defineModel<string>('query', { default: '' })
const { locale, t } = useI18n()
const notify = useNotify()
const messages = computed(() => getPythonPluginHelpMessages(locale.value))
const tabs = computed(() => [
  { value: 'quickstart', label: messages.value.quickstart },
  { value: 'reference', label: messages.value.reference },
])
const steps = computed(() => [
  messages.value.start.runtime,
  messages.value.start[props.scope].edit,
  messages.value.start[props.scope].enable,
])
const quickstartDetails = computed(() => [
  { value: 'template', label: messages.value.minimal_template, slot: 'template' },
  { value: 'runtime', label: messages.value.runtime_details, slot: 'runtime' },
])
const groups = computed(() => {
  const summaries: Record<string, string> = messages.value.group_summaries
  return getPythonPluginApiGroups(locale.value, query.value).map((group) => ({
    ...group,
    value: group.name,
    label: group.name,
    summary: summaries[group.name],
  }))
})
const expandedGroups = ref<string[]>([])
watch(
  groups,
  (items) => {
    expandedGroups.value = query.value.trim() ? items.map((item) => item.value) : []
  },
  { immediate: true },
)

async function copy(source: string, success: string) {
  try {
    await copyText(source)
    notify.success(success)
  } catch (error) {
    notify.error(t('python_plugin_help.copy_failed', { error: String(error) }))
  }
}

function copyContext() {
  return copy(
    buildPythonPluginAIContext({ scope: props.scope, locale: locale.value }),
    messages.value.copied_context,
  )
}
</script>

<template>
  <aside
    class="flex h-full min-h-0 min-w-0 flex-col bg-default text-default"
    :aria-label="messages.title"
  >
    <div class="shrink-0 space-y-3 border-b border-default p-4">
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0">
          <h2 class="text-sm font-semibold">{{ messages.title }}</h2>
          <p class="mt-1 text-xs text-muted">{{ messages.version }}</p>
        </div>
        <UButton
          icon="i-lucide-x"
          color="neutral"
          variant="ghost"
          size="sm"
          :aria-label="messages.close"
          @click="emit('close')"
        />
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <UBadge color="neutral" variant="subtle" size="sm">{{ messages[scope] }}</UBadge>
        <UButton
          icon="i-lucide-copy"
          size="sm"
          :label="messages.copy_context"
          @click="copyContext"
        />
      </div>
      <UTabs
        v-model="activeTab"
        :items="tabs"
        :content="false"
        size="sm"
        :ui="{ list: 'w-full' }"
      />
    </div>

    <div class="min-h-0 flex-1 overflow-auto overscroll-contain p-4 text-sm leading-relaxed">
      <div v-if="activeTab === 'quickstart'" class="space-y-5">
        <p class="text-muted">{{ messages.start.ai_hint }}</p>
        <ol class="space-y-4">
          <li v-for="(step, index) in steps" :key="index" class="flex gap-3">
            <span
              class="flex size-5 shrink-0 items-center justify-center rounded-full border border-default text-xs text-muted"
              >{{ index + 1 }}</span
            >
            <div class="min-w-0 space-y-1">
              <h3 class="font-medium">{{ step.title }}</h3>
              <p class="text-xs text-muted">{{ step.text }}</p>
            </div>
          </li>
        </ol>
        <section class="space-y-2 border-t border-default pt-4">
          <h3 class="text-xs font-semibold">{{ messages.start.essentials_title }}</h3>
          <ul class="list-disc space-y-2 ps-4 text-xs text-muted">
            <li v-for="(text, key) in messages.start.essentials" :key="key">{{ text }}</li>
          </ul>
        </section>
        <UAccordion
          :items="quickstartDetails"
          :ui="{ content: 'motion-reduce:animate-none', body: 'space-y-3' }"
        >
          <template #template-body>
            <UButton
              icon="i-lucide-copy"
              color="neutral"
              variant="ghost"
              size="xs"
              :label="messages.copy_template"
              @click="copy(PYTHON_PLUGIN_MINIMAL_SCRIPT, messages.copied_template)"
            />
            <pre
              class="overflow-x-auto rounded-md bg-elevated p-3 font-mono text-xs leading-relaxed"
            ><code>{{ PYTHON_PLUGIN_MINIMAL_SCRIPT }}</code></pre>
          </template>
          <template #runtime-body>
            <ul class="list-disc space-y-2 ps-4 text-xs text-muted">
              <li v-for="(text, key) in messages.runtime_summary" :key="key">{{ text }}</li>
            </ul>
          </template>
        </UAccordion>
      </div>

      <div v-else class="space-y-3">
        <UInput
          v-model="query"
          icon="i-lucide-search"
          :placeholder="messages.search"
          :aria-label="messages.search"
          class="w-full"
        />
        <UCollapsible :ui="{ content: 'motion-reduce:animate-none' }">
          <template #default="{ open }">
            <UButton
              color="neutral"
              variant="link"
              size="xs"
              :icon="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              :label="messages.import_notes"
              class="px-0"
            />
          </template>
          <template #content>
            <p class="py-2 text-xs text-muted">{{ messages.provided_hint }}</p>
          </template>
        </UCollapsible>
        <div v-if="!groups.length" class="py-8 text-center">
          <p class="font-medium">{{ messages.no_results }}</p>
          <p class="mt-1 text-xs text-muted">{{ messages.no_results_hint }}</p>
        </div>
        <UAccordion
          v-else
          v-model="expandedGroups"
          :items="groups"
          type="multiple"
          :ui="{ label: 'min-w-0', content: 'motion-reduce:animate-none', body: 'space-y-3' }"
        >
          <template #default="{ item }">
            <span class="block font-mono text-xs font-semibold">{{ item.name }}</span>
            <span class="mt-0.5 block text-xs font-normal text-muted">{{ item.summary }}</span>
          </template>
          <template #body="{ item: group }">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
              <UBadge color="neutral" variant="outline" size="sm">
                {{
                  group.name === 'Hooks'
                    ? messages.hook
                    : group.importable
                      ? messages.importable
                      : messages.provided
                }}
              </UBadge>
              <UCollapsible
                v-if="group.note"
                :ui="{ content: 'basis-full motion-reduce:animate-none' }"
                class="contents"
              >
                <template #default="{ open }">
                  <UButton
                    color="neutral"
                    variant="link"
                    size="xs"
                    :icon="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                    :label="messages.usage_notes"
                    class="px-0"
                  />
                </template>
                <template #content>
                  <p class="whitespace-pre-line py-2 text-xs text-muted">{{ group.note }}</p>
                </template>
              </UCollapsible>
            </div>
            <div class="divide-y divide-default border-y border-default">
              <div v-for="entry in group.entries" :key="entry.signature" class="space-y-1.5 py-3">
                <div class="flex items-start justify-between gap-2">
                  <code class="min-w-0 wrap-anywhere font-mono text-xs text-toned">{{
                    entry.signature.replace(/\s+# read-only$/, '')
                  }}</code>
                  <UBadge
                    v-if="entry.kind === 'property'"
                    color="neutral"
                    variant="subtle"
                    size="sm"
                    class="shrink-0"
                  >
                    {{ entry.writable ? messages.writable : messages.readonly }}
                  </UBadge>
                </div>
                <p class="text-xs text-muted">{{ entry.description }}</p>
              </div>
            </div>
          </template>
        </UAccordion>
      </div>
    </div>
  </aside>
</template>
