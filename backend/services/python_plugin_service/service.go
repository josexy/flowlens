package pythonpluginservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	appfs "github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	RegistryEventName = "python-plugins:registry"
	StatusEventName   = "python-plugins:status"
	LogEventName      = "python-plugins:log"

	workerShutdownWait = 5 * time.Second
)

type ManagedPluginFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type RuntimeStatus struct {
	Enabled         bool   `json:"enabled"`
	InterpreterPath string `json:"interpreterPath"`
	HookTimeoutMs   int    `json:"hookTimeoutMs"`
	Ready           bool   `json:"ready"`
	WorkerCount     int    `json:"workerCount"`
	ProtocolVersion int    `json:"protocolVersion"`
	SDKAPIVersion   int    `json:"sdkApiVersion"`
	PythonMajor     int    `json:"pythonMajor"`
	PythonMinor     int    `json:"pythonMinor"`
	PythonPatch     int    `json:"pythonPatch"`
	Implementation  string `json:"implementation"`
	Error           string `json:"error"`
}

type RegistryEvent struct {
	EventID  uint64 `json:"eventId"`
	Change   string `json:"change"`
	PluginID string `json:"pluginId,omitempty"`
}

type StatusEvent struct {
	EventID uint64        `json:"eventId"`
	Status  RuntimeStatus `json:"status"`
}

type PluginLogEntry struct {
	EventID     uint64 `json:"eventId"`
	RequestID   string `json:"requestId"`
	ExecutionID string `json:"executionId"`
	PluginID    string `json:"pluginId"`
	Level       string `json:"level"`
	Stream      string `json:"stream"`
	Message     string `json:"message"`
	Timestamp   int64  `json:"timestamp"`
}

func init() {
	application.RegisterEvent[RegistryEvent](RegistryEventName)
	application.RegisterEvent[StatusEvent](StatusEventName)
	application.RegisterEvent[PluginLogEntry](LogEventName)
}

type serviceEventEmitter func(name string, value any)

type PythonPluginService struct {
	repository  *repository
	packages    *packageManager
	settings    *settingservice.SettingService
	runner      *httpRequestRunner
	runtimeRoot string

	baseMu     sync.RWMutex
	baseCtx    context.Context
	baseCancel context.CancelFunc
	app        *application.App
	emit       serviceEventEmitter

	mutationMu sync.Mutex
	runtimeMu  sync.Mutex
	pool       *workerPool
	poolPath   string
	hello      WorkerHello
	runtimeErr string
	closed     bool

	eventID      atomic.Uint64
	shutdownOnce sync.Once
	shutdownErr  error
}

func New(db *sql.DB, settings *settingservice.SettingService) (*PythonPluginService, error) {
	baseDir, err := appfs.GetBaseStorageDir()
	if err != nil {
		return nil, fmt.Errorf("resolve Python plugin storage root: %w", err)
	}
	service, err := newWithPaths(
		db,
		settings,
		filepath.Join(baseDir, "python_plugins"),
		filepath.Join(baseDir, "python_plugin_runtime"),
		nil,
	)
	if err != nil {
		return nil, err
	}
	return service, nil
}

func newWithPaths(
	db *sql.DB,
	settings *settingservice.SettingService,
	packagesRoot string,
	runtimeRoot string,
	emit serviceEventEmitter,
) (*PythonPluginService, error) {
	if db == nil {
		return nil, errors.New("Python plugin database is not available")
	}
	if settings == nil {
		return nil, errors.New("Python plugin setting service is not available")
	}
	repository := newRepository(db)
	service := &PythonPluginService{
		repository:  repository,
		settings:    settings,
		runtimeRoot: runtimeRoot,
		emit:        emit,
	}
	packages, err := newPackageManager(repository, packagesRoot, runtimeRoot, service)
	if err != nil {
		return nil, err
	}
	service.packages = packages
	service.runner = newHTTPRequestRunner(repository, settings, packages, service)
	return service, nil
}

func (s *PythonPluginService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s == nil {
		return errors.New("Python plugin service is not available")
	}
	s.baseMu.Lock()
	s.baseCtx, s.baseCancel = context.WithCancel(ctx)
	s.app = application.Get()
	s.baseMu.Unlock()
	if err := s.packages.reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile Python plugin packages: %w", err)
	}
	logger.G().Info("Python plugin service started")
	s.emitRegistry("reconciled", "")
	s.emitStatus()
	return nil
}

