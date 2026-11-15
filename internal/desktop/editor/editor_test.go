package editor

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/database"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type sentRequest struct {
	config      proxyservice.SendRequestConfig
	method, url string
	headers     []logic.HeaderField
	body        proxyservice.SendRequestBody
}

type fakeRequests struct {
	mu        sync.Mutex
	sent      []sentRequest
	response  proxyservice.SendRequestResponse
	sendErr   error
	recovered *proxyservice.RequestBodyRecoveryResult
	wsSent    []proxyservice.WebSocketSendRequest
	wsConnect []proxyservice.WebSocketConnectRequest
}

func (f *fakeRequests) SendHTTPRequest(_ context.Context, config proxyservice.SendRequestConfig, method, url string, headers []logic.HeaderField, body proxyservice.SendRequestBody) (proxyservice.SendRequestResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentRequest{config, method, url, slices.Clone(headers), body})
	return f.response, f.sendErr
}
func (f *fakeRequests) DisconnectHTTPRequestStream(string) error { return nil }
func (f *fakeRequests) ConnectWebSocket(_ context.Context, request proxyservice.WebSocketConnectRequest) (proxyservice.WebSocketConnectResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wsConnect = append(f.wsConnect, request)
	return proxyservice.WebSocketConnectResponse{SessionID: "ws-1", Status: "connected"}, nil
}
func (f *fakeRequests) SendWebSocketMessage(request proxyservice.WebSocketSendRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wsSent = append(f.wsSent, request)
	return nil
}
func (f *fakeRequests) DisconnectWebSocket(string) error { return nil }
func (f *fakeRequests) RecoverRequestBodyForEditing(string, []logic.HeaderField, proxyservice.TrafficBodyView) (proxyservice.RequestBodyRecoveryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recovered != nil {
		return *f.recovered, nil
	}
	return proxyservice.RequestBodyRecoveryResult{}, errors.New("no recovery")
}

func (f *fakeRequests) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeRequests) last() sentRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[len(f.sent)-1]
}

type fakeSaved struct{}

func (fakeSaved) SaveHTTPRequest(_, name string, request *apicollectionservice.SavedHTTPRequest) (*apicollectionservice.APICollectionRequest, error) {
	return &apicollectionservice.APICollectionRequest{ID: "saved", Name: name, Type: apicollectionservice.APICollectionNodeTypeHTTP, HTTP: request, UpdatedAt: 5}, nil
}
func (fakeSaved) SaveWebSocketRequest(_, name string, request *apicollectionservice.SavedWebSocketRequest) (*apicollectionservice.APICollectionRequest, error) {
	return &apicollectionservice.APICollectionRequest{ID: "saved-ws", Name: name, Type: apicollectionservice.APICollectionNodeTypeWebSocket, WebSocket: request, UpdatedAt: 5}, nil
}
func (fakeSaved) UpdateHTTPRequest(id string, request *apicollectionservice.SavedHTTPRequest) (*apicollectionservice.APICollectionRequest, error) {
	return &apicollectionservice.APICollectionRequest{ID: id, Name: "same", Type: apicollectionservice.APICollectionNodeTypeHTTP, HTTP: request, UpdatedAt: 6}, nil
}
func (fakeSaved) UpdateWebSocketRequest(id string, request *apicollectionservice.SavedWebSocketRequest) (*apicollectionservice.APICollectionRequest, error) {
	return &apicollectionservice.APICollectionRequest{ID: id, Name: "same", Type: apicollectionservice.APICollectionNodeTypeWebSocket, WebSocket: request, UpdatedAt: 6}, nil
}

type fixture struct {
	t        testing.TB
	requests *fakeRequests
	data     *store.Store
	window   *runtimetest.Window
	tester   *ui.Tester
	notifier *widgets.Notifier
	confirm  *widgets.ConfirmDialog
	dispatch *shortcuts.Dispatcher
	editor   *Editor
	python   bool
	// picked are the names the save dialog suggested, in order.
	pickedMu sync.Mutex
	picked   []string
	saved    []string
}

// savedBodies returns the bodies saved, in order.
func (f *fixture) savedBodies() []string {
	f.pickedMu.Lock()
	defer f.pickedMu.Unlock()
	return slices.Clone(f.saved)
}

// pickedNames returns the names the save dialog suggested.
func (f *fixture) pickedNames() []string {
	f.pickedMu.Lock()
	defer f.pickedMu.Unlock()
	return slices.Clone(f.picked)
}

