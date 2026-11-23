package store

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
)

// Rule types of the rewrite service.
type (
	RewriteRule  = rewriteservice.Rule
	RewriteState = rewriteservice.State
)

const (
	jobRewriteRead   = "rewrite:read"
	jobRewriteMutate = "rewrite:mutate"

	maxRuleNameBytes    = 256
	maxRulePatternBytes = 16384
)

// Field problems of a rule.
const (
	RuleErrorNameRequired = "name_required"
	RuleErrorNameTooLong  = "name_too_long"
	RuleErrorURLRequired  = "url_required"
	RuleErrorURLTooLong   = "url_too_long"
)

// Kinds of pending mutation.
const (
	RewriteMutationSave        = "save"
	RewriteMutationDelete      = "delete"
	RewriteMutationRuleEnabled = "ruleEnabled"
	RewriteMutationEnabled     = "enabled"
	RewriteMutationReorder     = "reorder"
)

// RewriteBackend reads and changes rewrite rules.
type RewriteBackend interface {
	GetState(ctx context.Context) (RewriteState, error)
	SaveRule(rule RewriteRule, expectedRevision int64) (RewriteState, error)
	DeleteRule(id string, expectedRevision int64) (RewriteState, error)
	SetRuleEnabled(id string, enabled bool, expectedRevision int64) (RewriteState, error)
	SetEnabled(enabled bool, expectedRevision int64) (RewriteState, error)
	ReorderRules(ids []string, expectedRevision int64) (RewriteState, error)
}

// RuleFieldErrors names the problems of a rule's fields.
type RuleFieldErrors struct {
	Name, URLPattern string
}

// Empty reports whether no field has a problem.
func (e RuleFieldErrors) Empty() bool { return e.Name == "" && e.URLPattern == "" }

// First returns one of the problems.
func (e RuleFieldErrors) First() string {
	if e.Name != "" {
		return e.Name
	}
	return e.URLPattern
}

// RuleDraft is an edited copy of a rule.
type RuleDraft struct {
	EditorKey           string
	Rule                *RewriteRule
	Baseline            *RewriteRule
	Conflict            bool
	ValidationAttempted bool
}

// Dirty reports whether the draft differs from the saved rule.
func (d *RuleDraft) Dirty() bool { return d.Baseline == nil || !SameRuleContent(*d.Rule, *d.Baseline) }

// RewriteMutation is the write in progress.
type RewriteMutation struct {
	Kind, ID  string
	Candidate *RewriteRule
	Enabled   bool
}

// RewriteRow is one row of the rule list.
type RewriteRow struct {
	Key   string
	Rule  *RewriteRule
	Saved *RewriteRule
}

// RuleSaveResult tells how a save went.
type RuleSaveResult struct {
	Saved  bool
	RuleID string
}

// RewriteStore holds rewrite rules and their drafts.
type RewriteStore struct {
	rt      Runtime
	backend RewriteBackend
	connect Topic[rewriteservice.Changed]

	State      RewriteState
	SelectedID string
	Loading    bool
	Pending    *RewriteMutation
	Error      string

	drafts     map[string]*RuleDraft
	draftOrder []string

	off               func()
	refreshing        bool
	waiters           []func()
	requestedRevision int64
	generation        uint64
	draftCounter      int
}

// NewRewriteStore returns an unloaded store that follows changes, when
// connect is set.
func NewRewriteStore(rt Runtime, backend RewriteBackend, connect Topic[rewriteservice.Changed]) *RewriteStore {
	return &RewriteStore{rt: rt, backend: backend, connect: connect, State: RewriteState{Revision: -1}, drafts: map[string]*RuleDraft{}, requestedRevision: -1}
}

func validateRuleFields(rule *RewriteRule) RuleFieldErrors {
	var errors RuleFieldErrors
	switch {
	case isBlank(rule.Name):
		errors.Name = RuleErrorNameRequired
	case len(rule.Name) > maxRuleNameBytes:
		errors.Name = RuleErrorNameTooLong
	}
	switch {
	case isBlank(rule.URLPattern):
		errors.URLPattern = RuleErrorURLRequired
	case len(rule.URLPattern) > maxRulePatternBytes:
		errors.URLPattern = RuleErrorURLTooLong
	}
	return errors
}

