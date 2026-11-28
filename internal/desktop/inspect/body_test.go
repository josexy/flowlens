package inspect

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestBodyTextIsPreparedOnlyOnDemand(t *testing.T) {
	v := NewBodyViewer(Dependencies{}, true)
	v.active = logic.BodyTabHex
	content := BodyContent{Identity: 1, ContentType: "application/json", Body: "{\"a\":1}\r\n"}
	v.set(content)
	if v.rawReady || v.formattedReady || v.raw.Text() != "" || v.formatted.Text() != "" {
		t.Fatal("Hex prepared hidden text buffers")
	}
	v.raw.SetWordWrap(true)
	v.active = logic.BodyTabRaw
	v.set(content)
	if v.copyable() != content.Body || v.formattedReady {
		t.Fatal("Raw changed original bytes or prepared Formatted")
	}
	if saved, _ := v.saveTarget(); saved != content.Body {
		t.Fatal("Raw save changed line endings")
	}
	v.active = logic.BodyTabFormatted
	v.set(content)
	want := strings.ReplaceAll(logic.FormatBody(content.Body, logic.BodyJSON), "\r\n", "\n")
	if v.copyable() != want {
		t.Fatal("formatted copy differs")
	}
	if saved, _ := v.saveTarget(); saved != want {
		t.Fatal("formatted save differs")
	}
	v.Reset()
	if !v.raw.WordWrap() || v.raw.Text() != "" || v.formatted.Text() != "" {
		t.Fatal("reset retained data or lost wrap preference")
	}
}

func TestLargeJSONNeverPreparesHiddenFormatting(t *testing.T) {
	v := NewBodyViewer(Dependencies{}, false)
	v.active = logic.BodyTabFormatted
	body := `{"data":"` + strings.Repeat("x", logic.LargeTextThreshold) + `"}`
	v.set(BodyContent{Identity: 1, Body: body, ContentType: "application/json"})
	if v.Active() != logic.BodyTabRaw || v.formattedReady || v.formatted.Text() != "" {
		t.Fatal("large JSON retained formatted text")
	}
	if v.raw.Text() != body || v.copyable() != body {
		t.Fatal("large Raw changed")
	}
}

func TestRepeatedBodyReplacementReleasesOldText(t *testing.T) {
	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	v := NewBodyViewer(Dependencies{}, false)
	baseline := heap()
	for i := 1; i <= 20; i++ {
		v.set(BodyContent{Identity: uint64(i), Body: strings.Repeat(string(rune('a'+i)), 1<<20), ContentType: "text/plain"})
	}
	loaded := heap()
	v.set(BodyContent{Identity: 21, Body: "tiny", ContentType: "text/plain"})
	released := heap()
	t.Logf("20 x 1 MiB responses: live delta %.2f MiB; after tiny response %.2f MiB", float64(int64(loaded)-int64(baseline))/(1<<20), float64(int64(released)-int64(baseline))/(1<<20))
	if int64(loaded)-int64(baseline) > 5<<20 || int64(released)-int64(baseline) > 2<<20 {
		t.Fatal("obsolete responses remain reachable")
	}
	runtime.KeepAlive(v)
}

func TestSSEBodyViewerAppendsWithoutCopyingTheSnapshot(t *testing.T) {
	var body strings.Builder
	body.Grow(2 << 20)
	body.WriteString(strings.Repeat("data: snapshot\n\n", (1<<20)/16))
	viewer := NewBodyViewer(Dependencies{}, true)
	content := BodyContent{Identity: 1, ContentType: "text/event-stream", Body: body.String()}
	viewer.set(content)
	viewer.active = logic.BodyTabRaw
	viewer.prepareText()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 32 {
		body.WriteString("data: 新数据\n\n")
		content.Body = body.String()
		viewer.set(content)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("32 small chunks after a 1 MiB snapshot: %d bytes allocated", allocated)
	if allocated > 1<<20 {
		t.Fatalf("32 small SSE chunks after a 1 MiB snapshot allocated %d bytes", allocated)
	}
	if got := viewer.raw.Text(); got != content.Body {
		t.Fatal("incremental text differs from the body")
	}
	if viewer.formatted.Text() != "" {
		t.Fatal("SSE kept a second, unused formatted copy")
	}
	if viewer.raw.State().Buffer.CanUndo() {
		t.Fatal("read-only streamed text kept undo history")
	}
}

func TestSSEBodyViewerBuildKeepsRawAndHexCurrent(t *testing.T) {
	f := sseFixture(t, "data: first\n\n")
	f.tester.SetSize(800, 500)
	viewer := f.panel.responseBody
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		viewer.tabChoice = string(logic.BodyTabRaw)
		f.settle()
		buffer := viewer.raw.State().Buffer
		f.data.body.ResponseBody += "data: 中文😀\r\n\r\n"
		f.data.version++
		f.settle()
		if viewer.raw.State().Buffer != buffer || viewer.raw.State().Buffer.CanUndo() {
			t.Fatal("a streamed update replaced the raw editor or added undo history")
		}
		if got := viewer.raw.Text(); got != strings.ReplaceAll(f.data.body.ResponseBody, "\r\n", "\n") {
			t.Fatal("rendered raw content missed a chunk")
		}
		viewer.tabChoice = string(logic.BodyTabHex)
		f.settle()
		if viewer.hex.body != f.data.body.ResponseBody {
			t.Fatal("Hex missed the streamed bytes")
		}
		viewer.tabChoice = string(logic.BodyTabSSE)
		f.settle()
		if viewer.sse.body != f.data.body.ResponseBody || len(viewer.sse.events) == 0 {
			t.Fatal("SSE view missed the streamed bytes")
		}
		if _, ok := f.tester.AccessByRole(ui.RoleColumnHeader, locales.DetailSseId()); !ok {
			t.Fatal("SSE list disappeared after changing viewing modes")
		}
	}
}

