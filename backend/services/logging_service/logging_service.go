package loggingservice

import (
	"errors"

	"github.com/josexy/flowlens/backend/pkg/logger"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type LoggingService struct {
	settings *settingservice.SettingService
}

func New(settings *settingservice.SettingService) *LoggingService {
	return &LoggingService{settings: settings}
}

func (s *LoggingService) GetLogStatus() (*logger.LogStatus, error) {
	status := logger.Status()
	return &status, nil
}

func (s *LoggingService) SetLogEnabled(enabled bool) (*logger.LogStatus, error) {
	logger.G().Infof("Log enabled update requested: enabled=%t", enabled)
	if s.settings == nil {
		return nil, errors.New("setting service is not available")
	}
	if err := settingservice.SetLogEnabled(s.settings, enabled); err != nil {
		logger.G().Warnf("Log enabled setting update failed: enabled=%t error=%v", enabled, err)
		return nil, err
	}
	status, err := logger.SetEnabled(enabled)
	if err != nil {
		logger.G().Warnf("Log enabled runtime update failed: enabled=%t error=%v", enabled, err)
		return nil, err
	}
	if err := s.settings.Save(); err != nil {
		logger.G().Warnf("Log enabled setting save failed: enabled=%t error=%v", enabled, err)
		return nil, err
	}
	if enabled {
		logger.G().Info("Log output enabled")
	}
	return &status, nil
}

func (s *LoggingService) SetLogLevel(level logger.LogLevel) (*logger.LogStatus, error) {
	logger.G().Infof("Log level update requested: level=%s", level)
	if s.settings == nil {
		return nil, errors.New("setting service is not available")
	}
	if err := settingservice.SetLogLevel(s.settings, string(level)); err != nil {
		logger.G().Warnf("Log level setting update failed: level=%s error=%v", level, err)
		return nil, err
	}
	status, err := logger.SetLevel(level)
	if err != nil {
		logger.G().Warnf("Log level runtime update failed: level=%s error=%v", level, err)
		return nil, err
	}
	if err := s.settings.Save(); err != nil {
		logger.G().Warnf("Log level setting save failed: level=%s error=%v", level, err)
		return nil, err
	}
	logger.G().Infof("Log level updated: level=%s", status.Level)
	return &status, nil
}

func (s *LoggingService) ClearLogs() error {
	logger.G().Info("Clear logs requested")
	if err := logger.DeleteOldFiles(); err != nil {
		logger.G().Warnf("Delete old log files failed: %v", err)
		return err
	}
	if err := logger.ClearCurrentFile(); err != nil {
		logger.G().Warnf("Clear current log file failed: %v", err)
		return err
	}
	logger.G().Info("Logs cleared")
	return nil
}

func (s *LoggingService) OpenLogDir() error {
	if err := logger.OpenDir(); err != nil {
		logger.G().Warnf("Open log directory failed: %v", err)
		return err
	}
	logger.G().Info("Log directory opened")
	return nil
}
