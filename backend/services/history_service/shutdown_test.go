package historyservice

import (
	"testing"
	"time"
)

func TestShutdownWaitsForStartupMaintenanceAndIsIdempotent(t *testing.T) {
	service := New(nil, nil)
	service.maintenanceWG.Add(1)
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- service.Shutdown()
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before maintenance completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	service.maintenanceWG.Done()
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not finish after maintenance completed")
	}

	if err := service.Shutdown(); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
