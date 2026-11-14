package widgets

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

type reorderFixture struct {
	state    ReorderListState
	tester   *ui.Tester
	ids      []string
	revision string
	enabled  bool
	writes   int
	clicks   int
}

func newReorderFixture() *reorderFixture {
	f := &reorderFixture{ids: []string{"a", "b", "c"}, revision: "1", enabled: true}
	config := nui.DefaultApp()
	f.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ids := slices.Clone(f.ids)
		f.state.Build(c.Key("list"), ids, f.revision, f.enabled, len(ids), func(ids []string) {
			f.ids = ids
			f.writes++
		}).Size(280, 180).ItemKey(func(i int) any { return ids[i] }).Rows(func(item ui.ListRow) {
			id := ids[item.Index]
			// Unequal rows exercise measured insertion positions.
			height := float32(44)
			if id == "b" {
				height = 80
			}
			ui.ButtonBase(item.Context).Role(ui.RoleGroup).Label(id).FillWidth().Height(height).Gap(8).
				OnClick(func() { f.clicks++ }).Children(func() {
				ReorderGrip(item.Context.Key("grip"), theme, "drag "+id, f.enabled)
				ui.Text(item.Context, id)
			})
		})
	}, 400, 300)
	return f
}

func (f *reorderFixture) start(t *testing.T) (float32, float32) {
	t.Helper()
	grip, ok := f.tester.Find("drag a")
	if !ok {
		t.Fatal("missing drag handle")
	}
	x, y := grip.X+grip.W/2, grip.Y+grip.H/2
	f.tester.Press(x, y)
	f.tester.Move(x, y+8)
	f.tester.Frame()
	return x, y
}

func TestNativeReorderUsesMeasuredRowsAndCommitsOnce(t *testing.T) {
	f := newReorderFixture()
	x, _ := f.start(t)
	b, _ := f.tester.Find("b")
	y := b.Y + b.H - 4
	f.tester.Move(x, y)
	f.tester.Frame()
	if f.writes != 0 || !slices.Equal(f.ids, []string{"a", "b", "c"}) {
		t.Fatal("drag changed data before drop")
	}
	f.tester.Release(x, y)
	for range 3 {
		f.tester.Frame()
	}
	if !slices.Equal(f.ids, []string{"b", "a", "c"}) || f.writes != 1 || f.clicks != 0 {
		t.Fatalf("order %v, writes %d, clicks %d", f.ids, f.writes, f.clicks)
	}
}

func TestNativeReorderRejectsCanceledAndStaleDrags(t *testing.T) {
	for _, reason := range []string{"escape", "cancel", "outside", "revision", "order", "removed", "disabled"} {
		t.Run(reason, func(t *testing.T) {
			f := newReorderFixture()
			x, _ := f.start(t)
			f.tester.Move(x, 160)
			f.tester.Frame()
			switch reason {
			case "escape":
				f.tester.Key(0, ui.KeyEscape)
			case "cancel":
				f.tester.CancelPress()
			case "outside":
				x = 350
			case "revision":
				f.revision = "2"
			case "order":
				f.ids = []string{"c", "b", "a"}
			case "removed":
				f.ids = []string{"b", "c"}
			case "disabled":
				f.enabled = false
				f.tester.Frame()
				f.enabled = true
			}
			f.tester.Frame()
			before := slices.Clone(f.ids)
			f.tester.Release(x, 160)
			for range 3 {
				f.tester.Frame()
			}
			if f.writes != 0 || !slices.Equal(f.ids, before) {
				t.Fatalf("%s drag saved %v", reason, f.ids)
			}
		})
	}
}

func TestNativeReorderScrollsAtTheListEdge(t *testing.T) {
	f := newReorderFixture()
	f.ids = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	f.tester.Frame()
	x, _ := f.start(t)
	f.tester.Move(x, 176)
	for range 40 {
		f.tester.Advance(time.Second / 60)
	}
	f.tester.Release(x, 176)
	f.tester.Frame()
	if slices.Index(f.ids, "a") < 4 || f.writes != 1 {
		t.Fatalf("edge drag did not reach hidden rows: %v, writes %d", f.ids, f.writes)
	}
}
