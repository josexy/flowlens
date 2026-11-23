package appservice

import (
	"context"
	"errors"
	"sync"
	"time"
)

type UpdatePhase string

const (
	UpdatePhaseIdle       UpdatePhase = "idle"
	UpdatePhaseChecking   UpdatePhase = "checking"
	UpdatePhaseUpToDate   UpdatePhase = "up-to-date"
	UpdatePhaseAvailable  UpdatePhase = "available"
	UpdatePhaseInstalling UpdatePhase = "installing"
	UpdatePhaseReady      UpdatePhase = "ready"
	UpdatePhaseError      UpdatePhase = "error"
)

type UpdateApplyMode string

const (
	UpdateApplyModeSelf   UpdateApplyMode = "self"
	UpdateApplyModeManual UpdateApplyMode = "manual"
)

type UpdateRelease struct {
	Version      string `json:"version"`
	Name         string `json:"name"`
	Notes        string `json:"notes"`
	PublishedAt  string `json:"publishedAt"`
	ReleaseURL   string `json:"releaseURL"`
	ArtifactName string `json:"artifactName"`
	ArtifactSize int64  `json:"artifactSize"`
}

type UpdateProgress struct {
	Written int64   `json:"written"`
	Total   int64   `json:"total"`
	Rate    float64 `json:"rate"`
}

type UpdateFailure struct {
	Stage   UpdatePhase `json:"stage"`
	Message string      `json:"message"`
}

type UpdateSnapshot struct {
	Revision            uint64          `json:"revision"`
	Phase               UpdatePhase     `json:"phase"`
	CurrentVersion      string          `json:"currentVersion"`
	ApplyMode           UpdateApplyMode `json:"applyMode"`
	Release             *UpdateRelease  `json:"release,omitempty"`
	Progress            *UpdateProgress `json:"progress,omitempty"`
	Failure             *UpdateFailure  `json:"failure,omitempty"`
	CanCancel           bool            `json:"canCancel"`
	RestartAfterInstall bool            `json:"restartAfterInstall"`
}

var (
	errUpdateBusy             = errors.New("an update operation is already running")
	errUpdateReady            = errors.New("an update is ready to restart")
	errUpdateUnavailable      = errors.New("no update is available")
	errUpdateManualOnly       = errors.New("this installation must be updated manually")
	errUpdateCannotBeCanceled = errors.New("the current update stage cannot be canceled")
	errUpdateNotReady         = errors.New("no update is ready to restart")
)

// Release describes a newer version found by an UpdateEngine.
type Release struct {
	Version      string
	Name         string
	Notes        string
	PublishedAt  time.Time
	ReleaseURL   string
	ArtifactName string
	ArtifactSize int64
}

// Progress reports how far a download has come.
type Progress struct {
	Written int64
	Total   int64
	Rate    float64
}

// UpdateEngine finds and installs updates. Check returns nil when the
// application is up to date.
type UpdateEngine interface {
	Check(context.Context) (*Release, error)
	DownloadAndInstall(context.Context) error
}

type updateOperationKind uint8

const (
	updateOperationCheck updateOperationKind = iota + 1
	updateOperationDownload
)

type updateOperation struct {
	id     uint64
	kind   updateOperationKind
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type updateController struct {
	mu sync.Mutex

	engine UpdateEngine
	// publish receives every new snapshot, outside the state lock.
	publish func(UpdateSnapshot)
	// requestRestart asks the application to restart for the staged update,
	// outside the state lock.
	requestRestart func()

	snapshot      UpdateSnapshot
	operation     *updateOperation
	nextOperation uint64
	closed        bool
}

func newUpdateController(
	engine UpdateEngine,
	currentVersion string,
	applyMode UpdateApplyMode,
	publish func(UpdateSnapshot),
	requestRestart func(),
) *updateController {
	return &updateController{
		engine:         engine,
		publish:        publish,
		requestRestart: requestRestart,
		snapshot: UpdateSnapshot{
			Phase:          UpdatePhaseIdle,
			CurrentVersion: currentVersion,
			ApplyMode:      applyMode,
		},
	}
}

func (c *updateController) Snapshot() UpdateSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneUpdateSnapshot(c.snapshot)
}

