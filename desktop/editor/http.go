package editor

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const defaultEditorSplit float32 = 50

var requestMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

// httpPane shows one HTTP Request Editor tab.
type httpPane struct {
	editor *Editor
	key    string
	tab    *logic.WorkspaceTab
	state  *logic.HTTPEditorState
	requestRows
	urlEncoded rowsView
	formData   formDataView
	bodyGuard  int

	body, script         *widgets.CodeView
	bodyText, scriptText string

	split        float32
	restoreProxy string
	seenMethod   string
	seenURL      string
	timeout      numberField
	redirects    numberField

	response *responseView
}

func bodyLanguage(bodyType string) string {
	switch bodyType {
	case logic.BodyTypeJSON:
		return "json"
	case logic.BodyTypeXML:
		return "xml"
	default:
		return "plaintext"
	}
}

func newHTTPPane(e *Editor, tab *logic.WorkspaceTab) *httpPane {
	state := tab.HTTP
	p := &httpPane{editor: e, key: tab.Key, tab: tab, state: state, split: defaultEditorSplit, seenMethod: state.Method, seenURL: state.URL}
	p.initQuery(&state.URL, &state.Params)
	p.seenURL = state.URL
	p.body = widgets.NewCodeView(bodyLanguage(state.RequestBodyType), false)
	p.body.SetText(state.RequestBodyText)
	p.bodyText = state.RequestBodyText
	p.body.OnChange = func(text string) {
		p.bodyText, state.RequestBodyText = text, text
	}
	p.script = widgets.NewCodeView("python", false)
	p.script.SetText(state.InlineScriptSource)
	p.scriptText = state.InlineScriptSource
	p.script.OnChange = func(text string) {
		p.scriptText, state.InlineScriptSource = text, text
	}
	pythonapi.Attach(p.script)
	p.response = newResponseView(e, tab.Key, state)
	return p
}

func (p *httpPane) busy() bool { return p.state.IsSending || p.state.IsStreaming }

func (p *httpPane) syncState() {
	state := p.state
	p.syncQuery(&state.URL, &state.Params)
	if state.Method != p.seenMethod || state.URL != p.seenURL {
		p.seenMethod, p.seenURL = state.Method, state.URL
		state.Headers = logic.SynchronizeRequestRouteHeaders(state.Headers, logic.RequestProtocol(state.Settings.Protocol), state.URL)
		p.editor.Data.Workspace.UpdateRequestTabTitle(p.key, state.URL)
	}
	p.syncCookies(&state.Headers)
	if state.RequestBodyText != p.bodyText {
		p.bodyText = state.RequestBodyText
		p.body.SetText(state.RequestBodyText)
	}
	if state.InlineScriptSource != p.scriptText {
		p.scriptText = state.InlineScriptSource
		p.script.SetText(state.InlineScriptSource)
	}
	p.body.SetLanguage(bodyLanguage(state.RequestBodyType))
	p.script.SetReadOnly(p.busy())
}

func (p *httpPane) send() {
	if problem := p.editor.Data.Workspace.SendTab(p.key, p.editor.pythonEnabled()); problem != "" {
		p.editor.Notifier.Error(problem)
	}
}

func (p *httpPane) stop() { p.editor.Data.Workspace.StopTab(p.key) }

func (p *httpPane) declareShortcuts() {
	workspace := p.editor.Data.Workspace
	p.editor.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.RequestEditorSend,
		When:      func() bool { return workspace.ActiveKey == p.key && !p.busy() },
		Run:       p.send,
	})
}

func (p *httpPane) build(c *ui.Context, theme nui.Theme) {
	p.declareShortcuts()
	p.syncState()
	ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		p.requestLine(c, theme)
		nui.Splitter(c.Key("split"), theme, &p.split, nui.SplitterOptions{Percent: true, Min: nui.Set[float32](30), Max: nui.Set[float32](82)},
			func() { p.requestPanel(c, theme) },
			func() { p.response.build(c, theme) },
		).Fill()
	})
}

