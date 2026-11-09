package memstatsservice

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestShutdownIsIdempotentAndStopsMonitoring(t *testing.T) {
	var cancelCalls atomic.Int32
	service := New()
	service.baseCtx = context.Background()
	service.baseCancel = func() {
		cancelCalls.Add(1)
	}
	service.StartMonitoring(100)

	for range 2 {
		if err := service.Shutdown(); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("base cancel calls = %d, want 1", got)
	}
	if status := service.GetMonitoringStatus(); status.Monitoring {
		t.Fatal("memory monitoring should be stopped during shutdown")
	}
	service.StartMonitoring(100)
	if status := service.GetMonitoringStatus(); status.Monitoring {
		t.Fatal("memory monitoring must not restart after shutdown")
	}
}
