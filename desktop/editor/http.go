package editor

import (
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
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
	formData  formDataView
	bodyGuard int

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
	return slices.Contains(values, value)
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
		widgets.TabStrip(c.Key("request-tabs"), theme, &state.ActiveRequestTab, items, widgets.TabStripOptions{Label: locales.WorkspaceHttpRequestRequestHeaders(), PaddingX: 10})
		ui.Column(c.Key(state.ActiveRequestTab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch state.ActiveRequestTab {
			case logic.RequestTabQuery:
				p.editor.queryPane(c, theme, &state.Params)
			case logic.RequestTabCookies:
				p.editor.cookiesPane(c, theme, &p.requestRows, &state.Headers)
			case logic.RequestTabBody:
				p.bodyPane(c, theme)
			case logic.RequestTabScript:
				p.scriptPane(c, theme)
			case logic.RequestTabSettings:
				p.settingsPane(c, theme)
			default:
				p.editor.headersPane(c, theme, &state.Headers, logic.RequestProtocol(state.Settings.Protocol))
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
		{Value: logic.BodyTypeFile, Label: locales.WorkspaceHttpRequestBodyTypeFile()},
		{Value: logic.BodyTypeFormData, Label: locales.WorkspaceHttpRequestBodyTypeFormData()},
		{Value: logic.BodyTypeURLEncoded, Label: locales.WorkspaceHttpRequestBodyTypeUrlencoded()},
	}
	text := state.RequestBodyType == logic.BodyTypeJSON || state.RequestBodyType == logic.BodyTypeText || state.RequestBodyType == logic.BodyTypeXML
	widgets.PaneToolbar(c.Key("body-type"), func() {
		paneSelect(c, theme, locales.WorkspaceHttpRequestBodyType(), &state.RequestBodyType, options)
	}, func() {
		switch {
		case text:
			wordWrapButton(c, theme, p.body)
		case state.RequestBodyType == logic.BodyTypeFormData:
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestFormDataClearAll(), false, func() {
				p.formData.guards = nil
				state.RequestBodyFormData = []logic.FormDataItem{logic.NewFormDataRow()}
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestFormDataAddRow(), false, func() {
				state.RequestBodyFormData = append(state.RequestBodyFormData, logic.NewFormDataRow())
			})
		case state.RequestBodyType == logic.BodyTypeURLEncoded:
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestUrlencodedClearAll(), false, func() {
				state.RequestBodyURLEncoded = []logic.EditableRow{{Enabled: true}}
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestUrlencodedAddRow(), false, func() {
				state.RequestBodyURLEncoded = append(state.RequestBodyURLEncoded, logic.EditableRow{Enabled: true})
			})
		}
	})
	ui.Column(c.Key(state.RequestBodyType)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		switch {
		case text:
			widgets.PaneContent(c, func() {
				p.body.Build(c, p.editor.Data.Settings.CodeFont())
			})
		case state.RequestBodyType == logic.BodyTypeFile:
			widgets.PaneContent(c, func() {
				p.editor.fileControl(c, theme, p.key+":body", &state.RequestBodyFile, &p.bodyGuard)
			})
		case state.RequestBodyType == logic.BodyTypeFormData:
			p.formData.build(c, theme, p.editor, p.key, &state.RequestBodyFormData)
		case state.RequestBodyType == logic.BodyTypeURLEncoded:
			editableRows(c, theme, &state.RequestBodyURLEncoded, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestUrlencodedNamePlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestUrlencodedValuePlaceholder()})
		}
	})
}

