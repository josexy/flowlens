import { computed, reactive, shallowRef } from 'vue'
import { defineStore } from 'pinia'
import { Events } from '@wailsio/runtime'
import {
  GetState,
  SaveRule,
  DeleteRule,
  SetEnabled,
  SetRuleEnabled,
  ReorderRules,
} from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/rewriteservice'
import type {
  Rule,
  State,
  FieldOperation,
} from '#bindings/github.com/josexy/flowlens/backend/services/rewrite_service/models'

type RuleSaveResult = { saved: true; ruleId: string } | { saved: false; ruleId: null }
type PendingMutation =
  | { kind: 'save'; id: string; candidate: Rule }
  | { kind: 'delete' | 'ruleEnabled'; id: string }
  | { kind: 'enabled' | 'reorder' }
export type RuleFieldErrors = Partial<Record<'name' | 'urlPattern', string>>

interface RuleDraft {
  editorKey: string
  rule: Rule
  baseline: Rule | null
  readonly dirty: boolean
  conflict: boolean
  validationAttempted?: boolean
}

function validateRuleFields(rule: Rule): RuleFieldErrors {
  const errors: RuleFieldErrors = {}
  const encoder = new TextEncoder()
  if (!rule.name.trim()) errors.name = 'rewrite_rules.validation.name_required'
  else if (encoder.encode(rule.name).length > 256)
    errors.name = 'rewrite_rules.validation.name_too_long'
  if (!rule.urlPattern.trim()) errors.urlPattern = 'rewrite_rules.validation.url_required'
  else if (encoder.encode(rule.urlPattern).length > 16384)
    errors.urlPattern = 'rewrite_rules.validation.url_too_long'
  return errors
}

function cloneRule(rule: Rule): Rule {
  // Body strings are immutable; only the editable containers need copying.
  return {
    ...rule,
    action: {
      ...rule.action,
      headers: rule.action.headers?.map((field) => ({ ...field })) ?? null,
      query: rule.action.query?.map((field) => ({ ...field })) ?? null,
      body: { ...rule.action.body },
    },
  }
}

function sameOperations(left: FieldOperation[] | null, right: FieldOperation[] | null): boolean {
  return (
    left === right ||
    (!!left &&
      !!right &&
      left.length === right.length &&
      left.every(
        (field, index) =>
          field.operation === right[index]?.operation &&
          field.name === right[index]?.name &&
          field.value === right[index]?.value,
      ))
  )
}

// Enabled/order changes are published separately from editable rule content.
function sameContent(left: Rule, right: Rule): boolean {
  const a = left.action
  const b = right.action
  return (
    left.name === right.name &&
    left.method === right.method &&
    left.urlPattern === right.urlPattern &&
    a.version === b.version &&
    a.type === b.type &&
    a.targetURL === b.targetURL &&
    a.hostPolicy === b.hostPolicy &&
    sameOperations(a.headers, b.headers) &&
    sameOperations(a.query, b.query) &&
    a.body.mode === b.body.mode &&
    a.body.text === b.body.text &&
    a.body.pattern === b.body.pattern &&
    a.body.replacement === b.body.replacement
  )
}

// The editable rule is owned by the draft; clone persisted snapshots before passing them in.
function makeDraft(rule: Rule, baseline: Rule | null, editorKey: string): RuleDraft {
  const draft: RuleDraft = reactive({
    editorKey,
    rule,
    // Snapshots are immutable. Keep their strings without proxying the baseline.
    baseline: shallowRef(baseline),
    conflict: false,
    dirty: computed(
      (): boolean => draft.baseline === null || !sameContent(draft.rule, draft.baseline),
    ),
  })
  return draft
}