func isBlank(value string) bool { return strings.TrimSpace(value) == "" }

// CloneRule copies a rule's editable containers; strings are shared.
func CloneRule(rule RewriteRule) *RewriteRule {
	clone := rule
	clone.Action.Headers = slices.Clone(rule.Action.Headers)
	clone.Action.Query = slices.Clone(rule.Action.Query)
	return &clone
}

// SameRuleContent compares what the editor can change. Enabled state and
// ordering are published separately.
func SameRuleContent(left, right RewriteRule) bool {
	a, b := left.Action, right.Action
	return left.Name == right.Name && left.Method == right.Method && left.URLPattern == right.URLPattern &&
		a.Version == b.Version && a.Type == b.Type && a.TargetURL == b.TargetURL && a.HostPolicy == b.HostPolicy &&
		slices.Equal(a.Headers, b.Headers) && slices.Equal(a.Query, b.Query) && a.Body == b.Body
}

func (s *RewriteStore) savedRule(id string) *RewriteRule {
	for index := range s.State.Rules {
		if s.State.Rules[index].ID == id {
			return &s.State.Rules[index]
		}
	}
	return nil
}

func (s *RewriteStore) setDraft(key string, draft *RuleDraft) {
	if _, ok := s.drafts[key]; !ok {
		s.draftOrder = append(s.draftOrder, key)
	}
	s.drafts[key] = draft
}

func (s *RewriteStore) deleteDraft(key string) {
	if _, ok := s.drafts[key]; ok {
		delete(s.drafts, key)
		s.draftOrder = slices.DeleteFunc(s.draftOrder, func(candidate string) bool { return candidate == key })
	}
}

// SelectedDraft returns the draft on show.
func (s *RewriteStore) SelectedDraft() *RuleDraft { return s.drafts[s.SelectedID] }

// SelectedRule returns the rule on show.
func (s *RewriteStore) SelectedRule() *RewriteRule {
	if draft := s.SelectedDraft(); draft != nil {
		return draft.Rule
	}
	return nil
}

// Draft returns a draft by key.
func (s *RewriteStore) Draft(id string) *RuleDraft { return s.drafts[id] }

// Busy reports whether a write is in progress.
func (s *RewriteStore) Busy() bool { return s.Pending != nil }

// Ready reports whether the saved rules have loaded.
func (s *RewriteStore) Ready() bool { return s.State.Revision >= 0 }

// IsDirty reports whether a draft has unsaved edits.
func (s *RewriteStore) IsDirty(id string) bool {
	draft := s.drafts[id]
	return draft != nil && draft.Dirty()
}

// DirtyIDs lists the drafts with unsaved edits.
func (s *RewriteStore) DirtyIDs() []string {
	var ids []string
	for _, key := range s.draftOrder {
		if s.drafts[key].Dirty() {
			ids = append(ids, key)
		}
	}
	return ids
}

// HasDirtyDrafts reports whether any draft has unsaved edits.
func (s *RewriteStore) HasDirtyDrafts() bool {
	return slices.ContainsFunc(s.draftOrder, func(key string) bool { return s.drafts[key].Dirty() })
}

// Rows lists the saved rules, then the unsaved drafts.
func (s *RewriteStore) Rows() []RewriteRow {
	rows := make([]RewriteRow, 0, len(s.State.Rules)+len(s.draftOrder))
	for index := range s.State.Rules {
		saved := &s.State.Rules[index]
		row := RewriteRow{Key: saved.ID, Rule: saved, Saved: saved}
		if draft := s.drafts[saved.ID]; draft != nil {
			row.Rule = draft.Rule
		}
		rows = append(rows, row)
	}
	for _, key := range s.draftOrder {
		if s.savedRule(key) == nil {
			rows = append(rows, RewriteRow{Key: key, Rule: s.drafts[key].Rule})
		}
	}
	return rows
}

// FieldErrors returns a draft's problems once validation has been tried.
func (s *RewriteStore) FieldErrors(id string) RuleFieldErrors {
	if draft := s.drafts[id]; draft != nil && draft.ValidationAttempted {
		return validateRuleFields(draft.Rule)
	}
	return RuleFieldErrors{}
}

// Validate checks a draft and marks it as checked.
func (s *RewriteStore) Validate(id string) bool {
	draft := s.drafts[id]
	if draft == nil {
		return false
	}
	draft.ValidationAttempted = true
	s.Error = ""
	return s.FieldErrors(id).Empty()
}