func (s *PythonPluginService) ServiceShutdown() error {
	return s.Shutdown()
}

// Shutdown stops accepting runtime work and terminates every worker process.
//
//wails:ignore
func (s *PythonPluginService) Shutdown() error {
	if s == nil {
		return nil
	}
	s.shutdownOnce.Do(func() {
		s.baseMu.Lock()
		if s.baseCancel != nil {
			s.baseCancel()
		}
		s.baseMu.Unlock()
		s.runtimeMu.Lock()
		s.closed = true
		pool := s.pool
		s.pool = nil
		s.poolPath = ""
		s.runtimeMu.Unlock()
		if pool != nil {
			ctx, cancel := context.WithTimeout(context.Background(), workerShutdownWait)
			s.shutdownErr = pool.Shutdown(ctx)
			cancel()
		}
		logger.G().Info("Python plugin service stopped")
	})
	return s.shutdownErr
}

// BeginRequest implements the narrow proxy_service runner contract.
//
//wails:ignore
func (s *PythonPluginService) BeginRequest(
	ctx context.Context,
	request proxyservice.HTTPRequestPluginBeginRequest,
) (proxyservice.HTTPRequestPluginSession, error) {
	if s == nil || s.runner == nil {
		return nil, errors.New("Python plugin runner is unavailable")
	}
	return s.runner.BeginRequest(ctx, request)
}

// ValidateRevision lazily provisions the worker pool for package activation.
//
//wails:ignore
func (s *PythonPluginService) ValidateRevision(ctx context.Context, request RevisionValidationRequest) error {
	pool, err := s.currentPool(ctx)
	if err != nil {
		return err
	}
	if err := pool.ValidateRevision(ctx, request); err != nil {
		s.markPoolRuntimeError(pool, err)
		return err
	}
	s.markRuntimeReady(pool)
	return nil
}

// Invoke implements the HTTP request hook runtime without exposing worker protocol
// details through Wails bindings.
//
//wails:ignore
func (s *PythonPluginService) Invoke(ctx context.Context, request InvokeRequest) (InvokeResult, error) {
	pool, err := s.currentPool(ctx)
	if err != nil {
		return InvokeResult{}, err
	}
	result, err := pool.Invoke(ctx, request)
	if err != nil {
		s.markPoolRuntimeError(pool, err)
		return InvokeResult{}, err
	}
	s.markRuntimeReady(pool)
	return result, nil
}

func (s *PythonPluginService) ListPlugins() ([]*Plugin, error) {
	return s.repository.listPlugins(s.operationContext())
}

func (s *PythonPluginService) GetPlugin(pluginID string) (*Plugin, error) {
	return s.repository.getPlugin(s.operationContext(), pluginID)
}

func (s *PythonPluginService) CreatePlugin(input CreatePluginInput) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	plugin, err := s.packages.createPlugin(s.operationContext(), input)
	if err != nil {
		return nil, err
	}
	s.emitRegistry("created", plugin.ID)
	return plugin, nil
}

func (s *PythonPluginService) UpdatePlugin(pluginID string, input UpdatePluginInput) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	ctx := s.operationContext()
	current, err := s.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	name, description, paramsJSON, err := normalizePluginInput(input.Name, input.Description, input.ParamsJSON)
	if err != nil {
		return nil, err
	}
	if name != current.Name || description != current.Description {
		manifest, err := defaultManifest(current.ID, name, description)
		if err != nil {
			return nil, err
		}
		value, err := marshalManifest(manifest)
		if err != nil {
			return nil, err
		}
		if _, err := s.packages.writeFile(ctx, current.ID, manifestFileName, value); err != nil {
			s.emitRegistry("validation_failed", current.ID)
			return nil, err
		}
	}
	updated, err := s.repository.updatePlugin(ctx, current.ID, UpdatePluginInput{
		Name: name, Description: description, ParamsJSON: paramsJSON,
	})
	if err != nil {
		return nil, err
	}
	s.emitRegistry("updated", updated.ID)
	return updated, nil
}

func (s *PythonPluginService) UpdatePluginParams(pluginID, paramsJSON string) (*Plugin, error) {
	current, err := s.repository.getPlugin(s.operationContext(), pluginID)
	if err != nil {
		return nil, err
	}
	return s.UpdatePlugin(pluginID, UpdatePluginInput{
		Name: current.Name, Description: current.Description, ParamsJSON: paramsJSON,
	})
}

