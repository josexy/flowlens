package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

type controlledHistory struct {
	*fakeHistory
	list   func(context.Context, string) ([]*logic.TrafficEntry, error)
	detail func(context.Context, string, uint64) (historyservice.HistoryDetail, error)
	entry  func(context.Context, string, uint64) (*proxyservice.TrafficEntry, error)
	body   func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error)
}

func (b *controlledHistory) ListHistoryEntries(ctx context.Context, key string) ([]*logic.TrafficEntry, error) {
	if b.list != nil {
		return b.list(ctx, key)
	}
	return b.fakeHistory.ListHistoryEntries(ctx, key)
}

func (b *controlledHistory) GetHistoryDetail(ctx context.Context, key string, id uint64) (historyservice.HistoryDetail, error) {
	if b.detail == nil {
		return b.fakeHistory.GetHistoryDetail(ctx, key, id)
	}
	return b.detail(ctx, key, id)
}

func (b *controlledHistory) GetHistoryEntry(ctx context.Context, key string, id uint64) (*proxyservice.TrafficEntry, error) {
	if b.entry != nil {
		return b.entry(ctx, key, id)
	}
	detail, err := b.GetHistoryDetail(ctx, key, id)
	return detail.Entry, err
}

func (b *controlledHistory) GetHistoryBody(ctx context.Context, key string, id uint64) (*proxyservice.TrafficBodyView, error) {
	if b.body != nil {
		return b.body(ctx, key, id)
	}
	detail, err := b.GetHistoryDetail(ctx, key, id)
	return detail.Body, err
}

func waitHistoryLoad(t *testing.T, started <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-started:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("history read did not start")
		return nil
	}
}

func TestHistoryDetailsStaySeparateFromSummaryAndFailures(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	full := trafficEntry(1)
	full.Request = &proxyservice.HTTPMessage{HeaderFields: []proxyservice.HTTPHeaderField{{Name: "Cookie", Value: "complete"}}}
	var detailErr error
	b := &controlledHistory{fakeHistory: backend, detail: func(context.Context, string, uint64) (historyservice.HistoryDetail, error) {
		if detailErr != nil {
			return historyservice.HistoryDetail{}, detailErr
		}
		return historyservice.HistoryDetail{Entry: full, Body: &proxyservice.TrafficBodyView{RequestBody: "body"}}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	summary := s.Traffic.Entries[0]
	s.Traffic.Select(summary)
	s.Traffic.SetShowDetailPanel(true)
	settle()
	if (HistoryDetailData{History: s}).SelectedEntry() != full || s.Traffic.Selected != summary || s.Traffic.Entries[0] != summary {
		t.Fatal("detail replaced summary or summary reached panel")
	}
	s.Traffic.SetShowDetailPanel(false)
	if s.Traffic.DetailEntry != nil || s.Traffic.BodyView != nil {
		t.Fatal("closed detail retained full record")
	}
	var actionEntry *logic.TrafficEntry
	s.Traffic.GetEntry(1, func(entry *logic.TrafficEntry, _ *proxyservice.TrafficBodyView) { actionEntry = entry })
	settle()
	if actionEntry != full || s.Traffic.DetailEntry != nil {
		t.Fatal("action did not resolve full entry independently")
	}
	detailErr = errors.New("unreadable history")
	s.Traffic.SetShowDetailPanel(true)
	settle()
	if s.Traffic.DetailEntry != nil || s.Traffic.BodyViewLoading {
		t.Fatal("failed read displayed a summary as complete")
	}
}

func TestHistoryReadCancellationDiscardsLateResults(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	started := make(chan context.Context, 4)
	b := &controlledHistory{fakeHistory: backend, detail: func(ctx context.Context, _ string, id uint64) (historyservice.HistoryDetail, error) {
		started <- ctx
		<-ctx.Done()
		// A backend ignoring cancellation at its final return must not publish.
		return historyservice.HistoryDetail{Entry: trafficEntry(id), Body: &proxyservice.TrafficBodyView{ResponseBody: "late"}}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	s.Traffic.Select(s.Traffic.Entries[0])
	s.Traffic.SetShowDetailPanel(true)
	first := waitHistoryLoad(t, started)
	s.Traffic.SetShowDetailPanel(true)
	s.Traffic.Select(trafficEntry(2))
	second := waitHistoryLoad(t, started)
	if first.Err() != context.Canceled {
		t.Fatal("superseded detail was not canceled")
	}
	s.CloseOpened()
	settle()
	if second.Err() != context.Canceled || s.Traffic.DetailEntry != nil || s.Traffic.BodyView != nil {
		t.Fatal("late detail restored closed history")
	}
	s.Select("new")
	settle()
	actionCalled := false
	s.Traffic.GetEntry(1, func(*logic.TrafficEntry, *proxyservice.TrafficBodyView) { actionCalled = true })
	action := waitHistoryLoad(t, started)
	s.CloseOpened()
	settle()
	if action.Err() != context.Canceled || actionCalled {
		t.Fatal("closed history left an action read running")
	}
	b.list = func(ctx context.Context, _ string) ([]*logic.TrafficEntry, error) {
		started <- ctx
		<-ctx.Done()
		return []*logic.TrafficEntry{trafficEntry(3)}, nil
	}
	s.Select("old")
	listing := waitHistoryLoad(t, started)
	s.CloseOpened()
	settle()
	if listing.Err() != context.Canceled || len(s.Traffic.Entries) != 0 {
		t.Fatal("late list restored history")
	}
}