func TestSSEBodyViewerHandlesReplacementAndTextBoundaries(t *testing.T) {
	viewer := NewBodyViewer(Dependencies{}, true)
	for _, content := range []BodyContent{
		{Identity: 1, ContentType: "text/event-stream", Body: "data: 中\r"},
		{Identity: 1, ContentType: "text/event-stream", Body: "data: 中\r\n"},
		{Identity: 1, ContentType: "text/event-stream", Body: "data: 中\r\n\r\ndata: 😀\n\n"},
		{Identity: 1, ContentType: "text/event-stream", Body: "data: replaced\n\n"},
		{Identity: 2, ContentType: "text/event-stream", Body: "data: replaced\n\ndata: new entry\n\n"},
		{Identity: 2, ContentType: "text/event-stream", Body: "data: binary\xc3", Binary: true},
		{Identity: 2, ContentType: "text/event-stream", Body: "data: binaryé\n\n"},
		{Identity: 2, ContentType: "application/json", Body: `{"restored":true}`},
	} {
		viewer.set(content)
		if content.Binary {
			continue
		}
		viewer.active = logic.BodyTabRaw
		viewer.prepareText()
		want := strings.ReplaceAll(strings.ReplaceAll(content.Body, "\r\n", "\n"), "\r", "\n")
		if got := viewer.raw.Text(); got != want {
			t.Fatalf("raw text = %q, want %q", got, want)
		}
		if content.ContentType == "application/json" {
			viewer.active = logic.BodyTabFormatted
			viewer.prepareText()
			if viewer.formatted.Text() != logic.FormatBody(content.Body, logic.BodyJSON) {
				t.Fatal("switching away from SSE did not restore formatted text")
			}
		}
	}
}

func TestRenderBodyModes(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", language, dark), func(t *testing.T) {
				f := bodyFixture(t, "text/plain", "readable text", false)
				viewer := f.panel.responseBody
				content := viewer.content
				i18n.SetLocaleFunc(func() string { return language })
				f.tester = ui.NewTester(func(c *ui.Context) {
					palette := nui.App(c, f.settings.Theme().Config)
					viewer.Build(c, palette, content)
				}, 800, 400)
				f.tester.SetLocale(language)
				f.setDark(dark)
				rendertest.SettleTheme(f.tester)
				f.has(locales.DetailBodyRaw())

				// A new image replaces an active Raw view, including its buffer.
				content = BodyContent{Identity: 2, ContentType: "image/avif", Binary: true, Body: string(avifFixture(t, "alpha"))}
				f.settle()
				f.lacks(locales.DetailBodyRaw())
				f.lacks(locales.DetailFormatted())
				if viewer.Active() != logic.BodyTabImage || viewer.image.failed || viewer.image.image.bitmap == nil || viewer.raw.Text() != "" {
					t.Fatal("binary image did not replace the Raw view with a preview")
				}
				rendertest.Shoot(t, f.tester, fmt.Sprintf("body-image-%s-dark-%v", language, dark))
				f.click(locales.DetailHex())
				if viewer.hex.body != content.Body {
					t.Fatal("image Hex lost the original bytes")
				}

				content = BodyContent{Identity: 3, ContentType: "text/plain", Binary: true, Body: "caf\xe9 \xff"}
				f.settle()
				f.lacks(locales.DetailBodyRaw())
				f.has("63 61 66 E9 20 FF")
				rendertest.Shoot(t, f.tester, fmt.Sprintf("body-binary-%s-dark-%v", language, dark))

				// Text restores its modes without overriding the valid Hex choice.
				content = BodyContent{Identity: 4, ContentType: "application/json", Body: `{"restored":true}`}
				f.settle()
				if viewer.Active() != logic.BodyTabHex {
					t.Fatal("restoring text discarded the applicable Hex choice")
				}
				f.click(locales.DetailBodyRaw())
				if viewer.raw.Text() != content.Body {
					t.Fatal("restored Raw does not show the current text")
				}
				f.click(locales.DetailFormatted())
				content.Binary, content.Body = true, "\xff"
				f.settle()
				f.lacks(locales.DetailBodyRaw())
				f.lacks(locales.DetailFormatted())
				if viewer.Active() != logic.BodyTabHex || viewer.formatted.Text() != "" {
					t.Fatal("binary JSON did not replace the formatted text with Hex")
				}

				content = BodyContent{Identity: 5, ContentType: "image/svg+xml", Body: `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect width="40" height="40" fill="orange"/></svg>`}
				f.settle()
				f.has(locales.DetailPreview())
				f.has(locales.DetailFormatted())
				f.click(locales.DetailBodyRaw())
				if viewer.raw.Text() != content.Body {
					t.Fatal("SVG lost its textual Raw view")
				}
				rendertest.Shoot(t, f.tester, fmt.Sprintf("body-svg-raw-%s-dark-%v", language, dark))

				content = BodyContent{Identity: 6, ContentType: "text/event-stream", Binary: true, Body: "data: caf\xc3"}
				f.settle()
				f.lacks(locales.DetailBodyRaw())
				f.has(locales.DetailSseUnavailable())
				content.Binary, content.Body = false, "data: café\n\n"
				f.settle()
				f.click(locales.DetailBodyRaw())
				if viewer.raw.Text() != content.Body {
					t.Fatal("SSE text recovery did not restore Raw")
				}
			})
		}
	}
}
