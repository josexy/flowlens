package app

import (
	"fmt"
	"os"

	"github.com/josexy/flowlens/internal/desktop/startupfailure"
	"github.com/josexy/flowlens/internal/pkg/logger"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

// startupStageDescriptions name the startup steps in the log.
var startupStageDescriptions = map[startupfailure.Stage]string{
	startupfailure.StageReset:    "restore initial state",
	startupfailure.StageDatabase: "open application database",
	startupfailure.StageSettings: "load application settings",
	startupfailure.StageServices: "start services",
}

// startupError is a startup step that failed.
type startupError struct {
	stage startupfailure.Stage
	err   error
	// language is the saved language when the settings could be read, and
	// empty otherwise.
	language string
}

func startupFailed(stage startupfailure.Stage, err error) *startupError {
	return &startupError{stage: stage, err: err}
}

func (e *startupError) Error() string {
	return startupStageDescriptions[e.stage] + ": " + e.err.Error()
}

func (e *startupError) Unwrap() error { return e.err }

// failure is what the startup failure window shows.
func (e *startupError) failure() startupfailure.Failure {
	return startupfailure.Failure{Stage: e.stage, Error: e.err.Error()}
}

// reportStartupFailure writes the failure to standard error and to the log
// file, which the startup failure window offers to open. Logging is enabled
// for it even when the settings turn it off, since they may be unreadable.
func reportStartupFailure(failure *startupError) {
	message := "FlowLens startup failed: " + failure.Error()
	_, _ = fmt.Fprintln(os.Stderr, message)
	if _, err := logger.Configure(logger.Config{
		Enabled:      true,
		Level:        logger.DefaultLogLevel(),
		LogDir:       logger.DefaultLogDir(),
		MaxSizeBytes: logger.DefaultLogMaxSizeBytes,
		MaxBackups:   logger.DefaultLogMaxBackups,
		Console:      logger.DebugMode(),
		DebugMode:    logger.DebugMode(),
	}); err == nil {
		logger.G().Error(message)
	}
}

// applicationName also keys the single-instance lock, so a development build
// can run beside an installed copy.
func applicationName() string {
	if logger.DebugMode() {
		return appservice.APP_NAME + " Dev"
	}
	return appservice.APP_NAME
}
