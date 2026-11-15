package app

import "testing"

func TestRewriteQuitGuardRequiresMatchingDecision(t *testing.T) {
	g := &rewriteQuitGuard{}
	if blocked, _ := g.request("quit"); blocked {
		t.Fatal("clean blocked")
	}
	g.setDirty(true)
	blocked, req := g.request("quit")
	if !blocked || req == nil {
		t.Fatal("dirty not guarded")
	}
	if blocked, again := g.request("update"); !blocked || again != nil {
		t.Fatal("parallel request replaced dialog")
	}
	if _, proceed := g.confirm("stale", "quit", "discard"); proceed {
		t.Fatal("stale approval")
	}
	if _, proceed := g.confirm(req.RequestID, "quit", "cancel"); proceed {
		t.Fatal("cancel approved")
	}
	_, req = g.request("update")
	if req == nil {
		t.Fatal("cancel discarded dirty state")
	}
	reason, proceed := g.confirm(req.RequestID, "update", "save")
	if !proceed || reason != "update" {
		t.Fatal(reason, proceed)
	}
	if blocked, _ := g.request("update"); blocked {
		t.Fatal("successful save still dirty")
	}
	if _, proceed = g.confirm(req.RequestID, "update", "save"); proceed {
		t.Fatal("duplicate approved")
	}
}
