package inspect

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func TestProcessPathCopyShowsTooltip(t *testing.T) {
	entry := httpEntry()
	entry.Metadata = &proxyservice.Metadata{Process: &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, DisplayName: "Safari", ExecutablePath: "/usr/bin/safari"}}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.click(locales.DetailProcess())
	rendertest.RequireTooltip(t, f.tester, locales.DetailCopyProcessPath(), locales.DetailCopyProcessPath())
}

func TestWebSocketDirectionFilterShowsTooltip(t *testing.T) {
	entry := httpEntry()
	entry.Type = "ws"
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{{Direction: "send", MsgType: "text", Data: "outgoing", DataSize: 8}}})
	rendertest.RequireTooltip(t, f.tester, locales.WorkspaceWebsocketClientFilterAll(), locales.WorkspaceWebsocketClientFilterAll())
}

func TestSSEJumpToLatestShowsTooltip(t *testing.T) {
	f := sseFixture(t, "data: first\n\ndata: second\n\n")
	rendertest.RequireTooltip(t, f.tester, locales.DetailSseJumpToLatest(), locales.DetailSseJumpToLatest())
}
