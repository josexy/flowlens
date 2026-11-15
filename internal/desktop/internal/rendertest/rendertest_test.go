package rendertest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func view(c *ui.Context) {
	ui.Box(c).Fill().Background(ui.RGB(30, 60, 90)).Children(func() { ui.Text(c, "render") })
}

func TestShootIsOffWithoutDirectory(t *testing.T) {
	t.Setenv(EnvDir, "")
	tester := ui.NewTester(view, 200, 100)
	if path := Shoot(t, tester, "off"); path != "" {
		t.Fatalf("path = %q, want none", path)
	}
	if paths := ShootThemes(t, tester, "off", func(bool) { t.Fatal("setDark called while off") }); paths != nil {
		t.Fatalf("paths = %v", paths)
	}
}

func TestShootThemesWritesTwoImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	tester := ui.NewTester(view, 200, 100)
	var modes []bool
	paths := ShootThemes(t, tester, "probe/view", func(dark bool) { modes = append(modes, dark) })
	if len(paths) != 2 || filepath.Base(paths[0]) != "probe-view-light.png" || filepath.Base(paths[1]) != "probe-view-dark.png" {
		t.Fatalf("paths = %v", paths)
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if len(modes) != 3 || modes[0] || !modes[1] || modes[2] {
		t.Fatalf("modes = %v, want light, dark, then back to light", modes)
	}
}

func TestTrafficFixtures(t *testing.T) {
	entries := Traffic()
	methods := map[string]bool{}
	for _, entry := range entries {
		methods[entry.Method] = true
		if entry.Metadata == nil || entry.Metadata.Process == nil || entry.Response == nil {
			t.Fatalf("entry %d lacks metadata, process, or response", entry.ID)
		}
	}
	for _, method := range []string{"GET", "POST", "OPTIONS", "CONNECT", "PUT", "DELETE"} {
		if !methods[method] {
			t.Fatalf("no %s fixture", method)
		}
	}
	found := false
	for _, entry := range entries {
		found = found || entry.ID == Inspected
	}
	if !found {
		t.Fatalf("inspected fixture %d missing", Inspected)
	}
}
