package capture

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/database"
	"github.com/josexy/flowlens/internal/serviceruntime"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type fakeBackend struct {
	mu       sync.Mutex
	entries  []*logic.TrafficEntry
	stats    proxyservice.TrafficStatistics
	bodies   map[uint64]*proxyservice.TrafficBodyView
	readBody func(uint64) (*proxyservice.TrafficBodyView, error)
	deleted  []int64
	cleared  int
	status   proxyservice.ProxyStatus
	system   proxyservice.SystemProxyStatus
	dns      proxyservice.DNSCacheStatus
	startErr error
	starts   int
	stops    int
	restarts []bool
	alias    string
	dnsClear int
}

func (f *fakeBackend) GetTraffic() []*logic.TrafficEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries)
}
func (f *fakeBackend) GetStatistics() proxyservice.TrafficStatistics {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}
func (f *fakeBackend) ClearTraffic() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return nil
}
func (f *fakeBackend) DeleteTraffic(ids []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids...)
}
func (f *fakeBackend) SetLiveTrafficDetail(uint64) {}
func (f *fakeBackend) GetTrafficBodyView(id uint64) (*proxyservice.TrafficBodyView, error) {
	f.mu.Lock()
	if read := f.readBody; read != nil {
		f.mu.Unlock()
		return read(id)
	}
	defer f.mu.Unlock()
	if view, ok := f.bodies[id]; ok {
		return view, nil
	}
	return &proxyservice.TrafficBodyView{}, nil
}
func (f *fakeBackend) Start() (proxyservice.ProxyStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.startErr != nil {
		return proxyservice.ProxyStatus{}, f.startErr
	}
	f.status = proxyservice.ProxyStatus{Running: true, Address: "127.0.0.1:8080"}
	return f.status, nil
}
func (f *fakeBackend) Stop() (proxyservice.ProxyStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.status = proxyservice.ProxyStatus{}
	return f.status, nil
}
func (f *fakeBackend) GetStatus() proxyservice.ProxyStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}
func (f *fakeBackend) GetSystemProxyStatus() proxyservice.SystemProxyStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.system
}
func (f *fakeBackend) SetSystemProxyEnabled(enabled bool) (proxyservice.SystemProxyStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.system.Active, f.system.Address = enabled, "127.0.0.1:8080"
	return f.system, nil
}
func (f *fakeBackend) GetDNSCacheStatus() proxyservice.DNSCacheStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dns
}
func (f *fakeBackend) ClearDNSCache() proxyservice.DNSCacheStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dnsClear++
	return f.dns
}
func (f *fakeBackend) RestartCapture(save bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts = append(f.restarts, save)
	return nil
}
func (f *fakeBackend) UpdateHistoryAlias(alias string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alias = alias
}
func (f *fakeBackend) ApplyCurrentProxyConfig() (proxyservice.ProxyConfigApplyResult, error) {
	return proxyservice.ProxyConfigApplyResult{Applied: true}, nil
}

func firstHistory(history []store.HistoryBackend) store.HistoryBackend {
	if len(history) > 0 {
		return history[0]
	}
	return nil
}

type resendCall struct {
	id     uint64
	config proxyservice.ResendConfig
}

type fixture struct {
	t              testing.TB
	backend        *fakeBackend
	data           *store.Store
	window         *runtimetest.Window
	workspace      *Workspace
	tester         *ui.Tester
	notifier       *widgets.Notifier
	confirm        *widgets.ConfirmDialog
	dispatch       *shortcuts.Dispatcher
	service        *settingservice.SettingService
	mu             sync.Mutex
	resends        []resendCall
	resendErr      error
	block          chan struct{}
	opened         []uint64
	exported       []string
	showCategories bool
	pickedName     string
	savePath       string
	// afterBuild, when set, runs at the end of each build of the view.
	afterBuild func()
}

func (f *fixture) settle() {
	for range 4 {
		f.data.Runtime.Jobs.Wait()
		f.window.Drain()
		f.tester.Frame()
	}
}

func entry(id uint64, host string) *logic.TrafficEntry {
	return &logic.TrafficEntry{ID: id, Revision: 1, Type: "http", Method: "GET", Host: host, Path: fmt.Sprintf("/p%d", id), URL: fmt.Sprintf("http://%s/p%d", host, id)}
}

func newFixtureSized(t testing.TB, entries []*logic.TrafficEntry, width, height int, history ...store.HistoryBackend) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := settingservice.New(db)
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{entries: entries, bodies: map[uint64]*proxyservice.TrafficBodyView{}, system: proxyservice.SystemProxyStatus{Supported: true, ModeSupported: true}, dns: proxyservice.DNSCacheStatus{TTLSeconds: 60}}
	backend.stats.Total = int64(len(entries))
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	data := store.New(rt, store.Dependencies{
		Settings: store.SettingsDependencies{Backend: service, Proxy: backend},
		Traffic:  backend, Proxy: backend, History: firstHistory(history), Requests: requestStub{},
	})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	notifier := &widgets.Notifier{}
	confirm := &widgets.ConfirmDialog{}
	dispatch := shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig { return nil })
	f := &fixture{t: t, backend: backend, data: data, window: window, notifier: notifier, confirm: confirm, dispatch: dispatch, service: service}
	workspace := New(data, Host{
		Resend: func(ctx context.Context, id uint64, config proxyservice.ResendConfig) (proxyservice.ResendResult, error) {
			f.mu.Lock()
			f.resends = append(f.resends, resendCall{id, config})
			block, err := f.block, f.resendErr
			f.mu.Unlock()
			if block != nil {
				select {
				case <-block:
				case <-ctx.Done():
					return proxyservice.ResendResult{}, ctx.Err()
				}
			}
			if err != nil {
				return proxyservice.ResendResult{}, err
			}
			return proxyservice.ResendResult{Success: config.Count}, nil
		},
		OpenEditor: func(entry *logic.TrafficEntry, _ *proxyservice.TrafficBodyView, _ bool, _ string, done func(error)) {
			f.opened = append(f.opened, entry.ID)
			done(nil)
		},
		Export: f.exportHost(),
	}, dispatch, notifier, confirm, nil)
	f.workspace = workspace
	view := func(c *ui.Context) {
		theme := nui.App(c, data.Settings.Theme().Config)
		dispatch.Begin()
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(240).FillHeight().Shrink(0).Children(func() {
				if f.showCategories {
					workspace.CategorySidebar(c, theme)
					return
				}
				workspace.Sidebar(c, theme)
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() { workspace.Content(c, theme) })
		})
		notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 3})
		if confirm.Open() {
			dispatch.NoteModal()
		}
		confirm.Build(c, theme)
		dispatch.Install(c)
		if f.afterBuild != nil {
			f.afterBuild()
		}
	}
	f.tester = ui.NewTester(view, width, height)
	f.tester.SetLocale("en")
	t.Cleanup(rt.Close)
	data.Proxy.Initialize(nopBus{})
	data.Traffic.Initialize(nil)
	data.Filter.Refresh()
	if data.History != nil {
		data.History.LoadList()
	}
	f.settle()
	return f
}

type requestStub struct{}

func (requestStub) SendHTTPRequest(context.Context, proxyservice.SendRequestConfig, string, string, []logic.HeaderField, proxyservice.SendRequestBody) (proxyservice.SendRequestResponse, error) {
	return proxyservice.SendRequestResponse{}, nil
}
func (requestStub) DisconnectHTTPRequestStream(string) error { return nil }
func (requestStub) ConnectWebSocket(context.Context, proxyservice.WebSocketConnectRequest) (proxyservice.WebSocketConnectResponse, error) {
	return proxyservice.WebSocketConnectResponse{}, nil
}
func (requestStub) SendWebSocketMessage(proxyservice.WebSocketSendRequest) error { return nil }
func (requestStub) DisconnectWebSocket(string) error                             { return nil }
func (requestStub) RecoverRequestBodyForEditing(string, []logic.HeaderField, proxyservice.TrafficBodyView) (proxyservice.RequestBodyRecoveryResult, error) {
	return proxyservice.RequestBodyRecoveryResult{}, nil
}

type nopBus struct{}

func (nopBus) Emit(string, ...any) bool                     { return true }
func (nopBus) On(string, func(serviceruntime.Event)) func() { return func() {} }

func resendConfirmLabel() string { return locales.ResendModalConfirm() }

func newFixture(t *testing.T, entries ...*logic.TrafficEntry) *fixture {
	t.Helper()
	return newFixtureSized(t, entries, 1400, 800)
}