func (p *httpPane) requestLine(c *ui.Context, theme nui.Theme) {
	state := p.state
	requestLine(c, theme, "request-line", func() {
		urlField{
			url: &state.URL, label: locales.WorkspaceHttpRequestUrlPlaceholder(), placeholder: locales.WorkspaceHttpRequestUrlPlaceholder(),
			method: func() { methodSelect(c, theme, &state.Method) },
			mitm:   state.Settings.ProxyMode == logic.ProxyModeMITM,
			toggleMITM: func() {
				state.Settings.ProxyMode = logic.ToggleMITMProxyMode(state.Settings.ProxyMode, &p.restoreProxy)
			},
			submit: func(string) {
				if !p.busy() {
					p.send()
				}
			},
		}.build(c, theme)
		p.pluginsSwitch(c, theme)
		p.primaryButton(c, theme)
	})
}

func (p *httpPane) pluginsSwitch(c *ui.Context, theme nui.Theme) {
	state := p.state
	enabled := p.editor.pythonEnabled()
	tooltip := locales.WorkspaceHttpRequestPluginsGlobalDisabled()
	if enabled && state.PluginsEnabled {
		tooltip = locales.WorkspaceHttpRequestPluginsEnabled()
	} else if enabled {
		tooltip = locales.WorkspaceHttpRequestPluginsBypassed()
	}
	box := requestLineBox(c, theme, "plugins").Gap(8).PaddingX(10)
	box.Children(func() {
		color := theme.Muted
		if enabled && state.PluginsEnabled {
			color = theme.Primary
		}
		ui.Icon(c, lucide.FileCode()).Size(16, 16).Shrink(0).TextColor(color)
		nui.Switch(c.Key("switch"), theme, &state.PluginsEnabled, nui.SwitchOptions{Disabled: !enabled || p.busy(), Size: nui.SM}).Label(locales.WorkspaceHttpRequestPluginsToggle())
	})
	nui.Tooltip(c, theme, box, tooltip, nui.TooltipOptions{})
}

