package inspect

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/database"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
	"golang.org/x/image/bmp"
)

type fakeData struct {
	entry    *logic.TrafficEntry
	body     *proxyservice.TrafficBodyView
	version  uint64
	loading  bool
	pending  *logic.TrafficEntry
	request  func()
	retained *store.HistoryDetailSnapshot
	release  func()
}

func (d *fakeData) SelectedEntry() *logic.TrafficEntry           { return d.entry }
func (d *fakeData) BodyView() *proxyservice.TrafficBodyView      { return d.body }
func (d *fakeData) BodyVersion() uint64                          { return d.version }
func (d *fakeData) BodyLoading() bool                            { return d.loading }
func (d *fakeData) PendingEntry() *logic.TrafficEntry            { return d.pending }
func (d *fakeData) RetainedDetail() *store.HistoryDetailSnapshot { return d.retained }
func (d *fakeData) ReleaseRetainedDetail() {
	d.retained = nil
	if d.release != nil {
		d.release()
	}
}
func (d *fakeData) RequestBody() {
	if d.request != nil {
		d.request()
	}
}

type fixture struct {
	t        testing.TB
	data     *fakeData
	panel    *Panel
	tester   *ui.Tester
	window   *runtimetest.Window
	rt       store.Runtime
	settings *store.SettingsStore
	service  *settingservice.SettingService
	saved    []proxyservice.SaveBodyToFileRequest
	path     string
	// picked are the names the save dialog suggested, in order.
	mu     sync.Mutex
	picked []string
}

// pickedNames returns the names the save dialog suggested.
func (f *fixture) pickedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.picked)
}

func (f *fixture) settle() {
	for range 4 {
		f.rt.Jobs.Wait()
		f.window.Drain()
		f.tester.Frame()
	}
}

func newFixture(t testing.TB, entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView) *fixture {
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
	rt := store.NewRuntime()
	t.Cleanup(rt.Close)
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	settings := store.NewSettingsStore(rt, store.SettingsDependencies{Backend: service})
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, data: &fakeData{entry: entry, body: body}, window: window, rt: rt, settings: settings, service: service, path: "/tmp/out"}
	notifier := &widgets.Notifier{}
	f.panel = NewPanel(f.data, Dependencies{
		Runtime: rt, Settings: settings, Notifier: notifier,
		PickSavePath: func(name string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.picked = append(f.picked, name)
			return f.path, nil
		},
		SaveBody: func(request proxyservice.SaveBodyToFileRequest) error {
			f.saved = append(f.saved, request)
			return nil
		},
	})
	f.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, settings.Theme().Config)
		ui.Column(c).Fill().Children(func() { f.panel.Build(c, theme) })
		notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 3})
	}, 1400, 2200)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\n%v", label, err, f.tester.Texts())
	}
	f.settle()
}

func (f *fixture) has(text string) {
	f.t.Helper()
	if !f.tester.HasText(text) {
		f.t.Fatalf("missing %q in %v", text, f.tester.Texts())
	}
}

func (f *fixture) lacks(text string) {
	f.t.Helper()
	if f.tester.HasText(text) {
		f.t.Fatalf("unexpected %q in %v", text, f.tester.Texts())
	}
}

func httpEntry() *logic.TrafficEntry {
	return &logic.TrafficEntry{
		ID: 1, Revision: 1, Type: "https", Method: "GET", Host: "a.test", Path: "/x", URL: "https://a.test/x?q=1", StatusCode: 200, Status: "200 OK",
		Request:  &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []logic.HeaderField{{Name: ":authority", Value: "a.test"}, {Name: "Accept", Value: "*/*"}}},
		Response: &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []logic.HeaderField{{Name: "Set-Cookie", Value: "a=1; Path=/"}, {Name: "set-cookie", Value: "b=2"}}},
	}
}

func TestApplicableTabs(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	for _, label := range []string{"Overview", "Raw", "Query(1)", "Cookie(2)"} {
		f.has(label)
	}
	f.lacks("Trailers")
}

func TestPanelWithoutCloseHandler(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	// The response tab strip ends with the summary chips.
	f.has("200")
	f.has("HTTP/2.0")
	if _, ok := f.tester.Find("Close details"); ok {
		t.Fatal("the panel offers a close button")
	}
	if _, ok := f.tester.Access("Close details"); ok {
		t.Fatal("the panel offers a close button")
	}
}

func TestCloseActionOnHTTPErrorAndRawTCP(t *testing.T) {
	failed := httpEntry()
	failed.Error = &proxyservice.TrafficError{Error: "failed"}
	raw := &logic.TrafficEntry{ID: 2, Type: "tcp", RawTCP: &proxyservice.RawTCPTunnelInfo{HostPort: "a.test:443"}}
	for name, entry := range map[string]*logic.TrafficEntry{"http": httpEntry(), "error": failed, "tcp": raw} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, entry, nil)
			closed := 0
			f.panel.OnClose = func() { closed++ }
			f.settle()
			keytest.TabTo(t, f.tester, locales.DetailClosePanel())
			f.tester.Key(0, ui.KeyEnter)
			f.settle()
			if closed != 1 {
				t.Fatalf("close called %d times", closed)
			}
		})
	}
}

func TestErrorTab(t *testing.T) {
	entry := httpEntry()
	entry.Error = &proxyservice.TrafficError{Timestamp: time.Unix(1, 0), Error: "connection refused"}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.has("connection refused")
	f.lacks("Cookie(2)")
}