func (f *fixture) exportHost() *ExportHost {
	return &ExportHost{
		Generation: func() uint64 { return 3 },
		ExportLive: func(_ context.Context, request proxyservice.TrafficExportRequest) (proxyservice.TrafficExportResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.exported = append(f.exported, fmt.Sprintf("%s:%s:%s:%v:%d", request.Kind, request.TargetType, request.Path, request.TrafficIDs, *request.CaptureGeneration))
			return proxyservice.TrafficExportResult{Exported: len(request.TrafficIDs)}, nil
		},
		ExportHistory: func(_ context.Context, request historyservice.TrafficExportRequest) (proxyservice.TrafficExportResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.exported = append(f.exported, fmt.Sprintf("history:%s:%s:%s", request.Key, request.Kind, request.Path))
			return proxyservice.TrafficExportResult{Exported: 1}, nil
		},
		ExportLiveHAR: func(request proxyservice.HARExportRequest) (proxyservice.HARWriteResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.exported = append(f.exported, fmt.Sprintf("har:%s:%v", request.Path, request.TrafficIDs))
			return proxyservice.HARWriteResult{Exported: 1}, nil
		},
		ExportHistoryHAR: func(request historyservice.HARExportRequest) (proxyservice.HARWriteResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.exported = append(f.exported, fmt.Sprintf("history-har:%s:%s", request.Key, request.Path))
			return proxyservice.HARWriteResult{Exported: 1}, nil
		},
		PickSavePath: func(filename, _, _ string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.pickedName = filename
			if f.savePath != "" {
				return f.savePath, nil
			}
			return "/tmp/x", nil
		},
		PickDirectory: func(string) (string, error) { return "/tmp/dir", nil },
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\ntexts: %v", label, err, f.tester.Texts())
	}
	f.settle()
}

func (f *fixture) requireText(text string) {
	f.t.Helper()
	if !f.tester.HasText(text) {
		f.t.Fatalf("missing %q in %v", text, f.tester.Texts())
	}
}

func (f *fixture) requireNoText(text string) {
	f.t.Helper()
	if f.tester.HasText(text) {
		f.t.Fatalf("unexpected %q in %v", text, f.tester.Texts())
	}
}

func (f *fixture) deliver(entries ...*logic.TrafficEntry) {
	for _, item := range entries {
		f.data.Traffic.AddOrUpdate(item)
	}
	f.data.Traffic.FlushPending()
	f.settle()
}

func TestFilterTabsNarrowRows(t *testing.T) {
	json := entry(1, "json.test")
	json.Response = &proxyservice.HTTPMessage{HeaderFields: []logic.HeaderField{{Name: "Content-Type", Value: "application/json"}}}
	html := entry(2, "html.test")
	html.Response = &proxyservice.HTTPMessage{HeaderFields: []logic.HeaderField{{Name: "Content-Type", Value: "text/html"}}}
	f := newFixture(t, json, html)
	f.requireText("json.test")
	f.requireText("html.test")
	f.click("JSON")
	f.requireText("json.test")
	f.requireNoText("html.test")
	f.click("HTML")
	f.requireNoText("json.test")
	f.requireText("html.test")
	f.click("All")
	f.requireText("json.test")
}

func TestSearchNarrowsRowsAndKeepsTotal(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
	f.data.Filter.SearchText = "b.test"
	f.settle()
	f.requireNoText("a.test")
	f.requireText("b.test")
	if f.data.Traffic.Statistics.Total != 2 {
		t.Fatalf("total %d", f.data.Traffic.Statistics.Total)
	}
}

func TestFocusFilterShortcut(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	if !f.dispatch.Invoke(shortcuts.CaptureFocusFilter) {
		t.Fatal("shortcut did not run")
	}
	f.settle()
	f.tester.Type("needle")
	f.settle()
	if f.data.Filter.SearchText != "needle" {
		t.Fatalf("search %q", f.data.Filter.SearchText)
	}
}

func TestStartAndStop(t *testing.T) {
	f := newFixture(t)
	f.requireText("Proxy not running")
	f.click("Start")
	if f.backend.starts != 1 || !f.data.Proxy.Status.Running {
		t.Fatal("not started")
	}
	f.requireText("http://127.0.0.1:8080")
	f.click("Stop")
	if f.backend.stops != 1 || f.data.Proxy.Status.Running {
		t.Fatal("not stopped")
	}
	f.requireText("Proxy not running")
}

func TestStartFailureStaysStopped(t *testing.T) {
	f := newFixture(t)
	f.backend.startErr = errors.New("port in use")
	f.click("Start")
	f.requireText("port in use")
	f.requireText("Proxy not running")
	if f.data.Proxy.LastError == "" {
		t.Fatal("error not recorded")
	}
}

func TestProxyDisplayAddress(t *testing.T) {
	if got := ProxyDisplayAddress("1.2.3.4:1", settingservice.ProxyModeSOCKS5); got != "socks5://1.2.3.4:1" {
		t.Fatal(got)
	}
	if got := ProxyDisplayAddress("http://x", settingservice.ProxyModeSOCKS5); got != "http://x" {
		t.Fatal(got)
	}
}

func TestHostMenuRefreshesWithoutBlocking(t *testing.T) {
	f := newFixture(t)
	responses := make(chan []settingservice.LocalIPAddress, 1)
	defer close(responses)
	var calls atomic.Int32
	f.workspace.Host.ListLocalIPs = func() []settingservice.LocalIPAddress {
		calls.Add(1)
		select {
		case options := <-responses:
			return options
		case <-time.After(2 * time.Second):
			t.Error("address lookup did not receive its result while the menu was open")
			return nil
		}
	}
	openMenu := func() {
		t.Helper()
		if err := f.tester.Click(locales.ToolbarQuickHost("127.0.0.1")); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			f.tester.Frame()
		}
	}

	openMenu()
	f.requireText("ALL")
	f.requireText("127.0.0.1")
	first := settingservice.LocalIPAddress{Value: "192.0.2.1", Label: "192.0.2.1 (Ethernet)"}
	responses <- []settingservice.LocalIPAddress{first}
	f.settle()
	f.requireText(first.Label)
	if got := calls.Load(); got != 1 {
		t.Fatalf("address lookups after one opening and several frames = %d, want 1", got)
	}

	menutest.Close(f.tester, f.settle)
	openMenu()
	f.requireText(first.Label)
	updated := settingservice.LocalIPAddress{Value: "192.0.2.2", Label: "192.0.2.2 (Ethernet)"}
	responses <- []settingservice.LocalIPAddress{updated}
	f.settle()
	f.requireText(updated.Label)
	f.requireNoText(first.Label)
	if err := menutest.Choose(f.tester, f.settle, updated.Label); err != nil {
		t.Fatal(err)
	}
	if got := f.data.Settings.Current.ProxyConfig.Host; got != updated.Value {
		t.Fatalf("saved host = %q, want %q", got, updated.Value)
	}
}

func TestPortValidation(t *testing.T) {
	for _, value := range []float64{0, 65536, 1.5} {
		if validPort(nui.Set(value)) {
			t.Fatalf("%v accepted", value)
		}
	}
	if validPort(nui.Optional[float64]{}) || !validPort(nui.Set[float64](8080)) {
		t.Fatal("bounds")
	}
}

func TestInvalidPortIsRejectedAndSavedPortUnchanged(t *testing.T) {
	f := newFixture(t)
	saved := f.data.Settings.Current.ProxyConfig.Port
	f.click("Proxy port: 8080")
	f.requireText("Port")
	f.workspace.toolbar.portValue = nui.Set[float64](70000)
	f.settle()
	f.requireText("Enter a port between 1 and 65535")
	if err := f.tester.Click("Save"); err == nil {
		f.settle()
	}
	if f.data.Settings.Current.ProxyConfig.Port != saved {
		t.Fatalf("port %d", f.data.Settings.Current.ProxyConfig.Port)
	}
}

func TestValidPortSaves(t *testing.T) {
	f := newFixture(t)
	f.click("Proxy port: 8080")
	f.workspace.toolbar.portValue = nui.Set[float64](9090)
	f.settle()
	f.click("Save")
	if f.data.Settings.Current.ProxyConfig.Port != 9090 {
		t.Fatalf("port %d", f.data.Settings.Current.ProxyConfig.Port)
	}
	if f.workspace.toolbar.portOpen {
		t.Fatal("popover should close")
	}
}

func TestBooleanToggleSaves(t *testing.T) {
	f := newFixture(t)
	f.click("Disable HTTP/2")
	if !f.data.Settings.Current.ProxyConfig.DisableHTTP2 {
		t.Fatal("not saved")
	}
	f.click("Disable HTTP/2")
	if f.data.Settings.Current.ProxyConfig.DisableHTTP2 {
		t.Fatal("not turned off")
	}
}

func (f *fixture) requireTextLabel(label string) {
	f.t.Helper()
	if _, ok := f.tester.Find(label); !ok {
		f.t.Fatalf("missing %q", label)
	}
}

