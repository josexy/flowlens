package inspect

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const loadingIndicatorDelay = 180 * time.Millisecond

type loadingDelay struct{ started time.Time }

func (d *loadingDelay) visible(c *ui.Context) bool {
	if d.started.IsZero() {
		d.started = c.Now()
	}
	remaining := loadingIndicatorDelay - c.Now().Sub(d.started)
	if remaining > 0 {
		c.After(remaining)
		return false
	}
	return true
}

func (d *loadingDelay) spinner(c *ui.Context, theme nui.Theme) {
	if d.visible(c) {
		widgets.Spinner(c, theme).Size(24, 24).TextColor(theme.Muted).
			Role(ui.RoleStatus).Label(locales.AppLoading()).Busy(true)
	}
}

func rememberTabIDs(ids []string, items []nui.TabItem) []string {
	ids = ids[:0]
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func pendingTabLabel(id string) string {
	switch id {
	case tabOverview:
		return locales.DetailOverview()
	case tabRequestRaw, tabResponseRaw:
		return locales.DetailRaw()
	case tabRequestHeaders:
		return locales.DetailRequestHeaders()
	case tabResponseHeaders:
		return locales.DetailResponseHeaders()
	case tabRequestQuery:
		return locales.DetailQuery()
	case tabRequestCookies, tabResponseCookies:
		return locales.DetailCookies()
	case tabRequestBody:
		return locales.DetailRequestBody()
	case tabResponseBody:
		return locales.DetailResponseBody()
	case tabWebSocket:
		return "WebSocket"
	case tabResponseTrailer:
		return locales.DetailResponseTrailers()
	case tabResponseError:
		return locales.DetailError()
	default:
		return ""
	}
}

func (p *Panel) pendingDetail(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if p.pendingID != entry.ID {
		p.pendingID, p.detailWait = entry.ID, loadingDelay{}
	}
	if logic.IsRawTCP(entry) {
		ui.Column(c).Fill().Children(func() {
			widgets.PaneToolbar(c, nil, func() { p.closeButton(c, theme) })
			ui.Column(c).Fill().Center().Children(func() { p.detailWait.spinner(c, theme) })
		})
		return
	}
	p.splitPanes(c, theme,
		func() { p.pendingPane(c, theme, false) },
		func() { p.pendingPane(c, theme, true) },
	)
}

func (p *Panel) pendingPane(c *ui.Context, theme nui.Theme, response bool) {
	key, selected, ids := "request-tabs", p.requestTab, p.requestTabIDs
	var trailing func()
	if response {
		key, selected, ids = "response-tabs", p.responseTab, p.responseTabIDs
		trailing = func() { p.closeButton(c, theme) }
	}
	if len(ids) == 0 {
		ids = []string{tabOverview, tabRequestRaw, tabRequestHeaders, tabRequestBody}
		if response {
			ids = []string{tabResponseRaw, tabResponseHeaders, tabResponseBody}
		}
	}
	items := make([]nui.TabItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, nui.TabItem{ID: id, Label: pendingTabLabel(id), Disabled: true})
	}
	ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		// Bind a local selection: disabled pending tabs cannot alter preferences.
		p.tabBar(c, theme, key, &selected, items, trailing)
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Center().Children(func() {
			p.detailWait.spinner(c, theme)
		})
	})
}

func (p *Panel) requestBodyIfNeeded() {
	if deferred, ok := p.data.(deferredData); ok {
		deferred.RequestBody()
	}
}
