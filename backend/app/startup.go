package app

import (
	"fmt"
	"os"

	"github.com/josexy/flowlens/backend/pkg/logger"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
)

func reportStartupFailure(stage string, err error) {
	message := fmt.Sprintf("FlowLens startup failed: %s: %v", stage, err)
	_, _ = fmt.Fprintln(os.Stderr, message)
	if _, configureErr := logger.Configure(logger.Config{
		Enabled:      true,
		Level:        logger.DefaultLogLevel(),
		LogDir:       logger.DefaultLogDir(),
		MaxSizeBytes: logger.DefaultLogMaxSizeBytes,
		MaxBackups:   logger.DefaultLogMaxBackups,
		Console:      logger.DebugMode(),
		DebugMode:    logger.DebugMode(),
	}); configureErr == nil {
		logger.G().Error(message)
		_ = logger.Close()
	}
}

func singleInstanceUniqueID() string {
	if logger.DebugMode() {
		return appservice.APP_IDENTIFIER + ".dev"
	}
	return appservice.APP_IDENTIFIER
}
