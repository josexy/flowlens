// Package memstats builds the Memory Statistics section.
package memstats

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const unknown = "-"

var intervals = []nui.Option[int64]{
	{Value: 500, Label: "500ms"}, {Value: 1000, Label: "1s"}, {Value: 2000, Label: "2s"}, {Value: 5000, Label: "5s"}, {Value: 10000, Label: "10s"},
}

// View is the content of the Memory Statistics section.
type View struct {
	Data *store.MemStatsStore
	// Select shows the section.
	Select func()

	interval int64
	chart    nui.ChartState
	charted  uint64
	drawn    bool
}

// New returns the section's content.
func New(data *store.MemStatsStore) *View { return &View{Data: data} }

func formatBytes(bytes uint64) string {
	return logic.FormatFileSizeWith(int64(bytes), logic.FileSizeOptions{Precision: 2, UnknownValue: unknown})
}

func formatNs(ns uint64) string {
	switch {
	case ns == 0:
		return "0"
	case ns >= 1_000_000:
		return strconv.FormatFloat(float64(ns)/1_000_000, 'f', 3, 64) + " ms"
	case ns >= 1_000:
		return strconv.FormatFloat(float64(ns)/1_000, 'f', 1, 64) + " μs"
	}
	return strconv.FormatUint(ns, 10) + " ns"
}

func formatPercent(fraction float64) string {
	return strconv.FormatFloat(fraction*100, 'f', 4, 64) + "%"
}

func formatLastGC(milliseconds int64) string {
	if milliseconds == 0 {
		return locales.MemstatsNoGc()
	}
	return time.UnixMilli(milliseconds).Local().Format("2006-01-02 15:04:05")
}

// groupDigits writes a count with thousands separators.
func groupDigits(value uint64) string {
	digits := strconv.FormatUint(value, 10)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}

func liveObjects(sample *memstatsservice.MemSnapshot) string {
	if sample.Frees > sample.Mallocs {
		return "0"
	}
	return groupDigits(sample.Mallocs - sample.Frees)
}

type metric struct{ label, value string }

// value reads a sample, or shows the unknown mark before the first one.
func value(sample *memstatsservice.MemSnapshot, read func(*memstatsservice.MemSnapshot) string) string {
	if sample == nil {
		return unknown
	}
	return read(sample)
}

func bytesOf(sample *memstatsservice.MemSnapshot, read func(*memstatsservice.MemSnapshot) uint64) string {
	return value(sample, func(s *memstatsservice.MemSnapshot) string { return formatBytes(read(s)) })
}

func countOf[T int | uint32 | uint64](sample *memstatsservice.MemSnapshot, read func(*memstatsservice.MemSnapshot) T) string {
	return value(sample, func(s *memstatsservice.MemSnapshot) string { return fmt.Sprint(read(s)) })
}

func (v *View) toolbar(c *ui.Context, theme nui.Theme) {
	data := v.Data
	ui.Row(c.Key("toolbar")).FillWidth().Shrink(0).Wrap().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Padding(10, 14).Background(theme.Background).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Row(c.Key("title")).Wrap().Gap(8).AlignItems(ui.Center).MinWidth(0).Children(func() {
			ui.Text(c, locales.MemstatsTitle()).SingleLine().FontSize(apptheme.TextSize(c, 15.2)).FontWeight(600).TextColor(theme.Text)
			nui.Badge(c.Key("subtitle"), theme, locales.MemstatsSubtitle(), nui.BadgeOptions{
				Color: nui.Info, Variant: nui.Subtle, Size: nui.MD,
				UI: nui.Styles{nui.Root: {FontSize: nui.Set(apptheme.TextSize(c, 14)), Weight: nui.Set(600), PaddingX: nui.Set[float32](8), PaddingY: nui.Set[float32](2)}},
			})
		})
		ui.Row(c.Key("actions")).Wrap().Gap(8).AlignItems(ui.Center).MinWidth(0).Children(func() {
			label, icon, color := locales.MemstatsStart(), lucide.Play(), nui.Primary
			if data.Monitoring {
				label, icon, color = locales.MemstatsStop(), lucide.Pause(), nui.Warning
			}
			nui.Button(c.Key("toggle"), theme, label, nui.ButtonOptions{Color: color, Size: nui.SM, Leading: icon}).OnClick(func() {
				if data.Monitoring {
					data.Stop()
				} else {
					data.Start()
				}
			})
			v.interval = data.IntervalMs
			ui.Box(c.Key("interval")).Width(110).Children(func() {
				nui.Select(c.Key("select"), theme, &v.interval, intervals, nui.SelectOptions{Label: locales.MemstatsInterval(), Size: nui.SM})
			})
			data.SetInterval(v.interval)
			nui.Button(c.Key("refresh"), theme, locales.MemstatsRefresh(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Leading: lucide.RefreshCw()}).OnClick(data.FetchOnce)
			nui.Button(c.Key("clear"), theme, locales.MemstatsClear(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Leading: lucide.Trash()}).OnClick(data.Clear)
		})
	})
}

