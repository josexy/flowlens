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
	response := map[string]any{"requestId": "stale", "reason": "quit", "decision": "discard"}
	if _, proceed := g.confirm(response); proceed {
		t.Fatal("stale approval")
	}
	response["requestId"] = req.RequestID
	response["decision"] = "cancel"
	if _, proceed := g.confirm(response); proceed {
		t.Fatal("cancel approved")
	}
	_, req = g.request("update")
	if req == nil {
		t.Fatal("cancel discarded dirty state")
	}
	response["requestId"] = req.RequestID
	response["reason"] = "update"
	response["decision"] = "save"
	reason, proceed := g.confirm(response)
	if !proceed || reason != "update" {
		t.Fatal(reason, proceed)
	}
	if blocked, _ := g.request("update"); blocked {
		t.Fatal("successful save still dirty")
	}
	if _, proceed = g.confirm(response); proceed {
		t.Fatal("duplicate approved")
	}
}
