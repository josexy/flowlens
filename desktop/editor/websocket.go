package editor

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/inspect"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Tabs of a WebSocket Client.
const (
	socketTabMessage         = "message"
	socketTabHeaders         = "headers"
	socketTabQuery           = "query"
	socketTabCookies         = "cookies"
	socketTabSettings        = "settings"
	socketTabResponseHeaders = "response-headers"
	socketTabResponseCookies = "response-cookies"
	socketTabResponse        = "response"
	socketTabResponseError   = "response-error"
	socketStatusConnected    = "connected"
	socketStatusConnecting   = "connecting"
	socketStatusError        = "error"
	socketStatusCancelled    = "cancelled"
	socketStatusClosed       = "closed"
	socketStatusIdle         = "idle"
	draftTypeBinaryFile      = "binary-file"
	viewModeConversation     = "conversation"
)

// socketPane shows one WebSocket Client tab.
type socketPane struct {
	editor *Editor
	key    string
	tab    *logic.WorkspaceTab
	state  *logic.WebSocketClientState
	requestRows
	draft        *widgets.CodeView
	draftText    string
	fileGuard    int
	split        float32
	restoreProxy string
	seenURL      string
	timeout      numberField
	stream       *inspect.MessageStream
	cookiesView  *inspect.CookieList
	seenCount    int
	seenFilter   string
	seenFirst    string
	rightTab     string
}

func draftLanguage(kind string) string {
	switch kind {
	case "json":
		return "json"
	case "xml":
		return "xml"
	default:
		return "plaintext"
	}
}

func newSocketPane(e *Editor, tab *logic.WorkspaceTab) *socketPane {
	state := tab.WebSocket
	p := &socketPane{editor: e, key: tab.Key, tab: tab, state: state, split: defaultEditorSplit, seenCount: -1}
	p.initQuery(&state.URL, &state.Params)
	p.seenURL = state.URL
	p.draft = widgets.NewCodeView(draftLanguage(state.DraftType), false)
	p.draft.SetText(state.DraftText)
	p.draftText = state.DraftText
	p.draft.OnChange = func(text string) { p.draftText, state.DraftText = text, text }
	p.stream = inspect.NewMessageStream(e.Host.Inspect)
	p.stream.OnClear = func() { e.Data.Workspace.ClearWebSocketMessages(tab.Key) }
	p.cookiesView = inspect.NewCookieList(e.Host.Inspect)
	return p
}

func (p *socketPane) syncState() {
	state := p.state
	p.syncQuery(&state.URL, &state.Params)
	if state.URL != p.seenURL {
		p.seenURL = state.URL
		p.editor.Data.Workspace.UpdateRequestTabTitle(p.key, state.URL)
	}
	p.syncCookies(&state.Headers)
	if state.DraftText != p.draftText {
		p.draftText = state.DraftText
		p.draft.SetText(state.DraftText)
	}
	p.draft.SetLanguage(draftLanguage(state.DraftType))
	first := ""
	if len(state.Messages) > 0 {
		first = state.Messages[0].ID
	}
	if filter := state.DirectionFilter; filter != "" && filter != p.stream.Filter {
		p.stream.Filter, p.seenCount = filter, -1
	}
	if len(state.Messages) != p.seenCount || p.stream.Filter != p.seenFilter || first != p.seenFirst {
		p.seenCount, p.seenFilter, p.seenFirst = len(state.Messages), p.stream.Filter, first
		p.stream.SetMessages(state.Messages)
	}
	p.stream.Conversation = state.ViewMode == viewModeConversation
}

func (p *socketPane) canSend() bool {
	state := p.state
	if state.ConnectionStatus != socketStatusConnected || state.SessionID == "" {
		return false
	}
	return state.DraftType != draftTypeBinaryFile || state.DraftFile != nil
}

func (p *socketPane) send() {
	state := p.state
	if state.IsSendingMessage {
		return
	}
	if !p.canSend() {
		if state.DraftType == draftTypeBinaryFile && state.DraftFile == nil {
			p.editor.Notifier.Error(locales.WorkspaceWebsocketClientFileRequired())
			return
		}
		p.editor.Notifier.Error(locales.WorkspaceWebsocketClientNotConnected())
		return
	}
	p.editor.Data.Workspace.SendWebSocketMessage(p.key, func(err error) {
		if err != nil {
			p.editor.Notifier.Error(locales.WorkspaceWebsocketClientSendFailed(err.Error()))
		}
	})
}