func (p *httpPane) scriptPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	label := locales.WorkspaceHttpRequestInlineScriptEnable()
	widgets.PaneToolbar(c.Key("script-head"), func() {
		ui.Text(c, label).Shrink(1).MinWidth(0).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		nui.Switch(c.Key("script-enabled"), theme, &state.InlineScriptEnabled, nui.SwitchOptions{Disabled: p.busy(), Size: nui.SM}).Label(label).Shrink(0)
	}, func() {
		if p.editor.Host.ShowPythonHelp != nil {
			iconAction(c, theme, "help", lucide.BookOpen(), locales.PythonPluginHelpTitle(), false, p.editor.Host.ShowPythonHelp)
		}
		wordWrapButton(c, theme, p.script)
	})
	widgets.PaneContent(c.Key("script-editor"), func() {
		p.script.Build(c, p.editor.Data.Settings.CodeFont())
	})
	if !p.editor.pythonEnabled() {
		widgets.PaneWarning(c.Key("script-warning"), theme, locales.WorkspaceHttpRequestInlineScriptRuntimeDisabled())
	}
}

func (p *httpPane) settingsPane(c *ui.Context, theme nui.Theme) {
	state := p.state
	settings := &state.Settings
	protocols := []nui.Option[string]{
		{Value: string(logic.ProtocolAuto), Label: locales.WorkspaceHttpRequestProtocolAuto()},
		{Value: string(logic.ProtocolHTTP1), Label: "HTTP/1.1"},
		{Value: string(logic.ProtocolHTTP2), Label: "HTTP/2"},
	}
	settingsContent(c, theme, locales.WorkspaceHttpRequestSettings(), func() {
		selectRow(c, theme, "protocol", locales.WorkspaceHttpRequestProtocol(), &settings.Protocol, protocols, func() {
			state.Headers = logic.ConvertRequestRouteHeaders(state.Headers, logic.RequestProtocol(settings.Protocol), state.URL)
		})
		proxySettings(c, theme, &settings.ProxyMode, &settings.CustomProxy)
		settingRow(c, theme, "skip-tls", locales.WorkspaceHttpRequestSkipVerifyTls(), func() {
			nui.Switch(c.Key("switch"), theme, &settings.SkipVerifyTLS, nui.SwitchOptions{Size: nui.SM}).Label(locales.WorkspaceHttpRequestSkipVerifyTls())
		})
		selectRow(c, theme, "tls", locales.WorkspaceHttpRequestTlsFingerprint(), &settings.TLSClientHelloID, tlsProfileOptions(), nil)
		settingRow(c, theme, "h2", locales.WorkspaceHttpRequestHttp2Fingerprint(), func() {
			nui.Input(c.Key("input"), theme, &settings.HTTP2Fingerprint, nui.InputOptions{Label: locales.WorkspaceHttpRequestHttp2Fingerprint(), Size: nui.SM, Disabled: settings.Protocol == string(logic.ProtocolHTTP1)}).FillWidth()
		})
		p.timeout.build(c, theme, "timeout", locales.WorkspaceHttpRequestRequestTimeout(), locales.WorkspaceHttpRequestRequestTimeoutPlaceholder(), &settings.TimeoutMs, 0, 100)
		redirects := int64(settings.MaxRedirects)
		p.redirects.build(c, theme, "redirects", locales.WorkspaceHttpRequestMaxRedirects(), "", &redirects, 0, 1)
		settings.MaxRedirects = logic.NormalizeHTTPMaxRedirects(float64(redirects))
	})
}