func (s *PythonPluginService) DeletePlugin(pluginID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := s.packages.deletePlugin(s.operationContext(), pluginID); err != nil {
		return err
	}
	s.emitRegistry("deleted", strings.TrimSpace(pluginID))
	return nil
}

func (s *PythonPluginService) ReorderPlugins(pluginIDs []string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := s.repository.reorderPlugins(s.operationContext(), pluginIDs); err != nil {
		return err
	}
	s.emitRegistry("reordered", "")
	return nil
}

func (s *PythonPluginService) SetPluginEnabled(pluginID string, enabled bool) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	ctx := s.operationContext()
	pluginID = strings.TrimSpace(pluginID)
	if !enabled {
		if err := s.repository.setPluginEnabled(ctx, pluginID, false); err != nil {
			return nil, err
		}
		plugin, err := s.repository.getPlugin(ctx, pluginID)
		if err == nil {
			s.emitRegistry("disabled", pluginID)
		}
		return plugin, err
	}
	plugin, err := s.packages.activateCurrent(ctx, pluginID)
	if err != nil {
		_ = s.repository.setPluginEnabled(ctx, pluginID, false)
		s.emitRegistry("validation_failed", pluginID)
		return nil, err
	}
	if err := s.repository.setPluginEnabled(ctx, pluginID, true); err != nil {
		return nil, err
	}
	plugin, err = s.repository.getPlugin(ctx, pluginID)
	if err == nil {
		s.emitRegistry("enabled", pluginID)
	}
	return plugin, err
}

func (s *PythonPluginService) ValidatePlugin(pluginID string) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	plugin, err := s.packages.activateCurrent(s.operationContext(), pluginID)
	if err != nil {
		s.emitRegistry("validation_failed", strings.TrimSpace(pluginID))
		return nil, err
	}
	s.emitRegistry("validated", plugin.ID)
	return plugin, nil
}

func (s *PythonPluginService) ReloadPlugin(pluginID string) (*Plugin, error) {
	return s.ValidatePlugin(pluginID)
}

func (s *PythonPluginService) ListPluginFiles(pluginID string) ([]string, error) {
	return s.packages.listFiles(s.operationContext(), pluginID)
}

func (s *PythonPluginService) ReadPluginFile(pluginID, relativePath string) (*ManagedPluginFile, error) {
	value, err := s.packages.readFile(s.operationContext(), pluginID, relativePath)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(value) {
		return nil, errors.New("Python plugin editor only supports UTF-8 text files")
	}
	return &ManagedPluginFile{Path: filepath.ToSlash(relativePath), Content: string(value)}, nil
}

func (s *PythonPluginService) WritePluginFile(pluginID, relativePath, content string) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	plugin, err := s.packages.writeFile(s.operationContext(), pluginID, relativePath, []byte(content))
	if err != nil {
		s.emitRegistry("validation_failed", strings.TrimSpace(pluginID))
		return nil, err
	}
	s.emitRegistry("files_changed", plugin.ID)
	return plugin, nil
}

func (s *PythonPluginService) RenamePluginFile(pluginID, oldRelativePath, newRelativePath string) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	plugin, err := s.packages.renameFile(s.operationContext(), pluginID, oldRelativePath, newRelativePath)
	if err != nil {
		s.emitRegistry("validation_failed", strings.TrimSpace(pluginID))
		return nil, err
	}
	s.emitRegistry("files_changed", plugin.ID)
	return plugin, nil
}

func (s *PythonPluginService) DeletePluginFile(pluginID, relativePath string) (*Plugin, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	plugin, err := s.packages.deleteFile(s.operationContext(), pluginID, relativePath)
	if err != nil {
		s.emitRegistry("validation_failed", strings.TrimSpace(pluginID))
		return nil, err
	}
	s.emitRegistry("files_changed", plugin.ID)
	return plugin, nil
}

func (s *PythonPluginService) CreateRule(pluginID string, input CreateRuleInput) (*Rule, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	rule, err := s.repository.createRule(s.operationContext(), pluginID, input)
	if err != nil {
		return nil, err
	}
	s.emitRegistry("rules_changed", rule.PluginID)
	return rule, nil
}

func (s *PythonPluginService) UpdateRule(pluginID, ruleID string, input UpdateRuleInput) (*Rule, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	rule, err := s.repository.updateRule(s.operationContext(), pluginID, ruleID, input)
	if err != nil {
		return nil, err
	}
	s.emitRegistry("rules_changed", rule.PluginID)
	return rule, nil
}

