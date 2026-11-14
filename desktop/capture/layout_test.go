package capture

import (
	"maps"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestSidebarItemsKeepRightInset(t *testing.T) {
	f := newHistoryFixture(t)
	for _, label := range []string{"session one", "old format"} {
		row, ok := f.tester.Access(label)
		if !ok || row.Bounds.X != 8 || row.Bounds.X+row.Bounds.W != 232 {
			t.Fatalf("%q must leave 8px on both sides of the 240px sidebar: %+v", label, row)
		}
	}
	// Capture's count is its rightmost child; it must have the same
	// internal padding and outer margin as the saved-session rows.
	count, ok := f.tester.Find("0")
	if !ok || count.X+count.W > 232-10 {
		t.Fatalf("capture count overflows the right inset: %v", count)
	}
}

func TestTableFillsViewportWithSavedOrHiddenPath(t *testing.T) {
	for _, name := range []string{"saved-path", "hidden-path", "reordered"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, rendertest.Traffic()...)
			table := f.workspace.Table
			widths := map[string]float32{"path": 150, "size": 110}
			table.list.Columns.Widths = maps.Clone(widths)
			if name == "hidden-path" {
				f.data.Settings.SetTrafficColumnVisible(logic.ColumnPath, false, nil)
			}
			if name == "reordered" {
				table.list.Columns.Order = append(slices.DeleteFunc(slices.Clone(table.list.Columns.Order), func(key string) bool { return key == "host" }), "host")
			}
			for _, width := range []int{2000, 2400, 2000} {
				f.tester.SetSize(width, 800)
				f.settle()
				viewport, ok := f.tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
				if !ok {
					t.Fatal("no table")
				}
				visible := table.visibleColumns()
				order := table.displayOrder(visible)
				var right float32
				for _, column := range visible {
					header := headerRect(t, f, column.title())
					if string(column.key) == order[len(order)-1] {
						right = header.X + header.W
					}
					if column.key == logic.ColumnPath && header.W != 150 {
						t.Fatalf("saved path width changed: %v", header.W)
					}
				}
				if want := viewport.Bounds.X + viewport.Bounds.W; right != want {
					t.Fatalf("window %d: columns end at %v, table ends at %v", width, right, want)
				}
				_, saved := f.data.Settings.TrafficColumnLayout()
				if !maps.Equal(saved, widths) {
					t.Fatalf("automatic fill changed saved widths: %v", saved)
				}
			}
			rendertest.ShootThemes(t, f.tester, "table-fill-"+name, f.setDark)
			// A narrow viewport must restore the saved width and allow
			// horizontal scrolling instead of squeezing the other columns.
			f.tester.SetSize(1000, 800)
			f.settle()
			f.tester.Scroll(500, 200, 3000, 0)
			f.settle()
			title, want := locales.TrafficSize(), widths["size"]
			if name == "reordered" {
				title, want = locales.TrafficHost(), 200
			}
			if got := headerRect(t, f, title).W; got != want {
				t.Fatalf("narrow viewport kept the automatic fill: %v, want %v", got, want)
			}
		})
	}
}

func TestPathColumnDefaultAndUserWidth(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "a.test"))
	header, ok := f.tester.AccessByRole(ui.RoleColumnHeader, locales.TrafficPath())
	if !ok || header.Bounds.W < 300 {
		t.Fatalf("default path header = %+v", header)
	}
	f.tester.SetSize(2000, 800)
	f.settle()
	header, ok = f.tester.AccessByRole(ui.RoleColumnHeader, locales.TrafficPath())
	if !ok || header.Bounds.W <= 300 {
		t.Fatalf("default path did not grow into spare room: %+v", header)
	}
	f.workspace.Table.list.Columns.Widths = map[string]float32{"path": 150}
	f.tester.SetSize(1400, 800)
	f.settle()
	header, ok = f.tester.AccessByRole(ui.RoleColumnHeader, locales.TrafficPath())
	if !ok || header.Bounds.W != 150 {
		t.Fatalf("explicit width was not retained: %+v", header)
	}
}
