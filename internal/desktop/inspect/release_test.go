package inspect

import (
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestHistoryDetailReleaseDropsRenderedCachesAndReopens(t *testing.T) {
	entry := &logic.TrafficEntry{ID: 1, Type: "http", URL: "http://saved.test/", Response: &proxyservice.HTTPMessage{HeaderFields: []proxyservice.HTTPHeaderField{{Name: "Content-Type", Value: "text/plain"}}}}
	body := &proxyservice.TrafficBodyView{ResponseBody: strings.Repeat("saved body\n", 100000)}
	f := newFixture(t, entry, body)
	history := store.NewHistoryStore(f.rt, nil)
	f.panel.data = store.HistoryDetailData{History: history}
	open := func() {
		history.Traffic.Load("saved-session", []*logic.TrafficEntry{entry})
		history.SelectedKey = "saved-session"
		history.Traffic.Selected, history.Traffic.BodyView, history.Traffic.ShowDetailPanel = entry, body, true
		f.panel.responseTab = tabResponseBody
		f.tester.Frame()
	}
	open()
	viewer := f.panel.responseBody
	viewer.hex.load(entry.ID, body.ResponseBody, false)
	viewer.sse.update(BodyContent{Identity: entry.ID, Body: "data: saved event\n\n"})
	f.panel.requestRaw.set("GET /saved HTTP/1.1")
	f.panel.responseRaw.set(body.ResponseBody)
	f.panel.messages.SetMessages([]logic.WebSocketDisplayMessage{{Data: body.ResponseBody}})
	if viewer.raw.Text() != body.ResponseBody {
		t.Fatal("body was not rendered")
	}
	history.CloseOpened()
	f.panel.Release()
	for range 3 {
		f.tester.Frame()
	}
	for _, v := range []*BodyViewer{f.panel.requestBody, f.panel.responseBody} {
		if v.content.Body != "" || v.bodyText != "" || v.raw.Text() != "" || v.formatted.Text() != "" || v.hex.source != nil || v.sse.body != "" || len(v.sse.events) != 0 || v.image.body != "" || v.image.image.bitmap != nil || v.image.image.svg != nil {
			t.Fatal("closed history retained viewer data")
		}
	}
	if f.panel.requestRaw.text != "" || f.panel.responseRaw.text != "" || len(f.panel.messages.source) != 0 || len(f.panel.messages.filtered) != 0 {
		t.Fatal("closed history retained Raw or WebSocket data")
	}
	open()
	if f.panel.responseBody.raw.Text() != body.ResponseBody {
		t.Fatal("reopened body differs")
	}
}

func TestPanelReleaseDiscardsPendingViewerResults(t *testing.T) {
	f := newFixture(t, &logic.TrafficEntry{ID: 1, Type: "http"}, nil)
	viewer := f.panel.responseBody
	viewer.set(BodyContent{Identity: 1, ContentType: "image/png", Body: "invalid image"})
	viewer.image.ensure(viewer)
	viewer.sse.update(BodyContent{Identity: 1, Body: strings.Repeat("data: pending\n\n", 100000)})
	f.rt.Jobs.Wait() // Results are posted, but have not reached the main thread.
	f.panel.Release()
	f.window.Drain()
	if viewer.image.failed || viewer.image.image.bitmap != nil || len(viewer.sse.events) != 0 {
		t.Fatal("a canceled result repopulated a released viewer")
	}
	if f.panel.responseBody.content.Body != "" || len(f.panel.responseBody.sse.events) != 0 {
		t.Fatal("pending work repopulated the replacement panel")
	}
}
