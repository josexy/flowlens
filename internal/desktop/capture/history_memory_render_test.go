package capture

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

func TestRenderHistoryMemoryMatrix(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, mode := range []string{"light", "dark"} {
			for _, size := range []int{16, 24} {
				t.Run(fmt.Sprintf("%s-%s-%d", language, mode, size), func(t *testing.T) {
					f := newRenderFixture(t)
					i18n.SetLocaleFunc(func() string { return language })
					table := f.workspace.Table
					tester := ui.NewTester(func(c *ui.Context) {
						palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode, AppFontSize: size, DisableAnimations: true}).Config)
						table.Build(c, palette)
					}, 1160, 714)
					tester.SetLocale(language)
					for range 6 {
						tester.Frame()
					}
					if _, ok := tester.AccessByRole(ui.RoleTable, locales.TrafficColumns()); !ok {
						t.Fatal("native table role missing")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-table")
					table.state.List.Columns.Widths = map[string]float32{"path": 160}
					table.state.List.Columns.Order = []string{"host", "path", "id", "method"}
					for range 4 {
						tester.Frame()
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-fixed-reordered")
					f.data.Filter.SearchText = "no matching row"
					for range 4 {
						tester.Frame()
					}
					if len(table.rows) != 0 {
						t.Fatal("empty state retained rows")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-empty")
					f.data.Filter.SearchText = ""
					// Test the fill-only path directly; the settings menu normally
					// prevents hiding the last data column.
					definitions := columnDefinitions
					columnDefinitions = nil
					defer func() { columnDefinitions = definitions }()
					for range 4 {
						tester.Frame()
					}
					if len(table.visibleColumns()) != 0 {
						t.Fatal("fill-only fixture still has data columns")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-fill-only")
				})
			}
		}
	}
}
