package proxyservice

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeHistorySaveClock records the delays the periodic save waits for and
// fires each one when the test sends on fire.
type fakeHistorySaveClock struct {
	mu     sync.Mutex
	now    time.Time
	delays chan time.Duration
	fire   chan time.Time
}

func (c *fakeHistorySaveClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeHistorySaveClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeHistorySaveClock) After(d time.Duration) <-chan time.Time {
	c.delays <- d
	return c.fire
}

func TestNextHistorySaveDelay(t *testing.T) {
	for _, tt := range []struct{ save, want time.Duration }{
		{0, time.Minute},
		{2 * time.Second, time.Minute},
		{12 * time.Second, time.Minute},
		{30 * time.Second, 150 * time.Second},
	} {
		if got := nextHistorySaveDelay(tt.save); got != tt.want {
			t.Fatalf("nextHistorySaveDelay(%s) = %s, want %s", tt.save, got, tt.want)
		}
	}
}

func TestPeriodicSaveWaitsLongerAfterASlowSave(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeHistorySaveClock{now: time.Unix(1_700_000_000, 0), delays: make(chan time.Duration, 1), fire: make(chan time.Time)}
	durations := []time.Duration{30 * time.Second, time.Second}
	saves := 0
	svc := &ProxyService{
		baseCtx:          ctx,
		historySaveClock: clock,
		periodicHistorySave: func() {
			clock.advance(durations[saves])
			saves++
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.syncHistoriesToDiskPeriodically()
	}()

	for _, want := range []time.Duration{time.Minute, 150 * time.Second, time.Minute} {
		select {
		case got := <-clock.delays:
			if got != want {
				t.Fatalf("periodic save waited %s, want %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("periodic save did not schedule its next run")
		}
		if want == time.Minute && saves == len(durations) {
			break
		}
		clock.fire <- clock.Now()
	}
	cancel()
	<-done
	if saves != len(durations) {
		t.Fatalf("saves = %d, want %d", saves, len(durations))
	}
}