func TestDomainFiltersSaveAndShowCount(t *testing.T) {
	f := newFixture(t)
	f.click("Domain filters: 0")
	f.requireText("Domain Filters")
	dialog := &f.workspace.toolbar.hosts
	dialog.add(&dialog.include)
	dialog.include[0].value = " *.a.test "
	dialog.add(&dialog.exclude)
	dialog.add(&dialog.exclude)
	dialog.exclude[1].value = "b.test"
	f.settle()
	f.click("Save")
	config := f.data.Settings.Current.ProxyConfig
	if !slices.Equal(config.IncludeHosts, []string{"*.a.test"}) || !slices.Equal(config.ExcludeHosts, []string{"b.test"}) {
		t.Fatalf("%v %v", config.IncludeHosts, config.ExcludeHosts)
	}
	f.requireTextLabel("Domain filters: 2")
}

func TestDNSValidationBounds(t *testing.T) {
	for value, want := range map[float64]bool{0: false, 1: true, 3600: true, 3601: false, 1.5: false} {
		if got := validTTL(nui.Set(value)); got != want {
			t.Errorf("%v: %v", value, got)
		}
	}
}

func TestDNSInvalidDurationKeepsConfiguration(t *testing.T) {
	f := newFixture(t)
	f.click("DNS Cache")
	f.workspace.toolbar.dnsTTL = nui.Set[float64](4000)
	f.settle()
	f.requireText("3600")
	if err := f.tester.Click("Apply"); err == nil {
		f.settle()
	}
	if f.data.Settings.Current.ProxyConfig.DNSCacheTTLSeconds == 4000 {
		t.Fatal("invalid duration saved")
	}
}

func TestDNSApplyAndClear(t *testing.T) {
	f := newFixture(t)
	f.click("DNS Cache")
	f.workspace.toolbar.dnsEnabled = true
	f.workspace.toolbar.dnsTTL = nui.Set[float64](120)
	f.settle()
	f.click("Clear cache")
	if f.backend.dnsClear != 1 {
		t.Fatal("not cleared")
	}
	f.click("Apply")
	config := f.data.Settings.Current.ProxyConfig
	if !config.DNSCacheEnabled || config.DNSCacheTTLSeconds != 120 {
		t.Fatalf("%+v", config)
	}
}

func TestSystemProxyAbsentWhenUnsupported(t *testing.T) {
	f := newFixture(t)
	f.data.Proxy.SystemProxy = proxyservice.SystemProxyStatus{Supported: false}
	f.settle()
	if _, ok := f.tester.Find("System proxy: off"); ok {
		t.Fatal("control shown on Linux")
	}
}

func TestSystemProxyDisabledForUnsupportedMode(t *testing.T) {
	f := newFixture(t)
	f.data.Proxy.SystemProxy = proxyservice.SystemProxyStatus{Supported: true, ModeSupported: false}
	f.settle()
	label := "System proxy is not supported for HTTP mode on this platform"
	f.requireTextLabel(label)
	_ = f.tester.Click(label)
	f.settle()
	if f.data.Proxy.SystemProxy.Active {
		t.Fatal("disabled control acted")
	}
}

func TestSystemProxyToggle(t *testing.T) {
	f := newFixture(t)
	f.click("System proxy: off")
	if !f.data.Proxy.SystemProxy.Active {
		t.Fatal("not on")
	}
	f.requireTextLabel("System proxy: on (127.0.0.1:8080)")
}

func TestSortCycleAndIncompleteValues(t *testing.T) {
	complete := entry(2, "b.test")
	complete.Request = &proxyservice.HTTPMessage{Metrics: &proxyservice.HTTPMessageMetrics{StartedAtMicros: 1, EndedAtMicros: 2}}
	complete.Response = &proxyservice.HTTPMessage{Metrics: &proxyservice.HTTPMessageMetrics{StartedAtMicros: 3, EndedAtMicros: 9}}
	f := newFixture(t, entry(1, "a.test"), complete, entry(3, "c.test"))
	table := f.workspace.Table
	// The default path column preserves its readable width, so later columns
	// are reached through the table's native horizontal scrolling.
	f.tester.Scroll(700, 150, 400, 0)
	f.settle()
	f.click("Duration")
	if table.state.Sort.Column != "duration" || table.state.Sort.Descending {
		t.Fatalf("first click %+v", table.state.Sort)
	}
	if got := rowIDs(table); !slices.Equal(got, []uint64{2, 1, 3}) {
		t.Fatal(got)
	}
	f.click("Duration")
	if !table.state.Sort.Descending {
		t.Fatalf("second click %+v", table.state.Sort)
	}
	if got := rowIDs(table); !slices.Equal(got, []uint64{2, 1, 3}) {
		t.Fatal(got)
	}
	f.click("Duration")
	if table.state.Sort.Column != "" {
		t.Fatalf("third click %+v", table.state.Sort)
	}
	if got := rowIDs(table); !slices.Equal(got, []uint64{1, 2, 3}) {
		t.Fatal(got)
	}
}

func rowIDs(table *Table) []uint64 {
	ids := make([]uint64, len(table.rows))
	for index, row := range table.rows {
		ids[index] = row.ID
	}
	return ids
}

func TestHidingColumnsPersistsAndClearsItsSort(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	table := f.workspace.Table
	f.click("Host")
	if table.state.Sort.Column != "host" {
		t.Fatal("sort")
	}
	if !f.data.Settings.SetTrafficColumnVisible(logic.ColumnHost, false, nil) {
		t.Fatal("hide")
	}
	f.settle()
	f.requireNoText("a.test")
	if table.state.Sort.Column != "" {
		t.Fatalf("sort stayed on a hidden column: %+v", table.state.Sort)
	}
	snapshot, err := f.service.Snapshot()
	if err != nil || !slices.Contains(snapshot.TrafficTableConfig.HiddenColumns, "host") {
		t.Fatalf("%v %v", snapshot.TrafficTableConfig, err)
	}
}

func TestColumnLayoutPersists(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	table := f.workspace.Table
	order := slices.Clone(table.state.List.Columns.Order)
	order[0], order[1] = order[1], order[0]
	table.state.List.Columns.Order = order
	table.state.List.Columns.Widths = map[string]float32{"host": 333}
	f.settle()
	snapshot, err := f.service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	config := snapshot.TrafficTableConfig
	if config.ColumnOrder[0] != "method" || config.ColumnWidths["host"] != 333 {
		t.Fatalf("%+v", config)
	}
	restored := NewTable(table.source, table.deps)
	restored.loadLayout()
	if restored.state.List.Columns.Order[0] != "method" || restored.state.List.Columns.Widths["host"] != 333 {
		t.Fatalf("%+v", restored.state.List.Columns)
	}
}

func TestHeaderMenuHidesColumn(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	if err := menutest.Open(f.tester, f.settle, "Protocol"); err != nil {
		t.Fatal(err)
	}
	f.choose("Protocol")
	if !slices.Contains(f.data.Settings.HiddenTrafficColumns(), "protocol") {
		t.Fatalf("%v", f.data.Settings.HiddenTrafficColumns())
	}
	// As 1.0.5's, the menu stays open to choose another column.
	if !menutest.Shown(f.tester) {
		t.Fatal("the column menu closed")
	}
	if item, ok := menutest.Item(f.tester, "Protocol"); !ok || item.Checked {
		t.Fatalf("Protocol %+v", item)
	}
}

// clickRow clicks the row of host with mods.
func (f *fixture) clickRow(mods ui.Modifiers, host string) {
	f.t.Helper()
	if err := f.tester.ClickWith(mods, host); err != nil {
		f.t.Fatalf("click %q: %v", host, err)
	}
	f.settle()
}

// selectedIDs returns the IDs of the rows selected, in table order.
func selectedIDs(table *Table) []uint64 {
	var ids []uint64
	for _, entry := range table.selectedEntries() {
		ids = append(ids, entry.ID)
	}
	return ids
}

// requireSelection checks the rows selected, the entry focused (0 for none),
// and whether the detail panel is open.
func (f *fixture) requireSelection(ids []uint64, focused uint64, detail bool) {
	f.t.Helper()
	table := f.workspace.Table
	got := selectedIDs(table)
	var focus uint64
	if f.data.Traffic.Selected != nil {
		focus = f.data.Traffic.Selected.ID
	}
	if !slices.Equal(got, ids) || focus != focused || f.data.Traffic.ShowDetailPanel != detail || f.data.Traffic.SelectedCount != len(ids) {
		f.t.Fatalf("selected %v focused %d detail %v count %d, want %v %d %v", got, focus, f.data.Traffic.ShowDetailPanel, f.data.Traffic.SelectedCount, ids, focused, detail)
	}
}

func TestClickSelectsWithoutOpeningDetails(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.clickRow(0, "host2.test")
	f.requireSelection([]uint64{2}, 2, false)
}

func (f *fixture) openDetails(host string) {
	f.t.Helper()
	f.clickRow(0, host)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
}

func TestClickWhileDetailsAreOpenMovesThem(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.openDetails("host1.test")
	f.requireSelection([]uint64{1}, 1, true)
	f.clickRow(0, "host3.test")
	f.requireSelection([]uint64{3}, 3, true)
}