func (v *View) summary(c *ui.Context, theme nui.Theme, sample *memstatsservice.MemSnapshot) {
	cards := []metric{
		{locales.MemstatsHeapAlloc(), bytesOf(sample, func(s *memstatsservice.MemSnapshot) uint64 { return s.HeapAlloc })},
		{locales.MemstatsSys(), bytesOf(sample, func(s *memstatsservice.MemSnapshot) uint64 { return s.Sys })},
		{locales.MemstatsNumGc(), countOf(sample, func(s *memstatsservice.MemSnapshot) uint32 { return s.NumGC })},
		{locales.MemstatsNumGoroutine(), countOf(sample, func(s *memstatsservice.MemSnapshot) int { return s.NumGoroutine })},
	}
	ui.Row(c.Key("summary")).FillWidth().Wrap().Gap(12).Children(func() {
		for index, card := range cards {
			panelCard(c, theme, index, 6, func() {
				ui.Text(c, card.label).SingleLine().AlignSelf(ui.Center).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.5).TextColor(theme.Muted)
				ui.Text(c, card.value).SingleLine().AlignSelf(ui.Center).FontSize(apptheme.TextSize(c, 17.6)).LineHeight(1.5).FontWeight(600).TextColor(theme.Primary)
			}).Grow(1).Basis(180)
		}
	})
}

// syncChart gives the chart the samples, once per change. Samples taken within
// one millisecond, or by Refresh during monitoring, cannot share a position.
func (v *View) syncChart() {
	if v.drawn && v.charted == v.Data.Version {
		return
	}
	v.drawn, v.charted = true, v.Data.Version
	samples := v.Data.Samples
	times := make([]time.Time, 0, len(samples))
	heap := make([]float64, 0, len(samples))
	sys := make([]float64, 0, len(samples))
	stack := make([]float64, 0, len(samples))
	for index := range samples {
		sample := &samples[index]
		at := time.UnixMilli(sample.Timestamp)
		if len(times) > 0 && !at.After(times[len(times)-1]) {
			continue
		}
		times = append(times, at)
		heap = append(heap, float64(sample.HeapAlloc))
		sys = append(sys, float64(sample.Sys))
		stack = append(stack, float64(sample.StackInuse))
	}
	_ = v.chart.SetTimeData(times, []nui.ChartSeries{
		{ID: "heap", Label: "HeapAlloc", Values: heap, Color: nui.Success},
		{ID: "sys", Label: "Sys", Values: sys, Color: nui.Info},
		{ID: "stack", Label: "StackInuse", Values: stack, Color: nui.Warning},
	})
}

