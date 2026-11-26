package store

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestHistoryBodyDemandDoesNotBlockMetadataOrDuplicateReads(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	backend.entries["new"] = append(backend.entries["new"], trafficEntry(2))
	started := make(chan context.Context, 2)
	var calls atomic.Int32
	b := &controlledHistory{fakeHistory: backend, body: func(ctx context.Context, _ string, _ uint64) (*proxyservice.TrafficBodyView, error) {
		calls.Add(1)
		started <- ctx
		<-ctx.Done()
		return &proxyservice.TrafficBodyView{ResponseBody: "obsolete body"}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	target := s.Traffic
	target.Select(target.Entries[0])
	target.SetShowDetailPanel(true)
	if (HistoryDetailData{History: s}).PendingEntry() != target.Selected {
		t.Fatal("pending selection identity missing")
	}
	settle()
	if target.DetailEntry == nil || target.BodyView != nil || calls.Load() != 0 {
		t.Fatal("metadata inspection started a body read")
	}
	target.RequestBody()
	first := waitHistoryLoad(t, started)
	for range 10 {
		target.RequestBody()
	}
	if !target.BodyViewLoading || target.DetailEntry == nil || calls.Load() != 1 {
		t.Fatal("body demand hid metadata or duplicated the read")
	}
	target.Select(target.Entries[1])
	settle()
	if first.Err() != context.Canceled || target.BodyView != nil || target.DetailEntry.ID != 2 {
		t.Fatal("selection change retained or published the previous body")
	}
	target.RequestBody()
	second := waitHistoryLoad(t, started)
	target.SetShowDetailPanel(false)
	settle()
	if second.Err() != context.Canceled || target.DetailEntry != nil || target.BodyView != nil || target.BodyViewLoading {
		t.Fatal("closing left a body read or detail alive")
	}
}

func TestHistoryFailedBodyDemandRetriesOnlyAfterReopen(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	var calls atomic.Int32
	b := &controlledHistory{fakeHistory: backend, body: func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error) {
		calls.Add(1)
		return nil, errors.New("body unavailable")
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	target := s.Traffic
	target.Select(target.Entries[0])
	target.SetShowDetailPanel(true)
	// Demand may arrive before metadata; it starts once that stage completes.
	target.RequestBody()
	settle()
	if target.DetailEntry == nil || target.BodyView != nil || target.BodyViewLoading || calls.Load() != 1 {
		t.Fatal("body failure discarded headers or failed to settle")
	}
	for range 5 {
		target.RequestBody()
		settle()
	}
	if calls.Load() != 1 {
		t.Fatal("redraw demand retried a failed body")
	}
	target.SetShowDetailPanel(false)
	target.SetShowDetailPanel(true)
	target.RequestBody()
	settle()
	if calls.Load() != 2 {
		t.Fatal("reopening did not permit a fresh body read")
	}
}

func TestHistoryTCPAndInvalidMetadataNeverRequestBodies(t *testing.T) {
	for _, kind := range []string{"tcp", "missing", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			var calls atomic.Int32
			b := &controlledHistory{fakeHistory: backend,
				entry: func(context.Context, string, uint64) (*proxyservice.TrafficEntry, error) {
					switch kind {
					case "missing":
						return nil, nil
					case "mismatch":
						return trafficEntry(999), nil
					default:
						return &logic.TrafficEntry{ID: 1, Type: "tcp"}, nil
					}
				},
				body: func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error) {
					calls.Add(1)
					return &proxyservice.TrafficBodyView{}, nil
				},
			}
			s.backend, s.Traffic.backend = b, b
			s.Select("new")
			settle()
			if kind == "tcp" {
				s.Traffic.Entries[0].Type = "tcp"
			}
			s.Traffic.Select(s.Traffic.Entries[0])
			s.Traffic.SetShowDetailPanel(true)
			s.Traffic.RequestBody()
			settle()
			if calls.Load() != 0 || s.Traffic.DetailLoading || s.Traffic.BodyViewLoading {
				t.Fatal("unexpected body request or unfinished load")
			}
			if kind != "tcp" && s.Traffic.DetailEntry != nil {
				t.Fatal("invalid metadata reached the panel")
			}
		})
	}
}