func (p *socketPane) toggleConnection() {
	state := p.state
	workspace := p.editor.Data.Workspace
	switch state.ConnectionStatus {
	case socketStatusConnecting:
		workspace.CancelWebSocketConnection(p.key)
		return
	case socketStatusConnected:
		workspace.DisconnectWebSocket(p.key, func(err error) {
			if err != nil {
				p.editor.Notifier.Error(err.Error())
			}
		})
		return
	}
	if problem := logic.ValidateWebSocketURL(state.URL); problem != "" {
		p.editor.Notifier.Error(problem)
		return
	}
	if name, invalid := logic.FindInvalidRequestHeaderName(state.Headers, logic.ProtocolAuto); invalid {
		state.ActiveLeftTab = socketTabHeaders
		p.editor.Notifier.Error(locales.WorkspaceHttpRequestHeaderInvalidName(name))
		return
	}
	workspace.ConnectWebSocket(p.key, func(err error) {
		if err != nil {
			p.editor.Notifier.Error(locales.WorkspaceWebsocketClientConnectFailed(err.Error()))
		}
	})
}

func (p *socketPane) declareShortcuts() {
	workspace := p.editor.Data.Workspace
	p.editor.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.RequestEditorSend,
		When:      func() bool { return workspace.ActiveKey == p.key && p.canSend() && !p.state.IsSendingMessage },
		Run:       p.send,
	})
}

func (p *socketPane) build(c *ui.Context, theme nui.Theme) {
	p.declareShortcuts()
	p.syncState()
	ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		p.connectionLine(c, theme)
		nui.Splitter(c.Key("split"), theme, &p.split, nui.SplitterOptions{Percent: true, Min: nui.Set[float32](30), Max: nui.Set[float32](82)},
			func() { p.leftPanel(c, theme) },
			func() { p.rightPanel(c, theme) },
		).Fill()
	})
}

func (p *socketPane) connectionLine(c *ui.Context, theme nui.Theme) {
	state := p.state
	requestLine(c, theme, "connection-line", func() {
		urlField{
			url: &state.URL, label: locales.WorkspaceWebsocketClientUrlPlaceholder(), placeholder: locales.WorkspaceWebsocketClientUrlPlaceholder(),
			mitm: state.Settings.ProxyMode == logic.ProxyModeMITM,
			toggleMITM: func() {
				state.Settings.ProxyMode = logic.ToggleMITMProxyMode(state.Settings.ProxyMode, &p.restoreProxy)
			},
		}.build(c, theme)
		label, tooltip, icon, color := locales.WorkspaceWebsocketClientConnect(), locales.WorkspaceWebsocketClientConnect(), lucide.Link(), nui.Primary
		switch state.ConnectionStatus {
		case socketStatusConnecting:
			label, tooltip, icon, color = locales.WorkspaceWebsocketClientStop(), locales.WorkspaceWebsocketClientStopConnecting(), lucide.Square(), nui.Error
		case socketStatusConnected:
			label, tooltip, icon, color = locales.WorkspaceWebsocketClientDisconnect(), locales.WorkspaceWebsocketClientDisconnect(), lucide.CircleStop(), nui.Error
		}
		primaryAction(c, theme, label, tooltip, icon, color, nil, p.toggleConnection)
	})
}

func (p *socketPane) leftPanel(c *ui.Context, theme nui.Theme) {
	state := p.state
	items := []nui.TabItem{
		{ID: socketTabMessage, Label: locales.WorkspaceWebsocketClientMessage()},
		{ID: socketTabHeaders, Label: locales.WorkspaceWebsocketClientRequestHeaders() + "(" + strconv.Itoa(enabledRowCount(state.Headers)) + ")"},
		{ID: socketTabQuery, Label: locales.WorkspaceHttpRequestQuery() + "(" + strconv.Itoa(logic.CountRequestQueryRows(state.Params)) + ")"},
		{ID: socketTabCookies, Label: locales.WorkspaceHttpRequestCookies() + "(" + strconv.Itoa(logic.CountRequestCookieRows(state.Headers)) + ")"},
		{ID: socketTabSettings, Label: locales.WorkspaceWebsocketClientSettings()},
	}
	ui.Column(c.Key("left-panel")).Fill().MinHeight(0).Children(func() {
		widgets.TabStrip(c.Key("left-tabs"), theme, &state.ActiveLeftTab, items, widgets.TabStripOptions{Label: locales.WorkspaceWebsocketClientMessage(), PaddingX: 10})
		ui.Column(c.Key(state.ActiveLeftTab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch state.ActiveLeftTab {
			case socketTabHeaders:
				p.editor.headersPane(c, theme, &state.Headers, logic.ProtocolAuto)
			case socketTabQuery:
				p.editor.queryPane(c, theme, &state.Params)
			case socketTabCookies:
				p.editor.cookiesPane(c, theme, &p.requestRows, &state.Headers)
			case socketTabSettings:
				p.settingsPane(c, theme)
			default:
				p.messagePane(c, theme)
			}
		})
	})
}