func (s *PythonPluginService) DeleteRule(pluginID, ruleID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := s.repository.deleteRule(s.operationContext(), pluginID, ruleID); err != nil {
		return err
	}
	s.emitRegistry("rules_changed", strings.TrimSpace(pluginID))
	return nil
}

func (s *PythonPluginService) ReorderRules(pluginID string, ruleIDs []string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := s.repository.reorderRules(s.operationContext(), pluginID, ruleIDs); err != nil {
		return err
	}
	s.emitRegistry("rules_changed", strings.TrimSpace(pluginID))
	return nil
}

func (s *PythonPluginService) GetRuntimeConfig() (*settingservice.PythonPluginConfig, error) {
	config, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *PythonPluginService) ConfigureRuntime(config settingservice.PythonPluginConfig) (*RuntimeStatus, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	config.InterpreterPath = strings.TrimSpace(config.InterpreterPath)
	if config.InterpreterPath != "" {
		resolved, err := validateInterpreterPath(config.InterpreterPath)
		if err != nil {
			return nil, err
		}
		config.InterpreterPath = resolved
	} else if config.Enabled {
		return nil, errors.New("Python interpreter path is required before enabling plugins")
	}
	oldConfig, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		return nil, err
	}

	var candidatePool *workerPool
	var candidateHello WorkerHello
	if config.Enabled {
		s.runtimeMu.Lock()
		closed := s.closed
		s.runtimeMu.Unlock()
		if closed {
			return nil, errors.New("Python plugin service is shutting down")
		}
		candidatePool, err = newWorkerPool(workerPoolConfig{
			InterpreterPath: config.InterpreterPath,
			RuntimeRoot:     s.runtimeRoot,
			Size:            defaultWorkerCount,
			LogSink:         s.handleWorkerLog,
		})
		if err != nil {
			return nil, err
		}
		probeCtx, probeCancel := context.WithTimeout(s.operationContext(), 10*time.Second)
		candidateHello, err = candidatePool.Probe(probeCtx)
		probeCancel()
		if err != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
			_ = candidatePool.Shutdown(shutdownCtx)
			shutdownCancel()
			return nil, fmt.Errorf("validate Python runtime before enabling: %w", err)
		}
	}
	if err := s.settings.SavePythonPluginConfig(&config); err != nil {
		if candidatePool != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
			_ = candidatePool.Shutdown(shutdownCtx)
			shutdownCancel()
		}
		return nil, err
	}
	persistedConfig, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		if candidatePool != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
			_ = candidatePool.Shutdown(shutdownCtx)
			shutdownCancel()
		}
		_ = s.settings.SavePythonPluginConfig(&oldConfig)
		return nil, err
	}

	if candidatePool != nil {
		s.runtimeMu.Lock()
		if s.closed {
			s.runtimeMu.Unlock()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
			_ = candidatePool.Shutdown(shutdownCtx)
			shutdownCancel()
			_ = s.settings.SavePythonPluginConfig(&oldConfig)
			return nil, errors.New("Python plugin service is shutting down")
		}
		oldPool := s.pool
		s.pool = candidatePool
		s.poolPath = persistedConfig.InterpreterPath
		s.hello = candidateHello
		s.runtimeErr = ""
		s.runtimeMu.Unlock()
		if oldPool != nil && oldPool != candidatePool {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
			shutdownErr := oldPool.Shutdown(shutdownCtx)
			shutdownCancel()
			if shutdownErr != nil {
				s.markRuntimeError(shutdownErr)
			}
		}
	} else if oldConfig.InterpreterPath != persistedConfig.InterpreterPath || !persistedConfig.Enabled {
		if err := s.closeCurrentPool(); err != nil {
			s.markRuntimeError(err)
			return nil, err
		}
		s.clearRuntimeError()
	}

	status := s.runtimeStatus(persistedConfig)
	s.emitStatusValue(status)
	return &status, nil
}

func (s *PythonPluginService) TestInterpreter(interpreterPath string) (*RuntimeStatus, error) {
	resolved, err := validateInterpreterPath(interpreterPath)
	if err != nil {
		return nil, err
	}
	pool, err := newWorkerPool(workerPoolConfig{
		InterpreterPath: resolved,
		RuntimeRoot:     s.runtimeRoot,
		Size:            1,
		LogSink:         s.handleWorkerLog,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.operationContext(), 10*time.Second)
	hello, probeErr := pool.Probe(ctx)
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), workerShutdownWait)
	shutdownErr := pool.Shutdown(shutdownCtx)
	shutdownCancel()
	if probeErr != nil {
		return nil, probeErr
	}
	if shutdownErr != nil {
		return nil, shutdownErr
	}
	config, _ := settingservice.GetPythonPluginConfig(s.settings)
	config.InterpreterPath = resolved
	status := runtimeStatusFromHello(config, hello)
	return &status, nil
}

