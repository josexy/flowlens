<script setup lang="ts">
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { computed, nextTick, onBeforeUnmount, shallowRef, watch } from 'vue'
import type { editor as MonacoEditor } from 'monaco-editor'
import AppLoading from '@/components/common/AppLoading.vue'
import { useSettingStore } from '@/stores/setting'
import { useThemeStore } from '@/stores/theme'
import {
  registerMonacoLightLanguages,
  resolveMonacoLightLanguage,
} from '@/components/common/monacoLightLanguages'
import { requiresMonacoLargeTextOptimizations } from '@/components/common/monacoLargeText'
import {
  syncMonacoModelBracketPairColorization,
  type MonacoBracketPairColorizationOptions,
} from '@/components/common/monacoModelOptions'
import { getReadonlyMonacoAppendText } from '@/components/common/monacoTextUpdate'
import {
  remeasureMonacoFontsAfterLoad,
  type MonacoFontMeasurementRequest,
} from '@/components/common/monacoFontMeasurements'

type MonacoEditorOptions = MonacoEditor.IStandaloneEditorConstructionOptions
type MonacoApi = typeof import('monaco-editor')

const props = withDefaults(
  defineProps<{
    value: string
    language?: string
    readonly?: boolean
    wordWrap?: boolean
    allowLargeTextWordWrap?: boolean
    followTailOnAppend?: boolean
    flowLensPythonApi?: boolean
    options?: MonacoEditorOptions
  }>(),
  {
    language: 'plaintext',
    readonly: false,
    wordWrap: true,
    allowLargeTextWordWrap: false,
    followTailOnAppend: false,
    flowLensPythonApi: false,
    options: () => ({}),
  },
)

const emit = defineEmits<{
  'update:value': [value: string]
}>()

const themeStore = useThemeStore()
const settingStore = useSettingStore()

const EDITOR_SCROLLBAR_GUTTER_WIDTH = 10
const EDITOR_FONT_SIZE = 13
const TAIL_FOLLOW_THRESHOLD_PX = 80
const editorInstance = shallowRef<MonacoEditor.IStandaloneCodeEditor | null>(null)
const monacoInstance = shallowRef<MonacoApi | null>(null)
const editorModelValue = shallowRef(props.value)
let modelLanguageChangeDisposable: { dispose(): void } | null = null

const editorValue = computed({
  get: () => editorModelValue.value,
  set: (value) => emit('update:value', value),
})

const usesLargeTextOptimizations = computed(
  () =>
    props.readonly &&
    (props.allowLargeTextWordWrap || requiresMonacoLargeTextOptimizations(props.value)),
)
const usesFlowLensPythonApi = computed(
  () =>
    props.flowLensPythonApi &&
    props.language === 'python' &&
    !usesLargeTextOptimizations.value,
)
const editorLanguage = computed(() =>
  usesLargeTextOptimizations.value
    ? 'plaintext'
    : resolveMonacoLightLanguage(props.language, usesFlowLensPythonApi.value),
)
const editorWordWrap = computed(
  () =>
    props.wordWrap &&
    (!usesLargeTextOptimizations.value || props.allowLargeTextWordWrap),
)
const modelBracketPairColorization = computed<MonacoBracketPairColorizationOptions>(() => {
  if (usesLargeTextOptimizations.value) {
    return { enabled: false, independentColorPoolPerBracketType: false }
  }

  const option = props.options.bracketPairColorization
  const configured =
    option && typeof option === 'object' ? (option as Record<string, unknown>) : {}
  return {
    enabled: typeof configured.enabled === 'boolean' ? configured.enabled : true,
    independentColorPoolPerBracketType:
      typeof configured.independentColorPoolPerBracketType === 'boolean'
        ? configured.independentColorPoolPerBracketType
        : false,
  }
})
const monacoTheme = computed(() => (themeStore.isDark ? 'vs-dark' : 'vs'))
const editorFrameStyle = computed(() => ({
  '--editor-frame-right-gutter': `${EDITOR_SCROLLBAR_GUTTER_WIDTH}px`,
  ...(themeStore.isDark
    ? {
        '--editor-bg': 'var(--app-panel-bg)',
        '--editor-gutter-bg': 'var(--app-shell-bg)',
        '--editor-active-line-bg': 'rgba(255,255,255,0.035)',
      }
    : {
        '--editor-bg': 'var(--app-panel-bg)',
        '--editor-gutter-bg': 'var(--app-shell-bg)',
        '--editor-active-line-bg': 'rgba(0,0,0,0.035)',
      }),
}))