func TestActiveTabSurvivesLiveUpdates(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.click("Query(1)")
	updated := *f.data.entry
	updated.Revision = 2
	f.data.entry = &updated
	f.settle()
	if f.panel.requestTab != tabRequestQuery {
		t.Fatal(f.panel.requestTab)
	}
}

func TestTabFallsBackWhenItDisappears(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.click("Query(1)")
	replaced := *f.data.entry
	replaced.ID, replaced.URL = 2, "https://a.test/x"
	f.data.entry = &replaced
	f.settle()
	if f.panel.requestTab != tabRequestHeaders {
		t.Fatal(f.panel.requestTab)
	}
}

func TestWebSocketEntryOpensItsTab(t *testing.T) {
	entry := httpEntry()
	entry.Type = "wss"
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{{Direction: "send", MsgType: "text", Data: "hello ws", DataSize: 8}}, WsMsgsTruncated: true})
	if f.panel.responseTab != tabWebSocket {
		t.Fatal(f.panel.responseTab)
	}
	f.has("hello ws")
	f.has(locales.TrafficWsMsgsTruncated())
}

func TestWebSocketDetailAndFilter(t *testing.T) {
	entry := httpEntry()
	entry.Type = "ws"
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{
		{Direction: "send", MsgType: "text", Data: "outgoing", DataSize: 8},
		{Direction: "receive", MsgType: "text", Data: "incoming", DataSize: 8},
	}})
	f.has("outgoing")
	f.has("incoming")
	f.click(locales.WorkspaceWebsocketClientFilterAll())
	f.has("outgoing")
	f.lacks("incoming")
	f.click(locales.WorkspaceWebsocketClientFilterSend())
	f.lacks("outgoing")
	f.has("incoming")
	f.click("incoming")
	f.has(locales.WorkspaceWebsocketClientMessageDetailReceive())
	f.click(locales.DetailSaveBody())
	if len(f.saved) != 1 || f.saved[0].Body != "incoming" {
		t.Fatalf("%+v", f.saved)
	}
}

// wsFixture shows a WebSocket entry with messages.
func wsFixture(t testing.TB, messages ...*proxyservice.WebSocketMessage) *fixture {
	entry := httpEntry()
	entry.Type = "ws"
	return newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: messages})
}

// openMessage opens the detail of the message shown as label and lets the
// dialog's opening transition end.
func (f *fixture) openMessage(label string) {
	f.t.Helper()
	f.click(label)
	f.tester.Advance(time.Second)
	f.settle()
}

func TestWebSocketMessageDetailContent(t *testing.T) {
	f := wsFixture(t,
		&proxyservice.WebSocketMessage{Direction: "receive", MsgType: "text", Data: "hello, world", DataSize: 12},
		&proxyservice.WebSocketMessage{Direction: "send", MsgType: "binary", Data: "\x01\x02\x03", DataSize: 3},
	)
	f.openMessage("hello, world")
	f.has(locales.WorkspaceWebsocketClientMessageDetailReceive())
	for _, text := range []string{locales.WorkspaceWebsocketClientFilterReceive(), locales.WorkspaceWebsocketClientDraftTypeText(), locales.WorkspaceWebsocketClientBinarySize("12")} {
		f.has(text)
	}
	if got := f.panel.messages.view.Text(); got != "hello, world" {
		t.Fatalf("text %q", got)
	}
	title, ok := f.tester.AccessByRole(ui.RoleHeading, locales.WorkspaceWebsocketClientMessageDetailReceive())
	if !ok {
		t.Fatal("message detail heading is missing")
	}
	field, ok := f.tester.AccessByRole(ui.RoleTextField, "Code editor")
	// The title and content frame share a left edge. Only the frame's
	// one-point border should inset the editor.
	if !ok || field.Bounds.X != title.Bounds.X+1 {
		t.Fatalf("editor bounds %v, want left edge at %v", field.Bounds, title.Bounds.X+1)
	}
	for _, label := range []string{locales.DetailWrapBody(), locales.DetailCopyBody(), locales.DetailSaveBody()} {
		if _, ok := f.tester.Access(label); !ok {
			t.Fatalf("text message without %q", label)
		}
	}
	f.click(locales.DetailCopyBody())
	if got := f.tester.Clipboard(); got != "hello, world" {
		t.Fatalf("clipboard %q", got)
	}
	f.panel.messages.open = false
	f.settle()

	f.openMessage(locales.WorkspaceWebsocketClientBinarySize("3"))
	f.has(locales.WorkspaceWebsocketClientMessageDetailSend())
	f.has(locales.WorkspaceWebsocketClientFilterSend())
	f.has(locales.WorkspaceWebsocketClientDraftTypeBinaryFile())
	f.has("01 02 03")
	// Only Save is offered for a binary message.
	for _, label := range []string{locales.DetailWrapBody(), locales.DetailCopyBody()} {
		if _, ok := f.tester.Access(label); ok {
			t.Fatalf("binary message offers %q", label)
		}
	}
	if _, ok := f.tester.Access(locales.DetailSaveBody()); !ok {
		t.Fatal("binary message without Save")
	}
}