// formDataView edits Form Data rows.
type formDataView struct {
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
	number := func(index int) string { return strconv.Itoa(index + 1) }
	widgets.PaneContent(c.Key("grid"), func() {
		widgets.EditableGrid(c, theme, widgets.EditableGridOptions{
			Label: locales.WorkspaceHttpRequestBodyTypeFormData(), Rows: len(*items),
			RowKey:  func(i int) any { return (*items)[i].ID },
			Enabled: func(i int) *bool { return &(*items)[i].Enabled },
			Key:     func(i int) *string { return &(*items)[i].Name },
			Value: func(i int) *string {
				if (*items)[i].ItemType == logic.FormDataItemFile {
					return nil
				}
				return &(*items)[i].Value
			},
			EnabledLabel:     func(i int) string { return locales.WorkspaceHttpRequestFormDataColEnabled() + " " + number(i) },
			KeyLabel:         func(i int) string { return locales.WorkspaceHttpRequestFormDataNamePlaceholder() + " " + number(i) },
			ValueLabel:       func(i int) string { return locales.WorkspaceHttpRequestFormDataValuePlaceholder() + " " + number(i) },
			KeyPlaceholder:   locales.WorkspaceHttpRequestFormDataNamePlaceholder(),
			ValuePlaceholder: locales.WorkspaceHttpRequestFormDataValuePlaceholder(),
			Row: func(i int, row ui.Element) {
				id := (*items)[i].ID
				if row.FileDragOver() {
					row.Background(theme.Accented).Border(1, theme.Primary)
				}
				if dropped := row.DroppedFiles(); len(dropped) > 0 {
					e.useDroppedFile(key+":"+id, dropped, v.begin(id), v.setFile(items, id))
				}
			},
			ValueCell: func(i int) { v.fileCell(c, theme, e, key, items, i) },
			Actions:   func(i int) { v.rowMenu(c, theme, e, key, items, i) },
		})
	})
}

// setFile returns how the row id takes a chosen file.
func (v *formDataView) setFile(items *[]logic.FormDataItem, id string) func(*logic.RequestFileValue) {
	return func(file *logic.RequestFileValue) {
		v.mutate(items, id, func(row *logic.FormDataItem) {
			row.File, row.ItemType, row.Value = file, logic.FormDataItemFile, ""
		})
	}
}

// fileCell is a file row's value: a chip naming the file, which picks one
// when clicked.
func (v *formDataView) fileCell(c *ui.Context, theme nui.Theme, e *Editor, key string, items *[]logic.FormDataItem, index int) {
	item := (*items)[index]
	id := item.ID
	name, color := locales.WorkspaceHttpRequestFormDataFilePlaceholder(), theme.Muted
	if item.File != nil {
		name, color = item.File.Name, theme.Text
	}
	chip := ui.ButtonBase(c.Key("file")).FillWidth().Height(28).Justify(ui.Start).FocusRing(true).Gap(6).PaddingX(10).Border(1, theme.Border).Radius(999).Label(locales.WorkspaceHttpRequestFormDataRowMenuUpload() + " " + strconv.Itoa(index+1))
	chip.OnClick(func() { e.pickFile(key+":"+id, v.begin(id), v.setFile(items, id)) })
	chip.Children(func() {
		ui.Icon(c, lucide.File()).Size(16, 16).Shrink(0).TextColor(color)
		ui.Text(c, name).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(color)
	})
	if item.File != nil {
		nui.Tooltip(c, theme, chip, item.File.Path, nui.TooltipOptions{})
	}
}

// rowMenu is a row's actions: text or file, upload, and delete.
func (v *formDataView) rowMenu(c *ui.Context, theme nui.Theme, e *Editor, key string, items *[]logic.FormDataItem, index int) {
	id := (*items)[index].ID
	number := strconv.Itoa(index + 1)
	state := v.menu(id)
	anchor := nui.Button(c.Key("row-menu"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Ellipsis(), AccessibleLabel: locales.WorkspaceHttpRequestFormDataRowMenuActions() + " " + number})
	nui.DropdownMenu(c.Key("row-menu-items"), theme, anchor, state, []nui.MenuItem{
		{ID: "text", Label: locales.WorkspaceHttpRequestFormDataRowMenuText(), OnSelect: func() {
			v.begin(id)
			v.mutate(items, id, func(row *logic.FormDataItem) { row.ItemType, row.File = logic.FormDataItemText, nil })
		}},
		{ID: "file", Label: locales.WorkspaceHttpRequestFormDataRowMenuFile(), OnSelect: func() {
			v.mutate(items, id, func(row *logic.FormDataItem) { row.ItemType, row.Value = logic.FormDataItemFile, "" })
		}},
		{ID: "upload", Label: locales.WorkspaceHttpRequestFormDataRowMenuUpload(), OnSelect: func() { e.pickFile(key+":"+id, v.begin(id), v.setFile(items, id)) }},
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
