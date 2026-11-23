package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
)

func testRule(id string, name ...string) RewriteRule {
	label := id
	if len(name) > 0 {
		label = name[0]
	}
	return RewriteRule{ID: id, Name: label, Method: "ALL", URLPattern: "https://example.com/*", CreatedAt: 1, UpdatedAt: 1, Action: rewriteservice.Action{
		Version: 1, Type: "request", HostPolicy: "target", Headers: []rewriteservice.FieldOperation{}, Query: []rewriteservice.FieldOperation{}, Body: rewriteservice.BodyAction{Mode: "none"},
	}}
}

func cloneState(state RewriteState) RewriteState {
	copied := state
	copied.Rules = make([]RewriteRule, len(state.Rules))
	for index, rule := range state.Rules {
		copied.Rules[index] = *CloneRule(rule)
	}
	return copied
}

type rewriteCall struct {
	kind     string
	id       string
	rule     RewriteRule
	revision int64
}

type fakeRewrite struct {
	t      *testing.T
	mu     sync.Mutex
	server RewriteState
	nextID int
	calls  []rewriteCall

	get        func() (RewriteState, error)
	save       func(RewriteRule, int64) (RewriteState, error)
	setEnabled func(bool, int64) (RewriteState, error)
	setRule    func(string, bool, int64) (RewriteState, error)
	reorder    func([]string, int64) (RewriteState, error)
	remove     func(string, int64) (RewriteState, error)
}

func (f *fakeRewrite) server_() RewriteState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneState(f.server)
}

func (f *fakeRewrite) setServer(next RewriteState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.server = cloneState(next)
}

func (f *fakeRewrite) callCount(kinds ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(kinds) == 0 {
		return len(f.calls)
	}
	count := 0
	for _, call := range f.calls {
		if slices.Contains(kinds, call.kind) {
			count++
		}
	}
	return count
}

func (f *fakeRewrite) write(call rewriteCall, apply func(*RewriteState)) (RewriteState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if call.revision != f.server.Revision {
		f.t.Errorf("write %s used revision %d, server is at %d", call.kind, call.revision, f.server.Revision)
	}
	next := cloneState(f.server)
	apply(&next)
	next.Revision++
	f.server = next
	return cloneState(next), nil
}

func (f *fakeRewrite) GetState(context.Context) (RewriteState, error) {
	if f.get != nil {
		return f.get()
	}
	return f.server_(), nil
}

func (f *fakeRewrite) defaultSave(candidate RewriteRule, revision int64) (RewriteState, error) {
	return f.write(rewriteCall{kind: "save", rule: candidate, revision: revision}, func(next *RewriteState) {
		id := candidate.ID
		if id == "" {
			f.nextID++
			id = fmt.Sprintf("new-%d", f.nextID)
		}
		saved := *CloneRule(candidate)
		saved.ID = id
		if candidate.ID == "" {
			saved.Enabled = false
		}
		index := slices.IndexFunc(next.Rules, func(rule RewriteRule) bool { return rule.ID == id })
		if index < 0 {
			next.Rules = append(next.Rules, saved)
		} else {
			next.Rules[index] = saved
		}
	})
}

func (f *fakeRewrite) SaveRule(rule RewriteRule, revision int64) (RewriteState, error) {
	if f.save != nil {
		return f.save(rule, revision)
	}
	return f.defaultSave(rule, revision)
}

func (f *fakeRewrite) DeleteRule(id string, revision int64) (RewriteState, error) {
	if f.remove != nil {
		return f.remove(id, revision)
	}
	return f.write(rewriteCall{kind: "delete", id: id, revision: revision}, func(next *RewriteState) {
		next.Rules = slices.DeleteFunc(next.Rules, func(rule RewriteRule) bool { return rule.ID == id })
	})
}

func (f *fakeRewrite) SetRuleEnabled(id string, enabled bool, revision int64) (RewriteState, error) {
	if f.setRule != nil {
		return f.setRule(id, enabled, revision)
	}
	return f.write(rewriteCall{kind: "ruleEnabled", id: id, revision: revision}, func(next *RewriteState) {
		next.Rules[slices.IndexFunc(next.Rules, func(rule RewriteRule) bool { return rule.ID == id })].Enabled = enabled
	})
}

