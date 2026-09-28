package appservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

type fakeUpdateEngine struct {
	check    func(context.Context) (*updater.Release, error)
	download func(context.Context) error
}

func (f *fakeUpdateEngine) Check(ctx context.Context) (*updater.Release, error) {
	return f.check(ctx)
}

func (f *fakeUpdateEngine) DownloadAndInstall(ctx context.Context) error {
	return f.download(ctx)
}

func TestUpdateControllerCheckFindsManualRelease(t *testing.T) {
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*updater.Release, error) {
			return &updater.Release{
				Version: "1.1.0",
				Name:    "FlowLens 1.1.0",
				Notes:   "Changes",
				Artifact: updater.Artifact{
					Filename: "flowlens_v1.1.0_windows_x64_setup.exe",
					Size:     42,
				},
				Metadata: map[string]any{
					"github.release.htmlURL": "https://github.com/josexy/flowlens/releases/tag/v1.1.0",
				},
			}, nil
		},
		download: func(context.Context) error { return nil },
	}
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeManual, nil)

	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	snapshot := waitForUpdatePhase(t, controller, UpdatePhaseAvailable)
	if snapshot.ApplyMode != UpdateApplyModeManual || snapshot.Release == nil {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.Release.ReleaseURL != "https://github.com/josexy/flowlens/releases/tag/v1.1.0" {
		t.Fatalf("ReleaseURL = %q", snapshot.Release.ReleaseURL)
	}
	if err := controller.Download(); !errors.Is(err, errUpdateManualOnly) {
		t.Fatalf("manual Download error = %v, want %v", err, errUpdateManualOnly)
	}
}

func TestUpdateControllerCancelCheckReturnsToIdle(t *testing.T) {
	started := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(ctx context.Context) (*updater.Release, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		download: func(context.Context) error { return nil },
	}
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeSelf, nil)

	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	<-started
	if err := controller.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	snapshot := controller.Snapshot()
	if snapshot.Phase != UpdatePhaseIdle || snapshot.CanCancel || snapshot.Failure != nil {
		t.Fatalf("cancelled snapshot = %#v", snapshot)
	}
}

func TestUpdateControllerDownloadTracksFrameworkPhases(t *testing.T) {
	downloadStarted := make(chan struct{})
	downloadDone := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*updater.Release, error) {
			return &updater.Release{
				Version:  "1.1.0",
				Artifact: updater.Artifact{Filename: "flowlens_windows_amd64.exe", Size: 100},
			}, nil
		},
		download: func(context.Context) error {
			close(downloadStarted)
			<-downloadDone
			return nil
		},
	}
	var eventsMu sync.Mutex
	var events []string
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeSelf, func(name string, _ ...any) bool {
		eventsMu.Lock()
		events = append(events, name)
		eventsMu.Unlock()
		return true
	})

	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	waitForUpdatePhase(t, controller, UpdatePhaseAvailable)
	if err := controller.Download(); err != nil {
		t.Fatalf("Download: %v", err)
	}
	<-downloadStarted
	controller.FrameworkProgress(updater.Progress{Written: 25, Total: 100, Rate: 12.5})
	if progress := controller.Snapshot().Progress; progress == nil || progress.Written != 25 {
		t.Fatalf("progress = %#v", progress)
	}
	controller.FrameworkPhase(UpdatePhaseVerifying)
	if snapshot := controller.Snapshot(); snapshot.Phase != UpdatePhaseVerifying || snapshot.CanCancel {
		t.Fatalf("verifying snapshot = %#v", snapshot)
	}
	controller.FrameworkPhase(UpdatePhasePreparing)
	close(downloadDone)
	waitForUpdatePhase(t, controller, UpdatePhaseReady)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	foundProgress := false
	for _, event := range events {
		if event == UpdateProgressEventName {
			foundProgress = true
			break
		}
	}
	if !foundProgress {
		t.Fatalf("events did not include %q: %v", UpdateProgressEventName, events)
	}
}

func TestUpdateControllerCancelDownloadKeepsRelease(t *testing.T) {
	downloadStarted := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*updater.Release, error) {
			return &updater.Release{
				Version:  "1.1.0",
				Artifact: updater.Artifact{Filename: "flowlens_windows_amd64.exe"},
			}, nil
		},
		download: func(ctx context.Context) error {
			close(downloadStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeSelf, nil)

	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	waitForUpdatePhase(t, controller, UpdatePhaseAvailable)
	if err := controller.Download(); err != nil {
		t.Fatalf("Download: %v", err)
	}
	<-downloadStarted
	if err := controller.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	snapshot := controller.Snapshot()
	if snapshot.Phase != UpdatePhaseAvailable || snapshot.Release == nil || snapshot.Failure != nil {
		t.Fatalf("cancelled download snapshot = %#v", snapshot)
	}
}

func TestUpdateControllerRejectsOverlappingOperations(t *testing.T) {
	started := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(ctx context.Context) (*updater.Release, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		download: func(context.Context) error { return nil },
	}
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeSelf, nil)

	if err := controller.Check(); err != nil {
		t.Fatalf("first Check: %v", err)
	}
	<-started
	if err := controller.Check(); !errors.Is(err, errUpdateBusy) {
		t.Fatalf("second Check error = %v, want %v", err, errUpdateBusy)
	}
	if err := controller.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func waitForUpdatePhase(t *testing.T, controller *updateController, phase UpdatePhase) UpdateSnapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := controller.Snapshot()
		if snapshot.Phase == phase {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for update phase %q; last snapshot: %#v", phase, controller.Snapshot())
	return UpdateSnapshot{}
}
