package rewrite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/pkg/database"
	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

func testRule(id string, name ...string) store.RewriteRule {
	label := id
	if len(name) > 0 {
		label = name[0]
	}
	return store.RewriteRule{ID: id, Name: label, Method: "ALL", URLPattern: "https://example.com/*", CreatedAt: 1, UpdatedAt: 1, Action: rewriteservice.Action{
		Version: 1, Type: "request", HostPolicy: "target", Headers: []rewriteservice.FieldOperation{}, Query: []rewriteservice.FieldOperation{}, Body: rewriteservice.BodyAction{Mode: "none"},
	}}
}

type fakeBackend struct {
	mu      sync.Mutex
	server  store.RewriteState
	nextID  int
	calls   []string
	reads   int
	save    func(store.RewriteRule, int64) (store.RewriteState, error)
	reorder func([]string, int64) (store.RewriteState, error)
	enable  func(string, bool, int64) (store.RewriteState, error)
}

func cloneState(state store.RewriteState) store.RewriteState {
	copied := state
	copied.Rules = make([]store.RewriteRule, len(state.Rules))
	for index, rule := range state.Rules {
		copied.Rules[index] = *store.CloneRule(rule)
	}
	return copied
}

func (b *fakeBackend) snapshot() store.RewriteState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneState(b.server)
}

func (b *fakeBackend) setServer(state store.RewriteState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.server = cloneState(state)
}

func (b *fakeBackend) count(kind string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for _, call := range b.calls {
		if strings.HasPrefix(call, kind) {
			count++
		}
	}
	return count
}

func (b *fakeBackend) log() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

func (b *fakeBackend) GetState(context.Context) (store.RewriteState, error) {
	b.mu.Lock()
	b.reads++
	b.mu.Unlock()
	return b.snapshot(), nil
}

func (b *fakeBackend) apply(call string, change func(*store.RewriteState)) (store.RewriteState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, call)
	next := cloneState(b.server)
	change(&next)
	next.Revision++
	b.server = next
	return cloneState(next), nil
}

func (b *fakeBackend) defaultSave(candidate store.RewriteRule, _ int64) (store.RewriteState, error) {
	return b.apply("save", func(next *store.RewriteState) {
		id := candidate.ID
		if id == "" {
			b.nextID++
			id = fmt.Sprintf("new-%d", b.nextID)
		}
		saved := *store.CloneRule(candidate)
		saved.ID = id
		index := slices.IndexFunc(next.Rules, func(rule store.RewriteRule) bool { return rule.ID == id })
		if index < 0 {
			next.Rules = append(next.Rules, saved)
		} else {
			next.Rules[index] = saved
		}
	})
}

func (b *fakeBackend) SaveRule(rule store.RewriteRule, revision int64) (store.RewriteState, error) {
	if b.save != nil {
		return b.save(rule, revision)
	}
	return b.defaultSave(rule, revision)
}

func (b *fakeBackend) DeleteRule(id string, _ int64) (store.RewriteState, error) {
	return b.apply("delete:"+id, func(next *store.RewriteState) {
		next.Rules = slices.DeleteFunc(next.Rules, func(rule store.RewriteRule) bool { return rule.ID == id })
	})
}

func (b *fakeBackend) SetRuleEnabled(id string, enabled bool, revision int64) (store.RewriteState, error) {
	if b.enable != nil {
		return b.enable(id, enabled, revision)
	}
	return b.applyEnable(id, enabled)
}

func (b *fakeBackend) applyEnable(id string, enabled bool) (store.RewriteState, error) {
	return b.apply("rule-enabled", func(next *store.RewriteState) {
		next.Rules[slices.IndexFunc(next.Rules, func(rule store.RewriteRule) bool { return rule.ID == id })].Enabled = enabled
	})
}

func (b *fakeBackend) SetEnabled(enabled bool, _ int64) (store.RewriteState, error) {
	return b.apply("enabled", func(next *store.RewriteState) { next.Enabled = enabled })
}

func (b *fakeBackend) ReorderRules(ids []string, revision int64) (store.RewriteState, error) {
	if b.reorder != nil {
		return b.reorder(ids, revision)
	}
	return b.applyOrder(ids)
}

func (b *fakeBackend) applyOrder(ids []string) (store.RewriteState, error) {
	return b.apply("reorder", func(next *store.RewriteState) {
		byID := map[string]store.RewriteRule{}
		for _, rule := range next.Rules {
			byID[rule.ID] = rule
		}
		next.Rules = next.Rules[:0]
		for _, id := range ids {
			next.Rules = append(next.Rules, byID[id])
		}
	})
}

type previewCall struct {
	action   rewriteservice.BodyAction
	input    string
	resolve  chan previewOutcome
	mu       sync.Mutex
	canceled bool
}

type previewOutcome struct {
	result rewriteservice.BodyPreviewResult
	err    error
}

func (p *previewCall) wasCanceled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.canceled
}

type fixture struct {
	t        *testing.T
	backend  *fakeBackend
	data     *store.Store
	window   *runtimetest.Window
	tester   *ui.Tester
	notifier *widgets.Notifier
	dispatch *shortcuts.Dispatcher
	view     *View
	notify   func(int64)
	mu       sync.Mutex
	previews []*previewCall
	matches  int
	selected int
}