func (f *fakeRewrite) SetEnabled(enabled bool, revision int64) (RewriteState, error) {
	if f.setEnabled != nil {
		return f.setEnabled(enabled, revision)
	}
	return f.write(rewriteCall{kind: "enabled", revision: revision}, func(next *RewriteState) { next.Enabled = enabled })
}

func (f *fakeRewrite) ReorderRules(ids []string, revision int64) (RewriteState, error) {
	if f.reorder != nil {
		return f.reorder(ids, revision)
	}
	return f.write(rewriteCall{kind: "reorder", revision: revision}, func(next *RewriteState) {
		byID := map[string]RewriteRule{}
		for _, rule := range next.Rules {
			byID[rule.ID] = rule
		}
		next.Rules = next.Rules[:0]
		for _, id := range ids {
			next.Rules = append(next.Rules, byID[id])
		}
	})
}

type rewriteHarness struct {
	t       *testing.T
	store   *RewriteStore
	backend *fakeRewrite
	window  *runtimetest.Window
	changes *countingTopic[rewriteservice.Changed]
}

func newRewriteHarness(t *testing.T, initial ...RewriteState) *rewriteHarness {
	t.Helper()
	state := RewriteState{Revision: 1, Rules: []RewriteRule{testRule("a"), testRule("b")}}
	if len(initial) > 0 {
		state = initial[0]
	}
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	backend := &fakeRewrite{t: t, server: cloneState(state)}
	h := &rewriteHarness{t: t, backend: backend, window: window}
	h.changes = &countingTopic[rewriteservice.Changed]{}
	h.store = NewRewriteStore(rt, backend, h.changes)
	return h
}

func (h *rewriteHarness) await(what string, done func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		h.window.Drain()
		time.Sleep(time.Millisecond)
	}
	h.window.Drain()
}

func (h *rewriteHarness) idle() {
	h.t.Helper()
	h.await("reads to finish", func() bool { return !h.store.refreshing })
}

func (h *rewriteHarness) initialize() {
	h.t.Helper()
	h.store.Initialize()
	h.idle()
}

func (h *rewriteHarness) emit(revision int64) {
	h.t.Helper()
	h.store.handleChanged(revision)
}

func (h *rewriteHarness) edit(name string) {
	rule := *h.store.SelectedRule()
	rule.Name = name
	h.store.Update(rule)
}

type pendingSave struct {
	h      *rewriteHarness
	result *RuleSaveResult
}

func (h *rewriteHarness) startSave(id string) *pendingSave {
	pending := &pendingSave{h: h}
	h.store.Save(id, func(result RuleSaveResult) { pending.result = &result })
	return pending
}

func (p *pendingSave) wait() RuleSaveResult {
	p.h.t.Helper()
	p.h.await("save", func() bool { return p.result != nil })
	return *p.result
}

func (h *rewriteHarness) save(id string) RuleSaveResult {
	h.t.Helper()
	return h.startSave(id).wait()
}

func (h *rewriteHarness) bool(start func(func(bool))) bool {
	h.t.Helper()
	var result *bool
	start(func(value bool) { result = &value })
	h.await("result", func() bool { return result != nil })
	return *result
}

func (h *rewriteHarness) ruleIDs() string {
	ids := make([]string, len(h.store.State.Rules))
	for index, rule := range h.store.State.Rules {
		ids[index] = rule.ID
	}
	return strings.Join(ids, ",")
}

func TestRewriteDraftsSurviveSelectionAndNewRulesAreDisabled(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("first edit")
	h.store.Select("b")
	h.edit("second edit")
	h.store.Select("a")
	if h.store.SelectedRule().Name != "first edit" {
		t.Fatal(h.store.SelectedRule().Name)
	}
	if !h.save("").Saved {
		t.Fatal("save")
	}
	if h.store.IsDirty("a") || !h.store.IsDirty("b") {
		t.Fatal("only the selected draft is saved")
	}
	if h.backend.calls[0].rule.Name != "first edit" {
		t.Fatal(h.backend.calls[0].rule.Name)
	}
	h.store.Create("new rule")
	rule := h.store.SelectedRule()
	if rule.Enabled || rule.Method != "ALL" || rule.ID != "" {
		t.Fatalf("%+v", rule)
	}
	if !h.save("").Saved || h.store.SelectedID != "new-1" || h.store.SelectedRule().Enabled {
		t.Fatalf("%s %+v", h.store.SelectedID, h.store.SelectedRule())
	}
}

