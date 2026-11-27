package inspect

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
)

// beginRetainedSwitch mirrors HistoryTraffic.Select: it retains the shown
// view, including whether its body was loading, and cancels that read.
func beginRetainedSwitch(f *fixture, next *logic.TrafficEntry) {
	f.data.retained = &store.HistoryDetailSnapshot{Entry: f.data.entry, Body: f.data.body, Version: f.data.version, Loading: f.data.loading}
	f.data.release = f.panel.Release
	f.data.entry, f.data.body, f.data.pending, f.data.loading = nil, nil, next, false
	f.data.version++
	f.tester.Frame()
}

func TestRenderHistoryRetainedTransition(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			for _, layout := range []settingservice.TrafficDetailLayout{settingservice.TrafficDetailLayoutHorizontal, settingservice.TrafficDetailLayoutVertical} {
				name := fmt.Sprintf("history-retained-%s-%t-%s", language, dark, layout)
				t.Run(name, func(t *testing.T) {
					f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{ResponseBody: "old body"})
					i18n.SetLocaleFunc(func() string { return language })
					f.tester.SetLocale(language)
					f.tester.SetSize(1200, 640)
					if f.settings.TrafficDetailLayout() != layout {
						f.settings.ToggleTrafficDetailLayout(nil)
					}
					f.setDark(dark)
					closed := false
					f.panel.OnClose = func() { closed = true }
					rendertest.SettleTheme(f.tester)
					before := f.tester.Image()
					texts := f.tester.Texts()
					next := httpEntry()
					next.ID, next.StatusCode, next.Status = 200, 201, "201 Created"
					beginRetainedSwitch(f, next)
					if !slices.Equal(texts, f.tester.Texts()) {
						t.Fatal("pending selection replaced content, counts, or response chips")
					}
					if !bytes.Equal(before.Pix, f.tester.Image().Pix) {
						t.Fatal("pending selection changed the retained view's appearance")
					}
					assertLoadingIndicator(t, f, false)
					for _, label := range []string{locales.DetailOverview(), countLabel(locales.DetailResponseHeaders(), len(f.data.retained.Entry.Response.HeaderFields)), locales.DetailCopyResponseHeadersText()} {
						info, ok := f.tester.Access(label)
						if !ok || !info.Disabled {
							t.Fatalf("retained control %q is actionable: %+v", label, info)
						}
					}
					if info, ok := f.tester.Access(locales.DetailClosePanel()); !ok || info.Disabled {
						t.Fatal("retained view disabled close")
					}
					rendertest.Shoot(t, f.tester, name+"-pending")
					f.click(locales.DetailCopyResponseHeadersText())
					f.click(locales.DetailRequestBody())
					f.tester.Key(ui.Ctrl, ui.KeyC)
					if f.panel.clipboard != nil || f.panel.requestTab != tabOverview {
						t.Fatal("retained view accepted content input")
					}
					f.data.pending, f.data.entry = nil, next
					f.tester.Frame()
					if f.panel.entryID != next.ID || f.data.retained != nil || f.panel.retained != nil {
						t.Fatal("ready metadata did not immediately replace the old view")
					}
					assertLoadingIndicator(t, f, false)
					f.has("201")
					rendertest.Shoot(t, f.tester, name+"-ready")
					beginRetainedSwitch(f, httpEntry())
					f.click(locales.DetailClosePanel())
					if !closed {
						t.Fatal("retained close action did not run")
					}
				})
			}
		}
	}
}

func TestHistoryTransitionWaitsForRequiredBody(t *testing.T) {
	for _, tab := range []string{tabResponseHeaders, tabRequestRaw, tabRequestBody, tabResponseRaw, tabResponseBody, tabWebSocket} {
		t.Run(tab, func(t *testing.T) {
			old, next := httpEntry(), httpEntry()
			next.ID = 200
			if tab == tabWebSocket {
				old.Type, next.Type = "wss", "wss"
			}
			body := &proxyservice.TrafficBodyView{RequestBody: "old request", ResponseBody: "old response"}
			f := newFixture(t, old, body)
			if tab == tabRequestBody || tab == tabRequestRaw {
				f.panel.requestTab = tab
			} else {
				f.panel.responseTab = tab
			}
			f.settle()
			before := f.tester.Texts()
			beginRetainedSwitch(f, next)
			requested := false
			f.data.request = func() { requested, f.data.loading = true, true }
			f.data.pending, f.data.entry = nil, next
			f.tester.Advance(60 * time.Millisecond)
			f.tester.Frame()
			if requested != (tab != tabResponseHeaders) {
				t.Fatal("body demand does not match the incoming tabs")
			}
			if requested {
				if f.data.retained == nil || !slices.Equal(before, f.tester.Texts()) {
					t.Fatal("metadata completion flashed an incomplete body view")
				}
				f.data.request = nil
				f.data.loading = false
				f.data.body = &proxyservice.TrafficBodyView{RequestBody: "new request", ResponseBody: "new response"}
				f.tester.Advance(40 * time.Millisecond)
				f.tester.Frame()
			}
			if f.data.retained != nil || f.panel.entryID != next.ID {
				t.Fatal("ready panes did not commit the new detail")
			}
			assertLoadingIndicator(t, f, false)
			if tab == tabResponseBody && f.panel.responseBody.content.Body != "new response" {
				t.Fatal("new body was not displayed on the completion frame")
			}
			if tab == tabRequestRaw && f.panel.requestRaw.text != requestRawText(next, f.data.body) {
				t.Fatal("Raw mixed metadata and body from different selections")
			}
		})
	}
}

