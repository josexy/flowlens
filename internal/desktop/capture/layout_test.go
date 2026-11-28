package capture

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/shell"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestSidebarItemsKeepRightInset(t *testing.T) {
	f := newHistoryFixture(t)
	for _, label := range []string{"session one", "old format"} {
		row, ok := f.tester.Access(label)
		if !ok || row.Bounds.X != 6 || row.Bounds.X+row.Bounds.W != 234 {
			t.Fatalf("%q must leave 6px on both sides of the 240px sidebar: %+v", label, row)
		}
	}
	// Capture's count is its rightmost child; it must have the same
	// internal padding and outer margin as the saved-session rows.
	count, ok := f.tester.Find("0")
	if !ok || count.X+count.W > 234-8 {
		t.Fatalf("capture count overflows the right inset: %v", count)
	}
}

func TestTableKeepsSavedWidthsWithFlexRowFill(t *testing.T) {
	for _, name := range []string{"saved-path", "hidden-path", "reordered"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, rendertest.Traffic()...)
			table := f.workspace.Table
			widths := map[string]float32{"path": 150, "size": 110}
			table.state.List.Columns.Widths = maps.Clone(widths)
			if name == "hidden-path" {
				f.data.Settings.SetTrafficColumnVisible(logic.ColumnPath, false, nil)
			}
			if name == "reordered" {
				table.state.List.Columns.Order = append(slices.DeleteFunc(slices.Clone(table.state.List.Columns.Order), func(key string) bool { return key == "host" }), "host")
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
				for _, column := range visible {
					header := headerRect(t, f, column.title())
					if string(column.key) == order[len(order)-1] {
						want := column.width
						if saved := widths[string(column.key)]; saved > 0 {
							want = saved
						}
						if header.W != want {
							t.Fatalf("last data column width %v, want %v", header.W, want)
						}
					}
					if column.key == logic.ColumnPath && header.W != 150 {
						t.Fatalf("saved path width changed: %v", header.W)
					}
				}
				assertTrafficRowFillsViewport(t, f.tester, viewport.Bounds, "81")
				_, saved := f.data.Settings.TrafficColumnLayout()
				if !maps.Equal(saved, widths) {
					t.Fatalf("automatic fill changed saved widths: %v", saved)
				}
			}
			rendertest.ShootThemes(t, f.tester, "table-fill-"+name, f.setDark)
			// A narrow viewport keeps saved widths and scrolls horizontally.
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
	if !ok || header.Bounds.W < 120 {
		t.Fatalf("default path header = %+v", header)
	}
	narrowWidth := header.Bounds.W
	f.tester.SetSize(2000, 800)
	f.settle()
	header, ok = f.tester.AccessByRole(ui.RoleColumnHeader, locales.TrafficPath())
	if !ok || header.Bounds.W <= narrowWidth {
		t.Fatalf("default path did not grow into spare room: %+v", header)
	}
	f.workspace.Table.state.List.Columns.Widths = map[string]float32{"path": 150}
	f.tester.SetSize(1400, 800)
	f.settle()
	header, ok = f.tester.AccessByRole(ui.RoleColumnHeader, locales.TrafficPath())
	if !ok || header.Bounds.W != 150 {
		t.Fatalf("explicit width was not retained: %+v", header)
	}
}