func (p *httpPane) primaryButton(c *ui.Context, theme nui.Theme) {
	label, tooltip, icon, color := locales.WorkspaceHttpRequestSend(), locales.WorkspaceHttpRequestSendRequest(), lucide.Send(), nui.Primary
	run := p.send
	var kbds []string
	switch {
	case p.state.IsStreaming:
		label, tooltip, icon, color, run = locales.WorkspaceHttpRequestStop(), locales.WorkspaceHttpRequestStopStream(), lucide.Square(), nui.Error, p.stop
	case p.state.IsSending:
		label, tooltip, icon, color, run = locales.WorkspaceHttpRequestStop(), locales.WorkspaceHttpRequestStopRequest(), lucide.Square(), nui.Error, p.stop
	default:
		kbds = p.editor.kbds(shortcuts.RequestEditorSend)
	}
	primaryAction(c, theme, label, tooltip, icon, color, kbds, run)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (p *httpPane) requestPanel(c *ui.Context, theme nui.Theme) {
	state := p.state
	ui.Column(c.Key("request-panel")).Fill().MinHeight(0).Children(func() {
		queryCount := logic.CountRequestQueryRows(state.Params)
		cookieCount := logic.CountRequestCookieRows(state.Headers)
		script := nui.TabItem{ID: logic.RequestTabScript, Label: locales.WorkspaceHttpRequestInlineScript()}
		if state.InlineScriptEnabled {
			script.Icon = lucide.CircleCheck()
		}
		items := []nui.TabItem{
			{ID: logic.RequestTabHeaders, Label: locales.WorkspaceHttpRequestRequestHeaders() + "(" + strconv.Itoa(enabledRowCount(state.Headers)) + ")"},
			{ID: logic.RequestTabQuery, Label: locales.WorkspaceHttpRequestQuery() + "(" + strconv.Itoa(queryCount) + ")"},
			{ID: logic.RequestTabCookies, Label: locales.WorkspaceHttpRequestCookies() + "(" + strconv.Itoa(cookieCount) + ")"},
			{ID: logic.RequestTabBody, Label: locales.WorkspaceHttpRequestRequestBody()},
			script,
			{ID: logic.RequestTabSettings, Label: locales.WorkspaceHttpRequestSettings()},
		}
		ui.ScrollHorizontal(c).FillWidth().Shrink(0).PaddingX(10).Scrollbars(false).Children(func() {
			nui.Tabs(c.Key("request-tabs"), theme, &state.ActiveRequestTab, items, nui.TabsOptions{Label: locales.WorkspaceHttpRequestRequestHeaders(), Content: nui.Set(false), Underline: true})
		})
		ui.Column(c.Key(state.ActiveRequestTab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch state.ActiveRequestTab {
			case logic.RequestTabQuery:
				p.editor.queryPane(c, theme, &p.query, &state.Params)
			case logic.RequestTabCookies:
				p.editor.cookiesPane(c, theme, &p.requestRows, &state.Headers)
			case logic.RequestTabBody:
				p.bodyPane(c, theme)
			case logic.RequestTabScript:
				p.scriptPane(c, theme)
			case logic.RequestTabSettings:
				p.settingsPane(c, theme)
			default:
				p.editor.headersPane(c, theme, &p.headers, &state.Headers, logic.RequestProtocol(state.Settings.Protocol))
			}
		})
	})
}

func wordWrapButton(c *ui.Context, theme nui.Theme, view *widgets.CodeView) {
	color := nui.Neutral
	if view.WordWrap() {
		color = nui.Primary
	}
	button := nui.Button(c.Key("wrap"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: color, Size: nui.SM, Square: true, Leading: lucide.TextWrap(), AccessibleLabel: locales.WorkspaceWordWrap()})
	button.OnClick(func() { view.SetWordWrap(!view.WordWrap()) })
	nui.Tooltip(c, theme, button, locales.WorkspaceWordWrap(), nui.TooltipOptions{})
}

func (p *httpPane) bodyPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	options := []nui.Option[string]{
		{Value: logic.BodyTypeNone, Label: locales.WorkspaceHttpRequestBodyTypeNone()},
		{Value: logic.BodyTypeJSON, Label: locales.WorkspaceHttpRequestBodyTypeJson()},
		{Value: logic.BodyTypeText, Label: locales.WorkspaceHttpRequestBodyTypeText()},
		{Value: logic.BodyTypeXML, Label: locales.WorkspaceHttpRequestBodyTypeXml()},
		{Value: logic.BodyTypeURLEncoded, Label: locales.WorkspaceHttpRequestBodyTypeUrlencoded()},
		{Value: logic.BodyTypeFormData, Label: locales.WorkspaceHttpRequestBodyTypeFormData()},
		{Value: logic.BodyTypeFile, Label: locales.WorkspaceHttpRequestBodyTypeFile()},
	}
	text := state.RequestBodyType == logic.BodyTypeJSON || state.RequestBodyType == logic.BodyTypeText || state.RequestBodyType == logic.BodyTypeXML
	ui.Row(c.Key("body-type")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(8, 10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, locales.WorkspaceHttpRequestBodyType()).FontSize(14).TextColor(theme.Muted)
			ui.Box(c).Width(150).Children(func() {
				nui.Select(c, theme, &state.RequestBodyType, options, nui.SelectOptions{Label: locales.WorkspaceHttpRequestBodyType(), Size: nui.SM})
			})
		})
		if text {
			wordWrapButton(c, theme, p.body)
		}
	})
	ui.Box(c.Key("body-content")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		switch {
		case text:
			p.body.Build(c, ui.Font{Family: theme.MonoFont, Size: 13}).Fill()
		case state.RequestBodyType == logic.BodyTypeFile:
			p.editor.fileControl(c, theme, p.key+":body", &state.RequestBodyFile, &p.bodyGuard)
		case state.RequestBodyType == logic.BodyTypeFormData:
			p.formData.build(c, theme, p.editor, p.key, &state.RequestBodyFormData)
		case state.RequestBodyType == logic.BodyTypeURLEncoded:
			p.urlEncodedPane(c, theme)
		}
	})
}

