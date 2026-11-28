// Package memstats builds the Memory Statistics section.
package memstats

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/i18n"
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

	intervalChoice widgets.SelectState[int64]
	interval       int64
	chart          nui.ChartState
	charted        uint64
	chartLocale    string
	drawn          bool
	// viewed is set by the first Content call, which takes one sample if none
	// exists. Showing the section again never samples.
	viewed      bool
	detailsOpen bool
}

// New returns the section's content.
func New(data *store.MemStatsStore) *View { return &View{Data: data} }

func formatBytes(bytes int64) string {
	return logic.FormatFileSizeWith(bytes, logic.FileSizeOptions{Precision: 1, UnknownValue: unknown})
}

func formatPause(ns int64) string {
	return logic.FormatDurationMicros(0, ns/int64(time.Microsecond))
}

func formatLastGC(memory memstatsservice.RuntimeMemory) string {
	if memory.LastGC == 0 {
		return locales.MemstatsNoGc()
	}
	return locales.MemstatsLastGcValue(formatPause(memory.LastPauseNs), logic.FormatTimeLocal(time.UnixMilli(memory.LastGC)))
}

// processCaption says when Memory cannot be read; a readable value needs none.
func processCaption(memory memstatsservice.ProcessMemory) string {
	if memory.Available() {
		return ""
	}
	return locales.MemstatsMemoryUnavailable()
}

// metric is a row of a details table. A total row is emphasized, and its
// parts are indented under it.
type metric struct {
	label, value string
	total, part  bool
}

// value reads a sample, or shows the unknown mark before the first one.
func value(sample *memstatsservice.MemSnapshot, read func(*memstatsservice.MemSnapshot) string) string {
	if sample == nil {
		return unknown
	}
	return read(sample)
}

func bytesOf(sample *memstatsservice.MemSnapshot, read func(*memstatsservice.RuntimeMemory) uint64) string {
	return value(sample, func(s *memstatsservice.MemSnapshot) string { return formatBytes(int64(read(&s.Runtime))) })
}

func countOf(sample *memstatsservice.MemSnapshot, read func(*memstatsservice.RuntimeMemory) uint64) string {
	return value(sample, func(s *memstatsservice.MemSnapshot) string { return fmt.Sprint(read(&s.Runtime)) })
}

func (v *View) toolbar(c *ui.Context, theme nui.Theme) {
	data := v.Data
	ui.Row(c.Key("toolbar")).FillWidth().Shrink(0).Wrap().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(6).Padding(6, 10).Background(theme.Background).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c.Key("title"), locales.MemstatsTitle()).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 15.2)).FontWeight(600).TextColor(theme.Text)
		ui.Row(c.Key("actions")).Wrap().Gap(8).AlignItems(ui.Center).MinWidth(0).Children(func() {
			label, icon, color := locales.MemstatsStart(), lucide.Play(), nui.Primary
			if data.Monitoring {
				label, icon, color = locales.MemstatsStop(), lucide.Pause(), nui.Warning
			}
			nui.Button(c.Key("toggle"), theme, label, nui.ButtonOptions{Color: color, Size: nui.MD, Leading: icon}).OnClick(func() {
				if data.Monitoring {
					data.Stop()
				} else {
					data.Start()
				}
			})
			v.interval = data.IntervalMs
			ui.Box(c.Key("interval")).Width(110).Children(func() {
				nui.Select(c.Key("select"), theme, &v.interval, v.intervalChoice.Prepare("", func() []nui.Option[int64] { return intervals }), nui.SelectOptions{Label: locales.MemstatsInterval(), Size: nui.MD})
			})
			data.SetInterval(v.interval)
			nui.Button(c.Key("refresh"), theme, locales.MemstatsRefresh(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.MD, Leading: lucide.RefreshCw()}).OnClick(data.FetchOnce)
			nui.Button(c.Key("clear"), theme, locales.MemstatsClear(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.MD, Leading: lucide.Trash()}).OnClick(data.Clear)
		})
	})
}

// summaryCard is a headline figure. Memory has a caption only when it cannot
// be read.
type summaryCard struct {
	key, label, value, caption string
	color                      ui.Color
}

func (v *View) summary(c *ui.Context, theme nui.Theme, sample *memstatsservice.MemSnapshot) {
	memory := summaryCard{key: "process", label: locales.MemstatsMemory(), value: unknown, color: theme.Primary}
	heap := summaryCard{key: "heap", label: locales.MemstatsHeapInuse(), value: unknown, color: theme.Text}
	if sample != nil {
		// An unreadable process value shows the unknown mark, never a Go value.
		memory.value, memory.caption = formatBytes(sample.Process.Bytes), processCaption(sample.Process)
		heap.value = formatBytes(int64(sample.Runtime.HeapInUse))
	}
	ui.Row(c.Key("summary")).FillWidth().Wrap().Gap(12).AlignItems(ui.Stretch).Children(func() {
		for _, card := range []summaryCard{memory, heap} {
			panelCard(c, theme, card.key, 4, func() {
				ui.Text(c, card.label).SingleLine().AlignSelf(ui.Center).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.5).TextColor(theme.Muted)
				ui.Text(c, card.value).SingleLine().AlignSelf(ui.Center).FontSize(apptheme.TextSize(c, 17.6)).LineHeight(1.5).FontWeight(600).TextColor(card.color)
				if card.caption != "" {
					ui.Text(c, card.caption).FillWidth().TextAlign(ui.Center).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.4).TextColor(theme.Muted)
				}
			}).Grow(1).Basis(240)
		}
	})
}