func (p *socketPane) messagePane(c *ui.Context, theme nui.Theme) {
	state := p.state
	options := []nui.Option[string]{
		{Value: "text", Label: locales.WorkspaceWebsocketClientDraftTypeText()},
		{Value: "json", Label: locales.WorkspaceWebsocketClientDraftTypeJson()},
		{Value: "xml", Label: locales.WorkspaceWebsocketClientDraftTypeXml()},
		{Value: draftTypeBinaryFile, Label: locales.WorkspaceWebsocketClientDraftTypeBinaryFile()},
	}
	binary := state.DraftType == draftTypeBinaryFile
	ui.Row(c.Key("draft-type")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(8, 10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, locales.WorkspaceWebsocketClientDraftType()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			ui.Box(c).Width(130).Children(func() {
				nui.Select(c, theme, &state.DraftType, options, nui.SelectOptions{Label: locales.WorkspaceWebsocketClientDraftType()})
			})
		})
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			if !binary {
				wordWrapButton(c, theme, p.draft)
			}
		})
	})
	ui.Box(c.Key("draft")).FillWidth().Grow(1).Basis(0).MinHeight(0).Padding(0, 10, 10, 10).Children(func() {
		ui.Box(c.Key("draft-content")).Fill().Children(func() {
			if binary {
				p.editor.fileControl(c, theme, p.key+":draft", &state.DraftFile, &p.fileGuard)
			} else {
				p.draft.Build(c, p.editor.Data.Settings.CodeFont()).Fill()
			}
			ui.Box(c.Key("send-position")).Absolute().Right(20).Bottom(12).Children(func() {
				button := nui.Button(c.Key("send-message"), theme, "", nui.ButtonOptions{Size: nui.XL, Square: true, Leading: lucide.Send(), AccessibleLabel: locales.WorkspaceWebsocketClientSendMessage(), Disabled: !p.canSend() || state.IsSendingMessage, Loading: state.IsSendingMessage}).Radius(100)
				if p.canSend() && !state.IsSendingMessage {
					button.OnClick(p.send)
				}
				nui.Tooltip(c, theme, button, locales.WorkspaceWebsocketClientSendMessage(), nui.TooltipOptions{Kbds: p.editor.kbds(shortcuts.RequestEditorSend)})
			})
		})
	})
}

func (p *socketPane) settingsPane(c *ui.Context, theme nui.Theme) {
	settings := &p.state.Settings
	ui.Scroll(c.Key("settings")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Padding(10).Children(func() {
			selectRow(c, theme, "proxy", locales.WorkspaceHttpRequestProxyMode(), &settings.ProxyMode, proxyModeOptions(), nil)
			settingRow(c, theme, "skip-tls", locales.WorkspaceHttpRequestSkipVerifyTls(), func() {
				nui.Switch(c.Key("switch"), theme, &settings.SkipVerifyTLS, nui.SwitchOptions{Size: nui.SM}).Label(locales.WorkspaceHttpRequestSkipVerifyTls())
			})
			selectRow(c, theme, "tls", locales.WorkspaceHttpRequestTlsFingerprint(), &settings.TLSClientHelloID, tlsProfileOptions(), nil)
			if settings.ProxyMode == logic.ProxyModeCustom {
				settingRow(c, theme, "custom", locales.WorkspaceHttpRequestCustomProxy(), func() {
					nui.Input(c.Key("input"), theme, &settings.CustomProxy, nui.InputOptions{Label: locales.WorkspaceHttpRequestCustomProxy(), Placeholder: locales.WorkspaceHttpRequestCustomProxyPlaceholder(), Size: nui.SM}).FillWidth()
				})
			}
			p.timeout.build(c, theme, "timeout", locales.WorkspaceHttpRequestRequestTimeout(), locales.WorkspaceHttpRequestRequestTimeoutPlaceholder(), &settings.TimeoutMs, 0, 100)
		})
	})
}

func (p *socketPane) statusLabel() string {
	switch p.state.ConnectionStatus {
	case socketStatusConnected:
		return locales.WorkspaceWebsocketClientConnectionStatusConnected()
	case socketStatusConnecting:
		return locales.WorkspaceWebsocketClientConnectionStatusConnecting()
	case socketStatusError:
		return locales.WorkspaceWebsocketClientConnectionStatusError()
	case socketStatusCancelled:
		return locales.WorkspaceWebsocketClientConnectionStatusCancelled()
	case socketStatusClosed:
		return locales.WorkspaceWebsocketClientConnectionStatusClosed()
	default:
		return locales.WorkspaceWebsocketClientConnectionStatusIdle()
	}
}

func (p *socketPane) statusColor() nui.Color {
	switch p.state.ConnectionStatus {
	case socketStatusConnected:
		return nui.Success
	case socketStatusConnecting:
		return nui.Warning
	case socketStatusError:
		return nui.Error
	default:
		return nui.Neutral
	}
}