func (p *httpPane) urlEncodedPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, locales.WorkspaceHttpRequestBodyTypeUrlencoded(), func() {
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestFormDataClearAll(), false, func() {
				state.RequestBodyURLEncoded = []logic.EditableRow{{Enabled: true}}
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestFormDataAddRow(), false, func() {
				state.RequestBodyURLEncoded = append(state.RequestBodyURLEncoded, logic.EditableRow{Enabled: true})
			})
		})
		p.urlEncoded.build(c, theme, &state.RequestBodyURLEncoded, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestFormDataNamePlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestFormDataValuePlaceholder()})
	})
}

func (p *httpPane) scriptPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	ui.Row(c.Key("script-head")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(8, 10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, locales.WorkspaceHttpRequestInlineScriptEnable()).FontSize(14).TextColor(theme.Muted)
			nui.Switch(c.Key("script-enabled"), theme, &state.InlineScriptEnabled, nui.SwitchOptions{Disabled: p.busy(), Size: nui.SM}).Label(locales.WorkspaceHttpRequestInlineScriptEnable())
		})
		ui.Row(c).Gap(4).Children(func() {
			if p.editor.Host.ShowPythonHelp != nil {
				iconAction(c, theme, "help", lucide.BookOpen(), locales.PythonPluginHelpTitle(), false, p.editor.Host.ShowPythonHelp)
			}
			wordWrapButton(c, theme, p.script)
		})
	})
	if !p.editor.pythonEnabled() {
		ui.Box(c.Key("script-warning")).FillWidth().Shrink(0).Padding(0, 10, 8, 10).Children(func() {
			nui.Alert(c, theme, nui.AlertOptions{Description: locales.WorkspaceHttpRequestInlineScriptRuntimeDisabled(), Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()})
		})
	}
	ui.Box(c.Key("script-editor")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		p.script.Build(c, ui.Font{Family: theme.MonoFont, Size: 13}).Fill()
	})
}

func (p *httpPane) settingsPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	settings := &state.Settings
	protocols := []nui.Option[string]{
		{Value: string(logic.ProtocolAuto), Label: locales.WorkspaceHttpRequestProtocolAuto()},
		{Value: string(logic.ProtocolHTTP1), Label: "HTTP/1.1"},
		{Value: string(logic.ProtocolHTTP2), Label: "HTTP/2"},
	}
	ui.Scroll(c.Key("settings")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Padding(10).Children(func() {
			selectRow(c, theme, "protocol", locales.WorkspaceHttpRequestProtocol(), &settings.Protocol, protocols, func() {
				state.Headers = logic.ConvertRequestRouteHeaders(state.Headers, logic.RequestProtocol(settings.Protocol), state.URL)
			})
			selectRow(c, theme, "proxy", locales.WorkspaceHttpRequestProxyMode(), &settings.ProxyMode, proxyModeOptions(), nil)
			settingRow(c, theme, "skip-tls", locales.WorkspaceHttpRequestSkipVerifyTls(), func() {
				nui.Switch(c.Key("switch"), theme, &settings.SkipVerifyTLS, nui.SwitchOptions{Size: nui.SM}).Label(locales.WorkspaceHttpRequestSkipVerifyTls())
			})
			selectRow(c, theme, "tls", locales.WorkspaceHttpRequestTlsFingerprint(), &settings.TLSClientHelloID, tlsProfileOptions(), nil)
			settingRow(c, theme, "h2", locales.WorkspaceHttpRequestHttp2Fingerprint(), func() {
				nui.Input(c.Key("input"), theme, &settings.HTTP2Fingerprint, nui.InputOptions{Label: locales.WorkspaceHttpRequestHttp2Fingerprint(), Size: nui.SM, Disabled: settings.Protocol == string(logic.ProtocolHTTP1)}).FillWidth()
			})
			if settings.ProxyMode == logic.ProxyModeCustom {
				settingRow(c, theme, "custom", locales.WorkspaceHttpRequestCustomProxy(), func() {
					nui.Input(c.Key("input"), theme, &settings.CustomProxy, nui.InputOptions{Label: locales.WorkspaceHttpRequestCustomProxy(), Placeholder: locales.WorkspaceHttpRequestCustomProxyPlaceholder(), Size: nui.SM}).FillWidth()
				})
			}
			p.timeout.build(c, theme, "timeout", locales.WorkspaceHttpRequestRequestTimeout(), locales.WorkspaceHttpRequestRequestTimeoutPlaceholder(), &settings.TimeoutMs, 0, 100)
			redirects := int64(settings.MaxRedirects)
			p.redirects.build(c, theme, "redirects", locales.WorkspaceHttpRequestMaxRedirects(), "", &redirects, 0, 1)
			settings.MaxRedirects = logic.NormalizeHTTPMaxRedirects(float64(redirects))
		})
	})
}