func TestRewriteBlankFieldsStayLocalAndClear(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	rule := *h.store.SelectedRule()
	rule.Name, rule.URLPattern = "  ", ""
	h.store.Update(rule)
	if h.save("").Saved || h.backend.callCount() != 0 || h.store.Error != "" {
		t.Fatal("blank fields stay local")
	}
	errors := h.store.FieldErrors("a")
	if errors.Name != RuleErrorNameRequired || errors.URLPattern != RuleErrorURLRequired {
		t.Fatalf("%+v", errors)
	}
	h.store.Select("b")
	if !h.store.FieldErrors("b").Empty() {
		t.Fatal("other rules have no errors")
	}
	h.store.Select("a")
	h.edit("Valid name")
	if errors := h.store.FieldErrors("a"); errors.Name != "" || errors.URLPattern != RuleErrorURLRequired {
		t.Fatalf("%+v", errors)
	}
	rule = *h.store.SelectedRule()
	rule.URLPattern = "https://example.com/*"
	h.store.Update(rule)
	if !h.store.FieldErrors("a").Empty() || !h.save("").Saved || h.backend.callCount() != 1 {
		t.Fatal("corrected rule saves")
	}
}

func TestRewriteFieldLimitsUseUTF8Bytes(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	rule := *h.store.SelectedRule()
	rule.Name, rule.URLPattern = strings.Repeat("中", 86), strings.Repeat("中", 5462)
	h.store.Update(rule)
	if h.save("").Saved || h.backend.callCount() != 0 {
		t.Fatal("over the limit")
	}
	if errors := h.store.FieldErrors("a"); errors.Name != RuleErrorNameTooLong || errors.URLPattern != RuleErrorURLTooLong {
		t.Fatalf("%+v", errors)
	}
	rule = *h.store.SelectedRule()
	rule.Name, rule.URLPattern = strings.Repeat("中", 85)+"a", strings.Repeat("中", 5461)+"*"
	h.store.Update(rule)
	if !h.save("").Saved || h.backend.callCount() != 1 {
		t.Fatal("exact limits are accepted")
	}
}

func TestRewriteSaveAllSelectsInvalidAndRevertClears(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	rule := *h.store.SelectedRule()
	rule.URLPattern = "   "
	h.store.Update(rule)
	h.store.Select("b")
	if h.bool(h.store.SaveAll) || h.store.SelectedID != "a" || h.backend.callCount() != 0 {
		t.Fatal("stops at the invalid draft")
	}
	if h.store.FieldErrors("a").URLPattern != RuleErrorURLRequired {
		t.Fatal("error shown")
	}
	h.store.Revert("")
	if !h.store.FieldErrors("a").Empty() || h.store.HasDirtyDrafts() {
		t.Fatal("revert clears")
	}
}

func TestRewriteExternalEditsRequireAcknowledgement(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("local")
	h.backend.setServer(RewriteState{Revision: 2, Rules: []RewriteRule{testRule("a", "remote"), testRule("b")}})
	h.emit(2)
	h.idle()
	if h.store.SelectedRule().Name != "local" || !h.store.SelectedDraft().Conflict {
		t.Fatal("draft keeps content and conflicts")
	}
	if h.save("").Saved || h.backend.callCount() != 0 {
		t.Fatal("a conflict blocks saving")
	}
	h.store.AcceptConflict("")
	if !h.save("").Saved || h.backend.server_().Rules[0].Name != "local" {
		t.Fatal("accepted")
	}
}

func TestRewriteStaleEventsIgnoredAndToggleDoesNotConflict(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("local")
	enabled := testRule("a")
	enabled.Enabled = true
	h.backend.setServer(RewriteState{Revision: 2, Rules: []RewriteRule{enabled, testRule("b")}})
	h.emit(2)
	h.idle()
	if !h.store.SelectedRule().Enabled || h.store.SelectedDraft().Conflict {
		t.Fatal("toggle is not a conflict")
	}
	h.backend.get = func() (RewriteState, error) { return RewriteState{}, errors.New("stale event must not fetch") }
	h.emit(1)
	h.emit(2)
	h.idle()
	if h.store.Error != "" {
		t.Fatal(h.store.Error)
	}
}