const editorOptions = computed<MonacoEditorOptions>(() => {
  const baseScrollbar = {
    verticalScrollbarSize: 8,
    horizontalScrollbarSize: 8,
  }
  const scrollbarOption =
    props.options.scrollbar && typeof props.options.scrollbar === 'object'
      ? (props.options.scrollbar as Record<string, unknown>)
      : {}
  const suggestionOptions = usesFlowLensPythonApi.value
    ? {
        quickSuggestions: { other: true, comments: false, strings: false },
        suggestOnTriggerCharacters: true,
        acceptSuggestionOnEnter: 'smart',
        wordBasedSuggestions: 'off',
        parameterHints: { enabled: true },
        hover: { enabled: 'on' as const },
      } as const
    : {
        quickSuggestions: false,
        suggestOnTriggerCharacters: false,
        acceptSuggestionOnEnter: 'off',
        wordBasedSuggestions: 'off',
        parameterHints: { enabled: false },
        hover: { enabled: 'off' as const },
      } as const

  return {
    readOnly: props.readonly,
    minimap: { enabled: false },
    stickyScroll: { enabled: false },
    scrollBeyondLastLine: false,
    fontSize: EDITOR_FONT_SIZE,
    fontFamily: settingStore.resolvedCodeFontFamily,
    automaticLayout: true,
    ...suggestionOptions,
    formatOnPaste: false,
    formatOnType: false,
    lineNumbers: 'on' as const,
    renderWhitespace: 'none' as const,
    ...props.options,
    bracketPairColorization: modelBracketPairColorization.value,
    matchBrackets: usesLargeTextOptimizations.value
      ? ('never' as const)
      : (props.options.matchBrackets ?? 'always'),
    wordWrap: usesLargeTextOptimizations.value && !props.allowLargeTextWordWrap
      ? ('off' as const)
      : (props.options.wordWrap ?? (editorWordWrap.value ? 'on' : 'off')),
    wordWrapOverride1: usesLargeTextOptimizations.value && !props.allowLargeTextWordWrap
      ? ('off' as const)
      : (props.options.wordWrapOverride1 ?? 'inherit'),
    wordWrapOverride2: usesLargeTextOptimizations.value && !props.allowLargeTextWordWrap
      ? ('off' as const)
      : (props.options.wordWrapOverride2 ?? 'inherit'),
    scrollbar: {
      ...baseScrollbar,
      ...scrollbarOption,
    },
  }
})

const editorFontMeasurementRequest = computed<MonacoFontMeasurementRequest>(() => ({
  fontFamily:
    typeof props.options.fontFamily === 'string'
      ? props.options.fontFamily
      : settingStore.resolvedCodeFontFamily,
  fontSize:
    typeof props.options.fontSize === 'number'
      ? props.options.fontSize
      : EDITOR_FONT_SIZE,
  fontWeight:
    typeof props.options.fontWeight === 'string'
      ? props.options.fontWeight
      : 'normal',
}))

function refreshMonacoFontMeasurements() {
  const monaco = monacoInstance.value
  if (!monaco) {
    return
  }
  void remeasureMonacoFontsAfterLoad(monaco, editorFontMeasurementRequest.value).catch(
    () => undefined,
  )
}

function handleBeforeMount(monaco: MonacoApi) {
  monacoInstance.value = monaco
  registerMonacoLightLanguages(monaco)
}

function handleMount(editor: MonacoEditor.IStandaloneCodeEditor) {
  editorInstance.value = editor
  const model = editor.getModel()
  modelLanguageChangeDisposable?.dispose()
  modelLanguageChangeDisposable =
    model?.onDidChangeLanguage(() => {
      syncMonacoModelBracketPairColorization(model, modelBracketPairColorization.value)
    }) ?? null
  prepareLargeTextModelUpdate(editor, model)
  refreshMonacoFontMeasurements()
}

function prepareLargeTextModelUpdate(
  editor: MonacoEditor.IStandaloneCodeEditor,
  model: MonacoEditor.ITextModel | null,
  forceWordWrapOff = false,
) {
  if (!model) {
    return false
  }

  const monaco = monacoInstance.value
  if (
    usesLargeTextOptimizations.value &&
    monaco &&
    model.getLanguageId() !== editorLanguage.value
  ) {
    monaco.editor.setModelLanguage(model, editorLanguage.value)
  }
  syncMonacoModelBracketPairColorization(model, modelBracketPairColorization.value)
  if (!usesLargeTextOptimizations.value) {
    return false
  }

  // The wrapper applies value before language/options. Switch the expensive
  // settings off first when a live body crosses into large-text mode.
  const shouldDisableWordWrap = forceWordWrapOff || !props.allowLargeTextWordWrap
  editor.updateOptions({
    bracketPairColorization: { enabled: false },
    matchBrackets: 'never',
    ...(shouldDisableWordWrap
      ? {
          wordWrap: 'off',
          wordWrapOverride1: 'off',
          wordWrapOverride2: 'off',
        }
      : {}),
  })

  return forceWordWrapOff && props.allowLargeTextWordWrap
}

onBeforeUnmount(() => {
  modelLanguageChangeDisposable?.dispose()
  modelLanguageChangeDisposable = null
})

