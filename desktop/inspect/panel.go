// Package inspect builds the traffic detail panel.
package inspect

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Data is the entry a panel shows.
type Data interface {
	SelectedEntry() *logic.TrafficEntry
	BodyView() *proxyservice.TrafficBodyView
	// BodyVersion changes whenever BodyView does.
	BodyVersion() uint64
	BodyLoading() bool
}

// LiveData shows the selection of the live capture.
type LiveData struct{ Traffic *store.TrafficStore }

// SelectedEntry implements Data.
func (d LiveData) SelectedEntry() *logic.TrafficEntry { return d.Traffic.Selected }

// BodyView implements Data.
func (d LiveData) BodyView() *proxyservice.TrafficBodyView { return d.Traffic.BodyView }

// BodyVersion implements Data.
func (d LiveData) BodyVersion() uint64 { return d.Traffic.BodyViewVersion }

// BodyLoading implements Data.
func (d LiveData) BodyLoading() bool { return d.Traffic.BodyViewLoading }

// Dependencies are what a panel needs besides its data.
type Dependencies struct {
	Runtime  store.Runtime
	Settings *store.SettingsStore
	Notifier *widgets.Notifier
	Icons    *widgets.ProcessIcons
	// PickSavePath asks where to save a file; an empty path means canceled.
	PickSavePath func(filename string) (string, error)
	// SaveBody writes a body to a file.
	SaveBody func(proxyservice.SaveBodyToFileRequest) error
}

// Tabs of the two halves of the panel.
const (
	tabOverview        = "request-overview"
	tabRequestRaw      = "request-raw"
	tabRequestHeaders  = "request-headers"
	tabRequestQuery    = "request-query"
	tabRequestCookies  = "request-cookies"
	tabRequestBody     = "request-body"
	tabWebSocket       = "websocket"
	tabResponseError   = "response-error"
	tabResponseRaw     = "response-raw"
	tabResponseHeaders = "response-headers"
	tabResponseCookies = "response-cookies"
	tabResponseTrailer = "response-trailers"
	tabResponseBody    = "response-body"

	splitRequestResponse = "requestResponse"
	defaultSplit         = 50
	minSplit             = 30
	maxSplit             = 70
)

// Panel shows one traffic entry.
type Panel struct {
	data Data
	deps Dependencies
	// OnClose, when set, shows a control that closes the panel.
	OnClose func()

	entryID        uint64
	requestTab     string
	responseTab    string
	requestSort    logic.HeaderSortOrder
	responseSort   logic.HeaderSortOrder
	trailerSort    logic.HeaderSortOrder
	split          float32
	splitLayout    settingservice.TrafficDetailLayout
	splitLoaded    bool
	overview       overviewState
	requestBody    *BodyViewer
	responseBody   *BodyViewer
	requestRaw     *rawPane
	responseRaw    *rawPane
	messages       *MessageStream
	cookiePopovers map[int]*bool
	clipboard      *string
}

// NewPanel returns a panel of data.
func NewPanel(data Data, deps Dependencies) *Panel {
	return &Panel{
		data: data, deps: deps, requestTab: tabOverview, responseTab: tabResponseHeaders,
		requestBody: NewBodyViewer(deps, false), responseBody: NewBodyViewer(deps, true),
		requestRaw: newRawPane(), responseRaw: newRawPane(), messages: NewMessageStream(deps),
		cookiePopovers: map[int]*bool{},
	}
}

func (p *Panel) copyText(text, success string) {
	p.clipboard = &text
	p.deps.Notifier.Success(success)
}

func splitName(layout settingservice.TrafficDetailLayout) string {
	return splitRequestResponse + ":" + string(layout)
}