func syncRuleMetadata(rule *RewriteRule, saved *RewriteRule) {
	rule.ID, rule.Enabled, rule.CreatedAt, rule.UpdatedAt, rule.UnavailableReason = saved.ID, saved.Enabled, saved.CreatedAt, saved.UpdatedAt, saved.UnavailableReason
}

func (s *RewriteStore) applySnapshot(next RewriteState) {
	if next.Revision <= s.State.Revision {
		return
	}
	saved := map[string]*RewriteRule{}
	for index := range next.Rules {
		saved[next.Rules[index].ID] = &next.Rules[index]
	}
	for _, key := range slices.Clone(s.draftOrder) {
		draft := s.drafts[key]
		if draft.Rule.ID == "" {
			continue
		}
		current := saved[key]
		saving := s.Pending != nil && s.Pending.Kind == RewriteMutationSave && s.Pending.ID == key
		// Our own change event can arrive before the save returns. Keep the
		// editor stable and let the response acknowledge the content.
		if saving && current != nil && SameRuleContent(*current, *s.Pending.Candidate) {
			syncRuleMetadata(draft.Rule, current)
			continue
		}
		switch {
		case !draft.Dirty() && !saving:
			switch {
			case current != nil && SameRuleContent(*draft.Rule, *current):
				syncRuleMetadata(draft.Rule, current)
				draft.Baseline, draft.Conflict = current, false
			case current != nil:
				s.drafts[key] = &RuleDraft{EditorKey: draft.EditorKey, Rule: CloneRule(*current), Baseline: current}
			default:
				s.deleteDraft(key)
			}
		case current == nil || draft.Baseline == nil || !SameRuleContent(*current, *draft.Baseline):
			draft.Conflict = true
		default:
			draft.Rule.Enabled = current.Enabled
		}
	}
	s.State = next
	s.reconcileSelection()
}

func (s *RewriteStore) reconcileSelection() {
	if s.SelectedID == "" || (s.drafts[s.SelectedID] == nil && s.savedRule(s.SelectedID) == nil) {
		s.SelectedID = ""
		if rows := s.Rows(); len(rows) > 0 {
			s.SelectedID = rows[0].Key
		}
	}
	if s.SelectedID != "" {
		s.Select(s.SelectedID)
	}
}

// Refresh reads the saved state. Reads coalesce, and a read overtaken by a
// change is repeated.
func (s *RewriteStore) Refresh(showLoading bool, done func()) {
	if showLoading {
		s.Loading = true
	}
	if done != nil {
		s.waiters = append(s.waiters, done)
	}
	if s.refreshing {
		return
	}
	s.refreshing = true
	s.read(s.generation)
}

func (s *RewriteStore) read(token uint64) {
	target := s.requestedRevision
	runtime.Run(s.rt.Jobs, jobRewriteRead, s.backend.GetState, func(next RewriteState, err error) {
		if token != s.generation {
			return
		}
		if err != nil {
			s.Error = err.Error()
			s.finishRefresh()
			return
		}
		s.applySnapshot(next)
		if s.requestedRevision <= next.Revision || s.requestedRevision <= target {
			s.finishRefresh()
			return
		}
		s.read(token)
	})
}

func (s *RewriteStore) finishRefresh() {
	s.Loading, s.refreshing = false, false
	waiters := s.waiters
	s.waiters = nil
	for _, waiter := range waiters {
		waiter()
	}
}

// Initialize follows changes and loads the state once.
func (s *RewriteStore) Initialize() {
	if s.off == nil && s.connect != nil {
		s.off = s.connect.Subscribe(func(changed rewriteservice.Changed) {
			s.rt.Dispatcher.Post(func() { s.handleChanged(changed.Revision) })
		})
	}
	if s.State.Revision < 0 && !s.refreshing {
		s.Refresh(true, nil)
	}
}

func (s *RewriteStore) handleChanged(revision int64) {
	if revision <= s.State.Revision {
		return
	}
	s.requestedRevision = max(s.requestedRevision, revision)
	s.Refresh(false, nil)
}