func TestRewriteFailedTogglesKeepStateAndDraft(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("draft")
	h.backend.setEnabled = func(bool, int64) (RewriteState, error) { return RewriteState{}, errors.New("disk full") }
	if h.bool(func(done func(bool)) { h.store.SetEnabled(true, done) }) || h.store.State.Enabled {
		t.Fatal("enabled")
	}
	h.backend.reorder = func([]string, int64) (RewriteState, error) { return RewriteState{}, errors.New("disk full") }
	if h.bool(func(done func(bool)) { h.store.Reorder([]string{"b", "a"}, done) }) {
		t.Fatal("reorder")
	}
	if h.ruleIDs() != "a,b" || h.store.SelectedRule().Name != "draft" || !strings.Contains(h.store.Error, "disk full") {
		t.Fatalf("%s %s %s", h.ruleIDs(), h.store.SelectedRule().Name, h.store.Error)
	}
}

func TestRewriteMutationsWaitAndAreSerialized(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	release := make(chan struct{})
	h.backend.setEnabled = func(bool, int64) (RewriteState, error) {
		<-release
		next := h.backend.server_()
		next.Revision, next.Enabled = 2, true
		return next, nil
	}
	var pending *bool
	h.store.SetEnabled(true, func(value bool) { pending = &value })
	if h.store.State.Enabled || !h.store.Busy() {
		t.Fatal("waits for persistence")
	}
	if h.bool(func(done func(bool)) { h.store.SetRuleEnabled("a", true, done) }) {
		t.Fatal("concurrent writes are refused")
	}
	close(release)
	h.await("toggle", func() bool { return pending != nil })
	if !*pending || !h.store.State.Enabled || h.store.Busy() {
		t.Fatal("finished")
	}
}

func TestRewriteSaveFailureRefreshesAndKeepsDraft(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("local")
	h.backend.save = func(RewriteRule, int64) (RewriteState, error) { return RewriteState{}, errors.New("revision conflict") }
	h.backend.setServer(RewriteState{Revision: 2, Rules: []RewriteRule{testRule("a", "external"), testRule("b")}})
	if h.save("").Saved {
		t.Fatal("failed")
	}
	if h.store.State.Revision != 2 || h.store.SelectedRule().Name != "local" || !h.store.SelectedDraft().Conflict || !h.store.HasDirtyDrafts() {
		t.Fatalf("%+v", h.store.SelectedDraft())
	}
}

func TestRewriteSaveAllStopsOnFailure(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("first")
	h.store.Select("b")
	h.edit("second")
	h.backend.save = func(candidate RewriteRule, revision int64) (RewriteState, error) {
		if candidate.ID == "b" {
			return RewriteState{}, errors.New("cannot save")
		}
		return h.backend.defaultSave(candidate, revision)
	}
	if h.bool(h.store.SaveAll) || h.store.IsDirty("a") || !h.store.IsDirty("b") || h.store.SelectedID != "b" {
		t.Fatal(h.store.SelectedID)
	}
	h.store.DiscardAll()
	if h.store.HasDirtyDrafts() || h.store.SelectedRule().Name != "b" {
		t.Fatal(h.store.SelectedRule().Name)
	}
}

func TestRewriteExternallyDeletedDraftCanBeRecreatedDisabled(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("retained")
	h.backend.setServer(RewriteState{Revision: 2, Rules: []RewriteRule{testRule("b")}})
	h.emit(2)
	h.idle()
	if !h.store.SelectedDraft().Conflict || len(h.store.Rows()) != 2 {
		t.Fatal("retained")
	}
	h.store.AcceptConflict("")
	if h.store.SelectedRule().ID != "" || !h.save("").Saved || h.store.SelectedRule().Name != "retained" || h.store.SelectedRule().Enabled {
		t.Fatalf("%+v", h.store.SelectedRule())
	}
}

