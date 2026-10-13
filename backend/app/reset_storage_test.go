package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/google/uuid"
	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
	appfs "github.com/josexy/flowlens/backend/pkg/fs"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func resetTestStorage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	base, err := appfs.BaseStoragePath()
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func resetTestManifest() *resetManifest {
	return &resetManifest{Version: 1, Phase: resetPending, Token: uuid.NewString(), Language: "zh"}
}

func writeResetTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("retained test content"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResetClearsManagedDataAndRecreatesFirstRunDefaults(t *testing.T) {
	base := resetTestStorage(t)
	db, err := appdatabase.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	initial, _ := settings.Get()
	defaults, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	oldCA, err := settings.GetCACertificateInfo()
	if err != nil || !oldCA.ValidPair {
		t.Fatalf("initial CA: %+v %v", oldCA, err)
	}
	initial.CommonConfig.Language = "en"
	initial.CommonConfig.ThemeMode = "dark"
	initial.WindowConfig.Width = 1500
	initial.PythonPluginConfig.InterpreterPath = "external-python"
	if err := settings.Save(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO api_nodes(id, kind, name, normalized_name, sort_order, created_at, updated_at) VALUES('folder', 'folder', 'saved', 'saved', 0, 1, 1)`,
		`INSERT INTO python_plugins(id, name, sort_order, created_at, updated_at) VALUES('plugin', 'saved', 0, 1, 1)`,
		`UPDATE rewrite_state SET enabled=1, revision=100`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"histories/unknown-version.hbin", "api_collections/files/body.bin",
		"python_plugins/plugin/main.py", "python_plugin_runtime/worker.py",
		"logs/flowlens.log", "cache/process-icons/icon.png", "request-draft-cache/body.bin",
	} {
		writeResetTestFile(t, filepath.Join(base, relative))
	}
	external := filepath.Join(filepath.Dir(base), "external", "certificate.crt")
	writeResetTestFile(t, external)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	// Scheduling alone leaves the current process's files intact. A manual
	// launch loads the durable intent and cleans before opening SQLite.
	if _, err := os.Stat(filepath.Join(base, "histories", "unknown-version.hbin")); err != nil {
		t.Fatalf("scheduling reset deleted live data: %v", err)
	}
	pending, err := loadResetManifest()
	if err != nil || pending == nil || pending.Phase != resetPending {
		t.Fatalf("pending manual reset: %+v %v", pending, err)
	}
	if err := cleanResetData(pending, os.RemoveAll, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed storage remains: %v", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external file was deleted: %v", err)
	}
	marker, err := loadResetManifest()
	if err != nil || marker.Phase != resetCleaned {
		t.Fatalf("completion marker: %+v %v", marker, err)
	}
	newDB, err := appdatabase.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer newDB.Close()
	newSettings := settingservice.New(newDB)
	if err := newSettings.Load(); err != nil {
		t.Fatal(err)
	}
	current, _ := newSettings.Get()
	got, _ := json.Marshal(current)
	if string(got) != string(defaults) {
		t.Fatalf("reset did not recreate default settings:\ngot %s\nwant %s", got, defaults)
	}
	for _, table := range []string{"api_nodes", "python_plugins", "python_plugin_rules", "rewrite_rules", "api_request_payloads"} {
		var count int
		if err := newDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("table %s not empty: %d %v", table, count, err)
		}
	}
	if err := newSettings.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	newCA, err := newSettings.GetCACertificateInfo()
	if err != nil || !newCA.ValidPair || newCA.SHA256Fingerprint == oldCA.SHA256Fingerprint {
		t.Fatalf("CA was not replaced: %+v %v", newCA, err)
	}
	// A cleaned marker must not erase newly initialised defaults on a retry.
	if err := cleanResetData(marker, func(string) error { t.Fatal("cleaned reset deleted new data"); return nil }, 1, 0); err != nil {
		t.Fatal(err)
	}
}

func TestResetFailureRetainsIntentAndRetryCompletes(t *testing.T) {
	base := resetTestStorage(t)
	writeResetTestFile(t, filepath.Join(base, "flowlens.db"))
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	failure := &os.PathError{Op: "remove", Path: base, Err: syscall.EBUSY}
	calls := 0
	err := cleanResetData(manifest, func(string) error { calls++; return failure }, 3, 0)
	if !errors.Is(err, failure) || calls != 3 {
		t.Fatalf("bounded retry: %d %v", calls, err)
	}
	pending, err := loadResetManifest()
	if err != nil || pending.Phase != resetPending {
		t.Fatalf("lost reset intent: %+v %v", pending, err)
	}
	if err := cleanResetData(pending, os.RemoveAll, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := clearResetMarker(); err != nil {
		t.Fatal(err)
	}
	if marker, err := loadResetManifest(); err != nil || marker != nil {
		t.Fatalf("marker not removed: %+v %v", marker, err)
	}
}

func TestUncommittedResetNeverDeletesFiles(t *testing.T) {
	resetTestStorage(t)
	manifest := resetTestManifest()
	manifest.Phase = resetPrepared
	if err := cleanResetData(manifest, func(string) error { t.Fatal("deleted uncommitted data"); return nil }, 1, 0); err == nil {
		t.Fatal("uncommitted reset accepted")
	}
}

func TestResetIgnoresUnusedMarkerMetadata(t *testing.T) {
	base := resetTestStorage(t)
	writeResetTestFile(t, filepath.Join(base, "flowlens.db"))
	external := filepath.Join(filepath.Dir(base), "external", "data.txt")
	writeResetTestFile(t, external)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	root, err := resetDirectory()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unusedMetadata"] = filepath.Dir(external)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pending, err := loadResetManifest()
	if err != nil || !reflect.DeepEqual(pending, manifest) {
		t.Fatalf("unused metadata changed the reset: %+v %v", pending, err)
	}
	if err := cleanResetData(pending, os.RemoveAll, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed storage remains: %v", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("unused metadata authorised deleting external data: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields = nil
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["unusedMetadata"]; ok {
		t.Fatal("unused metadata was persisted after cleanup")
	}
}

func TestResetRejectsUnsafeMarkerPaths(t *testing.T) {
	resetTestStorage(t)
	for _, mutate := range []func(*resetManifest){
		func(m *resetManifest) { m.UpdateStaging = []string{os.TempDir()} },
		func(m *resetManifest) { m.UpdateStaging = []string{filepath.Join(t.TempDir(), "wails-update-fake")} },
		func(m *resetManifest) { m.Token = "../../external" },
		func(m *resetManifest) { m.Version = 2 },
	} {
		manifest := resetTestManifest()
		mutate(manifest)
		if err := saveResetManifest(manifest); err == nil {
			t.Fatalf("unsafe marker accepted: %+v", manifest)
		}
	}
}

func TestResetTracksOnlyKnownUpdateStaging(t *testing.T) {
	root := filepath.Join(os.TempDir(), "wails-update-known")
	want := []string{root}
	if got := resetUpdateStaging(filepath.Join(root, "FlowLens.app", "Contents", "MacOS", "flowlens")); !reflect.DeepEqual(got, want) {
		t.Fatalf("staged app: %v", got)
	}
	if got := resetUpdateStaging(filepath.Join(t.TempDir(), "external", "flowlens.exe")); got != nil {
		t.Fatalf("external artifact retained as cleanup target: %v", got)
	}
}

func TestResetRejectsLinkedStorageRoot(t *testing.T) {
	base := resetTestStorage(t)
	external := t.TempDir()
	file := filepath.Join(external, "external.txt")
	writeResetTestFile(t, file)
	if err := os.Symlink(external, base); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	manifest := resetTestManifest()
	if err := cleanResetData(manifest, os.RemoveAll, 1, 0); err == nil {
		t.Fatal("linked storage root accepted")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("external linked data was deleted")
	}
}