func (p *Panel) reconcileTabs(entry *logic.TrafficEntry, entryChanged bool) {
	if entryChanged {
		p.requestSort, p.responseSort, p.trailerSort = logic.HeaderSortDefault, logic.HeaderSortDefault, logic.HeaderSortDefault
	}
	hasQuery := len(logic.ParseURLQuery(entry.URL).Fields) > 0
	if p.requestTab == tabRequestQuery && !hasQuery {
		p.requestTab = tabRequestHeaders
	}
	if p.requestTab == tabRequestCookies && !hasRequestCookies(entry) {
		p.requestTab = tabRequestHeaders
	}
	websocket := logic.IsWebSocket(entry)
	switch {
	case entry.Error != nil:
		p.responseTab = tabResponseError
	case entryChanged && websocket:
		p.responseTab = tabWebSocket
	case p.responseTab == tabResponseCookies && !hasResponseCookies(entry):
		p.responseTab = tabResponseHeaders
	case (p.responseTab == tabResponseError && !websocket) || (p.responseTab == tabWebSocket && !websocket) || (p.responseTab == tabResponseTrailer && !hasTrailers(entry)):
		p.responseTab = tabResponseHeaders
	case p.responseTab == tabResponseError && websocket:
		p.responseTab = tabWebSocket
	}
}

func hasRequestCookies(entry *logic.TrafficEntry) bool {
	return entry.Request != nil && logic.HasCookieHeader(entry.Request.HeaderFields, "cookie")
}

func hasResponseCookies(entry *logic.TrafficEntry) bool {
	return entry.Response != nil && logic.HasCookieHeader(entry.Response.HeaderFields, "set-cookie")
}

func hasTrailers(entry *logic.TrafficEntry) bool {
	return entry.Response != nil && len(entry.Response.TrailerFields) > 0
}

func (p *Panel) loadSplit() {
	layout := p.deps.Settings.TrafficDetailLayout()
	if p.splitLoaded && layout == p.splitLayout {
		return
	}
	p.splitLoaded, p.splitLayout = true, layout
	p.split = p.deps.Settings.TrafficDetailSplit(splitName(layout), defaultSplit)
}

// Build builds the panel for the current entry.
func (p *Panel) Build(c *ui.Context, theme nui.Theme) {
	if p.clipboard != nil {
		c.WriteClipboard(*p.clipboard)
		p.clipboard = nil
	}
	entry := p.data.SelectedEntry()
	if entry == nil {
		p.entryID = 0
		ui.Column(c).Fill().Center().Children(func() {
			nui.Empty(c, theme, nui.EmptyOptions{Icon: lucide.MousePointerClick(), Title: locales.DetailSelectRequest(), Variant: nui.Ghost})
		})
		return
	}
	entryChanged := entry.ID != p.entryID
	p.entryID = entry.ID
	p.reconcileTabs(entry, entryChanged)
	if logic.IsRawTCP(entry) {
		p.rawTCP(c, theme, entry)
		return
	}
	p.loadSplit()
	layout := p.splitLayout
	nui.Splitter(c.Key("detail-split"), theme, &p.split, nui.SplitterOptions{
		Percent: true, Vertical: layout == settingservice.TrafficDetailLayoutVertical,
		Min: nui.Set[float32](minSplit), Max: nui.Set[float32](maxSplit),
		OnChange: func(percent float32) { p.deps.Settings.SetTrafficDetailSplit(splitName(layout), percent) },
	}, func() { p.requestPane(c, theme, entry) }, func() { p.responsePane(c, theme, entry) }).Fill()
}

func countLabel(label string, count int) string { return label + "(" + strconv.Itoa(count) + ")" }

func (p *Panel) tabBar(c *ui.Context, theme nui.Theme, key string, value *string, items []nui.TabItem, trailing func()) {
	ui.Row(c.Key(key)).FillWidth().Height(38).Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(10).Children(func() {
		ui.Scroll(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			nui.Tabs(c, theme, value, items, nui.TabsOptions{Label: key, Size: nui.SM, Content: nui.Set(false), Underline: true})
		})
		if trailing != nil {
			trailing()
		}
	})
}