// chartValues returns the chart's times and values. Samples that cannot follow
// the previous one in time are skipped, and a sample without process memory
// leaves a gap (NaN) in Memory.
func chartValues(samples []memstatsservice.MemSnapshot) (times []time.Time, process, heap []float64) {
	times = make([]time.Time, 0, len(samples))
	process = make([]float64, 0, len(samples))
	heap = make([]float64, 0, len(samples))
	for index := range samples {
		sample := &samples[index]
		at := time.UnixMilli(sample.Timestamp)
		if len(times) > 0 && !at.After(times[len(times)-1]) {
			continue
		}
		times = append(times, at)
		memory := math.NaN()
		if sample.Process.Available() {
			memory = float64(sample.Process.Bytes)
		}
		process = append(process, memory)
		heap = append(heap, float64(sample.Runtime.HeapInUse))
	}
	return times, process, heap
}

// syncChart gives the chart the samples, once per change of samples or
// language. Samples taken within one millisecond, or by Refresh during
// monitoring, cannot share a position.
func (v *View) syncChart() {
	locale := i18n.Locale()
	if v.drawn && v.charted == v.Data.Version && v.chartLocale == locale {
		return
	}
	v.drawn, v.charted, v.chartLocale = true, v.Data.Version, locale
	times, process, heap := chartValues(v.Data.Samples)
	// The process series is listed even when every value is a gap, so the
	// legend does not change between samples.
	_ = v.chart.SetTimeData(times, []nui.ChartSeries{
		{ID: "process", Label: locales.MemstatsMemory(), Values: process, Color: nui.Primary},
		// Neutral stays distinct from every primary color a user can choose.
		{ID: "heap", Label: locales.MemstatsHeapInuse(), Values: heap, Color: nui.Neutral},
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
			Animate: nui.Set(false), FormatY: func(value float64) string {
				if math.IsNaN(value) || value < 0 {
					return unknown
				}
				return formatBytes(int64(value))
			},
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
				label := ui.Text(c, item.label).WidthPercent(38).MinWidth(120).Shrink(0).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.35).Padding(7, 9).TextColor(theme.Muted).Background(theme.Elevated)
				value := ui.Text(c, item.value).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.35).Padding(7, 9).TextColor(theme.Text).Selectable()
				switch {
				case item.total:
					label.FontWeight(600).TextColor(theme.Text)
					value.FontWeight(600)
				case item.part:
					label.Padding(7, 9, 7, 21)
				}
			})
		}
	})
}

func heading(c *ui.Context, theme nui.Theme, title string) {
	ui.Text(c.Key(title), title).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
}

// details holds the Go runtime values, collapsed until the user expands them.
func (v *View) details(c *ui.Context, theme nui.Theme, s *memstatsservice.MemSnapshot) {
	type memory = memstatsservice.RuntimeMemory
	panelCard(c, theme, "details", 0, func() {
		nui.Collapsible(c.Key("collapsible"), theme, &v.detailsOpen, nui.CollapsibleOptions{Label: locales.MemstatsDetails()}, func(theme nui.Theme) {
			ui.Column(c.Key("body")).FillWidth().Gap(12).Children(func() {
				ui.Row(c.Key("tables")).FillWidth().Wrap().Gap(12).AlignItems(ui.Start).Children(func() {
					ui.Column(c.Key("memory")).Grow(1).Basis(300).MinWidth(0).Gap(8).Children(func() {
						heading(c, theme, locales.MemstatsSectionMemory())
						metrics(c, theme, "memory", []metric{
							{label: locales.MemstatsHeld(), value: bytesOf(s, func(m *memory) uint64 { return m.Held }), total: true},
							{label: locales.MemstatsHeapInuse(), value: bytesOf(s, func(m *memory) uint64 { return m.HeapInUse }), part: true},
							{label: locales.MemstatsKeptForReuse(), value: bytesOf(s, func(m *memory) uint64 { return m.KeptForReuse }), part: true},
							{label: locales.MemstatsStacks(), value: bytesOf(s, func(m *memory) uint64 { return m.Stacks }), part: true},
							{label: locales.MemstatsOverhead(), value: bytesOf(s, func(m *memory) uint64 { return m.Overhead }), part: true},
							{label: locales.MemstatsHeapReleased(), value: bytesOf(s, func(m *memory) uint64 { return m.Released })},
						})
					})
					ui.Column(c.Key("gc")).Grow(1).Basis(300).MinWidth(0).Gap(8).Children(func() {
						heading(c, theme, locales.MemstatsSectionGc())
						metrics(c, theme, "gc", []metric{
							{label: locales.MemstatsNextGc(), value: bytesOf(s, func(m *memory) uint64 { return m.NextGC })},
							{label: locales.MemstatsGcCount(), value: countOf(s, func(m *memory) uint64 { return m.GCCycles })},
							{label: locales.MemstatsLastGc(), value: value(s, func(s *memstatsservice.MemSnapshot) string { return formatLastGC(s.Runtime) })},
							{label: locales.MemstatsNumGoroutine(), value: countOf(s, func(m *memory) uint64 { return m.Goroutines })},
						})
					})
				})
			})
		})
	}).FillWidth()
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
	if !v.viewed {
		v.viewed = true
		if len(v.Data.Samples) == 0 && !v.Data.Monitoring {
			v.Data.FetchOnce()
		}
	}
	ui.Column(c.Key("memstats")).Fill().Children(func() {
		v.toolbar(c, theme)
		ui.Scroll(c.Key("body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("content")).FillWidth().Gap(8).Padding(8).Children(func() {
				v.summary(c, theme, v.Data.Latest)
				v.chartCard(c, theme)
				v.details(c, theme, v.Data.Latest)
			})
		})
	})
}
