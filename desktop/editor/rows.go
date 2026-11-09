package editor

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	editableRowHeight  float32 = 36
	minKeyColumnWidth  float32 = 140
	minValueColumnSize float32 = 252
)

type rowsOptions struct {
	KeyPlaceholder, ValuePlaceholder string
	ValidateHeaders                  bool
	Protocol                         logic.RequestProtocol
}

// rowsView edits an ordered list of name and value rows.
type rowsView struct {
	list     ui.ListState
	keyWidth float32
	width    float32
}

func (v *rowsView) build(c *ui.Context, theme nui.Theme, rows *[]logic.EditableRow, options rowsOptions) {
	if len(*rows) == 0 {
		*rows = []logic.EditableRow{{Enabled: true}}
	}
	if options.KeyPlaceholder == "" {
		options.KeyPlaceholder = locales.WorkspaceHttpRequestHeaderKeyPlaceholder()
	}
	if options.ValuePlaceholder == "" {
		options.ValuePlaceholder = locales.WorkspaceHttpRequestHeaderValuePlaceholder()
	}
	element := ui.List(c.Key("rows"), &v.list, len(*rows), func(index int) {
		if index < len(*rows) {
			v.row(c, theme, rows, index, options)
		}
	}).Fill().MinHeight(0)
	if bounds := element.Bounds(); bounds.W > 0 {
		v.width = bounds.W
	}
}

func (v *rowsView) row(c *ui.Context, theme nui.Theme, rows *[]logic.EditableRow, index int, options rowsOptions) {
	row := &(*rows)[index]
	number := strconv.Itoa(index + 1)
	line := ui.Row(c.Key(index)).FillWidth().Height(editableRowHeight).AlignItems(ui.Center).Gap(6).PaddingX(10)
	line.BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderMuted)
	line.Children(func() {
		nui.Checkbox(c.Key("enabled"), theme, &row.Enabled, nui.CheckboxOptions{}).Label(options.KeyPlaceholder + " " + number + " enabled")
		key := ui.Row(c.Key("key-cell")).AlignItems(ui.Center).Gap(4).MinWidth(0)
		if v.keyWidth > 0 {
			key.Width(v.keyWidth).Shrink(0)
		} else {
			key.Grow(2).Basis(0)
		}
		key.Children(func() {
			name := strings.TrimSpace(row.Key)
			invalid := options.ValidateHeaders && row.Enabled && name != "" && !logic.IsValidRequestHeaderName(name, options.Protocol)
			nui.Input(c.Key("key"), theme, &row.Key, nui.InputOptions{Label: options.KeyPlaceholder + " " + number, Placeholder: options.KeyPlaceholder, Size: nui.SM, Invalid: invalid}).Grow(1).Basis(0).MinWidth(0)
			if invalid {
				icon := ui.Icon(c.Key("invalid"), lucide.CircleX()).Size(14, 14).Shrink(0).TextColor(theme.Error)
				nui.Tooltip(c, theme, icon, locales.WorkspaceHttpRequestHeaderInvalidName(name), nui.TooltipOptions{})
			}
		})
		v.resizeHandle(c, theme, key)
		ui.Box(c.Key("value-cell")).Grow(3).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c.Key("value"), theme, &row.Value, nui.InputOptions{Label: options.ValuePlaceholder + " " + number, Placeholder: options.ValuePlaceholder, Size: nui.SM}).FillWidth()
		})
		nui.Button(c.Key("remove"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.CircleMinus(), AccessibleLabel: locales.WorkspaceHttpRequestFormDataRowMenuDelete() + " " + number}).
			OnClick(func() { *rows = removeEditableRow(*rows, index) })
	})
}

func (v *rowsView) resizeHandle(c *ui.Context, theme nui.Theme, key ui.Element) {
	handle := ui.Box(c.Key("resize")).Width(4).Height(22).Shrink(0).Cursor(ui.CursorResizeEW)
	if handle.Hovered() {
		handle.Background(theme.Accented)
	}
	if dx, _, moved := handle.Dragged(); moved {
		if v.keyWidth == 0 {
			v.keyWidth = key.Bounds().W
		}
		limit := max(minKeyColumnWidth, v.width-minValueColumnSize)
		v.keyWidth = min(limit, max(minKeyColumnWidth, v.keyWidth+dx))
	}
}

func removeEditableRow(rows []logic.EditableRow, index int) []logic.EditableRow {
	if len(rows) <= 1 {
		return []logic.EditableRow{{Enabled: true}}
	}
	next := make([]logic.EditableRow, 0, len(rows)-1)
	next = append(next, rows[:index]...)
	return append(next, rows[index+1:]...)
}