// formDataView edits Form Data rows.
type formDataView struct {
	list   ui.ListState
	menus  map[string]*nui.MenuState
	guards map[string]int
}

func (v *formDataView) begin(id string) func() bool {
	if v.guards == nil {
		v.guards = map[string]int{}
	}
	v.guards[id]++
	seen := v.guards[id]
	return func() bool { return v.guards[id] == seen }
}

func (v *formDataView) menu(id string) *nui.MenuState {
	if v.menus == nil {
		v.menus = map[string]*nui.MenuState{}
	}
	if v.menus[id] == nil {
		v.menus[id] = &nui.MenuState{}
	}
	return v.menus[id]
}

func (v *formDataView) build(c *ui.Context, theme nui.Theme, e *Editor, key string, items *[]logic.FormDataItem) {
	if len(*items) == 0 {
		*items = []logic.FormDataItem{logic.NewFormDataRow()}
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, locales.WorkspaceHttpRequestBodyTypeFormData(), func() {
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestFormDataClearAll(), false, func() {
				v.guards = nil
				*items = []logic.FormDataItem{logic.NewFormDataRow()}
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestFormDataAddRow(), false, func() {
				*items = append(*items, logic.NewFormDataRow())
			})
		})
		ui.List(c.Key("form-rows"), &v.list, len(*items), func(index int) {
			if index < len(*items) {
				v.row(c, theme, e, key, items, index)
			}
		}).Fill().MinHeight(0)
	})
}