export const useRewriteRulesStore = defineStore('rewriteRules', () => {
  const state = shallowRef<State>({ revision: -1, enabled: false, rules: [] })
  const drafts = shallowRef<Record<string, RuleDraft>>({})
  const selectedId = shallowRef('')
  const loading = shallowRef(false)
  const pendingMutation = shallowRef<PendingMutation | null>(null)
  const busy = computed(() => pendingMutation.value !== null)
  const error = shallowRef('')
  const selectedDraft = computed(() => drafts.value[selectedId.value] ?? null)
  const selectedRule = computed(() => selectedDraft.value?.rule ?? null)
  const selectedFieldErrors = computed(() => fieldErrors())
  const rules = computed(() => state.value.rules ?? [])
  const dirtyIds = computed(() => Object.keys(drafts.value).filter(isDirty))
  const hasDirtyDrafts = computed(() => dirtyIds.value.length > 0)
  const rows = computed(() => [
    ...rules.value.map((rule) => ({
      key: rule.id,
      rule: drafts.value[rule.id]?.rule ?? rule,
      saved: rule,
    })),
    ...Object.entries(drafts.value)
      .filter(([id]) => !rules.value.some((rule) => rule.id === id))
      .map(([key, draft]) => ({ key, rule: draft.rule, saved: null })),
  ])
  let offChanged: (() => void) | null = null
  let refreshPromise: Promise<void> | null = null
  let requestedRevision = -1
  let generation = 0
  let draftCounter = 0

  function isDirty(id: string): boolean {
    const draft = drafts.value[id]
    return draft?.dirty ?? false
  }

  function fieldErrors(id = selectedId.value): RuleFieldErrors {
    const draft = drafts.value[id]
    return draft?.validationAttempted ? validateRuleFields(draft.rule) : {}
  }

  function validate(id = selectedId.value): boolean {
    const draft = drafts.value[id]
    if (!draft) return false
    draft.validationAttempted = true
    error.value = ''
    return Object.keys(fieldErrors(id)).length === 0
  }

  function applySnapshot(next: State) {
    if (!Number.isSafeInteger(next.revision) || next.revision <= state.value.revision) return
    const savedRules = new Map(next.rules?.map((rule) => [rule.id, rule]))
    const nextDrafts = { ...drafts.value }
    for (const [id, draft] of Object.entries(drafts.value)) {
      if (!draft.rule.id) continue
      const saved = savedRules.get(id)
      const pending = pendingMutation.value
      const saving = pending?.kind === 'save' && pending.id === id
      // Our own changed event can arrive before SaveRule returns. Keep the
      // editor stable and let the response acknowledge the submitted content.
      if (saving && saved && sameContent(saved, pending.candidate)) {
        syncMetadata(draft.rule, saved)
        continue
      }
      if (!draft.dirty && !saving) {
        if (saved && sameContent(draft.rule, saved)) {
          syncMetadata(draft.rule, saved)
          draft.baseline = saved
          draft.conflict = false
        } else if (saved) nextDrafts[id] = makeDraft(cloneRule(saved), saved, draft.editorKey)
        else delete nextDrafts[id]
      } else if (!saved || !draft.baseline || !sameContent(saved, draft.baseline)) {
        draft.conflict = true
      } else {
        draft.rule.enabled = saved.enabled
      }
    }
    state.value = next
    // The synchronous quit watcher sees one complete snapshot, and each draft's
    // computed dirty flag only compares content when that draft changes.
    drafts.value = nextDrafts
    reconcileSelection()
  }

  function syncMetadata(rule: Rule, saved: Rule) {
    rule.id = saved.id
    rule.enabled = saved.enabled
    rule.createdAt = saved.createdAt
    rule.updatedAt = saved.updatedAt
    rule.unavailableReason = saved.unavailableReason
  }

  function reconcileSelection() {
    if (
      !selectedId.value ||
      (!drafts.value[selectedId.value] && !rules.value.some((r) => r.id === selectedId.value))
    ) {
      selectedId.value = rows.value[0]?.key ?? ''
    }
    if (selectedId.value) select(selectedId.value)
  }

  async function refresh(showLoading = true) {
    if (showLoading) loading.value = true
    if (refreshPromise) return refreshPromise
    const token = generation
    refreshPromise = (async () => {
      // Coalesce events, but fetch again when an event overtakes an in-flight read.
      do {
        const targetRevision = requestedRevision
        const next = await GetState()
        if (token !== generation) return
        applySnapshot(next)
        if (requestedRevision <= next.revision || requestedRevision <= targetRevision) break
      } while (token === generation)
    })()
      .catch((cause) => {
        if (token === generation) error.value = String(cause)
      })
      .finally(() => {
        if (token === generation) {
          loading.value = false
          refreshPromise = null
        }
      })
    return refreshPromise
  }

  async function initialize() {
    if (!offChanged) {
      offChanged = Events.On('rewrite:changed', (event) => {
        const revision = (event.data as { revision?: number } | null)?.revision
        if (
          typeof revision !== 'number' ||
          !Number.isSafeInteger(revision) ||
          revision <= state.value.revision
        )
          return
        requestedRevision = Math.max(requestedRevision, revision)
        void refresh(false)
      })
    }
    if (state.value.revision < 0) await refresh()
  }

  function select(id: string) {
    if (!drafts.value[id]) {
      const saved = rules.value.find((rule) => rule.id === id)
      if (!saved) return
      drafts.value = { ...drafts.value, [id]: makeDraft(cloneRule(saved), saved, id) }
    }
    selectedId.value = id
  }

  function create(name: string) {
    const id = `draft:${++draftCounter}`
    const rule: Rule = {
      id: '',
      name,
      enabled: false,
      method: 'ALL',
      urlPattern: 'https://example.com/*',
      action: {
        version: 1,
        type: 'request',
        targetURL: '',
        hostPolicy: 'target',
        headers: [],
        query: [],
        body: { mode: 'none', text: '', pattern: '', replacement: '' },
      },
      createdAt: 0,
      updatedAt: 0,
      unavailableReason: '',
    }
    drafts.value = { ...drafts.value, [id]: makeDraft(rule, null, id) }
    selectedId.value = id
    error.value = ''
  }

  function update(rule: Rule) {
    const draft = selectedDraft.value
    if (draft && !draft.rule.unavailableReason) {
      draft.rule = cloneRule(rule)
      error.value = ''
    }
  }

  function revert(id = selectedId.value) {
    const saved = rules.value.find((rule) => rule.id === id)
    const nextDrafts = { ...drafts.value }
    if (saved) nextDrafts[id] = makeDraft(cloneRule(saved), saved, drafts.value[id]?.editorKey ?? id)
    else delete nextDrafts[id]
    drafts.value = nextDrafts
    reconcileSelection()
    error.value = ''
  }

  function acceptConflict(id = selectedId.value) {
    const draft = drafts.value[id]
    if (!draft) return
    const saved = rules.value.find((rule) => rule.id === id)
    if (saved) {
      draft.baseline = saved
      draft.rule.enabled = saved.enabled
    } else {
      draft.rule.id = ''
      draft.rule.enabled = false
      draft.baseline = null
    }
    draft.conflict = false
  }

  async function mutate(
    mutation: PendingMutation,
    operation: (revision: number) => Promise<State>,
    after?: (next: State) => void,
  ) {
    if (busy.value || state.value.revision < 0) return false
    pendingMutation.value = mutation
    error.value = ''
    try {
      const next = await operation(state.value.revision)
      after?.(next)
      applySnapshot(next)
      reconcileSelection()
      return true
    } catch (cause) {
      const message = String(cause)
      await refresh(false)
      error.value = message
      return false
    } finally {
      pendingMutation.value = null
    }
  }

  async function save(id = selectedId.value): Promise<RuleSaveResult> {
    const draft = drafts.value[id]
    if (busy.value || !draft || draft.conflict) return { saved: false, ruleId: null }
    if (!validate(id)) return { saved: false, ruleId: null }
    if (!isDirty(id)) return { saved: true, ruleId: draft.rule.id }
    const candidate = cloneRule(draft.rule)
    const priorIds = new Set(rules.value.map((rule) => rule.id))
    let savedId: string | null = null
    const persisted = await mutate(
      { kind: 'save', id, candidate },
      (revision) => SaveRule(candidate, revision),
      (next) => {
        const saved = candidate.id
          ? next.rules?.find((rule) => rule.id === candidate.id)
          : next.rules?.find((rule) => !priorIds.has(rule.id))
        if (!saved) return
        savedId = saved.id
        const nextDrafts = { ...drafts.value }
        delete nextDrafts[id]
        // A changed event can refresh a newer revision before this write returns.
        // Keep edits made while saving against the newest persisted baseline.
        const current =
          state.value.revision > next.revision
            ? rules.value.find((rule) => rule.id === saved.id)
            : saved
        const editedWhileSaving = !sameContent(draft.rule, candidate)
        let nextId = current?.id
        if (current) {
          const editable =
            editedWhileSaving || sameContent(draft.rule, current) ? draft.rule : cloneRule(current)
          syncMetadata(editable, current)
          const nextDraft = makeDraft(editable, current, draft.editorKey)
          nextDraft.conflict = editedWhileSaving && !sameContent(current, saved)
          nextDrafts[current.id] = nextDraft
        } else if (editedWhileSaving) {
          nextId = id
          draft.rule.id = ''
          draft.rule.enabled = false
          const nextDraft = makeDraft(draft.rule, null, draft.editorKey)
          nextDraft.conflict = true
          nextDrafts[id] = nextDraft
        }
        drafts.value = nextDrafts
        if (selectedId.value === id) {
          selectedId.value = nextId ?? rows.value[0]?.key ?? ''
          if (selectedId.value) select(selectedId.value)
        }
      },
    )
    return persisted && savedId ? { saved: true, ruleId: savedId } : { saved: false, ruleId: null }
  }

  async function saveAll() {
    if (busy.value) return false
    for (const id of [...dirtyIds.value]) {
      if (!(await save(id)).saved) {
        select(id)
        return false
      }
    }
    return !hasDirtyDrafts.value
  }

  function discardAll() {
    const savedRules = new Map(rules.value.map((rule) => [rule.id, rule]))
    const nextDrafts = { ...drafts.value }
    for (const id of dirtyIds.value) {
      const saved = savedRules.get(id)
      if (saved) nextDrafts[id] = makeDraft(cloneRule(saved), saved, drafts.value[id]?.editorKey ?? id)
      else delete nextDrafts[id]
    }
    drafts.value = nextDrafts
    reconcileSelection()
    error.value = ''
  }

  async function remove(id = selectedId.value) {
    const saved = rules.value.find((rule) => rule.id === id)
    if (!saved) {
      revert(id)
      return true
    }
    return mutate(
      { kind: 'delete', id },
      (revision) => DeleteRule(id, revision),
      () => {
        const nextDrafts = { ...drafts.value }
        delete nextDrafts[id]
        drafts.value = nextDrafts
      },
    )
  }

  function setEnabled(enabled: boolean) {
    return mutate({ kind: 'enabled' }, (revision) => SetEnabled(enabled, revision))
  }

  function setRuleEnabled(id: string, enabled: boolean) {
    return mutate({ kind: 'ruleEnabled', id }, (revision) => SetRuleEnabled(id, enabled, revision))
  }

  function reorder(ids: string[]) {
    return mutate({ kind: 'reorder' }, (revision) => ReorderRules(ids, revision))
  }

  function cleanup() {
    generation++
    offChanged?.()
    offChanged = null
    refreshPromise = null
    loading.value = false
  }

  return {
    state,
    selectedId,
    selectedDraft,
    selectedRule,
    selectedFieldErrors,
    rules,
    rows,
    dirtyIds,
    hasDirtyDrafts,
    loading,
    busy,
    pendingMutation,
    error,
    isDirty,
    fieldErrors,
    validate,
    initialize,
    refresh,
    select,
    create,
    update,
    revert,
    acceptConflict,
    save,
    saveAll,
    discardAll,
    remove,
    setEnabled,
    setRuleEnabled,
    reorder,
    cleanup,
  }
})