func TestHistoryTransitionTimeoutKeepsLoadingDeadline(t *testing.T) {
	for _, bodyPending := range []bool{false, true} {
		t.Run(fmt.Sprintf("body=%t", bodyPending), func(t *testing.T) {
			f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{ResponseBody: "old body"})
			f.panel.responseTab = tabResponseBody
			f.settle()
			next := httpEntry()
			next.ID = 200
			beginRetainedSwitch(f, next)
			f.tester.Advance(100 * time.Millisecond)
			if bodyPending {
				f.data.pending, f.data.entry, f.data.loading = nil, next, true
			} else {
				// Further selections cannot extend retention of the original view.
				f.data.pending = &logic.TrafficEntry{ID: 201, Type: "https"}
			}
			f.tester.Frame()
			f.tester.Advance(loadingIndicatorDelay - 100*time.Millisecond)
			// Store tests verify the deadline fires even without a panel frame.
			f.data.ReleaseRetainedDetail()
			assertPanelPayloadsReleased(t, f.panel)
			f.tester.Frame()
			assertLoadingIndicator(t, f, true)
			f.data.pending, f.data.loading = nil, false
			f.panel.Release()
			f.tester.Advance(loadingIndicatorDelay)
			assertLoadingIndicator(t, f, false)
		})
	}
}

func TestRetainedTCPContentIsDisabledAndClosable(t *testing.T) {
	entry := &logic.TrafficEntry{ID: 1, Type: "tcp", Host: "old.test", URL: "old.test:443"}
	f := newFixture(t, entry, nil)
	closed := false
	f.panel.OnClose = func() { closed = true }
	f.settle()
	before := f.tester.Texts()
	beginRetainedSwitch(f, &logic.TrafficEntry{ID: 2, Type: "https"})
	if !slices.Equal(before, f.tester.Texts()) {
		t.Fatal("TCP transition cleared the previous content")
	}
	if info, ok := f.tester.Access(logic.TrafficTarget(entry)); !ok || !info.Disabled {
		t.Fatalf("retained TCP value remains selectable: %+v found=%t texts=%v", info, ok, f.tester.Texts())
	}
	f.click(locales.DetailClosePanel())
	if !closed {
		t.Fatal("retained TCP lost its close action")
	}
}

func TestRetainedCookiePopoverCannotCopy(t *testing.T) {
	f := cookieFixture(t, "sid=old-value; Path=/")
	f.openAttributes("sid", "1")
	raw, _ := f.tester.Find(locales.DetailCookieRawHeader())
	head, _ := f.tester.Find(locales.DetailCookieAttributeCount("1"))
	next := httpEntry()
	next.ID = 200
	beginRetainedSwitch(f, next)
	f.tester.ClickAt(head.X+head.W-12, raw.Y+raw.H/2)
	f.tester.Frame()
	if f.tester.Clipboard() != "" || f.panel.clipboard != nil {
		t.Fatal("an existing popover copied stale content during replacement")
	}
}

func TestRetainedBodyBlocksKeyboardCopy(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{ResponseBody: "previous selected text"})
	f.panel.OnClose = func() {}
	f.panel.responseTab = tabResponseBody
	f.settle()
	editor, ok := f.tester.AccessByRole(ui.RoleTextField, "")
	if !ok || editor.Value != "previous selected text" {
		t.Fatalf("body editor missing: %+v", editor)
	}
	f.tester.ClickAt(editor.Bounds.X+30, editor.Bounds.Y+10)
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.SetClipboard("unchanged")
	next := httpEntry()
	next.ID = 200
	beginRetainedSwitch(f, next)
	editor, ok = f.tester.AccessByRole(ui.RoleTextField, "")
	if !ok || !editor.Disabled || editor.Focusable {
		t.Fatalf("retained editor still accepts input: %+v", editor)
	}
	f.tester.Key(ui.Ctrl, ui.KeyC)
	f.tester.Key(ui.Cmd, ui.KeyC)
	if f.tester.Clipboard() != "unchanged" {
		t.Fatal("keyboard copied the previous row's body during replacement")
	}
}

func TestRetainedViewKeepsPendingBodyLoading(t *testing.T) {
	for _, tab := range []string{tabRequestRaw, tabResponseBody, tabResponseRaw, tabWebSocket} {
		for _, shown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/spinner=%t", tab, shown), func(t *testing.T) {
				old, next := httpEntry(), httpEntry()
				next.ID = 200
				if tab == tabWebSocket {
					old.Type, next.Type = "wss", "wss"
				}
				f := newFixture(t, old, nil)
				if tab == tabRequestRaw {
					f.panel.requestTab = tab
				} else {
					f.panel.responseTab = tab
				}
				f.data.loading = true
				f.settle()
				if shown {
					f.tester.Advance(loadingIndicatorDelay)
					f.tester.Frame()
				}
				assertLoadingIndicator(t, f, shown)
				before, texts := f.tester.Image(), f.tester.Texts()
				beginRetainedSwitch(f, next)
				// An unread body is not an empty one, and its wait keeps its deadline.
				if !slices.Equal(texts, f.tester.Texts()) || !bytes.Equal(before.Pix, f.tester.Image().Pix) {
					t.Fatalf("retained pending body changed the previous view: %v", f.tester.Texts())
				}
				if slices.Contains(f.tester.Texts(), locales.DetailBodyEmpty()) {
					t.Fatal("retained view called a loading body empty")
				}
				assertLoadingIndicator(t, f, shown)
				f.data.pending, f.data.entry = nil, next
				f.data.body = &proxyservice.TrafficBodyView{RequestBody: "new request", ResponseBody: "new response"}
				f.tester.Frame()
				if f.data.retained != nil || f.panel.entryID != next.ID {
					t.Fatal("ready detail did not replace the retained view")
				}
			})
		}
	}
}