func TestClickingTheOnlySelectedRowKeepsIt(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.openDetails("host2.test")
	f.clickRow(0, "host2.test")
	f.requireSelection([]uint64{2}, 2, true)
}

func TestPrimaryClickAddsAndRemovesRows(t *testing.T) {
	f := newFixture(t, manyEntries(9)...)
	f.clickRow(0, "host2.test")
	f.clickRow(ui.Cmd, "host5.test")
	f.clickRow(ui.Cmd, "host8.test")
	f.requireSelection([]uint64{2, 5, 8}, 8, false)
	f.clickRow(ui.Cmd, "host8.test")
	f.requireSelection([]uint64{2, 5}, 2, false)
	f.clickRow(ui.Cmd, "host5.test")
	f.requireSelection([]uint64{2}, 2, false)
}

func TestPrimaryClickRemovingTheLastRowClosesDetails(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.openDetails("host3.test")
	f.clickRow(ui.Cmd, "host3.test")
	f.requireSelection(nil, 0, false)
}

func TestShiftClickSelectsRange(t *testing.T) {
	f := newFixture(t, manyEntries(9)...)
	f.clickRow(0, "host2.test")
	f.clickRow(ui.Shift, "host5.test")
	f.requireSelection([]uint64{2, 3, 4, 5}, 5, false)
	f.clickRow(ui.Shift, "host3.test")
	f.requireSelection([]uint64{2, 3}, 3, false)
}

func TestKeyboardSelectionLeavesDetailsAsTheyWere(t *testing.T) {
	f := newFixture(t, manyEntries(5)...)
	f.clickRow(0, "host2.test")
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	f.requireSelection([]uint64{3}, 3, false)
	f.tester.Key(ui.Shift, ui.KeyDown)
	f.settle()
	f.requireSelection([]uint64{3, 4}, 4, false)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.requireSelection([]uint64{3, 4}, 4, true)
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	f.requireSelection([]uint64{5}, 5, true)
}

func TestClickAfterTheTableScrollsUnderAStillPointer(t *testing.T) {
	f := newFixture(t, manyEntries(200)...)
	table := f.workspace.Table
	table.state.List.ScrollTo(0, ui.Start)
	f.settle()
	row, ok := f.tester.Find("host5.test")
	if !ok {
		t.Fatal("row 5")
	}
	x, y := row.X+row.W/2, row.Y+row.H/2
	f.tester.Move(x, y)
	f.settle()
	table.state.List.ScrollTo(40, ui.Start)
	f.settle()
	var under uint64
	for _, entry := range table.rows {
		if r, ok := f.tester.Find(fmt.Sprintf("host%d.test", entry.ID)); ok && y >= r.Y && y < r.Y+r.H {
			under = entry.ID
			break
		}
	}
	if under == 0 || under == 5 {
		t.Fatalf("row under the pointer %d", under)
	}
	f.tester.ClickAt(x, y)
	f.settle()
	f.requireSelection([]uint64{under}, under, false)
}

func TestPressOnAHeaderSelectsNothing(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.clickRow(0, "host2.test")
	f.click("Host")
	f.requireSelection([]uint64{2}, 2, false)
}

// rowCenter returns the center of the row showing host.
func (f *fixture) rowCenter(host string) (x, y float32) {
	f.t.Helper()
	r, ok := f.tester.Find(host)
	if !ok {
		f.t.Fatalf("row %s", host)
	}
	return r.X + r.W/2, r.Y + r.H/2
}

func TestQuickDragUsesNativeAnchor(t *testing.T) {
	f := newFixture(t, manyEntries(9)...)
	f.tester.Press(f.rowCenter("host3.test"))
	x, y := f.rowCenter("host6.test")
	f.tester.Move(x, y)
	f.tester.Release(x, y)
	f.settle()
	f.requireSelection([]uint64{3, 4, 5, 6}, 6, false)
}

func TestReleaseOutsideTheTableLeavesTheKeysWorking(t *testing.T) {
	f := newFixture(t, manyEntries(6)...)
	f.clickRow(0, "host2.test")
	_, y := f.rowCenter("host4.test")
	// The table begins where the 240 DIP sidebar ends: the release lands
	// on the sidebar, short of a drag.
	f.tester.Press(241, y)
	f.tester.Move(238, y)
	f.tester.Release(238, y)
	f.settle()
	f.requireSelection([]uint64{4}, 4, false)
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	f.requireSelection([]uint64{5}, 5, false)
}

func TestLosingTheWindowFocusDuringAPressLeavesTheKeysWorking(t *testing.T) {
	f := newFixture(t, manyEntries(6)...)
	f.clickRow(0, "host2.test")
	f.tester.Press(f.rowCenter("host4.test"))
	f.tester.SetFocused(false)
	f.tester.SetFocused(true)
	f.settle()
	f.requireSelection([]uint64{4}, 4, false)
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	f.requireSelection([]uint64{5}, 5, false)
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.settle()
	f.requireSelection([]uint64{1, 2, 3, 4, 5, 6}, 5, false)
}

func TestAReleaseAfterAPressElsewhereClicksNoRow(t *testing.T) {
	f := newFixture(t, manyEntries(6)...)
	f.clickRow(0, "host2.test")
	// A press on row 4 whose release the table never sees.
	_, y := f.rowCenter("host4.test")
	f.tester.Press(241, y)
	f.tester.Move(238, y)
	f.tester.Release(238, y)
	f.settle()
	// A press on the sidebar, released over the table.
	x, ry := f.rowCenter("host5.test")
	f.tester.Press(100, ry)
	f.tester.Move(x, ry)
	f.tester.Release(x, ry)
	f.settle()
	f.requireSelection([]uint64{4}, 4, false)
}

func TestEvictionFocusesTheFirstRowLeftInTableOrder(t *testing.T) {
	f := newFixture(t, entry(1, "d.test"), entry(2, "b.test"), entry(3, "a.test"), entry(4, "c.test"))
	table := f.workspace.Table
	table.state.Sort = ui.SortOrder{Column: string(logic.ColumnHost)}
	table.lastSort = table.state.Sort
	f.settle()
	f.clickRow(0, "d.test")
	f.clickRow(ui.Cmd, "c.test")
	f.clickRow(ui.Cmd, "b.test")
	f.requireSelection([]uint64{2, 4, 1}, 2, false)
	// Row 4 comes before row 1 by host, though its ID is larger.
	f.data.Traffic.Delete([]uint64{2})
	f.settle()
	f.requireSelection([]uint64{4, 1}, 4, false)
}

func TestSwitchingHistoriesClearsTheSelection(t *testing.T) {
	backend := &fakeHistoryBackend{
		list: []*proxyservice.HistoryMetadata{
			{Key: "k1", Alias: "first-session", CreatedAt: 1700000000000, Total: 3, FormatVersion: 2},
			{Key: "k2", Alias: "second-session", CreatedAt: 1700000001000, Total: 3, FormatVersion: 2},
		},
		// Each session numbers its entries from 1.
		entries: map[string][]*logic.TrafficEntry{
			"k1": {entry(1, "a1.test"), entry(2, "a2.test"), entry(3, "a3.test")},
			"k2": {entry(1, "b1.test"), entry(2, "b2.test"), entry(3, "b3.test")},
		},
	}
	h := newFixtureSized(t, nil, 1400, 800, backend)
	table := h.workspace.HistoryTable
	h.click("first-session")
	h.openDetails("a2.test")
	if ids := selectedIDs(table); !slices.Equal(ids, []uint64{2}) || !h.data.History.Traffic.ShowDetailPanel {
		t.Fatalf("first session: selected %v detail %v", ids, h.data.History.Traffic.ShowDetailPanel)
	}
	h.click("second-session")
	h.settle()
	if ids := selectedIDs(table); len(ids) != 0 || h.data.History.Traffic.Selected != nil || h.data.History.Traffic.SelectedCount != 0 || h.data.History.Traffic.ShowDetailPanel {
		t.Fatalf("second session: selected %v focus %v count %d detail %v", ids, h.data.History.Traffic.Selected, h.data.History.Traffic.SelectedCount, h.data.History.Traffic.ShowDetailPanel)
	}
}

func TestEvictedSelectionIsRemoved(t *testing.T) {
	entries := make([]*logic.TrafficEntry, 5000)
	for index := range entries {
		entries[index] = entry(uint64(index+1), "a.test")
	}
	f := newFixtureSized(t, entries, 1400, 800)
	table := f.workspace.Table
	table.replaceSelection(0)
	f.data.Traffic.Select(table.rows[0])
	f.settle()
	if f.data.Traffic.SelectedCount != 1 {
		t.Fatal("count")
	}
	f.data.Traffic.ResumeLiveEviction()
	f.deliver(entry(5001, "z.test"))
	if f.data.Traffic.SelectedCount != 0 || f.data.Traffic.Selected != nil {
		t.Fatalf("count %d selected %v", f.data.Traffic.SelectedCount, f.data.Traffic.Selected)
	}
}

