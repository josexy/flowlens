package app

import "github.com/josexy/flowlens/internal/desktop/rewrite"

// rewriteQuitView is the rewrite rules view as its quit guard uses it. Every
// method runs on the main thread.
type rewriteQuitView interface {
	Dirty() bool
	QuitPending() bool
	AskQuit(prompt rewrite.QuitPrompt)
}

// rewriteQuitGuard asks what to do with unsaved rewrite rules before quitting
// or updating. It runs on the main thread, like the view it asks.
type rewriteQuitGuard struct {
	view rewriteQuitView
	// show brings the main window forward before asking.
	show func()
	// asking is the prompt whose decision is awaited. A decision of any other
	// prompt is stale and authorizes nothing.
	asking *rewrite.QuitPrompt
}

func (g *rewriteQuitGuard) guard(reason string, proceed func()) (blocked bool) {
	if g.asking != nil || g.view.QuitPending() {
		return true
	}
	if !g.view.Dirty() {
		return false
	}
	prompt := &rewrite.QuitPrompt{Reason: reason}
	prompt.Respond = func(decision rewrite.QuitDecision) {
		if g.asking != prompt {
			return
		}
		g.asking = nil
		if decision != rewrite.DecisionCancel {
			proceed()
		}
	}
	g.asking = prompt
	if g.show != nil {
		g.show()
	}
	g.view.AskQuit(*prompt)
	return true
}