func enabledRowCount(rows []logic.EditableRow) int {
	count := 0
	for _, row := range rows {
		if row.Enabled {
			count++
		}
	}
	return count
}

func serializeRows(rows []logic.EditableRow) string {
	var text strings.Builder
	for _, row := range rows {
		text.WriteString(strconv.FormatBool(row.Enabled))
		text.WriteByte(0)
		text.WriteString(row.Key)
		text.WriteByte(0)
		text.WriteString(row.Value)
		text.WriteByte(0)
	}
	return text.String()
}

func paneHeader(c *ui.Context, theme nui.Theme, title string, actions func()) {
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(8, 10).Children(func() {
		ui.Text(c, title).FontSize(13).TextColor(theme.Muted)
		ui.Row(c).Gap(4).Children(actions)
	})
}

func iconAction(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, label string, disabled bool, run func()) ui.Element {
	button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled})
	if !disabled && run != nil {
		button.OnClick(run)
	}
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
	return button
}

// requestRows is the state shared by editors with query, cookie, and header
// tables.
type requestRows struct {
	headers, query, cookies rowsView
	lastURL, lastParams     string
	cookieRows              []logic.EditableRow
	cookieSignature         string
	cookieRowsSeen          string
}

func hasURLQuery(raw string) bool {
	_, query, found := strings.Cut(strings.SplitN(raw, "#", 2)[0], "?")
	return found && query != ""
}

func (r *requestRows) initQuery(url *string, params *[]logic.EditableRow) {
	if hasURLQuery(*url) {
		if next := logic.ParseRequestQueryRows(*url); logic.SerializeRequestQueryRows(next) != logic.SerializeRequestQueryRows(*params) {
			*params = next
		}
	} else if next := logic.ReplaceRequestURLQuery(*url, *params); next != *url {
		*url = next
	}
	r.lastURL, r.lastParams = *url, serializeRows(*params)
}

// syncQuery keeps the URL and the query rows equal, whichever was edited.
func (r *requestRows) syncQuery(url *string, params *[]logic.EditableRow) {
	if *url != r.lastURL {
		if next := logic.ParseRequestQueryRows(*url); logic.SerializeRequestQueryRows(next) != logic.SerializeRequestQueryRows(*params) {
			*params = next
		}
		r.lastURL, r.lastParams = *url, serializeRows(*params)
		return
	}
	if seen := serializeRows(*params); seen != r.lastParams {
		r.lastParams = seen
		*url = logic.ReplaceRequestURLQuery(*url, *params)
		r.lastURL = *url
	}
}

// syncCookies keeps the cookie rows and the Cookie headers equal.
func (r *requestRows) syncCookies(headers *[]logic.EditableRow) {
	signature := serializeRows(logic.RequestCookieRows(*headers))
	if signature != r.cookieSignature || r.cookieRows == nil {
		r.cookieRows = logic.RequestCookieRows(*headers)
		if len(r.cookieRows) == 0 {
			r.cookieRows = []logic.EditableRow{{Enabled: true}}
		}
		r.cookieSignature, r.cookieRowsSeen = signature, serializeRows(r.cookieRows)
		return
	}
	if seen := serializeRows(r.cookieRows); seen != r.cookieRowsSeen {
		r.cookieRowsSeen = seen
		*headers = logic.ReplaceRequestCookieHeaders(*headers, r.cookieRows)
		r.cookieSignature = serializeRows(logic.RequestCookieRows(*headers))
	}
}

func (e *Editor) headersPane(c *ui.Context, theme nui.Theme, view *rowsView, headers *[]logic.EditableRow, protocol logic.RequestProtocol) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, locales.WorkspaceHttpRequestRequestHeaderList(), func() {
			iconAction(c, theme, "copy-json", lucide.Code(), locales.WorkspaceHttpRequestCopyRequestHeadersJson(), false, func() {
				e.copy(logic.FormatHeaderFieldsAsJSON(logic.EditableRowsToHeaderFields(*headers)), locales.WorkspaceHttpRequestRequestHeadersCopiedJson())
			})
			iconAction(c, theme, "copy-text", lucide.Copy(), locales.WorkspaceHttpRequestCopyRequestHeadersText(), false, func() {
				e.copy(logic.FormatHeaderFieldsAsText(logic.EditableRowsToHeaderFields(*headers)), locales.WorkspaceHttpRequestRequestHeadersCopiedText())
			})
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestClearRequestHeaders(), false, func() {
				*headers = []logic.EditableRow{{Enabled: true}}
				e.Notifier.Success(locales.WorkspaceHttpRequestRequestHeadersCleared())
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestAddRequestHeader(), false, func() {
				*headers = append(*headers, logic.EditableRow{Enabled: true})
			})
		})
		view.build(c, theme, headers, rowsOptions{ValidateHeaders: true, Protocol: protocol})
	})
}

