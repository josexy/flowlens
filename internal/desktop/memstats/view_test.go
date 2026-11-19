package memstats

import (
	"math"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/serviceruntime"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type fakeService struct {
	status  memstatsservice.MonitoringStatus
	starts  []int64
	stops   int
	fetches atomic.Int32
	sample  memstatsservice.MemSnapshot
}

func (f *fakeService) GetMemStats() memstatsservice.MemSnapshot {
	f.fetches.Add(1)
	return f.sample
}
func (f *fakeService) GetMonitoringStatus() memstatsservice.MonitoringStatus { return f.status }
func (f *fakeService) StartMonitoring(ms int64)                              { f.starts = append(f.starts, ms) }
func (f *fakeService) StopMonitoring()                                       { f.stops++ }

type fixture struct {
	t       *testing.T
	service *fakeService
	data    *store.MemStatsStore
	bus     *serviceruntime.Bus
	rt      store.Runtime
	window  *runtimetest.Window
	tester  *ui.Tester
	view    *View
	config  nui.AppConfig
}

// openFixture shows the section for the first time; the service answers a
// one-time sample with first.
func openFixture(t *testing.T, status memstatsservice.MonitoringStatus, first memstatsservice.MemSnapshot) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	service := &fakeService{status: status, sample: first}
	data := store.NewMemStatsStore(rt, service)
	bus := serviceruntime.NewBus()
	data.Initialize(bus, rt.Dispatcher.Post)
	t.Cleanup(data.Cleanup)
	// The app reads the monitoring status at startup, before the section shows.
	rt.Jobs.Wait()
	window.Drain()
	config := theme.Resolve(theme.Appearance{}).Config
	view := New(data)
	f := &fixture{t: t, service: service, data: data, bus: bus, rt: rt, window: window, view: view, config: config}
	f.tester = ui.NewTester(func(c *ui.Context) { view.Content(c, nui.App(c, f.config)) }, 1200, 1200)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

// newFixture starts from a section that was shown and then cleared, so it
// holds no samples and takes none on its own.
func newFixture(t *testing.T, status memstatsservice.MonitoringStatus) *fixture {
	t.Helper()
	f := openFixture(t, status, sample(500))
	f.data.Clear()
	f.settle()
	return f
}

func (f *fixture) settle() {
	for range 3 {
		f.rt.Jobs.Wait()
		f.window.Drain()
		f.tester.Frame()
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\n%v", label, err, f.tester.Texts())
	}
	f.settle()
}

// expand opens Go runtime details and lets the animation finish.
func (f *fixture) expand() {
	f.t.Helper()
	f.click(locales.MemstatsDetails())
	f.tester.Advance(time.Second)
	f.settle()
	if !f.view.detailsOpen {
		f.t.Fatal("details did not expand")
	}
}

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

func (f *fixture) shows(text string) bool {
	_, ok := f.tester.Find(text)
	return ok
}