func (s *PythonPluginService) ReloadRuntime() (*RuntimeStatus, error) {
	if err := s.closeCurrentPool(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.operationContext(), 10*time.Second)
	defer cancel()
	pool, err := s.currentPool(ctx)
	if err != nil {
		s.markRuntimeError(err)
		return nil, err
	}
	hello, err := pool.Probe(ctx)
	if err != nil {
		s.markRuntimeError(err)
		return nil, err
	}
	s.setRuntimeHello(hello)
	config, _ := settingservice.GetPythonPluginConfig(s.settings)
	status := runtimeStatusFromHello(config, hello)
	s.emitStatusValue(status)
	return &status, nil
}

func (s *PythonPluginService) GetRuntimeStatus() (*RuntimeStatus, error) {
	config, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		return nil, err
	}
	status := s.runtimeStatus(config)
	return &status, nil
}

func (s *PythonPluginService) OpenPluginsDirectory() error {
	return openDirectory(s.packages.packagesRoot)
}

func (s *PythonPluginService) OpenPluginDirectory(pluginID string) error {
	plugin, err := s.repository.getPlugin(s.operationContext(), pluginID)
	if err != nil {
		return err
	}
	return openDirectory(s.packages.packagePath(plugin.ID))
}

func (s *PythonPluginService) operationContext() context.Context {
	s.baseMu.RLock()
	ctx := s.baseCtx
	s.baseMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *PythonPluginService) currentPool(ctx context.Context) (*workerPool, error) {
	config, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(config.InterpreterPath)
	if path == "" {
		err := errors.New("Python interpreter path is not configured")
		s.markRuntimeError(err)
		return nil, err
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.closed {
		return nil, errors.New("Python plugin service is shutting down")
	}
	if s.pool != nil && s.poolPath == path {
		return s.pool, nil
	}
	if s.pool != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), workerShutdownWait)
		_ = s.pool.Shutdown(shutdownCtx)
		cancel()
		s.pool = nil
	}
	pool, err := newWorkerPool(workerPoolConfig{
		InterpreterPath: path,
		RuntimeRoot:     s.runtimeRoot,
		Size:            defaultWorkerCount,
		LogSink:         s.handleWorkerLog,
	})
	if err != nil {
		s.runtimeErr = sanitizeRuntimeError(err)
		return nil, err
	}
	s.pool = pool
	s.poolPath = path
	s.hello = WorkerHello{}
	s.runtimeErr = ""
	return pool, nil
}

func (s *PythonPluginService) closeCurrentPool() error {
	s.runtimeMu.Lock()
	pool := s.pool
	s.pool = nil
	s.poolPath = ""
	s.hello = WorkerHello{}
	s.runtimeMu.Unlock()
	if pool == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), workerShutdownWait)
	err := pool.Shutdown(ctx)
	cancel()
	return err
}

func (s *PythonPluginService) markRuntimeReady(pool *workerPool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	hello, err := pool.Probe(ctx)
	cancel()
	if err != nil {
		return
	}
	s.runtimeMu.Lock()
	if s.pool != pool {
		s.runtimeMu.Unlock()
		return
	}
	changed := s.hello != hello || s.runtimeErr != ""
	s.hello = hello
	s.runtimeErr = ""
	s.runtimeMu.Unlock()
	if changed {
		s.emitStatus()
	}
}

func (s *PythonPluginService) markPoolRuntimeError(pool *workerPool, err error) {
	message := sanitizeRuntimeError(err)
	s.runtimeMu.Lock()
	if s.pool != pool {
		s.runtimeMu.Unlock()
		return
	}
	changed := s.runtimeErr != message
	s.runtimeErr = message
	s.runtimeMu.Unlock()
	if changed {
		s.emitStatus()
	}
}

func (s *PythonPluginService) setRuntimeHello(hello WorkerHello) {
	s.runtimeMu.Lock()
	s.hello = hello
	s.runtimeErr = ""
	s.runtimeMu.Unlock()
}