func (v *formDataView) row(c *ui.Context, theme nui.Theme, e *Editor, key string, items *[]logic.FormDataItem, index int) {
	item := &(*items)[index]
	id := item.ID
	number := strconv.Itoa(index + 1)
	setFile := func(file *logic.RequestFileValue) {
		for position := range *items {
			if (*items)[position].ID == id {
				(*items)[position].File, (*items)[position].ItemType, (*items)[position].Value = file, logic.FormDataItemFile, ""
			}
		}
	}
	line := ui.Row(c.Key(id)).FillWidth().Height(editableRowHeight).AlignItems(ui.Center).Gap(6).PaddingX(10)
	line.BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderMuted)
	if line.FileDragOver() {
		line.Background(theme.Accented).Border(1, theme.Primary)
	}
	if dropped := line.DroppedFiles(); len(dropped) > 0 {
		e.useDroppedFile(key+":"+id, dropped, v.begin(id), setFile)
	}
	line.Children(func() {
		nui.Checkbox(c.Key("enabled"), theme, &item.Enabled, nui.CheckboxOptions{}).Label(locales.WorkspaceHttpRequestFormDataColEnabled() + " " + number)
		ui.Box(c.Key("name")).Grow(2).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c, theme, &item.Name, nui.InputOptions{Label: locales.WorkspaceHttpRequestFormDataNamePlaceholder() + " " + number, Placeholder: locales.WorkspaceHttpRequestFormDataNamePlaceholder(), Size: nui.SM}).FillWidth()
		})
		ui.Box(c.Key("value")).Grow(3).Basis(0).MinWidth(0).Children(func() {
			if item.ItemType != logic.FormDataItemFile {
				nui.Input(c, theme, &item.Value, nui.InputOptions{Label: locales.WorkspaceHttpRequestFormDataValuePlaceholder() + " " + number, Placeholder: locales.WorkspaceHttpRequestFormDataValuePlaceholder(), Size: nui.SM}).FillWidth()
				return
			}
			name, color := locales.WorkspaceHttpRequestFormDataFilePlaceholder(), theme.Muted
			if item.File != nil {
				name, color = item.File.Name, theme.Text
			}
			chip := ui.Row(c.Key("file")).FillWidth().Height(28).AlignItems(ui.Center).Gap(6).PaddingX(8).Border(1, theme.Border).Radius(theme.Radius).Role(ui.RoleButton).Label(locales.WorkspaceHttpRequestFormDataRowMenuUpload() + " " + number)
			chip.OnClick(func() { e.pickFile(key+":"+id, v.begin(id), setFile) })
			chip.Children(func() {
				ui.Icon(c, lucide.FileText()).Size(14, 14).Shrink(0).TextColor(theme.Muted)
				ui.Text(c, name).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(14).TextColor(color)
			})
		})
		state := v.menu(id)
		anchor := nui.Button(c.Key("row-menu"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.EllipsisVertical(), AccessibleLabel: locales.WorkspaceHttpRequestFormDataRowMenuActions() + " " + number})
		anchor.OnClick(func() { state.Open = !state.Open })
		nui.DropdownMenu(c.Key("row-menu-items"), theme, anchor, state, []nui.MenuItem{
			{ID: "text", Label: locales.WorkspaceHttpRequestFormDataRowMenuText(), OnSelect: func() {
				v.begin(id)
				v.mutate(items, id, func(row *logic.FormDataItem) { row.ItemType, row.File = logic.FormDataItemText, nil })
			}},
			{ID: "file", Label: locales.WorkspaceHttpRequestFormDataRowMenuFile(), OnSelect: func() {
				v.mutate(items, id, func(row *logic.FormDataItem) { row.ItemType, row.Value = logic.FormDataItemFile, "" })
			}},
			{ID: "upload", Label: locales.WorkspaceHttpRequestFormDataRowMenuUpload(), OnSelect: func() { e.pickFile(key+":"+id, v.begin(id), setFile) }},
			{ID: "delete", Label: locales.WorkspaceHttpRequestFormDataRowMenuDelete(), OnSelect: func() {
				v.begin(id)
				if position := indexOfFormRow(*items, id); position >= 0 {
					if len(*items) <= 1 {
						*items = []logic.FormDataItem{logic.NewFormDataRow()}
						return
					}
					next := make([]logic.FormDataItem, 0, len(*items)-1)
					next = append(next, (*items)[:position]...)
					*items = append(next, (*items)[position+1:]...)
				}
			}},
		}, nui.MenuOptions{Label: locales.WorkspaceHttpRequestFormDataRowMenuActions()})
	})
}

func (v *formDataView) mutate(items *[]logic.FormDataItem, id string, change func(*logic.FormDataItem)) {
	if position := indexOfFormRow(*items, id); position >= 0 {
		change(&(*items)[position])
	}
}

func indexOfFormRow(items []logic.FormDataItem, id string) int {
	for position := range items {
		if items[position].ID == id {
			return position
		}
	}
	return -1
}