func TestNewRowsAppearAtTheLatestEdge(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.deliver(entry(2, "b.test"))
	f.requireText("b.test")
}

func TestHoldWhileReadingAndShowNew(t *testing.T) {
	entries := make([]*logic.TrafficEntry, 5000)
	for index := range entries {
		entries[index] = entry(uint64(index+1), "a.test")
	}
	f := newFixtureSized(t, entries, 1400, 800)
	table := f.workspace.Table
	table.state.List.ScrollTo(100, ui.Start)
	f.settle()
	f.settle()
	if !f.data.Traffic.LiveEvictionPaused() {
		t.Fatal("scrolling away should hold the window")
	}
	first, _ := table.state.List.Visible()
	if first < 90 {
		t.Fatalf("first %d", first)
	}
	f.deliver(entry(5001, "new.test"))
	if f.data.Traffic.PendingLiveEntryCount() != 1 {
		t.Fatalf("queued %d", f.data.Traffic.PendingLiveEntryCount())
	}
	now, _ := table.state.List.Visible()
	if now != first {
		t.Fatalf("view moved from %d to %d", first, now)
	}
	f.requireText("Show new requests (1)")

	// A status change on a row on screen applies in place.
	updated := entry(uint64(first+1), "a.test")
	updated.Revision, updated.StatusCode = 2, 418
	f.deliver(updated)
	f.requireText("418")
	if f.data.Traffic.PendingLiveEntryCount() != 1 {
		t.Fatal("queue changed")
	}

	f.click("Show new requests (1)")
	if f.data.Traffic.PendingLiveEntryCount() != 0 || f.data.Traffic.LiveEvictionPaused() {
		t.Fatalf("queued %d paused %v", f.data.Traffic.PendingLiveEntryCount(), f.data.Traffic.LiveEvictionPaused())
	}
	if len(table.rows) != 5000 || table.rows[len(table.rows)-1].ID != 5001 || table.rows[0].ID != 2 {
		t.Fatal("drained in order")
	}
	f.settle()
	f.requireText("new.test")
}

func openRowMenu(t *testing.T, f *fixture, label string) {
	t.Helper()
	menutest.Close(f.tester, f.settle)
	if err := menutest.Open(f.tester, f.settle, label); err != nil {
		t.Fatalf("%v: %v", err, f.tester.Texts())
	}
}

func (f *fixture) choose(path ...string) {
	f.t.Helper()
	if err := menutest.Choose(f.tester, f.settle, path...); err != nil {
		f.t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	f.settle()
}

func TestRowMenuShowDetails(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
	f.clickRow(0, "a.test")
	f.requireSelection([]uint64{1}, 1, false)
	openRowMenu(t, f, "a.test")
	f.choose("Show")
	f.requireSelection([]uint64{1}, 1, true)

	// Show is unavailable while several rows are selected.
	f.clickRow(ui.Cmd, "b.test")
	openRowMenu(t, f, "b.test")
	if err := menutest.Choose(f.tester, f.settle, "Show"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Show with two rows selected: %v", err)
	}
}

func TestRowMenuCopyValues(t *testing.T) {
	item := entry(1, "a.test")
	item.Metadata = &proxyservice.Metadata{RemoteDestinationAddr: "9.9.9.9:443"}
	f := newFixture(t, item)
	openRowMenu(t, f, "a.test")
	f.choose("Copy", "Server IP")
	if got := f.tester.Clipboard(); got != "9.9.9.9" {
		t.Fatalf("clipboard %q", got)
	}
}

func TestCopyRowUsesVisibleColumns(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Copy", "Row")
	if got := f.tester.Clipboard(); !strings.HasPrefix(got, "1\tGET\ta.test\t/p1") {
		t.Fatalf("clipboard %q", got)
	}
}

func TestCopyCurlAndUnavailableBody(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
	f.backend.bodies[1] = &proxyservice.TrafficBodyView{RequestBody: "hi"}
	f.backend.bodies[2] = &proxyservice.TrafficBodyView{RequestBodyUnavailable: true}
	openRowMenu(t, f, "a.test")
	f.choose("Copy as cURL")
	if got := f.tester.Clipboard(); !strings.Contains(got, "--data-raw 'hi'") {
		t.Fatalf("clipboard %q", got)
	}
	openRowMenu(t, f, "b.test")
	f.choose("Copy as cURL")
	f.requireText("unavailable")
	if got := f.tester.Clipboard(); strings.Contains(got, "b.test") {
		t.Fatalf("incomplete copy: %q", got)
	}
}

func TestHighlightAndClear(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Highlight", "Red")
	if f.data.Traffic.Highlights[1] != "#ef4444" {
		t.Fatal("highlight")
	}
	openRowMenu(t, f, "a.test")
	f.choose("Highlight", "Clear")
	if len(f.data.Traffic.Highlights) != 0 {
		t.Fatal("clear")
	}
}

func TestDeleteSingleAndMultiple(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"), entry(3, "c.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Delete")
	f.requireNoText("a.test")
	if !slices.Equal(f.backend.deleted, []int64{1}) {
		t.Fatal(f.backend.deleted)
	}
	table := f.workspace.Table
	table.state.Selection.Add(trafficRowID(2))
	table.state.Selection.Add(trafficRowID(3))
	f.settle()
	openRowMenu(t, f, "b.test")
	f.choose("Delete")
	if !f.confirm.Open() {
		t.Fatal("should confirm")
	}
	f.workspace.Table.deleteConfirmed([]uint64{2, 3})
	f.settle()
	f.requireNoText("b.test")
	f.requireNoText("c.test")
	if len(f.backend.deleted) != 3 {
		t.Fatal(f.backend.deleted)
	}
}

func TestRawTCPHasNoRequestActions(t *testing.T) {
	item := entry(1, "a.test")
	item.Type = "tcp"
	item.RawTCP = &proxyservice.RawTCPTunnelInfo{HostPort: "a.test:443"}
	f := newFixture(t, item)
	openRowMenu(t, f, "a.test:443")
	for _, absent := range []string{"Copy as cURL", "Edit", "Resend"} {
		if _, ok := menutest.Item(f.tester, absent); ok {
			t.Fatalf("%q in %v", absent, f.tester.Texts())
		}
	}
}

func TestQuickResend(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Resend", "Resend 3 Time(s)")
	if len(f.resends) != 1 || f.resends[0].id != 1 || f.resends[0].config.Count != 3 || !f.resends[0].config.UseProxy {
		t.Fatalf("%+v", f.resends)
	}
}

func TestResendRefusesUnavailableBody(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.backend.bodies[1] = &proxyservice.TrafficBodyView{RequestBodyUnavailable: true}
	openRowMenu(t, f, "a.test")
	f.choose("Resend", "Resend 1 Time(s)")
	if len(f.resends) != 0 {
		t.Fatalf("%+v", f.resends)
	}
	f.requireText("unavailable")
}

func TestResendNTimesBounds(t *testing.T) {
	if validResendCount(nui.Set[float64](0)) || validResendCount(nui.Set[float64](1001)) || !validResendCount(nui.Set[float64](1000)) {
		t.Fatal("bounds")
	}
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Resend", "Resend N Times")
	dialog := &f.workspace.Table.resend
	if !dialog.open {
		t.Fatal("not open")
	}
	dialog.count = nui.Set[float64](1001)
	f.settle()
	_ = f.tester.Click(resendConfirmLabel())
	f.settle()
	if len(f.resends) != 0 {
		t.Fatal("over limit sent")
	}
	dialog.count = nui.Set[float64](7)
	f.settle()
	if err := dialog.startLabelClick(f); err != nil {
		t.Fatal(err)
	}
	if len(f.resends) != 1 || f.resends[0].config.Count != 7 || dialog.open {
		t.Fatalf("%+v open=%v", f.resends, dialog.open)
	}
}

func (d *resendDialog) startLabelClick(f *fixture) error {
	if err := f.tester.Click(resendConfirmLabel()); err != nil {
		return err
	}
	f.settle()
	return nil
}

func TestResendStopKeepsDialog(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.block = make(chan struct{})
	dialog := &f.workspace.Table.resend
	dialog.show(1)
	f.settle()
	if err := f.tester.Click(resendConfirmLabel()); err != nil {
		t.Fatal(err)
	}
	for {
		f.mu.Lock()
		sent := len(f.resends)
		f.mu.Unlock()
		if sent == 1 {
			break
		}
	}
	f.window.Drain()
	f.tester.Frame()
	if !dialog.sending {
		t.Fatal("should be sending")
	}
	if err := f.tester.Click(locales.ResendModalStop()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if dialog.sending || !dialog.cancelled || !dialog.open {
		t.Fatalf("%+v", dialog)
	}
}

func TestEditLimit(t *testing.T) {
	entries := make([]*logic.TrafficEntry, 51)
	for index := range entries {
		entries[index] = entry(uint64(index+1), "a.test")
	}
	f := newFixtureSized(t, entries, 1400, 800)
	table := f.workspace.Table
	for _, item := range entries {
		table.state.Selection.Add(trafficRowID(item.ID))
	}
	f.settle()
	openRowMenu(t, f, "/p51")
	f.choose("Edit")
	if len(f.opened) != 0 {
		t.Fatalf("%v", f.opened)
	}
	f.requireText("50")
	table.state.Selection.Remove(trafficRowID(1))
	f.settle()
	openRowMenu(t, f, "/p51")
	f.choose("Edit")
	// Each body load schedules the next one through the main dispatcher.
	// Complete that chain without rebuilding the whole table for every load.
	for range maxEditEntries {
		f.data.Runtime.Jobs.Wait()
		f.window.Drain()
		if len(f.opened) == maxEditEntries {
			break
		}
	}
	f.settle()
	if len(f.opened) != 50 {
		t.Fatalf("opened %d", len(f.opened))
	}
}

func TestExportSelection(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	f.choose("Export", "HAR")
	if !slices.Equal(f.exported, []string{"har:/tmp/x.har:[1]"}) {
		t.Fatal(f.exported)
	}

	// Live capture: the file is named for the count.
	f = newFixture(t, entry(1, "a.test"), entry(2, "b.test"), entry(3, "c.test"))
	f.clickRow(0, "a.test")
	f.clickRow(ui.Cmd, "b.test")
	f.clickRow(ui.Cmd, "c.test")
	openRowMenu(t, f, "b.test")
	f.choose("Export", exportFormatLabel(logic.ExportCSV))
	if f.pickedName != "flowlens-selection-3-csv.csv" {
		t.Fatalf("suggested %q", f.pickedName)
	}

	// A saved history with an alias: the file is named for the alias.
	backend := &fakeHistoryBackend{
		list:    []*proxyservice.HistoryMetadata{{Key: "k1", Alias: "login-flow", CreatedAt: 1700000000000, Total: 3, FormatVersion: 2}},
		entries: map[string][]*logic.TrafficEntry{"k1": {entry(11, "one.test"), entry(12, "two.test"), entry(13, "three.test")}},
	}
	h := newFixtureSized(t, nil, 1400, 800, backend)
	h.click("login-flow")
	h.clickRow(0, "one.test")
	h.clickRow(ui.Shift, "two.test")
	h.clickRow(ui.Shift, "three.test")
	openRowMenu(t, h, "two.test")
	h.choose("Export", exportFormatLabel(logic.ExportHAR))
	if h.pickedName != "login-flow-selection.har" {
		t.Fatalf("suggested %q", h.pickedName)
	}
}

func TestProcessColumnStates(t *testing.T) {
	process := func(id uint64, info proxyservice.ProcessInfo) *logic.TrafficEntry {
		item := entry(id, fmt.Sprintf("h%d.test", id))
		item.Metadata = &proxyservice.Metadata{Process: &info}
		return item
	}
	f := newFixture(t,
		process(1, proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusPending}),
		process(2, proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, DisplayName: "Safari", IconKey: "x"}),
		process(3, proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusRemote}),
		process(4, proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, PID: 77}),
	)
	f.requireText("Identifying...")
	f.requireText("Safari")
	f.requireText("PID 77")
	f.requireText("Remote")
}

