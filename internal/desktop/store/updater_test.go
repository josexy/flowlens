package store

import (
	"errors"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

type fakeUpdater struct {
	initial                              appservice.UpdateSnapshot
	checks, downloads, cancels, restarts int
	restartAfterInstall                  bool
	fail                                 error
}

func (f *fakeUpdater) GetUpdateSnapshot() appservice.UpdateSnapshot { return f.initial }
func (f *fakeUpdater) CheckForUpdates() error                       { f.checks++; return f.fail }
func (f *fakeUpdater) DownloadUpdate(restart bool) error {
	f.downloads++
	f.restartAfterInstall = restart
	return f.fail
}
func (f *fakeUpdater) CancelUpdate() error     { f.cancels++; return f.fail }
func (f *fakeUpdater) RestartForUpdate() error { f.restarts++; return f.fail }

type updaterFixture struct {
	store   *UpdaterStore
	backend *fakeUpdater
	updates *countingTopic[appservice.UpdateSnapshot]
	rt      Runtime
	window  *runtimetest.Window
}

func updaterSnapshot(revision uint64, phase appservice.UpdatePhase) appservice.UpdateSnapshot {
	return appservice.UpdateSnapshot{Revision: revision, Phase: phase, CurrentVersion: "1.0.2", ApplyMode: appservice.UpdateApplyModeSelf}
}

func newUpdaterFixture(t *testing.T, initial appservice.UpdateSnapshot) *updaterFixture {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	backend := &fakeUpdater{initial: initial}
	return &updaterFixture{store: NewUpdaterStore(rt, backend), backend: backend, updates: &countingTopic[appservice.UpdateSnapshot]{}, rt: rt, window: window}
}

func (f *updaterFixture) settle() {
	f.rt.Jobs.Wait()
	f.window.Drain()
}

func (f *updaterFixture) initialize() {
	f.store.Initialize(f.updates, f.rt.Dispatcher.Post)
	f.settle()
}

func TestUpdaterKeepsTheNewestRevision(t *testing.T) {
	initial := updaterSnapshot(2, appservice.UpdatePhaseAvailable)
	initial.Release = &appservice.UpdateRelease{Version: "1.1.0", ArtifactName: "update.exe", ArtifactSize: 100}
	f := newUpdaterFixture(t, initial)
	f.initialize()
	if f.store.Phase() != appservice.UpdatePhaseAvailable || f.updates.count() != 1 {
		t.Fatal(f.store.Phase(), f.updates.count())
	}
	f.updates.Publish(updaterSnapshot(1, appservice.UpdatePhaseIdle))
	f.settle()
	if f.store.Phase() != appservice.UpdatePhaseAvailable {
		t.Fatal("older snapshots must not roll state back")
	}
	progress := updaterSnapshot(3, appservice.UpdatePhaseInstalling)
	progress.CanCancel = true
	progress.Progress = &appservice.UpdateProgress{Written: 25, Total: 100, Rate: 50}
	f.updates.Publish(progress)
	f.settle()
	if f.store.Snapshot.Revision != 3 || f.store.Snapshot.Progress.Written != 25 || !f.store.Snapshot.CanCancel || f.store.Phase() != appservice.UpdatePhaseInstalling {
		t.Fatalf("%+v", f.store.Snapshot)
	}
	f.updates.Publish(updaterSnapshot(4, appservice.UpdatePhaseInstalling))
	f.settle()
	if f.store.Phase() != appservice.UpdatePhaseInstalling {
		t.Fatal(f.store.Phase())
	}
	f.updates.Publish(progress)
	f.settle()
	if f.store.Snapshot.Progress != nil {
		t.Fatal("stale progress must not overwrite a newer phase")
	}
	f.store.Cleanup()
	if f.updates.count() != 0 {
		t.Fatal(f.updates.count())
	}
}

func TestUpdaterActionsCallTheService(t *testing.T) {
	f := newUpdaterFixture(t, updaterSnapshot(1, appservice.UpdatePhaseIdle))
	f.initialize()
	for _, action := range []func(func(error)){f.store.Check, f.store.Download, f.store.Cancel, f.store.Restart} {
		action(nil)
		if !f.store.ActionPending {
			t.Fatal("pending while running")
		}
		f.settle()
	}
	if b := f.backend; b.checks != 1 || b.downloads != 1 || b.cancels != 1 || b.restarts != 1 || f.store.ActionPending {
		t.Fatalf("%+v", b)
	}
}

func TestUpdaterRunsOneActionAtATime(t *testing.T) {
	f := newUpdaterFixture(t, updaterSnapshot(1, appservice.UpdatePhaseIdle))
	f.store.Check(nil)
	f.store.Check(nil)
	f.settle()
	if f.backend.checks != 1 {
		t.Fatal(f.backend.checks)
	}
}

func TestUpdaterCapturesRestartChoiceBeforeStartingJob(t *testing.T) {
	f := newUpdaterFixture(t, updaterSnapshot(1, appservice.UpdatePhaseAvailable))
	if !f.store.RestartAfterInstall {
		t.Fatal("automatic restart defaults on")
	}
	f.store.Download(nil)
	f.store.RestartAfterInstall = false
	f.settle()
	if !f.backend.restartAfterInstall {
		t.Fatal("job read a later UI choice")
	}
	f.store.Download(nil)
	f.settle()
	if f.backend.restartAfterInstall {
		t.Fatal("disabled automatic restart was ignored")
	}
}

func TestUpdaterReportsActionFailure(t *testing.T) {
	f := newUpdaterFixture(t, updaterSnapshot(1, appservice.UpdatePhaseIdle))
	f.backend.fail = errors.New("no")
	var got error
	f.store.Check(func(err error) { got = err })
	f.settle()
	if got == nil || f.store.ActionPending {
		t.Fatal(got)
	}
}

func TestUpdaterInitializeIsIdempotent(t *testing.T) {
	f := newUpdaterFixture(t, updaterSnapshot(1, appservice.UpdatePhaseIdle))
	f.store.Initialize(f.updates, f.rt.Dispatcher.Post)
	f.store.Initialize(f.updates, f.rt.Dispatcher.Post)
	f.settle()
	if f.updates.count() != 1 {
		t.Fatal(f.updates.count())
	}
	f.store.Cleanup()
	f.initialize()
	if f.updates.count() != 1 {
		t.Fatal(f.updates.count())
	}
	f.store.Cleanup()
}
