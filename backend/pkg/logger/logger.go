package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/logx"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	currentLogFileName      = "flowlens.log"
	rotatedLogFilePrefix    = "flowlens-"
	rotatedLogFileExtension = ".log"
	defaultLogDirName       = "logs"

	DefaultLogMaxSizeBytes int64 = 10 * 1024 * 1024
	DefaultLogMaxBackups         = 5
)

type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type Config struct {
	Enabled      bool
	Level        LogLevel
	LogDir       string
	MaxSizeBytes int64
	MaxBackups   int
	Console      bool
	DebugMode    bool
}

type LogStatus struct {
	Enabled      bool     `json:"enabled"`
	Level        LogLevel `json:"level"`
	LogDir       string   `json:"logDir"`
	CurrentFile  string   `json:"currentFile"`
	MaxSizeBytes int64    `json:"maxSizeBytes"`
	MaxBackups   int      `json:"maxBackups"`
	Console      bool     `json:"console"`
	DebugMode    bool     `json:"debugMode"`
}

var (
	globalManager = newRuntimeManager(detectDebugBuild())

	G = GetLoggerX
)

type managedSink struct {
	mu     sync.Mutex
	writer io.Writer
}

type managedSlogHandler struct {
	enabled *atomic.Bool
	handler slog.Handler
}

func (h managedSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.enabled != nil && !h.enabled.Load() {
		return false
	}
	return h.handler.Enabled(ctx, level)
}

func (h managedSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.enabled != nil && !h.enabled.Load() {
		return nil
	}
	return h.handler.Handle(ctx, record)
}

func (h managedSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return managedSlogHandler{
		enabled: h.enabled,
		handler: h.handler.WithAttrs(attrs),
	}
}

func (h managedSlogHandler) WithGroup(name string) slog.Handler {
	return managedSlogHandler{
		enabled: h.enabled,
		handler: h.handler.WithGroup(name),
	}
}

func (s *managedSink) SetWriter(writer io.Writer) {
	s.mu.Lock()
	s.writer = writer
	s.mu.Unlock()
}

func (s *managedSink) Write(data []byte) (int, error) {
	s.mu.Lock()
	writer := s.writer
	s.mu.Unlock()

	if writer == nil {
		return len(data), nil
	}
	return writer.Write(data)
}

func (s *managedSink) Sync() error {
	s.mu.Lock()
	writer := s.writer
	s.mu.Unlock()

	type syncer interface {
		Sync() error
	}
	if writer == nil {
		return nil
	}
	if value, ok := writer.(syncer); ok {
		return value.Sync()
	}
	return nil
}

type rollingFileWriter struct {
	mu          sync.Mutex
	dir         string
	maxSize     int64
	maxBackups  int
	currentPath string
	file        *os.File
	size        int64
}

func newRollingFileWriter() *rollingFileWriter {
	return &rollingFileWriter{}
}

func (w *rollingFileWriter) configure(dir string, maxSize int64, maxBackups int, ensureOpen bool) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.dir = absDir
	w.maxSize = maxSize
	w.maxBackups = maxBackups
	w.currentPath = filepath.Join(absDir, currentLogFileName)
	if w.file != nil && !samePath(w.file.Name(), w.currentPath) {
		if err := w.closeLocked(); err != nil {
			return err
		}
	}

	if !ensureOpen {
		return w.closeLocked()
	}

	if err := ensurePrivateLogDir(absDir); err != nil {
		return err
	}
	return w.openCurrentLocked()
}

func (w *rollingFileWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentPath == "" {
		return 0, errors.New("log file path is not configured")
	}
	if w.file == nil {
		if err := fs.EnsurePrivateDir(w.dir); err != nil {
			return 0, err
		}
	}
	if err := w.openCurrentLocked(); err != nil {
		return 0, err
	}
	if w.maxSize > 0 && w.size > 0 && w.size+int64(len(data)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	return n, err
}

func (w *rollingFileWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Sync()
}

func (w *rollingFileWriter) StatusPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentPath
}

func (w *rollingFileWriter) TruncateCurrent() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncateLocked(w.currentPath)
}

