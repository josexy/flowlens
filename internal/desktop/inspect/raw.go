package inspect

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type rawPane struct {
	view *widgets.CodeView
	text string
}

func newRawPane() *rawPane { return &rawPane{view: widgets.NewCodeView("http", true)} }

func (r *rawPane) set(text string) {
	if text != r.text {
		r.text = text
		r.view.SetText(text)
	}
}

// loadedBody returns the panel's body view when it belongs to entry.
func (p *Panel) loadedBody(entry *logic.TrafficEntry) *proxyservice.TrafficBodyView {
	if p.data.SelectedEntry() != entry {
		return nil
	}
	return p.data.BodyView()
}

func requestRawText(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView) string {
	input := logic.RawHTTPRequestInput{Method: entry.Method, URL: entry.URL, Host: entry.Host}
	if entry.Request != nil {
		input.Protocol, input.HeaderFields = entry.Request.Proto, entry.Request.HeaderFields
	}
	if body != nil {
		input.Body, input.BodyEncoding = &body.RequestBody, body.RequestBodyEncoding
	}
	return logic.FormatRawHTTPRequest(input)
}

func hasRawResponse(entry *logic.TrafficEntry) bool {
	if entry.Response == nil {
		return false
	}
	_, status := logic.FirstHeaderValue(entry.Response.HeaderFields, ":status")
	return entry.Response.Proto != "" || entry.StatusCode > 0 || status
}

func responseRawText(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView) string {
	input := logic.RawHTTPResponseInput{Status: entry.Status, StatusCode: entry.StatusCode}
	if entry.Response != nil {
		input.Protocol, input.HeaderFields = entry.Response.Proto, entry.Response.HeaderFields
	}
	if body != nil {
		input.Body, input.BodyEncoding = &body.ResponseBody, body.ResponseBodyEncoding
	}
	return logic.FormatRawHTTPResponse(input)
}

func (p *Panel) requestRawPane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	body := p.loadedBody(entry)
	p.requestRaw.set(requestRawText(entry, body))
	unavailable := body != nil && body.RequestBodyUnavailable
	p.buildRaw(c, theme, p.requestRaw, truncatedHeaders(entry.Request), unavailable, body == nil, false)
}

func (p *Panel) responseRawPane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	body := p.loadedBody(entry)
	waiting := !hasRawResponse(entry)
	if !waiting {
		p.responseRaw.set(responseRawText(entry, body))
	}
	unavailable := body != nil && body.ResponseBodyUnavailable
	p.buildRaw(c, theme, p.responseRaw, truncatedHeaders(entry.Response), unavailable, body == nil, waiting)
}

func (p *Panel) buildRaw(c *ui.Context, theme nui.Theme, pane *rawPane, warning string, unavailable, copyDisabled, waiting bool) {
	if waiting {
		emptyState(c, theme, lucide.Clock3(), locales.DetailRawHttpWaiting())
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneToolbar(c, nil, func() {
			wrap := pane.view.WordWrap()
			wrapColor := nui.Neutral
			if wrap {
				wrapColor = nui.Primary
			}
			button := nui.Button(c.Key("wrap"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: wrapColor, Size: nui.SM, Square: true, Leading: lucide.TextWrap(), AccessibleLabel: locales.DetailWrapBody()})
			button.OnClick(func() { pane.view.SetWordWrap(!wrap) })
			nui.Tooltip(c, theme, button, locales.DetailWrapBody(), nui.TooltipOptions{})
			iconButton(c, theme, "copy", lucide.Copy(), locales.DetailCopyRawHttp(), pane.text == "" || unavailable || copyDisabled, func() { p.copyText(pane.text, locales.DetailRawHttpCopied()) })
		})
		widgets.PaneContent(c, func() { pane.view.Build(c, p.deps.Settings.CodeFont()) })
		if unavailable {
			widgets.PaneWarning(c, theme, locales.DetailBodyUnavailable())
		}
		widgets.PaneWarning(c, theme, warning)
	})
}

