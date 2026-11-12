package collection

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/pkg/database"
	apicollectionservice "github.com/josexy/flowlens/backend/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/internal/menutest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

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

type failingList struct {
	*apicollectionservice.APICollectionService
	err error
}

func (f *failingList) ListCollection() (*store.CollectionTree, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.APICollectionService.ListCollection()
}

type fixture struct {
	t        *testing.T
	service  *apicollectionservice.APICollectionService
	backend  *failingList
	data     *store.Store
	window   *runtimetest.Window
	tester   *ui.Tester
	notifier *widgets.Notifier
	confirm  *widgets.ConfirmDialog
	view     *View
}

func newFixture(t *testing.T) *fixture {
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
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	service := apicollectionservice.New(db)
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	backend := &failingList{APICollectionService: service}
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	data := store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: settings}, Requests: requestStub{}, Saved: service, Collection: backend})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, service: service, backend: backend, data: data, window: window, notifier: &widgets.Notifier{}, confirm: &widgets.ConfirmDialog{}}
	f.view = New(data, f.notifier, f.confirm)
	f.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, data.Settings.Theme().Config)
		ui.Box(c).Width(320).Height(700).Children(func() { f.view.Sidebar(c, theme) })
		f.view.Dialogs(c, theme)
		f.notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 5})
		f.confirm.Build(c, theme)
	}, 1000, 760)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