func (v *View) chartCard(c *ui.Context, theme nui.Theme) {
	panelCard(c, theme, "chart", 12, func() {
		heading(c, theme, locales.MemstatsChartTitle())
		if len(v.Data.Samples) == 0 {
			widgets.EmptyState(c.Key("empty"), theme, nui.EmptyOptions{Icon: lucide.ChartLine(), Title: locales.MemstatsNoData()}).MinHeight(120)
			return
		}
		v.syncChart()
		nui.Chart(c.Key("plot"), theme, &v.chart, nui.ChartOptions{
			Label: locales.MemstatsChartTitle(), Kind: nui.ChartLine, Height: 220, Legend: nui.Set(true), Grid: nui.Set(true), Tooltip: nui.Set(false),
			Animate: nui.Set(false), FormatY: func(value float64) string { return formatBytes(uint64(max(value, 0))) },
			FormatTime: func(at time.Time) string { return at.Local().Format("15:04:05") },
		}).FillWidth()
	}).FillWidth()
}

// panelCard uses the shared Card with the monitoring panel's compact spacing.
func panelCard(c *ui.Context, theme nui.Theme, key any, gap float32, body func()) ui.Element {
	return nui.Card(c.Key(key), theme, nui.CardOptions{
		Decoration: nui.Decoration{UI: nui.Styles{
			nui.Body: {PaddingX: nui.Set(float32(12)), PaddingY: nui.Set(float32(12)), Gap: nui.Set(gap)},
		}},
	}, body).
		Shadow(0, 14, 30, -26, ui.RGBA(15, 23, 42, 0.35))
}

func metrics(c *ui.Context, theme nui.Theme, key string, items []metric) {
	ui.Column(c.Key(key)).FillWidth().Border(1, theme.Border).Radius(6).Clip().Children(func() {
		for index, item := range items {
			line := ui.Row(c.Key(index)).FillWidth().AlignItems(ui.Stretch)
			if index > 0 {
				line.BorderWidth(1, 0, 0, 0).BorderColor(theme.Border)
			}
			line.Children(func() {
				ui.Text(c, item.label).WidthPercent(38).MinWidth(120).Shrink(0).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.35).Padding(7, 9).TextColor(theme.Muted).Background(theme.Elevated)
				ui.Text(c, item.value).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.35).Padding(7, 9).TextColor(theme.Text).Selectable()
			})
		}
	})
}

func heading(c *ui.Context, theme nui.Theme, title string) {
	ui.Text(c.Key(title), title).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
}

// divider names a group inside a card, with a rule after the name.
func divider(c *ui.Context, theme nui.Theme, title string) {
	ui.Row(c.Key(title)).FillWidth().AlignItems(ui.Center).Gap(8).Margin(12, 0, 12, 0).Children(func() {
		ui.Text(c, title).SingleLine().FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Muted)
		ui.Box(c).Grow(1).Height(1).Background(theme.Border)
	})
}