func (p *Panel) requestPane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	headers := []logic.HeaderField{}
	if entry.Request != nil {
		headers = entry.Request.HeaderFields
	}
	items := []nui.TabItem{
		{ID: tabOverview, Label: locales.DetailOverview()},
		{ID: tabRequestRaw, Label: locales.DetailRaw()},
		{ID: tabRequestHeaders, Label: countLabel(locales.DetailRequestHeaders(), len(headers))},
	}
	query := logic.ParseURLQuery(entry.URL)
	if len(query.Fields) > 0 {
		items = append(items, nui.TabItem{ID: tabRequestQuery, Label: countLabel(locales.DetailQuery(), len(query.Fields))})
	}
	if hasRequestCookies(entry) {
		items = append(items, nui.TabItem{ID: tabRequestCookies, Label: countLabel(locales.DetailCookies(), len(logic.HeadersRecordToFields(logic.RequestCookiesRecord(headers))))})
	}
	items = append(items, nui.TabItem{ID: tabRequestBody, Label: locales.DetailRequestBody()})
	ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		p.tabBar(c, theme, "request-tabs", &p.requestTab, items, nil)
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch p.requestTab {
			case tabRequestRaw:
				p.requestRawPane(c, theme, entry)
			case tabRequestHeaders:
				p.headersPane(c, theme, headersPaneOptions{
					title: locales.WorkspaceHttpRequestRequestHeaderList(), fields: headers, sort: &p.requestSort,
					warning: truncatedHeaders(entry.Request), orderUnavailable: entry.Request != nil && entry.Request.HeaderOrderUnavailable,
					jsonLabel: locales.DetailCopyRequestHeadersJson(), sortLabel: locales.DetailSortRequestHeaders(), textLabel: locales.DetailCopyRequestHeadersText(),
					jsonCopied: locales.DetailRequestHeadersCopiedJson(), textCopied: locales.DetailRequestHeadersCopiedText(), sorted: locales.DetailRequestHeadersSortedState,
				})
			case tabRequestQuery:
				p.queryPane(c, theme, query)
			case tabRequestCookies:
				p.requestCookiesPane(c, theme, headers)
			case tabRequestBody:
				p.bodyPane(c, theme, p.requestBody, entry, false)
			default:
				p.overviewPane(c, theme, entry)
			}
		})
	})
}

func (p *Panel) responseItems(entry *logic.TrafficEntry) []nui.TabItem {
	if entry.Error != nil {
		return []nui.TabItem{{ID: tabResponseError, Label: locales.DetailError()}}
	}
	var items []nui.TabItem
	if logic.IsWebSocket(entry) {
		items = append(items, nui.TabItem{ID: tabWebSocket, Label: "WebSocket"})
	}
	headers := []logic.HeaderField{}
	if entry.Response != nil {
		headers = entry.Response.HeaderFields
	}
	items = append(items, nui.TabItem{ID: tabResponseRaw, Label: locales.DetailRaw()},
		nui.TabItem{ID: tabResponseHeaders, Label: countLabel(locales.DetailResponseHeaders(), len(headers))})
	if hasResponseCookies(entry) {
		items = append(items, nui.TabItem{ID: tabResponseCookies, Label: countLabel(locales.DetailCookies(), len(logic.ParseResponseCookies(headers)))})
	}
	if hasTrailers(entry) {
		items = append(items, nui.TabItem{ID: tabResponseTrailer, Label: countLabel(locales.DetailResponseTrailers(), len(entry.Response.TrailerFields))})
	}
	return append(items, nui.TabItem{ID: tabResponseBody, Label: locales.DetailResponseBody()})
}