func TestWebSocketMessageFileNames(t *testing.T) {
	f := wsFixture(t,
		&proxyservice.WebSocketMessage{Direction: "receive", MsgType: "text", Data: "hello", DataSize: 5},
		&proxyservice.WebSocketMessage{Direction: "send", MsgType: "binary", Data: "\x01\x02\x03", DataSize: 3},
	)
	f.openMessage("hello")
	f.click(locales.DetailSaveBody())
	f.panel.messages.open = false
	f.settle()
	f.openMessage(locales.WorkspaceWebsocketClientBinarySize("3"))
	f.click(locales.DetailSaveBody())

	// Exporting a selection of the hex view.
	hex := f.panel.messages.hex
	hex.anchor, hex.focus, hex.hasChoice = 0, 1, true
	f.settle()
	row, ok := f.tester.Find("01 02 03")
	if !ok {
		t.Fatalf("no hex row in %v", f.tester.Texts())
	}
	f.tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	f.settle()
	if err := menutest.Choose(f.tester, f.settle, locales.DetailHexExport()); err != nil {
		t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	f.settle()
	if got, want := f.pickedNames(), []string{"websocket-message.txt", "websocket-message.bin", "websocket-selection.bin"}; !slices.Equal(got, want) {
		t.Fatalf("suggested %v, want %v", got, want)
	}
	if len(f.saved) != 3 {
		t.Fatalf("saved %+v", f.saved)
	}
	if text := f.saved[0]; text.Body != "hello" || text.ContentType != "text/plain" {
		t.Fatalf("saved text message %+v", text)
	}
	// The binary message and the selection are written as their exact bytes.
	if message := f.saved[1]; message.Body != "\x01\x02\x03" || message.ContentType != "application/octet-stream" {
		t.Fatalf("saved binary message %+v", message)
	}
	if last := f.saved[2]; last.Body != "\x01\x02" || last.ContentType != "application/octet-stream" {
		t.Fatalf("exported %+v", last)
	}
}

func TestWebSocketMessageRows(t *testing.T) {
	f := wsFixture(t,
		&proxyservice.WebSocketMessage{Direction: "receive", MsgType: "text", Data: "incoming", DataSize: 8},
		&proxyservice.WebSocketMessage{Direction: "send", MsgType: "text", Data: "outgoing", DataSize: 8},
	)
	// Each row shows its direction's tag and, as in 1.0.5, no size.
	f.has(locales.WorkspaceWebsocketClientFilterReceive())
	f.has(locales.WorkspaceWebsocketClientFilterSend())
	f.lacks(logic.FormatFileSize(8))
	first, _ := f.tester.Find("incoming")
	second, _ := f.tester.Find("outgoing")
	if second.Y-first.Y != 44 {
		t.Fatalf("rows are %v apart, want 44", second.Y-first.Y)
	}
}

func TestWebSocketHexExportWaitsForTheDialogsSave(t *testing.T) {
	f := wsFixture(t, &proxyservice.WebSocketMessage{Direction: "send", MsgType: "binary", Data: "\x01\x02\x03", DataSize: 3})
	f.openMessage(locales.WorkspaceWebsocketClientBinarySize("3"))
	stream := f.panel.messages
	stream.hex.anchor, stream.hex.focus, stream.hex.hasChoice = 0, 1, true
	// A Save of the message is in progress.
	stream.exporting = true
	f.settle()
	row, ok := f.tester.Find("01 02 03")
	if !ok {
		t.Fatalf("no hex row in %v", f.tester.Texts())
	}
	f.tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	f.settle()
	if err := menutest.Choose(f.tester, f.settle, locales.DetailHexExport()); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Export during a Save: %v", err)
	}
	if info, ok := f.tester.Access(locales.DetailSaveBody()); !ok || !info.Disabled {
		t.Fatalf("Save %+v", info)
	}
}

func TestWebSocketPaneWithoutABodyView(t *testing.T) {
	entry := httpEntry()
	entry.Type = "ws"
	f := newFixture(t, entry, nil)
	f.panel.responseTab = tabWebSocket
	f.settle()
	// As 1.0.5: the stream, with its toolbar, says there are no messages.
	f.has(locales.WorkspaceWebsocketClientNoMessages())
	f.lacks(locales.CommonNoContent())
	if _, ok := f.tester.Access(locales.WorkspaceWebsocketClientFilterAll()); !ok {
		t.Fatalf("no direction filter in %v", f.tester.Texts())
	}
	f.data.loading = true
	f.settle()
	f.lacks(locales.WorkspaceWebsocketClientNoMessages())
}

func TestEmptyWebSocketMessageStream(t *testing.T) {
	f := wsFixture(t, &proxyservice.WebSocketMessage{Direction: "receive", MsgType: "text", Data: "incoming", DataSize: 8})
	f.click(locales.WorkspaceWebsocketClientFilterAll())
	f.lacks("incoming")
	f.has(locales.WorkspaceWebsocketClientNoMessages())
	f.lacks(locales.CommonNoContent())
}

func TestOverviewUnknownValues(t *testing.T) {
	entry := httpEntry()
	entry.Request.Metrics = &proxyservice.HTTPMessageMetrics{StartedAtMicros: 1, EndedAtMicros: -1, HeaderSize: -1, BodySize: -1, State: proxyservice.HTTPMessageStateFailed}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.click(locales.DetailTiming())
	f.has(locales.DetailMetricStateFailed())
	f.has(logic.UnknownValue)
	f.click(locales.DetailMessageSize())
	// Overview labels show in capitals.
	f.has(strings.ToUpper(locales.DetailTotalSize()))
}

func TestLegacyEntryHasNoMetrics(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.has(locales.DetailLegacyMetricsUnavailable())
}

