package capture

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestHeaderResizeKeepsCaptureAcrossFastMoves(t *testing.T) {
	for _, title := range []string{locales.TrafficHost(), locales.TrafficPath()} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", title, dark), func(t *testing.T) {
				f := newFixtureSized(t, []*logic.TrafficEntry{entry(1, "a.test")}, 2000, 800)
				f.setDark(dark)
				table := f.workspace.Table
				header := headerRect(t, f, title)
				order, sort := slices.Clone(table.state.List.Columns.Order), table.state.Sort
				x, y := header.X+header.W-2, header.Y+header.H/2
				f.tester.Move(x, y)
				f.settle()
				f.tester.Press(x, y)
				f.tester.Frame()
				for _, delta := range []ui.Rect{{X: 120}, {X: -80}, {X: 220, Y: 70}, {X: 100, Y: -60}, {X: 160}} {
					f.tester.Move(x+delta.X, y+delta.Y)
					f.tester.Frame()
					if got := headerRect(t, f, title).W; got != header.W+delta.X {
						t.Fatalf("move %+v: width %v, want %v", delta, got, header.W+delta.X)
					}
				}
				f.tester.Release(x+180, y)
				f.settle()
				if got := headerRect(t, f, title).W; got != header.W+180 {
					t.Fatalf("release: width %v, want %v", got, header.W+180)
				}
				if !slices.Equal(order, table.state.List.Columns.Order) || sort != table.state.Sort {
					t.Fatal("resizing also reordered or sorted columns")
				}
				snapshot, err := f.service.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				key := "host"
				if title == locales.TrafficPath() {
					key = "path"
				}
				if got := snapshot.TrafficTableConfig.ColumnWidths[key]; float32(got) != header.W+180 {
					t.Fatalf("saved width %v, want %v", got, header.W+180)
				}
			})
		}
	}
}

func TestResizingLastPathColumnPersistsItsWidth(t *testing.T) {
	f := newFixtureSized(t, []*logic.TrafficEntry{entry(1, "a.test")}, 2000, 800)
	table := f.workspace.Table
	table.state.List.Columns.Order = append(slices.DeleteFunc(slices.Clone(table.state.List.Columns.Order), func(key string) bool { return key == "path" }), "path")
	f.settle()
	order := slices.Clone(table.state.List.Columns.Order)
	for _, want := range []float32{150, 120} {
		header := headerRect(t, f, locales.TrafficPath())
		x, y := header.X+header.W-2, header.Y+header.H/2
		target := x + want - header.W
		if want == 120 {
			target -= 100 // The native grip must stop at the 120-DIP minimum.
		}
		f.tester.Move(x, y)
		f.tester.Press(x, y)
		f.tester.Move(target, y)
		f.tester.Release(target, y)
		f.settle()
		if got := headerRect(t, f, locales.TrafficPath()).W; got != want {
			t.Fatalf("dragged path width %v, want %v", got, want)
		}
		snapshot, err := f.service.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot.TrafficTableConfig.ColumnWidths; !maps.Equal(got, map[string]float32{"path": want}) {
			t.Fatalf("saved widths %v, want only path=%v", got, want)
		}
		if !slices.Equal(snapshot.TrafficTableConfig.ColumnOrder, order) {
			t.Fatalf("saved order changed: %v", snapshot.TrafficTableConfig.ColumnOrder)
		}
	}
	f.workspace.Table = NewTable(table.source, table.deps)
	f.tester.SetSize(2400, 800)
	f.settle()
	if got := headerRect(t, f, locales.TrafficPath()).W; got != 120 {
		t.Fatalf("recreated table did not retain dragged width: %v", got)
	}
}