func TestRewriteCleanupIgnoresOutstandingSnapshot(t *testing.T) {
	h := newRewriteHarness(t)
	release := make(chan struct{})
	finished := make(chan struct{})
	h.backend.get = func() (RewriteState, error) {
		defer close(finished)
		<-release
		return RewriteState{Revision: 5, Enabled: true}, nil
	}
	h.store.Initialize()
	if h.changes.count() != 1 {
		t.Fatal(h.changes.count())
	}
	h.store.Cleanup()
	if h.changes.count() != 0 {
		t.Fatal(h.changes.count())
	}
	close(release)
	<-finished
	h.window.Drain()
	time.Sleep(5 * time.Millisecond)
	h.window.Drain()
	if h.store.State.Revision != -1 {
		t.Fatal(h.store.State.Revision)
	}
}

func TestRewriteWritesWaitForInitialLoad(t *testing.T) {
	h := newRewriteHarness(t, RewriteState{Revision: 4, Enabled: true, Rules: []RewriteRule{testRule("saved")}})
	release := make(chan struct{})
	h.backend.get = func() (RewriteState, error) {
		<-release
		return h.backend.server_(), nil
	}
	h.store.Initialize()
	if !h.store.Loading || h.store.State.Revision != -1 || h.store.SelectedRule() != nil {
		t.Fatal("loading")
	}
	if h.bool(func(done func(bool)) { h.store.SetEnabled(false, done) }) || h.backend.callCount() != 0 {
		t.Fatal("writes unavailable")
	}
	close(release)
	h.idle()
	if h.store.Loading || h.store.State.Revision != 4 || !h.store.State.Enabled || h.store.SelectedID != "saved" {
		t.Fatalf("%+v", h.store.State)
	}
}

func TestRewriteInitialFailureStaysVisibleAndUnwritable(t *testing.T) {
	h := newRewriteHarness(t)
	h.backend.get = func() (RewriteState, error) {
		return RewriteState{}, errors.New("load rewrite rules: database read failed")
	}
	h.initialize()
	if h.store.Loading || h.store.State.Revision != -1 || !strings.Contains(h.store.Error, "database read failed") {
		t.Fatalf("%+v %s", h.store.State, h.store.Error)
	}
	if h.bool(func(done func(bool)) { h.store.SetEnabled(true, done) }) || h.backend.callCount() != 0 || h.store.HasDirtyDrafts() {
		t.Fatal("no writable empty state")
	}
}

func TestRewriteEventDuringInitialReadReadsAgain(t *testing.T) {
	h := newRewriteHarness(t)
	release := make(chan struct{})
	h.backend.get = func() (RewriteState, error) {
		<-release
		h.backend.get = nil
		return RewriteState{Revision: 1, Rules: []RewriteRule{testRule("a")}}, nil
	}
	h.store.Initialize()
	next := h.backend.server_()
	next.Revision, next.Enabled = 2, true
	h.backend.setServer(next)
	h.emit(2)
	close(release)
	h.idle()
	if h.store.State.Revision != 2 || !h.store.State.Enabled {
		t.Fatalf("%+v", h.store.State)
	}
}

func TestRewriteSaveResponseCannotReplaceNewerSnapshot(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("saved by us")
	release := make(chan struct{})
	h.backend.save = func(RewriteRule, int64) (RewriteState, error) {
		<-release
		return RewriteState{Revision: 2, Rules: []RewriteRule{testRule("a", "saved by us"), testRule("b")}}, nil
	}
	pending := h.startSave("")
	h.backend.setServer(RewriteState{Revision: 3, Rules: []RewriteRule{testRule("a", "newer remote edit"), testRule("b")}})
	h.emit(3)
	h.idle()
	close(release)
	if !pending.wait().Saved || h.store.SelectedRule().Name != "newer remote edit" || h.store.State.Revision != 3 || h.store.IsDirty("a") {
		t.Fatalf("%s %d", h.store.SelectedRule().Name, h.store.State.Revision)
	}
}

func TestRewriteDeletingDirtyRuleSelectsRemaining(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("discard me")
	release := make(chan struct{})
	h.backend.remove = func(string, int64) (RewriteState, error) {
		<-release
		return h.backend.server_(), nil
	}
	var removed *bool
	h.store.Remove("", func(value bool) { removed = &value })
	h.backend.setServer(RewriteState{Revision: 2, Rules: []RewriteRule{testRule("b")}})
	h.emit(2)
	h.idle()
	if h.store.SelectedID != "a" {
		t.Fatal(h.store.SelectedID)
	}
	close(release)
	h.await("remove", func() bool { return removed != nil })
	if !*removed || h.store.SelectedID != "b" || h.store.HasDirtyDrafts() {
		t.Fatal(h.store.SelectedID)
	}
}

