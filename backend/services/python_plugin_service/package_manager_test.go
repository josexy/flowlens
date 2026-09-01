package pythonpluginservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type recordingRevisionValidator struct {
	mu       sync.Mutex
	requests []RevisionValidationRequest
	err      error
}

type revisionValidatorFunc func(context.Context, RevisionValidationRequest) error

func (f revisionValidatorFunc) ValidateRevision(
	ctx context.Context,
	request RevisionValidationRequest,
) error {
	return f(ctx, request)
}

func (v *recordingRevisionValidator) ValidateRevision(_ context.Context, request RevisionValidationRequest) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.requests = append(v.requests, request)
	if _, err := os.Stat(filepath.Join(request.Path, mainFileName)); err != nil {
		return err
	}
	return v.err
}

func TestInlineRevisionIsValidatedCorrelatedAndRemovedOnRelease(t *testing.T) {
	validator := &recordingRevisionValidator{}
	manager, _, _, _ := newTestPackageManager(t, validator)
	source := `from flowlens import *

def onRequest(context, request):
    return request

def onResponse(context, response):
    return response
`
	revision, lease, err := manager.createInlineRevision(context.Background(), "execution-one", source)
	if err != nil {
		t.Fatalf("createInlineRevision: %v", err)
	}
	if !isRevisionName(revision) || lease == nil || lease.Path == "" {
		t.Fatalf("revision=%q lease=%+v", revision, lease)
	}
	content, err := os.ReadFile(filepath.Join(lease.Path, mainFileName))
	if err != nil || string(content) != source {
		t.Fatalf("inline source=%q err=%v", content, err)
	}
	validator.mu.Lock()
	requests := append([]RevisionValidationRequest(nil), validator.requests...)
	validator.mu.Unlock()
	if len(requests) != 1 || requests[0].ExecutionID != "execution-one" || requests[0].PluginID != inlineHTTPRequestPluginID || requests[0].Revision != revision {
		t.Fatalf("validation requests = %+v", requests)
	}

	secondRevision, secondLease, err := manager.createInlineRevision(context.Background(), "execution-two", source)
	if err != nil {
		t.Fatalf("create second inline revision: %v", err)
	}
	if secondRevision == revision || secondLease.Path == lease.Path {
		t.Fatalf("inline revisions were reused: first=%q second=%q", revision, secondRevision)
	}
	secondLease.Release()
	if _, err := os.Stat(secondLease.Path); !os.IsNotExist(err) {
		t.Fatalf("second inline directory still exists: %v", err)
	}

	lease.Release()
	lease.Release()
	if _, err := os.Stat(lease.Path); !os.IsNotExist(err) {
		t.Fatalf("inline directory still exists: %v", err)
	}
}

func TestInlineRevisionValidationErrorExposesRedactedDiagnosticDetail(t *testing.T) {
	var revisionPath string
	validator := revisionValidatorFunc(func(_ context.Context, request RevisionValidationRequest) error {
		revisionPath = request.Path
		return &PythonExecutionError{
			Code:      "validation_failed",
			Message:   "invalid syntax",
			Traceback: "Traceback (most recent call last):\n  File \"" + filepath.Join(request.Path, mainFileName) + "\", line 1\nSyntaxError: invalid syntax",
		}
	})
	manager, _, _, _ := newTestPackageManager(t, validator)

	_, lease, err := manager.createInlineRevision(context.Background(), "execution-one", "broken(")
	if err == nil || lease != nil {
		t.Fatalf("createInlineRevision lease=%+v err=%v", lease, err)
	}
	var detailer interface{ DiagnosticDetail() string }
	if !errors.As(err, &detailer) {
		t.Fatalf("validation error does not expose diagnostic detail: %T %v", err, err)
	}
	detail := detailer.DiagnosticDetail()
	if strings.Contains(detail, revisionPath) || !strings.Contains(detail, filepath.Join("<plugin>", mainFileName)) {
		t.Fatalf("diagnostic path was not redacted: %q", detail)
	}
	if !strings.Contains(detail, "\nSyntaxError: invalid syntax") {
		t.Fatalf("diagnostic detail is incomplete: %q", detail)
	}
}

func TestInlineRevisionRejectsInvalidSourceWithoutLeavingFiles(t *testing.T) {
	validator := &recordingRevisionValidator{}
	manager, _, _, runtimeRoot := newTestPackageManager(t, validator)
	for name, source := range map[string]string{
		"blank":     " \n\t",
		"oversized": strings.Repeat("x", maxInlinePythonScriptBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := manager.createInlineRevision(context.Background(), "execution", source); err == nil {
				t.Fatal("invalid inline source was accepted")
			}
		})
	}
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		t.Fatalf("read runtime root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid inline source left runtime entries: %+v", entries)
	}
}

