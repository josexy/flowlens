package capture

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

func filterTabLabel(tab string) string {
	switch tab {
	case logic.TabAll:
		return locales.FilterAll()
	case logic.TabHTTP:
		return locales.FilterHttp()
	case logic.TabHTTPS:
		return locales.FilterHttps()
	case logic.TabWebSocket:
		return locales.FilterWebsocket()
	case logic.TabSSE:
		return locales.FilterSse()
	case logic.TabTCP:
		return locales.FilterTcp()
	case logic.TabHTTP1:
		return locales.FilterHttp1()
	case logic.TabHTTP2:
		return locales.FilterHttp2()
	case logic.TabJSON:
		return locales.FilterJson()
	case logic.TabXML:
		return locales.FilterXml()
	case logic.TabText:
		return locales.FilterText()
	case logic.TabHTML:
		return locales.FilterHtml()
	case logic.TabJS:
		return locales.FilterJs()
	case logic.TabImage:
		return locales.FilterImage()
	case logic.TabMedia:
		return locales.FilterMedia()
	case logic.TabBinary:
		return locales.FilterBinary()
	default:
		return tab
	}
}

// BuildFilterBar builds the type tabs and the search box.
func BuildFilterBar(c *ui.Context, theme nui.Theme, filter *store.FilterStore, focus *ui.Handle) {
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).PaddingX(10).PaddingY(4).Background(theme.MutedBackground)
	row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	row.Children(func() {
		ui.Scroll(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			items := make([]nui.TabItem, len(logic.TrafficFilterTabs))
			for index, tab := range logic.TrafficFilterTabs {
				items[index] = nui.TabItem{ID: tab, Label: filterTabLabel(tab)}
			}
			nui.Tabs(c, theme, &filter.ActiveTab, items, nui.TabsOptions{
				Label: locales.TrafficType(), Size: nui.XS, Content: nui.Set(false),
			})
		})
		ui.Box(c).Width(200).Shrink(0).Children(func() {
			nui.Input(c, theme, &filter.SearchText, nui.InputOptions{
				Label: locales.FilterSearchPlaceholder(), Placeholder: locales.FilterSearchPlaceholder(),
				Leading: lucide.Search(), Size: nui.SM, Clearable: true, ClearLabel: locales.ToolbarClear(),
				Style: func(_, editor, _, _ ui.Element) { editor.Bind(focus) },
			})
		})
	})
}
