<script setup lang="ts">
import { VueMonacoDiffEditor } from '@guolao/vue-monaco-editor'
import { computed, onBeforeUnmount, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { editor as MonacoEditor } from 'monaco-editor'
import MonacoBodyEditor from '@/components/common/MonacoBodyEditor.vue'
import AppLoading from '@/components/common/AppLoading.vue'
import { requiresMonacoLargeTextOptimizations } from '@/components/common/monacoLargeText'
import { remeasureMonacoFontsAfterLoad } from '@/components/common/monacoFontMeasurements'
import { useSettingStore } from '@/stores/setting'
import { useThemeStore } from '@/stores/theme'
import { useNotify } from '@/composables/useNotify'
import { copyText } from '@/utils/clipboard'
import { PreviewBody } from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/rewriteservice'
import type {
  BodyAction,
  BodyPreviewResult,
} from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'

const props = defineProps<{ body: BodyAction }>()
const { t } = useI18n()
const settingStore = useSettingStore()
const themeStore = useThemeStore()
const notify = useNotify()
const mode = shallowRef<'edit' | 'preview'>('edit')
const sample = shallowRef('')
const result = shallowRef<BodyPreviewResult | null>(null)
const error = shallowRef('')
const busy = shallowRef(false)
const copying = shallowRef(false)
const wordWrap = shallowRef(true)
const diffMounted = shallowRef(false)
const monaco = shallowRef<typeof import('monaco-editor') | null>(null)
let diffEditor: MonacoEditor.IStandaloneDiffEditor | null = null
let sequence = 0
let pending: ReturnType<typeof PreviewBody> | null = null
let timer: ReturnType<typeof setTimeout> | undefined

const largeSample = computed(() => requiresMonacoLargeTextOptimizations(sample.value))
const largeResult = computed(
  () =>
    !!result.value &&
    (largeSample.value || requiresMonacoLargeTextOptimizations(result.value.output)),
)
const hasPreview = computed(() => mode.value === 'preview' && result.value !== null)
const showDiff = computed(() => hasPreview.value && !largeResult.value)
const canCopy = computed(
  () => !copying.value && (mode.value === 'edit' || (hasPreview.value && !busy.value)),
)
const effectiveWrap = computed(() => wordWrap.value && !largeSample.value && !largeResult.value)
const monacoTheme = computed(() => (themeStore.isDark ? 'vs-dark' : 'vs'))
const font = computed(() => ({
  fontFamily: settingStore.resolvedCodeFontFamily,
  fontSize: settingStore.resolvedCodeFontSize,
}))
const diffOptions = computed<MonacoEditor.IStandaloneDiffEditorConstructionOptions>(() => ({
  ...font.value,
  automaticLayout: true,
  readOnly: true,
  originalEditable: false,
  renderSideBySide: false,
  compactMode: true,
  experimental: { useTrueInlineView: true },
  ignoreTrimWhitespace: false,
  renderMarginRevertIcon: false,
  renderGutterMenu: false,
  renderOverviewRuler: false,
  maxComputationTime: 1000,
  codeLens: false,
  colorDecorators: false,
  links: false,
  folding: false,
  occurrencesHighlight: 'off',
  selectionHighlight: false,
  quickSuggestions: false,
  hover: { enabled: 'off' },
  minimap: { enabled: false },
  stickyScroll: { enabled: false },
  scrollBeyondLastLine: false,
  wordWrap: effectiveWrap.value ? 'on' : 'off',
  diffWordWrap: effectiveWrap.value ? 'on' : 'off',
  scrollbar: { verticalScrollbarSize: 8, horizontalScrollbarSize: 8 },
}))

function refreshFonts() {
  if (monaco.value)
    void remeasureMonacoFontsAfterLoad(monaco.value, font.value).catch(() => undefined)
}
function beforeDiffMount(api: typeof import('monaco-editor')) {
  monaco.value = api
  refreshFonts()
}
function onDiffMount(editor: MonacoEditor.IStandaloneDiffEditor) {
  diffEditor = editor
}
watch(font, refreshFonts, { flush: 'post' })

function cancelPending() {
  sequence++
  clearTimeout(timer)
  timer = undefined
  void pending?.cancel().catch(() => undefined)
  pending = null
  busy.value = false
}

function editSample() {
  cancelPending()
  mode.value = 'edit'
  result.value = null
  error.value = ''
}

function invalidate() {
  cancelPending()
  error.value = ''
  if (mode.value === 'preview') {
    busy.value = true
    timer = setTimeout(() => {
      void preview()
    }, 300)
  } else {
    result.value = null
  }
}
// Getter sources avoid invalidating results when a save replaces an equivalent body object.
watch([() => props.body.pattern, () => props.body.replacement, () => sample.value], invalidate, {
  flush: 'sync',
})
onBeforeUnmount(() => {
  cancelPending()
  // Detach before disposing models; the Vue wrapper otherwise disposes them while still attached.
  const models = diffEditor?.getModel()
  diffEditor?.setModel(null)
  models?.original.dispose()
  models?.modified.dispose()
  diffEditor = null
})

async function preview() {
  cancelPending()
  mode.value = 'preview'
  error.value = ''
  if (new TextEncoder().encode(sample.value).length > 8 * 1024 * 1024) {
    result.value = null
    error.value = t('rewrite_rules.regex_test_too_large')
    return
  }
  const token = sequence
  busy.value = true
  try {
    pending = PreviewBody(
      { mode: 'regex', text: '', pattern: props.body.pattern, replacement: props.body.replacement },
      sample.value,
    )
    const next = await pending
    if (token === sequence) {
      result.value = next
      if (!largeResult.value) diffMounted.value = true
    }
  } catch (cause) {
    if (token === sequence) {
      result.value = null
      error.value = String(cause)
    }
  } finally {
    if (token === sequence) {
      pending = null
      busy.value = false
    }
  }
}

async function copy() {
  if (!canCopy.value) return
  const text = mode.value === 'edit' ? sample.value : result.value!.output
  copying.value = true
  try {
    await copyText(text)
    notify.success(t('detail.body_copied'))
  } catch (cause) {
    notify.error(t('detail.body_copy_failed', { error: String(cause) }))
  } finally {
    copying.value = false
  }
}
</script>

<template>
  <section class="space-y-3 rounded-md border border-default bg-muted/25 p-3">
    <div>
      <h3 class="text-sm font-semibold text-highlighted">{{ t('rewrite_rules.regex_test') }}</h3>
      <p class="mt-1 text-xs text-muted">{{ t('rewrite_rules.regex_test_hint') }}</p>
    </div>
    <div class="overflow-hidden rounded-md border border-default">
      <div
        class="flex flex-wrap items-center justify-between gap-2 border-b border-default px-3 py-1.5"
      >
        <span class="text-sm font-medium text-highlighted">
          {{
            t(
              mode === 'edit'
                ? 'rewrite_rules.regex_test_input'
                : 'rewrite_rules.regex_test_output',
            )
          }}
        </span>
        <div class="flex items-center gap-1">
          <UTooltip
            :text="
              t(
                mode === 'edit'
                  ? 'rewrite_rules.regex_test_copy_input'
                  : 'rewrite_rules.regex_test_copy_output',
              )
            "
          >
            <UButton
              icon="i-lucide-copy"
              color="neutral"
              variant="ghost"
              size="xs"
              :aria-label="
                t(
                  mode === 'edit'
                    ? 'rewrite_rules.regex_test_copy_input'
                    : 'rewrite_rules.regex_test_copy_output',
                )
              "
              :disabled="!canCopy"
              @click="copy"
            />
          </UTooltip>
          <UTooltip :text="t('rewrite_rules.regex_test_wrap')">
            <UButton
              icon="i-lucide-wrap-text"
              :color="effectiveWrap ? 'primary' : 'neutral'"
              variant="ghost"
              size="xs"
              :aria-label="t('rewrite_rules.regex_test_wrap')"
              :aria-pressed="effectiveWrap"
              :disabled="largeSample || largeResult"
              @click="wordWrap = !wordWrap"
            />
          </UTooltip>
          <UTooltip
            v-if="mode === 'edit'"
            :text="t('rewrite_rules.regex_test_preview')"
          >
            <UButton
              icon="i-lucide-scan-text"
              color="neutral"
              variant="ghost"
              size="xs"
              :aria-label="t('rewrite_rules.regex_test_preview')"
              @click="preview"
            />
          </UTooltip>
          <UTooltip v-else :text="t('rewrite_rules.regex_test_edit')">
            <UButton
              icon="i-lucide-pencil"
              color="neutral"
              variant="ghost"
              size="xs"
              :aria-label="t('rewrite_rules.regex_test_edit')"
              @click="editSample"
            />
          </UTooltip>
        </div>
      </div>
      <div class="relative h-72 bg-app-panel" :aria-busy="busy">
        <!-- Keep both panes laid out to retain view state. Monaco sets visibility on descendants,
             so hide the entire layer with opacity and exclude inactive panes from all input. -->
        <div
          class="absolute inset-0"
          :class="{ 'pointer-events-none opacity-0': hasPreview }"
          :inert="hasPreview"
          :aria-hidden="hasPreview"
        >
          <MonacoBodyEditor
            v-model:value="sample"
            :readonly="mode === 'preview'"
            language="plaintext"
            :word-wrap="wordWrap && !largeSample"
            :options="{ ariaLabel: t('rewrite_rules.regex_test_input') }"
            class="h-full"
          />
        </div>
        <div
          class="absolute inset-0"
          :class="{ 'pointer-events-none opacity-0': !showDiff }"
          :inert="!showDiff"
          :aria-hidden="!showDiff"
        >
          <VueMonacoDiffEditor
            v-if="diffMounted"
            :original="showDiff ? sample : ''"
            :modified="showDiff ? result!.output : ''"
            language="plaintext"
            :theme="monacoTheme"
            :options="diffOptions"
            class="regex-preview-diff"
            @before-mount="beforeDiffMount"
            @mount="onDiffMount"
          >
            <template #default><AppLoading fill /></template>
          </VueMonacoDiffEditor>
        </div>
        <MonacoBodyEditor
          v-if="hasPreview && largeResult"
          :value="result!.output"
          readonly
          language="plaintext"
          :word-wrap="false"
          :options="{ ariaLabel: t('rewrite_rules.regex_test_output') }"
          class="absolute inset-0 h-full"
        />
      </div>
    </div>
    <div
      v-if="busy || hasPreview"
      role="status"
      aria-live="polite"
      class="flex min-h-6 flex-wrap items-center gap-2 text-xs text-muted"
    >
      <template v-if="busy">
        <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />
        <span>{{ t('rewrite_rules.regex_test_updating') }}</span>
      </template>
      <template v-else-if="hasPreview">
        <UBadge :color="result!.matchCount > 0 ? 'success' : 'neutral'" variant="subtle">
          {{
            result!.matchCount > 0
              ? t('rewrite_rules.regex_test_matches', { count: result!.matchCount })
              : t('rewrite_rules.not_matched')
          }}
        </UBadge>
      </template>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error" />
  </section>
</template>

<style scoped>
/* Monaco generates these internal elements; keep its diff overlays while matching the app surface. */
.regex-preview-diff :deep(.monaco-editor),
.regex-preview-diff :deep(.monaco-editor-background) {
  background-color: var(--app-panel-bg) !important;
}
.regex-preview-diff :deep(.margin) {
  background-color: var(--app-shell-bg) !important;
}
</style>
