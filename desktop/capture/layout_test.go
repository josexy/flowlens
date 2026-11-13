package capture

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/locales"
)

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