func TestProcessAndTLSSections(t *testing.T) {
	entry := httpEntry()
	entry.Metadata = &proxyservice.Metadata{
		Process:     &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, DisplayName: "Safari", ProcessName: "safari", PID: 77, AppID: "com.apple", ExecutablePath: "/usr/bin/safari"},
		TLS:         &proxyservice.TLSState{ServerName: "sni.test", SelectedVersion: "TLS1.3"},
		Certificate: &proxyservice.ServerCertificate{Version: 3, Subject: &proxyservice.PkixName{CommonName: "cn.test"}, DNSNames: []string{"a", "b"}},
	}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.click(locales.DetailProcess())
	for _, text := range []string{"Safari", "safari", "77", "com.apple", "/usr/bin/safari"} {
		f.has(text)
	}
	f.click(locales.DetailTlsInfo())
	f.has("sni.test")
	f.click(locales.DetailCertInfo())
	f.has("cn.test")
	f.has("a, b")
}

func TestHeadersKeepOrderDuplicatesAndCase(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.panel.requestTab = tabOverview
	f.panel.responseTab = tabResponseHeaders
	f.settle()
	texts := strings.Join(f.tester.Texts(), "|")
	first := strings.Index(texts, "Set-Cookie")
	second := strings.Index(texts, "set-cookie")
	if first < 0 || second < first {
		t.Fatalf("order lost: %s", texts)
	}
	f.click(locales.DetailCopyResponseHeadersText())
	if got := f.tester.Clipboard(); !strings.Contains(got, "Set-Cookie: a=1; Path=/") || !strings.Contains(got, "set-cookie: b=2") {
		t.Fatalf("%q", got)
	}
}

func TestHeaderNotices(t *testing.T) {
	entry := httpEntry()
	entry.Response.HeadersTruncated = true
	entry.Response.HeaderOrderUnavailable = true
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.has(locales.DetailHeaderFieldsTruncated())
	f.has(locales.DetailHeaderOrderUnavailable())
}

func TestQueryAndCookiePanes(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.click("Query(1)")
	f.has("q")
	f.click(locales.DetailCopyQuery())
	if got := f.tester.Clipboard(); got != "q=1" {
		t.Fatalf("%q", got)
	}
	f.click("Cookie(2)")
	f.has("a")
	f.has("b")
	f.click(locales.DetailCopySetCookieHeader())
	if got := f.tester.Clipboard(); !strings.Contains(got, "a=1; Path=/") {
		t.Fatalf("%q", got)
	}
}

func TestRawHTTPSynthesizesHTTP2Lines(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{RequestBody: "payload"})
	f.panel.requestTab = tabRequestRaw
	f.settle()
	text := f.panel.requestRaw.text
	if !strings.HasPrefix(text, "GET /x?q=1 HTTP/2.0") || !strings.Contains(text, "payload") {
		t.Fatalf("%q", text)
	}
	f.click(locales.DetailCopyRawHttp())
	if f.tester.Clipboard() != text {
		t.Fatal("copy")
	}
}

func TestRawTCPConnectDetails(t *testing.T) {
	entry := &logic.TrafficEntry{ID: 2, Type: "tcp", Host: "a.test:443", StartedAt: time.Unix(5, 0),
		RawTCP:  &proxyservice.RawTCPTunnelInfo{Source: proxyservice.RawTCPTunnelSourceHTTPConnect, HostPort: "a.test:443", TLS: true},
		Request: &proxyservice.HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []logic.HeaderField{{Name: "Proxy-Auth", Value: "x"}}},
	}
	f := newFixture(t, entry, nil)
	f.has("a.test:443")
	f.has(locales.DetailRawTcpSourceHttpConnect())
	f.has("HTTP/1.1")
	f.has("Proxy-Auth")
	f.has("TCP/TLS")
}

// bodyFixture shows a response body holding raw bytes. binary is the marker
// the service sets for a body that cannot be shown as text.
func bodyFixture(t testing.TB, contentType, body string, binary bool) *fixture {
	entry := httpEntry()
	entry.Response.HeaderFields = []logic.HeaderField{{Name: "Content-Type", Value: contentType}}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{ResponseBody: body, ResponseBodyBinary: binary})
	f.panel.responseTab = tabResponseBody
	f.settle()
	return f
}

func TestJSONBodyIsFormatted(t *testing.T) {
	f := bodyFixture(t, "application/json", `{"a":1}`, false)
	if f.panel.responseBody.Active() != logic.BodyTabRaw {
		t.Fatal(f.panel.responseBody.Active())
	}
	f.click(locales.DetailFormatted())
	if got := f.panel.responseBody.formatted.Text(); got != "{\n  \"a\": 1\n}" {
		t.Fatalf("%q", got)
	}
	f.click(locales.DetailCopyBody())
	if got := f.tester.Clipboard(); got != "{\n  \"a\": 1\n}" {
		t.Fatalf("%q", got)
	}
}

func TestSaveUsesInferredNameAndCompleteContent(t *testing.T) {
	f := bodyFixture(t, "text/plain", "whole text", false)
	f.click(locales.DetailSaveBody())
	if len(f.saved) != 1 || f.saved[0].Body != "whole text" || f.saved[0].Path != "/tmp/out" {
		t.Fatalf("%+v", f.saved)
	}
}

func TestEmptyIsDistinctFromUnavailable(t *testing.T) {
	f := bodyFixture(t, "text/plain", "", false)
	f.has(locales.DetailBodyEmpty())
	f.lacks(locales.DetailBodyUnavailable())
	f.data.body = &proxyservice.TrafficBodyView{ResponseBodyUnavailable: true}
	f.settle()
	f.has(locales.DetailBodyUnavailable())
	f.lacks(locales.DetailBodyEmpty())
}