func (w *rollingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

func (w *rollingFileWriter) closeLocked() error {
	if w.file == nil {
		w.size = 0
		return nil
	}
	err := w.file.Close()
	w.file = nil
	w.size = 0
	return err
}

func (w *rollingFileWriter) openCurrentLocked() error {
	if w.currentPath == "" {
		return errors.New("log file path is not configured")
	}
	if w.file != nil {
		if info, err := w.file.Stat(); err == nil {
			w.size = info.Size()
		}
		return nil
	}
	file, err := os.OpenFile(w.currentPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fs.PrivateFileMode)
	if err != nil {
		return err
	}
	if err := fs.EnsurePrivateFile(w.currentPath); err != nil {
		_ = file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rollingFileWriter) rotateLocked() error {
	currentPath := w.currentPath
	if currentPath == "" {
		return errors.New("log file path is not configured")
	}
	if err := w.closeLocked(); err != nil {
		return err
	}

	if _, err := os.Stat(currentPath); err == nil {
		rotatedPath, rotatedErr := nextRotatedLogPath(w.dir)
		if rotatedErr != nil {
			return rotatedErr
		}
		if err := os.Rename(currentPath, rotatedPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := w.openCurrentLocked(); err != nil {
		return err
	}
	return w.enforceBackupsLocked()
}

func (w *rollingFileWriter) enforceBackupsLocked() error {
	entries, err := listManagedLogEntries(w.dir, w.currentPath)
	if err != nil {
		return err
	}
	oldFiles := make([]managedLogEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsCurrent {
			oldFiles = append(oldFiles, entry)
		}
	}
	sort.SliceStable(oldFiles, func(i, j int) bool {
		if oldFiles[i].ModifiedAt == oldFiles[j].ModifiedAt {
			return oldFiles[i].Name > oldFiles[j].Name
		}
		return oldFiles[i].ModifiedAt > oldFiles[j].ModifiedAt
	})

	for idx := w.maxBackups; idx < len(oldFiles); idx++ {
		if err := os.Remove(oldFiles[idx].Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (w *rollingFileWriter) truncateLocked(path string) error {
	if path == "" {
		return errors.New("log file path is empty")
	}
	if samePath(path, w.currentPath) {
		if err := w.closeLocked(); err != nil {
			return err
		}
		if err := fs.EnsurePrivateDir(filepath.Dir(path)); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.PrivateFileMode)
		if err != nil {
			return err
		}
		if err := fs.EnsurePrivateFile(path); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := w.openCurrentLocked(); err != nil {
			return err
		}
		w.size = 0
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err = file.Seek(0, io.SeekStart)
	return err
}

type runtimeManager struct {
	mu         sync.RWMutex
	sink       *managedSink
	rolling    *rollingFileWriter
	atomic     *logx.AtomicLevel
	console    bool
	slogOn     atomic.Bool
	logger     logx.Logger
	wails      *slog.Logger
	levelVar   slog.LevelVar
	status     LogStatus
	configured bool
}

func newRuntimeManager(debugMode bool) *runtimeManager {
	sink := &managedSink{}
	if debugMode {
		sink.SetWriter(os.Stdout)
	}

	manager := &runtimeManager{
		sink:    sink,
		rolling: newRollingFileWriter(),
		atomic:  logx.NewAtomicLevel(logx.LevelInfo),
		console: debugMode,
		status: LogStatus{
			Enabled:      false,
			Level:        LogLevelInfo,
			LogDir:       DefaultLogDir(),
			CurrentFile:  filepath.Join(DefaultLogDir(), currentLogFileName),
			MaxSizeBytes: DefaultLogMaxSizeBytes,
			MaxBackups:   DefaultLogMaxBackups,
			Console:      debugMode,
			DebugMode:    debugMode,
		},
	}
	manager.levelVar.Set(toSlogLevel(LogLevelInfo))
	manager.slogOn.Store(debugMode)
	manager.wails = slog.New(managedSlogHandler{
		enabled: &manager.slogOn,
		handler: slog.NewTextHandler(manager.sink, &slog.HandlerOptions{
			Level: &manager.levelVar,
		}),
	})
	manager.logger = manager.buildLogger()
	return manager
}

func (m *runtimeManager) buildLogger() logx.Logger {
	return newBaseLogContext().
		WithColorfulset(false, logx.TextColorAttri{}).
		WithAtomicLevel(m.atomic).
		WithWriter(m.sink).
		Build()
}

func (m *runtimeManager) buildOutputWriter(enabled bool, console bool) io.Writer {
	if !enabled {
		return nil
	}
	if console {
		return io.MultiWriter(os.Stdout, m.rolling)
	}
	return m.rolling
}

func (m *runtimeManager) Configure(cfg Config) (LogStatus, error) {
	normalized := sanitizeConfig(cfg)
	ensureOpen := normalized.Enabled

	if err := m.rolling.configure(normalized.LogDir, normalized.MaxSizeBytes, normalized.MaxBackups, ensureOpen); err != nil {
		return LogStatus{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sink.SetWriter(m.buildOutputWriter(normalized.Enabled, normalized.Console))
	m.atomic.SetLevel(toLogxLevel(normalized.Level))
	m.levelVar.Set(toSlogLevel(normalized.Level))
	m.slogOn.Store(normalized.Enabled)
	m.console = normalized.Console
	m.logger = m.buildLogger()
	m.status = LogStatus{
		Enabled:      normalized.Enabled,
		Level:        normalized.Level,
		LogDir:       normalized.LogDir,
		CurrentFile:  filepath.Join(normalized.LogDir, currentLogFileName),
		MaxSizeBytes: normalized.MaxSizeBytes,
		MaxBackups:   normalized.MaxBackups,
		Console:      normalized.Enabled && normalized.Console,
		DebugMode:    normalized.DebugMode,
	}
	m.configured = true
	return m.status, nil
}

func (m *runtimeManager) EnableBootstrapOutput(writer io.Writer) {
	m.sink.SetWriter(writer)
	m.slogOn.Store(writer != nil)
}

func (m *runtimeManager) SetEnabled(enabled bool) (LogStatus, error) {
	m.mu.RLock()
	current := m.status
	console := m.console
	m.mu.RUnlock()

	if err := m.rolling.configure(current.LogDir, current.MaxSizeBytes, current.MaxBackups, enabled); err != nil {
		return LogStatus{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sink.SetWriter(m.buildOutputWriter(enabled, console))
	m.slogOn.Store(enabled)
	m.status.Enabled = enabled
	m.status.Console = enabled && console
	return m.status, nil
}

func (m *runtimeManager) SetLevel(level LogLevel) (LogStatus, error) {
	normalizedLevel, err := ParseLogLevel(string(level))
	if err != nil {
		return LogStatus{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.atomic.SetLevel(toLogxLevel(normalizedLevel))
	m.levelVar.Set(toSlogLevel(normalizedLevel))
	m.status.Level = normalizedLevel
	return m.status, nil
}

func (m *runtimeManager) Status() LogStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *runtimeManager) Logger() logx.Logger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.logger
}

func (m *runtimeManager) WailsLogger() *slog.Logger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wails
}

func (m *runtimeManager) ClearCurrentFile() error {
	return m.rolling.TruncateCurrent()
}

func (m *runtimeManager) DeleteOldFiles() error {
	status := m.Status()
	files, err := listManagedLogEntries(status.LogDir, status.CurrentFile)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.IsCurrent {
			continue
		}
		if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (m *runtimeManager) OpenDir() error {
	status := m.Status()
	if err := ensurePrivateLogDir(status.LogDir); err != nil {
		return err
	}
	app := application.Get()
	if app == nil {
		return errors.New("application is not ready")
	}
	return app.Env.OpenFileManager(status.LogDir, false)
}

func (m *runtimeManager) Close() error {
	m.sink.SetWriter(nil)
	return m.rolling.Close()
}

func Configure(cfg Config) (LogStatus, error) {
	return globalManager.Configure(cfg)
}

func EnableBootstrapOutput(writer io.Writer) {
	globalManager.EnableBootstrapOutput(writer)
}

func SetEnabled(enabled bool) (LogStatus, error) {
	return globalManager.SetEnabled(enabled)
}

func SetLevel(level LogLevel) (LogStatus, error) {
	return globalManager.SetLevel(level)
}

func Status() LogStatus {
	return globalManager.Status()
}

func ClearCurrentFile() error {
	return globalManager.ClearCurrentFile()
}

func DeleteOldFiles() error {
	return globalManager.DeleteOldFiles()
}

func OpenDir() error {
	return globalManager.OpenDir()
}

func Close() error {
	return globalManager.Close()
}

func WailsLogger() *slog.Logger {
	return globalManager.WailsLogger()
}

func GetLoggerX() logx.Logger {
	return globalManager.Logger()
}

func GetFileLoggerX(logFile string) (logx.Logger, error) {
	fp, err := os.OpenFile(logFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.PrivateFileMode)
	if err != nil {
		return nil, err
	}
	if err := fs.EnsurePrivateFile(logFile); err != nil {
		_ = fp.Close()
		return nil, err
	}
	return newBaseLogContext().
		WithColorfulset(false, logx.TextColorAttri{}).
		WithWriter(logx.AddSync(io.MultiWriter(os.Stdout, fp))).
		Build(), nil
}

func GetLoggerXContext() *logx.LogContext {
	status := Status()
	return newBaseLogContext().WithLevel(toLogxLevel(status.Level))
}

func SetLoggerX(value logx.Logger) {
	globalManager.mu.Lock()
	globalManager.logger = value
	globalManager.mu.Unlock()
}

func DefaultLogDir() string {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return filepath.Join(".", defaultLogDirName)
	}
	return filepath.Join(baseDir, defaultLogDirName)
}

func DefaultLogLevel() LogLevel {
	return LogLevelInfo
}

func DebugMode() bool {
	return detectDebugBuild()
}

func ParseLogLevel(value string) (LogLevel, error) {
	level := LogLevel(strings.ToLower(strings.TrimSpace(value)))
	switch level {
	case LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return level, nil
	default:
		return "", fmt.Errorf("invalid log level: %s", value)
	}
}

func NormalizeLogLevel(value string) LogLevel {
	level, err := ParseLogLevel(value)
	if err != nil {
		return DefaultLogLevel()
	}
	return level
}

func NormalizeLogDir(dir string) string {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return DefaultLogDir()
	}
	absDir, err := filepath.Abs(filepath.Clean(trimmed))
	if err != nil {
		return DefaultLogDir()
	}
	return absDir
}

func NormalizeLogMaxSizeBytes(size int64) int64 {
	if size <= 0 {
		return DefaultLogMaxSizeBytes
	}
	return size
}

func NormalizeLogMaxBackups(backups int) int {
	if backups <= 0 {
		return DefaultLogMaxBackups
	}
	return backups
}

func sanitizeConfig(cfg Config) Config {
	if cfg.Level == "" {
		cfg.Level = DefaultLogLevel()
	}
	cfg.Level = NormalizeLogLevel(string(cfg.Level))
	cfg.LogDir = NormalizeLogDir(cfg.LogDir)
	cfg.MaxSizeBytes = NormalizeLogMaxSizeBytes(cfg.MaxSizeBytes)
	cfg.MaxBackups = NormalizeLogMaxBackups(cfg.MaxBackups)
	if !cfg.DebugMode {
		cfg.Console = false
	}
	return cfg
}

func newBaseLogContext() *logx.LogContext {
	return logx.NewLogContext().
		WithLevel(logx.LevelInfo).
		WithColorfulset(false, logx.TextColorAttri{}).
		WithCallerKey(true, logx.CallerOption{Formatter: logx.ShortFile}).
		WithTimeKey(true, logx.TimeOption{Layout: time.RFC3339Nano}).
		WithLevelKey(true, logx.LevelOption{LowerKey: false}).
		WithEscapeQuote(true).
		WithEncoder(logx.Text).
		WithReflectValue(true)
}

func toLogxLevel(level LogLevel) logx.LevelType {
	switch level {
	case LogLevelTrace:
		return logx.LevelTrace
	case LogLevelDebug:
		return logx.LevelDebug
	case LogLevelWarn:
		return logx.LevelWarn
	case LogLevelError:
		return logx.LevelError
	default:
		return logx.LevelInfo
	}
}

func toSlogLevel(level LogLevel) slog.Level {
	switch level {
	case LogLevelTrace, LogLevelDebug:
		return slog.LevelDebug
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type managedLogEntry struct {
	Name       string
	Path       string
	ModifiedAt int64
	IsCurrent  bool
}

func ensurePrivateLogDir(dir string) error {
	if err := fs.EnsurePrivateDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var permissionErrors []error
	for _, entry := range entries {
		if !isManagedLogName(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := fs.EnsurePrivateFile(path); err != nil {
			permissionErrors = append(permissionErrors, fmt.Errorf("tighten log file %s: %w", path, err))
		}
	}
	return errors.Join(permissionErrors...)
}

func listManagedLogEntries(dir string, currentFile string) ([]managedLogEntry, error) {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []managedLogEntry{}, nil
		}
		return nil, err
	}

	files := make([]managedLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isManagedLogName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(absDir, entry.Name())
		files = append(files, managedLogEntry{
			Name:       entry.Name(),
			Path:       path,
			ModifiedAt: info.ModTime().UnixMilli(),
			IsCurrent:  samePath(path, currentFile),
		})
	}

	sort.SliceStable(files, func(i, j int) bool {
		if files[i].IsCurrent != files[j].IsCurrent {
			return files[i].IsCurrent
		}
		if files[i].ModifiedAt == files[j].ModifiedAt {
			return files[i].Name > files[j].Name
		}
		return files[i].ModifiedAt > files[j].ModifiedAt
	})
	return files, nil
}

func isManagedLogName(name string) bool {
	if name == currentLogFileName {
		return true
	}
	return strings.HasPrefix(name, rotatedLogFilePrefix) && strings.HasSuffix(name, rotatedLogFileExtension)
}

func nextRotatedLogPath(dir string) (string, error) {
	baseName := rotatedLogFilePrefix + time.Now().Format("20060102-150405")
	candidate := filepath.Join(dir, baseName+rotatedLogFileExtension)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	}
	for idx := 1; idx <= 999; idx++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%03d%s", baseName, idx, rotatedLogFileExtension))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", errors.New("unable to create unique rotated log filename")
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftPath := filepath.Clean(left)
	rightPath := filepath.Clean(right)
	if runtimeGOOSWindows() {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func runtimeGOOSWindows() bool {
	return os.PathSeparator == '\\'
}