func (c *updateController) Check() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("update controller is shutting down")
	}
	if c.operation != nil {
		c.mu.Unlock()
		return errUpdateBusy
	}
	if c.readyLocked() {
		c.mu.Unlock()
		return errUpdateReady
	}

	op := c.newOperationLocked(updateOperationCheck)
	c.snapshot.Phase = UpdatePhaseChecking
	c.snapshot.Release = nil
	c.snapshot.Progress = nil
	c.snapshot.Failure = nil
	c.snapshot.CanCancel = true
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)

	go c.runCheck(op)
	return nil
}

func (c *updateController) runCheck(op *updateOperation) {
	defer close(op.done)
	defer op.cancel()
	release, err := c.engine.Check(op.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || op.ctx.Err() != nil {
			c.finishOperation(op, func(snapshot *UpdateSnapshot) {
				snapshot.Phase = UpdatePhaseIdle
				snapshot.Release = nil
				snapshot.Progress = nil
				snapshot.Failure = nil
			})
			return
		}
		c.finishWithError(op, UpdatePhaseChecking, err)
		return
	}

	if release == nil {
		c.finishOperation(op, func(snapshot *UpdateSnapshot) {
			snapshot.Phase = UpdatePhaseUpToDate
			snapshot.Release = nil
			snapshot.Progress = nil
			snapshot.Failure = nil
		})
		return
	}

	mapped := mapUpdateRelease(release)
	c.finishOperation(op, func(snapshot *UpdateSnapshot) {
		snapshot.Phase = UpdatePhaseAvailable
		snapshot.Release = &mapped
		snapshot.Progress = nil
		snapshot.Failure = nil
	})
}

func (c *updateController) Download(restartAfterInstall bool) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("update controller is shutting down")
	}
	if c.operation != nil {
		c.mu.Unlock()
		return errUpdateBusy
	}
	if c.snapshot.ApplyMode != UpdateApplyModeSelf {
		c.mu.Unlock()
		return errUpdateManualOnly
	}
	if c.readyLocked() || c.snapshot.Release == nil || (c.snapshot.Phase != UpdatePhaseAvailable && c.snapshot.Phase != UpdatePhaseError) {
		c.mu.Unlock()
		return errUpdateUnavailable
	}

	op := c.newOperationLocked(updateOperationDownload)
	c.snapshot.Phase = UpdatePhaseInstalling
	c.snapshot.RestartAfterInstall = restartAfterInstall
	c.snapshot.Progress = &UpdateProgress{Total: c.snapshot.Release.ArtifactSize}
	c.snapshot.Failure = nil
	c.snapshot.CanCancel = true
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)

	go c.runDownload(op)
	return nil
}

func (c *updateController) runDownload(op *updateOperation) {
	defer close(op.done)
	defer op.cancel()
	err := c.engine.DownloadAndInstall(op.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || op.ctx.Err() != nil {
			c.finishOperation(op, func(snapshot *UpdateSnapshot) {
				snapshot.Phase = UpdatePhaseAvailable
				snapshot.Progress = nil
				snapshot.Failure = nil
			})
			return
		}
		c.finishWithError(op, UpdatePhaseInstalling, err)
		return
	}

	var restart bool
	c.finishOperation(op, func(snapshot *UpdateSnapshot) {
		snapshot.Phase = UpdatePhaseReady
		snapshot.Progress = nil
		snapshot.Failure = nil
		// A successful Install may win a cancellation race. Keep the
		// installed update, but do not turn cancellation or quit into restart.
		restart = snapshot.RestartAfterInstall && op.ctx.Err() == nil && !c.closed
	})
	if restart {
		_ = c.Restart()
	}
}

// readyLocked includes a failed restart: installation has still succeeded.
func (c *updateController) readyLocked() bool {
	return c.snapshot.Phase == UpdatePhaseReady || (c.snapshot.Phase == UpdatePhaseError && c.snapshot.Failure != nil && c.snapshot.Failure.Stage == UpdatePhaseReady)
}

// Restart routes both explicit and automatic restarts through the app's guards.
func (c *updateController) Restart() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("update controller is shutting down")
	}
	if c.snapshot.ApplyMode != UpdateApplyModeSelf || !c.readyLocked() {
		c.mu.Unlock()
		return errUpdateNotReady
	}
	c.snapshot.Phase = UpdatePhaseReady
	c.snapshot.Failure = nil
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
	if c.requestRestart != nil {
		c.requestRestart()
	}
	return nil
}