func TestRewriteEveryContentFieldMakesDraftDirtyAndRevertsExactly(t *testing.T) {
	saved := testRule("a")
	saved.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Test", Value: "original"}, {Operation: "add", Name: "X-Test"}}
	saved.Action.Query = []rewriteservice.FieldOperation{{Operation: "set", Name: "q", Value: "original"}}
	saved.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "original", Pattern: "a", Replacement: "b"}
	h := newRewriteHarness(t, RewriteState{Revision: 1, Rules: []RewriteRule{saved}})
	h.initialize()
	changes := []func(*RewriteRule){
		func(r *RewriteRule) { r.Name = "renamed" },
		func(r *RewriteRule) { r.Method = "POST" },
		func(r *RewriteRule) { r.URLPattern = "https://other.example/*" },
		func(r *RewriteRule) { r.Action.Version++ },
		func(r *RewriteRule) { r.Action.Type = "response" },
		func(r *RewriteRule) { r.Action.TargetURL = "https://target.example/" },
		func(r *RewriteRule) { r.Action.HostPolicy = "preserve" },
		func(r *RewriteRule) { r.Action.Headers[0].Operation = "remove" },
		func(r *RewriteRule) { r.Action.Headers[0].Name = "x-test" },
		func(r *RewriteRule) { r.Action.Headers[0].Value = "changed" },
		func(r *RewriteRule) { slices.Reverse(r.Action.Headers) },
		func(r *RewriteRule) { r.Action.Query[0].Value = "changed" },
		func(r *RewriteRule) { r.Action.Query = nil },
		func(r *RewriteRule) { r.Action.Body.Mode = "regex" },
		func(r *RewriteRule) { r.Action.Body.Text = "changed" },
		func(r *RewriteRule) { r.Action.Body.Pattern = "different" },
		func(r *RewriteRule) { r.Action.Body.Replacement = "" },
	}
	for index, change := range changes {
		next := *CloneRule(saved)
		change(&next)
		h.store.Update(next)
		if !h.store.HasDirtyDrafts() {
			t.Fatalf("change %d not dirty", index)
		}
		h.store.Update(saved)
		if h.store.HasDirtyDrafts() {
			t.Fatalf("change %d stayed dirty", index)
		}
	}
	meta := saved
	meta.Enabled, meta.CreatedAt, meta.UpdatedAt, meta.UnavailableReason = true, 2, 3, "reason"
	h.store.Update(meta)
	if h.store.HasDirtyDrafts() {
		t.Fatal("metadata is outside editable content")
	}
}

func TestRewriteCopiesIsolateContainersAndShareBody(t *testing.T) {
	saved := testRule("a")
	saved.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Test", Value: "original"}}
	saved.Action.Body.Text = strings.Repeat("中", 1024*1024)
	h := newRewriteHarness(t, RewriteState{Revision: 1, Rules: []RewriteRule{saved}})
	h.initialize()
	candidate := *CloneRule(saved)
	h.store.Update(candidate)
	candidate.Action.Headers[0].Value = "caller mutation"
	candidate.Action.Body.Text = "caller mutation"
	if h.store.SelectedRule().Action.Headers[0].Value != "original" || h.store.SelectedRule().Action.Body.Text != saved.Action.Body.Text || h.store.HasDirtyDrafts() {
		t.Fatal("copies isolate editable containers")
	}
	h.store.SelectedRule().Action.Headers[0].Value = "local edit"
	if h.store.State.Rules[0].Action.Headers[0].Value != "original" || !h.store.HasDirtyDrafts() {
		t.Fatal("drafts do not touch saved rules")
	}
	if !h.save("").Saved || h.store.HasDirtyDrafts() {
		t.Fatal("saved")
	}
	h.edit("local")
	h.store.Revert("")
	if h.store.HasDirtyDrafts() || h.store.SelectedRule().Action.Body.Text != saved.Action.Body.Text {
		t.Fatal("reverted")
	}
}