func (p *Panel) responseChips(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if entry.Error != nil {
		return
	}
	ui.Row(c).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
		color, status := nui.Neutral, "-"
		if entry.StatusCode != 0 {
			status = strconv.Itoa(entry.StatusCode)
			switch {
			case entry.StatusCode >= 500:
				color = nui.Error
			case entry.StatusCode >= 400:
				color = nui.Warning
			case entry.StatusCode >= 300:
				color = nui.Info
			case entry.StatusCode >= 200:
				color = nui.Success
			}
		}
		chip := nui.Badge(c, theme, status, nui.BadgeOptions{Color: color, Variant: nui.Subtle, Size: nui.SM})
		if tip := entry.Status; tip != "" {
			nui.Tooltip(c, theme, chip, tip, nui.TooltipOptions{})
		}
		protocol := "-"
		if entry.Response != nil && entry.Response.Proto != "" {
			protocol = entry.Response.Proto
		}
		nui.Badge(c, theme, protocol, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Subtle, Size: nui.SM})
	})
}

func (p *Panel) responsePane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	headers := []logic.HeaderField{}
	var trailers []logic.HeaderField
	if entry.Response != nil {
		headers, trailers = entry.Response.HeaderFields, entry.Response.TrailerFields
	}
	ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		p.tabBar(c, theme, "response-tabs", &p.responseTab, p.responseItems(entry), func() {
			p.responseChips(c, theme, entry)
			if p.OnClose != nil {
				iconButton(c, theme, "close-detail", lucide.X(), locales.DetailClosePanel(), false, p.OnClose)
			}
		})
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch p.responseTab {
			case tabResponseError:
				p.errorPane(c, theme, entry)
			case tabWebSocket:
				p.webSocketPane(c, theme)
			case tabResponseRaw:
				p.responseRawPane(c, theme, entry)
			case tabResponseCookies:
				p.responseCookiesPane(c, theme, headers, truncatedHeaders(entry.Response))
			case tabResponseTrailer:
				p.headersPane(c, theme, headersPaneOptions{
					title: locales.DetailResponseTrailerList(), fields: trailers, sort: &p.trailerSort,
					warning: truncatedTrailers(entry.Response), orderUnavailable: entry.Response != nil && entry.Response.TrailerOrderUnavailable,
					jsonLabel: locales.DetailCopyResponseTrailersJson(), sortLabel: locales.DetailSortResponseTrailers(), textLabel: locales.DetailCopyResponseTrailersText(),
					jsonCopied: locales.DetailResponseTrailersCopiedJson(), textCopied: locales.DetailResponseTrailersCopiedText(), sorted: locales.DetailResponseTrailersSortedState,
				})
			case tabResponseBody:
				p.bodyPane(c, theme, p.responseBody, entry, true)
			default:
				p.headersPane(c, theme, headersPaneOptions{
					title: locales.WorkspaceHttpRequestResponseHeaderList(), fields: headers, sort: &p.responseSort,
					warning: truncatedHeaders(entry.Response), orderUnavailable: entry.Response != nil && entry.Response.HeaderOrderUnavailable,
					jsonLabel: locales.DetailCopyResponseHeadersJson(), sortLabel: locales.DetailSortResponseHeaders(), textLabel: locales.DetailCopyResponseHeadersText(),
					jsonCopied: locales.DetailResponseHeadersCopiedJson(), textCopied: locales.DetailResponseHeadersCopiedText(), sorted: locales.DetailResponseHeadersSortedState,
				})
			}
		})
	})
}

func (p *Panel) errorPane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	ui.Column(c).FillWidth().Padding(16).Gap(8).Children(func() {
		ui.Text(c, logic.FormatTimeRFC3339(entry.Error.Timestamp)).FontSize(13).TextColor(theme.Muted)
		ui.Text(c, entry.Error.Error).Selectable().FontSize(13).TextColor(theme.Error)
	})
}

func truncatedHeaders(message *proxyservice.HTTPMessage) string {
	if message != nil && message.HeadersTruncated {
		return locales.DetailHeaderFieldsTruncated()
	}
	return ""
}

func truncatedTrailers(message *proxyservice.HTTPMessage) string {
	if message != nil && message.TrailersTruncated {
		return locales.DetailTrailerFieldsTruncated()
	}
	return ""
}
