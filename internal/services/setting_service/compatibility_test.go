package settingservice

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/josexy/flowlens/internal/pkg/database"
)

// testdata/old_build_settings.db was written by the Wails-based 1.0.5 build.
func TestSettingsDatabaseFromTheOldBuildLoadsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flowlens.db")
	source, err := os.Open(filepath.Join("testdata", "old_build_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		t.Fatal(err)
	}
	destination.Close()
	db, err := database.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	settings, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if settings.CommonConfig.Language != "en" || settings.CommonConfig.ThemeMode != "dark" {
		t.Fatalf("%+v", settings.CommonConfig)
	}
	if settings.ProxyConfig.Port != 18888 || !slices.Equal(settings.ProxyConfig.IncludeHosts, []string{"*.old.test"}) {
		t.Fatalf("%+v", settings.ProxyConfig)
	}
	if !slices.Equal(settings.TrafficTableConfig.HiddenColumns, []string{"size"}) || settings.TrafficTableConfig.ColumnOrder != nil {
		t.Fatalf("%+v", settings.TrafficTableConfig)
	}
	if settings.TrafficDetailConfig.Layout != TrafficDetailLayoutHorizontal || settings.TrafficDetailConfig.Splits != nil {
		t.Fatalf("%+v", settings.TrafficDetailConfig)
	}
}
