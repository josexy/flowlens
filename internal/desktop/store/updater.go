package store

import (
	"context"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/serviceruntime"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

// UpdaterBackend is the update service.
type UpdaterBackend interface {
	GetUpdateSnapshot() appservice.UpdateSnapshot
	CheckForUpdates() error
	DownloadUpdate() error
	CancelUpdate() error
	RestartForUpdate() error
}

// UpdaterStore mirrors the state of the update service for every window.
type UpdaterStore struct {
	Snapshot      appservice.UpdateSnapshot
	ActionPending bool
	Initialized   bool

	rt      Runtime
	backend UpdaterBackend
	offs    []func()
}

// NewUpdaterStore returns an empty store.
func NewUpdaterStore(rt Runtime, backend UpdaterBackend) *UpdaterStore {
	return &UpdaterStore{rt: rt, backend: backend, Snapshot: appservice.UpdateSnapshot{Phase: appservice.UpdatePhaseIdle, ApplyMode: appservice.UpdateApplyModeManual}}
}

// Phase is the current phase.
func (s *UpdaterStore) Phase() appservice.UpdatePhase { return s.Snapshot.Phase }

// Busy reports whether an operation is running.
func (s *UpdaterStore) Busy() bool {
	switch s.Snapshot.Phase {
	case appservice.UpdatePhaseChecking, appservice.UpdatePhaseDownloading, appservice.UpdatePhaseVerifying, appservice.UpdatePhasePreparing:
		return true
	}
	return false
}

// SelfUpdate reports whether the installation can replace itself.
func (s *UpdaterStore) SelfUpdate() bool {
	return s.Snapshot.ApplyMode == appservice.UpdateApplyModeSelf
}

// ApplySnapshot takes a state that is not older than the current one.
func (s *UpdaterStore) ApplySnapshot(next appservice.UpdateSnapshot) {
	if next.Revision < s.Snapshot.Revision {
		return
	}
	s.Snapshot = next
}

// ApplyProgress takes download progress that is not older than the state.
func (s *UpdaterStore) ApplyProgress(event appservice.UpdateProgressEvent) {
	if event.Revision < s.Snapshot.Revision {
		return
	}
	s.Snapshot.Revision = event.Revision
	s.Snapshot.Progress = event.Progress
}

// Initialize subscribes to the service and reads its state.
func (s *UpdaterStore) Initialize(bus serviceruntime.EventBus, post func(func())) {
	if s.Initialized || s.offs != nil {
		return
	}
	s.offs = []func(){
		bus.On(appservice.UpdateStateChangedEventName, func(event serviceruntime.Event) {
			if snapshot, ok := event.Data.(appservice.UpdateSnapshot); ok {
				post(func() { s.ApplySnapshot(snapshot) })
			}
		}),
		bus.On(appservice.UpdateProgressEventName, func(event serviceruntime.Event) {
			if progress, ok := event.Data.(appservice.UpdateProgressEvent); ok {
				post(func() { s.ApplyProgress(progress) })
			}
		}),
	}
	runtime.Run(s.rt.Jobs, "updater:initialize", func(context.Context) (appservice.UpdateSnapshot, error) {
		return s.backend.GetUpdateSnapshot(), nil
	}, func(snapshot appservice.UpdateSnapshot, err error) {
		s.Initialized = true
		if err == nil {
			s.ApplySnapshot(snapshot)
		}
	})
}

// Cleanup unsubscribes, after which Initialize subscribes again.
func (s *UpdaterStore) Cleanup() {
	for _, off := range s.offs {
		off()
	}
	s.offs, s.Initialized = nil, false
}

func (s *UpdaterStore) run(action func() error, done func(error)) {
	if s.ActionPending {
		return
	}
	s.ActionPending = true
	runtime.Run(s.rt.Jobs, "updater:action", func(context.Context) (struct{}, error) { return struct{}{}, action() }, func(_ struct{}, err error) {
		s.ActionPending = false
		if done != nil {
			done(err)
		}
	})
}

// Check looks for a newer version.
func (s *UpdaterStore) Check(done func(error)) { s.run(s.backend.CheckForUpdates, done) }

// Download downloads, verifies, and stages the version found.
func (s *UpdaterStore) Download(done func(error)) { s.run(s.backend.DownloadUpdate, done) }

// Cancel stops the running operation.
func (s *UpdaterStore) Cancel(done func(error)) { s.run(s.backend.CancelUpdate, done) }

// Restart asks to restart into the staged version.
func (s *UpdaterStore) Restart(done func(error)) { s.run(s.backend.RestartForUpdate, done) }