// Select shows a rule, drafting it on first use.
func (s *RewriteStore) Select(id string) {
	if s.drafts[id] == nil {
		saved := s.savedRule(id)
		if saved == nil {
			return
		}
		s.setDraft(id, &RuleDraft{EditorKey: id, Rule: CloneRule(*saved), Baseline: saved})
	}
	s.SelectedID = id
}

// Create starts a new disabled rule.
func (s *RewriteStore) Create(name string) {
	s.draftCounter++
	id := fmt.Sprintf("draft:%d", s.draftCounter)
	rule := &RewriteRule{Name: name, Method: "ALL", URLPattern: "https://example.com/*", Action: rewriteservice.Action{
		Version: 1, Type: rewriteservice.ActionRequest, HostPolicy: "target", Headers: []rewriteservice.FieldOperation{}, Query: []rewriteservice.FieldOperation{}, Body: rewriteservice.BodyAction{Mode: "none"},
	}}
	s.setDraft(id, &RuleDraft{EditorKey: id, Rule: rule})
	s.SelectedID = id
	s.Error = ""
}

// Update replaces the selected draft's content.
func (s *RewriteStore) Update(rule RewriteRule) {
	if draft := s.SelectedDraft(); draft != nil && draft.Rule.UnavailableReason == "" {
		draft.Rule = CloneRule(rule)
		s.Error = ""
	}
}

// Revert restores a draft's saved content.
func (s *RewriteStore) Revert(id string) {
	if id == "" {
		id = s.SelectedID
	}
	if saved := s.savedRule(id); saved != nil {
		key := id
		if draft := s.drafts[id]; draft != nil {
			key = draft.EditorKey
		}
		s.setDraft(id, &RuleDraft{EditorKey: key, Rule: CloneRule(*saved), Baseline: saved})
	} else {
		s.deleteDraft(id)
	}
	s.reconcileSelection()
	s.Error = ""
}

// AcceptConflict keeps a draft's edits over a change made elsewhere.
func (s *RewriteStore) AcceptConflict(id string) {
	if id == "" {
		id = s.SelectedID
	}
	draft := s.drafts[id]
	if draft == nil {
		return
	}
	if saved := s.savedRule(id); saved != nil {
		draft.Baseline = saved
		draft.Rule.Enabled = saved.Enabled
	} else {
		draft.Rule.ID, draft.Rule.Enabled, draft.Baseline = "", false, nil
	}
	draft.Conflict = false
}

func (s *RewriteStore) mutate(mutation RewriteMutation, operation func(revision int64) (RewriteState, error), after func(RewriteState), done func(bool)) {
	if s.Busy() || !s.Ready() {
		done(false)
		return
	}
	s.Pending, s.Error = &mutation, ""
	revision := s.State.Revision
	runtime.Run(s.rt.Jobs, jobRewriteMutate, func(context.Context) (RewriteState, error) {
		return operation(revision)
	}, func(next RewriteState, err error) {
		if err != nil {
			message := err.Error()
			s.Refresh(false, func() {
				s.Error = message
				s.Pending = nil
				done(false)
			})
			return
		}
		if after != nil {
			after(next)
		}
		s.applySnapshot(next)
		s.reconcileSelection()
		s.Pending = nil
		done(true)
	})
}