func (f *fixture) settle() {
	for range 5 {
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

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

func (f *fixture) center(label string) (float32, float32) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	return rect.X + rect.W/2, rect.Y + rect.H/2
}

func (f *fixture) menu(row string, path ...string) {
	f.t.Helper()
	if err := menutest.Open(f.tester, f.settle, row); err != nil {
		f.t.Fatal(err)
	}
	if err := menutest.Choose(f.tester, f.settle, path...); err != nil {
		f.t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	f.settle()
}

// grip returns the center of the drag handle of the row named label, at
// depth in the tree: rows are inset 10 px and 14 px a level, and the grip
// is the first thing in them.
func (f *fixture) grip(label string, depth int) (float32, float32) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	return rect.X + 10 + float32(depth)*14 + 7, rect.Y + rect.H/2
}

func (f *fixture) drag(from string, depth int, x, y float32) {
	f.t.Helper()
	startX, startY := f.grip(from, depth)
	f.tester.Press(startX, startY)
	f.tester.Frame()
	const steps = 8
	for step := 1; step <= steps; step++ {
		fraction := float32(step) / steps
		f.tester.Move(startX+(x-startX)*fraction, startY+(y-startY)*fraction)
		f.tester.Frame()
	}
	f.tester.Release(x, y)
	f.settle()
}

func (f *fixture) folder(parent, name string) string {
	f.t.Helper()
	folder, err := f.service.CreateFolder(parent, name)
	if err != nil {
		f.t.Fatal(err)
	}
	return folder.ID
}

func (f *fixture) request(parent, name, url string) string {
	f.t.Helper()
	request, err := f.service.SaveHTTPRequest(parent, name, &apicollectionservice.SavedHTTPRequest{Method: "POST", URL: url, ProxyMode: "none"})
	if err != nil {
		f.t.Fatal(err)
	}
	return request.ID
}

func (f *fixture) reload() {
	f.data.Collection.Load(nil)
	f.settle()
}

func TestEmptyAndLoadFailure(t *testing.T) {
	f := newFixture(t)
	if !f.has(locales.ApiCollectionEmpty()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	id := f.folder("", "Partial")
	f.backend.err = errors.New("storage broke")
	f.click(locales.ApiCollectionRefresh())
	if !f.has("storage broke") {
		t.Fatalf("%v", f.tester.Texts())
	}
	if f.has("Partial") {
		t.Fatal("no partial tree while loading failed")
	}
	_ = id
	f.backend.err = nil
	f.click(locales.ApiCollectionRefresh())
	if !f.has("Partial") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestCreateRenameCopyDelete(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.click(locales.ApiCollectionCreate())
	if !f.has(locales.ApiCollectionFolderNameRequired()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.ApiCollectionFolderNamePlaceholder())
	f.tester.Type("Parent")
	f.settle()
	f.click(locales.ApiCollectionCreate())
	if !f.has("Parent") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.menu("Parent", locales.ApiCollectionNewChildFolder())
	f.click(locales.ApiCollectionFolderNamePlaceholder())
	f.tester.Type("Child")
	f.settle()
	f.click(locales.ApiCollectionCreate())
	if !f.has("Child") {
		t.Fatalf("%v", f.tester.Texts())
	}
	child := f.data.Collection.SelectedNodeID
	if f.data.Collection.ParentOf(child) == "" {
		t.Fatal("child folder sits in its parent")
	}
	f.request(child, "Req", "http://example.com/")
	f.reload()
	f.data.Collection.SelectNode(f.data.Collection.Rows()[1].ID)
	f.data.Collection.Expanded[child] = true
	f.settle()
	if !f.has("Req") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.menu("Req", locales.ApiCollectionRename())
	f.click(locales.ApiCollectionRenameNamePlaceholder())
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("Renamed")
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.has("Renamed") || f.has("Req") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.menu("Renamed", locales.ApiCollectionCopy())
	folders := f.data.Collection.Rows()
	copies := 0
	for _, row := range folders {
		if row.Name == "Renamed" || strings.HasPrefix(row.Name, "Renamed") {
			copies++
		}
	}
	if copies != 2 {
		t.Fatalf("%+v", folders)
	}
	f.menu("Parent", locales.ApiCollectionDelete())
	if !f.confirm.Open() {
		t.Fatal("deleting asks")
	}
	f.confirm.Accept()
	f.settle()
	if !f.data.Collection.Empty() || !f.has(locales.ApiCollectionEmpty()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestExpandCollapseAll(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	f.request(parent, "Inner", "http://x/")
	f.reload()
	if f.has("Inner") {
		t.Fatal("collapsed")
	}
	f.click(locales.ApiCollectionExpandAll())
	if !f.has("Inner") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.ApiCollectionCollapseAll())
	if f.has("Inner") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestMultiSelectDelete(t *testing.T) {
	f := newFixture(t)
	one := f.folder("", "One")
	f.request(one, "First", "http://a/")
	f.folder("", "Two")
	f.folder("", "Three")
	f.reload()
	f.data.Collection.Expanded[one] = true
	f.settle()
	f.click(locales.ApiCollectionMultiSelect())
	f.click(locales.ApiCollectionSelectItem("One"))
	f.click(locales.ApiCollectionSelectItem("First"))
	f.click(locales.ApiCollectionSelectItem("Two"))
	if got := f.view.deleteRoots(f.data.Collection.Rows()); len(got) != 2 {
		t.Fatalf("a checked folder absorbs its children: %v", got)
	}
	f.click(locales.ApiCollectionDelete())
	if !f.has(locales.ApiCollectionDeleteMultiConfirm("2")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Accept()
	f.settle()
	if f.has("One") || f.has("Two") || !f.has("Three") {
		t.Fatalf("%v", f.tester.Texts())
	}
	if len(f.view.checked) != 0 {
		t.Fatal(f.view.checked)
	}
}

func TestDragMovesRequestIntoFolder(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "A-Source")
	target := f.folder("", "B-Target")
	request := f.request(source, "Moved", "http://a/")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	x, y := f.center("B-Target")
	f.drag("Moved", 1, x, y)
	if got := f.data.Collection.ParentOf(request); got != target {
		t.Fatalf("parent %q", got)
	}
}

func TestDragRejectsMoveIntoDescendant(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	child := f.folder(parent, "Child")
	f.reload()
	f.data.Collection.Expanded[parent] = true
	f.settle()
	x, y := f.center("Child")
	before := f.data.Collection.Version
	f.drag("Parent", 0, x, y)
	if f.data.Collection.ParentOf(parent) != "" || f.data.Collection.ParentOf(child) != parent {
		t.Fatal("tree changed")
	}
	if f.data.Collection.Version != before {
		t.Fatal("no move attempted")
	}
	if f.data.Collection.CanMove(parent, child) || f.data.Collection.CanMove(parent, parent) {
		t.Fatal("rejected")
	}
}

func TestDragFolderToRoot(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	child := f.folder(parent, "Child")
	f.folder("", "Sibling")
	f.reload()
	f.data.Collection.Expanded[parent] = true
	f.settle()
	x, y := f.center("Parent")
	f.drag("Child", 1, x, y-14)
	if got := f.data.Collection.ParentOf(child); got != "" {
		t.Fatalf("parent %q", got)
	}
}

func TestMoveToRootFromMenu(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	child := f.folder(parent, "Child")
	f.reload()
	f.data.Collection.Expanded[parent] = true
	f.settle()
	f.menu("Child", locales.ApiCollectionMoveToRoot())
	if f.data.Collection.ParentOf(child) != "" {
		t.Fatal("moved")
	}
}

func TestHoveringFolderOpensIt(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "A-Source")
	target := f.folder("", "B-Target")
	f.request(target, "Inside", "http://b/")
	request := f.request(source, "Moved", "http://a/")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	startX, startY := f.grip("Moved", 1)
	x, y := f.center("B-Target")
	f.tester.Press(startX, startY)
	f.tester.Frame()
	f.tester.Move(startX, startY+8)
	f.tester.Frame()
	f.tester.Move(x, y)
	f.tester.Frame()
	f.tester.Advance(dragExpandDelay + 1)
	f.tester.Frame()
	f.tester.Frame()
	if !f.has("Inside") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.tester.Release(x, y)
	f.settle()
	if f.data.Collection.ParentOf(request) != target {
		t.Fatal(f.data.Collection.ParentOf(request))
	}
}

func TestSavingNeedsAFolder(t *testing.T) {
	f := newFixture(t)
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	f.view.SaveTab(key)
	f.settle()
	if !f.has(locales.ApiCollectionCreateFolderFirst()) || f.view.save.open {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestSavingANewTabLinksIt(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.reload()
	f.data.Collection.SelectNode(folder)
	workspace := f.data.Workspace
	key := workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	tab := workspace.FindTab(key)
	tab.HTTP.URL, tab.Title = "http://example.com/users", "example.com/users"
	f.view.SaveTab(key)
	f.settle()
	if !f.view.save.open || f.view.save.name != "example.com/users" || f.view.save.parent != folder {
		t.Fatalf("%+v", f.view.save)
	}
	f.click(locales.ApiCollectionSave())
	if tab.APIID == "" || workspace.IsDirty(key) || f.view.save.open {
		t.Fatalf("%+v", tab)
	}
	if !f.has("example.com/users") {
		t.Fatalf("%v", f.tester.Texts())
	}
	if f.data.Collection.ParentOf(tab.APIID) != folder {
		t.Fatal(f.data.Collection.ParentOf(tab.APIID))
	}
	if f.data.Collection.SelectedNodeID != tab.APIID {
		t.Fatal("selected", f.data.Collection.SelectedNodeID)
	}
}

func TestSavingALinkedTabUpdatesWithoutDialog(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	id := f.request(folder, "Linked", "http://example.com/")
	f.reload()
	f.data.Collection.Expanded[folder] = true
	f.settle()
	f.click("Linked")
	workspace := f.data.Workspace
	tab := workspace.ActiveTab()
	if tab.APIID != id || tab.Title != "Linked" {
		t.Fatalf("%+v", tab)
	}
	tab.HTTP.URL = "http://example.com/changed"
	if !workspace.IsDirty(tab.Key) {
		t.Fatal("dirty")
	}
	f.view.SaveTab(tab.Key)
	f.settle()
	if f.view.save.open || workspace.IsDirty(tab.Key) {
		t.Fatal("linked save needs no dialog")
	}
	stored, err := f.service.GetRequest(id)
	if err != nil || stored.HTTP.URL != "http://example.com/changed" {
		t.Fatal(err, stored)
	}
	f.click("Linked")
	if len(workspace.Tabs) != 2 || workspace.ActiveKey != tab.Key {
		t.Fatalf("existing tab reused: %d", len(workspace.Tabs))
	}
}

func TestDeletingRequestClosesItsTab(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	id := f.request(folder, "Open Me", "http://example.com/")
	f.reload()
	f.data.Collection.Expanded[folder] = true
	f.settle()
	f.click("Open Me")
	if f.data.Workspace.FindTabByAPIID(id) == nil {
		t.Fatal("opened")
	}
	f.menu("Folder", locales.ApiCollectionDelete())
	f.confirm.Accept()
	f.settle()
	if f.data.Workspace.FindTabByAPIID(id) != nil {
		t.Fatal("tab of a deleted request stays open")
	}
}

func TestRoundTripKeepsEverySavedField(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.reload()
	workspace := f.data.Workspace
	state := logic.BuildEmptyHTTPEditorState(logic.DraftNew)
	state.Method, state.URL = "PUT", "https://example.com/a?x=1"
	state.Params = []logic.EditableRow{{Key: "x", Value: "1", Enabled: true}, {Key: "off", Value: "2"}}
	state.Headers = []logic.EditableRow{{Key: "X-B", Value: "1", Enabled: true}, {Key: "X-A", Value: "2", Enabled: true}, {Key: "X-Off", Value: "3"}}
	state.RequestBodyType, state.RequestBodyText = logic.BodyTypeJSON, `{"k":"v"}`
	state.RequestBodyURLEncoded = []logic.EditableRow{{Key: "u", Value: "e", Enabled: true}}
	state.RequestBodyFormData = []logic.FormDataItem{{ID: "f1", Enabled: true, Name: "field", ItemType: logic.FormDataItemText, Value: "val"}}
	state.InlineScriptEnabled, state.PluginsEnabled, state.InlineScriptSource = true, true, "print('saved')"
	state.Settings = logic.HTTPSendSettings{ProxyMode: logic.ProxyModeCustom, Protocol: string(logic.ProtocolHTTP2), CustomProxy: "http://127.0.0.1:9", TimeoutMs: 2500, SkipVerifyTLS: true, TLSClientHelloID: "chrome_auto", HTTP2Fingerprint: "1:1|2|3|m,a,s,p", MaxRedirects: 7}
	key := workspace.CreateHTTPRequestTab(logic.DraftNew, state)
	var saved *store.CollectionRequest
	workspace.SaveTabAsAPI(key, folder, "Everything", func(request *store.CollectionRequest, err error) {
		if err != nil {
			t.Error(err)
		}
		saved = request
	})
	f.settle()
	workspace.CloseTab(key)
	stored, err := f.service.GetRequest(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	reopened := logic.BuildHTTPEditorStateFromSaved(stored.HTTP, stored.Name)
	if reopened.Method != "PUT" || reopened.URL != state.URL || reopened.RequestBodyType != logic.BodyTypeJSON || reopened.RequestBodyText != `{"k":"v"}` {
		t.Fatalf("%+v", reopened)
	}
	if reopened.Settings != state.Settings {
		t.Fatalf("%+v vs %+v", reopened.Settings, state.Settings)
	}
	if reopened.InlineScriptSource != "print('saved')" || reopened.InlineScriptEnabled || reopened.PluginsEnabled {
		t.Fatalf("%+v", reopened)
	}
	if len(reopened.Headers) != 3 || reopened.Headers[0].Key != "X-B" || reopened.Headers[1].Key != "X-A" || reopened.Headers[2].Enabled {
		t.Fatalf("%+v", reopened.Headers)
	}
	if len(reopened.Params) != 2 || reopened.Params[1].Key != "off" || reopened.Params[1].Enabled {
		t.Fatalf("%+v", reopened.Params)
	}
	if len(reopened.RequestBodyURLEncoded) == 0 || reopened.RequestBodyURLEncoded[0].Key != "u" || reopened.RequestBodyFormData[0].Name != "field" {
		t.Fatalf("%+v %+v", reopened.RequestBodyURLEncoded, reopened.RequestBodyFormData)
	}
	socket := logic.BuildEmptyWebSocketClientState(logic.DraftNew)
	socket.URL, socket.DraftType, socket.DraftText = "wss://example.com/s", "json", `{"m":1}`
	socket.Headers = []logic.EditableRow{{Key: "X-W", Value: "1", Enabled: true}}
	socket.Settings = logic.WebSocketSettings{ProxyMode: logic.ProxyModeSystem, TimeoutMs: 900, SkipVerifyTLS: true, TLSClientHelloID: "firefox_auto"}
	socketKey := workspace.CreateWebSocketClientTab(logic.DraftNew, socket)
	workspace.SaveTabAsAPI(socketKey, folder, "Socket", func(request *store.CollectionRequest, err error) {
		if err != nil {
			t.Error(err)
		}
		saved = request
	})
	f.settle()
	storedSocket, err := f.service.GetRequest(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	reopenedSocket := logic.BuildWebSocketClientStateFromSaved(storedSocket.WebSocket, storedSocket.Name)
	if reopenedSocket.URL != socket.URL || reopenedSocket.DraftText != socket.DraftText || reopenedSocket.DraftType != "json" || reopenedSocket.Settings != socket.Settings || reopenedSocket.Headers[0].Key != "X-W" {
		t.Fatalf("%+v", reopenedSocket)
	}
}

func TestOlderSavedRequestUsesDefaults(t *testing.T) {
	state := logic.BuildHTTPEditorStateFromSaved(&apicollectionservice.SavedHTTPRequest{Method: "GET", URL: "http://x/"}, "old")
	if state.Settings.Protocol != string(logic.ProtocolAuto) || state.Settings.MaxRedirects != 0 || state.Settings.SkipVerifyTLS || state.Settings.TLSClientHelloID != "golang" || state.Settings.ProxyMode != logic.ProxyModeNone {
		t.Fatalf("%+v", state.Settings)
	}
	if state.InlineScriptSource != logic.DefaultHTTPRequestPythonScript {
		t.Fatalf("%q", state.InlineScriptSource)
	}
}

// Opening a row's menu selects the row, and a folder's menu lists the
// folder's actions.
func TestRowMenuSelectsItsRow(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	child := f.folder(parent, "Child")
	f.reload()
	f.data.Collection.Expanded[parent] = true
	f.settle()
	if err := menutest.Open(f.tester, f.settle, "Child"); err != nil {
		t.Fatal(err)
	}
	if f.data.Collection.SelectedNodeID != child {
		t.Fatalf("selected %q, want %q", f.data.Collection.SelectedNodeID, child)
	}
	for _, label := range []string{locales.ApiCollectionNewChildFolder(), locales.ApiCollectionRename(), locales.ApiCollectionMoveToRoot(), locales.ApiCollectionDelete()} {
		if _, ok := menutest.Item(f.tester, label); !ok {
			t.Fatalf("no %q in %v", label, f.tester.Texts())
		}
	}
	if _, ok := menutest.Item(f.tester, locales.ApiCollectionCopy()); ok {
		t.Fatal("a folder offers Copy")
	}
	menutest.Close(f.tester, f.settle)
	if menutest.Shown(f.tester) {
		t.Fatal("Escape leaves the menu open")
	}
}
