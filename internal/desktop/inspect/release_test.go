package inspect

import (
	"runtime"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func assertPanelPayloadsReleased(t *testing.T, panel *Panel) {
	t.Helper()
	for _, v := range []*BodyViewer{panel.requestBody, panel.responseBody} {
		if v.content.Body != "" || v.bodyText != "" || v.raw.Text() != "" || v.formatted.Text() != "" || v.hex.source != nil || v.sse.body != "" || len(v.sse.events) != 0 || v.image.body != "" || v.image.image.bitmap != nil || v.image.image.svg != nil {
			t.Fatal("discarded detail retained viewer payloads")
		}
	}
	if panel.requestRaw.text != "" || panel.responseRaw.text != "" || len(panel.messages.source) != 0 || len(panel.messages.filtered) != 0 {
		t.Fatal("discarded detail retained Raw or WebSocket payloads")
	}
}

func TestLiveDetailInvalidationReleasesPayloadsWithoutRendering(t *testing.T) {
	for _, action := range []string{"reset", "deselect", "close", "replace"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, nil, nil)
			traffic := store.NewTrafficStore(f.rt, nil, nil, nil)
			traffic.SetSurfaceActive(false)
			f.panel.data = LiveData{Traffic: traffic}
			traffic.OnDetailReset = f.panel.Release
			entry := &logic.TrafficEntry{ID: 1, Type: "http"}
			traffic.Select(entry)
			traffic.SetShowDetailPanel(true)
			body := strings.Repeat("body line\n", 100000)
			traffic.BodyView = &proxyservice.TrafficBodyView{ResponseBody: body}
			f.panel.responseBody.set(BodyContent{Identity: 1, ContentType: "text/plain", Body: body})
			f.panel.responseBody.hex.load(1, body, false)
			f.panel.responseBody.sse.update(BodyContent{Identity: 1, Body: "data: retained\n\n"})
			f.panel.responseRaw.set(body)
			f.panel.messages.SetMessages([]logic.WebSocketDisplayMessage{{Data: body}})
			f.panel.responseTab = tabResponseBody
			f.panel.responseBody.active, f.panel.responseBody.tabChoice = logic.BodyTabHex, string(logic.BodyTabHex)
			f.panel.responseBody.raw.SetWordWrap(true)
			switch action {
			case "reset":
				traffic.ResetState()
			case "deselect":
				traffic.Select(nil)
			case "close":
				traffic.SetShowDetailPanel(false)
				if traffic.Selected != entry {
					t.Fatal("closing details cleared selection")
				}
			case "replace":
				traffic.Select(&logic.TrafficEntry{ID: 2, Type: "http"})
			}
			assertPanelPayloadsReleased(t, f.panel)
			if f.panel.responseTab != tabResponseBody || f.panel.responseBody.active != logic.BodyTabHex || !f.panel.responseBody.raw.WordWrap() {
				t.Fatal("release discarded view preferences")
			}
			traffic.Selected, traffic.ShowDetailPanel = entry, true
			traffic.BodyView = &proxyservice.TrafficBodyView{ResponseBody: "reopened body"}
			f.settle()
			if f.panel.responseBody.content.Body != "reopened body" {
				t.Fatal("details did not reopen")
			}
		})
	}
}

func TestDetailResetReleasesLargeBodyHeap(t *testing.T) {
	f := newFixture(t, nil, nil)
	traffic := store.NewTrafficStore(f.rt, nil, nil, nil)
	f.panel.data = LiveData{Traffic: traffic}
	traffic.OnDetailReset = f.panel.Release
	heap := func() uint64 {
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		return stats.HeapAlloc
	}
	baseline := heap()
	func() {
		traffic.Select(&logic.TrafficEntry{ID: 1, Type: "http"})
		body := strings.Repeat("a sample response line for memory attribution\n", 180000)
		traffic.BodyView = &proxyservice.TrafficBodyView{ResponseBody: body}
		f.panel.responseBody.set(BodyContent{Identity: 1, ContentType: "text/plain", Body: body})
		f.panel.responseRaw.set(body)
	}()
	loaded := heap()
	traffic.ResetState()
	assertPanelPayloadsReleased(t, f.panel)
	released := heap()
	t.Logf("HeapAlloc baseline %.2f MiB, loaded %.2f MiB, after reset %.2f MiB", float64(baseline)/(1<<20), float64(loaded)/(1<<20), float64(released)/(1<<20))
	// Leave room for runtime/test noise, but not another complete body or
	// any of its three editor buffers surviving the reset.
	if retained := int64(released) - int64(baseline); retained > 8<<20 {
		t.Fatalf("reset retained %.2f MiB above baseline", float64(retained)/(1<<20))
	}
	runtime.KeepAlive(f)
}

