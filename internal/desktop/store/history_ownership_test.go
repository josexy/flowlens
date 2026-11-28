package store

import (
	"context"
	"slices"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

type ownedHistoryResult struct {
	*fakeHistory
	result []*logic.TrafficEntry
}

func (b *ownedHistoryResult) ListHistoryEntries(context.Context, string) ([]*logic.TrafficEntry, error) {
	result := b.result
	b.result = nil // transfer, never reuse the returned backing array
	return result, nil
}

func TestHistoryLoadOwnsAndCompactsResult(t *testing.T) {
	first, last := trafficEntry(1), trafficEntry(2)
	for _, test := range []struct {
		name        string
		input, want []*logic.TrafficEntry
	}{
		{"nil", nil, nil},
		{"empty", []*logic.TrafficEntry{}, nil},
		{"all-nil", []*logic.TrafficEntry{nil, nil}, nil},
		{"partial", []*logic.TrafficEntry{nil, first, nil, last, nil}, []*logic.TrafficEntry{first, last}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			returned := slices.Clone(test.input)
			s.backend = &ownedHistoryResult{fakeHistory: backend, result: returned}
			s.Select("candidate")
			settle()
			if s.Traffic.Key != "candidate" || !slices.Equal(s.Traffic.Entries, test.want) {
				t.Fatalf("unexpected publication: %q %v", s.Traffic.Key, s.Traffic.Entries)
			}
			if len(test.want) != 0 && &returned[0] != &s.Traffic.Entries[0] {
				t.Fatal("successful read cloned its exclusively owned result")
			}
			for _, entry := range returned[len(test.want):] {
				if entry != nil {
					t.Fatal("compaction retained a removed tail pointer")
				}
			}
		})
	}
}

func TestHistoryBackendFixturesSurviveRepeatedLoads(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	fixture := []*logic.TrafficEntry{nil, trafficEntry(10), nil, trafficEntry(20)}
	backend.entries["candidate"] = slices.Clone(fixture)
	for range 3 {
		s.Select("candidate")
		settle()
		if !slices.Equal(backend.entries["candidate"], fixture) {
			t.Fatal("load modified fixture array")
		}
		if len(s.Traffic.Entries) != 2 {
			t.Fatal("repeated load lost rows")
		}
		s.CloseOpened()
	}
}

func TestHistoryDuplicateIdentityKeepsPreviousWithoutWrites(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	s.Select("new")
	settle()
	previous := s.Traffic.Entries[0]
	backend.entries["duplicate"] = []*logic.TrafficEntry{trafficEntry(42), nil, trafficEntry(42)}
	s.Select("duplicate")
	settle()
	if s.SelectedKey != "new" || s.Traffic.Key != "new" || s.Traffic.Entries[0] != previous || s.LoadingHistory {
		t.Fatal("duplicate candidate replaced the previous history")
	}
	if len(backend.deleted) != 0 || backend.cleared != 0 || len(backend.imports) != 0 {
		t.Fatal("invalid candidate wrote history files")
	}
	backend.entries["numeric"] = []*logic.TrafficEntry{trafficEntry(0), trafficEntry(^uint64(0))}
	s.Select("numeric")
	settle()
	if s.Traffic.Key != "numeric" || len(s.Traffic.Entries) != 2 {
		t.Fatal("identity validation imposed a new numeric ID restriction")
	}
}

func TestHistoryRapidReplacementAndShutdownRejectLateResults(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "A-B-A", true: "shutdown"}[shutdown], func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			s.Select("new")
			settle()
			previous := s.Traffic.Entries[0]
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			s.backend = &controlledHistory{fakeHistory: backend, list: func(ctx context.Context, key string) ([]*logic.TrafficEntry, error) {
				if key == "new" {
					return backend.entries[key], nil
				}
				started <- ctx
				<-release
				return []*logic.TrafficEntry{trafficEntry(99)}, nil
			}}
			s.Select("late")
			ctx := waitHistoryLoad(t, started)
			if shutdown {
				// Jobs.Close waits for workers, so let this worker finish only
				// after shutdown has canceled its context.
				go func() { <-ctx.Done(); close(release) }()
				s.rt.Close()
			} else {
				s.Select("new")
				close(release)
			}
			settle()
			if ctx.Err() != context.Canceled || s.Traffic.Key != "new" || s.Traffic.Entries[0] != previous {
				t.Fatal("obsolete load republished after replacement/shutdown")
			}
		})
	}
}