func TestClearTraffic(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.click("Clear")
	if f.backend.cleared != 1 {
		t.Fatal("not cleared")
	}
	f.requireText("Traffic cleared")
}

func TestClearDisabledWhenEmpty(t *testing.T) {
	f := newFixture(t)
	_ = f.tester.Click("Clear")
	f.settle()
	if f.backend.cleared != 0 {
		t.Fatal("cleared nothing")
	}
}

func TestSessionAlias(t *testing.T) {
	f := newFixture(t)
	f.workspace.ShowAliasDialog()
	f.settle()
	f.requireText("Set Alias")
	f.workspace.alias.draft = "  my session "
	f.settle()
	f.click("Save")
	if f.backend.alias != "my session" || f.data.Proxy.Status.CaptureAlias != "my session" {
		t.Fatalf("%q", f.backend.alias)
	}
	f.requireText("Alias saved")
}

func TestRestartWithoutRecordsDoesNotAsk(t *testing.T) {
	f := newFixture(t)
	f.workspace.Restart()
	f.settle()
	if f.confirm.Open() || !slices.Equal(f.backend.restarts, []bool{false}) {
		t.Fatalf("%v", f.backend.restarts)
	}
}

func TestRestartWithRecordsAsks(t *testing.T) {
	for label, want := range map[string]bool{"Save and Restart": true, "Discard and Restart": false} {
		f := newFixture(t, entry(1, "a.test"))
		f.workspace.Restart()
		f.settle()
		if !f.confirm.Open() {
			t.Fatal("should ask")
		}
		f.click(label)
		if !slices.Equal(f.backend.restarts, []bool{want}) {
			t.Fatalf("%s: %v", label, f.backend.restarts)
		}
	}
}

func TestSidebarRestartAndAlias(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.requireText("Capture")
	f.click(locales.CaptureRestart())
	if !f.confirm.Open() {
		t.Fatal("should ask")
	}
	f.click("Discard and Restart")
	if !slices.Equal(f.backend.restarts, []bool{false}) {
		t.Fatal(f.backend.restarts)
	}
	if err := menutest.Open(f.tester, f.settle, "Capture"); err != nil {
		t.Fatal(err)
	}
	f.choose("Set Alias")
	if !f.workspace.alias.open {
		t.Fatal("alias dialog should open")
	}
	f.requireText("Set Alias")
}

func TestToggleProxyShortcut(t *testing.T) {
	f := newFixture(t)
	if !f.dispatch.Invoke(shortcuts.CaptureToggleProxy) {
		t.Fatal("shortcut")
	}
	f.settle()
	if f.backend.starts != 1 {
		t.Fatal("not started")
	}
}

func manyEntries(count int) []*logic.TrafficEntry {
	entries := make([]*logic.TrafficEntry, count)
	for index := range entries {
		entries[index] = entry(uint64(index+1), fmt.Sprintf("host%d.test", index+1))
	}
	return entries
}

// drag presses the row of from and drags to the row of to, running during
// each frame of the drag.
func (f *fixture) drag(from, to string, during func()) {
	f.t.Helper()
	start, ok := f.tester.Find(from)
	if !ok {
		f.t.Fatalf("row %s", from)
	}
	end, ok := f.tester.Find(to)
	if !ok {
		f.t.Fatalf("row %s", to)
	}
	x, y := start.X+start.W/2, start.Y+start.H/2
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+8)
	f.tester.Frame()
	if during != nil {
		during()
	}
	f.tester.Move(end.X+end.W/2, end.Y+end.H/2)
	f.tester.Frame()
	f.tester.Release(end.X+end.W/2, end.Y+end.H/2)
	f.settle()
}

func TestDragSelectsRangeOfRows(t *testing.T) {
	f := newFixture(t, manyEntries(9)...)
	f.clickRow(0, "host2.test")
	f.clickRow(ui.Cmd, "host9.test")
	f.drag("host4.test", "host6.test", nil)
	f.requireSelection([]uint64{4, 5, 6}, 6, false)
}

func TestDragNearTheEdgeScrollsAndExtends(t *testing.T) {
	f := newFixture(t, manyEntries(200)...)
	table := f.workspace.Table
	table.state.List.ScrollTo(0, ui.Start)
	f.settle()
	f.clickRow(0, "host1.test")
	first, last := table.state.List.Visible()
	if first != 0 {
		t.Fatalf("first %d", first)
	}
	row, ok := f.tester.Find(fmt.Sprintf("host%d.test", last-2))
	if !ok {
		t.Fatalf("row %d", last-2)
	}
	x, y := row.X+row.W/2, row.Y+row.H/2
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+10)
	f.tester.Frame()
	f.tester.Move(x, 790)
	for range 30 {
		f.tester.Advance(16 * time.Millisecond)
		f.tester.Frame()
	}
	f.tester.Release(x, 790)
	f.settle()
	if now, _ := table.state.List.Visible(); now == 0 {
		t.Fatal("the table did not scroll")
	}
	ids := selectedIDs(table)
	if len(ids) < 4 || ids[0] != uint64(last-2) || f.data.Traffic.SelectedCount != len(ids) {
		t.Fatalf("selected %v, want the rows from %d on alone", ids, last-2)
	}
	if focus := f.data.Traffic.Selected; focus == nil || focus.ID != ids[len(ids)-1] || f.data.Traffic.ShowDetailPanel {
		t.Fatalf("focus %+v detail %v", focus, f.data.Traffic.ShowDetailPanel)
	}
}