func (s *PythonPluginService) markRuntimeError(err error) {
	message := sanitizeRuntimeError(err)
	s.runtimeMu.Lock()
	changed := s.runtimeErr != message
	s.runtimeErr = message
	s.runtimeMu.Unlock()
	if changed {
		s.emitStatus()
	}
}

func (s *PythonPluginService) clearRuntimeError() {
	s.runtimeMu.Lock()
	s.runtimeErr = ""
	s.runtimeMu.Unlock()
}

func (s *PythonPluginService) runtimeStatus(config settingservice.PythonPluginConfig) RuntimeStatus {
	s.runtimeMu.Lock()
	hello, runtimeErr, poolPath := s.hello, s.runtimeErr, s.poolPath
	s.runtimeMu.Unlock()
	status := RuntimeStatus{
		Enabled: config.Enabled, InterpreterPath: config.InterpreterPath,
		HookTimeoutMs: config.HookTimeoutMs, WorkerCount: defaultWorkerCount,
		Error: runtimeErr,
	}
	if poolPath == strings.TrimSpace(config.InterpreterPath) && hello.PythonMajor != 0 {
		status = runtimeStatusFromHello(config, hello)
		status.Error = runtimeErr
	}
	return status
}

func runtimeStatusFromHello(config settingservice.PythonPluginConfig, hello WorkerHello) RuntimeStatus {
	return RuntimeStatus{
		Enabled: config.Enabled, InterpreterPath: config.InterpreterPath,
		HookTimeoutMs: config.HookTimeoutMs, Ready: true, WorkerCount: defaultWorkerCount,
		ProtocolVersion: hello.ProtocolVersion, SDKAPIVersion: hello.SDKAPIVersion,
		PythonMajor: hello.PythonMajor, PythonMinor: hello.PythonMinor, PythonPatch: hello.PythonPatch,
		Implementation: hello.Implementation,
	}
}

func (s *PythonPluginService) handleWorkerLog(value WorkerLog) {
	entry := PluginLogEntry{
		EventID: s.eventID.Add(1), RequestID: value.RequestID, ExecutionID: value.ExecutionID,
		PluginID: value.PluginID, Level: strings.ToLower(strings.TrimSpace(value.Level)),
		Stream: value.Stream, Message: value.Message, Timestamp: value.Timestamp,
	}
	if entry.Timestamp <= 0 {
		entry.Timestamp = time.Now().UnixMicro()
	}
	s.logWorkerEntry(entry)
	s.emitEvent(LogEventName, entry)
}

func (s *PythonPluginService) logWorkerEntry(entry PluginLogEntry) {
	message := fmt.Sprintf("Python plugin log: plugin_id=%s stream=%s", entry.PluginID, entry.Stream)
	switch entry.Level {
	case "debug":
		logger.G().Debug(message)
	case "warning", "warn":
		logger.G().Warn(message)
	case "error":
		logger.G().Error(message)
	default:
		logger.G().Info(message)
	}
}

func (s *PythonPluginService) emitRegistry(change, pluginID string) {
	s.emitEvent(RegistryEventName, RegistryEvent{
		EventID: s.eventID.Add(1), Change: change, PluginID: pluginID,
	})
}

func (s *PythonPluginService) emitStatus() {
	config, err := settingservice.GetPythonPluginConfig(s.settings)
	if err != nil {
		return
	}
	s.emitStatusValue(s.runtimeStatus(config))
}

func (s *PythonPluginService) emitStatusValue(status RuntimeStatus) {
	s.emitEvent(StatusEventName, StatusEvent{EventID: s.eventID.Add(1), Status: status})
}

func (s *PythonPluginService) emitEvent(name string, value any) {
	if s.emit != nil {
		s.emit(name, value)
		return
	}
	s.baseMu.RLock()
	app := s.app
	s.baseMu.RUnlock()
	if app != nil {
		app.Event.Emit(name, value)
	}
}

func sanitizeRuntimeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Map(func(value rune) rune {
		if value == '\n' || value == '\r' || value == '\t' {
			return ' '
		}
		if value < 0x20 || value == 0x7f {
			return -1
		}
		return value
	}, strings.TrimSpace(err.Error()))
	runes := []rune(message)
	if len(runes) > 2048 {
		message = string(runes[:2048]) + "…"
	}
	return message
}

func openDirectory(path string) error {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	case "linux":
		command = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("opening directories is unsupported on %s", runtime.GOOS)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}
