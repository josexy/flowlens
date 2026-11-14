package memstats

import (
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/host"
	memstatsservice "github.com/josexy/flowlens/backend/services/mem_stats_service"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type fakeService struct {
	status memstatsservice.MonitoringStatus
	starts []int64
	stops  int
	sample memstatsservice.MemSnapshot
}

func (f *fakeService) GetMemStats() memstatsservice.MemSnapshot              { return f.sample }
func (f *fakeService) GetMonitoringStatus() memstatsservice.MonitoringStatus { return f.status }
func (f *fakeService) StartMonitoring(ms int64)                              { f.starts = append(f.starts, ms) }
func (f *fakeService) StopMonitoring()                                       { f.stops++ }

type fixture struct {
	t       *testing.T
	service *fakeService
	data    *store.MemStatsStore
	bus     *host.Bus
	rt      store.Runtime
	window  *runtimetest.Window
	tester  *ui.Tester
	view    *View
	config  nui.AppConfig
}

func newFixture(t *testing.T, status memstatsservice.MonitoringStatus) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	service := &fakeService{status: status}
	data := store.NewMemStatsStore(rt, service)
	bus := host.NewBus()
	data.Initialize(bus, rt.Dispatcher.Post)
	t.Cleanup(data.Cleanup)
	config := theme.Resolve(theme.Appearance{}).Config
	view := New(data)
	f := &fixture{t: t, service: service, data: data, bus: bus, rt: rt, window: window, view: view, config: config}
	f.tester = ui.NewTester(func(c *ui.Context) { view.Content(c, nui.App(c, f.config)) }, 1200, 1200)
	f.tester.SetLocale("en")
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

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

func (f *fixture) want(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if !f.has(text) {
			f.t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

func sample(timestamp int64) memstatsservice.MemSnapshot {
	return memstatsservice.MemSnapshot{
		Timestamp: timestamp, GoVersion: "go1.26", GOOS: "linux", GOARCH: "arm64", NumGoroutine: 12, NumCPU: 8,
		HeapAlloc: 2048, HeapSys: 4096, HeapObjects: 77, StackInuse: 1024, StackSys: 2048, Sys: 8192, TotalAlloc: 16384,
		Mallocs: 1_234_567, Frees: 200_000, NumGC: 3, NumForcedGC: 1, GCCPUFraction: 0.001, PauseTotalNs: 2_500_000, PauseNs: 1500, NextGC: 4096,
		LastGC: timestamp,
	}
}

func (f *fixture) emit(timestamp int64) {
	f.bus.Emit(memstatsservice.MemStatsEventName, sample(timestamp))
	f.settle()
}

func TestNoDataBeforeTheFirstSample(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.want(locales.MemstatsNoData(), locales.MemstatsStart(), locales.MemstatsTitle())
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
	f.want(formatBytes(2048), formatBytes(8192), "go1.26", "linux / arm64", "1,034,567", "2.500 ms", "1.5 μs", "0.1000%", "HeapAlloc", "Sys", "StackInuse", locales.MemstatsChartTitle())
}

func TestNoGarbageCollectionYet(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	value := sample(1000)
	value.LastGC = 0
	f.data.Add(value)
	f.settle()
	f.want(locales.MemstatsNoGc())
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
	f.click(locales.MemstatsClear())
	if len(f.data.Samples) != 0 || f.data.Latest != nil {
		t.Fatal(f.data.Samples)
	}
	f.want(locales.MemstatsNoData())
	if f.has("go1.26") {
		t.Fatal("metrics are cleared")
	}
}

func TestRetainedSamplesAreBounded(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	for index := range 500 {
		f.data.Add(sample(int64(index+1) * 1000))
	}
	f.settle()
	if len(f.data.Samples) != store.MaxMemStatsSamples || cap(f.data.Samples) > store.MaxMemStatsSamples {
		t.Fatal(len(f.data.Samples), cap(f.data.Samples))
	}
	if f.data.Samples[0].Timestamp != 441000 || f.data.Latest.Timestamp != 500000 {
		t.Fatal(f.data.Samples[0].Timestamp)
	}
}

func TestSamplesAtTheSameInstantStillDraw(t *testing.T) {
	f := newFixture(t, memstatsservice.MonitoringStatus{})
	f.emit(1000)
	f.emit(1000)
	f.emit(900)
	f.emit(2000)
	f.want("HeapAlloc")
	if len(f.data.Samples) != 4 {
		t.Fatal(len(f.data.Samples))
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
	if got := formatNs(0); got != "0" {
		t.Fatal(got)
	}
	if got := formatNs(999); got != "999 ns" {
		t.Fatal(got)
	}
	if got := groupDigits(1000); got != "1,000" {
		t.Fatal(got)
	}
	if got := groupDigits(999); got != "999" {
		t.Fatal(got)
	}
	if got := formatBytes(1536); got != "1.50 KB" {
		t.Fatal(got)
	}
}