// Read committed cell geometry while the frame is being painted. Tester.Click
// and Frame settle pending redraws internally, which would otherwise hide a
// gap that repairs itself in a later frame.
func TestSidebarToggleFillsTrafficRowsInEveryPaint(t *testing.T) {
	for _, source := range []string{"capture", "history"} {
		for _, layout := range []string{"default", "saved-path", "hidden-path", "path-last"} {
			for _, appearance := range []string{"light", "dark"} {
				t.Run(source+"/"+layout+"/"+appearance, func(t *testing.T) {
					f := newHistoryFixture(t, entry(1, "first.test"), entry(2, "second.test")).fixture
					app := shell.New(f.data, shell.Host{})
					app.Surfaces[store.WorkspaceCapture] = shell.Surface{Sidebar: f.workspace.Sidebar, Content: f.workspace.Content}
					f.workspace.Dispatcher = app.Dispatcher
					var tester *ui.Tester
					var checking bool
					var paints int
					rowIDs := []string{"1", "2"}
					if source == "history" {
						rowIDs = []string{"11", "12"}
					}
					tester = ui.NewTester(func(c *ui.Context) {
						app.View(c)
						c.Root().DrawOver(func(_ *ui.Painter, _ ui.Rect) {
							if !checking {
								return
							}
							paints++
							viewport, ok := tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
							if !ok {
								t.Fatal("no traffic table during paint")
							}
							for _, id := range rowIDs {
								assertTrafficRowFillsViewport(t, tester, viewport.Bounds, id)
							}
						})
					}, 1920, 800)
					f.tester = tester
					f.setDark(appearance == "dark")
					rendertest.SettleTheme(tester)
					table := f.workspace.Table
					if source == "history" {
						f.click("session one")
						table = f.workspace.HistoryTable
					}
					switch layout {
					case "saved-path":
						table.state.List.Columns.Widths = map[string]float32{"path": 150, "size": 110}
					case "hidden-path":
						f.data.Settings.SetTrafficColumnVisible(logic.ColumnPath, false, nil)
					case "path-last":
						table.state.List.Columns.Order = append(slices.DeleteFunc(slices.Clone(table.state.List.Columns.Order), func(key string) bool { return key == "path" }), "path")
					}
					table.replaceSelection(0)
					f.settle()
					order, widths := f.data.Settings.TrafficColumnLayout()
					checking = true
					for range 6 {
						if err := tester.Click(locales.MenuCapture()); err != nil {
							t.Fatal(err)
						}
						viewport, _ := tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
						first := headerRect(t, f, locales.TrafficId())
						for _, id := range rowIDs {
							row, _ := tester.AccessByRole(ui.RoleRow, id)
							y := row.Bounds.Y + row.Bounds.H/2
							left := pixel(tester, first.X+first.W-6, y)
							right := pixel(tester, viewport.Bounds.X+viewport.Bounds.W-8, y)
							if !near(left, right) {
								t.Fatalf("row %s background changes at the right edge: %v / %v", id, left, right)
							}
						}
					}
					for _, width := range []int{2400, 1920} {
						tester.SetSize(width, 800)
					}
					if paints < 8 {
						t.Fatalf("only checked %d paints", paints)
					}
					gotOrder, gotWidths := f.data.Settings.TrafficColumnLayout()
					if !slices.Equal(order, gotOrder) || !maps.Equal(widths, gotWidths) {
						t.Fatalf("viewport changes altered saved layout: %v / %v", gotOrder, gotWidths)
					}
					rendertest.Shoot(t, tester, "sidebar-flex-"+source+"-"+layout+"-"+appearance)
				})
			}
		}
	}
}

func assertTrafficRowFillsViewport(t *testing.T, tester *ui.Tester, viewport ui.Rect, id string) {
	t.Helper()
	row, ok := tester.AccessByRole(ui.RoleRow, id)
	if !ok {
		t.Fatalf("no traffic row %s", id)
	}
	y := row.Bounds.Y + row.Bounds.H/2
	var right float32
	for _, node := range tester.AccessAll() {
		if node.Role == ui.RoleCell && node.Bounds.Y <= y && y < node.Bounds.Y+node.Bounds.H {
			right = max(right, node.Bounds.X+node.Bounds.W)
		}
	}
	if want := viewport.X + viewport.W; math.Abs(float64(right-want)) > 1 {
		t.Fatalf("row %s cells end at %v, viewport ends at %v", id, right, want)
	}
}

func TestTrafficTableEdgesMeetSplitters(t *testing.T) {
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
						middleX, middleY := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
						var sidebar, detail bool
						for _, node := range f.tester.AccessAll() {
							if node.Role != ui.RoleSplitter {
								continue
							}
							r := node.Bounds
							sidebar = sidebar || r.H > r.W && r.Y <= middleY && middleY < r.Y+r.H && pixelAt(r.X+r.W) == left
							detail = detail || r.W > r.H && r.X <= middleX && middleX < r.X+r.W && pixelAt(r.Y) == bottom
						}
						if !sidebar || !detail {
							t.Fatalf("dark=%v scale=%v zoom=%v: table meets sidebar=%t and detail=%t", dark, scale, zoom, sidebar, detail)
						}
					}
				}
			}
		})
	}
}

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