watch(
  editorFontMeasurementRequest,
  async () => {
    await nextTick()
    refreshMonacoFontMeasurements()
  },
  { flush: 'post' },
)

watch(
  modelBracketPairColorization,
  (options) => {
    const model = editorInstance.value?.getModel()
    if (model) {
      syncMonacoModelBracketPairColorization(model, options)
    }
  },
  { deep: true, flush: 'pre' },
)

watch(
  () => props.value,
  async (nextValue, previousValue) => {
    const editor = editorInstance.value
    if (nextValue === previousValue) {
      return
    }

    const model = editor?.getModel() ?? null
    const shouldRestoreLargeTextWordWrap = editor
      ? prepareLargeTextModelUpdate(editor, model, true)
      : false
    const incrementalAppendText =
      props.followTailOnAppend &&
      props.readonly &&
      !!editor &&
      !!model &&
      getReadonlyMonacoAppendText(previousValue, nextValue, model.getValue())
    const canApplyIncrementalAppend = typeof incrementalAppendText === 'string'

    if (!canApplyIncrementalAppend) {
      editorModelValue.value = nextValue
      if (
        !props.followTailOnAppend ||
        !editor ||
        !nextValue.startsWith(previousValue)
      ) {
        if (shouldRestoreLargeTextWordWrap && editor) {
          await nextTick()
          if (editorInstance.value === editor) {
            editor.updateOptions(editorOptions.value)
          }
        }
        return
      }
    }

    const activeEditor = editor!
    const previousScrollTop = activeEditor.getScrollTop()
    const viewportHeight = activeEditor.getLayoutInfo().height
    const wasNearBottom =
      activeEditor.getScrollHeight() - previousScrollTop - viewportHeight <
      TAIL_FOLLOW_THRESHOLD_PX

    if (canApplyIncrementalAppend) {
      if (incrementalAppendText) {
        const lastLine = model!.getLineCount()
        const lastColumn = model!.getLineMaxColumn(lastLine)
        model!.applyEdits([
          {
            range: {
              startLineNumber: lastLine,
              startColumn: lastColumn,
              endLineNumber: lastLine,
              endColumn: lastColumn,
            },
            text: incrementalAppendText,
            forceMoveMarkers: true,
          },
        ])
      }
      editorModelValue.value = model!.getValue()
    }

    await nextTick()
    if (editorInstance.value !== activeEditor) {
      return
    }
    if (shouldRestoreLargeTextWordWrap) {
      activeEditor.updateOptions(editorOptions.value)
    }
    activeEditor.setScrollTop(
      wasNearBottom ? activeEditor.getScrollHeight() : previousScrollTop,
    )
  },
  { flush: 'pre' },
)
</script>

<template>
  <div
    class="relative flex min-h-0 w-full min-w-0 flex-1 overflow-hidden bg-app-panel before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:right-(--editor-frame-right-gutter) before:z-2 before:border before:border-app-border before:content-['']"
    :style="editorFrameStyle"
  >
    <div class="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden bg-(--editor-bg)">
      <VueMonacoEditor
        v-model:value="editorValue"
        :language="editorLanguage"
        :theme="monacoTheme"
        :options="editorOptions"
        class="monaco-body-editor__instance min-h-0 w-full min-w-0 flex-1 bg-(--editor-bg)"
        @before-mount="handleBeforeMount"
        @mount="handleMount"
      >
        <template #default>
          <AppLoading fill />
        </template>
      </VueMonacoEditor>
    </div>
  </div>
</template>

<style scoped>
/* Monaco editor internals (third-party generated DOM) reached via :deep with
   !important to override Monaco's inline styles. Inherently scoped CSS — there
   is no Tailwind-utility way to reach .monaco-editor / .margin / .line-numbers
   etc. inside the editor instance. */
.monaco-body-editor__instance :deep(.monaco-editor),
.monaco-body-editor__instance :deep(.overflow-guard),
.monaco-body-editor__instance :deep(.monaco-editor-background) {
  border-radius: 0;
  background-color: var(--editor-bg) !important;
}

.monaco-body-editor__instance :deep(.margin) {
  background-color: var(--editor-gutter-bg) !important;
  border-right: 1px solid var(--app-border-color);
}

.monaco-body-editor__instance :deep(.monaco-scrollable-element > .scrollbar) {
  background-color: var(--editor-bg) !important;
  z-index: 20 !important;
}

.monaco-body-editor__instance :deep(.monaco-editor .view-overlays .current-line),
.monaco-body-editor__instance :deep(.monaco-editor .view-overlays .selected-text) {
  max-width: calc(100% - var(--editor-frame-right-gutter)) !important;
}

.monaco-body-editor__instance :deep(.monaco-editor .current-line) {
  background-color: var(--editor-active-line-bg) !important;
  border-color: transparent !important;
}

.monaco-body-editor__instance :deep(.monaco-editor .line-numbers) {
  color: var(--app-text-muted) !important;
}
</style>
