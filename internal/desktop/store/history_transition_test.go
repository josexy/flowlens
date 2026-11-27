package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func historyTransitionHarness(t *testing.T) (*HistoryStore, *controlledHistory, *fakeClock, func()) {
	t.Helper()
	s, backend, settle := newHistoryHarness(t)
	backend.entries["new"] = []*logic.TrafficEntry{trafficEntry(1), trafficEntry(2), trafficEntry(3)}
	b := &controlledHistory{fakeHistory: backend}
	s.backend, s.Traffic.backend = b, b
	clock := &fakeClock{}
	s.Traffic.clock = clock
	s.Select("new")
	settle()
	s.Traffic.Select(s.Traffic.Entries[0])
	s.Traffic.SetShowDetailPanel(true)
	s.Traffic.RequestBody()
	settle()
	return s, b, clock, settle
}

func TestHistoryTransitionRetainsOneViewUntilOriginalDeadline(t *testing.T) {
	s, _, clock, settle := historyTransitionHarness(t)
	target := s.Traffic
	previous, body := target.DetailEntry, target.BodyView
	released := 0
	target.OnDetailReset = func() { released++ }
	target.Select(target.Entries[1])
	snapshot := (HistoryDetailData{History: s}).RetainedDetail()
	if snapshot == nil || snapshot.SelectedEntry() != previous || snapshot.BodyView() != body || snapshot.BodyLoading() {
		t.Fatal("previous view was not retained intact")
	}
	if target.DetailEntry != nil || target.BodyView != nil || target.Selected.ID != 2 || released != 0 {
		t.Fatal("retained data leaked into the new selection or was released early")
	}
	settle()
	clock.tick(100 * time.Millisecond)
	target.Select(target.Entries[2])
	settle()
	if target.retained != snapshot || target.DetailEntry.ID != 3 {
		t.Fatal("rapid selection replaced the snapshot or published the wrong record")
	}
	clock.tick(HistoryDetailTransitionDelay - 100*time.Millisecond - time.Nanosecond)
	if target.retained == nil || released != 0 {
		t.Fatal("retention ended before its deadline")
	}
	clock.tick(time.Nanosecond)
	if target.retained != nil || target.stopRetention != nil || released != 1 || target.DetailEntry.ID != 3 {
		t.Fatal("deadline did not release the previous view without a panel frame")
	}
}

func TestHistoryTransitionTerminalCleanup(t *testing.T) {
	for _, action := range []string{"close", "deselect", "delete", "reset", "replace", "metadata-failure", "body-failure", "commit"} {
		t.Run(action, func(t *testing.T) {
			s, backend, clock, settle := historyTransitionHarness(t)
			target := s.Traffic
			released := 0
			target.OnDetailReset = func() { released++ }
			if action == "metadata-failure" {
				backend.entry = func(context.Context, string, uint64) (*proxyservice.TrafficEntry, error) {
					return nil, errors.New("unreadable metadata")
				}
			}
			if action == "body-failure" {
				backend.body = func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error) {
					return nil, errors.New("unreadable body")
				}
			}
			target.Select(target.Entries[1])
			switch action {
			case "close":
				target.SetShowDetailPanel(false)
			case "deselect":
				target.Select(nil)
			case "delete":
				target.Delete([]uint64{2})
			case "reset":
				s.CloseOpened()
			case "replace":
				target.Load("another", nil)
			case "body-failure":
				target.RequestBody()
			case "commit":
				settle()
				(HistoryDetailData{History: s}).ReleaseRetainedDetail()
			}
			settle()
			if target.retained != nil || target.stopRetention != nil || released != 1 {
				t.Fatalf("terminal cleanup: retained=%v released=%d", target.retained != nil, released)
			}
			clock.tick(time.Second)
			if released != 1 {
				t.Fatal("obsolete timer reset a finished selection")
			}
			if action == "body-failure" && (target.DetailEntry == nil || target.DetailEntry.ID != 2) {
				t.Fatal("body failure discarded new metadata")
			}
		})
	}
}

func TestHistoryTransitionRejectsCanceledReadsAndOldDeadline(t *testing.T) {
	s, backend, clock, settle := historyTransitionHarness(t)
	started := make(chan context.Context, 1)
	backend.entry = func(ctx context.Context, _ string, id uint64) (*proxyservice.TrafficEntry, error) {
		if id == 2 {
			started <- ctx
			<-ctx.Done()
		}
		return trafficEntry(id), nil
	}
	target := s.Traffic
	target.Select(target.Entries[1])
	old := waitHistoryLoad(t, started)
	deadline := clock.timers[0].fn
	target.Select(target.Entries[2])
	settle()
	if old.Err() != context.Canceled || target.DetailEntry.ID != 3 {
		t.Fatal("canceled metadata replaced the latest selection")
	}
	(HistoryDetailData{History: s}).ReleaseRetainedDetail()
	target.Select(target.Entries[0])
	snapshot := target.retained
	deadline()
	if snapshot == nil || target.retained != snapshot {
		t.Fatal("an obsolete deadline discarded a newer transition")
	}
	target.clearBody()
	settle()
}

func TestHistoryTransitionRetainsPendingBodyAsLoading(t *testing.T) {
	s, backend, _, settle := historyTransitionHarness(t)
	started := make(chan context.Context, 1)
	backend.body = func(ctx context.Context, _ string, id uint64) (*proxyservice.TrafficBodyView, error) {
		if id == 2 {
			started <- ctx
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &proxyservice.TrafficBodyView{ResponseBody: "new"}, nil
	}
	target := s.Traffic
	target.Select(target.Entries[1])
	settle()
	(HistoryDetailData{History: s}).ReleaseRetainedDetail()
	target.RequestBody()
	read := waitHistoryLoad(t, started)
	if target.DetailEntry == nil || target.BodyView != nil || !target.BodyViewLoading {
		t.Fatal("fixture did not leave the shown record's body loading")
	}
	previous := target.DetailEntry
	target.Select(target.Entries[2])
	settle()
	snapshot := (HistoryDetailData{History: s}).RetainedDetail()
	// An unread body is not an empty one: keep the loading presentation.
	if snapshot == nil || snapshot.SelectedEntry() != previous || snapshot.BodyView() != nil || !snapshot.BodyLoading() {
		t.Fatalf("retained a pending body as complete: %+v", snapshot)
	}
	if read.Err() != context.Canceled || target.BodyViewLoading {
		t.Fatal("the retained view kept its body read running")
	}
	target.clearBody()
	settle()
}