func newFixture(t testing.TB, backend store.RequestBackend, requests *fakeRequests) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
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
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	data := store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: service}, Requests: backend, Saved: fakeSaved{}})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, requests: requests, data: data, window: window, notifier: &widgets.Notifier{}, confirm: &widgets.ConfirmDialog{}}
	f.dispatch = shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig { return nil })
	f.editor = New(data, Host{
		ResolveFile: func(path string) (*logic.RequestFileValue, error) {
			return &logic.RequestFileValue{Path: path, Name: filepath.Base(path), Size: 7}, nil
		},
		PickFile:      func() (string, error) { return "/tmp/picked.bin", nil },
		PythonEnabled: func() bool { return f.python },
		Inspect: inspect.Dependencies{Runtime: data.Runtime, Settings: data.Settings, Notifier: f.notifier,
			PickSavePath: func(name string) (string, error) {
				f.pickedMu.Lock()
				defer f.pickedMu.Unlock()
				f.picked = append(f.picked, name)
				return "/tmp/out", nil
			},
			SaveBody: func(request proxyservice.SaveBodyToFileRequest) error {
				f.pickedMu.Lock()
				defer f.pickedMu.Unlock()
				f.saved = append(f.saved, request.Body)
				return nil
			},
		},
	}, f.dispatch, f.notifier, f.confirm)
	view := func(c *ui.Context) {
		theme := nui.App(c, data.Settings.Theme().Config)
		f.dispatch.Begin()
		ui.Column(c).Fill().Children(func() {
			f.editor.TabBar(c, theme)
			ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
				f.editor.Build(c, theme, data.Workspace.ActiveTab())
			})
		})
		f.notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 5})
		if f.confirm.Open() {
			f.dispatch.NoteModal()
		}
		f.confirm.Build(c, theme)
		f.dispatch.Install(c)
	}
	f.tester = ui.NewTester(view, 1400, 800)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

func newFake(t testing.TB) *fixture {
	requests := &fakeRequests{response: proxyservice.SendRequestResponse{StatusCode: 200, StatusText: "OK", Protocol: "HTTP/1.1", Body: "hello", HeaderFields: []logic.HeaderField{{Name: "Content-Type", Value: "text/plain"}}}}
	return newFixture(t, requests, requests)
}

