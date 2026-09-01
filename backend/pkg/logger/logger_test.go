package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigureTightensExistingLogStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod dir setup: %v", err)
	}
	rotated := filepath.Join(dir, "flowlens-20260827-120000.000.log")
	if err := os.WriteFile(rotated, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(rotated, 0o644); err != nil {
		t.Fatalf("Chmod file setup: %v", err)
	}

	manager := newRuntimeManager(false)
	t.Cleanup(func() { _ = manager.Close() })
	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                0o700,
		rotated:            0o600,
		status.CurrentFile: 0o600,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat %s: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("mode for %s = %04o, want %04o", path, got, want)
		}
	}
}

func TestBootstrapOutputUsesExistingWailsLoggerUntilConfigured(t *testing.T) {
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	existingLogger := manager.WailsLogger()
	var output bytes.Buffer
	manager.EnableBootstrapOutput(&output)
	existingLogger.Error("bootstrap failure")
	if !strings.Contains(output.String(), "bootstrap failure") {
		t.Fatalf("expected bootstrap output, got %q", output.String())
	}

	if _, err := manager.Configure(Config{
		Enabled:      false,
		Level:        LogLevelInfo,
		LogDir:       t.TempDir(),
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	existingLogger.Error("after configuration")
	if strings.Contains(output.String(), "after configuration") {
		t.Fatalf("bootstrap writer remained active after configuration: %q", output.String())
	}
}

func TestEnabledDefaultWritesToCurrentLogFile(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	manager.Logger().Info("hello logger")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "hello logger") {
		t.Fatalf("expected log file to contain message, got %q", string(content))
	}
}

func TestDisabledModeDropsLogsAndDoesNotCreateFile(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	if _, err := manager.Configure(Config{
		Enabled:      false,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	manager.Logger().Info("this should be dropped")

	if _, err := os.Stat(filepath.Join(dir, currentLogFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected current log file to be absent, stat error: %v", err)
	}
}

func TestSetEnabledDynamicallyUpdatesExistingLogger(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	existingLogger := manager.Logger()
	existingLogger.Info("before disable")

	if _, err := manager.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	existingLogger.Info("while disabled")

	if _, err := manager.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	existingLogger.Info("after re-enable")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "before disable") {
		t.Fatalf("expected initial log to be written, got %q", text)
	}
	if strings.Contains(text, "while disabled") {
		t.Fatalf("expected logs while disabled to be dropped, got %q", text)
	}
	if !strings.Contains(text, "after re-enable") {
		t.Fatalf("expected existing logger to resume after re-enable, got %q", text)
	}
}

func TestLevelFilteringSuppressesLowerLevels(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelError,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	manager.Logger().Info("ignore me")
	manager.Logger().Error("keep me")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "ignore me") {
		t.Fatalf("expected lower level message to be filtered, got %q", text)
	}
	if !strings.Contains(text, "keep me") {
		t.Fatalf("expected error message to remain, got %q", text)
	}
}

func TestSetLevelDynamicallyUpdatesExistingLogger(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	existingLogger := manager.Logger()
	existingLogger.Debug("before update")

	if _, err := manager.SetLevel(LogLevelDebug); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	existingLogger.Debug("after update")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "before update") {
		t.Fatalf("expected debug logs before update to be filtered, got %q", text)
	}
	if !strings.Contains(text, "after update") {
		t.Fatalf("expected existing logger to honor dynamic level updates, got %q", text)
	}
}

func TestSlogLoggerHonorsDynamicEnabledAndLevel(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	existingLogger := manager.WailsLogger()
	existingLogger.Info("slog before disable")

	if _, err := manager.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	existingLogger.Error("slog while disabled")

	if _, err := manager.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	if _, err := manager.SetLevel(LogLevelError); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	existingLogger.Info("slog info filtered")
	existingLogger.Error("slog error kept")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "slog before disable") {
		t.Fatalf("expected initial slog message to be written, got %q", text)
	}
	if strings.Contains(text, "slog while disabled") {
		t.Fatalf("expected disabled slog message to be dropped, got %q", text)
	}
	if strings.Contains(text, "slog info filtered") {
		t.Fatalf("expected slog info message to be filtered after level update, got %q", text)
	}
	if !strings.Contains(text, "slog error kept") {
		t.Fatalf("expected slog error message to remain, got %q", text)
	}
}

func TestRotationKeepsConfiguredBackups(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	if _, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: 1,
		MaxBackups:   2,
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	for idx := range 6 {
		manager.Logger().Infof("line-%d", idx)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var currentCount int
	var backupCount int
	for _, file := range files {
		if file.IsDir() || !isManagedLogName(file.Name()) {
			continue
		}
		if file.Name() == currentLogFileName {
			currentCount++
			continue
		}
		backupCount++
	}
	if currentCount != 1 {
		t.Fatalf("expected one current log file, got %d", currentCount)
	}
	if backupCount != 2 {
		t.Fatalf("expected two rotated backups, got %d", backupCount)
	}
}

func TestClearCurrentLogFileAllowsFutureWrites(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	status, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: DefaultLogMaxSizeBytes,
		MaxBackups:   DefaultLogMaxBackups,
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	manager.Logger().Info("before clear")
	if err := manager.ClearCurrentFile(); err != nil {
		t.Fatalf("ClearCurrentFile: %v", err)
	}
	manager.Logger().Info("after clear")

	content, err := os.ReadFile(status.CurrentFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "before clear") {
		t.Fatalf("expected current log to be truncated, got %q", text)
	}
	if !strings.Contains(text, "after clear") {
		t.Fatalf("expected logging to continue after truncation, got %q", text)
	}
}

func TestDeleteOldFilesRemovesRotatedLogs(t *testing.T) {
	dir := t.TempDir()
	manager := newRuntimeManager(false)
	t.Cleanup(func() {
		_ = manager.Close()
	})

	if _, err := manager.Configure(Config{
		Enabled:      true,
		Level:        LogLevelInfo,
		LogDir:       dir,
		MaxSizeBytes: 1,
		MaxBackups:   3,
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	manager.Logger().Info("first")
	manager.Logger().Info("second")

	if err := manager.DeleteOldFiles(); err != nil {
		t.Fatalf("DeleteOldFiles: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, file := range files {
		if file.IsDir() || !isManagedLogName(file.Name()) || file.Name() == currentLogFileName {
			continue
		}
		t.Fatalf("expected rotated log files to be removed, found %s", file.Name())
	}
}
