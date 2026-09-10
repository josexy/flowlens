//go:build windows

package pythonpluginservice

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"golang.org/x/sys/windows"
)

func TestParsePythonManagerPaths(t *testing.T) {
	output := `{"versions":[
		{"executable":"C:\\Python313\\python.exe"},
		{"executable":"C:\\Users\\Test\\Microsoft\\WindowsApps\\PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0\\python.exe"},
		{"executable":"relative\\python.exe"}, {"executable":""}, {}
	]}`
	want := []string{`C:\Python313\python.exe`, `C:\Users\Test\Microsoft\WindowsApps\PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0\python.exe`}
	if got := parsePythonManagerPaths(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("manager paths = %#v, want %#v", got, want)
	}
	for _, output := range []string{"", `{"versions":[`, `{"versions":[{"executable":42}]}`, "No runtimes installed"} {
		if got := parsePythonManagerPaths(output); len(got) != 0 {
			t.Fatalf("invalid manager output %q produced %#v", output, got)
		}
	}
	versions := make([]map[string]string, maxInterpreterDiscoveryPaths+1)
	for i := range versions {
		versions[i] = map[string]string{"executable": `C:\Python\python.exe`}
	}
	large, err := json.Marshal(map[string]any{"versions": versions})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(parsePythonManagerPaths(string(large))); got != maxInterpreterDiscoveryPaths {
		t.Fatalf("manager result count = %d", got)
	}
}

func TestWindowsStoreDiscoveryExcludesGlobalAndUnregisteredAliases(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root := windowsAppAliasesRoot()
	unregistered := filepath.Join(root, "PythonSoftwareFoundation.Python.3.999_qbz5n2kfra8p0", "python.exe")
	if err := os.MkdirAll(filepath.Dir(unregistered), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unregistered, []byte("not a Python package"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "python.exe"),
		filepath.Join(root, "python3.exe"),
		filepath.Join(root, "PythonSoftwareFoundation.PythonManager_3847v3x7pw1km", "python.exe"),
		filepath.Join(root, "PythonSoftwareFoundation.Python.3.13_wrongpublisher", "python.exe"),
		filepath.Join(root, "PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0", "pythonw.exe"),
		unregistered,
	} {
		if !shouldSkipInterpreterDiscoveryPath(path) {
			t.Errorf("automatic discovery accepted %q", path)
		}
	}
	if paths := windowsStoreInterpreterPaths(context.Background()); len(paths) != 0 {
		t.Fatalf("unregistered directory produced candidates: %#v", paths)
	}
	if shouldSkipInterpreterDiscoveryPath(`C:\Python313\python.exe`) {
		t.Fatal("ordinary installation was excluded")
	}
	// Package aliases are allowed syntactically, including Windows path casing.
	path := filepath.Join(root, "PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0", "python3.13.exe")
	if got := windowsStorePythonAliasFamily(strings.ToUpper(path)); !strings.EqualFold(got, filepath.Base(filepath.Dir(path))) {
		t.Fatalf("package family for %q = %q", path, got)
	}
}

func TestWindowsInterpreterValidationPreservesFileAndSymlinkRules(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "python.exe")
	if err := os.WriteFile(file, []byte("executable validation still requires a runtime probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateInterpreterPath(file); err != nil {
		t.Fatalf("regular file: %v", err)
	}
	for _, path := range []string{file, root, filepath.Join(root, "missing.exe")} {
		if isWindowsAppExecutionAlias(path) {
			t.Errorf("ordinary path identified as AppExecLink: %q", path)
		}
	}
	for _, path := range []string{root, filepath.Join(root, "missing.exe")} {
		if _, err := validateInterpreterPath(path); err == nil {
			t.Errorf("invalid interpreter accepted: %q", path)
		}
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(root, "linked-python.exe")
		if err := os.Symlink(file, link); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		if isWindowsAppExecutionAlias(link) {
			t.Fatal("symlink incorrectly identified as AppExecLink")
		}
		if _, err := validateInterpreterPath(link); err != nil {
			t.Fatalf("symlink to regular interpreter: %v", err)
		}
	})
}

func TestWindowsInterpreterCommandsDisableAutomaticInstall(t *testing.T) {
	t.Setenv("PYTHON_MANAGER_AUTOMATIC_INSTALL", "true")
	python := requirePython311(t)
	output, stderr, err := runInterpreterDiscoveryCommand(context.Background(), 2*time.Second, python,
		"-I", "-c", `import os; print(os.environ.get("PYTHON_MANAGER_AUTOMATIC_INSTALL"))`)
	if err != nil || strings.TrimSpace(output) != "false" {
		t.Fatalf("automatic install guard = %q, %q, %v", output, stderr, err)
	}
	command := exec.Command(python)
	command.Env = append(os.Environ(), "PYTHON_MANAGER_AUTOMATIC_INSTALL=true")
	configureWorkerCommand(command)
	for _, value := range command.Environ() {
		if strings.HasPrefix(strings.ToUpper(value), "PYTHON_MANAGER_AUTOMATIC_INSTALL=") && value != "PYTHON_MANAGER_AUTOMATIC_INSTALL=false" {
			t.Fatalf("worker environment retained automatic installation: %q", value)
		}
	}
}

