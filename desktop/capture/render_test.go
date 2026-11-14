package capture

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/inspect"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/shell"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
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

func TestTrafficTableEdgesMeetSinglePixelSplitters(t *testing.T) {
	for _, source := range []string{"capture", "history"} {
		t.Run(source, func(t *testing.T) {
			f := newHistoryFixture(t, entry(1, "live.test")).fixture
			f.data.Settings.ToggleTrafficDetailLayout(nil)
			deps := inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier}
			f.workspace.Detail = inspect.NewPanel(inspect.LiveData{Traffic: f.data.Traffic}, deps).Build
			f.workspace.HistoryDetail = inspect.NewPanel(store.HistoryDetailData{History: f.data.History}, deps).Build
			app := shell.New(f.data, shell.Host{})
			app.Surfaces[store.WorkspaceCapture] = shell.Surface{Sidebar: f.workspace.Sidebar, Content: f.workspace.Content}
			f.workspace.Dispatcher = app.Dispatcher
			f.tester = ui.NewTester(app.View, 1400, 800)
			f.tester.SetLocale("en")
			if source == "history" {
				f.click("session one")
				f.openDetails("saved.test")
			} else {
				f.openDetails("live.test")
			}
			f.tester.Move(1, 1)
			for _, dark := range []bool{false, true} {
				f.setDark(dark)
				palette := f.theme()
				for _, scale := range []float32{1, 1.25, 1.5, 2} {
					f.tester.SetScale(scale)
					for _, zoom := range []float32{1, 1.19} {
						f.tester.SetPreferences(ui.Preferences{TextScale: zoom})
						f.settle()
						table, ok := f.tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
						if !ok {
							t.Fatal("traffic table missing")
						}
						bounds := table.Bounds
						pixelAt := func(dip float32) int { return int(math.Round(float64(dip * scale))) }
						left, bottom := pixelAt(bounds.X), pixelAt(bounds.Y+bounds.H)
						frame := f.tester.Image()
						leftPixels, bottomPixels := 0, 0
						for offset := -5; offset <= 5; offset++ {
							if frame.RGBAAt(left+offset, bottom-32) != rgba(palette.Background) {
								leftPixels++
							}
							if frame.RGBAAt(pixelAt(bounds.X+bounds.W/4), bottom+offset) != rgba(palette.Background) {
								bottomPixels++
							}
						}
						if leftPixels != 1 || bottomPixels != 1 {
							t.Fatalf("dark=%v scale=%v zoom=%v: sidebar divider is %d pixels and detail divider is %d pixels, want one each", dark, scale, zoom, leftPixels, bottomPixels)
						}
					}
				}
			}
		})
	}
}
