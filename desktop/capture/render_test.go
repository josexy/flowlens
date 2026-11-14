package capture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/josexy/flowlens/desktop/inspect"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/theme"
)

// newRenderFixture shows the reference traffic at the reference size, with
// its highlights and the inspected row open in the detail panel.
func newRenderFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixtureSized(t, rendertest.Traffic(), rendertest.Width, rendertest.Height)
	for id, color := range rendertest.Highlighted {
		f.data.Traffic.SetHighlight(id, color)
	}
	for _, entry := range f.data.Traffic.Entries {
		if entry.ID == rendertest.Inspected {
			f.data.Traffic.Select(entry)
			f.data.Traffic.SetShowDetailPanel(true)
		}
	}
	f.settle()
	return f
}

// setDark switches the fixture's theme mode.
func (f *fixture) setDark(dark bool) {
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := f.data.Settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
}

func TestRenderCaptureWorkspace(t *testing.T) {
	dir := t.TempDir()
	if configured := os.Getenv(rendertest.EnvDir); configured != "" {
		dir = configured
	}
	t.Setenv(rendertest.EnvDir, dir)
	f := newRenderFixture(t)
	paths := rendertest.ShootThemes(t, f.tester, "capture-workspace", f.setDark)
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 || filepath.Dir(path) != dir {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

// TestRenderCaptureWithDetail renders the table with the inspected row
// selected and its detail panel open beside it, after an explicit open action.
func TestRenderCaptureWithDetail(t *testing.T) {
	f := newRenderFixture(t)
	panel := inspect.NewPanel(inspect.LiveData{Traffic: f.data.Traffic}, inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier})
	f.workspace.Detail = panel.Build
	table := f.workspace.Table
	table.replaceSelection(table.rowIndex(rendertest.Inspected))
	f.settle()
	if !f.data.Traffic.ShowDetailPanel || table.state.Selection.Len() != 1 {
		t.Fatal("the inspected row is not shown")
	}
	rendertest.ShootThemes(t, f.tester, "capture-detail", f.setDark)
}
