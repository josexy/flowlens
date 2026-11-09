package app

import (
	"errors"

	"github.com/google/uuid"
)

// resetGuard keeps settings from being saved while a reset is being prepared.
type resetGuard interface {
	RemoveBuiltinCATrust() error
	Commit()
	Cancel()
}

// resetRequest commits Restore Initial State for the next launch.
type resetRequest struct {
	lifecycle     *appLifecycle
	language      func() string
	begin         func() (resetGuard, error)
	cancelUpdates func()
	saveManifest  func(*resetManifest) error
	clearMarker   func() error
	// committed runs once the reset can no longer be undone.
	committed func()
}

// run removes the built-in CA's trust and then writes the reset marker. Any
// failure leaves the application's data and running state as they were.
func (r *resetRequest) run() (resetErr error) {
	lifecycle := r.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.coordinator.InProgress() || lifecycle.coordinator.CanQuit() || !lifecycle.resetting.CompareAndSwap(false, true) {
		return errors.New("application_reset_in_progress")
	}
	defer func() {
		if resetErr != nil {
			lifecycle.resetting.Store(false)
		}
	}()
	language := r.language()
	if language != "en" {
		language = "zh"
	}
	guard, err := r.begin()
	if err != nil {
		return err
	}
	defer guard.Cancel()
	r.cancelUpdates()
	manifest := &resetManifest{Version: 1, Phase: resetPrepared, Token: uuid.NewString(), Language: language}
	if err := r.saveManifest(manifest); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = r.clearMarker()
		}
	}()
	if err := guard.RemoveBuiltinCATrust(); err != nil {
		return err
	}
	manifest.Phase = resetPending
	if err := r.saveManifest(manifest); err != nil {
		return err
	}
	guard.Commit()
	committed = true
	if r.committed != nil {
		r.committed()
	}
	lifecycle.resetReady.Store(true)
	return nil
}
