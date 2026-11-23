package appservice

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/egoist/mygo"
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

// restartRequests counts the restarts a service asked for.
type restartRequests struct {
	mu    sync.Mutex
	count int
}

func (r *restartRequests) request() {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
}

func (r *restartRequests) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func startedAppService(t *testing.T) (*AppService, *restartRequests) {
	t.Helper()
	restarts := &restartRequests{}
	service := New()
	service.SetRestartRequester(restarts.request)
	service.SetUpdateEngine(readyEngine{}, UpdateApplyModeSelf)
	if err := service.ServiceStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.ShutdownUpdater)
	return service, restarts
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
	service, restarts := startedAppService(t)
	if err := service.RestartForUpdate(); err == nil {
		t.Fatal("restart before an update is staged should fail")
	}
	if restarts.len() != 0 {
		t.Fatal("restart requested without a staged update")
	}
}

func TestRestartForUpdatePublishesGuardedRestart(t *testing.T) {
	service, restarts := startedAppService(t)
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
	if got := restarts.len(); got != 1 {
		t.Fatalf("restart requests = %d", got)
	}
}

func TestRestartFailureIsRecorded(t *testing.T) {
	service, restarts := startedAppService(t)
	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseAvailable)
	if err := service.DownloadUpdate(false); err != nil {
		t.Fatal(err)
	}
	waitForServicePhase(t, service, UpdatePhaseReady)
	service.RestartFailed(nil)
	if phase := service.GetUpdateSnapshot().Phase; phase != UpdatePhaseReady {
		t.Fatalf("a nil failure changed the phase to %q", phase)
	}
	service.RestartFailed(errors.New("cannot relaunch"))
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
	if restarts.len() != 1 {
		t.Fatal("retry did not request a guarded restart")
	}
}

func TestReportedProgressReachesSubscribers(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	service := New()
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
	off := service.Events.UpdateState.Subscribe(func(snapshot UpdateSnapshot) {
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