func (c *updateController) Cancel() error {
	c.mu.Lock()
	op := c.operation
	if op == nil {
		c.mu.Unlock()
		return nil
	}
	if !c.snapshot.CanCancel {
		c.mu.Unlock()
		return errUpdateCannotBeCanceled
	}
	done := op.done
	op.cancel()
	c.mu.Unlock()

	<-done
	return nil
}

func (c *updateController) Shutdown() {
	c.mu.Lock()
	if c.closed {
		op := c.operation
		c.mu.Unlock()
		if op != nil {
			<-op.done
		}
		return
	}
	c.closed = true
	op := c.operation
	if op != nil {
		op.cancel()
	}
	c.mu.Unlock()

	if op != nil {
		<-op.done
	}
}

func (c *updateController) FrameworkProgress(progress Progress) {
	c.mu.Lock()
	op := c.operation
	if op == nil || op.kind != updateOperationDownload || op.ctx.Err() != nil || c.snapshot.Phase != UpdatePhaseInstalling {
		c.mu.Unlock()
		return
	}
	c.snapshot.Progress = &UpdateProgress{
		Written: progress.Written,
		Total:   progress.Total,
		Rate:    progress.Rate,
	}
	c.snapshot.CanCancel = progress.Total <= 0 || progress.Written < progress.Total
	// Progress carries the complete state too: an initial snapshot or a
	// slower publisher must never leave a newer byte count with old controls.
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) RestartFailed(message string) {
	c.mu.Lock()
	if c.closed || c.operation != nil || !c.readyLocked() {
		c.mu.Unlock()
		return
	}
	c.snapshot.Phase = UpdatePhaseError
	c.snapshot.Progress = nil
	c.snapshot.Failure = &UpdateFailure{Stage: UpdatePhaseReady, Message: message}
	c.snapshot.CanCancel = false
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) newOperationLocked(kind updateOperationKind) *updateOperation {
	c.nextOperation++
	ctx, cancel := context.WithCancel(context.Background())
	op := &updateOperation{
		id:     c.nextOperation,
		kind:   kind,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	c.operation = op
	return op
}

func (c *updateController) finishOperation(op *updateOperation, update func(*UpdateSnapshot)) {
	c.mu.Lock()
	if c.operation != op {
		c.mu.Unlock()
		return
	}
	update(&c.snapshot)
	c.snapshot.CanCancel = false
	c.operation = nil
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) finishWithError(op *updateOperation, stage UpdatePhase, err error) {
	c.mu.Lock()
	if c.operation != op {
		c.mu.Unlock()
		return
	}
	c.snapshot.Phase = UpdatePhaseError
	c.snapshot.Progress = nil
	c.snapshot.Failure = &UpdateFailure{Stage: stage, Message: err.Error()}
	c.snapshot.CanCancel = false
	c.operation = nil
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) reviseLocked() UpdateSnapshot {
	c.snapshot.Revision++
	return cloneUpdateSnapshot(c.snapshot)
}

func (c *updateController) emitState(snapshot UpdateSnapshot) {
	if c.publish != nil {
		c.publish(snapshot)
	}
}

func cloneUpdateSnapshot(snapshot UpdateSnapshot) UpdateSnapshot {
	cloned := snapshot
	if snapshot.Release != nil {
		release := *snapshot.Release
		cloned.Release = &release
	}
	cloned.Progress = cloneUpdateProgress(snapshot.Progress)
	if snapshot.Failure != nil {
		failure := *snapshot.Failure
		cloned.Failure = &failure
	}
	return cloned
}

func cloneUpdateProgress(progress *UpdateProgress) *UpdateProgress {
	if progress == nil {
		return nil
	}
	cloned := *progress
	return &cloned
}

func mapUpdateRelease(release *Release) UpdateRelease {
	if release == nil {
		return UpdateRelease{}
	}
	publishedAt := ""
	if !release.PublishedAt.IsZero() {
		publishedAt = release.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return UpdateRelease{
		Version:      release.Version,
		Name:         release.Name,
		Notes:        release.Notes,
		PublishedAt:  publishedAt,
		ReleaseURL:   release.ReleaseURL,
		ArtifactName: release.ArtifactName,
		ArtifactSize: release.ArtifactSize,
	}
}
