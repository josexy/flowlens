package editor

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type rowsOptions struct {
	KeyPlaceholder, ValuePlaceholder string
	ValidateHeaders                  bool
	Protocol                         logic.RequestProtocol
}

// editableRows edits an ordered list of name and value rows in a grid, as
// 1.0.5's EditableKeyValueTable.
func editableRows(c *ui.Context, theme nui.Theme, rows *[]logic.EditableRow, options rowsOptions) {
	if len(*rows) == 0 {
		*rows = []logic.EditableRow{{Enabled: true}}
	}
	if options.KeyPlaceholder == "" {
		options.KeyPlaceholder = locales.WorkspaceHttpRequestHeaderKeyPlaceholder()
	}
	if options.ValuePlaceholder == "" {
		options.ValuePlaceholder = locales.WorkspaceHttpRequestHeaderValuePlaceholder()
	}
	number := func(index int) string { return strconv.Itoa(index + 1) }
	invalidName := func(index int) string {
		row := (*rows)[index]
		name := strings.TrimSpace(row.Key)
		if options.ValidateHeaders && row.Enabled && name != "" && !logic.IsValidRequestHeaderName(name, options.Protocol) {
			return name
		}
		return ""
	}
	widgets.PaneContent(c.Key("grid"), func() {
		widgets.EditableGrid(c, theme, widgets.EditableGridOptions{
			Label: options.KeyPlaceholder, Rows: len(*rows), Resizable: true,
			Enabled: func(i int) *bool { return &(*rows)[i].Enabled },
			Key:     func(i int) *string { return &(*rows)[i].Key },
			Value:   func(i int) *string { return &(*rows)[i].Value },
			EnabledLabel: func(i int) string {
				return options.KeyPlaceholder + " " + number(i) + " " + locales.WorkspaceHttpRequestFormDataColEnabled()
			},
			KeyLabel:         func(i int) string { return options.KeyPlaceholder + " " + number(i) },
			ValueLabel:       func(i int) string { return options.ValuePlaceholder + " " + number(i) },
			KeyPlaceholder:   options.KeyPlaceholder,
			ValuePlaceholder: options.ValuePlaceholder,
			KeyTrailing: func(i int) {
				if name := invalidName(i); name != "" {
					icon := ui.Icon(c.Key("invalid"), lucide.CircleX()).Size(16, 16).Shrink(0).TextColor(theme.Error).Label(locales.WorkspaceHttpRequestHeaderInvalidName(name))
					nui.Tooltip(c, theme, icon, locales.WorkspaceHttpRequestHeaderInvalidName(name), nui.TooltipOptions{})
				}
			},
			Actions: func(i int) {
				nui.Button(c.Key("remove"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.CircleMinus(), AccessibleLabel: locales.WorkspaceHttpRequestFormDataRowMenuDelete() + " " + number(i)}).
					OnClick(func() { *rows = removeEditableRow(*rows, i) })
			},
		})
	})
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

func paneSelect(c *ui.Context, theme nui.Theme, label string, value *string, options []nui.Option[string]) {
	ui.Text(c, label).Shrink(1).MinWidth(0).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
	nui.Select(c, theme, value, options, nui.SelectOptions{Label: label, Size: nui.XS}).Width(150).Shrink(1).MinWidth(0)
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
	lastURL, lastParams string
	cookieRows          []logic.EditableRow
	cookieSignature     string
	cookieRowsSeen      string
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

func (e *Editor) headersPane(c *ui.Context, theme nui.Theme, headers *[]logic.EditableRow, protocol logic.RequestProtocol) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.WorkspaceHttpRequestRequestHeaderList(), func() {
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
		editableRows(c, theme, headers, rowsOptions{ValidateHeaders: true, Protocol: protocol})
	})
}

func (e *Editor) queryPane(c *ui.Context, theme nui.Theme, params *[]logic.EditableRow) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.WorkspaceHttpRequestQueryList(), func() {
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
		editableRows(c, theme, params, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestQueryKeyPlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestQueryValuePlaceholder()})
	})
}

func (e *Editor) cookiesPane(c *ui.Context, theme nui.Theme, shared *requestRows, headers *[]logic.EditableRow) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.WorkspaceHttpRequestCookieList(), func() {
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
		editableRows(c, theme, &shared.cookieRows, rowsOptions{KeyPlaceholder: locales.WorkspaceHttpRequestCookieNamePlaceholder(), ValuePlaceholder: locales.WorkspaceHttpRequestCookieValuePlaceholder()})
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
			ui.Text(c, locales.WorkspaceHttpRequestFileDropHint()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			choose := nui.Button(c.Key("choose"), theme, locales.WorkspaceHttpRequestFileChoose(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() { e.pickFile(job, begin(), apply) })
			nui.Tooltip(c, theme, choose, locales.WorkspaceHttpRequestFileChoose(), nui.TooltipOptions{})
			return
		}
		chosen := *file
		ui.Icon(c, lucide.FileText()).Size(28, 28).TextColor(theme.Primary)
		ui.Text(c, chosen.Name).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Text).Selectable()
		ui.Text(c, locales.WorkspaceHttpRequestFilePath()+": "+chosen.Path).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).Selectable()
		ui.Text(c, locales.WorkspaceHttpRequestFileSize()+": "+logic.FormatFileSize(chosen.Size)).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		ui.Row(c).Gap(8).Children(func() {
			choose := nui.Button(c.Key("choose"), theme, locales.WorkspaceHttpRequestFileChoose(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() { e.pickFile(job, begin(), apply) })
			nui.Tooltip(c, theme, choose, locales.WorkspaceHttpRequestFileChoose(), nui.TooltipOptions{})
			nui.Button(c.Key("clear"), theme, locales.WorkspaceHttpRequestFileClear(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM}).
				OnClick(func() {
					*guard++
					*file = nil
				})
		})
	})
}