// Save writes a draft.
func (s *RewriteStore) Save(id string, done func(RuleSaveResult)) {
	if id == "" {
		id = s.SelectedID
	}
	draft := s.drafts[id]
	if s.Busy() || draft == nil || draft.Conflict || !s.Validate(id) {
		done(RuleSaveResult{})
		return
	}
	if !draft.Dirty() {
		done(RuleSaveResult{Saved: true, RuleID: draft.Rule.ID})
		return
	}
	candidate := CloneRule(*draft.Rule)
	prior := map[string]bool{}
	for index := range s.State.Rules {
		prior[s.State.Rules[index].ID] = true
	}
	savedID := ""
	s.mutate(RewriteMutation{Kind: RewriteMutationSave, ID: id, Candidate: candidate}, func(revision int64) (RewriteState, error) {
		return s.backend.SaveRule(*candidate, revision)
	}, func(next RewriteState) {
		var saved *RewriteRule
		for index := range next.Rules {
			rule := &next.Rules[index]
			if (candidate.ID != "" && rule.ID == candidate.ID) || (candidate.ID == "" && !prior[rule.ID]) {
				saved = rule
				break
			}
		}
		if saved == nil {
			return
		}
		savedID = saved.ID
		s.deleteDraft(id)
		current := saved
		if s.State.Revision > next.Revision {
			current = s.savedRule(saved.ID)
		}
		edited := !SameRuleContent(*draft.Rule, *candidate)
		nextID := ""
		switch {
		case current != nil:
			nextID = current.ID
			editable := draft.Rule
			if !edited && !SameRuleContent(*draft.Rule, *current) {
				editable = CloneRule(*current)
			}
			syncRuleMetadata(editable, current)
			s.setDraft(current.ID, &RuleDraft{EditorKey: draft.EditorKey, Rule: editable, Baseline: current, Conflict: edited && !SameRuleContent(*current, *saved)})
		case edited:
			nextID = id
			draft.Rule.ID, draft.Rule.Enabled = "", false
			s.setDraft(id, &RuleDraft{EditorKey: draft.EditorKey, Rule: draft.Rule, Conflict: true})
		}
		if s.SelectedID == id {
			s.SelectedID = nextID
			if s.SelectedID == "" {
				if rows := s.Rows(); len(rows) > 0 {
					s.SelectedID = rows[0].Key
				}
			}
			if s.SelectedID != "" {
				s.Select(s.SelectedID)
			}
		}
	}, func(persisted bool) {
		done(RuleSaveResult{Saved: persisted && savedID != "", RuleID: savedID})
	})
}

// SaveAll writes every dirty draft, stopping at the first failure.
func (s *RewriteStore) SaveAll(done func(bool)) {
	if s.Busy() {
		done(false)
		return
	}
	ids := s.DirtyIDs()
	var next func(int)
	next = func(index int) {
		if index == len(ids) {
			done(!s.HasDirtyDrafts())
			return
		}
		s.Save(ids[index], func(result RuleSaveResult) {
			if !result.Saved {
				s.Select(ids[index])
				done(false)
				return
			}
			next(index + 1)
		})
	}
	next(0)
}

// DiscardAll restores every dirty draft.
func (s *RewriteStore) DiscardAll() {
	for _, id := range s.DirtyIDs() {
		if saved := s.savedRule(id); saved != nil {
			s.setDraft(id, &RuleDraft{EditorKey: s.drafts[id].EditorKey, Rule: CloneRule(*saved), Baseline: saved})
		} else {
			s.deleteDraft(id)
		}
	}
	s.reconcileSelection()
	s.Error = ""
}

// Remove deletes a rule and its draft.
func (s *RewriteStore) Remove(id string, done func(bool)) {
	if id == "" {
		id = s.SelectedID
	}
	if s.savedRule(id) == nil {
		s.Revert(id)
		done(true)
		return
	}
	s.mutate(RewriteMutation{Kind: RewriteMutationDelete, ID: id}, func(revision int64) (RewriteState, error) {
		return s.backend.DeleteRule(id, revision)
	}, func(RewriteState) { s.deleteDraft(id) }, done)
}

// SetEnabled turns all rewrites on or off.
func (s *RewriteStore) SetEnabled(enabled bool, done func(bool)) {
	s.mutate(RewriteMutation{Kind: RewriteMutationEnabled, Enabled: enabled}, func(revision int64) (RewriteState, error) {
		return s.backend.SetEnabled(enabled, revision)
	}, nil, done)
}

// SetRuleEnabled turns one rule on or off.
func (s *RewriteStore) SetRuleEnabled(id string, enabled bool, done func(bool)) {
	s.mutate(RewriteMutation{Kind: RewriteMutationRuleEnabled, ID: id, Enabled: enabled}, func(revision int64) (RewriteState, error) {
		return s.backend.SetRuleEnabled(id, enabled, revision)
	}, nil, done)
}

// Reorder arranges the saved rules.
func (s *RewriteStore) Reorder(ids []string, done func(bool)) {
	s.mutate(RewriteMutation{Kind: RewriteMutationReorder}, func(revision int64) (RewriteState, error) {
		return s.backend.ReorderRules(ids, revision)
	}, nil, done)
}

// Cleanup stops following changes.
func (s *RewriteStore) Cleanup() {
	s.generation++
	if s.off != nil {
		s.off()
		s.off = nil
	}
	s.refreshing, s.Loading = false, false
	s.waiters = nil
}
