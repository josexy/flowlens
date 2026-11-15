package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/shell"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/pkg/database"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

// The workbench builds against real settings without a desktop session.
func TestWorkbenchBuilds(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := settingservice.New(db)
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	data := store.New(store.NewRuntime(), store.Dependencies{Settings: store.SettingsDependencies{Backend: service}})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	tester := ui.NewTester(shell.New(data, shell.Host{}).View, 1280, 800)
	if len(tester.Texts()) == 0 {
		t.Fatal("workbench drew no text")
	}
}
