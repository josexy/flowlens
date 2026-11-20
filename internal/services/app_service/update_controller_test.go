package appservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeUpdateEngine struct {
	check    func(context.Context) (*Release, error)
	download func(context.Context) error
}

func (f *fakeUpdateEngine) Check(ctx context.Context) (*Release, error) {
	return f.check(ctx)
}

func (f *fakeUpdateEngine) DownloadAndInstall(ctx context.Context) error {
	return f.download(ctx)
}

func TestUpdateControllerCheckFindsManualRelease(t *testing.T) {
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{
				Version:      "1.1.0",
				Name:         "FlowLens 1.1.0",
				Notes:        "Changes",
				ArtifactName: "flowlens_v1.1.0_windows_x64_setup.exe",
				ArtifactSize: 42,
				ReleaseURL:   "https://github.com/josexy/flowlens/releases/tag/v1.1.0",
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
	if err := controller.Download(false); !errors.Is(err, errUpdateManualOnly) {
		t.Fatalf("manual Download error = %v, want %v", err, errUpdateManualOnly)
	}
}

func TestUpdateControllerCancelCheckReturnsToIdle(t *testing.T) {
	started := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(ctx context.Context) (*Release, error) {
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

func TestUpdateControllerInstallTracksBytes(t *testing.T) {
	downloadStarted := make(chan struct{})
	downloadDone := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{
				Version:      "1.1.0",
				ArtifactName: "flowlens_windows_amd64.exe",
				ArtifactSize: 100,
			}, nil
		},
		download: func(context.Context) error {
			close(downloadStarted)
			<-downloadDone
			return nil
		},
	}
	var eventsMu sync.Mutex
	var events []UpdateSnapshot
	controller := newUpdateController(engine, "1.0.2", UpdateApplyModeSelf, func(name string, data ...any) bool {
		if name != UpdateStateChangedEventName {
			return false
		}
		eventsMu.Lock()
		events = append(events, data[0].(UpdateSnapshot))
		eventsMu.Unlock()
		return true
	})

	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	waitForUpdatePhase(t, controller, UpdatePhaseAvailable)
	if err := controller.Download(false); err != nil {
		t.Fatalf("Download: %v", err)
	}
	<-downloadStarted
	controller.FrameworkProgress(Progress{Written: 25, Total: 100, Rate: 12.5})
	if progress := controller.Snapshot().Progress; progress == nil || progress.Written != 25 {
		t.Fatalf("progress = %#v", progress)
	}
	controller.FrameworkProgress(Progress{Written: 100, Total: 100})
	if snapshot := controller.Snapshot(); snapshot.Phase != UpdatePhaseInstalling || snapshot.CanCancel {
		t.Fatalf("finishing snapshot = %#v", snapshot)
	}
	if err := controller.Cancel(); !errors.Is(err, errUpdateCannotBeCanceled) {
		t.Fatalf("cancel during finishing: %v", err)
	}
	controller.FrameworkProgress(Progress{Written: 5, Total: 200})
	if !controller.Snapshot().CanCancel {
		t.Fatal("full download fallback must allow cancellation")
	}
	close(downloadDone)
	waitForUpdatePhase(t, controller, UpdatePhaseReady)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	foundProgress := false
	for _, event := range events {
		if event.Progress != nil && event.Progress.Written == 25 && event.Phase == UpdatePhaseInstalling && event.CanCancel {
			foundProgress = true
		}
	}
	if !foundProgress {
		t.Fatal("no complete snapshot with download progress", events)
	}
}

func TestUpdateControllerCancelDownloadKeepsRelease(t *testing.T) {
	downloadStarted := make(chan struct{})
	engine := &fakeUpdateEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{
				Version:      "1.1.0",
				ArtifactName: "flowlens_windows_amd64.exe",
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
	if err := controller.Download(false); err != nil {
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
		check: func(ctx context.Context) (*Release, error) {
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

func TestUpdateControllerAutomaticRestartOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		auto, cancel, shutdown bool
		err                    error
		phase                  UpdatePhase
		restarts               int
	}{
		{"automatic", true, false, false, nil, UpdatePhaseReady, 1},
		{"later", false, false, false, nil, UpdatePhaseReady, 0},
		{"bad signature", true, false, false, errors.New("invalid signature"), UpdatePhaseError, 0},
		{"canceled", true, true, false, context.Canceled, UpdatePhaseAvailable, 0},
		{"install wins cancel race", true, true, false, nil, UpdatePhaseReady, 0},
		{"shutdown", true, false, true, context.Canceled, UpdatePhaseAvailable, 0},
		{"install wins shutdown race", true, false, true, nil, UpdatePhaseReady, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			engine := &fakeUpdateEngine{
				check: func(context.Context) (*Release, error) { return &Release{Version: "2.0.0"}, nil },
				download: func(ctx context.Context) error {
					close(started)
					if tc.cancel || tc.shutdown {
						<-ctx.Done()
					} else {
						<-release
					}
					return tc.err
				},
			}
			var controller *updateController
			restarts := 0
			controller = newUpdateController(engine, "1.0.0", UpdateApplyModeSelf, func(name string, _ ...any) bool {
				if name == UpdateUserRestartEventName {
					// The event must be emitted without the state lock and only
					// once the service can honor the guarded restart request.
					if controller.Snapshot().Phase != UpdatePhaseReady {
						t.Error("restart before ready")
					}
					restarts++
				}
				return true
			})
			if err := controller.Check(); err != nil {
				t.Fatal(err)
			}
			waitForUpdatePhase(t, controller, UpdatePhaseAvailable)
			if err := controller.Download(tc.auto); err != nil {
				t.Fatal(err)
			}
			<-started
			controller.mu.Lock()
			done := controller.operation.done
			controller.mu.Unlock()
			switch {
			case tc.cancel:
				if err := controller.Cancel(); err != nil {
					t.Fatal(err)
				}
			case tc.shutdown:
				controller.Shutdown()
			default:
				close(release)
			}
			<-done
			snapshot := controller.Snapshot()
			if snapshot.Phase != tc.phase || restarts != tc.restarts || snapshot.RestartAfterInstall != tc.auto {
				t.Fatalf("snapshot %+v, restart requests %d", snapshot, restarts)
			}
			// Merely reading state (including reopening a window) cannot
			// repeat an automatic request that the user may have declined.
			controller.Snapshot()
			if restarts != tc.restarts {
				t.Fatal("automatic restart repeated")
			}
			controller.Shutdown()
			if err := controller.Restart(); err == nil {
				t.Fatal("restart accepted during shutdown")
			}
		})
	}
}