func TestWindowsStorePythonDiscoveryTestEnableAndInvoke(t *testing.T) {
	root := windowsAppAliasesRoot()
	if root == "" {
		t.Skip("Windows app aliases directory unavailable")
	}
	directories, _ := filepath.Glob(filepath.Join(root, "PythonSoftwareFoundation.Python.3.*_qbz5n2kfra8p0"))
	var alias string
	for _, directory := range directories {
		family := filepath.Base(directory)
		minor, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(family, "PythonSoftwareFoundation.Python.3."), "_qbz5n2kfra8p0"))
		if minor >= 11 && isStorePythonFamily(family) && isWindowsPackageRegistered(family) {
			alias = filepath.Join(directory, "python.exe")
			break
		}
	}
	if alias == "" {
		t.Skip("Microsoft Store Python is not installed")
	}
	if !isWindowsAppExecutionAlias(alias) {
		t.Fatalf("installed Store Python is not an AppExecLink: %s", alias)
	}
	service, _ := newServiceHarness(t)
	candidates, err := service.DiscoverInterpreters("")
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, candidate := range candidates {
		if interpreterPathKey(candidate.InterpreterPath) == interpreterPathKey(alias) {
			matches++
			t.Logf("Detected Store %s %d.%d.%d: %s", candidate.Implementation, candidate.PythonMajor,
				candidate.PythonMinor, candidate.PythonPatch, candidate.InterpreterPath)
		}
	}
	if matches != 1 {
		t.Fatalf("Store alias occurs %d times in candidates: %+v", matches, candidates)
	}
	status, err := service.TestInterpreter(alias)
	if err != nil || !status.Ready {
		t.Fatalf("TestInterpreter = %+v, %v", status, err)
	}
	status, err = service.ConfigureRuntime(settingservice.PythonPluginConfig{
		Enabled: true, InterpreterPath: alias, HookTimeoutMs: 5000,
	})
	if err != nil || !status.Ready || !status.Enabled {
		t.Fatalf("ConfigureRuntime = %+v, %v", status, err)
	}
	plugin, err := service.CreatePlugin(CreatePluginInput{ID: testPluginIDOne, Name: "Store runtime", ParamsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	plugin = writeAndActivatePlugin(t, service.packages, plugin.ID, integrationPluginSource)
	pool, err := service.currentPool(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: service.packages.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
		Context: integrationContext(map[string]any{"header": "store-python"}, map[string]any{}),
		Value: map[string]any{"method": "GET", "url": "https://example.com/",
			"headers": []map[string]string{}, "body": map[string]any{"kind": "none", "value": nil}},
	})
	if err != nil || !strings.Contains(string(result.Value), "store-python") {
		t.Fatalf("Store plugin invocation = %s, %v", result.Value, err)
	}
}

func TestWindowsPythonManagerListsInstalledInterpreters(t *testing.T) {
	root := windowsAppAliasesRoot()
	if root == "" {
		t.Skip("Windows app aliases directory unavailable")
	}
	directories, _ := filepath.Glob(filepath.Join(root, "PythonSoftwareFoundation.PythonManager_*"))
	for _, directory := range directories {
		if !isPythonManagerFamily(filepath.Base(directory)) || !isWindowsPackageRegistered(filepath.Base(directory)) {
			continue
		}
		manager := filepath.Join(directory, "pymanager.exe")
		output, stderr, err := runInterpreterDiscoveryCommand(context.Background(), 2*time.Second, manager, "list", "--format=json")
		if err != nil {
			t.Fatalf("installed manager list failed: %v: %s", err, stderr)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(output), &payload); err != nil || payload["versions"] == nil {
			t.Fatalf("manager returned invalid listing: %q, %v", output, err)
		}
		paths := parsePythonManagerPaths(output)
		t.Logf("Installed Python manager listed %d runtime paths", len(paths))
		for _, path := range paths {
			if interpreterPathKey(path) == interpreterPathKey(manager) {
				t.Fatal("manager was listed as an interpreter")
			}
		}
		return
	}
	t.Skip("Python Install Manager is not installed")
}

func TestWindowsInterpreterDiscoveryCancellationTerminatesChildren(t *testing.T) {
	python := requirePython311(t)
	source := `import subprocess, sys, time
p = subprocess.Popen([sys.executable, "-I", "-c", "import time; time.sleep(30)"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
print(p.pid, flush=True)
time.sleep(30)`
	started := time.Now()
	output, stderr, err := runInterpreterDiscoveryCommand(context.Background(), time.Second, python, "-I", "-c", source)
	if err == nil || time.Since(started) > 4*time.Second {
		t.Fatalf("discovery cancellation: output=%q stderr=%q err=%v elapsed=%s", output, stderr, err, time.Since(started))
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(output))
	if parseErr != nil {
		t.Fatalf("probe did not report its child PID: %q, %v", output, parseErr)
	}
	process, openErr := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if openErr == windows.ERROR_INVALID_PARAMETER {
		return // The terminated child has already been reaped.
	}
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer windows.CloseHandle(process)
	defer windows.TerminateProcess(process, 1)
	if state, waitErr := windows.WaitForSingleObject(process, 1000); waitErr != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("discovery child survived cancellation: state=%d err=%v", state, waitErr)
	}
}
