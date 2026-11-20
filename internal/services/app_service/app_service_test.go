package appservice

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/josexy/flowlens/internal/serviceruntime"
	"github.com/josexy/flowlens/internal/serviceruntime/bustest"
)

func TestApplicationVersionUsesMyGoMetadata(t *testing.T) {
	original := mygo.App.Version()
	t.Cleanup(func() { mygo.App.SetVersion(original) })
	for _, tt := range []struct {
		name, version, want string
	}{
		{"release", "2.3.4", "2.3.4"},
		{"prerelease", "2.4.0-beta.1", "2.4.0-beta.1"},
		{"plain Go build", "", "0.0.0-dev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mygo.App.SetVersion(tt.version)
			if got := Version(); got != tt.want {
				t.Fatalf("Version() = %q, want %q", got, tt.want)
			}
			if got := buildEnvironmentInfo(nil).AppVersion; got != "v"+tt.want {
				t.Fatalf("AppVersion = %q, want %q", got, "v"+tt.want)
			}
			if got := New().GetUpdateSnapshot().CurrentVersion; got != tt.want {
				t.Fatalf("unstarted updater version = %q, want %q", got, tt.want)
			}
			service, _ := startedAppService(t)
			if got := service.GetUpdateSnapshot().CurrentVersion; got != tt.want {
				t.Fatalf("started updater version = %q, want %q", got, tt.want)
			}
		})
	}
}

type readyEngine struct{}

func (readyEngine) Check(context.Context) (*Release, error) {
	return &Release{Version: "9.9.9"}, nil
}

func (readyEngine) DownloadAndInstall(context.Context) error { return nil }

func startedAppService(t *testing.T) (*AppService, *bustest.RecordingBus) {
	t.Helper()
	bus := bustest.NewRecordingBus()
	service := New()
	service.SetEventBus(bus)
	service.SetUpdateEngine(readyEngine{}, UpdateApplyModeSelf)
	if err := service.ServiceStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.ShutdownUpdater)
	return service, bus
}

func waitForServicePhase(t *testing.T, service *AppService, phase UpdatePhase) {
	t.Helper()
	waitForUpdatePhase(t, service.updates, phase)
}

func TestUpdatesAreNotReadyWithoutAnEngine(t *testing.T) {
	service := New()
	if err := service.ServiceStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckForUpdates(); err == nil {
		t.Fatal("CheckForUpdates without an engine should fail")
	}
	if phase := service.GetUpdateSnapshot().Phase; phase != UpdatePhaseIdle {
		t.Fatalf("phase = %q", phase)
	}
}

func TestRestartForUpdateRequiresAStagedUpdate(t *testing.T) {
	service, bus := startedAppService(t)
	if err := service.RestartForUpdate(); err == nil {
		t.Fatal("restart before an update is staged should fail")
	}
	if len(bus.Named(UpdateUserRestartEventName)) != 0 {
		t.Fatal("restart event published without a staged update")
	}
}

func TestRestartForUpdatePublishesGuardedRestart(t *testing.T) {
	service, bus := startedAppService(t)
	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseAvailable)
	if err := service.DownloadUpdate(false); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseReady)
	if err := service.RestartForUpdate(); err != nil {
		t.Fatal(err)
	}
	if got := len(bus.Named(UpdateUserRestartEventName)); got != 1 {
		t.Fatalf("restart events = %d", got)
	}
}

func TestRestartFailureEventIsRecorded(t *testing.T) {
	service, bus := startedAppService(t)
	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseAvailable)
	if err := service.DownloadUpdate(false); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseReady)
	bus.Emit(UpdateRestartFailedEventName, "cannot relaunch")
	snapshot := service.GetUpdateSnapshot()
	if snapshot.Phase != UpdatePhaseError || snapshot.Failure == nil || snapshot.Failure.Message != "cannot relaunch" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := service.DownloadUpdate(true); err == nil {
		t.Fatal("a failed restart must not reinstall the already installed update")
	}
	if err := service.CheckForUpdates(); err == nil {
		t.Fatal("a failed restart must retain the installed release")
	}
	if err := service.RestartForUpdate(); err != nil {
		t.Fatalf("retry restart: %v", err)
	}
	if got := service.GetUpdateSnapshot(); got.Phase != UpdatePhaseReady || got.Failure != nil {
		t.Fatalf("retry snapshot: %+v", got)
	}
	if len(bus.Named(UpdateUserRestartEventName)) != 1 {
		t.Fatal("retry did not request a guarded restart")
	}
}

func TestReportedProgressReachesSubscribers(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	bus := bustest.NewRecordingBus()
	service := New()
	service.SetEventBus(bus)
	service.SetUpdateEngine(&fakeUpdateEngine{
		check: func(context.Context) (*Release, error) { return &Release{Version: "9.9.9", ArtifactSize: 10}, nil },
		download: func(context.Context) error {
			close(blocked)
			<-release
			return nil
		},
	}, UpdateApplyModeSelf)
	if err := service.ServiceStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.ShutdownUpdater)

	var received sync.WaitGroup
	received.Add(1)
	var once sync.Once
	off := bus.On(UpdateStateChangedEventName, func(event serviceruntime.Event) {
		snapshot := event.Data.(UpdateSnapshot)
		if snapshot.Progress != nil && snapshot.Progress.Written == 5 {
			once.Do(received.Done)
		}
	})
	defer off()

	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseAvailable)
	if err := service.DownloadUpdate(false); err != nil {
		t.Fatal(err)
	}
	<-blocked
	service.ReportUpdateProgress(Progress{Written: 5, Total: 10})
	received.Wait()
	close(release)
	waitForServicePhase(t, service, UpdatePhaseReady)
}
