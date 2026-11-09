package appservice

import (
	"context"
	"sync"
	"testing"

	"github.com/josexy/flowlens/backend/host"
	"github.com/josexy/flowlens/backend/host/hosttest"
)

func TestBuildEnvironmentInfoIncludesApplicationVersion(t *testing.T) {
	info := buildEnvironmentInfo(nil)
	want := "v" + APP_VERSION
	if info.AppVersion != want {
		t.Fatalf("AppVersion = %q, want %q", info.AppVersion, want)
	}
}

type readyEngine struct{}

func (readyEngine) Check(context.Context) (*Release, error) {
	return &Release{Version: "9.9.9"}, nil
}

func (readyEngine) DownloadAndInstall(context.Context) error { return nil }

func startedAppService(t *testing.T) (*AppService, *hosttest.RecordingBus) {
	t.Helper()
	bus := hosttest.NewRecordingBus()
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
	if err := service.DownloadUpdate(); err != nil {
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
	if err := service.DownloadUpdate(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseReady)
	bus.Emit(UpdateRestartFailedEventName, "cannot relaunch")
	snapshot := service.GetUpdateSnapshot()
	if snapshot.Phase != UpdatePhaseError || snapshot.Failure == nil || snapshot.Failure.Message != "cannot relaunch" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestReportedProgressReachesSubscribers(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	bus := hosttest.NewRecordingBus()
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
	off := bus.On(UpdateProgressEventName, func(host.Event) { once.Do(received.Done) })
	defer off()

	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseAvailable)
	if err := service.DownloadUpdate(); err != nil {
		t.Fatal(err)
	}
	<-blocked
	service.ReportUpdateProgress(Progress{Written: 5, Total: 10})
	received.Wait()
	close(release)
	waitForServicePhase(t, service, UpdatePhaseReady)
}
