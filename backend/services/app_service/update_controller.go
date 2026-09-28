package appservice

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

const (
	UpdateStateChangedEventName  = "app:update-state-changed"
	UpdateProgressEventName      = "app:update-progress"
	UpdateRestartFailedEventName = "app:update-restart-failed"
)

type UpdatePhase string

const (
	UpdatePhaseIdle        UpdatePhase = "idle"
	UpdatePhaseChecking    UpdatePhase = "checking"
	UpdatePhaseUpToDate    UpdatePhase = "up-to-date"
	UpdatePhaseAvailable   UpdatePhase = "available"
	UpdatePhaseDownloading UpdatePhase = "downloading"
	UpdatePhaseVerifying   UpdatePhase = "verifying"
	UpdatePhasePreparing   UpdatePhase = "preparing"
	UpdatePhaseReady       UpdatePhase = "ready"
	UpdatePhaseError       UpdatePhase = "error"
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
	Revision       uint64          `json:"revision"`
	Phase          UpdatePhase     `json:"phase"`
	CurrentVersion string          `json:"currentVersion"`
	ApplyMode      UpdateApplyMode `json:"applyMode"`
	Release        *UpdateRelease  `json:"release,omitempty"`
	Progress       *UpdateProgress `json:"progress,omitempty"`
	Failure        *UpdateFailure  `json:"failure,omitempty"`
	CanCancel      bool            `json:"canCancel"`
}

type UpdateProgressEvent struct {
	Revision uint64          `json:"revision"`
	Progress *UpdateProgress `json:"progress"`
}

var (
	errUpdateBusy             = errors.New("an update operation is already running")
	errUpdateReady            = errors.New("an update is ready to restart")
	errUpdateUnavailable      = errors.New("no update is available")
	errUpdateManualOnly       = errors.New("this installation must be updated manually")
	errUpdateCannotBeCanceled = errors.New("the current update stage cannot be canceled")
)

type updateEngine interface {
	Check(context.Context) (*updater.Release, error)
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

	engine updateEngine
	emit   func(string, ...any) bool

	snapshot      UpdateSnapshot
	operation     *updateOperation
	nextOperation uint64
	closed        bool
}