func tcpSourceLabel(source proxyservice.RawTCPTunnelSource) string {
	switch source {
	case proxyservice.RawTCPTunnelSourceDirect:
		return locales.DetailRawTcpSourceDirect()
	case proxyservice.RawTCPTunnelSourceHTTPConnect:
		return locales.DetailRawTcpSourceHttpConnect()
	case proxyservice.RawTCPTunnelSourceSOCKS5:
		return locales.DetailRawTcpSourceSocks5()
	default:
		return locales.DetailRawTcpSourceUnknown()
	}
}

func (p *Panel) rawTCP(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	source := proxyservice.RawTCPTunnelSourceUnknown
	tls := false
	if entry.RawTCP != nil {
		source, tls = entry.RawTCP.Source, entry.RawTCP.TLS
		if source == "" {
			source = proxyservice.RawTCPTunnelSourceUnknown
		}
	}
	metadata := entry.Metadata
	if metadata == nil {
		metadata = &proxyservice.Metadata{}
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().PaddingX(12).PaddingY(14).Gap(2).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Padding(0, 8, 4, 8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, lucide.Network()).Size(16, 16).TextColor(theme.Primary)
				ui.Text(c, locales.DetailRawTcpTunnelInfo()).FontSize(apptheme.TextSize(c, 14)).Bold()
				label, color := "TCP", nui.Neutral
				if tls {
					label, color = "TCP/TLS", nui.Info
				}
				nui.Badge(c, theme, label, nui.BadgeOptions{Color: color, Variant: nui.Subtle, Size: nui.SM})
				ui.Box(c).Grow(1)
				p.closeButton(c, theme)
			})
			detailRow(c, theme, "target", locales.DetailRawTcpTarget(), orDash(logic.TrafficTarget(entry)))
			detailRow(c, theme, "source", locales.DetailRawTcpSource(), tcpSourceLabel(source))
			detailRow(c, theme, "protocol", locales.DetailProtocol(), orDash(logic.TrafficProtocol(entry)))
			if source == proxyservice.RawTCPTunnelSourceHTTPConnect && entry.Request != nil && entry.Request.Proto != "" {
				detailRow(c, theme, "outer", locales.DetailRawTcpOuterConnectProtocol(), entry.Request.Proto)
			}
			optionalRow(c, theme, "local-src", locales.DetailLocalSourceAddress(), metadata.LocalSourceAddr)
			optionalRow(c, theme, "local-dst", locales.DetailLocalDestinationAddress(), metadata.LocalDestinationAddr)
			optionalRow(c, theme, "remote-src", locales.DetailRemoteSourceAddress(), metadata.RemoteSourceAddr)
			optionalRow(c, theme, "remote-dst", locales.DetailRemoteDestinationAddress(), metadata.RemoteDestinationAddr)
			timeRow(c, theme, "observed", locales.DetailRawTcpObservedAt(), entry.StartedAt)
			timeRow(c, theme, "local-time", locales.DetailLocalConnectionTime(), metadata.LocalConnectionEstablishedAt)
			timeRow(c, theme, "remote-time", locales.DetailRemoteConnectionTime(), metadata.RemoteConnectionEstablishedAt)
			timeRow(c, theme, "tls-time", locales.DetailSslHandshake(), metadata.SSLHandshakeCompletedAt)
			p.processSection(c, theme, entry)
			p.tlsSection(c, theme, entry)
			p.certificateSection(c, theme, entry)
			if source == proxyservice.RawTCPTunnelSourceHTTPConnect {
				p.connectHeaders(c, theme, entry)
			}
		})
	})
}

func (p *Panel) connectHeaders(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	ui.Column(c.Key("connect")).FillWidth().Gap(8).Padding(4, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(8).PaddingX(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, lucide.Rows3()).Size(16, 16).TextColor(theme.Primary)
			ui.Text(c, locales.DetailRawTcpOuterConnectHeaders()).FontSize(apptheme.TextSize(c, 14)).Bold()
		})
		if entry.Request == nil || len(entry.Request.HeaderFields) == 0 {
			ui.Text(c, locales.DetailNoHeaders()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			return
		}
		rows := make([]widgets.KeyValue, len(entry.Request.HeaderFields))
		for index, field := range entry.Request.HeaderFields {
			rows[index] = widgets.KeyValue{Key: field.Name, Value: field.Value}
		}
		widgets.KeyValueTable(c, theme, rows)
	})
}
