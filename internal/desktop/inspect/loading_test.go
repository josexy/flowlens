package inspect

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestLoadingHistoryDetailRemainsClosable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFixture(t, nil, nil)
		f.setDark(dark)
		f.data.loading = true
		closed := false
		f.panel.OnClose = func() { closed = true }
		f.settle()
		f.click(locales.DetailClosePanel())
		if !closed {
			t.Fatal("loading detail lost its close action")
		}
	}
}

func TestRenderHistoryLoadingTransitions(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			for _, layout := range []settingservice.TrafficDetailLayout{settingservice.TrafficDetailLayoutHorizontal, settingservice.TrafficDetailLayoutVertical} {
				name := fmt.Sprintf("history-loading-%s-%t-%s", language, dark, layout)
				t.Run(name, func(t *testing.T) {
					f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{ResponseBody: "old body"})
					i18n.SetLocaleFunc(func() string { return language })
					if f.settings.TrafficDetailLayout() != layout {
						f.settings.ToggleTrafficDetailLayout(nil)
					}
					f.tester = ui.NewTester(func(c *ui.Context) {
						palette := nui.App(c, f.settings.Theme().Config)
						f.panel.Build(c, palette)
					}, 1200, 640)
					f.tester.SetLocale(language)
					f.setDark(dark)
					rendertest.SettleTheme(f.tester)
					closed := false
					f.panel.OnClose = func() { closed = true }
					f.tester.Frame()
					before, ok := f.tester.AccessByRole(ui.RoleSplitter, "")
					if !ok {
						t.Fatal("detail splitter missing")
					}
					f.panel.responseRaw.set("obsolete raw payload")
					f.panel.Release()
					f.data.entry, f.data.body = nil, nil
					f.data.pending = &logic.TrafficEntry{ID: 200, Type: "https"}
					f.tester.Frame()
					assertPanelPayloadsReleased(t, f.panel)
					after, ok := f.tester.AccessByRole(ui.RoleSplitter, "")
					if !ok || after.Bounds != before.Bounds {
						t.Fatalf("pending splitter changed: before=%v after=%v", before.Bounds, after.Bounds)
					}
					for _, label := range []string{locales.DetailOverview(), locales.DetailRequestHeaders(), locales.DetailResponseHeaders()} {
						info, ok := f.tester.AccessByRole(ui.RoleTab, label)
						if !ok || !info.Disabled {
							t.Fatalf("pending tab %s is missing or enabled: %+v", label, info)
						}
					}
					if info, ok := f.tester.Access(locales.DetailClosePanel()); !ok || info.Disabled {
						t.Fatal("pending close is unavailable")
					}
					f.tester.Advance(loadingIndicatorDelay - time.Millisecond)
					assertLoadingIndicator(t, f, false)
					rendertest.Shoot(t, f.tester, name+"-fast")
					f.tester.Advance(time.Millisecond)
					assertLoadingIndicator(t, f, true)
					rendertest.Shoot(t, f.tester, name+"-slow")
					// A different pending row receives a fresh deadline even if no
					// complete entry was shown between the two selections.
					f.data.pending = &logic.TrafficEntry{ID: 201, Type: "https"}
					f.tester.Frame()
					assertLoadingIndicator(t, f, false)
					f.data.pending = nil
					f.data.entry = httpEntry()
					f.data.entry.ID = 201
					f.tester.Frame()
					assertLoadingIndicator(t, f, false)
					f.has(locales.DetailOverview())
					rendertest.Shoot(t, f.tester, name+"-ready")
					// A queued redraw after completion must not reintroduce loading.
					f.tester.Advance(loadingIndicatorDelay)
					assertLoadingIndicator(t, f, false)
					f.panel.Release()
					f.data.entry = nil
					f.data.pending = &logic.TrafficEntry{ID: 202, Type: "https"}
					f.tester.Frame()
					f.click(locales.DetailClosePanel())
					if !closed {
						t.Fatal("pending close did not run")
					}
					f.panel.Release()
					f.data.pending = nil
					f.tester.Advance(loadingIndicatorDelay)
					f.tester.Frame()
					assertLoadingIndicator(t, f, false)
				})
			}
		}
	}
}

func TestDeferredBodiesLoadOnlyForDependentPanes(t *testing.T) {
	for _, tab := range []string{tabResponseHeaders, tabResponseRaw, tabResponseBody, tabWebSocket, tabRequestRaw, tabRequestBody} {
		t.Run(tab, func(t *testing.T) {
			entry := httpEntry()
			if tab == tabWebSocket {
				entry.Type = "wss"
			}
			f := newFixture(t, entry, nil)
			requested := false
			f.data.request = func() {
				if !requested {
					requested, f.data.loading = true, true
				}
			}
			if tab == tabRequestRaw || tab == tabRequestBody {
				f.panel.requestTab = tab
			} else {
				f.panel.responseTab = tab
			}
			f.tester.Frame()
			if requested != (tab != tabResponseHeaders) {
				t.Fatalf("unexpected demand from %s", tab)
			}
			if !requested {
				return
			}
			assertLoadingIndicator(t, f, false)
			if tab == tabResponseRaw || tab == tabRequestRaw {
				if info, ok := f.tester.Access(locales.DetailCopyRawHttp()); !ok || !info.Disabled {
					t.Fatal("incomplete Raw copy is enabled")
				}
			}
			f.tester.Advance(loadingIndicatorDelay)
			assertLoadingIndicator(t, f, true)
			f.data.loading = false
			f.data.body = &proxyservice.TrafficBodyView{RequestBody: "request complete", ResponseBody: "response complete"}
			f.data.version++
			f.tester.Frame()
			assertLoadingIndicator(t, f, false)
			if tab == tabResponseRaw || tab == tabRequestRaw {
				if info, ok := f.tester.Access(locales.DetailCopyRawHttp()); !ok || info.Disabled {
					t.Fatal("complete Raw copy remains disabled")
				}
			}
		})
	}
}

func assertLoadingIndicator(t *testing.T, f *fixture, want bool) {
	t.Helper()
	_, got := f.tester.AccessByRole(ui.RoleStatus, locales.AppLoading())
	if got != want {
		t.Fatalf("loading indicator=%t, want %t", got, want)
	}
}

func TestDeferredBodyCompletesBeforeLoadingIndicator(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	f.panel.responseTab = tabResponseBody
	f.data.loading = true
	f.tester.Frame()
	f.tester.Advance(70 * time.Millisecond)
	assertLoadingIndicator(t, f, false)
	f.data.body = &proxyservice.TrafficBodyView{ResponseBody: "ready"}
	f.data.loading = false
	f.tester.Frame()
	if f.panel.responseBody.content.Body != "ready" {
		t.Fatal("ready content was delayed by the spinner deadline")
	}
	f.tester.Advance(loadingIndicatorDelay)
	assertLoadingIndicator(t, f, false)
}
