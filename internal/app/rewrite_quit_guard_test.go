package app

import (
	"slices"
	"sync"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/rewrite"
)

// fakeRewriteView holds one quit prompt, as the rewrite view does: a reply
// clears the prompt before the decision is delivered.
// Its lock lets tests poll it while a restart continues on a goroutine.
type fakeRewriteView struct {
	mu     sync.Mutex
	dirty  bool
	prompt *rewrite.QuitPrompt
	asked  []string
}

func (v *fakeRewriteView) Dirty() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dirty
}

func (v *fakeRewriteView) QuitPending() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.prompt != nil
}

func (v *fakeRewriteView) AskQuit(prompt rewrite.QuitPrompt) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.prompt = &prompt
	v.asked = append(v.asked, prompt.Reason)
}

func (v *fakeRewriteView) askedReasons() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.asked)
}

func (v *fakeRewriteView) currentPrompt() *rewrite.QuitPrompt {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.prompt
}

func (v *fakeRewriteView) reply(decision rewrite.QuitDecision) {
	v.mu.Lock()
	prompt := v.prompt
	v.prompt = nil
	if decision != rewrite.DecisionCancel {
		v.dirty = false
	}
	v.mu.Unlock()
	prompt.Respond(decision)
}

func TestRewriteQuitGuardPassesWithoutUnsavedRules(t *testing.T) {
	view := &fakeRewriteView{}
	guard := &rewriteQuitGuard{view: view}
	if guard.guard(quitReasonQuit, func() { t.Fatal("proceeded") }) || len(view.askedReasons()) != 0 {
		t.Fatal("clean rules blocked the quit")
	}
}

func TestRewriteQuitGuardProceedsOnlyAfterSaveOrDiscard(t *testing.T) {
	for _, decision := range []rewrite.QuitDecision{rewrite.DecisionSave, rewrite.DecisionDiscard} {
		view := &fakeRewriteView{dirty: true}
		shown := 0
		guard := &rewriteQuitGuard{view: view, show: func() { shown++ }}
		proceeded := 0
		if !guard.guard(quitReasonUpdate, func() { proceeded++ }) {
			t.Fatal("unsaved rules did not block")
		}
		if shown != 1 || len(view.askedReasons()) != 1 || view.askedReasons()[0] != quitReasonUpdate || proceeded != 0 {
			t.Fatalf("shown %d, asked %v, proceeded %d", shown, view.askedReasons(), proceeded)
		}
		view.reply(decision)
		if proceeded != 1 {
			t.Fatalf("decision %d proceeded %d times", decision, proceeded)
		}
		if guard.guard(quitReasonQuit, func() {}) {
			t.Fatal("resolved rules still block")
		}
	}
}

func TestRewriteQuitGuardIgnoresStaleDecisionsAfterACancel(t *testing.T) {
	view := &fakeRewriteView{dirty: true}
	guard := &rewriteQuitGuard{view: view}
	var quits, updates int
	guard.guard(quitReasonQuit, func() { quits++ })
	cancelled := view.currentPrompt()
	view.reply(rewrite.DecisionCancel)
	if quits != 0 || !view.Dirty() {
		t.Fatal("cancel proceeded or lost the unsaved rules")
	}
	if !guard.guard(quitReasonUpdate, func() { updates++ }) {
		t.Fatal("cancel cleared the guard")
	}
	// A decision for the cancelled prompt authorizes nothing.
	cancelled.Respond(rewrite.DecisionDiscard)
	if quits != 0 || updates != 0 || !view.QuitPending() {
		t.Fatalf("stale decision: quits %d, updates %d", quits, updates)
	}
	view.reply(rewrite.DecisionSave)
	if quits != 0 || updates != 1 {
		t.Fatalf("quits %d, updates %d", quits, updates)
	}
}

func TestRewriteQuitGuardBlocksASecondQuitWhileAsking(t *testing.T) {
	view := &fakeRewriteView{dirty: true}
	guard := &rewriteQuitGuard{view: view}
	var first, second int
	guard.guard(quitReasonQuit, func() { first++ })
	if !guard.guard(quitReasonUpdate, func() { second++ }) || len(view.askedReasons()) != 1 {
		t.Fatalf("a second request replaced the open prompt: asked %v", view.askedReasons())
	}
	view.reply(rewrite.DecisionDiscard)
	if first != 1 || second != 0 {
		t.Fatalf("first %d, second %d", first, second)
	}
}