func TestDraggingAHeaderReordersAndSaves(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	method, ok := f.tester.Find("Method")
	if !ok {
		t.Fatal("method header")
	}
	host, ok := f.tester.Find("Host")
	if !ok {
		t.Fatal("host header")
	}
	y := method.Y + method.H/2
	f.tester.Press(method.X+method.W/2, y)
	f.tester.Frame()
	for x := method.X + method.W/2; x < host.X+host.W+40; x += 20 {
		f.tester.Move(x, y)
		f.tester.Frame()
	}
	f.tester.Release(host.X+host.W+40, y)
	f.settle()
	order := f.workspace.Table.state.List.Columns.Order
	if slices.Index(order, "method") < slices.Index(order, "host") {
		t.Fatalf("order %v", order)
	}
	if len(order) != len(logic.TrafficColumnKeys) {
		t.Fatalf("hidden slots lost: %v", order)
	}
	snapshot, err := f.service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	saved := snapshot.TrafficTableConfig.ColumnOrder
	if slices.Index(saved, "method") < slices.Index(saved, "host") {
		t.Fatalf("saved %v", saved)
	}
}

func TestDetailPanelOpensBesideTheTable(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.workspace.Detail = func(c *ui.Context, _ nui.Theme) { ui.Text(c, "detail-content") }
	f.settle()
	f.requireNoText("detail-content")
	f.click("a.test")
	f.requireNoText("detail-content")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.requireText("detail-content")
	f.data.Traffic.SetShowDetailPanel(false)
	f.settle()
	f.requireNoText("detail-content")
}

type fakeHistoryBackend struct {
	list    []*proxyservice.HistoryMetadata
	entries map[string][]*logic.TrafficEntry
	deleted []string
	cleared int
	import_ func(path string) (proxyservice.HARImportResult, error)
}

func (b *fakeHistoryBackend) ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error) {
	return b.list, nil
}
func (b *fakeHistoryBackend) GetHistory(key string) ([]*logic.TrafficEntry, error) {
	return b.entries[key], nil
}
func (b *fakeHistoryBackend) GetHistoryTrafficBodyView(string, uint64) (*proxyservice.TrafficBodyView, error) {
	return &proxyservice.TrafficBodyView{}, nil
}
func (b *fakeHistoryBackend) DeleteHistory(key string) error {
	b.deleted = append(b.deleted, key)
	kept := b.list[:0:0]
	for _, item := range b.list {
		if item.Key != key {
			kept = append(kept, item)
		}
	}
	b.list = kept
	return nil
}
func (b *fakeHistoryBackend) ClearHistories() error {
	b.cleared++
	b.list = nil
	return nil
}
func (b *fakeHistoryBackend) ImportHAR(_ context.Context, request historyservice.HARImportRequest) (proxyservice.HARImportResult, error) {
	return b.import_(request.Path)
}

type historyFixture struct {
	*fixture
	history *fakeHistoryBackend
}

func newHistoryFixture(t *testing.T, entries ...*logic.TrafficEntry) *historyFixture {
	t.Helper()
	backend := &fakeHistoryBackend{
		list:    []*proxyservice.HistoryMetadata{{Key: "k1", Alias: "session one", CreatedAt: 1700000000000, Total: 2, FormatVersion: 2}, {Key: "k2", Alias: "old format", CreatedAt: 1600000000000, Total: 1, FormatVersion: 9}},
		entries: map[string][]*logic.TrafficEntry{"k1": {entry(11, "saved.test"), entry(12, "other.test")}},
	}
	f := newFixtureSized(t, entries, 1400, 800, backend)
	return &historyFixture{fixture: f, history: backend}
}

func TestHistoryListOpensAndShowsTraffic(t *testing.T) {
	f := newHistoryFixture(t)
	f.requireText("session one")
	f.click("session one")
	f.requireText("saved.test")
	if f.data.Workbench.CaptureSelection != "history:k1" {
		t.Fatal(f.data.Workbench.CaptureSelection)
	}
	f.click("Capture")
	f.requireNoText("saved.test")
}

func TestSidebarAndWorkspaceTabsStayTogether(t *testing.T) {
	f := newHistoryFixture(t)
	workspace := f.data.Workspace
	f.click("session one")
	if workspace.ActiveKey != logic.HistoryTabKey || workspace.ActiveTab().HistoryKey != "k1" || workspace.ActiveTab().Title != "session one" {
		t.Fatalf("%+v", workspace.ActiveTab())
	}
	f.click("Capture")
	if workspace.ActiveKey != logic.CaptureTabKey || f.data.Workbench.CaptureSelection != store.CaptureItemLive {
		t.Fatal(workspace.ActiveKey, f.data.Workbench.CaptureSelection)
	}
	workspace.Activate(logic.HistoryTabKey)
	f.settle()
	if f.data.Workbench.CaptureSelection != "history:k1" {
		t.Fatal(f.data.Workbench.CaptureSelection)
	}
	f.requireText("saved.test")
	workspace.CloseTab(logic.HistoryTabKey)
	f.settle()
	if f.data.Workbench.CaptureSelection != store.CaptureItemLive {
		t.Fatal("closing the history tab returns to capture", f.data.Workbench.CaptureSelection)
	}
	f.requireNoText("saved.test")
}

func TestRequestTabReplacesTrafficContent(t *testing.T) {
	f := newFixture(t, entry(1, "live.test"))
	shown := false
	f.workspace.RequestTab = func(c *ui.Context, theme nui.Theme, tab *logic.WorkspaceTab) { shown = tab.HTTP != nil }
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	f.settle()
	if !shown || f.tester.HasText("live.test") {
		t.Fatal(shown)
	}
	f.data.Workspace.CloseTab(key)
	f.settle()
	if !f.tester.HasText("live.test") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestHistoryDeleteAndUnsupportedFormatStaysListed(t *testing.T) {
	f := newHistoryFixture(t)
	if err := menutest.Open(f.tester, f.settle, "old format"); err != nil {
		t.Fatal(err)
	}
	if _, ok := menutest.Item(f.tester, "Delete"); !ok {
		t.Fatal(f.tester.Texts())
	}
	f.choose("Delete")
	if !f.confirm.Open() {
		t.Fatal("ask")
	}
	f.requireText("old format")
	f.confirm.Accept()
	f.settle()
	if !slices.Equal(f.history.deleted, []string{"k2"}) {
		t.Fatal(f.history.deleted)
	}
	f.requireNoText("old format")
	f.requireText("session one")
}

func TestHistoryClearAll(t *testing.T) {
	f := newHistoryFixture(t)
	f.click("session one")
	f.click(locales.HistoryClearAll())
	f.requireText(locales.HistoryClearAllConfirm())
	f.confirm.Accept()
	f.settle()
	if f.history.cleared != 1 || f.data.Workbench.CaptureSelection != store.CaptureItemLive {
		t.Fatalf("%d %s", f.history.cleared, f.data.Workbench.CaptureSelection)
	}
	f.requireText(locales.HistoryEmpty())
}

func TestHARImportFromDropOpensResult(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "live.test"))
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		metadata := &proxyservice.HistoryMetadata{Key: "imported", Alias: "imported one", CreatedAt: 1800000000000}
		f.history.list = append(f.history.list, metadata)
		f.history.entries["imported"] = []*logic.TrafficEntry{entry(21, "imported.test")}
		return proxyservice.HARImportResult{Imported: 1, Metadata: metadata}, nil
	}
	rect, ok := f.tester.Find("live.test")
	if !ok {
		t.Fatal("row")
	}
	if !f.tester.DropFiles(rect.X, rect.Y, "/tmp/a.har") {
		t.Fatal("drop not taken")
	}
	f.settle()
	f.requireText("imported.test")
	if f.data.Workbench.CaptureSelection != "history:imported" {
		t.Fatal(f.data.Workbench.CaptureSelection)
	}
	if got := f.data.Workspace.ActiveTab().Title; got != "imported one" {
		t.Fatalf("imported tab title %q", got)
	}
	f.requireText("Imported 1 files")
}

func TestHARImportPartialFailureAndCancel(t *testing.T) {
	f := newHistoryFixture(t)
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		if path == "/bad.har" {
			return proxyservice.HARImportResult{}, errors.New("corrupt")
		}
		return proxyservice.HARImportResult{Imported: 3, Metadata: &proxyservice.HistoryMetadata{Key: "k1"}}, nil
	}
	f.workspace.ImportHAR([]string{"/bad.har", "/good.har"})
	f.settle()
	f.requireText("bad.har")
	f.requireText("corrupt")

	f.workspace.Host.PickHARFiles = func() ([]string, error) { return nil, nil }
	f.workspace.ImportHAR(nil)
	f.settle()
	if f.data.History.Importing {
		t.Fatal("canceled dialog should not import")
	}
}

func TestExportSingleFileUsesFileTarget(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	exporter := f.workspace.exporter
	if !exporter.Export(logic.ExportCSV, ExportOptions{IDs: []uint64{1, 1}}) {
		t.Fatal("export")
	}
	f.settle()
	if !slices.Equal(f.exported, []string{"csv:file:/tmp/x.csv:[1]:3"}) {
		t.Fatal(f.exported)
	}
	f.requireText("1 exported")
}

