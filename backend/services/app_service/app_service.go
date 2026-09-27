package appservice

import (
	"context"
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"sync/atomic"

	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

const (
	APP_NAME       = "FlowLens"
	APP_IDENTIFIER = "com.josexy.flowlens"
	APP_VERSION    = "1.0.2"

	DEFAULT_WINDOW_WIDTH  = 1024
	DEFAULT_WINDOW_HEIGHT = 768
	MIN_WINDOW_WIDTH      = DEFAULT_WINDOW_WIDTH
	MIN_WINDOW_HEIGHT     = DEFAULT_WINDOW_HEIGHT

	wailsModulePath = "github.com/wailsapp/wails/v3"
)

// EnvironmentInfo describes the build and runtime environment used by the running application.
type EnvironmentInfo struct {
	AppVersion   string `json:"appVersion"`
	GoVersion    string `json:"goVersion"`
	WailsVersion string `json:"wailsVersion"`
	BuildCommit  string `json:"buildCommit"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
}

var injectedBuildCommit string

type AppService struct {
	app                 *application.App
	updateCheckInFlight atomic.Bool
}

func New() *AppService {
	return &AppService{}
}

func (a *AppService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.app = application.Get()
	return nil
}

// CheckForUpdates starts the Wails updater flow without blocking the frontend
// binding call. The updater owns the check, download, verification, install,
// and restart UI; this guard only prevents overlapping checks from repeated
// clicks on the status bar button.
func (a *AppService) CheckForUpdates() error {
	if a.app == nil {
		return errors.New("application is not ready")
	}
	if !CanSelfUpdate() {
		return errors.New("self-update is unavailable for this installation")
	}
	if !a.updateCheckInFlight.CompareAndSwap(false, true) {
		return nil
	}
	go func() {
		defer a.updateCheckInFlight.Store(false)
		if err := a.app.Updater.CheckAndInstall(context.Background()); err != nil {
			logger.G().Warnf("Check for updates failed: %v", err)
		}
	}()
	return nil
}

// RestartForUpdate requests the application's guarded restart flow, including
// the unsaved-settings confirmation, even if the update window was closed.
func (a *AppService) RestartForUpdate() error {
	if a.app == nil {
		return errors.New("application is not ready")
	}
	if !CanSelfUpdate() {
		return errors.New("self-update is unavailable for this installation")
	}
	if a.app.Updater.State() != updater.StateReady || a.app.Updater.DownloadedPath() == "" {
		return updater.ErrNotReady
	}
	a.app.Event.Emit(updater.EventUserRestart)
	return nil
}

// CanSelfUpdate reports whether this executable is installed in a location
// that the updater can replace without invoking an installer or package
// manager. Package-managed binaries and machine-wide Windows installs are
// intentionally excluded because the updater helper has no elevation path.
// Linux installations use manual updates until AppImage replacement is supported.
func CanSelfUpdate() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return canSelfUpdatePath(runtime.GOOS, executable, os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"))
}

// CanSelfUpdate reports whether the running installation can be replaced by
// the built-in updater.
func (a *AppService) CanSelfUpdate() bool {
	return CanSelfUpdate()
}

func (a *AppService) currentWindow() application.Window {
	if a.app == nil {
		return nil
	}
	if window := a.app.Window.Current(); window != nil {
		return window
	}
	windows := a.app.Window.GetAll()
	if len(windows) == 0 {
		return nil
	}
	return windows[0]
}

// WindowMinimize minimizes the window
func (a *AppService) WindowMinimize() {
	if window := a.currentWindow(); window != nil {
		window.Minimise()
	}
}

// WindowMaximize toggles window maximize/unmaximize
func (a *AppService) WindowMaximize() {
	window := a.currentWindow()
	if window == nil {
		return
	}
	if window.IsMaximised() {
		window.Restore()
		return
	}
	window.Maximise()
}

// WindowClose closes the window
func (a *AppService) WindowClose() {
	if window := a.currentWindow(); window != nil {
		window.Close()
		return
	}
	if a.app != nil {
		a.app.Quit()
	}
}

// WindowIsMaximized returns whether the window is maximized
func (a *AppService) WindowIsMaximized() bool {
	if window := a.currentWindow(); window != nil {
		return window.IsMaximised()
	}
	return false
}

// GetEnvironmentInfo returns build environment details for the running application.
func (a *AppService) GetEnvironmentInfo() EnvironmentInfo {
	buildInfo, _ := debug.ReadBuildInfo()
	var environment application.EnvironmentInfo
	if a.app != nil && a.app.Env != nil {
		environment = a.app.Env.Info()
	}
	return buildEnvironmentInfo(buildInfo, environment)
}

func buildEnvironmentInfo(buildInfo *debug.BuildInfo, environment application.EnvironmentInfo) EnvironmentInfo {
	goVersion := runtime.Version()
	if buildInfo != nil && buildInfo.GoVersion != "" {
		goVersion = buildInfo.GoVersion
	}
	goos := environment.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := environment.Arch
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	return EnvironmentInfo{
		AppVersion:   "v" + APP_VERSION,
		GoVersion:    goVersion,
		WailsVersion: moduleVersion(buildInfo, wailsModulePath),
		BuildCommit:  resolveBuildCommit(buildInfo),
		GOOS:         goos,
		GOARCH:       goarch,
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