func newFixture(t *testing.T, initial ...store.RewriteState) *fixture {
	t.Helper()
	state := store.RewriteState{Revision: 1, Rules: []store.RewriteRule{testRule("a"), testRule("b")}}
	if len(initial) > 0 {
		state = initial[0]
	}
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	backend := &fakeBackend{server: cloneState(state)}
	f := &fixture{t: t, backend: backend, window: window, notifier: &widgets.Notifier{}}
	f.data = store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: settings}, Rewrite: backend, RewriteChanges: func(notify func(int64)) func() {
		f.notify = notify
		return func() { f.notify = nil }
	}})
	if err := f.data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f.dispatch = shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig { return nil })
	f.view = New(f.data, Host{
		Preview: func(rule store.RewriteRule, method, url string) (rewriteservice.PreviewResult, error) {
			f.mu.Lock()
			f.matches++
			f.mu.Unlock()
			return rewriteservice.PreviewResult{Matched: true, Captures: []string{"cap"}, TargetURL: "https://target.test/" + method}, nil
		},
		PreviewBody: func(ctx context.Context, action rewriteservice.BodyAction, input string) (rewriteservice.BodyPreviewResult, error) {
			call := &previewCall{action: action, input: input, resolve: make(chan previewOutcome, 1)}
			f.mu.Lock()
			f.previews = append(f.previews, call)
			f.mu.Unlock()
			select {
			case outcome := <-call.resolve:
				return outcome.result, outcome.err
			case <-ctx.Done():
				call.mu.Lock()
				call.canceled = true
				call.mu.Unlock()
				return rewriteservice.BodyPreviewResult{}, ctx.Err()
			}
		},
		Select: func() { f.selected++ },
	}, f.dispatch, f.notifier)
	f.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, f.data.Settings.Theme().Config)
		f.dispatch.Begin()
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(320).FillHeight().Shrink(0).Children(func() { f.view.Sidebar(c, theme) })
			ui.Box(c).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() { f.view.Content(c, theme) })
		})
		f.notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 5})
		f.view.Overlay(c, theme)
		f.dispatch.Install(c)
	}, 1400, 1500)
	f.tester.SetLocale("en")
	f.pump()
	f.await("load", func() bool { return f.data.Rewrite.Ready() || f.data.Rewrite.Error != "" })
	return f
}

func (f *fixture) pump() {
	f.window.Drain()
	f.tester.Frame()
}

func (f *fixture) await(what string, done func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.pump()
		if done() {
			f.pump()
			return
		}
		time.Sleep(time.Millisecond)
	}
	f.t.Fatalf("timed out waiting for %s: %v", what, f.tester.Texts())
}

func (f *fixture) settle() {
	for range 4 {
		f.data.Runtime.Jobs.Wait()
		f.pump()
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\n%v", label, err, f.tester.Texts())
	}
	f.pump()
}

func (f *fixture) idle() {
	f.t.Helper()
	f.await("idle", func() bool { return !f.data.Rewrite.Busy() && !f.data.Rewrite.Loading })
}

func (f *fixture) emit(revision int64) {
	f.notify(revision)
}

func (f *fixture) edit(name string) {
	f.data.Rewrite.SelectedRule().Name = name
	f.pump()
}

func (f *fixture) center(label string) (float32, float32) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	return rect.X + rect.W/2, rect.Y + rect.H/2
}

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

func (f *fixture) ids() string {
	ids := []string{}
	for _, rule := range f.data.Rewrite.State.Rules {
		ids = append(ids, rule.ID)
	}
	return strings.Join(ids, ",")
}

func (f *fixture) preview(index int) *previewCall {
	f.t.Helper()
	f.await(fmt.Sprintf("preview call %d", index), func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.previews) > index
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previews[index]
}

func (f *fixture) previewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.previews)
}

func TestSidebarAndPageShareTheInitialRead(t *testing.T) {
	f := newFixture(t)
	f.settle()
	if f.backend.reads != 1 || f.notify == nil {
		t.Fatal(f.backend.reads)
	}
}

func TestCreatedRuleStartsDisabledAndTogglesSaveImmediately(t *testing.T) {
	f := newFixture(t)
	f.click(locales.RewriteRulesNewRule())
	rule := f.data.Rewrite.SelectedRule()
	if rule.Enabled || rule.ID != "" || !f.has(locales.RewriteRulesDraft()) {
		t.Fatalf("%+v", rule)
	}
	f.click(locales.RewriteRulesEnableAll())
	f.idle()
	if f.backend.count("enabled") != 1 || !f.data.Rewrite.State.Enabled {
		t.Fatal(f.backend.log())
	}
	f.click(locales.RewriteRulesEnableRule("a"))
	f.idle()
	if f.backend.count("rule-enabled") != 1 || !f.data.Rewrite.State.Rules[0].Enabled {
		t.Fatal(f.backend.log())
	}
}

