package inspect

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

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
