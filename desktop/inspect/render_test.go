package inspect

import (
	"encoding/base64"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

// renderEntry is the reference screenshot's inspected request, with
// headers, a query, cookies, and TLS details.
func renderEntry() *logic.TrafficEntry {
	entry := httpEntry()
	entry.URL = "https://www.bilibili.com/?spm_id_from=333.1007&vd_source=abc"
	entry.Request.HeaderFields = []logic.HeaderField{
		{Name: ":authority", Value: "www.bilibili.com"},
		{Name: "sec-ch-ua", Value: `"Chromium";v="154", "Microsoft Edge";v="154", "Not A(Brand";v="99"`},
		{Name: "user-agent", Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36 Edg/154.0.0.0"},
		{Name: "accept", Value: "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		{Name: "cookie", Value: "buvid3=ABC; b_nut=1700000000; _uuid=XYZ"},
	}
	entry.Response.HeaderFields = []logic.HeaderField{
		{Name: "content-type", Value: "text/html; charset=utf-8"},
		{Name: "set-cookie", Value: "buvid3=ABC; Path=/; Domain=.bilibili.com; Expires=Wed, 01 Jan 2031 00:00:00 GMT; Secure; HttpOnly"},
		{Name: "set-cookie", Value: "b_nut=1700000000; Domain=; SameSite=Lax"},
	}
	entry.Metadata = &proxyservice.Metadata{TLS: &proxyservice.TLSState{
		ServerName: "www.bilibili.com", SelectedVersion: "TLS1.3", SelectedALPN: "h2", SelectedCipherSuite: "TLS_AES_128_GCM_SHA256",
		SupportedVersion: []string{"TLS1.3", "TLS1.2"}, SupportedALPN: []string{"h2", "http/1.1"},
		SupportedCipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"},
	}}
	return entry
}

func (f *fixture) setDark(dark bool) {
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := f.settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
}

func renderFixture(t *testing.T) *fixture {
	f := newFixture(t, renderEntry(), &proxyservice.TrafficBodyView{})
	f.tester.SetSize(rendertest.Width, 520)
	f.settle()
	return f
}

func TestRenderDetailHeaders(t *testing.T) {
	f := renderFixture(t)
	f.panel.requestTab, f.panel.responseTab = tabRequestHeaders, tabResponseCookies
	f.settle()
	rendertest.ShootThemes(t, f.tester, "detail-headers", f.setDark)
}

func TestRenderWebSocketMessageDetails(t *testing.T) {
	entry := httpEntry()
	entry.Type = "ws"
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{
		{Direction: "receive", MsgType: "text", Data: `{"type":"hello","payload":"a received text message"}`, DataSize: 53},
		{Direction: "send", MsgType: "binary", Data: base64.StdEncoding.EncodeToString([]byte("\x00\x01binary frame\xff")), DataSize: 16},
	}})
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	f.settle()
	f.openMessage(`{"type":"hello","payload":"a received text message"}`)
	rendertest.ShootThemes(t, f.tester, "ws-detail-receive-text", f.setDark)
	f.panel.messages.open = false
	f.settle()
	f.openMessage(locales.WorkspaceWebsocketClientBinarySize("16"))
	rendertest.ShootThemes(t, f.tester, "ws-detail-send-binary", f.setDark)
}

func TestRenderSSEMessageDetail(t *testing.T) {
	f := sseFixture(t, "id: 41\nevent: update\ndata: {\"status\":\"ok\",\"items\":[1,2,3],\"message\":\"an SSE message with a JSON payload\"}\n\n")
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	f.settle()
	f.click(locales.DetailSseOpenMessage("1"))
	f.tester.Advance(time.Second)
	f.settle()
	f.click(locales.DetailFormatted())
	rendertest.ShootThemes(t, f.tester, "sse-detail-formatted", f.setDark)
}

func TestRenderWebSocketMessageList(t *testing.T) {
	entry := httpEntry()
	entry.Type = "ws"
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{
		{Direction: "send", MsgType: "text", Data: `{"type":"subscribe","channel":"prices"}`, DataSize: 39},
		{Direction: "receive", MsgType: "text", Data: `{"type":"ack","channel":"prices"}`, DataSize: 33},
		{Direction: "receive", MsgType: "binary", Data: base64.StdEncoding.EncodeToString([]byte("\x00\x01binary frame\xff")), DataSize: 16},
	}})
	f.tester.SetSize(rendertest.Width, 520)
	f.settle()
	rendertest.ShootThemes(t, f.tester, "ws-list", f.setDark)
}

func TestRenderSSEMessageList(t *testing.T) {
	f := sseFixture(t, "event: open\ndata: {\"session\":\"a1\"}\n\nid: 41\nevent: update\ndata: {\"status\":\"ok\",\"items\":[1,2,3]}\n\nid: 42\nevent: update\ndata:\n\ndata: tail")
	f.tester.SetSize(rendertest.Width, 520)
	f.settle()
	rendertest.ShootThemes(t, f.tester, "sse-list", f.setDark)
}
