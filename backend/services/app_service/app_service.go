package appservice

import (
	"context"
	"runtime"
	"runtime/debug"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	APP_NAME       = "FlowLens"
	APP_IDENTIFIER = "com.josexy.flowlens"
	APP_VERSION    = "1.0.0"

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
	app *application.App
}

func New() *AppService {
	return &AppService{}
}

func (a *AppService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.app = application.Get()
	return nil
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