func TestRewriteOtherDraftsStayCleanWhileOneIsEdited(t *testing.T) {
	var saved []RewriteRule
	for index := range 10 {
		rule := testRule(fmt.Sprint(index))
		rule.Action.Body.Text = fmt.Sprintf("%d:%s", index, strings.Repeat("x", 64*1024))
		saved = append(saved, rule)
	}
	h := newRewriteHarness(t, RewriteState{Revision: 1, Rules: saved})
	h.initialize()
	for _, rule := range saved {
		h.store.Select(rule.ID)
	}
	h.edit("changed")
	if !h.store.HasDirtyDrafts() {
		t.Fatal("dirty")
	}
	for _, rule := range saved[:9] {
		if h.store.IsDirty(rule.ID) {
			t.Fatal(rule.ID)
		}
	}
	h.edit(saved[9].Name)
	if h.store.HasDirtyDrafts() {
		t.Fatal("exact reversion is clean")
	}
}

func TestRewriteSnapshotRefreshesVisitedDrafts(t *testing.T) {
	var saved []RewriteRule
	for index := range 10 {
		saved = append(saved, testRule(fmt.Sprint(index)))
	}
	h := newRewriteHarness(t, RewriteState{Revision: 1, Rules: saved})
	h.initialize()
	for _, rule := range saved {
		h.store.Select(rule.ID)
	}
	updated := cloneState(RewriteState{Revision: 2, Enabled: true, Rules: saved})
	for index := range updated.Rules {
		updated.Rules[index].Name = "updated"
	}
	h.backend.setServer(updated)
	h.emit(2)
	h.idle()
	if h.store.SelectedRule().Name != "updated" || h.store.State.Revision != 2 || len(h.store.DirtyIDs()) != 0 {
		t.Fatal(h.store.DirtyIDs())
	}
}

func TestRewriteMetadataSnapshotsKeepIdentity(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	first := h.store.SelectedRule()
	h.store.Select("b")
	second := h.store.SelectedRule()
	h.store.Select("a")
	if !h.bool(func(done func(bool)) { h.store.SetEnabled(true, done) }) ||
		!h.bool(func(done func(bool)) { h.store.SetRuleEnabled("a", true, done) }) ||
		!h.bool(func(done func(bool)) { h.store.Reorder([]string{"b", "a"}, done) }) {
		t.Fatal("writes")
	}
	if h.store.SelectedRule() != first || !first.Enabled {
		t.Fatal("metadata changes must not replace the rule on show")
	}
	rows := h.store.Rows()
	if rows[slices.IndexFunc(rows, func(row RewriteRow) bool { return row.Key == "b" })].Rule != second {
		t.Fatal("other rule keeps identity")
	}
	h.edit("saved name")
	edited := h.store.SelectedRule()
	release := make(chan struct{})
	saved := make(chan RewriteState, 1)
	h.backend.save = func(candidate RewriteRule, revision int64) (RewriteState, error) {
		<-release
		next, err := h.backend.defaultSave(candidate, revision)
		saved <- next
		return next, err
	}
	pending := h.startSave("")
	close(release)
	next := <-saved
	h.emit(next.Revision)
	h.await("no conflict flash", func() bool { return !h.store.refreshing })
	if h.store.SelectedDraft().Conflict {
		t.Fatal("our own save event must not conflict")
	}
	if !pending.wait().Saved || h.store.SelectedRule() != edited || h.store.HasDirtyDrafts() {
		t.Fatal("saving keeps the rule on show")
	}
}

func TestRewriteBackgroundReadStaysSilent(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	release := make(chan struct{})
	reads := 0
	h.backend.get = func() (RewriteState, error) {
		reads++
		<-release
		next := h.backend.server_()
		next.Revision, next.Enabled = 2, true
		return next, nil
	}
	h.emit(2)
	if h.store.Loading {
		t.Fatal("background read is silent")
	}
	finished := false
	h.store.Refresh(true, func() { finished = true })
	if !h.store.Loading {
		t.Fatal("manual refresh shows progress")
	}
	close(release)
	h.await("refresh", func() bool { return finished })
	if reads != 1 || h.store.Loading || !h.store.State.Enabled {
		t.Fatal(reads)
	}
}