func (p *socketPane) rightPanel(c *ui.Context, theme nui.Theme) {
	state := p.state
	fields := logic.EditableRowsToHeaderFields(state.ResponseHeaders)
	hasCookies := logic.HasCookieHeader(fields, "set-cookie")
	hasResult := state.ConnectionStatus != socketStatusIdle || len(fields) > 0 || len(state.Messages) > 0 || state.ResponseError != ""
	failed := state.ConnectionStatus == socketStatusError
	ui.Column(c.Key("right-panel")).Fill().MinHeight(0).Children(func() {
		switch {
		case state.ConnectionStatus == socketStatusConnecting:
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.WorkspaceWebsocketClientConnectionStatusConnecting()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			})
			return
		case !hasResult:
			emptyPane(c, theme, lucide.Link2Off(), locales.WorkspaceHttpRequestRequestNotStarted(), "")
			return
		case state.ConnectionStatus == socketStatusCancelled:
			emptyPane(c, theme, lucide.CircleStop(), locales.WorkspaceWebsocketClientConnectionCancelledTitle(), locales.WorkspaceWebsocketClientConnectionCancelledDescription())
			return
		}
		visible := state.ActiveRightTab
		switch {
		case failed:
			visible = socketTabResponseError
		case visible == socketTabResponseError, visible == socketTabResponseCookies && !hasCookies:
			visible = socketTabResponseHeaders
		}
		p.rightTabs(c, theme, visible, failed, hasCookies, fields)
		ui.Column(c.Key(visible)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch visible {
			case socketTabResponseError:
				details := state.ResponseError
				if details == "" {
					details = locales.WorkspaceWebsocketClientConnectionStatusError()
				}
				errorCard(c, theme, p.editor, locales.WorkspaceWebsocketClientResponseErrorTitle(), locales.WorkspaceWebsocketClientResponseErrorDescription(), locales.WorkspaceWebsocketClientResponseErrorSummary(), details, locales.WorkspaceWebsocketClientCopyResponseErrorSummary(), locales.WorkspaceWebsocketClientResponseErrorSummaryCopied())
			case socketTabResponseCookies:
				p.cookiesView.Build(c, theme, fields, "")
			case socketTabResponse:
				p.stream.Build(c, theme, false)
				state.DirectionFilter, p.seenFilter = p.stream.Filter, p.stream.Filter
				state.ViewMode = "list"
				if p.stream.Conversation {
					state.ViewMode = viewModeConversation
				}
			default:
				fieldsPane(c, theme, p.editor, locales.WorkspaceHttpRequestResponseHeaderList(), fields, "", locales.WorkspaceHttpRequestCopyResponseHeadersJson(), locales.WorkspaceHttpRequestCopyResponseHeadersText(), locales.WorkspaceHttpRequestResponseHeadersCopiedJson(), locales.WorkspaceHttpRequestResponseHeadersCopiedText())
			}
		})
	})
}

func (p *socketPane) rightTabs(c *ui.Context, theme nui.Theme, visible string, failed, hasCookies bool, fields []logic.HeaderField) {
	state := p.state
	var items []nui.TabItem
	if failed {
		items = []nui.TabItem{{ID: socketTabResponseError, Label: locales.WorkspaceWebsocketClientResponseError()}}
	} else {
		items = []nui.TabItem{{ID: socketTabResponseHeaders, Label: locales.WorkspaceHttpRequestResponseHeaders() + "(" + strconv.Itoa(len(state.ResponseHeaders)) + ")"}}
		if hasCookies {
			items = append(items, nui.TabItem{ID: socketTabResponseCookies, Label: locales.WorkspaceHttpRequestCookies() + "(" + strconv.Itoa(len(logic.ParseResponseCookies(fields))) + ")"})
		}
		items = append(items, nui.TabItem{ID: socketTabResponse, Label: locales.WorkspaceWebsocketClientResponse()})
	}
	// As the HTTP response's tabs: a click lands after the build, so
	// OnChange records it.
	p.rightTab = visible
	widgets.TabStrip(c.Key("right-head"), theme, &p.rightTab, items, widgets.TabStripOptions{
		Label: locales.WorkspaceWebsocketClientResponse(), PaddingX: 10, Trailing: func() {
			nui.Badge(c.Key("count"), theme, strconv.Itoa(len(state.Messages)), nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft})
			nui.Badge(c.Key("status"), theme, p.statusLabel(), nui.BadgeOptions{Color: p.statusColor(), Variant: nui.Soft})
		},
		OnChange: func(id string) { state.ActiveRightTab = id },
	})
	if p.rightTab != visible {
		state.ActiveRightTab = p.rightTab
	}
}