func TestLargeTextIsPlain(t *testing.T) {
	f := bodyFixture(t, "application/json", `"`+strings.Repeat("x", logic.LargeTextThreshold)+`"`, false)
	if !f.panel.responseBody.raw.PlainText() {
		t.Fatal("not plain")
	}
	for _, tab := range f.panel.responseBody.tabs {
		if tab == logic.BodyTabFormatted {
			t.Fatal("formatted offered for a large body")
		}
	}
}

func imageFixture(t *testing.T, encode func(*bytes.Buffer, image.Image) error) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var data bytes.Buffer
	if err := encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.String()
}

// webpFixture is a 1 x 1 lossy WebP image.
func webpFixture(t *testing.T) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestImageFormatsDecode(t *testing.T) {
	cases := map[string]string{
		"image/png":  imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return png.Encode(w, i) }),
		"image/jpeg": imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return jpeg.Encode(w, i, nil) }),
		"image/gif":  imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return gif.Encode(w, i, nil) }),
		"image/bmp":  imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return bmp.Encode(w, i) }),
		"image/webp": webpFixture(t),
	}
	for contentType, body := range cases {
		t.Run(contentType, func(t *testing.T) {
			f := bodyFixture(t, contentType, body, true)
			if f.panel.responseBody.Active() != logic.BodyTabImage {
				t.Fatal(f.panel.responseBody.Active())
			}
			preview := &f.panel.responseBody.image
			if preview.failed || preview.image.bitmap == nil {
				t.Fatalf("%+v", preview)
			}
			f.has(logic.ImageFormatLabel(contentType))
		})
	}
}

func TestSVGPreview(t *testing.T) {
	f := bodyFixture(t, "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`, false)
	if f.panel.responseBody.image.failed || f.panel.responseBody.image.image.svg == nil {
		t.Fatalf("%+v", f.panel.responseBody.image)
	}
}

func TestInvalidBinaryImageKeepsHexWithoutRaw(t *testing.T) {
	avif := avifFixture(t, "opaque")
	for _, tc := range []struct {
		name, contentType, body, hex string
		unsupported                  bool
	}{
		{name: "truncated png", contentType: "image/png", body: "\x89PNG\r\n\x1a\n", hex: "89 50 4E 47"},
		{name: "truncated avif", contentType: "image/avif", body: string(avif[:len(avif)-8]), hex: "66 74 79 70"},
		{name: "unrecognized", contentType: "image/png", body: "not an image", hex: "6E 6F 74", unsupported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := bodyFixture(t, tc.contentType, tc.body, true)
			if tc.unsupported {
				f.has(locales.DetailImageFormatUnsupported())
				f.lacks(locales.DetailImagePreviewFailed())
			} else {
				f.has(locales.DetailImagePreviewFailed())
				f.lacks(locales.DetailImageFormatUnsupported())
			}
			if slices.Contains(f.panel.responseBody.tabs, logic.BodyTabRaw) {
				t.Fatal("binary image offers a Raw tab after decoding fails")
			}
			// A delayed choice from an earlier text body must not reopen Raw.
			f.panel.responseBody.tabChoice = string(logic.BodyTabRaw)
			f.settle()
			if f.panel.responseBody.Active() != logic.BodyTabImage || f.panel.responseBody.raw.Text() != "" {
				t.Fatal("stale Raw selection did not fall back to the image")
			}
			f.click(locales.DetailHex())
			if f.panel.responseBody.Active() != logic.BodyTabHex {
				t.Fatal(f.panel.responseBody.Active())
			}
			f.has(tc.hex)
		})
	}
}

func TestImageZoomBounds(t *testing.T) {
	f := bodyFixture(t, "image/png", imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return png.Encode(w, i) }), true)
	preview := &f.panel.responseBody.image
	for range 60 {
		preview.zoom(logic.ImageScaleStep)
	}
	if preview.scale != logic.ImageScaleMax {
		t.Fatal(preview.scale)
	}
	area, _ := f.tester.Find("image-area")
	f.tester.RightClickAt(area.X+area.W/2, area.Y+area.H/2)
	f.settle()
	if err := menutest.Choose(f.tester, f.settle, locales.DetailImageReset()); err != nil {
		t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	f.settle()
	if preview.scale != 1 {
		t.Fatal(preview.scale)
	}
}

func TestHexShowsColumnsAndCopiesSelection(t *testing.T) {
	f := bodyFixture(t, "application/octet-stream", string([]byte{0, 'A', '\n', '~', 0xFF}), true)
	f.click(locales.DetailHex())
	f.has("00 41 0A 7E FF")
	f.has(".A.~.")
	hex := f.panel.responseBody.hex
	hex.anchor, hex.focus, hex.hasChoice = 0, 4, true
	f.settle()
	for label, want := range map[string]string{
		locales.DetailHexCopyHex():    "00 41 0A 7E FF",
		locales.DetailHexCopyBase64(): "AEEKfv8=",
		locales.DetailHexCopyText():   "\\x00A\n~\u00ff",
	} {
		hex.copySelection(map[string]logic.HexCopyFormat{locales.DetailHexCopyHex(): logic.HexCopyHex, locales.DetailHexCopyBase64(): logic.HexCopyBase64, locales.DetailHexCopyText(): logic.HexCopyText}[label])
		f.settle()
		if got := f.tester.Clipboard(); got != want {
			t.Fatalf("%s: %q", label, got)
		}
	}
	hex.exportSelection()
	f.settle()
	if len(f.saved) != 1 || f.saved[0].Body != "\x00A\n~\xff" || f.saved[0].ContentType != "application/octet-stream" {
		t.Fatalf("%+v", f.saved)
	}
}