func TestSidebarReordersByDragWithoutArrows(t *testing.T) {
	f := newFixture(t)
	for _, label := range []string{locales.RewriteRulesMoveUp() + " a", locales.RewriteRulesMoveDown() + " a"} {
		if _, ok := f.tester.Find(label); ok {
			t.Fatalf("sidebar still shows %q", label)
		}
	}
	dragGrip(f, "a", ruleRowHeight*1.6)
	f.idle()
	if f.ids() != "b,a" || f.backend.count("reorder") != 1 {
		t.Fatal(f.ids(), f.backend.log())
	}
}

func dragGrip(f *fixture, name string, dy float32) {
	f.t.Helper()
	x, y := f.center("drag " + name)
	f.tester.Press(x, y)
	f.tester.Frame()
	const steps = 10
	for step := 1; step <= steps; step++ {
		f.tester.Move(x, y+dy*float32(step)/steps)
		f.tester.Frame()
	}
	f.tester.Release(x, y+dy)
	f.tester.Frame()
}

func TestDragUsesLocalOrderAndExcludesDrafts(t *testing.T) {
	f := newFixture(t)
	f.data.Rewrite.Create("unsaved")
	f.pump()
	release := make(chan struct{})
	var requested []string
	var mu sync.Mutex
	f.backend.reorder = func(ids []string, _ int64) (store.RewriteState, error) {
		mu.Lock()
		requested = slices.Clone(ids)
		mu.Unlock()
		<-release
		return f.backend.applyOrder(ids)
	}
	dragGrip(f, "a", ruleRowHeight*1.6)
	f.await("reorder call", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return requested != nil
	})
	mu.Lock()
	if !slices.Equal(requested, []string{"b", "a"}) {
		t.Fatal(requested)
	}
	mu.Unlock()
	if f.ids() != "a,b" || !f.data.Rewrite.Busy() {
		t.Fatal("waits for persistence", f.ids())
	}
	rows := f.view.rows()
	if rows[0].Key != "b" || rows[1].Key != "a" {
		t.Fatalf("local order: %v", rows)
	}
	close(release)
	f.idle()
	if f.ids() != "b,a" || !f.data.Rewrite.HasDirtyDrafts() {
		t.Fatal(f.ids())
	}
	rows = f.view.rows()
	if rows[0].Key != "b" || rows[2].Saved != nil {
		t.Fatalf("%v", rows)
	}
}

func TestDragRestoresOnFailureAndIgnoresFilteredOrStale(t *testing.T) {
	f := newFixture(t, store.RewriteState{Revision: 1, Rules: []store.RewriteRule{testRule("a", "Alpha"), testRule("b", "Beta")}})
	f.backend.reorder = func([]string, int64) (store.RewriteState, error) {
		return store.RewriteState{}, errors.New("disk full")
	}
	dragGrip(f, "Alpha", ruleRowHeight*1.6)
	f.await("failure", func() bool { return f.data.Rewrite.Error != "" })
	f.idle()
	if rows := f.view.rows(); rows[0].Key != "a" || rows[1].Key != "b" || !strings.Contains(f.data.Rewrite.Error, "disk full") {
		t.Fatal(rows, f.data.Rewrite.Error)
	}
	writes := 0
	f.backend.reorder = func([]string, int64) (store.RewriteState, error) {
		writes++
		return store.RewriteState{}, errors.New("must not write")
	}
	f.view.search = "Alpha"
	f.pump()
	if _, ok := f.tester.Find("drag Alpha"); ok {
		x, y := f.center("drag Alpha")
		f.tester.Press(x, y)
		f.tester.Frame()
		f.tester.Move(x, y+80)
		f.tester.Frame()
		f.tester.Release(x, y+80)
		f.tester.Frame()
	}
	f.settle()
	if len(f.view.rows()) != 1 || writes != 0 {
		t.Fatal(writes)
	}
	f.view.search = ""
	f.pump()
	x, y := f.center("drag Alpha")
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+60)
	f.tester.Frame()
	next := f.backend.snapshot()
	next.Revision, next.Enabled = next.Revision+1, true
	f.backend.setServer(next)
	f.emit(next.Revision)
	f.await("snapshot", func() bool { return f.data.Rewrite.State.Enabled })
	f.tester.Move(x, y+ruleRowHeight*1.6)
	f.tester.Frame()
	f.tester.Release(x, y+ruleRowHeight*1.6)
	f.tester.Frame()
	f.settle()
	if rows := f.view.rows(); writes != 0 || rows[0].Key != "a" {
		t.Fatal(writes, rows)
	}
}

