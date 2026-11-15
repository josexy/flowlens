package memstats

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/theme"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
	"github.com/josexy/flowlens/locales"
)

// These widths are the actual content after the activity rail and the
// secondary sidebar, not the outer window width.
func TestContentWidthLayoutAndSamplingSurviveResize(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			f := newFixture(t, memstatsservice.MonitoringStatus{Monitoring: running, IntervalMs: 2000})
			f.emit(1000)
			f.emit(2000)
			f.view.chart.SetSeriesVisible("heap", false)
			samples := slices.Clone(f.data.Samples)
			starts, stops := len(f.service.starts), f.service.stops
			for _, width := range []int{558, 770, 820, 1360, 558} {
				f.tester.SetSize(width, 768)
				f.settle()
				checkControls(t, f.tester, width, running)
				if !slices.Equal(samples, f.data.Samples) || f.data.Monitoring != running || f.data.IntervalMs != 2000 || len(f.service.starts) != starts || f.service.stops != stops || f.view.chart.SeriesVisible("heap") {
					t.Fatal("layout change reset sampling or chart visibility")
				}
			}
		})
	}
}

func TestContentLayoutLocalesThemesAndMaximumFont(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, mode := range []string{theme.ModeLight, theme.ModeDark} {
			t.Run(language+"/"+mode, func(t *testing.T) {
				f := newFixture(t, memstatsservice.MonitoringStatus{})
				i18n.SetLocaleFunc(func() string { return language })
				f.tester.SetLocale(language)
				f.config = theme.Resolve(theme.Appearance{Mode: mode, AppFontSize: theme.MaxAppFontSize}).Config
				f.emit(1_700_000_000_000)
				f.tester.SetSize(558, 2000)
				f.tester.SetScale(1.5)
				f.settle()
				checkControls(t, f.tester, 558, false)
				// Distinct labels and values of the lower metrics remain in
				// separate columns even when the two cards stack.
				for _, pair := range [][2]string{{locales.MemstatsGoVersion(), "go1.26"}, {locales.MemstatsOsArch(), "linux / arm64"}, {locales.MemstatsLiveObjects(), "1,034,567"}} {
					label, a := f.tester.Find(pair[0])
					value, b := f.tester.Find(pair[1])
					if !a || !b || value.X < label.X+label.W-1 || value.X+value.W > 558 || label.H <= 0 || value.H <= 0 {
						t.Fatalf("overlapping metric %q: %v / %v", pair[0], label, value)
					}
				}
				f.tester.SetSize(558, 768)
				f.settle()
				rendertest.Shoot(t, f.tester, "memstats-narrow-"+language+"-"+mode)
			})
		}
	}
}

func checkControls(t *testing.T, tester *ui.Tester, width int, running bool) {
	t.Helper()
	start := locales.MemstatsStart()
	if running {
		start = locales.MemstatsStop()
	}
	var boxes []ui.Rect
	for _, label := range []string{start, locales.MemstatsInterval(), locales.MemstatsRefresh(), locales.MemstatsClear()} {
		a, ok := tester.Access(label)
		if !ok || a.Bounds.W <= 0 || a.Bounds.H <= 0 || a.Bounds.X < 0 || a.Bounds.X+a.Bounds.W > float32(width)+1 || a.Bounds.Y+a.Bounds.H > 200 {
			t.Fatalf("%q outside content width %d: %+v", label, width, a)
		}
		for _, b := range boxes {
			if a.Bounds.X < b.X+b.W && a.Bounds.X+a.Bounds.W > b.X && a.Bounds.Y < b.Y+b.H && a.Bounds.Y+a.Bounds.H > b.Y {
				t.Fatalf("overlapping toolbar controls: %v / %v", a.Bounds, b)
			}
		}
		boxes = append(boxes, a.Bounds)
	}
	for _, label := range []string{locales.MemstatsHeapAlloc(), locales.MemstatsSys(), locales.MemstatsNumGc(), locales.MemstatsNumGoroutine()} {
		b, ok := tester.Find(label)
		if !ok || b.W <= 0 || b.X < 0 || b.X+b.W > float32(width)+1 {
			t.Fatalf("summary %q overflows: %v", label, b)
		}
	}
}
