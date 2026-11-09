package inspect

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

type headersPaneOptions struct {
	title            string
	fields           []logic.HeaderField
	sort             *logic.HeaderSortOrder
	warning          string
	orderUnavailable bool
	jsonLabel        string
	sortLabel        string
	textLabel        string
	jsonCopied       string
	textCopied       string
	sorted           func(string) string
}

func nextHeaderSort(order logic.HeaderSortOrder) logic.HeaderSortOrder {
	switch order {
	case logic.HeaderSortDefault:
		return logic.HeaderSortAsc
	case logic.HeaderSortAsc:
		return logic.HeaderSortDesc
	default:
		return logic.HeaderSortDefault
	}
}

func headerSortLabel(order logic.HeaderSortOrder) string {
	switch order {
	case logic.HeaderSortAsc:
		return locales.DetailHeaderSortAsc()
	case logic.HeaderSortDesc:
		return locales.DetailHeaderSortDesc()
	default:
		return locales.DetailHeaderSortDefault()
	}
}

func iconButton(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, label string, disabled bool, run func()) ui.Element {
	button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled})
	if !disabled && run != nil {
		button.OnClick(run)
	}
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
	return button
}

func warningAlert(c *ui.Context, theme nui.Theme, message string) {
	if message == "" {
		return
	}
	ui.Box(c).FillWidth().Padding(0, 10, 8, 10).Children(func() {
		nui.Alert(c, theme, nui.AlertOptions{Description: message, Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()})
	})
}

func emptyState(c *ui.Context, theme nui.Theme, icon *ui.SVG, title string) {
	ui.Column(c).Fill().Center().Children(func() {
		nui.Empty(c, theme, nui.EmptyOptions{Icon: icon, Title: title, Variant: nui.Ghost})
	})
}

func paneTitle(c *ui.Context, theme nui.Theme, title string, actions func()) {
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(10, 10, 8, 10).Children(func() {
		ui.Text(c, title).FontSize(13).TextColor(theme.Muted)
		ui.Row(c).Gap(4).Children(actions)
	})
}

func fieldsTable(c *ui.Context, theme nui.Theme, fields []logic.HeaderField) {
	rows := make([]widgets.KeyValue, len(fields))
	for index, field := range fields {
		rows[index] = widgets.KeyValue{Key: field.Name, Value: field.Value}
	}
	ui.Scroll(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Box(c).FillWidth().Padding(0, 10, 10, 10).Children(func() { widgets.KeyValueTable(c, theme, rows) })
	})
}

func (p *Panel) headersPane(c *ui.Context, theme nui.Theme, options headersPaneOptions) {
	display := logic.SortHeaderFields(options.fields, *options.sort)
	ui.Column(c.Key(options.title)).Fill().MinHeight(0).Children(func() {
		paneTitle(c, theme, options.title, func() {
			iconButton(c, theme, "copy-json", lucide.Code(), options.jsonLabel, false, func() { p.copyText(logic.FormatHeaderFieldsAsJSON(display), options.jsonCopied) })
			iconButton(c, theme, "sort", lucide.ArrowUpDown(), options.sortLabel, false, func() {
				*options.sort = nextHeaderSort(*options.sort)
				p.deps.Notifier.Success(options.sorted(headerSortLabel(*options.sort)))
			})
			iconButton(c, theme, "copy-text", lucide.Copy(), options.textLabel, false, func() { p.copyText(logic.FormatHeaderFieldsAsText(display), options.textCopied) })
		})
		warningAlert(c, theme, options.warning)
		if options.orderUnavailable {
			warningAlert(c, theme, locales.DetailHeaderOrderUnavailable())
		}
		if len(display) == 0 {
			emptyState(c, theme, lucide.Rows3(), locales.DetailNoHeaders())
			return
		}
		fieldsTable(c, theme, display)
	})
}

func (p *Panel) queryPane(c *ui.Context, theme nui.Theme, query logic.ParsedURLQuery) {
	fields := make([]logic.HeaderField, len(query.Fields))
	for index, field := range query.Fields {
		fields[index] = logic.HeaderField{Name: field.Name, Value: field.Value}
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneTitle(c, theme, locales.DetailQueryList(), func() {
			iconButton(c, theme, "copy-query", lucide.Copy(), locales.DetailCopyQuery(), false, func() { p.copyText(query.RawQuery, locales.DetailQueryCopied()) })
		})
		if len(fields) == 0 {
			emptyState(c, theme, lucide.ListFilter(), locales.DetailNoQueryParameters())
			return
		}
		fieldsTable(c, theme, fields)
	})
}