func newUpdateController(
	engine updateEngine,
	currentVersion string,
	applyMode UpdateApplyMode,
	emit func(string, ...any) bool,
) *updateController {
	return &updateController{
		engine: engine,
		emit:   emit,
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
	if c.snapshot.Phase == UpdatePhaseReady {
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

func (c *updateController) Download() error {
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
	if c.snapshot.Release == nil || (c.snapshot.Phase != UpdatePhaseAvailable && c.snapshot.Phase != UpdatePhaseError) {
		c.mu.Unlock()
		return errUpdateUnavailable
	}

	op := c.newOperationLocked(updateOperationDownload)
	c.snapshot.Phase = UpdatePhaseDownloading
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
		c.finishWithError(op, c.currentFailureStage(op), err)
		return
	}

	c.finishOperation(op, func(snapshot *UpdateSnapshot) {
		snapshot.Phase = UpdatePhaseReady
		snapshot.Progress = nil
		snapshot.Failure = nil
	})
}

func (c *updateController) Cancel() error {
	c.mu.Lock()
	op := c.operation
	if op == nil {
		c.mu.Unlock()
		return nil
	}
	if c.snapshot.Phase != UpdatePhaseChecking && c.snapshot.Phase != UpdatePhaseDownloading {
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

func (c *updateController) FrameworkPhase(phase UpdatePhase) {
	c.mu.Lock()
	op := c.operation
	if op == nil || op.kind != updateOperationDownload || op.ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	if c.snapshot.Phase == phase {
		c.mu.Unlock()
		return
	}
	c.snapshot.Phase = phase
	c.snapshot.CanCancel = phase == UpdatePhaseDownloading
	if phase != UpdatePhaseDownloading {
		c.snapshot.Progress = nil
	}
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) FrameworkProgress(progress updater.Progress) {
	c.mu.Lock()
	op := c.operation
	if op == nil || op.kind != updateOperationDownload || op.ctx.Err() != nil || c.snapshot.Phase != UpdatePhaseDownloading {
		c.mu.Unlock()
		return
	}
	c.snapshot.Progress = &UpdateProgress{
		Written: progress.Written,
		Total:   progress.Total,
		Rate:    progress.Rate,
	}
	c.snapshot.Revision++
	event := UpdateProgressEvent{
		Revision: c.snapshot.Revision,
		Progress: cloneUpdateProgress(c.snapshot.Progress),
	}
	c.mu.Unlock()
	if c.emit != nil {
		c.emit(UpdateProgressEventName, event)
	}
}

func (c *updateController) FrameworkError(info updater.ErrorInfo) {
	c.mu.Lock()
	op := c.operation
	if op == nil || op.ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	stage := updatePhaseFromUpdaterStage(info.Stage)
	c.snapshot.Phase = UpdatePhaseError
	c.snapshot.Progress = nil
	c.snapshot.Failure = &UpdateFailure{Stage: stage, Message: info.Message}
	c.snapshot.CanCancel = false
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) RestartFailed(message string) {
	c.mu.Lock()
	if c.snapshot.ApplyMode != UpdateApplyModeSelf || c.snapshot.Release == nil {
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
	close(op.done)
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
	alreadyReported := c.snapshot.Phase == UpdatePhaseError && c.snapshot.Failure != nil
	if !alreadyReported {
		c.snapshot.Phase = UpdatePhaseError
		c.snapshot.Progress = nil
		c.snapshot.Failure = &UpdateFailure{Stage: stage, Message: err.Error()}
		c.snapshot.CanCancel = false
	}
	c.operation = nil
	close(op.done)
	if alreadyReported {
		c.mu.Unlock()
		return
	}
	snapshot := c.reviseLocked()
	c.mu.Unlock()
	c.emitState(snapshot)
}

func (c *updateController) currentFailureStage(op *updateOperation) UpdatePhase {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.operation != op {
		return UpdatePhaseDownloading
	}
	switch c.snapshot.Phase {
	case UpdatePhaseVerifying, UpdatePhasePreparing:
		return c.snapshot.Phase
	default:
		return UpdatePhaseDownloading
	}
}

func (c *updateController) reviseLocked() UpdateSnapshot {
	c.snapshot.Revision++
	return cloneUpdateSnapshot(c.snapshot)
}

func (c *updateController) emitState(snapshot UpdateSnapshot) {
	if c.emit != nil {
		c.emit(UpdateStateChangedEventName, snapshot)
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

func mapUpdateRelease(release *updater.Release) UpdateRelease {
	if release == nil {
		return UpdateRelease{}
	}
	releaseURL, _ := release.Metadata["github.release.htmlURL"].(string)
	publishedAt := ""
	if !release.PublishedAt.IsZero() {
		publishedAt = release.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return UpdateRelease{
		Version:      release.Version,
		Name:         release.Name,
		Notes:        release.Notes,
		PublishedAt:  publishedAt,
		ReleaseURL:   releaseURL,
		ArtifactName: release.Artifact.Filename,
		ArtifactSize: release.Artifact.Size,
	}
}

func updatePhaseFromUpdaterStage(stage updater.Stage) UpdatePhase {
	switch stage {
	case updater.StageCheck:
		return UpdatePhaseChecking
	case updater.StageVerify:
		return UpdatePhaseVerifying
	case updater.StageInstall:
		return UpdatePhasePreparing
	default:
		return UpdatePhaseDownloading
	}
}

func progressFromEventData(data any) (updater.Progress, error) {
	switch value := data.(type) {
	case updater.Progress:
		return value, nil
	case *updater.Progress:
		if value != nil {
			return *value, nil
		}
	}
	return updater.Progress{}, fmt.Errorf("unexpected updater progress payload %T", data)
}

func errorInfoFromEventData(data any) (updater.ErrorInfo, error) {
	switch value := data.(type) {
	case updater.ErrorInfo:
		return value, nil
	case *updater.ErrorInfo:
		if value != nil {
			return *value, nil
		}
	}
	return updater.ErrorInfo{}, fmt.Errorf("unexpected updater error payload %T", data)
}
