package pythonpluginservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	appfs "github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/flowlens/internal/serviceruntime/topictest"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

func TestNewStoresPythonPluginRuntimeUnderBaseStorage(t *testing.T) {
	configRoot := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", configRoot)
	case "darwin":
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
	default:
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}

	repository := newTestRepository(t)
	settings := settingservice.New(repository.db)
	if err := settings.Load(); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	service, err := New(repository.db, settings)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = service.Shutdown() })

	baseDir, err := appfs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	if want := filepath.Join(baseDir, "python_plugins"); service.packages.packagesRoot != want {
		t.Fatalf("packages root = %q, want %q", service.packages.packagesRoot, want)
	}
	if want := filepath.Join(baseDir, "python_plugin_runtime"); service.runtimeRoot != want {
		t.Fatalf("runtime root = %q, want %q", service.runtimeRoot, want)
	}
}

func TestServiceExposesPluginFilesRulesOrderingAndEnableValidation(t *testing.T) {
	service, events := newServiceHarness(t)
	validator := &recordingRevisionValidator{}
	service.packages.validator = validator

	first, err := service.CreatePlugin(CreatePluginInput{
		ID: testPluginIDOne, Name: "First", Description: "first plugin", ParamsJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("CreatePlugin first: %v", err)
	}
	second, err := service.CreatePlugin(CreatePluginInput{
		ID: testPluginIDTwo, Name: "Second", Description: "second plugin", ParamsJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("CreatePlugin second: %v", err)
	}
	files, err := service.ListPluginFiles(first.ID)
	if err != nil || !reflect.DeepEqual(files, []string{helpersFileName, mainFileName, manifestFileName}) {
		t.Fatalf("ListPluginFiles = %#v, %v", files, err)
	}
	mainFile, err := service.ReadPluginFile(first.ID, mainFileName)
	if err != nil || mainFile.Path != mainFileName || mainFile.Content != defaultMainSource {
		t.Fatalf("ReadPluginFile = %+v, %v", mainFile, err)
	}
	if _, err := service.CreatePluginFile(first.ID, "extra.py"); err != nil {
		t.Fatalf("CreatePluginFile: %v", err)
	}
	if _, err := service.WritePluginFile(first.ID, "extra.py", "VALUE = 1\n"); err != nil {
		t.Fatalf("WritePluginFile: %v", err)
	}
	if _, err := service.RenamePluginFile(first.ID, "extra.py", "lib/extra.py"); err != nil {
		t.Fatalf("RenamePluginFile: %v", err)
	}
	if _, err := service.DeletePluginFile(first.ID, "lib/extra.py"); err != nil {
		t.Fatalf("DeletePluginFile: %v", err)
	}

	updated, err := service.UpdatePluginParams(first.ID, `{"token":"not-logged"}`)
	if err != nil || updated.ParamsJSON != `{"token":"not-logged"}` {
		t.Fatalf("UpdatePluginParams = %+v, %v", updated, err)
	}
	rule, err := service.CreateRule(first.ID, CreateRuleInput{
		ID: testRuleIDOne, Enabled: true, Method: "GET", URLPattern: "https://example.com/*",
	})
	if err != nil || rule.Method != "GET" {
		t.Fatalf("CreateRule = %+v, %v", rule, err)
	}
	if _, err := service.UpdateRule(first.ID, rule.ID, UpdateRuleInput{Enabled: true, Method: "*", URLPattern: "*"}); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if err := service.ReorderRules(first.ID, []string{rule.ID}); err != nil {
		t.Fatalf("ReorderRules: %v", err)
	}

	enabled, err := service.SetPluginEnabled(first.ID, true)
	if err != nil || !enabled.Enabled || enabled.ValidationStatus != ValidationStatusValid || enabled.ActiveRevision == "" {
		t.Fatalf("SetPluginEnabled = %+v, %v", enabled, err)
	}
	validator.mu.Lock()
	validationCount := len(validator.requests)
	validator.mu.Unlock()
	if validationCount < 4 {
		t.Fatalf("validation count = %d, want file mutations and enable validation", validationCount)
	}
	if err := service.ReorderPlugins([]string{second.ID, first.ID}); err != nil {
		t.Fatalf("ReorderPlugins: %v", err)
	}
	plugins, err := service.ListPlugins()
	if err != nil || len(plugins) != 2 || plugins[0].ID != second.ID || plugins[1].ID != first.ID {
		t.Fatalf("ListPlugins = %#v, %v", plugins, err)
	}
	if err := service.DeleteRule(first.ID, rule.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if err := service.DeletePlugin(second.ID); err != nil {
		t.Fatalf("DeletePlugin: %v", err)
	}

	registry := events.registry.Values()
	if len(registry) < 10 {
		t.Fatalf("registry events = %#v", registry)
	}
	var previous uint64
	for _, event := range registry {
		if event.EventID <= previous {
			t.Fatalf("event IDs are not monotonic: %#v", registry)
		}
		previous = event.EventID
	}
}

func TestServiceEnableValidationFailureLeavesPluginDisabled(t *testing.T) {
	service, _ := newServiceHarness(t)
	service.packages.validator = &recordingRevisionValidator{err: errors.New("syntax error")}
	plugin, err := service.CreatePlugin(CreatePluginInput{ID: testPluginIDOne, Name: "Broken", ParamsJSON: `{}`})
	if err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if _, err := service.SetPluginEnabled(plugin.ID, true); err == nil {
		t.Fatal("SetPluginEnabled succeeded with invalid source")
	}
	stored, err := service.GetPlugin(plugin.ID)
	if err != nil {
		t.Fatalf("GetPlugin: %v", err)
	}
	if stored.Enabled || stored.ValidationStatus != ValidationStatusInvalid || stored.ValidationError == "" || stored.ActiveRevision != "" {
		t.Fatalf("stored plugin = %+v", stored)
	}
}

func TestServiceRuntimeConfigurationProbeAndShutdown(t *testing.T) {
	pythonPath := requirePython3(t)
	service, events := newServiceHarness(t)
	status, err := service.ConfigureRuntime(settingservice.PythonPluginConfig{
		Enabled: false, InterpreterPath: pythonPath, HookTimeoutMs: 1234,
	})
	if err != nil {
		t.Fatalf("ConfigureRuntime: %v", err)
	}
	if status.Ready || status.InterpreterPath != pythonPath || status.HookTimeoutMs != 1234 || service.pool != nil {
		t.Fatalf("lazy runtime status = %+v pool=%v", status, service.pool)
	}
	status, err = service.ReloadRuntime()
	if err != nil {
		t.Fatalf("ReloadRuntime: %v", err)
	}
	if !status.Ready || status.PythonMajor < 3 || status.ProtocolVersion != workerProtocolVersion || status.SDKAPIVersion != workerSDKAPIVersion {
		t.Fatalf("ready runtime status = %+v", status)
	}
	probed, err := service.TestInterpreter(pythonPath)
	if err != nil || !probed.Ready || probed.PythonMajor < 3 {
		t.Fatalf("TestInterpreter = %+v, %v", probed, err)
	}
	if err := service.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := service.currentPool(context.Background()); err == nil {
		t.Fatal("currentPool succeeded after shutdown")
	}
	statusEvents := events.status.Values()
	if len(statusEvents) < 2 || statusEvents[len(statusEvents)-1].Status.Ready != true {
		t.Fatalf("status events = %#v", statusEvents)
	}
}

func TestServiceStartupWithDisabledMissingInterpreterKeepsRuntimeUnavailable(t *testing.T) {
	service, events := newServiceHarness(t)
	missingInterpreter := filepath.Join(t.TempDir(), "missing-python")
	if err := service.settings.SavePythonPluginConfig(&settingservice.PythonPluginConfig{
		Enabled: false, InterpreterPath: missingInterpreter, HookTimeoutMs: 2400,
	}); err != nil {
		t.Fatalf("SavePythonPluginConfig: %v", err)
	}

	if err := service.ServiceStartup(context.Background()); err != nil {
		t.Fatalf("ServiceStartup: %v", err)
	}
	status, err := service.GetRuntimeStatus()
	if err != nil {
		t.Fatalf("GetRuntimeStatus: %v", err)
	}
	if status.Enabled || status.Ready || status.Error != "" || status.InterpreterPath != missingInterpreter || service.pool != nil {
		t.Fatalf("disabled missing-interpreter status = %+v, pool=%v", status, service.pool)
	}

	if last, ok := events.status.Last(); !ok || last.Status.Ready {
		t.Fatalf("startup status events = %#v", events.status.Values())
	}
}

func TestServiceConfigureRuntimeFailedEnableKeepsPersistedStateDisabled(t *testing.T) {
	service, _ := newServiceHarness(t)
	notPython, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if _, err := service.ConfigureRuntime(settingservice.PythonPluginConfig{
		Enabled: true, InterpreterPath: notPython, HookTimeoutMs: 2500,
	}); err == nil {
		t.Fatal("ConfigureRuntime enabled a non-Python executable")
	}
	config, err := settingservice.GetPythonPluginConfig(service.settings)
	if err != nil {
		t.Fatalf("GetPythonPluginConfig: %v", err)
	}
	if config.Enabled || config.InterpreterPath != "" || config.HookTimeoutMs != 5000 {
		t.Fatalf("persisted config changed after failed enable: %+v", config)
	}
	if service.pool != nil {
		t.Fatal("failed enable retained a worker pool")
	}
}

func TestServiceLogEventsAreForwardedWithMonotonicIDs(t *testing.T) {
	service, events := newServiceHarness(t)
	for index := range 5 {
		pluginID := testPluginIDOne
		if index%2 == 0 {
			pluginID = testPluginIDTwo
		}
		service.handleWorkerLog(WorkerLog{
			RequestID: "request", PluginID: pluginID, Level: "debug", Stream: "context",
			Message: "entry", Timestamp: int64(index + 1),
		})
	}
	logs := events.logs.Values()
	if len(logs) != 5 {
		t.Fatalf("emitted log count = %d", len(logs))
	}
	for index := 1; index < len(logs); index++ {
		if logs[index].EventID <= logs[index-1].EventID {
			t.Fatalf("log event IDs are not monotonic at %d", index)
		}
	}
	if logs[0].Timestamp != 1 || logs[len(logs)-1].Timestamp != 5 {
		t.Fatalf("emitted log timestamps = %#v", logs)
	}
}

type capturedServiceEvents struct {
	registry *topictest.Recorder[RegistryEvent]
	status   *topictest.Recorder[StatusEvent]
	logs     *topictest.Recorder[PluginLogEntry]
}

func newServiceHarness(t *testing.T) (*PythonPluginService, *capturedServiceEvents) {
	t.Helper()
	repository := newTestRepository(t)
	settings := settingservice.New(repository.db)
	if err := settings.Load(); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	root := t.TempDir()
	service, err := newWithPaths(
		repository.db, settings, filepath.Join(root, "packages"), filepath.Join(root, "runtime"),
	)
	if err != nil {
		t.Fatalf("newWithPaths: %v", err)
	}
	t.Cleanup(func() { _ = service.Shutdown() })
	events := &capturedServiceEvents{
		registry: topictest.Record(t, &service.Events.Registry),
		status:   topictest.Record(t, &service.Events.Status),
		logs:     topictest.Record(t, &service.Events.Log),
	}
	return service, events
}