func (p *Panel) requestCookiesPane(c *ui.Context, theme nui.Theme, headers []logic.HeaderField) {
	cookies := logic.HeadersRecordToFields(logic.RequestCookiesRecord(headers))
	raw := logic.FormatHeaderFieldsAsText(logic.CookieHeaderFields(headers, "cookie"))
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneTitle(c, theme, locales.DetailCookieList(), func() {
			iconButton(c, theme, "copy-cookies", lucide.Copy(), locales.DetailCopyCookieHeader(), false, func() { p.copyText(raw, locales.DetailCookieHeaderCopied()) })
		})
		if len(cookies) == 0 {
			emptyState(c, theme, lucide.Cookie(), locales.DetailNoCookies())
			return
		}
		fieldsTable(c, theme, cookies)
	})
}

func (p *Panel) responseCookiesPane(c *ui.Context, theme nui.Theme, headers []logic.HeaderField, warning string) {
	cookies := logic.ParseResponseCookies(headers)
	raw := logic.FormatHeaderFieldsAsText(logic.CookieHeaderFields(headers, "set-cookie"))
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneTitle(c, theme, locales.DetailCookieList(), func() {
			iconButton(c, theme, "copy-cookies", lucide.Copy(), locales.DetailCopySetCookieHeader(), false, func() { p.copyText(raw, locales.DetailSetCookieHeaderCopied()) })
		})
		warningAlert(c, theme, warning)
		if len(cookies) == 0 {
			emptyState(c, theme, lucide.Cookie(), locales.DetailNoCookies())
			return
		}
		ui.Scroll(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 10, 10, 10).Gap(8).Children(func() {
				for index := range cookies {
					p.responseCookie(c, theme, index, cookies[index])
				}
			})
		})
	})
}

func (p *Panel) cookieOpen(index int) *bool {
	open, ok := p.cookiePopovers[index]
	if !ok {
		open = new(bool)
		p.cookiePopovers[index] = open
	}
	return open
}

func (p *Panel) responseCookie(c *ui.Context, theme nui.Theme, index int, cookie logic.ResponseCookie) {
	name := cookie.Name
	if name == "" {
		name = locales.DetailCookieUnnamed()
	}
	ui.Column(c.Key(index)).FillWidth().Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Padding(6, 8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(13).Bold().Selectable().Shrink(0).MaxWidth(240)
			ui.Text(c, cookie.Value).FontSize(13).Selectable().Grow(1).Basis(0).MinWidth(0)
			count := strconv.Itoa(len(cookie.Attributes))
			open := p.cookieOpen(index)
			label := locales.DetailCookieViewAttributes(count, name)
			anchor := nui.Button(c.Key("attributes"), theme, count, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Trailing: lucide.ChevronDown(), AccessibleLabel: label})
			anchor.OnClick(func() { *open = !*open })
			nui.Tooltip(c, theme, anchor, locales.DetailCookieAttributes(), nui.TooltipOptions{})
			nui.Popover(c.Key("popover"), theme, anchor, open, nui.PopoverOptions{Label: label, Width: nui.Set[float32](384)}, func(palette nui.Theme) {
				ui.Text(c, name).FontSize(14).Bold().TextColor(palette.Highlighted)
				ui.Text(c, locales.DetailCookieAttributeCount(count)).FontSize(12).TextColor(palette.Muted)
				for position, attribute := range cookie.Attributes {
					ui.Row(c.Key(position)).FillWidth().Gap(8).Children(func() {
						ui.Text(c, attribute.Name).FontSize(13).Bold().Width(120).Shrink(0).Selectable()
						ui.Text(c, attribute.Value).FontSize(13).Grow(1).Basis(0).MinWidth(0).Selectable()
					})
				}
				nui.Button(c.Key("copy-header"), palette, locales.DetailCopySetCookieHeader(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM}).
					OnClick(func() {
						p.copyText(logic.FormatHeaderFieldsAsText([]logic.HeaderField{cookie.Header}), locales.DetailSetCookieHeaderCopied())
					})
			})
		})
	})
}

// CookieList shows the cookies of Set-Cookie headers.
type CookieList struct{ panel Panel }

// NewCookieList returns an empty list.
func NewCookieList(deps Dependencies) *CookieList {
	return &CookieList{panel: Panel{deps: deps, cookiePopovers: map[int]*bool{}}}
}

// Build shows the cookies in headers.
func (l *CookieList) Build(c *ui.Context, theme nui.Theme, headers []logic.HeaderField, warning string) {
	if l.panel.clipboard != nil {
		c.WriteClipboard(*l.panel.clipboard)
		l.panel.clipboard = nil
	}
	l.panel.responseCookiesPane(c, theme, headers, warning)
}
