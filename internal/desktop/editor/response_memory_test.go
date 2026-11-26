package editor

import (
	"runtime"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestNewResponseReleasesHiddenBodyViewer(t *testing.T) {
	f := newFake(t)
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := f.data.Workspace.FindTab(key).HTTP
	response := func(body string) *logic.HTTPResponseView {
		return &logic.HTTPResponseView{StatusCode: 200, Body: body, Headers: []logic.EditableRow{{Key: "Content-Type", Value: "text/plain", Enabled: true}}}
	}
	state.ActiveResponseTab = logic.ResponseTabBody
	state.Response = response("seed")
	f.settle()
	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	before := heap()
	state.Response = response(strings.Repeat("large response line\n", 300000))
	f.settle()
	loaded := heap()
	state.ActiveResponseTab = logic.ResponseTabHeaders
	state.Response = response("tiny")
	f.settle()
	after := heap()
	t.Logf("rendered body delta %.2f MiB; new response with Headers visible %.2f MiB", float64(int64(loaded)-int64(before))/(1<<20), float64(int64(after)-int64(before))/(1<<20))
	if int64(after)-int64(before) > 4<<20 {
		t.Fatal("hidden response viewer retained old body")
	}
	state.ActiveResponseTab = logic.ResponseTabBody
	f.settle()
	runtime.KeepAlive(f)
}

func TestBackgroundResponseResetsViewerWithoutAnotherEditorFrame(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	tab.HTTP.URL = "https://response.test/"
	tab.HTTP.ActiveResponseTab = logic.ResponseTabBody
	tab.HTTP.Response = &logic.HTTPResponseView{StatusCode: 200, Body: "old response"}
	f.settle()
	view := f.editor.https[tab.Key].response
	before := view.identity
	f.data.Workspace.ActivateCapture()
	f.requests.mu.Lock()
	f.requests.response = proxyservice.SendRequestResponse{StatusCode: 200, Body: "new response"}
	f.requests.mu.Unlock()
	if err := f.data.Workspace.SendTab(tab.Key, false); err != "" {
		t.Fatal(err)
	}
	f.data.Runtime.Jobs.Wait()
	f.window.Drain()
	if view.identity == before || view.seen != tab.HTTP.Response || view.seen.Body != "new response" {
		t.Fatal("hidden tab required another frame to invalidate its old response")
	}
}