func TestRewriteEditsDuringSaveSurvive(t *testing.T) {
	for _, scenario := range []string{"existing", "new", "reverted"} {
		t.Run(scenario, func(t *testing.T) {
			h := newRewriteHarness(t)
			h.initialize()
			if scenario == "new" {
				h.store.Create("new rule")
			}
			h.edit("submitted")
			key := h.store.SelectedDraft().EditorKey
			release := make(chan struct{})
			saved := make(chan RewriteState, 1)
			h.backend.save = func(candidate RewriteRule, revision int64) (RewriteState, error) {
				<-release
				next, err := h.backend.defaultSave(candidate, revision)
				saved <- next
				return next, err
			}
			pending := h.startSave("")
			later := "typed after saving started"
			if scenario == "reverted" {
				later = "a"
			}
			h.edit(later)
			rule := h.store.SelectedRule()
			close(release)
			h.emit((<-saved).Revision)
			result := pending.wait()
			if !result.Saved || h.store.SelectedRule() != rule || rule.Name != later || h.store.SelectedDraft().EditorKey != key || h.store.SelectedDraft().Conflict || !h.store.IsDirty(result.RuleID) {
				t.Fatalf("%+v %+v", result, h.store.SelectedDraft())
			}
			server := h.backend.server_()
			name := func() string {
				server = h.backend.server_()
				return server.Rules[slices.IndexFunc(server.Rules, func(item RewriteRule) bool { return item.ID == result.RuleID })].Name
			}
			if name() != "submitted" {
				t.Fatal(name())
			}
			h.backend.save = nil
			if !h.save("").Saved || name() != later || h.store.HasDirtyDrafts() {
				t.Fatal(name())
			}
		})
	}
}

func TestRewriteFailedSaveKeepsLaterEdits(t *testing.T) {
	h := newRewriteHarness(t)
	h.initialize()
	h.edit("submitted")
	release := make(chan struct{})
	h.backend.save = func(RewriteRule, int64) (RewriteState, error) {
		<-release
		return RewriteState{}, errors.New("disk full")
	}
	pending := h.startSave("")
	h.edit("later draft")
	close(release)
	if pending.wait().Saved || h.store.SelectedRule().Name != "later draft" || h.store.Pending != nil || h.store.Busy() || !h.store.HasDirtyDrafts() || !strings.Contains(h.store.Error, "disk full") {
		t.Fatalf("%+v %s", h.store.Pending, h.store.Error)
	}
	h.backend.save = nil
	if !h.save("").Saved {
		t.Fatal("retry")
	}
}

func TestRewriteLaterEditsSurviveNewerExternalChange(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprint(removed), func(t *testing.T) {
			h := newRewriteHarness(t)
			h.initialize()
			h.edit("submitted")
			release := make(chan struct{})
			h.backend.save = func(RewriteRule, int64) (RewriteState, error) {
				<-release
				return RewriteState{Revision: 2, Rules: []RewriteRule{testRule("a", "submitted"), testRule("b")}}, nil
			}
			pending := h.startSave("")
			h.edit("later draft")
			rules := []RewriteRule{testRule("a", "remote"), testRule("b")}
			if removed {
				rules = []RewriteRule{testRule("b")}
			}
			h.backend.setServer(RewriteState{Revision: 3, Rules: rules})
			h.emit(3)
			h.idle()
			close(release)
			if !pending.wait().Saved || h.store.State.Revision != 3 || h.store.SelectedRule().Name != "later draft" || !h.store.HasDirtyDrafts() || !h.store.SelectedDraft().Conflict {
				t.Fatalf("%+v", h.store.SelectedDraft())
			}
			if h.save("").Saved {
				t.Fatal("conflict blocks")
			}
			h.backend.save = nil
			h.store.AcceptConflict("")
			if !h.save("").Saved || h.store.HasDirtyDrafts() {
				t.Fatal("resolved")
			}
		})
	}
}

func TestRewriteStaysLightweightUntilInitialized(t *testing.T) {
	h := newRewriteHarness(t)
	reads := 0
	h.backend.get = func() (RewriteState, error) {
		reads++
		return h.backend.server_(), nil
	}
	if h.store.HasDirtyDrafts() || h.changes.count() != 0 || h.store.State.Revision != -1 || reads != 0 {
		t.Fatal("a clean quit reads nothing")
	}
	h.store.Initialize()
	h.store.Initialize()
	h.idle()
	if reads != 1 || h.changes.count() != 1 || h.store.SelectedID != "a" {
		t.Fatal(reads, h.changes.count())
	}
}