func (f *fixture) settle() {
	for range 4 {
		f.data.Runtime.Jobs.Wait()
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

func (f *fixture) newHTTP() *logic.WorkspaceTab {
	f.t.Helper()
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	f.settle()
	return f.data.Workspace.FindTab(key)
}

func (f *fixture) center(label string) (float32, float32) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	return rect.X + rect.W/2, rect.Y + rect.H/2
}

func (f *fixture) tabKeys() []string {
	keys := make([]string, len(f.data.Workspace.Tabs))
	for index, tab := range f.data.Workspace.Tabs {
		keys[index] = tab.Key
	}
	return keys
}

func (f *fixture) hasError(text string) bool { return f.tester.HasText(text) }

func titled(t *logic.WorkspaceTab, title string) *logic.WorkspaceTab {
	t.Title = title
	if t.HTTP != nil {
		t.HTTP.Name = title
	}
	return t
}

func TestShortcutsManageTabs(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	if !f.dispatch.Invoke(shortcuts.WorkspaceNewHTTPRequest) {
		t.Fatal("new http")
	}
	f.settle()
	if !f.dispatch.Invoke(shortcuts.WorkspaceNewWebSocket) {
		t.Fatal("new ws")
	}
	f.settle()
	if len(workspace.Tabs) != 3 || workspace.ActiveTab().WebSocket == nil {
		t.Fatalf("%v", f.tabKeys())
	}
	second := workspace.Tabs[1].Key
	f.dispatch.Invoke(shortcuts.WorkspacePreviousTab)
	f.settle()
	if workspace.ActiveKey != second {
		t.Fatal(workspace.ActiveKey)
	}
	f.dispatch.Invoke(shortcuts.WorkspaceNextTab)
	f.dispatch.Invoke(shortcuts.WorkspaceNextTab)
	f.settle()
	if workspace.ActiveKey != logic.CaptureTabKey {
		t.Fatal("wraps to capture", workspace.ActiveKey)
	}
	if f.dispatch.Invoke(shortcuts.WorkspaceCloseTab) {
		t.Fatal("capture cannot close")
	}
	workspace.Activate(second)
	f.settle()
	f.dispatch.Invoke(shortcuts.WorkspaceCloseTab)
	f.settle()
	if len(workspace.Tabs) != 2 {
		t.Fatalf("%v", f.tabKeys())
	}
}

func TestDirtyTabMarkerAndGuards(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	tab := f.newHTTP()
	f.click(locales.WorkspaceHttpRequestUrlPlaceholder())
	f.tester.Type("https://example.com/a")
	f.settle()
	if !workspace.IsDirty(tab.Key) {
		t.Fatal("dirty after typing")
	}
	if _, ok := f.tester.Find(locales.WorkspaceTabBarUnsavedChanges()); !ok {
		t.Fatalf("marker missing: %v", f.tester.Texts())
	}
	if tab.Title != "example.com/a" && !strings.Contains(tab.Title, "example.com") {
		t.Fatal(tab.Title)
	}
	f.dispatch.Invoke(shortcuts.WorkspaceCloseTab)
	f.settle()
	if !f.confirm.Open() || len(workspace.Tabs) != 2 {
		t.Fatal("must ask first")
	}
	f.confirm.Accept()
	f.settle()
	if len(workspace.Tabs) != 1 {
		t.Fatalf("%v", f.tabKeys())
	}
	dirty := f.newHTTP()
	dirty.HTTP.URL = "https://x.test/"
	f.settle()
	if err := menutest.Open(f.tester, f.settle, DisplayTitle(dirty)); err != nil {
		t.Fatal(err)
	}
	if err := menutest.Choose(f.tester, f.settle, locales.WorkspaceTabContextForceCloseCurrent()); err != nil {
		t.Fatal(err)
	}
	if f.confirm.Open() || len(workspace.Tabs) != 1 {
		t.Fatal("force close skips the prompt")
	}
}

func TestCloseVariants(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	var tabs []*logic.WorkspaceTab
	for index := range 4 {
		tab := titled(f.newHTTP(), string(rune('A'+index))+"-tab")
		tabs = append(tabs, tab)
	}
	f.settle()
	menu := func(title, item string) {
		t.Helper()
		if err := menutest.Open(f.tester, f.settle, title); err != nil {
			t.Fatal(err)
		}
		if err := menutest.Choose(f.tester, f.settle, item); err != nil {
			t.Fatal(err)
		}
	}
	tabs[3].HTTP.URL = "https://dirty.test/"
	f.settle()
	menu("B-tab", locales.WorkspaceTabContextCloseRight())
	if !f.confirm.Open() || len(workspace.Tabs) != 5 {
		t.Fatalf("right guarded: %v", f.tabKeys())
	}
	f.confirm.Accept()
	f.settle()
	if len(workspace.Tabs) != 3 {
		t.Fatalf("%v", f.tabKeys())
	}
	menu("A-tab", locales.WorkspaceTabContextCloseOther())
	if len(workspace.Tabs) != 2 || workspace.Tabs[1].Key != tabs[0].Key {
		t.Fatalf("%v", f.tabKeys())
	}
	menu("A-tab", locales.WorkspaceTabContextCloseAll())
	if len(workspace.Tabs) != 1 || workspace.ActiveKey != logic.CaptureTabKey {
		t.Fatalf("%v", f.tabKeys())
	}
}

func TestDragReordersWithoutChangingActive(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	a := titled(f.newHTTP(), "A-tab")
	b := titled(f.newHTTP(), "B-tab")
	f.settle()
	workspace.Activate(b.Key)
	f.settle()
	x, y := f.center("A-tab")
	f.tester.Press(x, y)
	f.tester.Frame()
	for step := float32(30); step <= 180; step += 30 {
		f.tester.Move(x+step, y)
		f.tester.Frame()
	}
	f.tester.Release(x+180, y)
	f.settle()
	if got := f.tabKeys(); !slices.Equal(got, []string{logic.CaptureTabKey, b.Key, a.Key}) {
		t.Fatalf("%v", got)
	}
	if workspace.ActiveKey != b.Key {
		t.Fatal("active changed", workspace.ActiveKey)
	}
	x, y = f.center("A-tab")
	f.tester.Press(x, y)
	f.tester.Frame()
	for step := float32(40); step <= 800; step += 40 {
		f.tester.Move(x-step, y)
		f.tester.Frame()
	}
	f.tester.Release(x-800, y)
	f.settle()
	if got := f.tabKeys(); got[0] != logic.CaptureTabKey || got[1] != a.Key {
		t.Fatalf("capture stays first: %v", got)
	}
}

func TestQuerySynchronizesWithURL(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL = "https://example.com/a?x=1&y=2"
	f.settle()
	if len(state.Params) != 2 && len(state.Params) != 3 {
		t.Fatalf("%+v", state.Params)
	}
	if state.Params[0].Key != "x" || state.Params[1].Value != "2" {
		t.Fatalf("%+v", state.Params)
	}
	state.ActiveRequestTab = logic.RequestTabQuery
	f.settle()
	f.click(locales.WorkspaceHttpRequestQueryValuePlaceholder() + " 1")
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("9")
	f.settle()
	if state.URL != "https://example.com/a?x=9&y=2" {
		t.Fatal(state.URL)
	}
	if err := f.tester.Click(locales.WorkspaceHttpRequestAddQueryParameter()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if state.URL != "https://example.com/a?x=9&y=2" {
		t.Fatal("an empty row leaves the URL alone", state.URL)
	}
}

func TestHeaderOrderReachesSend(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL = "https://example.com/"
	state.Method = "POST"
	state.Headers = []logic.EditableRow{{Key: "X-Zulu", Value: "1", Enabled: true}, {Key: "accept", Value: "*/*", Enabled: true}, {Key: "skip", Value: "no", Enabled: false}, {Key: "X-Alpha", Value: "2", Enabled: true}}
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	var names []string
	for _, field := range f.requests.last().headers {
		names = append(names, field.Name)
	}
	if want := []string{"X-Zulu", "accept", "X-Alpha"}; !slices.Equal(names, want) && !slices.Equal(names[1:], want) {
		t.Fatalf("%v", names)
	}
	if !f.tester.HasText("200") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestInvalidInputIsNotSent(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	cases := []struct {
		name    string
		prepare func()
		message string
		tab     string
	}{
		{"url", func() { state.URL = "not a url" }, locales.WorkspaceHttpRequestInvalidUrl(), ""},
		{"scheme", func() { state.URL = "ftp://example.com" }, locales.WorkspaceHttpRequestInvalidUrlScheme(), ""},
		{"method", func() { state.URL, state.Method = "http://example.com", " " }, locales.WorkspaceHttpRequestInvalidMethod(), ""},
		{"header", func() {
			state.Method = "GET"
			state.ActiveRequestTab = logic.RequestTabBody
			state.Headers = []logic.EditableRow{{Key: "bad name", Value: "1", Enabled: true}}
		}, locales.WorkspaceHttpRequestHeaderInvalidName("bad name"), logic.RequestTabHeaders},
		{"file", func() {
			state.Headers = nil
			state.RequestBodyType = logic.BodyTypeFile
		}, locales.WorkspaceHttpRequestFileRequired(), ""},
		{"form file", func() {
			state.RequestBodyType = logic.BodyTypeFormData
			state.RequestBodyFormData = []logic.FormDataItem{{ID: "a", Enabled: true, Name: "up", ItemType: logic.FormDataItemFile}}
		}, locales.WorkspaceHttpRequestFormDataFileRequired("up"), ""},
	}
	for _, testCase := range cases {
		testCase.prepare()
		f.settle()
		f.click(locales.WorkspaceHttpRequestSend())
		if !f.hasError(testCase.message) {
			t.Fatalf("%s: no %q in %v", testCase.name, testCase.message, f.tester.Texts())
		}
		if testCase.tab != "" && state.ActiveRequestTab != testCase.tab {
			t.Fatalf("%s: tab %s", testCase.name, state.ActiveRequestTab)
		}
		if f.requests.sentCount() != 0 {
			t.Fatalf("%s sent", testCase.name)
		}
	}
}

func TestCookiesRewriteHeaders(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.Headers = []logic.EditableRow{{Key: "Accept", Value: "*/*", Enabled: true}, {Key: "Cookie", Value: "a=1; b=2", Enabled: true}}
	state.ActiveRequestTab = logic.RequestTabCookies
	f.settle()
	f.click(locales.WorkspaceHttpRequestCookieValuePlaceholder() + " 2")
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("7")
	f.settle()
	if state.Headers[1].Value != "a=1; b=7" {
		t.Fatalf("%+v", state.Headers)
	}
	state.Headers[1].Value = "c=3"
	f.settle()
	if len(f.editor.https[tab.Key].cookieRows) == 0 || f.editor.https[tab.Key].cookieRows[0].Key != "c" {
		t.Fatalf("%+v", f.editor.https[tab.Key].cookieRows)
	}
}

func TestBodyTypesReachSend(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL, state.Method = "http://example.com/", "POST"
	state.ActiveRequestTab = logic.RequestTabBody
	state.RequestBodyType = logic.BodyTypeJSON
	state.RequestBodyText = `{"a":1}`
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if body := f.requests.last().body; body.BodyType != "json" || body.Text != `{"a":1}` {
		t.Fatalf("%+v", body)
	}
	state.RequestBodyType = logic.BodyTypeURLEncoded
	state.RequestBodyURLEncoded = []logic.EditableRow{{Key: " a ", Value: "1", Enabled: true}, {Key: "off", Value: "x"}}
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if body := f.requests.last().body; len(body.URLEncoded) != 1 || body.URLEncoded[0].Name != "a" {
		t.Fatalf("%+v", body)
	}
	state.RequestBodyType = logic.BodyTypeNone
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if body := f.requests.last().body; body.BodyType != "none" {
		t.Fatalf("%+v", body)
	}
}

func TestFileDropAndPicker(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.ActiveRequestTab = logic.RequestTabBody
	state.RequestBodyType = logic.BodyTypeFile
	f.settle()
	if !f.tester.HasText(locales.WorkspaceHttpRequestFileDropHint()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	x, y := f.center(locales.WorkspaceHttpRequestFileDropHint())
	f.tester.DropFiles(x, y, "/tmp/dropped.bin")
	f.settle()
	if state.RequestBodyFile == nil || state.RequestBodyFile.Path != "/tmp/dropped.bin" {
		t.Fatalf("%+v", state.RequestBodyFile)
	}
	f.click(locales.WorkspaceHttpRequestFileChoose())
	if state.RequestBodyFile.Path != "/tmp/picked.bin" {
		t.Fatalf("%+v", state.RequestBodyFile)
	}
	f.click(locales.WorkspaceHttpRequestFileClear())
	if state.RequestBodyFile != nil {
		t.Fatal("clear")
	}
}

func TestDropOnOneFormRowOnly(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.ActiveRequestTab = logic.RequestTabBody
	state.RequestBodyType = logic.BodyTypeFormData
	state.RequestBodyFormData = []logic.FormDataItem{
		{ID: "one", Enabled: true, Name: "first", ItemType: logic.FormDataItemText, Value: "keep"},
		{ID: "two", Enabled: true, Name: "second", ItemType: logic.FormDataItemFile},
	}
	f.settle()
	x, y := f.center(locales.WorkspaceHttpRequestFormDataRowMenuUpload() + " 2")
	f.tester.DropFiles(x, y, "/tmp/row-two.bin")
	f.settle()
	if state.RequestBodyFormData[1].File == nil || state.RequestBodyFormData[1].File.Path != "/tmp/row-two.bin" {
		t.Fatalf("%+v", state.RequestBodyFormData)
	}
	if state.RequestBodyFormData[0].File != nil || state.RequestBodyFormData[0].ItemType != logic.FormDataItemText || state.RequestBodyFormData[0].Value != "keep" {
		t.Fatalf("other row changed: %+v", state.RequestBodyFormData[0])
	}
}

func TestFormDataRowMenuActions(t *testing.T) {
	f := newFake(t)
	state := f.newHTTP().HTTP
	state.ActiveRequestTab, state.RequestBodyType = logic.RequestTabBody, logic.BodyTypeFormData
	first := logic.FormDataItem{ID: "one", Enabled: true, Name: "first", ItemType: logic.FormDataItemText, Value: "keep"}
	state.RequestBodyFormData = []logic.FormDataItem{
		first,
		{ID: "two", Enabled: true, Name: "second", ItemType: logic.FormDataItemText, Value: "replace"},
	}
	f.settle()

	choose := func(label string) {
		t.Helper()
		f.click(locales.WorkspaceHttpRequestFormDataRowMenuActions() + " 2")
		if err := menutest.Choose(f.tester, f.settle, label); err != nil {
			t.Fatal(err)
		}
		if menutest.Shown(f.tester) {
			t.Fatal("row menu remained open after selection")
		}
		if state.RequestBodyFormData[0] != first {
			t.Fatalf("other row changed: %+v", state.RequestBodyFormData[0])
		}
	}
	choose(locales.WorkspaceHttpRequestFormDataRowMenuFile())
	if row := state.RequestBodyFormData[1]; row.ItemType != logic.FormDataItemFile || row.Value != "" {
		t.Fatalf("row did not switch to file: %+v", row)
	}
	choose(locales.WorkspaceHttpRequestFormDataRowMenuUpload())
	if file := state.RequestBodyFormData[1].File; file == nil || file.Path != "/tmp/picked.bin" {
		t.Fatalf("row did not receive the picked file: %+v", file)
	}
	choose(locales.WorkspaceHttpRequestFormDataRowMenuText())
	if row := state.RequestBodyFormData[1]; row.ItemType != logic.FormDataItemText || row.File != nil {
		t.Fatalf("row did not switch to text: %+v", row)
	}
	choose(locales.WorkspaceHttpRequestFormDataRowMenuDelete())
	if len(state.RequestBodyFormData) != 1 {
		t.Fatalf("row was not deleted: %+v", state.RequestBodyFormData)
	}
}

func TestSettingsDefaultsAndRouting(t *testing.T) {
	f := newFake(t)
	saved := &apicollectionservice.SavedHTTPRequest{Method: "GET", URL: "http://example.com/"}
	state := logic.BuildHTTPEditorStateFromSaved(saved, "old")
	if state.Settings.MaxRedirects != 0 || state.Settings.SkipVerifyTLS || state.Settings.ProxyMode != logic.ProxyModeNone || state.Settings.Protocol != string(logic.ProtocolAuto) || state.Settings.TLSClientHelloID != "golang" {
		t.Fatalf("%+v", state.Settings)
	}
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, state)
	state.ActiveRequestTab = logic.RequestTabSettings
	state.URL = "https://example.com/x"
	state.Settings.MaxRedirects = 3
	state.Settings.ProxyMode = logic.ProxyModeCustom
	state.Settings.CustomProxy = "http://127.0.0.1:1"
	state.Settings.HTTP2Fingerprint = "1:2|3|4|m,a,s,p"
	state.Settings.SkipVerifyTLS = true
	f.settle()
	if !f.tester.HasText(locales.WorkspaceHttpRequestCustomProxy()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.WorkspaceHttpRequestSend())
	sent := f.requests.last()
	if sent.config.MaxRedirects != 3 || sent.config.CustomProxy != "http://127.0.0.1:1" || !sent.config.SkipVerifyTLS || sent.config.HTTP2Fingerprint == "" {
		t.Fatalf("%+v", sent.config)
	}
	state.Settings.ProxyMode = logic.ProxyModeSystem
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if f.requests.last().config.CustomProxy != "" {
		t.Fatal("custom proxy only in custom mode")
	}
	f.click(locales.WorkspaceHttpRequestEnableMitmProxy())
	if state.Settings.ProxyMode != logic.ProxyModeMITM {
		t.Fatal(state.Settings.ProxyMode)
	}
	f.click(locales.WorkspaceHttpRequestDisableMitmProxy())
	if state.Settings.ProxyMode != logic.ProxyModeSystem {
		t.Fatal(state.Settings.ProxyMode)
	}
	_ = key
}

func TestPythonSwitchesConsoleAndScript(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL = "http://example.com/"
	state.ActiveRequestTab = logic.RequestTabScript
	state.InlineScriptEnabled = true
	f.settle()
	if !f.tester.HasText(locales.WorkspaceHttpRequestInlineScriptRuntimeDisabled()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.ActiveRequestTab = logic.RequestTabHeaders
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if state.ActiveRequestTab != logic.RequestTabScript || f.requests.sentCount() != 0 {
		t.Fatal("runtime disabled")
	}
	f.python = true
	state.InlineScriptSource = "  "
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if !f.tester.HasText(locales.WorkspaceHttpRequestInlineScriptEmpty()) || f.requests.sentCount() != 0 {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.InlineScriptSource = "print(1)"
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	sent := f.requests.last()
	if sent.config.PluginExecutionID == "" || sent.config.InlinePythonScript == nil || sent.config.InlinePythonScript.Source != "print(1)" || !sent.config.DisablePlugins {
		t.Fatalf("%+v", sent.config)
	}
	if state.ActiveResponseTab != logic.ResponseTabConsole {
		t.Fatal(state.ActiveResponseTab)
	}
	if !f.tester.HasText(locales.WorkspaceHttpRequestConsoleEmpty()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.ScriptConsoleEntries = []logic.PluginLogEntry{{EventID: 1, ExecutionID: state.ScriptExecutionID, PluginID: logic.CurrentRequestScriptPluginID, Level: "info", Stream: "stdout", Message: "line one\nline two", Timestamp: 1000}}
	f.settle()
	if !f.tester.HasText("line one line two") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click("line one line two")
	if console := f.editor.https[tab.Key].response.console; !f.tester.HasText(locales.WorkspaceHttpRequestConsoleEntryDetail()) || console.view.Text() != "line one\nline two" {
		t.Fatalf("%v", f.tester.Texts())
	}
	reopened := logic.BuildHTTPEditorStateFromSaved(logic.BuildSavedHTTPRequest(state), "again")
	if reopened.PluginsEnabled || reopened.InlineScriptEnabled {
		t.Fatalf("switches stay off on reopen: %+v", reopened)
	}
	if reopened.InlineScriptSource != "print(1)" {
		t.Fatal(reopened.InlineScriptSource)
	}
}

func TestEditFromTrafficKeepsHeaderOrderAndRecoveredBody(t *testing.T) {
	f := newFake(t)
	f.requests.recovered = &proxyservice.RequestBodyRecoveryResult{BodyType: "text", Text: "complete cached body"}
	entry := &logic.TrafficEntry{ID: 9, Type: "http", Method: "POST", URL: "http://example.com/submit?q=1", Host: "example.com", Path: "/submit", Status: "completed",
		Request: &proxyservice.HTTPMessage{HeaderFields: []logic.HeaderField{{Name: "X-First", Value: "1"}, {Name: "x-second", Value: "2"}, {Name: "Accept", Value: "*/*"}}}}
	body := &proxyservice.TrafficBodyView{RequestBody: "tru"}
	var opened string
	var failure error
	f.data.Workspace.OpenFromTraffic(entry, body, logic.DraftCaptureEdit, "", func(key string, err error) { opened, failure = key, err })
	f.settle()
	if failure != nil || opened == "" {
		t.Fatal(failure, opened)
	}
	state := f.data.Workspace.FindTab(opened).HTTP
	if state.Method != "POST" || !strings.HasPrefix(state.URL, "http://example.com/submit") || state.RequestBodyText != "complete cached body" || state.SourceEntryID != 9 {
		t.Fatalf("%+v", state)
	}
	var names []string
	for _, row := range state.Headers {
		names = append(names, row.Key)
	}
	want := []string{"X-First", "x-second", "Accept"}
	if len(names) < 3 || !slices.Equal(names[len(names)-3:], want) && !slices.Equal(names[:3], want) {
		t.Fatalf("%v", names)
	}
	var unavailable error
	f.data.Workspace.OpenFromTraffic(entry, &proxyservice.TrafficBodyView{RequestBodyUnavailable: true}, logic.DraftCaptureEdit, "", func(_ string, err error) { unavailable = err })
	if !errors.Is(unavailable, logic.ErrRequestBodyUnavailable) {
		t.Fatal(unavailable)
	}
}

func TestSavedTabsTrackDirtyState(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	tab := f.newHTTP()
	tab.HTTP.URL = "http://example.com/"
	f.settle()
	var saved *apicollectionservice.APICollectionRequest
	workspace.SaveTabAsAPI(tab.Key, "", "Mine", func(request *apicollectionservice.APICollectionRequest, err error) {
		if err != nil {
			t.Error(err)
		}
		saved = request
	})
	f.settle()
	if saved == nil || tab.APIID != "saved" || tab.Title != "Mine" || workspace.IsDirty(tab.Key) {
		t.Fatalf("%+v", tab)
	}
	tab.HTTP.URL = "http://example.com/changed"
	f.settle()
	if !workspace.IsDirty(tab.Key) || tab.Title != "Mine" {
		t.Fatalf("%+v", tab)
	}
	if key, err := workspace.OpenSavedAPI(&apicollectionservice.APICollectionRequest{ID: "saved"}); err != nil || key != tab.Key {
		t.Fatal(key, err)
	}
	workspace.CloseAPITabs([]string{"saved"})
	if workspace.FindTab(tab.Key) != nil {
		t.Fatal("deleted API closes its tab")
	}
}

func TestWebSocketClientMessageStream(t *testing.T) {
	f := newFake(t)
	key := f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := f.data.Workspace.FindTab(key).WebSocket
	state.URL = "ws://example.com/socket"
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	state.ActiveRightTab = socketTabResponse
	f.settle()
	if !f.tester.HasText(locales.WorkspaceWebsocketClientNoMessages()) {
		t.Fatalf("no empty state in %v", f.tester.Texts())
	}
	state.Messages = []logic.WebSocketDisplayMessage{
		{ID: "m1", Direction: "receive", MsgType: "text", Data: "hello, world", DataSize: 12},
		{ID: "m2", Direction: "send", MsgType: "binary", Data: "AQID", DataSize: 3},
	}
	f.settle()

	// Message detail content.
	f.click("hello, world")
	f.tester.Advance(time.Second)
	f.settle()
	for _, text := range []string{locales.WorkspaceWebsocketClientMessageDetailReceive(), locales.WorkspaceWebsocketClientFilterReceive(), locales.WorkspaceWebsocketClientDraftTypeText(), locales.WorkspaceWebsocketClientBinarySize("12")} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %v", text, f.tester.Texts())
		}
	}
	f.click(locales.DetailSaveBody())
	f.tester.Key(0, ui.KeyEscape)
	f.settle()
	f.click(locales.WorkspaceWebsocketClientBinarySize("3"))
	f.tester.Advance(time.Second)
	f.settle()
	if !f.tester.HasText(locales.WorkspaceWebsocketClientDraftTypeBinaryFile()) || !f.tester.HasText(locales.WorkspaceWebsocketClientFilterSend()) {
		t.Fatalf("binary message detail: %v", f.tester.Texts())
	}
	if _, ok := f.tester.Access(locales.DetailCopyBody()); ok {
		t.Fatal("a binary message offers Copy")
	}
	f.click(locales.DetailSaveBody())
	f.tester.Key(0, ui.KeyEscape)
	f.settle()
	if got, want := f.pickedNames(), []string{"websocket-message.txt", "websocket-message.bin"}; !slices.Equal(got, want) {
		t.Fatalf("suggested %v, want %v", got, want)
	}

	binary := locales.WorkspaceWebsocketClientBinarySize("3")
	f.click(locales.WorkspaceWebsocketClientFilterAll())
	if f.tester.HasText("hello, world") || !f.tester.HasText(binary) {
		t.Fatalf("sent messages: %v", f.tester.Texts())
	}
	f.click(locales.WorkspaceWebsocketClientFilterSend())
	if !f.tester.HasText("hello, world") || f.tester.HasText(binary) {
		t.Fatalf("received messages: %v", f.tester.Texts())
	}
	f.click(locales.WorkspaceWebsocketClientFilterReceive())
	if !f.tester.HasText("hello, world") || !f.tester.HasText(binary) {
		t.Fatalf("all messages: %v", f.tester.Texts())
	}
	if _, ok := f.tester.Access("List Mode"); ok {
		t.Fatal("the message list offers a view mode toggle")
	}

	state.Messages = state.Messages[:1]
	f.settle()
	f.click(locales.WorkspaceWebsocketClientFilterAll())
	if f.tester.HasText("hello, world") || !f.tester.HasText(locales.WorkspaceWebsocketClientNoMessages()) {
		t.Fatalf("empty sent messages: %v", f.tester.Texts())
	}
	f.click(locales.WorkspaceWebsocketClientClearMessages())
	if len(state.Messages) != 0 {
		t.Fatalf("clear left messages hidden by the filter: %+v", state.Messages)
	}
}

func TestWebSocketControls(t *testing.T) {
	f := newFake(t)
	workspace := f.data.Workspace
	key := workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := workspace.FindTab(key).WebSocket
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	if !f.tester.HasText(locales.WorkspaceWebsocketClientInvalidUrl()) && !f.tester.HasText(locales.WorkspaceWebsocketClientInvalidUrlScheme()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.URL = "http://example.com"
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	if !f.tester.HasText(locales.WorkspaceWebsocketClientInvalidUrlScheme()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.URL = "ws://example.com/socket"
	state.Headers = []logic.EditableRow{{Key: "X-B", Value: "1", Enabled: true}, {Key: "X-A", Value: "2", Enabled: true}}
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	if state.ConnectionStatus != "connected" {
		t.Fatal(state.ConnectionStatus, state.ResponseError)
	}
	if got := f.requests.wsConnect[0].HeaderFields; got[len(got)-2].Name != "X-B" || got[len(got)-1].Name != "X-A" {
		t.Fatalf("%+v", got)
	}
	if !f.tester.HasText(locales.WorkspaceWebsocketClientConnectionStatusConnected()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.DraftText = "ping"
	f.settle()
	f.click(locales.WorkspaceWebsocketClientSendMessage())
	if len(f.requests.wsSent) != 1 || f.requests.wsSent[0].Text != "ping" || f.requests.wsSent[0].MsgType != "text" {
		t.Fatalf("%+v", f.requests.wsSent)
	}
	state.DraftType = draftTypeBinaryFile
	f.settle()
	if err := f.tester.Click(locales.WorkspaceWebsocketClientSendMessage()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if len(f.requests.wsSent) != 1 {
		t.Fatal("no file chosen")
	}
}

func TestSaveShortcutNeedsADirtyRequestTab(t *testing.T) {
	f := newFake(t)
	var saved []string
	f.editor.Host.SaveTab = func(key string) { saved = append(saved, key) }
	tab := f.newHTTP()
	if f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("a clean tab has nothing to save")
	}
	tab.HTTP.URL = "http://example.com/"
	f.settle()
	if !f.dispatch.Invoke(shortcuts.AppSave) || !slices.Equal(saved, []string{tab.Key}) {
		t.Fatalf("%v", saved)
	}
}

// clickTab clicks the first tab named label.
func (f *fixture) clickTab(label string) {
	f.t.Helper()
	tab, ok := f.tester.AccessByRole(ui.RoleTab, label)
	if !ok {
		f.t.Fatalf("no tab %q in %v", label, f.tester.Texts())
	}
	f.tester.ClickAt(tab.Bounds.X+tab.Bounds.W/2, tab.Bounds.Y+tab.Bounds.H/2)
	f.settle()
}

// The response tabs, which follow the state each build, switch on a click
// once a response has come.
func TestResponseTabsSwitchOnClick(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL = "https://example.com/"
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	if state.Response == nil || logic.VisibleResponseTab(state, false) != logic.ResponseTabHeaders {
		t.Fatalf("%+v", state.Response)
	}
	f.clickTab(locales.WorkspaceHttpRequestConsole())
	if state.ActiveResponseTab != logic.ResponseTabConsole {
		t.Fatalf("console click left %q", state.ActiveResponseTab)
	}
	f.clickTab(locales.WorkspaceHttpRequestResponseHeaders() + "(1)")
	if state.ActiveResponseTab != logic.ResponseTabHeaders || !f.tester.HasText("Content-Type") {
		t.Fatalf("headers click left %q: %v", state.ActiveResponseTab, f.tester.Texts())
	}
}

func TestWebSocketResponseTabsSwitchOnClick(t *testing.T) {
	f := newFake(t)
	key := f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := f.data.Workspace.FindTab(key).WebSocket
	state.URL = "ws://example.com/socket"
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	if state.ConnectionStatus != "connected" {
		t.Fatal(state.ConnectionStatus)
	}
	f.clickTab(locales.WorkspaceWebsocketClientResponse())
	if state.ActiveRightTab != socketTabResponse || !f.tester.HasText(locales.WorkspaceWebsocketClientNoMessages()) {
		t.Fatalf("response click left %q: %v", state.ActiveRightTab, f.tester.Texts())
	}
}