func (f *fixture) want(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if !f.has(text) {
			f.t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

// summaryTexts returns the texts of the summary cards, in order.
func (f *fixture) summaryTexts() []string {
	f.t.Helper()
	texts := f.tester.Texts()
	start := slices.Index(texts, locales.MemstatsMemory())
	end := slices.Index(texts, locales.MemstatsChartTitle())
	if start < 0 || end < start {
		f.t.Fatalf("no summary in %v", texts)
	}
	return texts[start:end]
}

// detailTexts returns the texts after the details' title, in order. The
// title is listed once as the trigger's name and once as its text.
func (f *fixture) detailTexts() []string {
	f.t.Helper()
	texts := f.tester.Texts()
	start := -1
	for index, text := range texts {
		if text == locales.MemstatsDetails() {
			start = index
		}
	}
	if start < 0 {
		f.t.Fatalf("no details in %v", texts)
	}
	return texts[start+1:]
}

const mib = 1 << 20

func sample(timestamp int64) memstatsservice.MemSnapshot {
	return memstatsservice.MemSnapshot{
		Timestamp: timestamp,
		Process:   memstatsservice.ProcessMemory{Bytes: 300 * mib, Measure: memstatsservice.MeasureResident},
		Runtime: memstatsservice.RuntimeMemory{
			Held: 120 * mib, HeapInUse: 40 * mib, KeptForReuse: 50 * mib, Stacks: 2 * mib, Overhead: 28 * mib, Released: 9 * mib,
			NextGC: 80 * mib, GCCycles: 37, Goroutines: 23, LastGC: timestamp, LastPauseNs: 57_000,
		},
	}
}

func (f *fixture) emit(timestamp int64) {
	f.bus.Emit(memstatsservice.MemStatsEventName, sample(timestamp))
	f.settle()
}

func TestFirstViewTakesOneSampleWithoutMonitoring(t *testing.T) {
	f := openFixture(t, memstatsservice.MonitoringStatus{}, sample(1000))
	if got := f.service.fetches.Load(); got != 1 || len(f.service.starts) != 0 || f.data.Monitoring || len(f.data.Samples) != 1 {
		t.Fatalf("first view: fetches %d, starts %v, monitoring %v, samples %d", got, f.service.starts, f.data.Monitoring, len(f.data.Samples))
	}
	if f.has(locales.MemstatsNoData()) || !f.shows(formatBytes(300*mib)) {
		t.Fatalf("first sample is not shown: %v", f.tester.Texts())
	}
	f.want(locales.MemstatsStart())
	for _, width := range []int{558, 820, 1200} {
		f.tester.SetSize(width, 900)
		f.settle()
	}
	f.settle()
	if got := f.service.fetches.Load(); got != 1 || len(f.service.starts) != 0 || len(f.data.Samples) != 1 {
		t.Fatalf("later frames and resizes sampled: fetches %d, starts %v, samples %d", got, f.service.starts, len(f.data.Samples))
	}
}

func TestFirstViewDuringMonitoringTakesNoExtraSample(t *testing.T) {
	f := openFixture(t, memstatsservice.MonitoringStatus{Monitoring: true, IntervalMs: 2000}, sample(1000))
	if got := f.service.fetches.Load(); got != 0 || len(f.data.Samples) != 0 {
		t.Fatalf("fetches %d, samples %d", got, len(f.data.Samples))
	}
}

func TestNoDataAfterClearUntilRefreshOrStart(t *testing.T) {
	f := openFixture(t, memstatsservice.MonitoringStatus{}, sample(1000))
	f.click(locales.MemstatsClear())
	f.want(locales.MemstatsNoData(), locales.MemstatsStart(), locales.MemstatsTitle())
	if got := f.summaryTexts(); !slices.Equal(got, []string{locales.MemstatsMemory(), unknown, locales.MemstatsHeapInuse(), unknown, locales.MemstatsHeapInuseCaption()}) {
		t.Fatalf("summary without samples = %q", got)
	}
	f.tester.SetSize(558, 900)
	f.settle()
	if got := f.service.fetches.Load(); got != 1 || len(f.data.Samples) != 0 || !f.has(locales.MemstatsNoData()) {
		t.Fatalf("cleared section sampled on its own: fetches %d, samples %d", got, len(f.data.Samples))
	}
	f.click(locales.MemstatsRefresh())
	if got := f.service.fetches.Load(); got != 2 || len(f.data.Samples) != 1 || f.has(locales.MemstatsNoData()) {
		t.Fatalf("refresh: fetches %d, samples %d", got, len(f.data.Samples))
	}
	f.click(locales.MemstatsClear())
	f.click(locales.MemstatsStart())
	if len(f.service.starts) != 1 || f.service.fetches.Load() != 2 {
		t.Fatalf("start: starts %v, fetches %d", f.service.starts, f.service.fetches.Load())
	}
}

func TestMemoryCaptionFollowsTheMeasure(t *testing.T) {
	for _, test := range []struct {
		name    string
		process memstatsservice.ProcessMemory
		value   string
		caption string
	}{
		{"windows", memstatsservice.ProcessMemory{Bytes: 300 * mib, Measure: memstatsservice.MeasurePrivateWorkingSet}, formatBytes(300 * mib), locales.MemstatsMemoryWindows()},
		{"macos", memstatsservice.ProcessMemory{Bytes: 300 * mib, Measure: memstatsservice.MeasurePhysFootprint}, formatBytes(300 * mib), locales.MemstatsMemoryMacos()},
		{"linux", memstatsservice.ProcessMemory{Bytes: 300 * mib, Measure: memstatsservice.MeasureResident}, formatBytes(300 * mib), locales.MemstatsMemoryLinux()},
		{"unavailable", memstatsservice.ProcessMemory{Bytes: -1, Measure: memstatsservice.MeasureUnavailable}, unknown, locales.MemstatsMemoryUnavailable()},
		{"unread", memstatsservice.ProcessMemory{Bytes: -1, Measure: memstatsservice.MeasureResident}, unknown, locales.MemstatsMemoryUnavailable()},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, memstatsservice.MonitoringStatus{})
			value := sample(1000)
			value.Process = test.process
			f.data.Add(value)
			f.settle()
			// Exactly two figures; an unavailable Memory never shows a Go value.
			want := []string{locales.MemstatsMemory(), test.value, test.caption, locales.MemstatsHeapInuse(), formatBytes(40 * mib), locales.MemstatsHeapInuseCaption()}
			if got := f.summaryTexts(); !slices.Equal(got, want) {
				t.Fatalf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestChartHoverShowsOneReading(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.emit(1000)
	f.emit(2000)
	plot, ok := f.tester.AccessByRole(ui.RoleGroup, locales.MemstatsChartTitle())
	if !ok {
		t.Fatal("no memory chart")
	}
	f.tester.Move(plot.Bounds.X+plot.Bounds.W/4, plot.Bounds.Y+plot.Bounds.H/2)
	f.tester.Advance(time.Second)
	f.settle()
	f.want(locales.MemstatsMemory() + ": " + formatBytes(300*mib) + " · " + locales.MemstatsHeapInuse() + ": " + formatBytes(40*mib))
	if tip, ok := f.tester.AccessByRole(ui.RoleTooltip, ""); ok {
		t.Fatalf("unexpected chart tooltip: %+v", tip)
	}
	rendertest.Shoot(t, f.tester, "memstats-hover")
}

func TestChartValuesLeaveGapsWithoutProcessMemory(t *testing.T) {
	missing := sample(2000)
	missing.Process = memstatsservice.ProcessMemory{Bytes: -1}
	times, process, heap := chartValues([]memstatsservice.MemSnapshot{sample(1000), missing, sample(3000)})
	if len(times) != 3 || process[0] != 300*mib || !math.IsNaN(process[1]) || process[2] != 300*mib {
		t.Fatalf("process = %v", process)
	}
	if !slices.Equal(heap, []float64{40 * mib, 40 * mib, 40 * mib}) {
		t.Fatalf("heap = %v", heap)
	}

	f := newFixture(t, memstatsservice.MonitoringStatus{})
	for _, value := range []memstatsservice.MemSnapshot{sample(1000), missing, sample(3000)} {
		f.data.Add(value)
	}
	f.settle()
	plot, ok := f.tester.AccessByRole(ui.RoleGroup, locales.MemstatsChartTitle())
	if !ok {
		t.Fatal("no memory chart")
	}
	f.tester.Move(plot.Bounds.X+plot.Bounds.W/2, plot.Bounds.Y+plot.Bounds.H/2)
	f.tester.Advance(time.Second)
	f.settle()
	f.want(locales.MemstatsMemory() + ": missing · " + locales.MemstatsHeapInuse() + ": " + formatBytes(40*mib))
}

func TestChartListsMemoryWhenEveryValueIsUnknown(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	for _, timestamp := range []int64{1000, 2000} {
		value := sample(timestamp)
		value.Process = memstatsservice.ProcessMemory{Bytes: -1}
		f.data.Add(value)
	}
	f.settle()
	memory, heap := 0, 0
	for _, text := range f.tester.Texts() {
		switch text {
		case locales.MemstatsMemory():
			memory++
		case locales.MemstatsHeapInuse():
			heap++
		case "Sys", "StackInuse", "HeapAlloc":
			t.Fatalf("removed series %q is charted", text)
		}
	}
	// Both figures are named by a summary card and a legend entry.
	if memory < 2 || memory != heap || !f.view.chart.SeriesVisible("process") {
		t.Fatalf("memory series is not listed: %v", f.tester.Texts())
	}
}

func TestStartAndPause(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.click(locales.MemstatsStart())
	if len(f.service.starts) != 1 || f.service.starts[0] != store.DefaultMemStatsInterval || !f.data.Monitoring {
		t.Fatal(f.service.starts)
	}
	f.want(locales.MemstatsStop())
	f.click(locales.MemstatsStop())
	if f.service.stops != 1 || f.data.Monitoring {
		t.Fatal(f.service.stops)
	}
}

func TestStatusIsRestored(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{Monitoring: true, IntervalMs: 5000})
	if !f.data.Monitoring || f.data.IntervalMs != 5000 {
		t.Fatal(f.data.Monitoring, f.data.IntervalMs)
	}
	f.want(locales.MemstatsStop(), "5s")
}

func TestChangingIntervalWhileRunningContinuesAtTheNewOne(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{Monitoring: true, IntervalMs: 2000})
	f.view.interval = 500
	f.data.SetInterval(500)
	f.settle()
	if f.data.IntervalMs != 500 || !f.data.Monitoring || len(f.service.starts) != 1 || f.service.starts[0] != 500 {
		t.Fatal(f.service.starts, f.data.IntervalMs)
	}
	f.want("500ms")
}

func TestChangingIntervalWhilePausedDoesNotStart(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.data.SetInterval(1000)
	f.settle()
	if len(f.service.starts) != 0 || f.data.Monitoring {
		t.Fatal(f.service.starts)
	}
	f.click(locales.MemstatsStart())
	if f.service.starts[0] != 1000 {
		t.Fatal(f.service.starts)
	}
}

func TestSampleFillsTheMetrics(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{Monitoring: true, IntervalMs: 2000})
	f.emit(1_700_000_000_000)
	if f.has(locales.MemstatsNoData()) {
		t.Fatal("chart replaces the empty state")
	}
	f.want(formatBytes(300*mib), formatBytes(40*mib), locales.MemstatsMemoryLinux(), locales.MemstatsChartTitle())
	f.expand()
	for _, text := range []string{formatBytes(120 * mib), formatBytes(50 * mib), formatBytes(2 * mib), formatBytes(28 * mib), formatBytes(9 * mib), formatBytes(80 * mib), "37", "23", formatLastGC(sample(1_700_000_000_000).Runtime)} {
		if !f.shows(text) {
			t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

func TestDetailsAreCollapsedByDefault(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.emit(1_700_000_000_000)
	if f.view.detailsOpen || f.has(locales.MemstatsHeld()) || f.has(locales.MemstatsDetailsNote()) {
		t.Fatalf("details are expanded by default: %v", f.tester.Texts())
	}
	f.want(locales.MemstatsDetails(), formatBytes(300*mib), locales.MemstatsChartTitle())

	f.expand()
	want := []string{
		locales.MemstatsDetailsNote(),
		locales.MemstatsSectionMemory(),
		locales.MemstatsHeld(), formatBytes(120 * mib),
		locales.MemstatsHeapInuse(), formatBytes(40 * mib),
		locales.MemstatsKeptForReuse(), formatBytes(50 * mib),
		locales.MemstatsStacks(), formatBytes(2 * mib),
		locales.MemstatsOverhead(), formatBytes(28 * mib),
		locales.MemstatsHeapReleased(), formatBytes(9 * mib),
		locales.MemstatsSectionGc(),
		locales.MemstatsNextGc(), formatBytes(80 * mib),
		locales.MemstatsGcCount(), "37",
		locales.MemstatsLastGc(), formatLastGC(sample(1_700_000_000_000).Runtime),
		locales.MemstatsNumGoroutine(), "23",
	}
	if got := f.detailTexts(); !slices.Equal(got, want) {
		t.Fatalf("details = %q\nwant %q", got, want)
	}
	for _, text := range []string{runtime.Version(), runtime.GOOS + " / " + runtime.GOARCH} {
		if f.has(text) {
			t.Fatalf("details repeat environment information %q", text)
		}
	}
}

func TestNoGarbageCollectionYet(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	value := sample(1000)
	value.Runtime.LastGC, value.Runtime.LastPauseNs, value.Runtime.GCCycles = 0, 0, 0
	f.data.Add(value)
	f.settle()
	f.expand()
	if !f.shows(locales.MemstatsNoGc()) {
		t.Fatalf("no %q in %v", locales.MemstatsNoGc(), f.tester.Texts())
	}
}

func TestRefreshTakesOneSample(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.service.sample = sample(5000)
	f.click(locales.MemstatsRefresh())
	if len(f.data.Samples) != 1 || f.data.Latest.Timestamp != 5000 {
		t.Fatalf("%+v", f.data.Samples)
	}
}

func TestClearEmptiesTheChart(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.emit(1000)
	f.emit(2000)
	f.expand()
	f.click(locales.MemstatsClear())
	if len(f.data.Samples) != 0 || f.data.Latest != nil {
		t.Fatal(f.data.Samples)
	}
	f.want(locales.MemstatsNoData())
	if f.shows(formatBytes(300*mib)) || f.shows(formatBytes(120*mib)) {
		t.Fatal("metrics are cleared")
	}
}

func TestRetainedSamplesAreBounded(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	for index := range store.MaxMemStatsSamples + 1 {
		f.data.Add(sample(int64(index+1) * 1000))
	}
	f.settle()
	if len(f.data.Samples) != store.MaxMemStatsSamples || store.MaxMemStatsSamples != 600 || cap(f.data.Samples) > store.MaxMemStatsSamples {
		t.Fatal(len(f.data.Samples), cap(f.data.Samples))
	}
	for index, kept := range f.data.Samples {
		if want := int64(index+2) * 1000; kept.Timestamp != want {
			t.Fatalf("sample %d at %d, want %d", index, kept.Timestamp, want)
		}
	}
	if f.data.Latest.Timestamp != 601000 {
		t.Fatal(f.data.Latest.Timestamp)
	}
}

func TestSamplesAtTheSameInstantStillDraw(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.emit(1000)
	f.emit(1000)
	f.emit(900)
	f.emit(2000)
	if len(f.data.Samples) != 4 {
		t.Fatal(len(f.data.Samples))
	}
	if times, _, _ := chartValues(f.data.Samples); len(times) != 2 {
		t.Fatal(times)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleGroup, locales.MemstatsChartTitle()); !ok || !f.view.chart.SeriesVisible("heap") {
		t.Fatalf("chart is not drawn: %v", f.tester.Texts())
	}
}

func TestSidebarSelectsTheSection(t *testing.T) {
	selected := 0
	view := &View{Select: func() { selected++ }}
	config := theme.Resolve(theme.Appearance{}).Config
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	tester := ui.NewTester(func(c *ui.Context) { view.Sidebar(c, nui.App(c, config)) }, 300, 200)
	tester.SetLocale("en")
	tester.Frame()
	if err := tester.Click(locales.MenuMemstats()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if selected != 1 {
		t.Fatal(selected)
	}
	tester.Key(ui.Shift, ui.KeyTab)
	tester.Key(0, ui.KeyTab)
	if !tester.Focused(locales.MenuMemstats()) {
		t.Fatal("sidebar entry cannot receive keyboard focus")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Key(0, ui.KeySpace)
	tester.Frame()
	if selected != 3 {
		t.Fatalf("keyboard activation count %d", selected)
	}
}

func TestFormatting(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	if got := formatBytes(1536); got != "1.5 KB" {
		t.Fatal(got)
	}
	if got := formatBytes(-1); got != unknown {
		t.Fatal(got)
	}
	if got := formatPause(57_400); got != "57 μs" {
		t.Fatal(got)
	}
	if got := formatPause(2_400_000); got != "2 ms" {
		t.Fatal(got)
	}
	if got := formatLastGC(memstatsservice.RuntimeMemory{}); got != locales.MemstatsNoGc() {
		t.Fatal(got)
	}
	at := time.Date(2026, 11, 18, 9, 30, 15, 0, time.Local)
	if got := formatLastGC(memstatsservice.RuntimeMemory{LastGC: at.UnixMilli(), LastPauseNs: 57_000}); got != "2026-11-18 09:30:15.000000 · 57 μs pause" {
		t.Fatal(got)
	}
}
