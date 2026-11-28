package widgets

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestKeyValueTableKeepsOrderDuplicatesAndCase(t *testing.T) {
	rows := []KeyValue{{"Set-Cookie", "a=1"}, {"set-cookie", "b=2"}, {"X-Empty", ""}, {"Set-Cookie", "c=3"}}
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		KeyValueTable(c, theme, rows)
	}, 600, 300)
	var got []string
	for _, text := range tester.Texts() {
		if text != "" {
			got = append(got, text)
		}
	}
	want := []string{"Set-Cookie", "a=1", "set-cookie", "b=2", "X-Empty", "Set-Cookie", "c=3"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type fakeLoads struct {
	calls    []string
	finishes map[string]func(int, bool)
}

func newFakeLoads() *fakeLoads { return &fakeLoads{finishes: map[string]func(int, bool){}} }

func (f *fakeLoads) load(key string, finish func(int, bool)) {
	f.calls = append(f.calls, key)
	f.finishes[key] = finish
}

func TestMissingIconIsRetriedAfterADelayAndThenAvailable(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	now := time.Unix(1000, 0)
	cache.now = func() time.Time { return now }

	if _, ok := cache.Get("a"); ok {
		t.Fatal("unexpected hit")
	}
	loads.finishes["a"](0, false)
	cache.Get("a")
	if len(loads.calls) != 1 {
		t.Fatalf("retried immediately: %v", loads.calls)
	}
	now = now.Add(iconRetryDelay)
	cache.Get("a")
	if len(loads.calls) != 2 {
		t.Fatalf("not retried: %v", loads.calls)
	}
	loads.finishes["a"](7, true)
	if got, ok := cache.Get("a"); !ok || got != 7 {
		t.Fatalf("got %d, %v", got, ok)
	}
}

func TestConcurrentGetsMergeIntoOneLoad(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	cache.Get("a")
	cache.Get("a")
	if len(loads.calls) != 1 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestLeastRecentlyUsedIconIsEvicted(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(2, loads.load)
	for _, key := range []string{"a", "b"} {
		cache.Get(key)
		loads.finishes[key](1, true)
	}
	cache.Get("a") // b is now the oldest
	cache.Get("c")
	loads.finishes["c"](1, true)
	if cache.Len() != 2 {
		t.Fatalf("len %d", cache.Len())
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("recently used icon evicted")
	}
	if cache.index("b") >= 0 {
		t.Fatal("oldest icon kept")
	}
}

func TestCacheNeverExceedsItsSize(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(processIconCacheSize, loads.load)
	for i := range processIconCacheSize + 10 {
		key := fmt.Sprint(i)
		cache.Get(key)
		loads.finishes[key](i, true)
	}
	if cache.Len() != processIconCacheSize {
		t.Fatalf("len %d", cache.Len())
	}
}

func TestClearDiscardsRunningLoads(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	finish := loads.finishes["a"]
	cache.Clear()
	finish(1, true)
	if cache.Len() != 0 {
		t.Fatal("stale load stored after Clear")
	}
	cache.Get("a")
	if len(loads.calls) != 2 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestDeleteForcesReload(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	loads.finishes["a"](1, true)
	cache.Delete("a")
	if _, ok := cache.Get("a"); ok {
		t.Fatal("deleted icon returned")
	}
	if len(loads.calls) != 2 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestInvalidProcessIconKeysAreNotLoaded(t *testing.T) {
	icons := &ProcessIcons{cache: NewIconCache(1, func(string, func(*ui.Bitmap, bool)) { t.Fatal("loaded an invalid key") })}
	if icons.Bitmap("not-a-key") != nil {
		t.Fatal("unexpected bitmap")
	}
}

func TestTableDividerFollowsNativeHeaderAtDifferentTextSizes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, fontSize := range []float32{14, 21} {
			t.Run(fmt.Sprintf("dark=%v/font=%v", dark, fontSize), func(t *testing.T) {
				config := nui.DefaultApp()
				palette := config.Resolve(dark, ui.Preferences{})
				palette.FontSize, palette.Density = fontSize, 0.8
				var state nui.TableState[string]
				state.SetRows([]string{"first row", "second row"}, func(row string) string { return row })
				tester := ui.NewTester(func(c *ui.Context) {
					nui.App(c, config)
					Table(c, palette, &state, []nui.TableColumn[string]{{ID: "value", Title: "Value", Value: func(row string) string { return row }}}, nui.TableOptions{Label: "Records"})
				}, 400, 200)
				for _, scale := range []float32{1, 1.25, 1.5, 2} {
					tester.SetScale(scale)
					for _, zoom := range []float32{1, 1.19} {
						tester.SetZoom(zoom)
						header, ok := tester.AccessByRole(ui.RoleColumnHeader, "Value")
						if !ok {
							t.Fatal("missing header")
						}
						first, firstOK := tester.Find("first row")
						second, secondOK := tester.Find("second row")
						if !firstOK || !secondOK || first.H < (fontSize*1.5+4)*zoom || second.Y < first.Y+first.H-0.01 {
							t.Fatalf("scale=%v zoom=%v: clipped or crowded rows: %v, %v", scale, zoom, first, second)
						}
						x := int((header.Bounds.X + header.Bounds.W/2) * scale)
						y := int(math.Round(float64((header.Bounds.Y + header.Bounds.H) * scale)))
						frame := tester.Image()
						border := palette.Border
						line := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
						if frame.RGBAAt(x, y) != line || frame.RGBAAt(x, y-1) == line || frame.RGBAAt(x, y+1) == line {
							t.Fatalf("scale=%v zoom=%v: divider is not exactly one physical pixel at y=%d", scale, zoom, y)
						}
					}
				}
			})
		}
	}
}