func (v *View) details(c *ui.Context, theme nui.Theme, s *memstatsservice.MemSnapshot) {
	type snapshot = memstatsservice.MemSnapshot
	ui.Row(c.Key("details")).FillWidth().Wrap().Gap(12).AlignItems(ui.Start).Children(func() {
		panelCard(c, theme, "left", 0, func() {
			heading(c, theme, locales.MemstatsSectionHeap())
			ui.Box(c).Height(12)
			metrics(c, theme, "heap", []metric{
				{locales.MemstatsHeapAllocDesc(), bytesOf(s, func(s *snapshot) uint64 { return s.HeapAlloc })},
				{locales.MemstatsHeapSys(), bytesOf(s, func(s *snapshot) uint64 { return s.HeapSys })},
				{locales.MemstatsHeapInuse(), bytesOf(s, func(s *snapshot) uint64 { return s.HeapInuse })},
				{locales.MemstatsHeapIdle(), bytesOf(s, func(s *snapshot) uint64 { return s.HeapIdle })},
				{locales.MemstatsHeapReleased(), bytesOf(s, func(s *snapshot) uint64 { return s.HeapReleased })},
				{locales.MemstatsHeapObjects(), countOf(s, func(s *snapshot) uint64 { return s.HeapObjects })},
			})
			divider(c, theme, locales.MemstatsSectionStack())
			metrics(c, theme, "stack", []metric{
				{locales.MemstatsStackInuse(), bytesOf(s, func(s *snapshot) uint64 { return s.StackInuse })},
				{locales.MemstatsStackSys(), bytesOf(s, func(s *snapshot) uint64 { return s.StackSys })},
			})
		}).Grow(1).Basis(360)
		panelCard(c, theme, "right", 0, func() {
			heading(c, theme, locales.MemstatsSectionGc())
			ui.Box(c).Height(12)
			metrics(c, theme, "gc", []metric{
				{locales.MemstatsGcCount(), countOf(s, func(s *snapshot) uint32 { return s.NumGC })},
				{locales.MemstatsForcedGcCount(), countOf(s, func(s *snapshot) uint32 { return s.NumForcedGC })},
				{locales.MemstatsGcCpu(), value(s, func(s *snapshot) string { return formatPercent(s.GCCPUFraction) })},
				{locales.MemstatsPauseTotal(), value(s, func(s *snapshot) string { return formatNs(s.PauseTotalNs) })},
				{locales.MemstatsPauseLast(), value(s, func(s *snapshot) string { return formatNs(s.PauseNs) })},
				{locales.MemstatsNextGc(), bytesOf(s, func(s *snapshot) uint64 { return s.NextGC })},
				{locales.MemstatsLastGcTime(), value(s, func(s *snapshot) string { return formatLastGC(s.LastGC) })},
			})
			divider(c, theme, locales.MemstatsSectionOverall())
			metrics(c, theme, "overall", []metric{
				{locales.MemstatsTotalAlloc(), bytesOf(s, func(s *snapshot) uint64 { return s.TotalAlloc })},
				{locales.MemstatsMallocs(), countOf(s, func(s *snapshot) uint64 { return s.Mallocs })},
				{locales.MemstatsFrees(), countOf(s, func(s *snapshot) uint64 { return s.Frees })},
				{locales.MemstatsLiveObjects(), value(s, liveObjects)},
			})
			divider(c, theme, locales.MemstatsSectionRuntime())
			metrics(c, theme, "runtime", []metric{
				{locales.MemstatsGoVersion(), value(s, func(s *snapshot) string { return s.GoVersion })},
				{locales.MemstatsOsArch(), value(s, func(s *snapshot) string { return s.GOOS + " / " + s.GOARCH })},
				{locales.MemstatsNumCpu(), countOf(s, func(s *snapshot) int { return s.NumCPU })},
				{locales.MemstatsNumGoroutine(), countOf(s, func(s *snapshot) int { return s.NumGoroutine })},
			})
		}).Grow(1).Basis(360)
	})
}

// Sidebar shows the section's single entry.
func (v *View) Sidebar(c *ui.Context, theme nui.Theme) {
	ui.Column(c.Key("memstats-sidebar")).Fill().Children(func() {
		entry := ui.ButtonBase(c.Key("entry")).FillWidth().MinHeight(40).Justify(ui.Start).Gap(8).Padding(8, 10, 8, 7).Background(theme.Primary.Alpha(0.08)).FocusRing(true).Label(locales.MenuMemstats())
		entry.BorderWidth(0, 0, 0, 3).BorderColor(theme.Primary)
		if v.Select != nil {
			entry.OnClick(v.Select)
		}
		entry.Children(func() {
			ui.Icon(c, lucide.Activity()).Size(16, 16).Shrink(0).TextColor(theme.Primary)
			ui.Text(c, locales.MenuMemstats()).SingleLine().FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Text)
		})
	})
}

// Content shows the section.
func (v *View) Content(c *ui.Context, theme nui.Theme) {
	ui.Column(c.Key("memstats")).Fill().Children(func() {
		v.toolbar(c, theme)
		ui.Scroll(c.Key("body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("content")).FillWidth().Gap(12).Padding(12).Children(func() {
				v.summary(c, theme, v.Data.Latest)
				v.chartCard(c, theme)
				v.details(c, theme, v.Data.Latest)
			})
		})
	})
}