func allBytes() string {
	data := make([]byte, 256)
	for index := range data {
		data[index] = byte(index)
	}
	return string(data)
}

func TestHexShowsBinaryBytesOnTheFirstFrame(t *testing.T) {
	if err := apptheme.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	// No job runner: the bytes must not wait for background work.
	h := NewHexView(Dependencies{})
	tester := ui.NewTester(func(c *ui.Context) {
		palette := nui.App(c, apptheme.Resolve(apptheme.Appearance{}).Config)
		h.Build(c, palette, 1, string([]byte{0x00, 0x41, 0xff, 0x80}), true)
	}, 720, 300)
	tester.Frame()
	if !tester.HasText("00 41 FF 80") || !tester.HasText(".A..") {
		t.Fatalf("first frame shows %v", tester.Texts())
	}
}

func TestSavingABinaryBodyKeepsItsBytes(t *testing.T) {
	body := allBytes()
	f := bodyFixture(t, "application/octet-stream", body, true)
	f.click(locales.DetailSaveBody())
	f.settle()
	if len(f.saved) != 1 || f.saved[0].Body != body || f.saved[0].ContentType != "application/octet-stream" {
		t.Fatalf("saved %d requests, first body %d bytes", len(f.saved), len(f.saved[0].Body))
	}
	f.click(locales.DetailHex())
	if !f.tester.HasText("00 01 02 03") {
		t.Fatalf("hex shows %v", f.tester.Texts())
	}
}

func TestInvalidUTF8BodyOpensHexWithoutRaw(t *testing.T) {
	f := bodyFixture(t, "text/plain", "caf\xe9 \xff", true)
	if f.panel.responseBody.Active() != logic.BodyTabHex || slices.Contains(f.panel.responseBody.tabs, logic.BodyTabRaw) {
		t.Fatalf("active %q, tabs %v", f.panel.responseBody.Active(), f.panel.responseBody.tabs)
	}
	f.has("63 61 66 E9 20 FF")
	f.lacks("EF BF BD")
}

func TestHexPointerSelectsBytes(t *testing.T) {
	f := bodyFixture(t, "text/plain", strings.Repeat("A", 100), false)
	f.click(locales.DetailHex())
	hex := f.panel.responseBody.hex
	row, ok := f.tester.Find("00000")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	x, y := row.X+200, row.Y+row.H/2
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x+60, y+22)
	f.tester.Frame()
	f.tester.Move(x+60, y+22)
	f.tester.Frame()
	f.tester.Release(x+60, y+22)
	f.settle()
	start, end, selected := hex.SelectionRange()
	if !selected || end-start < 2 {
		t.Fatalf("%d %d %v", start, end, selected)
	}
}

func TestHexResetsOnReplacement(t *testing.T) {
	f := bodyFixture(t, "text/plain", "hello", false)
	f.click(locales.DetailHex())
	hex := f.panel.responseBody.hex
	hex.anchor, hex.focus, hex.hasChoice = 0, 1, true
	body := *f.data.body
	body.ResponseBody = "hello world"
	f.data.body = &body
	f.settle()
	if _, _, selected := hex.SelectionRange(); !selected {
		t.Fatal("an append keeps the selection")
	}
	body.ResponseBody = "other text"
	f.data.body = &body
	f.settle()
	if _, _, selected := hex.SelectionRange(); selected {
		t.Fatal("a replacement clears the selection")
	}
}

func TestHexBuildsOnlyVisibleRowsOf500MB(t *testing.T) {
	f := bodyFixture(t, "text/plain", "x", false)
	f.click(locales.DetailHex())
	// Frame does not advance the clock: let control transitions finish before
	// measuring the work of two settled frames.
	f.tester.Advance(time.Second)
	hex := f.panel.responseBody.hex
	hex.source = hugeSource{500 * 1024 * 1024}
	hex.RowBuilds = 0
	f.tester.Frame()
	f.tester.Frame()
	if hex.RowBuilds == 0 || hex.RowBuilds > 200 {
		t.Fatalf("built %d rows", hex.RowBuilds)
	}
}

type hugeSource struct{ size int64 }

func (s hugeSource) Len() int64 { return s.size }
func (s hugeSource) Get(index int64) (byte, bool) {
	if index < 0 || index >= s.size {
		return 0, false
	}
	return byte(index), true
}
func (s hugeSource) Range(int64, int64) iter.Seq[[]byte] { return func(func([]byte) bool) {} }

func BenchmarkHexFrame500MB(b *testing.B) {
	f := bodyFixture(b, "text/plain", "x", false)
	f.panel.responseBody.active = logic.BodyTabHex
	f.settle()
	f.panel.responseBody.hex.source = hugeSource{500 * 1024 * 1024}
	f.settle()
	for b.Loop() {
		f.tester.Frame()
	}
}

func sseFixture(t testing.TB, body string) *fixture {
	return bodyFixture(t, "text/event-stream", body, false)
}

func TestSSEOpensByDefaultAndListsEvents(t *testing.T) {
	f := sseFixture(t, "id: 7\nevent: hello\ndata: first\n\ndata: second\n\n")
	if f.panel.responseBody.Active() != logic.BodyTabSSE {
		t.Fatal(f.panel.responseBody.Active())
	}
	for _, text := range []string{"hello", "7", "first", "second"} {
		f.has(text)
	}
	f.has(locales.DetailSseMessageCount("2"))
}

