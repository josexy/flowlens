package capture

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
