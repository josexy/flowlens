package widgets

import (
	"testing"

	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/desktop/theme"
)

func TestFieldGridsLeaveRoomForLargeText(t *testing.T) {
	for _, editable := range []bool{false, true} {
		rows := []KeyValue{{"Accept", "first value"}, {"Host", "second value"}}
		enabled := []bool{true, true}
		tester := ui.NewTester(func(c *ui.Context) {
			palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{AppFontSize: 24}).Config)
			if editable {
				EditableGrid(c, palette, EditableGridOptions{
					Rows:    len(rows),
					Enabled: func(i int) *bool { return &enabled[i] },
					Key:     func(i int) *string { return &rows[i].Key },
					Value:   func(i int) *string { return &rows[i].Value },
				})
			} else {
				FieldGrid(c, palette, rows, FieldGridOptions{})
			}
		}, 900, 500)
		for range 3 {
			tester.Frame()
		}
		first, ok := tester.Find("first value")
		second, found := tester.Find("second value")
		if !ok || !found || first.H < 30 || second.Y-first.Y < first.H+10 {
			t.Fatalf("editable %v: clipped or crowded text: %+v, %+v", editable, first, second)
		}
	}
}
