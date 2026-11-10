package inspect

import (
	"testing"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/theme"
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