func TestManifestValidationAndDefaultPackage(t *testing.T) {
	manager, repository, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	plugin, err := manager.createPlugin(ctx, CreatePluginInput{
		ID: testPluginIDOne, Name: "Example", Description: "description", ParamsJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if plugin.Enabled || plugin.ValidationStatus != ValidationStatusUnavailable {
		t.Fatalf("new plugin state = %+v", plugin)
	}
	files, err := manager.listFiles(ctx, plugin.ID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if want := []string{helpersFileName, mainFileName, manifestFileName}; !reflect.DeepEqual(files, want) {
		t.Fatalf("default package files = %#v, want %#v", files, want)
	}
	manifestBytes, err := manager.readFile(ctx, plugin.ID, manifestFileName)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest, err := parseManifest(manifestBytes, plugin.ID)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.APIVersion != 1 || manifest.Name != "Example" {
		t.Fatalf("manifest = %+v", manifest)
	}
	mainSource, err := manager.readFile(ctx, plugin.ID, mainFileName)
	if err != nil {
		t.Fatalf("read main.py: %v", err)
	}
	if !strings.Contains(string(mainSource), "from flowlens import *") || !strings.Contains(string(mainSource), "def onRequest") || !strings.Contains(string(mainSource), "def onResponse") {
		t.Fatalf("unexpected default main.py:\n%s", mainSource)
	}
	if _, err := manager.activateCurrent(ctx, plugin.ID); err != nil {
		t.Fatalf("activate current package: %v", err)
	}
	stored, err := repository.getPlugin(ctx, plugin.ID)
	if err != nil {
		t.Fatalf("get activated plugin: %v", err)
	}
	if stored.ActiveRevision == "" || stored.LastGoodRevision != stored.ActiveRevision || stored.ValidationStatus != ValidationStatusValid {
		t.Fatalf("activated plugin = %+v", stored)
	}
	if _, err := os.Stat(manager.revisionPath(plugin.ID, stored.ActiveRevision)); err != nil {
		t.Fatalf("stat revision: %v", err)
	}
}

func TestManifestRejectsVersionIdentityAndShapeErrors(t *testing.T) {
	valid := []byte(`{"schemaVersion":1,"apiVersion":1,"id":"` + testPluginIDOne + `","name":"Plugin","description":""}`)
	if _, err := parseManifest(valid, testPluginIDOne); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	for name, value := range map[string][]byte{
		"invalid JSON":       []byte(`{"schemaVersion":`),
		"wrong schema":       []byte(`{"schemaVersion":2,"apiVersion":1,"id":"` + testPluginIDOne + `","name":"Plugin"}`),
		"wrong API":          []byte(`{"schemaVersion":1,"apiVersion":2,"id":"` + testPluginIDOne + `","name":"Plugin"}`),
		"wrong ID":           []byte(`{"schemaVersion":1,"apiVersion":1,"id":"` + testPluginIDTwo + `","name":"Plugin"}`),
		"non-canonical UUID": []byte(`{"schemaVersion":1,"apiVersion":1,"id":"NOT-A-UUID","name":"Plugin"}`),
		"empty name":         []byte(`{"schemaVersion":1,"apiVersion":1,"id":"` + testPluginIDOne + `","name":" "}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseManifest(value, testPluginIDOne); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
}

func TestPackageFileOperationsRejectEscapeAndSymlink(t *testing.T) {
	manager, _, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	for _, invalid := range []string{"", ".", "../outside.py", `..\outside.py`, "/absolute.py", `C:\absolute.py`, ".staging/file.py"} {
		if _, err := manager.readFile(ctx, testPluginIDOne, invalid); err == nil {
			t.Fatalf("unsafe path %q was accepted", invalid)
		}
	}

	outside := filepath.Join(t.TempDir(), "outside.py")
	if err := os.WriteFile(outside, []byte("value = 1\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(manager.packagePath(testPluginIDOne), "linked.py")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if _, err := manager.listFiles(ctx, testPluginIDOne); err == nil {
		t.Fatal("package containing a symlink was accepted")
	}
	if _, err := manager.activateCurrent(ctx, testPluginIDOne); err == nil {
		t.Fatal("package containing a symlink was activated")
	}
}

func TestPackageSaveActivatesAtomicallyAndKeepsLastGoodOnFailure(t *testing.T) {
	validator := &recordingRevisionValidator{}
	manager, repository, _, _ := newTestPackageManager(t, validator)
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	initial, err := manager.activateCurrent(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("activate initial revision: %v", err)
	}

	updatedSource := []byte("from flowlens import *\n\ndef onRequest(context, request):\n    request.headers.add('X-Test', 'one')\n    return request\n\ndef onResponse(context, response):\n    return response\n")
	updated, err := manager.writeFile(ctx, testPluginIDOne, mainFileName, updatedSource)
	if err != nil {
		t.Fatalf("write valid main.py: %v", err)
	}
	if updated.ActiveRevision == initial.ActiveRevision {
		t.Fatal("source change did not create a new revision")
	}
	storedSource, err := manager.readFile(ctx, testPluginIDOne, mainFileName)
	if err != nil || string(storedSource) != string(updatedSource) {
		t.Fatalf("stored source = %q, err=%v", storedSource, err)
	}

	validator.mu.Lock()
	validator.err = errors.New("candidate import failed")
	validator.mu.Unlock()
	failedSource := []byte("raise RuntimeError('broken')\n")
	if _, err := manager.writeFile(ctx, testPluginIDOne, mainFileName, failedSource); err == nil {
		t.Fatal("invalid candidate save unexpectedly succeeded")
	}
	storedSource, err = manager.readFile(ctx, testPluginIDOne, mainFileName)
	if err != nil || string(storedSource) != string(updatedSource) {
		t.Fatalf("failed save replaced managed source: %q, err=%v", storedSource, err)
	}
	stored, err := repository.getPlugin(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("get plugin after failed save: %v", err)
	}
	if stored.ActiveRevision != updated.ActiveRevision || stored.LastGoodRevision != updated.ActiveRevision || stored.ValidationStatus != ValidationStatusInvalid {
		t.Fatalf("failed save changed last-good revision: %+v", stored)
	}
}

func TestPackageRenameDeleteAndManifestUpdate(t *testing.T) {
	manager, _, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if _, err := manager.activateCurrent(ctx, testPluginIDOne); err != nil {
		t.Fatalf("activate plugin: %v", err)
	}
	if _, err := manager.writeFile(ctx, testPluginIDOne, "tools.py", []byte("VALUE = 1\n")); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}
	if _, err := manager.renameFile(ctx, testPluginIDOne, "tools.py", "lib/tools.py"); err != nil {
		t.Fatalf("rename tools.py: %v", err)
	}
	if _, err := manager.readFile(ctx, testPluginIDOne, "tools.py"); err == nil {
		t.Fatal("old filename still exists")
	}
	if value, err := manager.readFile(ctx, testPluginIDOne, "lib/tools.py"); err != nil || string(value) != "VALUE = 1\n" {
		t.Fatalf("renamed file = %q, err=%v", value, err)
	}
	if _, err := manager.deleteFile(ctx, testPluginIDOne, "lib/tools.py"); err != nil {
		t.Fatalf("delete tools.py: %v", err)
	}
	if _, err := manager.deleteFile(ctx, testPluginIDOne, mainFileName); err == nil {
		t.Fatal("deleting main.py unexpectedly succeeded")
	}

	manifest := Manifest{SchemaVersion: 1, APIVersion: 1, ID: testPluginIDOne, Name: "Renamed", Description: "new"}
	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	plugin, err := manager.writeFile(ctx, testPluginIDOne, manifestFileName, manifestBytes)
	if err != nil {
		t.Fatalf("update manifest: %v", err)
	}
	if plugin.Name != "Renamed" || plugin.Description != "new" {
		t.Fatalf("manifest metadata was not persisted: %+v", plugin)
	}
}

func TestRevisionLeaseRetainsOldSnapshotUntilRelease(t *testing.T) {
	manager, _, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	initial, err := manager.activateCurrent(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("activate initial: %v", err)
	}
	lease, err := manager.acquireRevision(testPluginIDOne, initial.ActiveRevision)
	if err != nil {
		t.Fatalf("acquire revision: %v", err)
	}
	updated, err := manager.writeFile(ctx, testPluginIDOne, helpersFileName, []byte("VALUE = 2\n"))
	if err != nil {
		t.Fatalf("activate updated revision: %v", err)
	}
	if updated.ActiveRevision == initial.ActiveRevision {
		t.Fatal("updated revision did not change")
	}
	oldPath := manager.revisionPath(testPluginIDOne, initial.ActiveRevision)
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("leased old revision was removed: %v", err)
	}
	lease.Release()
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("released stale revision still exists: %v", err)
	}
}

func TestPackageHashIgnoresRuntimeAndEditorTemporaryFiles(t *testing.T) {
	manager, _, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	first, err := manager.activateCurrent(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("activate initial package: %v", err)
	}
	packagePath := manager.packagePath(testPluginIDOne)
	for name, value := range map[string]string{
		"main.py~":                 "temporary",
		"scratch.swp":              "temporary",
		"__pycache__/main.pyc":     "bytecode",
		"nested/__pycache__/x.pyc": "bytecode",
	} {
		path := filepath.Join(packagePath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create temporary parent: %v", err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatalf("write temporary file: %v", err)
		}
	}
	second, err := manager.activateCurrent(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("activate package with temporary files: %v", err)
	}
	if second.ActiveRevision != first.ActiveRevision {
		t.Fatalf("temporary files changed revision: first=%s second=%s", first.ActiveRevision, second.ActiveRevision)
	}
}

func TestDeleteRestoresPackageAndEnabledStateWhenDatabaseCommitFails(t *testing.T) {
	manager, repository, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := repository.setPluginEnabled(ctx, testPluginIDOne, true); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := repository.db.Exec(`
		CREATE TRIGGER fail_python_plugin_delete
		BEFORE DELETE ON python_plugins
		BEGIN
			SELECT RAISE(ABORT, 'injected delete failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if err := manager.deletePlugin(ctx, testPluginIDOne); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if _, err := os.Stat(manager.packagePath(testPluginIDOne)); err != nil {
		t.Fatalf("package was not restored: %v", err)
	}
	plugin, err := repository.getPlugin(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("get restored plugin: %v", err)
	}
	if !plugin.Enabled {
		t.Fatal("enabled state was not restored")
	}
}

func TestStartupReconciliationCleansStagingRestoresTrashAndQuarantinesOrphans(t *testing.T) {
	manager, repository, packagesRoot, runtimeRoot := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	plugin, err := manager.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	plugin, err = manager.activateCurrent(ctx, plugin.ID)
	if err != nil {
		t.Fatalf("activate plugin: %v", err)
	}
	staleStage := filepath.Join(packagesRoot, stagingDirectoryName, "stale")
	if err := os.MkdirAll(staleStage, 0o755); err != nil {
		t.Fatalf("create stale stage: %v", err)
	}
	orphanID := testPluginIDTwo
	if err := os.MkdirAll(filepath.Join(packagesRoot, orphanID), 0o755); err != nil {
		t.Fatalf("create orphan package: %v", err)
	}
	trashEntry := manager.trashPath(plugin.ID)
	if err := os.MkdirAll(filepath.Dir(trashEntry), 0o755); err != nil {
		t.Fatalf("create trash root: %v", err)
	}
	if err := os.Rename(manager.packagePath(plugin.ID), trashEntry); err != nil {
		t.Fatalf("simulate interrupted delete: %v", err)
	}
	staleRevision := filepath.Join(runtimeRoot, plugin.ID, strings.Repeat("a", 64))
	if err := os.MkdirAll(staleRevision, 0o755); err != nil {
		t.Fatalf("create stale revision: %v", err)
	}

	if err := manager.reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := os.Stat(staleStage); !os.IsNotExist(err) {
		t.Fatalf("stale staging path still exists: %v", err)
	}
	if _, err := os.Stat(manager.packagePath(plugin.ID)); err != nil {
		t.Fatalf("registered package was not restored from trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packagesRoot, orphanID)); !os.IsNotExist(err) {
		t.Fatalf("orphan package was not moved: %v", err)
	}
	quarantined, err := filepath.Glob(filepath.Join(packagesRoot, quarantineDirectoryName, orphanID+"--*"))
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("quarantined orphan paths = %#v, err=%v", quarantined, err)
	}
	if _, err := os.Stat(staleRevision); !os.IsNotExist(err) {
		t.Fatalf("stale revision still exists: %v", err)
	}
	stored, err := repository.getPlugin(ctx, plugin.ID)
	if err != nil || stored.ValidationStatus != ValidationStatusValid {
		t.Fatalf("restored plugin = %+v, err=%v", stored, err)
	}
}

func TestStartupReconciliationMarksMissingRegisteredPackageUnavailable(t *testing.T) {
	manager, repository, _, _ := newTestPackageManager(t, &recordingRevisionValidator{})
	ctx := context.Background()
	if _, err := repository.createPlugin(ctx, CreatePluginInput{ID: testPluginIDOne, Name: "Missing", ParamsJSON: `{}`}); err != nil {
		t.Fatalf("create registry row: %v", err)
	}
	if err := repository.setPluginEnabled(ctx, testPluginIDOne, true); err != nil {
		t.Fatalf("enable registry row: %v", err)
	}
	if err := manager.reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	plugin, err := repository.getPlugin(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("get plugin: %v", err)
	}
	if plugin.Enabled || plugin.ValidationStatus != ValidationStatusUnavailable || plugin.ValidationError == "" {
		t.Fatalf("missing plugin reconciliation state = %+v", plugin)
	}
}

func newTestPackageManager(t *testing.T, validator RevisionValidator) (*packageManager, *repository, string, string) {
	t.Helper()
	repository := newTestRepository(t)
	root := t.TempDir()
	packagesRoot := filepath.Join(root, "python_plugins")
	runtimeRoot := filepath.Join(root, "python_plugin_runtime")
	manager, err := newPackageManager(repository, packagesRoot, runtimeRoot, validator)
	if err != nil {
		t.Fatalf("newPackageManager: %v", err)
	}
	return manager, repository, packagesRoot, runtimeRoot
}