func TestInvalidFieldsShowMessagesAndAreNotSaved(t *testing.T) {
	f := newFixture(t)
	f.data.Rewrite.SelectedRule().Name, f.data.Rewrite.SelectedRule().URLPattern = " ", ""
	f.pump()
	f.click(locales.RewriteRulesSave())
	f.idle()
	if !f.has(locales.RewriteRulesValidationNameRequired()) || !f.has(locales.RewriteRulesValidationUrlRequired()) || f.backend.count("save") != 0 {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestFormEditsRedirectAndOperationsInOrder(t *testing.T) {
	f := newFixture(t)
	rule := f.data.Rewrite.SelectedRule()
	f.click(locales.RewriteRulesAddOperation() + " " + locales.RewriteRulesRequestHeaders())
	f.click(locales.RewriteRulesAddOperation() + " " + locales.RewriteRulesRequestHeaders())
	f.click(locales.RewriteRulesFieldName() + " 1")
	f.tester.Type("First")
	f.pump()
	f.click(locales.RewriteRulesFieldName() + " 2")
	f.tester.Type("Second")
	f.pump()
	if rule.Action.Headers[0].Name != "First" || rule.Action.Headers[1].Name != "Second" {
		t.Fatalf("%+v", rule.Action.Headers)
	}
	f.click(locales.RewriteRulesMoveDown() + " 1")
	if rule.Action.Headers[0].Name != "Second" || rule.Action.Headers[1].Name != "First" {
		t.Fatalf("%+v", rule.Action.Headers)
	}
	f.click(locales.RewriteRulesSave())
	f.idle()
	saved := f.backend.snapshot().Rules[0]
	if saved.Action.Headers[0].Name != "Second" || saved.Action.Headers[1].Name != "First" {
		t.Fatalf("%+v", saved.Action.Headers)
	}
	draft := f.data.Rewrite.SelectedRule()
	changeAction(draft, rewriteservice.ActionRedirect)
	draft.Action.TargetURL = "https://target.example/${1}"
	f.pump()
	if !f.has(locales.RewriteRulesHostPolicy()) || f.has(locales.RewriteRulesRequestHeaders()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	if len(draft.Action.Headers) != 0 {
		t.Fatal("hidden controls keep no operations")
	}
}

func TestMatchTestValidatesBeforeCallingBackend(t *testing.T) {
	f := newFixture(t)
	rule := f.data.Rewrite.SelectedRule()
	rule.URLPattern = ""
	f.pump()
	f.click(locales.RewriteRulesTestMatch())
	if f.matches != 0 || !f.has(locales.RewriteRulesValidationFixFields()) || !f.has(locales.RewriteRulesValidationUrlRequired()) {
		t.Fatalf("%d %v", f.matches, f.tester.Texts())
	}
	rule.URLPattern = "https://example.com/*"
	f.pump()
	if f.has(locales.RewriteRulesValidationFixFields()) {
		t.Fatal("clears with the edit")
	}
	f.click(locales.RewriteRulesTestMatch())
	f.settle()
	if f.matches != 1 || !f.has(locales.RewriteRulesMatched()) || !f.has("https://target.test/GET") || !f.has("1: cap") {
		t.Fatalf("%d %v", f.matches, f.tester.Texts())
	}
	rule.Name = "changed"
	f.pump()
	if f.has(locales.RewriteRulesMatched()) {
		t.Fatal("result resets when the rule changes")
	}
}

func TestSaveShortcutAndRevert(t *testing.T) {
	f := newFixture(t)
	f.edit("shortcut")
	if !f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("save shortcut")
	}
	f.idle()
	if f.backend.snapshot().Rules[0].Name != "shortcut" {
		t.Fatal(f.backend.snapshot().Rules[0].Name)
	}
	f.edit("draft")
	f.click(locales.RewriteRulesRevert())
	if f.data.Rewrite.SelectedRule().Name != "shortcut" || f.data.Rewrite.HasDirtyDrafts() {
		t.Fatal(f.data.Rewrite.SelectedRule().Name)
	}
}

func TestConflictBannerResolves(t *testing.T) {
	f := newFixture(t)
	f.edit("local")
	f.backend.setServer(store.RewriteState{Revision: 2, Rules: []store.RewriteRule{testRule("a", "remote"), testRule("b")}})
	f.emit(2)
	f.await("conflict", func() bool { return f.data.Rewrite.SelectedDraft().Conflict })
	if !f.has(locales.RewriteRulesConflict()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.RewriteRulesKeepDraft())
	if f.data.Rewrite.SelectedDraft().Conflict {
		t.Fatal("kept")
	}
	f.click(locales.RewriteRulesSave())
	f.idle()
	if f.backend.snapshot().Rules[0].Name != "local" {
		t.Fatal(f.backend.snapshot().Rules[0].Name)
	}
}

func TestDeleteDirtyOffersSaveDiscardCancel(t *testing.T) {
	f := newFixture(t)
	f.edit("edited")
	f.click(locales.RewriteRulesDelete())
	if !f.view.remove.open || !f.has(locales.RewriteRulesDeleteDirty("edited")) || !f.has(locales.RewriteRulesDiscardDelete()) || !f.has(locales.RewriteRulesSaveDelete()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.RewriteRulesCancel())
	if f.view.remove.open || f.backend.count("delete") != 0 {
		t.Fatal("cancel")
	}
	f.click(locales.RewriteRulesDelete())
	f.click(locales.RewriteRulesDiscardDelete())
	f.idle()
	if f.backend.count("delete:a") != 1 || f.view.remove.open {
		t.Fatal(f.backend.log())
	}
	f.settle()
	if f.data.Rewrite.SelectedID != "b" {
		t.Fatal(f.data.Rewrite.SelectedID)
	}
}

func TestSaveAndDeleteKeepsInvalidDraft(t *testing.T) {
	f := newFixture(t)
	f.edit("")
	f.view.remove.show(f.view)
	f.view.remove.run(f.view, true)
	f.settle()
	if !f.view.remove.open || !f.has(locales.RewriteRulesValidationNameRequired()) || f.backend.count("save") != 0 || !f.data.Rewrite.HasDirtyDrafts() {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestSaveAndDeleteNeverDeletesFallbackAfterConcurrentRemoval(t *testing.T) {
	f := newFixture(t)
	f.edit("saved by us")
	f.view.remove.show(f.view)
	release := make(chan struct{})
	f.backend.save = func(store.RewriteRule, int64) (store.RewriteState, error) {
		<-release
		return store.RewriteState{Revision: 2, Rules: []store.RewriteRule{testRule("a", "saved by us"), testRule("b")}}, nil
	}
	f.view.remove.run(f.view, true)
	f.backend.setServer(store.RewriteState{Revision: 3, Rules: []store.RewriteRule{testRule("b")}})
	f.emit(3)
	f.await("snapshot", func() bool { return f.data.Rewrite.State.Revision == 3 })
	close(release)
	f.idle()
	f.settle()
	if f.backend.count("delete") != 0 {
		t.Fatal("rule a was already deleted; b must survive", f.backend.log())
	}
}

func TestSaveAndDeleteUsesNewDraftsAssignedID(t *testing.T) {
	f := newFixture(t)
	f.data.Rewrite.Create("new draft")
	f.pump()
	f.view.remove.show(f.view)
	release := make(chan struct{})
	f.backend.save = func(rule store.RewriteRule, revision int64) (store.RewriteState, error) {
		<-release
		return f.backend.defaultSave(rule, revision)
	}
	f.view.remove.run(f.view, true)
	f.data.Rewrite.Select("b")
	close(release)
	f.await("deleted", func() bool { return f.backend.count("delete") == 1 })
	f.idle()
	f.settle()
	if !slices.Contains(f.backend.log(), "delete:new-1") || f.data.Rewrite.SelectedID != "b" || f.ids() != "a,b" || f.view.remove.open {
		t.Fatal(f.backend.log(), f.data.Rewrite.SelectedID, f.ids())
	}
}

func TestSavingNewRuleKeepsEditorIdentity(t *testing.T) {
	f := newFixture(t)
	f.data.Rewrite.Create("new body rule")
	rule := f.data.Rewrite.SelectedRule()
	rule.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "replacement"}
	f.pump()
	key := f.data.Rewrite.SelectedDraft().EditorKey
	form := f.view.forms[key]
	if form == nil {
		t.Fatal("form")
	}
	var result store.RuleSaveResult
	f.data.Rewrite.Save("", func(value store.RuleSaveResult) { result = value })
	f.idle()
	f.settle()
	if !result.Saved || f.data.Rewrite.SelectedDraft().EditorKey != key || f.view.forms[key] != form {
		t.Fatal(result, f.data.Rewrite.SelectedDraft().EditorKey)
	}
	f.data.Rewrite.Select("b")
	f.pump()
	if f.data.Rewrite.SelectedDraft().EditorKey == key {
		t.Fatal("switching rules changes the editor")
	}
	f.data.Rewrite.Select(result.RuleID)
	f.pump()
	if f.view.forms[key] != form {
		t.Fatal("returning keeps the editor")
	}
}

func TestToggleFeedbackStaysOnActiveSwitch(t *testing.T) {
	f := newFixture(t)
	f.pump()
	key := f.data.Rewrite.SelectedDraft().EditorKey
	release := make(chan struct{})
	f.backend.enable = func(id string, enabled bool, _ int64) (store.RewriteState, error) {
		<-release
		return f.backend.applyEnable(id, enabled)
	}
	done := false
	f.data.Rewrite.SetRuleEnabled("b", true, func(value bool) { done = value })
	f.pump()
	if !f.data.Rewrite.Busy() || f.view.forms[key] == nil || f.data.Rewrite.SelectedDraft().EditorKey != key {
		t.Fatal("form stays")
	}
	f.edit("edited during toggle")
	close(release)
	f.await("toggle", func() bool { return done })
	if f.data.Rewrite.SelectedRule().Name != "edited during toggle" || !f.data.Rewrite.IsDirty("a") || f.data.Rewrite.Pending != nil {
		t.Fatal(f.data.Rewrite.SelectedRule().Name)
	}
}

func askQuit(f *fixture, reason string) *[]string {
	replies := &[]string{}
	f.view.AskQuit(QuitPrompt{Reason: reason, Respond: func(decision string) { *replies = append(*replies, decision) }})
	f.pump()
	return replies
}

func TestQuitGuardWaitsForSuccessfulSave(t *testing.T) {
	f := newFixture(t)
	f.edit("draft")
	release := make(chan struct{})
	f.backend.save = func(rule store.RewriteRule, revision int64) (store.RewriteState, error) {
		<-release
		return f.backend.defaultSave(rule, revision)
	}
	replies := askQuit(f, ReasonUpdate)
	if !f.has(locales.RewriteRulesUpdateDirty("1")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.RewriteRulesSaveContinue())
	if len(*replies) != 0 {
		t.Fatal("waits for the save")
	}
	close(release)
	f.await("reply", func() bool { return len(*replies) == 1 })
	if (*replies)[0] != DecisionSave || f.data.Rewrite.HasDirtyDrafts() || f.view.QuitPending() {
		t.Fatal(*replies)
	}
}

func TestQuitGuardFailedSaveCancelDiscard(t *testing.T) {
	f := newFixture(t)
	f.edit("draft")
	f.backend.save = func(store.RewriteRule, int64) (store.RewriteState, error) {
		return store.RewriteState{}, errors.New("disk full")
	}
	replies := askQuit(f, "quit")
	f.click(locales.RewriteRulesSaveContinue())
	f.await("failure", func() bool { return f.data.Rewrite.Error != "" })
	f.idle()
	if len(*replies) != 0 || !f.view.QuitPending() || !f.data.Rewrite.HasDirtyDrafts() || f.selected != 1 {
		t.Fatal(*replies, f.selected)
	}
	f.click(locales.RewriteRulesCancel())
	if (*replies)[0] != DecisionCancel || !f.data.Rewrite.HasDirtyDrafts() {
		t.Fatal(*replies)
	}
	replies = askQuit(f, "quit")
	f.click(locales.RewriteRulesDiscardContinue())
	if (*replies)[0] != DecisionDiscard || f.data.Rewrite.HasDirtyDrafts() {
		t.Fatal(*replies)
	}
}

func TestQuitGuardKeepsInvalidDraftOpen(t *testing.T) {
	f := newFixture(t)
	f.edit("")
	replies := askQuit(f, "quit")
	f.click(locales.RewriteRulesSaveContinue())
	f.settle()
	if len(*replies) != 0 || !f.view.QuitPending() || f.backend.count("save") != 0 || !f.has(locales.RewriteRulesValidationNameRequired()) {
		t.Fatalf("%v %v", *replies, f.tester.Texts())
	}
}

func TestQuitGuardAnswersItselfWhenClean(t *testing.T) {
	f := newFixture(t)
	replies := askQuit(f, "update")
	f.await("reply", func() bool { return len(*replies) == 1 })
	if (*replies)[0] != DecisionSave || f.view.QuitPending() {
		t.Fatal(*replies)
	}
}

func regexFixture(t *testing.T) (*fixture, *store.RewriteRule) {
	f := newFixture(t)
	rule := f.data.Rewrite.SelectedRule()
	rule.Action.Body = rewriteservice.BodyAction{Mode: "regex", Text: "unused replacement body", Pattern: "(foo)", Replacement: "1"}
	f.pump()
	return f, rule
}

func (f *fixture) form() *ruleForm {
	return f.view.forms[f.data.Rewrite.SelectedDraft().EditorKey]
}

func (p *previewCall) respond(count int, output string) {
	p.resolve <- previewOutcome{result: rewriteservice.BodyPreviewResult{MatchCount: count, Output: output}}
}

func (f *fixture) waitIdle() {
	f.t.Helper()
	f.await("preview", func() bool { return !f.form().preview.busy })
}

func TestBodyPreviewKeepsSampleAndCopiesActualOutput(t *testing.T) {
	f, _ := regexFixture(t)
	preview := f.form().preview
	preview.sample = "foo"
	f.pump()
	f.click(locales.RewriteRulesRegexTestCopyInput())
	f.pump()
	if f.tester.Clipboard() != "foo" || preview.previewing {
		t.Fatal(f.tester.Clipboard())
	}
	for _, testCase := range []struct {
		count  int
		output string
	}{{2, "bar\nbaz"}, {1, "foo"}, {0, "foo"}, {1, ""}} {
		f.click(locales.RewriteRulesRegexTestPreview())
		if !preview.busy || preview.canCopy() {
			t.Fatal("pending results cannot be copied")
		}
		call := f.preview(f.previewCount() - 1)
		if call.action.Text != "" || call.action.Pattern != "(foo)" || call.input != "foo" {
			t.Fatalf("%+v", call)
		}
		call.respond(testCase.count, testCase.output)
		f.waitIdle()
		label := locales.RewriteRulesNotMatched()
		if testCase.count > 0 {
			label = locales.RewriteRulesRegexTestMatches(fmt.Sprint(testCase.count))
		}
		if !preview.previewing || !f.has(label) || preview.sample != "foo" || !preview.sampleView.State().ReadOnly {
			t.Fatalf("%v", f.tester.Texts())
		}
		if !preview.showDiff() || preview.diff == nil || preview.diff.Old.String() != "foo" || preview.diff.New.String() != testCase.output {
			t.Fatalf("%+v", preview.diff)
		}
		f.click(locales.RewriteRulesRegexTestCopyOutput())
		f.pump()
		if f.tester.Clipboard() != testCase.output {
			t.Fatalf("%q", f.tester.Clipboard())
		}
		f.click(locales.RewriteRulesRegexTestEdit())
		if preview.sample != "foo" || preview.previewing || preview.showDiff() || preview.sampleView.State().ReadOnly {
			t.Fatal("back to editing")
		}
	}
}

func TestBodyPreviewDebouncesAndCancelsObsolete(t *testing.T) {
	f, rule := regexFixture(t)
	preview := f.form().preview
	preview.sample = "foo"
	f.pump()
	rule.Action.Body.Replacement = "bar"
	f.pump()
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	if f.previewCount() != 0 {
		t.Fatal("editing does not start a preview")
	}
	f.click(locales.RewriteRulesRegexTestPreview())
	f.preview(0).respond(1, "bar")
	f.waitIdle()
	f.pump()
	if preview.result == nil || preview.result.Output != "bar" || preview.scheduled {
		t.Fatal("equivalent body keeps the result")
	}
	rule.Action.Body.Pattern = "(f)"
	f.pump()
	f.tester.Advance(200 * time.Millisecond)
	f.pump()
	rule.Action.Body.Replacement = "new"
	f.pump()
	f.tester.Advance(299 * time.Millisecond)
	f.pump()
	if f.previewCount() != 1 || !preview.busy || preview.canCopy() {
		t.Fatal("debounced")
	}
	f.tester.Advance(time.Millisecond)
	f.pump()
	second := f.preview(1)
	if second.input != "foo" {
		t.Fatal(second.input)
	}
	rule.Action.Body.Pattern = "bar"
	f.pump()
	f.await("cancel", second.wasCanceled)
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	f.preview(2).respond(0, "foo")
	f.waitIdle()
	if preview.result.Output != "foo" {
		t.Fatal(preview.result.Output)
	}
	rule.Action.Body.Replacement = "again"
	f.pump()
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	fourth := f.preview(3)
	preview.editSample()
	f.await("cancel", fourth.wasCanceled)
	f.pump()
	if preview.err != "" || preview.previewing {
		t.Fatal(preview.err)
	}
	f.click(locales.RewriteRulesRegexTestPreview())
	fifth := f.preview(4)
	preview.close_()
	f.await("cancel", fifth.wasCanceled)
	f.pump()
	if preview.result != nil {
		t.Fatal("no result after leaving")
	}
}

func (p *bodyPreview) close_() { p.editSample() }

func TestReturningToEditingCancelsScheduledPreview(t *testing.T) {
	f, rule := regexFixture(t)
	preview := f.form().preview
	f.click(locales.RewriteRulesRegexTestPreview())
	f.preview(0).respond(0, "")
	f.waitIdle()
	rule.Action.Body.Pattern = "changed"
	f.pump()
	preview.editSample()
	f.pump()
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	if f.previewCount() != 1 || preview.scheduled {
		t.Fatal(f.previewCount())
	}
}

func TestBodyPreviewErrorClearsResultAndRecovers(t *testing.T) {
	f, rule := regexFixture(t)
	preview := f.form().preview
	f.click(locales.RewriteRulesRegexTestPreview())
	f.preview(0).respond(1, "empty")
	f.waitIdle()
	rule.Action.Body.Pattern = "("
	f.pump()
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	f.preview(1).resolve <- previewOutcome{err: errors.New("body regular expression: missing closing )")}
	f.waitIdle()
	if !strings.Contains(preview.err, "missing closing") || preview.hasPreview() || preview.result != nil || preview.canCopy() {
		t.Fatalf("%+v", preview)
	}
	if !f.has("body regular expression: missing closing )") {
		t.Fatalf("%v", f.tester.Texts())
	}
	rule.Action.Body.Pattern = "^$"
	f.pump()
	f.tester.Advance(bodyPreviewDelay)
	f.pump()
	third := f.preview(2)
	if third.input != "" {
		t.Fatal("empty samples are valid")
	}
	third.respond(1, "recovered")
	f.waitIdle()
	if preview.err != "" || preview.result.Output != "recovered" {
		t.Fatal(preview.err)
	}
	preview.editSample()
	preview.sample = strings.Repeat("中", 8*1024*1024/3+1)
	f.pump()
	f.click(locales.RewriteRulesRegexTestPreview())
	if f.previewCount() != 3 || preview.err != locales.RewriteRulesRegexTestTooLarge() || preview.busy {
		t.Fatal(preview.err)
	}
}

func TestLargeBodySkipsDiffAndWrapping(t *testing.T) {
	f, _ := regexFixture(t)
	preview := f.form().preview
	cases := [][2]string{
		{"foo", strings.Repeat("x", 128*1024)},
		{strings.Repeat("x", 128*1024), "small"},
		{strings.Repeat(strings.Repeat("x", 1023)+"\n", 512), ""},
	}
	for _, testCase := range cases {
		preview.editSample()
		preview.sample = testCase[0]
		f.pump()
		f.click(locales.RewriteRulesRegexTestPreview())
		f.preview(f.previewCount()-1).respond(1, testCase[1])
		f.waitIdle()
		if !preview.largeResult() || preview.showDiff() || preview.diff != nil || preview.effectiveWrap() {
			t.Fatalf("%d: %v %v %v", len(testCase[0]), preview.largeResult(), preview.showDiff(), preview.effectiveWrap())
		}
		if preview.outputView.Text() != testCase[1] {
			t.Fatal("full result shown")
		}
		f.click(locales.RewriteRulesRegexTestCopyOutput())
		f.pump()
		if f.tester.Clipboard() != testCase[1] {
			t.Fatal("copy")
		}
	}
	preview.editSample()
	preview.sample = " foo "
	f.pump()
	f.click(locales.RewriteRulesRegexTestPreview())
	f.preview(f.previewCount()-1).respond(1, "foo")
	f.waitIdle()
	if !preview.showDiff() || !preview.effectiveWrap() || preview.diff == nil {
		t.Fatal("small text diffs")
	}
	if preview.diff.Layout != 1 {
		t.Fatal("inline diff")
	}
	preview.wrap = false
	f.pump()
	if preview.diff.Wrap {
		t.Fatal("wrap follows the switch")
	}
}

func TestRegexTestOnlyForRegexEdits(t *testing.T) {
	f := newFixture(t)
	rule := f.data.Rewrite.SelectedRule()
	for _, kind := range []string{"request", "response", "redirect"} {
		for _, mode := range []string{"none", "replace", "regex"} {
			rule.Action.Type = kind
			rule.Action.Body.Mode = mode
			f.pump()
			if !f.has(locales.RewriteRulesPreview()) {
				t.Fatal("match test always shows")
			}
			if got, want := f.has(locales.RewriteRulesRegexTest()), kind != "redirect" && mode == "regex"; got != want {
				t.Fatalf("%s %s: %v", kind, mode, got)
			}
		}
	}
}

func TestLoadFailureBlocksEditing(t *testing.T) {
	f := newFixture(t)
	f.view.initialized = false
	f.data.Rewrite = store.NewRewriteStore(f.data.Runtime, failingBackend{f.backend}, nil)
	f.pump()
	f.await("failure", func() bool { return f.data.Rewrite.Error != "" })
	if !f.has("load rewrite rules: database read failed") || f.data.Rewrite.Ready() {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.RewriteRulesEnableAll())
	f.pump()
	if f.backend.count("enabled") != 0 {
		t.Fatal("editing is blocked")
	}
	if err := f.tester.Click(locales.RewriteRulesNewRule()); err == nil {
		f.pump()
		if f.data.Rewrite.HasDirtyDrafts() {
			t.Fatal("no new rules while unloaded")
		}
	}
}

type failingBackend struct{ *fakeBackend }

func (failingBackend) GetState(context.Context) (store.RewriteState, error) {
	return store.RewriteState{}, errors.New("load rewrite rules: database read failed")
}

func TestUnsupportedRuleStaysVisibleAndLocked(t *testing.T) {
	unavailable := testRule("old", "Legacy")
	unavailable.UnavailableReason = "unsupported action version"
	f := newFixture(t, store.RewriteState{Revision: 1, Rules: []store.RewriteRule{unavailable}})
	f.pump()
	if !f.has("Legacy") || !f.has("unsupported action version") || !f.has(locales.RewriteRulesUnavailable()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	before := f.data.Rewrite.SelectedRule().Name
	f.data.Rewrite.Update(testRule("old", "changed"))
	if f.data.Rewrite.SelectedRule().Name != before {
		t.Fatal("locked")
	}
}

// Native reordering leaves the source in place, draws a preview and commits
// only on drop. Passing the end and returning still uses the drop position.
func TestNativeDragCommitsAtTheInsertionLine(t *testing.T) {
	f := newFixture(t, store.RewriteState{Revision: 1, Rules: []store.RewriteRule{testRule("a"), testRule("b"), testRule("c")}})
	x, y := f.center("drag a")
	start, _ := f.tester.Find("a")
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+ruleRowHeight*4)
	f.tester.Frame()
	if keys := rowKeys(f.view.rows()); keys != "a,b,c" || f.backend.count("reorder") != 0 {
		t.Fatalf("drag changed the saved order: %s", keys)
	}
	if row, _ := f.tester.Find("a"); row.Y != start.Y {
		t.Fatalf("native source moved from %v to %v", start.Y, row.Y)
	}
	c, _ := f.tester.Find("c")
	dropY := c.Y + c.H/4
	f.tester.Move(x, dropY)
	f.tester.Frame()
	f.tester.Release(x, dropY)
	f.tester.Frame()
	f.idle()
	if f.ids() != "b,a,c" || f.backend.count("reorder") != 1 {
		t.Fatal(f.ids(), f.backend.log())
	}
}

// A press the system cancels puts the rows back and writes nothing.
func TestCanceledDragRestoresTheRows(t *testing.T) {
	f := newFixture(t)
	writes := 0
	f.backend.reorder = func(ids []string, _ int64) (store.RewriteState, error) {
		writes++
		return f.backend.applyOrder(ids)
	}
	x, y := f.center("drag a")
	f.tester.Press(x, y)
	f.tester.Frame()
	for step := 1; step <= 6; step++ {
		f.tester.Move(x, y+ruleRowHeight*float32(step)/6)
		f.tester.Frame()
	}
	if keys := rowKeys(f.view.rows()); keys != "a,b" {
		t.Fatalf("drag changes source order: %s", keys)
	}
	f.tester.CancelPress()
	f.settle()
	if keys := rowKeys(f.view.rows()); keys != "a,b" || writes != 0 {
		t.Fatalf("rows %s, writes %d", keys, writes)
	}
}

func rowKeys(rows []store.RewriteRow) string {
	keys := make([]string, len(rows))
	for index, row := range rows {
		keys[index] = row.Key
	}
	return strings.Join(keys, ",")
}
