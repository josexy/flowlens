package appservice

import (
	"context"
	"errors"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/josexy/flowlens/backend/host"
)

const (
	APP_NAME       = "FlowLens"
	APP_IDENTIFIER = "com.josexy.flowlens"
	APP_VERSION    = "1.0.5"

	DEFAULT_WINDOW_WIDTH  = 1024
	DEFAULT_WINDOW_HEIGHT = 768
	MIN_WINDOW_WIDTH      = DEFAULT_WINDOW_WIDTH
	MIN_WINDOW_HEIGHT     = DEFAULT_WINDOW_HEIGHT

	myGoModulePath = "github.com/egoist/mygo"
)

// EnvironmentInfo describes the build and runtime environment used by the running application.
type EnvironmentInfo struct {
	AppVersion  string `json:"appVersion"`
	GoVersion   string `json:"goVersion"`
	MyGoVersion string `json:"myGoVersion"`
	BuildCommit string `json:"buildCommit"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
}

var injectedBuildCommit string

type AppService struct {
	events          host.EventBus
	updateEngine    UpdateEngine
	updateApplyMode UpdateApplyMode
	updates         *updateController
	updateEventOffs []func()
	resetHandler    func() error
	resetting       atomic.Bool
	operationMu     sync.Mutex
}

func New() *AppService {
	return &AppService{}
}

// SetResetApplicationHandler wires application-owned lifecycle coordination.
func SetResetApplicationHandler(service *AppService, handler func() error) {
	service.resetHandler = handler
}

// ResetApplication schedules a confirmed factory reset for the next launch.
// The user exits and reopens the app; completion follows cleanup and startup.
func (a *AppService) ResetApplication() error {
	if a.resetHandler == nil {
		return errors.New("application is not ready")
	}
	if !a.resetting.CompareAndSwap(false, true) {
		return errors.New("application_reset_in_progress")
	}
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if err := a.resetHandler(); err != nil {
		a.resetting.Store(false)
		return err
	}
	return nil
}

// SetEventBus sets where update state changes are published and where
// restart failures are heard.
func (a *AppService) SetEventBus(events host.EventBus) { a.events = events }

// SetUpdateEngine sets how updates are found and installed. Without an engine
// update operations report that the application is not ready.
func (a *AppService) SetUpdateEngine(engine UpdateEngine, applyMode UpdateApplyMode) {
	a.updateEngine = engine
	a.updateApplyMode = applyMode
}

func (a *AppService) ServiceStartup(ctx context.Context) error {
	if a.updateEngine == nil {
		return nil
	}
	var emit func(string, ...any) bool
	if a.events != nil {
		emit = a.events.Emit
	}
	a.updates = newUpdateController(a.updateEngine, APP_VERSION, a.updateApplyMode, emit)
	if a.events != nil {
		a.updateEventOffs = []func(){
			a.events.On(UpdateRestartFailedEventName, func(event host.Event) {
				message, ok := event.Data.(string)
				if !ok || message == "" {
					return
				}
				a.updates.RestartFailed(message)
			}),
		}
	}
	return nil
}

// ReportUpdatePhase records a phase an UpdateEngine has reached.
func (a *AppService) ReportUpdatePhase(phase UpdatePhase) {
	if a.updates != nil {
		a.updates.FrameworkPhase(phase)
	}
}

// ReportUpdateProgress records how far an UpdateEngine's download has come.
func (a *AppService) ReportUpdateProgress(progress Progress) {
	if a.updates != nil {
		a.updates.FrameworkProgress(progress)
	}
}

func (a *AppService) ServiceShutdown() error {
	a.ShutdownUpdater()
	return nil
}

// ShutdownUpdater cancels and waits for any active check or download. It is
// idempotent because graceful shutdown and the service lifecycle may
// both reach it.
func (a *AppService) ShutdownUpdater() {
	for _, off := range a.updateEventOffs {
		off()
	}
	a.updateEventOffs = nil
	if a.updates != nil {
		a.updates.Shutdown()
	}
}

// GetUpdateSnapshot returns the authoritative state used by every window.
func (a *AppService) GetUpdateSnapshot() UpdateSnapshot {
	if a.updates == nil {
		return UpdateSnapshot{
			Phase:          UpdatePhaseIdle,
			CurrentVersion: APP_VERSION,
			ApplyMode:      UpdateApplyModeManual,
		}
	}
	return a.updates.Snapshot()
}

// CheckForUpdates starts a check without opening the framework updater UI.
func (a *AppService) CheckForUpdates() error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.resetting.Load() {
		return errors.New("application_reset_in_progress")
	}
	if a.updates == nil {
		return errors.New("application is not ready")
	}
	return a.updates.Check()
}

// DownloadUpdate downloads, verifies, and stages the release found by the
// latest successful check.
func (a *AppService) DownloadUpdate() error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.resetting.Load() {
		return errors.New("application_reset_in_progress")
	}
	if a.updates == nil {
		return errors.New("application is not ready")
	}
	return a.updates.Download()
}

// CancelUpdate cancels the active check or download and waits for cleanup.
func (a *AppService) CancelUpdate() error {
	if a.updates == nil {
		return errors.New("application is not ready")
	}
	return a.updates.Cancel()
}

// RestartForUpdate requests the application's guarded restart flow, including
// the unsaved-settings confirmation, even if the update window was closed.
func (a *AppService) RestartForUpdate() error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.resetting.Load() {
		return errors.New("application_reset_in_progress")
	}
	if a.updates == nil {
		return errors.New("application is not ready")
	}
	if a.updates.Snapshot().ApplyMode != UpdateApplyModeSelf {
		return errors.New("self-update is unavailable for this installation")
	}
	if a.updates.Snapshot().Phase != UpdatePhaseReady {
		return errUpdateNotReady
	}
	if a.events != nil {
		a.events.Emit(UpdateUserRestartEventName)
	}
	return nil
}

// GetEnvironmentInfo returns build environment details for the running application.
func (a *AppService) GetEnvironmentInfo() EnvironmentInfo {
	buildInfo, _ := debug.ReadBuildInfo()
	return buildEnvironmentInfo(buildInfo)
}

func buildEnvironmentInfo(buildInfo *debug.BuildInfo) EnvironmentInfo {
	goVersion := runtime.Version()
	if buildInfo != nil && buildInfo.GoVersion != "" {
		goVersion = buildInfo.GoVersion
	}

	return EnvironmentInfo{
		AppVersion:  "v" + APP_VERSION,
		GoVersion:   goVersion,
		MyGoVersion: moduleVersion(buildInfo, myGoModulePath),
		BuildCommit: resolveBuildCommit(buildInfo),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}

func resolveBuildCommit(buildInfo *debug.BuildInfo) string {
	if injectedBuildCommit != "" {
		return injectedBuildCommit
	}
	if buildInfo == nil {
		return ""
	}

	var revision string
	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		}
	}
	return revision
}

func moduleVersion(buildInfo *debug.BuildInfo, modulePath string) string {
	if buildInfo == nil {
		return ""
	}
	for _, dependency := range buildInfo.Deps {
		if dependency == nil || dependency.Path != modulePath {
			continue
		}
		if dependency.Replace != nil {
			if dependency.Replace.Version != "" {
				return dependency.Replace.Version
			}
			return "(devel)"
		}
		return dependency.Version
	}
	return ""
}