func TestSSEMessageListColumns(t *testing.T) {
	f := sseFixture(t, "event: hello\ndata: first\n\nid: 7\nevent: next\ndata: second\n\n")
	// The columns share the console's row height, with a dash for a message
	// before any ID.
	for _, title := range []string{locales.DetailSseId(), locales.DetailSseEventType(), locales.DetailSsePreview()} {
		f.has(title)
	}
	for _, text := range []string{"—", "hello", "first", "7", "next", "second"} {
		f.has(text)
	}
	first, _ := f.tester.Find("first")
	second, _ := f.tester.Find("second")
	if second.Y-first.Y != sseRowHeight {
		t.Fatalf("rows are %v apart, want %v", second.Y-first.Y, sseRowHeight)
	}
}

func TestSSEMessageListResizesColumns(t *testing.T) {
	f := sseFixture(t, "id: 7\nevent: hello\ndata: first\n\n")
	view := f.panel.responseBody.sse
	for _, column := range []struct {
		id, title, nextCell string
	}{
		{"id", locales.DetailSseId(), "hello"},
		{"event", locales.DetailSseEventType(), "first"},
	} {
		header, ok := f.tester.AccessByRole(ui.RoleColumnHeader, column.title)
		if !ok {
			t.Fatalf("missing header %q", column.title)
		}
		cell, ok := f.tester.Find(column.nextCell)
		if !ok {
			t.Fatalf("missing cell %q", column.nextCell)
		}
		x, y := header.Bounds.X+header.Bounds.W-2, header.Bounds.Y+header.Bounds.H/2
		f.tester.Move(x, y)
		f.settle()
		if got := f.tester.Cursor(); got != ui.CursorResizeEW {
			t.Fatalf("%q resize edge has cursor %v", column.title, got)
		}
		const delta = 40
		f.tester.Press(x, y)
		f.tester.Frame()
		f.tester.Move(x+delta, y)
		f.tester.Frame()
		f.tester.Release(x+delta, y)
		f.settle()
		if got, want := view.table.List.Columns.Widths[column.id], header.Bounds.W+delta; got != want {
			t.Fatalf("column %q width = %v, want %v", column.title, got, want)
		}
		if moved, ok := f.tester.Find(column.nextCell); !ok || moved.X != cell.X+delta {
			t.Fatalf("column resize did not move cell %q: before %v, after %v", column.nextCell, cell, moved)
		}
		if view.open {
			t.Fatal("resizing opened the message detail")
		}
	}
	f.click(locales.DetailSseOpenMessage("1"))
	f.has(locales.DetailSseMessageDetail())

	// Replacing the stream closes its detail and keeps the column widths.
	widths := view.table.List.Columns.Widths
	f.data.body = &proxyservice.TrafficBodyView{ResponseBody: "data: replacement\n\n"}
	f.settle()
	if view.open || !maps.Equal(view.table.List.Columns.Widths, widths) {
		t.Fatalf("stream reset: detail open %v, widths %v", view.open, view.table.List.Columns.Widths)
	}
}

func TestSSEMessagesOpenByKeyboard(t *testing.T) {
	f := sseFixture(t, "data: first\n\ndata: second\n\n")
	view := f.panel.responseBody.sse
	f.openMessage(locales.DetailSseOpenMessage("1"))
	if !view.open || view.selected.Sequence != 1 {
		t.Fatalf("open %v selected %+v", view.open, view.selected)
	}
	// Closing gives the focus back to the list, whose keys move from the
	// message opened, and Enter opens the one they reach.
	f.tester.Key(0, ui.KeyEscape)
	f.tester.Advance(time.Second)
	f.settle()
	if view.open {
		t.Fatal("Escape left the detail open")
	}
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	if view.open {
		t.Fatal("a key opened a message")
	}
	f.tester.Key(0, ui.KeyEnter)
	f.tester.Advance(time.Second)
	f.settle()
	if !view.open || view.selected.Sequence != 2 {
		t.Fatalf("open %v selected %+v", view.open, view.selected)
	}
}

func TestSSEIncompleteNotice(t *testing.T) {
	f := sseFixture(t, "data: done\n\ndata: tail")
	f.has(locales.DetailSseIncomplete())
	f.has(locales.DetailSseMessageCount("1"))
}

func TestSSEBinaryExplains(t *testing.T) {
	f := bodyFixture(t, "text/event-stream", "data: x\n\n", true)
	f.has(locales.DetailSseUnavailable())
	f.click(locales.DetailHex())
	if f.panel.responseBody.Active() != logic.BodyTabHex {
		t.Fatal("hex")
	}
}

func TestSSEAppendKeepsEvents(t *testing.T) {
	f := sseFixture(t, "data: first\n\n")
	view := f.panel.responseBody.sse
	first := view.events[0]
	body := *f.data.body
	body.ResponseBody += "data: next\n\n"
	f.data.body = &body
	f.settle()
	if len(view.events) != 2 || view.events[0] != first {
		t.Fatalf("%d", len(view.events))
	}
	body.ResponseBody = "data: replacement\n\n"
	f.data.body = &body
	f.settle()
	if len(view.events) != 1 || view.events[0].Preview != "replacement" {
		t.Fatalf("%+v", view.events)
	}
}