func TestExportManyEntriesUsesDirectory(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.workspace.exporter.Export(logic.ExportRequest, ExportOptions{IDs: []uint64{1, 2}})
	f.settle()
	if !slices.Equal(f.exported, []string{"request:directory:/tmp/dir:[1 2]:3"}) {
		t.Fatal(f.exported)
	}
}

func TestExportDialogCancelDoesNothing(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"))
	f.workspace.exporter.Host.PickSavePath = func(string, string, string) (string, error) { return " ", nil }
	f.workspace.exporter.Export(logic.ExportHAR, ExportOptions{})
	f.settle()
	if len(f.exported) != 0 || f.workspace.exporter.Exporting {
		t.Fatal(f.exported)
	}
}

func TestHistoryExportUsesHistoryService(t *testing.T) {
	f := newHistoryFixture(t)
	f.workspace.exporter.Export(logic.ExportHAR, ExportOptions{HistoryKey: "k1"})
	f.settle()
	f.workspace.exporter.Export(logic.ExportCSV, ExportOptions{HistoryKey: "k1"})
	f.settle()
	if !slices.Equal(f.exported, []string{"history-har:k1:/tmp/x.har", "history:k1:csv:/tmp/x.csv"}) {
		t.Fatal(f.exported)
	}
}

func TestHistoryMenuBlocksExportOfUnreadableFormat(t *testing.T) {
	f := newHistoryFixture(t)
	if err := menutest.Open(f.tester, f.settle, "old format"); err != nil {
		t.Fatal(err)
	}
	if err := menutest.Choose(f.tester, f.settle, locales.TrafficExportTitle(), "HAR"); err == nil {
		t.Fatal("HAR export of an unreadable format is offered")
	}
	if len(f.exported) != 0 {
		t.Fatal(f.exported)
	}
}

func categoryFixture(t *testing.T) *fixture {
	api := entry(1, "api.test")
	api.Metadata = &proxyservice.Metadata{Process: &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, AppID: "com.browser", DisplayName: "Browser"}}
	cdn := entry(2, "cdn.test")
	cdn.URL, cdn.Path = "http://cdn.test/assets/app.js", "/assets/app.js"
	f := newFixture(t, api, cdn)
	f.tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	f.showCategories = true
	f.settle()
	return f
}

func TestCategoryTreeListsAndFilters(t *testing.T) {
	f := categoryFixture(t)
	f.requireText("Browser")
	f.requireText(locales.CategoryProcessUnidentified())
	f.click("api.test")
	if !slices.Equal(f.data.Filter.SelectedHosts, []string{"api.test"}) || len(f.data.Filter.Filtered()) != 1 {
		t.Fatalf("%v", f.data.Filter.SelectedHosts)
	}
	f.click("api.test")
	if len(f.data.Filter.SelectedHosts) != 0 {
		t.Fatal("toggle")
	}
	f.click("Browser")
	if !slices.Equal(f.data.Filter.SelectedProcessKeys, []string{"app:com.browser"}) || len(f.data.Filter.Filtered()) != 1 {
		t.Fatal(f.data.Filter.SelectedProcessKeys)
	}
}

func TestCategorySearchAndSections(t *testing.T) {
	f := categoryFixture(t)
	f.workspace.category.search = "cdn"
	f.settle()
	labels := []string{}
	hosts, _ := f.workspace.category.facets(f.data.Filter)
	for _, host := range hosts {
		labels = append(labels, host.Host)
	}
	if !slices.Equal(labels, []string{"cdn.test"}) {
		t.Fatal(labels)
	}
	f.workspace.category.search = "no such thing"
	f.settle()
	f.requireText(locales.CategoryEmptyFiltered())
	f.workspace.category.search = ""
	f.settle()
	f.click(locales.CategoryHost())
	if slices.Contains(f.workspace.category.sections, sectionHost) {
		t.Fatal("collapse")
	}
}

func TestCategoryStructureExpandsAndFocuses(t *testing.T) {
	f := categoryFixture(t)
	f.click(locales.CategoryHost())
	f.click(locales.CategoryProcess())
	f.click("cdn.test")
	f.click("assets")
	f.click("app.js")
	if f.data.Traffic.Selected == nil || f.data.Traffic.Selected.ID != 2 {
		t.Fatalf("%+v", f.data.Traffic.Selected)
	}
}

func TestCategoryStructureFollowsSelections(t *testing.T) {
	for _, source := range []string{"capture", "history"} {
		t.Run(source, func(t *testing.T) {
			entries := []*logic.TrafficEntry{entry(1, "api.test"), entry(2, "api.test"), entry(3, "cdn.test"), entry(4, "other.test")}
			for _, i := range []int{0, 2} {
				entries[i].Metadata = &proxyservice.Metadata{Process: &proxyservice.ProcessInfo{
					Status: proxyservice.ProcessStatusResolved, AppID: "com.browser", DisplayName: "Browser",
				}}
			}
			var f *fixture
			if source == "history" {
				history := newHistoryFixture(t)
				history.history.entries["k1"] = entries
				f = history.fixture
				f.click("session one")
			} else {
				f = newFixture(t, entries...)
			}
			f.showCategories = true
			f.settle()

			for _, step := range []struct {
				label string
				ids   []uint64
			}{
				{"api.test", []uint64{1, 2}},
				{"cdn.test", []uint64{1, 2, 3}},
				{"other.test", []uint64{1, 2, 3, 4}},
				{"Browser", []uint64{1, 3}},
				{"api.test", []uint64{3}},
				{"cdn.test", nil},
				{"other.test", []uint64{1, 3}},
				{"Browser", []uint64{1, 2, 3, 4}},
			} {
				f.click(step.label)
				var ids []uint64
				for _, root := range f.workspace.category.roots {
					for _, leaf := range root.Children {
						ids = append(ids, leaf.EntryIDs...)
					}
				}
				if !slices.Equal(ids, step.ids) {
					t.Fatalf("after clicking %q, structure contains requests %v, want %v", step.label, ids, step.ids)
				}
			}
		})
	}
}

func TestClosingHistoryTabReleasesTheHistory(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "live.test"))
	var released int
	f.workspace.ReleaseHistoryDetail = func() { released++ }
	f.click("session one")
	opened := released
	f.requireText("saved.test")
	f.showCategories = true
	f.settle()
	view := &f.workspace.category
	if len(f.workspace.HistoryTable.Rows()) != 2 || view.context != "history:k1" || len(view.entries) != 2 || len(view.roots) == 0 {
		t.Fatalf("history not shown: rows %d context %q entries %d", len(f.workspace.HistoryTable.Rows()), view.context, len(view.entries))
	}

	f.data.Workspace.CloseTab(logic.HistoryTabKey)
	f.settle()
	if released != opened+1 {
		t.Fatalf("closing history released detail %d times, want once", released-opened)
	}
	history := f.data.History
	if history.Traffic.Key != "" || history.Traffic.Entries != nil || len(history.Filter.Base()) != 0 {
		t.Fatalf("closed history kept its entries: key %q entries %d", history.Traffic.Key, len(history.Traffic.Entries))
	}
	if rows := f.workspace.HistoryTable.Rows(); len(rows) != 0 || f.workspace.HistoryTable.state.Len() != 0 {
		t.Fatalf("history table kept %d rows", len(rows))
	}
	for _, row := range view.entries {
		if row.Host == "saved.test" || row.Host == "other.test" {
			t.Fatal("category view kept the closed history's entries")
		}
	}
	if view.context == "history:k1" {
		t.Fatal("category view still shows the closed history")
	}
	f.requireText("live.test")
	rendertest.ShootThemes(t, f.tester, "history-closed", f.setDark)

	f.showCategories = false
	f.settle()
	f.click("session one")
	f.requireText("saved.test")
	if len(f.workspace.HistoryTable.Rows()) != 2 {
		t.Fatalf("reopened history rows = %d", len(f.workspace.HistoryTable.Rows()))
	}
}

func TestReplacingAndClearingHistoryReleaseHiddenDetails(t *testing.T) {
	f := newHistoryFixture(t)
	var released int
	f.workspace.ReleaseHistoryDetail = func() { released++ }
	f.click("session one")
	opened := released
	// Switching histories need not show the previous detail panel again.
	f.data.History.Traffic.Load("k2", []*logic.TrafficEntry{entry(1, "replacement.test")})
	f.settle()
	if released != opened+1 {
		t.Fatal("replacing a history did not release its details")
	}
	// Deletion can clear the store before the tab is closed.
	f.data.History.CloseOpened()
	f.settle()
	if released != opened+2 || len(f.workspace.HistoryTable.Rows()) != 0 {
		t.Fatal("clearing a history kept hidden view state")
	}
	f.data.Workspace.CloseTab(logic.HistoryTabKey)
	f.settle()
	if released != opened+2 {
		t.Fatal("empty frames repeatedly released the detail panel")
	}
}