func (e *Editor) queryPane(c *ui.Context, theme nui.Theme, view *rowsView, params *[]logic.EditableRow) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, locales.WorkspaceHttpRequestQueryList(), func() {
			iconAction(c, theme, "copy", lucide.Copy(), locales.WorkspaceHttpRequestCopyQuery(), false, func() {
				e.copy(logic.SerializeRequestQueryRows(*params), locales.WorkspaceHttpRequestQueryCopied())
			})
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestClearQueryParameters(), false, func() {
				*params = []logic.EditableRow{{Enabled: true}}
				e.Notifier.Success(locales.WorkspaceHttpRequestQueryParametersCleared())
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestAddQueryParameter(), false, func() {
				*params = append(*params, logic.EditableRow{Enabled: true})
			})
		})
		view.build(c, theme, params, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestQueryKeyPlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestQueryValuePlaceholder()})
	})
}

func (e *Editor) cookiesPane(c *ui.Context, theme nui.Theme, shared *requestRows, headers *[]logic.EditableRow) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, locales.WorkspaceHttpRequestCookieList(), func() {
			iconAction(c, theme, "copy", lucide.Copy(), locales.DetailCopyCookieHeader(), false, func() {
				e.copy(logic.FormatHeaderFieldsAsText(logic.HeadersRecordToFields(logic.RequestCookieHeadersRecord(*headers))), locales.DetailCookieHeaderCopied())
			})
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestClearCookies(), false, func() {
				shared.cookieRows = []logic.EditableRow{{Enabled: true}}
				e.Notifier.Success(locales.WorkspaceHttpRequestCookiesCleared())
			})
			iconAction(c, theme, "add", lucide.Plus(), locales.WorkspaceHttpRequestAddCookie(), false, func() {
				shared.cookieRows = append(shared.cookieRows, logic.EditableRow{Enabled: true})
			})
		})
		shared.cookies.build(c, theme, &shared.cookieRows, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestCookieNamePlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestCookieValuePlaceholder()})
	})
}

// fileControl chooses one file by picking or dropping.
func (e *Editor) fileControl(c *ui.Context, theme nui.Theme, job string, file **logic.RequestFileValue, guard *int) {
	apply := func(value *logic.RequestFileValue) { *file = value }
	begin := func() func() bool {
		*guard++
		seen := *guard
		return func() bool { return *guard == seen }
	}
	area := ui.Column(c.Key("file-control")).Fill().Center().Gap(10).Padding(16)
	dragOver := area.FileDragOver()
	if dragOver {
		area.Background(theme.Accented).Border(2, theme.Primary).Radius(theme.Radius)
	}
	if dropped := area.DroppedFiles(); len(dropped) > 0 {
		e.useDroppedFile(job, dropped, begin(), apply)
	}
	area.Children(func() {
		if *file == nil {
			ui.Icon(c, lucide.FileText()).Size(28, 28).TextColor(theme.Muted)
			ui.Text(c, locales.WorkspaceHttpRequestFileDropHint()).FontSize(13).TextColor(theme.Muted)
			nui.Button(c.Key("choose"), theme, locales.WorkspaceHttpRequestFileChoose(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() { e.pickFile(job, begin(), apply) })
			return
		}
		chosen := *file
		ui.Icon(c, lucide.FileText()).Size(28, 28).TextColor(theme.Primary)
		ui.Text(c, chosen.Name).FontSize(14).Bold().TextColor(theme.Text).Selectable()
		ui.Text(c, locales.WorkspaceHttpRequestFilePath()+": "+chosen.Path).FontSize(12).TextColor(theme.Muted).Selectable()
		ui.Text(c, locales.WorkspaceHttpRequestFileSize()+": "+logic.FormatFileSize(chosen.Size)).FontSize(12).TextColor(theme.Muted)
		ui.Row(c).Gap(8).Children(func() {
			nui.Button(c.Key("choose"), theme, locales.WorkspaceHttpRequestFileChoose(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() { e.pickFile(job, begin(), apply) })
			nui.Button(c.Key("clear"), theme, locales.WorkspaceHttpRequestFileClear(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() {
					*guard++
					*file = nil
				})
		})
	})
}