func TestSSEDetailCopiesAndSavesSelectedView(t *testing.T) {
	f := sseFixture(t, "id: 1\ndata: {\"a\":1}\n\n")
	f.click(locales.DetailSseOpenMessage("1"))
	f.has(locales.DetailSseMessageDetail())
	f.click(locales.DetailSseCopyMessage())
	if got := f.tester.Clipboard(); got != `{"a":1}` {
		t.Fatalf("%q", got)
	}
	view := f.panel.responseBody.sse
	view.detailTab = "raw"
	f.settle()
	f.click(locales.DetailSseCopyMessage())
	if got := f.tester.Clipboard(); got != "id: 1\ndata: {\"a\":1}\n\n" {
		t.Fatalf("%q", got)
	}
	view.detailTab = "formatted"
	f.settle()
	f.click(locales.DetailSseSaveMessage())
	if len(f.saved) != 1 || f.saved[0].Body != "{\n  \"a\": 1\n}" || f.saved[0].ContentType != "application/json" {
		t.Fatalf("%+v", f.saved)
	}
}

func TestSSEMessageDetailContent(t *testing.T) {
	f := sseFixture(t, "data: a\n\ndata: b\n\ndata: c\n\nid: abc\nevent: update\ndata: {\"k\":1}\n\n")
	f.click(locales.DetailSseOpenMessage("4"))
	f.tester.Advance(time.Second)
	f.settle()
	for _, text := range []string{"#4", "update", locales.DetailSseId() + ": abc"} {
		f.has(text)
	}
	f.has(locales.DetailFormatted())
	if view := f.panel.responseBody.sse; view.detailView.WordWrap() {
		t.Fatal("wrap starts on")
	}

	// Turning on wrap wraps the text and shows Wrap as pressed.
	f.click(locales.DetailWrapBody())
	if info, ok := f.tester.Access(locales.DetailWrapBody()); !ok || !info.Checked || !f.panel.responseBody.sse.detailView.WordWrap() {
		t.Fatalf("wrap %+v", info)
	}

	// The Formatted view highlights JSON.
	f.click(locales.DetailFormatted())
	if view := f.panel.responseBody.sse; view.detailTab != "formatted" || view.detailView.State().Buffer.Language() == nil {
		t.Fatalf("tab %q", view.detailTab)
	}
	f.panel.responseBody.sse.open = false
	f.settle()

	f.click(locales.DetailSseOpenMessage("1"))
	f.has("#1")
	f.has(locales.DetailSseId() + ": —")
	if f.panel.responseBody.sse.detailView.WordWrap() {
		t.Fatal("wrap stayed on for the next message")
	}
}

func TestSSEMessageFileNames(t *testing.T) {
	f := sseFixture(t, "id: 1\ndata: {\"a\":1}\n\n")
	f.click(locales.DetailSseOpenMessage("1"))
	f.tester.Advance(time.Second)
	f.settle()
	f.click(locales.DetailSseSaveMessage())
	f.click(locales.DetailFormatted())
	f.click(locales.DetailSseSaveMessage())
	f.click(locales.DetailSseRawEvent())
	f.click(locales.DetailSseSaveMessage())
	if got, want := f.pickedNames(), []string{"sse-message.txt", "sse-message.json", "sse-event.txt"}; !slices.Equal(got, want) {
		t.Fatalf("suggested %v, want %v", got, want)
	}
}

func TestSSEFollowAndJumpToLatest(t *testing.T) {
	var body strings.Builder
	for index := range 500 {
		fmt.Fprintf(&body, "data: row-%d\n\n", index)
	}
	f := sseFixture(t, body.String())
	view := f.panel.responseBody.sse
	f.has("row-499")
	checkVirtualized := func() {
		t.Helper()
		if cells := view.table.CellBuilds; cells == 0 || cells > 50*len(sseColumnIDs) {
			t.Fatalf("built %d cells for a 500-event stream", cells)
		}
	}
	checkVirtualized()
	view.table.List.ScrollTo(0, ui.Start)
	f.settle()
	f.settle()
	f.has("row-0")
	checkVirtualized()
	f.has(locales.DetailSseJumpToLatest())
	f.click(locales.DetailSseJumpToLatest())
	f.settle()
	f.has("row-499")
	checkVirtualized()
}

func TestLoadingAndMissingBody(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	f.panel.responseTab = tabResponseBody
	f.settle()
	f.has(locales.DetailBodyEmpty())
	f.data.loading = true
	f.settle()
	f.lacks(locales.DetailBodyEmpty())
}

func TestNoSelection(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.has(locales.DetailSelectRequest())
}

func TestSplitIsSavedPerLayout(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	if f.panel.split != defaultSplit {
		t.Fatal(f.panel.split)
	}
	f.settings.SetTrafficDetailSplit(splitName(settingservice.TrafficDetailLayoutVertical), 62)
	if got := f.settings.TrafficDetailSplit(splitName(settingservice.TrafficDetailLayoutVertical), 0); got != 62 {
		t.Fatal(got)
	}
	if got := f.settings.TrafficDetailSplit(splitName(settingservice.TrafficDetailLayoutHorizontal), 50); got != 50 {
		t.Fatal(got)
	}
}

func (f *fixture) setDark(dark bool) {
	mode := apptheme.ModeLight
	if dark {
		mode = apptheme.ModeDark
	}
	if err := f.settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
}

func TestWebSocketMessageWrap(t *testing.T) {
	payload := `{"type":"hello","payload":"` + strings.Repeat("a received text message. ", 18) + `"}`
	f := wsFixture(t,
		&proxyservice.WebSocketMessage{Direction: "receive", MsgType: "text", Data: payload, DataSize: len(payload)},
	)
	f.openMessage(payload)
	view := f.panel.messages.view.State()
	unwrapped := view.LineTop(1)
	f.click(locales.DetailWrapBody())
	if view.LineTop(1) <= unwrapped {
		t.Fatal("the long message did not wrap")
	}
}
