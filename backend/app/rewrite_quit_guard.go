package app

import (
	"strconv"
	"sync"
)

type rewriteQuitRequest struct {
	RequestID string `json:"requestId"`
	Reason    string `json:"reason"`
}

// The guard never times out a user's decision. A request ID prevents a stale
// dialog response from authorizing a later quit/restart operation.
type rewriteQuitGuard struct {
	mu      sync.Mutex
	dirty   bool
	nextID  uint64
	pending *rewriteQuitRequest
}

func (g *rewriteQuitGuard) setDirty(dirty bool) { g.mu.Lock(); defer g.mu.Unlock(); g.dirty = dirty }
func (g *rewriteQuitGuard) request(reason string) (blocked bool, request *rewriteQuitRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending != nil {
		return true, nil
	}
	if !g.dirty {
		return false, nil
	}
	g.nextID++
	value := rewriteQuitRequest{RequestID: strconv.FormatUint(g.nextID, 10), Reason: reason}
	g.pending = &value
	copy := value
	return true, &copy
}
func (g *rewriteQuitGuard) confirm(id, responseReason, decision string) (reason string, proceed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending == nil || g.pending.RequestID != id || g.pending.Reason != responseReason {
		return "", false
	}
	if decision != "save" && decision != "discard" && decision != "cancel" {
		return "", false
	}
	reason = g.pending.Reason
	g.pending = nil
	if decision == "cancel" {
		return reason, false
	}
	g.dirty = false
	return reason, true
}
