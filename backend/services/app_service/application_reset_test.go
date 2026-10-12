package appservice

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResetApplicationRejectsDuplicatesAndBlocksUpdates(t *testing.T) {
	service := New()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	SetResetApplicationHandler(service, func() error {
		calls.Add(1)
		close(entered)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- service.ResetApplication() }()
	<-entered
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			if err := service.ResetApplication(); err == nil || !strings.Contains(err.Error(), "application_reset_in_progress") {
				t.Errorf("duplicate reset: %v", err)
			}
		})
	}
	callers.Wait()
	close(release)
	if err := <-done; err != nil || calls.Load() != 1 {
		t.Fatalf("reset handler: %v %d", err, calls.Load())
	}
	for _, operation := range []func() error{service.CheckForUpdates, service.DownloadUpdate, service.RestartForUpdate} {
		if err := operation(); err == nil || !strings.Contains(err.Error(), "application_reset_in_progress") {
			t.Fatalf("update not blocked by accepted reset: %v", err)
		}
	}
}

func TestResetApplicationFailureAllowsRetry(t *testing.T) {
	service := New()
	if err := service.ResetApplication(); err == nil {
		t.Fatal("uninitialised reset accepted")
	}
	failure := errors.New("could not save reset marker")
	SetResetApplicationHandler(service, func() error { return failure })
	if err := service.ResetApplication(); !errors.Is(err, failure) || service.resetting.Load() {
		t.Fatalf("failed reset retained busy state: %v", err)
	}
	SetResetApplicationHandler(service, func() error { return nil })
	if err := service.ResetApplication(); err != nil {
		t.Fatal(err)
	}
}