func TestHistoryDetailInvalidationReleasesWithoutRendering(t *testing.T) {
	for _, action := range []string{"close", "deselect", "delete", "reset", "replace"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, nil, nil)
			history := store.NewHistoryStore(f.rt, nil)
			f.panel.data = store.HistoryDetailData{History: history}
			history.Traffic.OnDetailReset = f.panel.Release
			entry := &logic.TrafficEntry{ID: 1, Type: "http"}
			history.Traffic.Load("saved", []*logic.TrafficEntry{entry})
			history.Traffic.Selected = entry
			f.panel.responseBody.set(BodyContent{Identity: 1, Body: "previous body"})
			f.panel.responseRaw.set("previous raw")
			switch action {
			case "close":
				history.Traffic.SetShowDetailPanel(false)
			case "deselect":
				history.Traffic.Select(nil)
			case "delete":
				history.Traffic.Delete([]uint64{1})
			case "reset":
				history.CloseOpened()
			case "replace":
				history.Traffic.Select(&logic.TrafficEntry{ID: 2, Type: "http"})
			}
			assertPanelPayloadsReleased(t, f.panel)
		})
	}
}

func TestBodyReplacementDropsHiddenDerivedViews(t *testing.T) {
	f := newFixture(t, nil, nil)
	v := f.panel.responseBody
	v.set(BodyContent{Identity: 1, ContentType: "image/png", Body: "old image"})
	v.hex.load(1, "old image", false)
	v.image.ensure(v)
	v.sse.update(BodyContent{Identity: 1, Body: strings.Repeat("data: pending\n\n", 100000)})
	f.rt.Jobs.Wait()
	v.set(BodyContent{Identity: 1, ContentType: "text/plain", Body: "replacement"})
	f.window.Drain()
	if v.hex.source != nil || v.image.body != "" || v.image.failed || v.sse.body != "" || len(v.sse.events) != 0 {
		t.Fatal("replacement retained a hidden view or accepted an obsolete result")
	}
	if v.raw.Text() != "replacement" {
		t.Fatal("replacement body missing")
	}
}

func TestClosingMessageDetailsDropsPayloadCaches(t *testing.T) {
	f := newFixture(t, nil, nil)
	sse := f.panel.responseBody.sse
	sse.formatted = "formatted message"
	sse.detailView.SetText("large message")
	sse.closeDetail()
	if sse.formatted != "" || sse.detailView.Text() != "" {
		t.Fatal("SSE dialog kept payload")
	}
	ws := f.panel.messages
	message := logic.WebSocketDisplayMessage{ID: "1", Data: "message", MsgType: "text"}
	ws.SetMessages([]logic.WebSocketDisplayMessage{message})
	ws.open_(message)
	ws.view.SetText(message.Data)
	ws.hex.load(1, message.Data, false)
	ws.closeDetail()
	if ws.active != nil || ws.view.Text() != "" || ws.hex.source != nil || len(ws.source) != 1 {
		t.Fatal("WebSocket close kept dialog data or removed list")
	}
	ws.open_(message)
	ws.view.SetText(message.Data)
	ws.SetMessages(nil)
	if ws.open || ws.active != nil || ws.view.Text() != "" {
		t.Fatal("cleared messages kept an open detail")
	}
}

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
