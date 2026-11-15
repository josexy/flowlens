package inspect

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
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

func emptyState(c *ui.Context, theme nui.Theme, icon *ui.SVG, title string) {
	ui.Column(c).Fill().Center().Children(func() {
		widgets.EmptyState(c, theme, nui.EmptyOptions{Icon: icon, Title: title})
	})
}

// fieldsTable shows fields in a compact grid, as 1.0.5's HeadersTable.
// trailing, when set, builds a control after a row's value.
func fieldsTable(c *ui.Context, theme nui.Theme, label string, fields []logic.HeaderField, trailing func(row int)) {
	rows := make([]widgets.KeyValue, len(fields))
	for index, field := range fields {
		rows[index] = widgets.KeyValue{Key: field.Name, Value: field.Value}
	}
	widgets.PaneContent(c.Key("fields"), func() {
		widgets.FieldGrid(c, theme, rows, widgets.FieldGridOptions{Label: label, ValueTrailing: trailing})
	})
}

func (p *Panel) headersPane(c *ui.Context, theme nui.Theme, options headersPaneOptions) {
	display := logic.SortHeaderFields(options.fields, *options.sort)
	ui.Column(c.Key(options.title)).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, options.title, func() {
			iconButton(c, theme, "copy-json", lucide.Code(), options.jsonLabel, false, func() { p.copyText(logic.FormatHeaderFieldsAsJSON(display), options.jsonCopied) })
			iconButton(c, theme, "sort", lucide.ArrowUpDown(), options.sortLabel, false, func() {
				*options.sort = nextHeaderSort(*options.sort)
				p.deps.Notifier.Success(options.sorted(headerSortLabel(*options.sort)))
			})
			iconButton(c, theme, "copy-text", lucide.Copy(), options.textLabel, false, func() { p.copyText(logic.FormatHeaderFieldsAsText(display), options.textCopied) })
		})
		if len(display) == 0 {
			widgets.PaneContent(c, func() { emptyState(c, theme, lucide.Rows3(), locales.DetailNoHeaders()) })
		} else {
			fieldsTable(c, theme, options.title, display, nil)
		}
		widgets.PaneWarning(c, theme, options.warning)
		if options.orderUnavailable {
			widgets.PaneWarning(c, theme, locales.DetailHeaderOrderUnavailable())
		}
	})
}

func (p *Panel) queryPane(c *ui.Context, theme nui.Theme, query logic.ParsedURLQuery) {
	fields := make([]logic.HeaderField, len(query.Fields))
	for index, field := range query.Fields {
		fields[index] = logic.HeaderField{Name: field.Name, Value: field.Value}
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.DetailQueryList(), func() {
			iconButton(c, theme, "copy-query", lucide.Copy(), locales.DetailCopyQuery(), false, func() { p.copyText(query.RawQuery, locales.DetailQueryCopied()) })
		})
		if len(fields) == 0 {
			emptyState(c, theme, lucide.ListFilter(), locales.DetailNoQueryParameters())
			return
		}
		fieldsTable(c, theme, locales.DetailQueryList(), fields, nil)
	})
}

func (p *Panel) requestCookiesPane(c *ui.Context, theme nui.Theme, headers []logic.HeaderField) {
	cookies := logic.HeadersRecordToFields(logic.RequestCookiesRecord(headers))
	raw := logic.FormatHeaderFieldsAsText(logic.CookieHeaderFields(headers, "cookie"))
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.DetailCookieList(), func() {
			iconButton(c, theme, "copy-cookies", lucide.Copy(), locales.DetailCopyCookieHeader(), false, func() { p.copyText(raw, locales.DetailCookieHeaderCopied()) })
		})
		if len(cookies) == 0 {
			emptyState(c, theme, lucide.Cookie(), locales.DetailNoCookies())
			return
		}
		fieldsTable(c, theme, locales.DetailCookieList(), cookies, nil)
	})
}

func (p *Panel) responseCookiesPane(c *ui.Context, theme nui.Theme, headers []logic.HeaderField, warning string) {
	cookies := logic.ParseResponseCookies(headers)
	raw := logic.FormatHeaderFieldsAsText(logic.CookieHeaderFields(headers, "set-cookie"))
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneTitle(c, theme, locales.DetailCookieList(), func() {
			iconButton(c, theme, "copy-cookies", lucide.Copy(), locales.DetailCopySetCookieHeader(), false, func() { p.copyText(raw, locales.DetailSetCookieHeaderCopied()) })
		})
		if len(cookies) == 0 {
			widgets.PaneContent(c, func() { emptyState(c, theme, lucide.Cookie(), locales.DetailNoCookies()) })
		} else {
			fields := make([]logic.HeaderField, len(cookies))
			for index, cookie := range cookies {
				fields[index] = logic.HeaderField{Name: cookie.Name, Value: cookie.Value}
			}
			fieldsTable(c, theme, locales.DetailCookieList(), fields, func(index int) {
				p.cookieAttributes(c, theme, index, cookies[index])
			})
		}
		widgets.PaneWarning(c, theme, warning)
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

func cookieName(name string) string {
	if name == "" {
		return locales.DetailCookieUnnamed()
	}
	return name
}

// cookieAttributes is a cookie's attributes button and popover, as 1.0.5's
// ResponseCookieTable: the name and count, a grid of attributes, and the raw
// Set-Cookie line with a copy button.
func (p *Panel) cookieAttributes(c *ui.Context, theme nui.Theme, index int, cookie logic.ResponseCookie) {
	name := cookieName(cookie.Name)
	count := strconv.Itoa(len(cookie.Attributes))
	open := p.cookieOpen(index)
	label := locales.DetailCookieViewAttributes(count, name)
	chevron := lucide.ChevronDown()
	if *open {
		chevron = lucide.ChevronUp()
	}
	anchor := nui.Button(c.Key("attributes"), theme, count, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Trailing: chevron, AccessibleLabel: label})
	anchor.FontFeatures("tnum").OnClick(func() { *open = !*open })
	nui.Tooltip(c, theme, anchor, locales.DetailCookieAttributes(), nui.TooltipOptions{})
	nui.Popover(c.Key("popover"), theme, anchor, open, nui.PopoverOptions{Label: label, Placement: nui.BottomEnd, Width: nui.Set[float32](384), MaxHeight: nui.Set[float32](420)}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Padding(10, 12).BorderWidth(0, 0, 1, 0).BorderColor(palette.Border).Children(func() {
				ui.Text(c, name).FontSize(apptheme.TextSize(c, 14)).FontWeight(500).TextColor(palette.Text).Grow(1).Basis(0).MinWidth(0).Selectable()
				ui.Text(c, locales.DetailCookieAttributeCount(count)).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted).Shrink(0)
			})
			ui.Scroll(c.Key("body")).FillWidth().MaxHeight(320).Children(func() {
				ui.Column(c).FillWidth().Padding(12).Children(func() {
					cookieAttributeGrid(c, palette, cookie.Attributes)
					ui.Column(c.Key("raw")).FillWidth().MarginY(0).Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(palette.Border).Gap(4).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, locales.DetailCookieRawHeader()).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
							iconButton(c, palette, "copy-header", lucide.Copy(), locales.DetailCopySetCookieHeader(), false, func() {
								p.copyText(logic.FormatHeaderFieldsAsText([]logic.HeaderField{cookie.Header}), locales.DetailSetCookieHeaderCopied())
							})
						})
						ui.Text(c, logic.FormatHeaderFieldsAsText([]logic.HeaderField{cookie.Header})).FillWidth().FontSize(apptheme.TextSize(c, 12)).Font(palette.MonoFont).TextColor(palette.Text).Selectable()
					})
				})
			})
		})
	})
}

// How a cookie attribute's value is shown.
type attributeShown int

const (
	attributeValue attributeShown = iota
	attributeEmpty                // "Name=" with nothing after it
	attributeFlag                 // "Name" without "="
)

func cookieAttributeShown(attribute logic.CookieAttribute) attributeShown {
	switch {
	case !attribute.HasEquals:
		return attributeFlag
	case attribute.Value == "":
		return attributeEmpty
	default:
		return attributeValue
	}
}

// cookieAttributeGrid lists attributes by name and value. An attribute with
// "=" but no value shows "Empty value"; a flag without "=" shows a check.
func cookieAttributeGrid(c *ui.Context, theme nui.Theme, attributes []logic.CookieAttribute) {
	if len(attributes) == 0 {
		ui.Text(c.Key("no-attributes"), locales.DetailCookieNoAttributes()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).MarginY(0)
		ui.Box(c).Height(12)
		return
	}
	ui.Column(c.Key("attributes")).FillWidth().Gap(8).Children(func() {
		for position, attribute := range attributes {
			ui.Row(c.Key(position)).FillWidth().Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, cookieName(attribute.Name)).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).Selectable()
				ui.Row(c.Key("value")).Grow(2).Basis(0).MinWidth(0).Children(func() {
					switch cookieAttributeShown(attribute) {
					case attributeFlag:
						ui.Icon(c, lucide.Check()).Size(14, 14).TextColor(theme.Text)
					case attributeEmpty:
						ui.Text(c, locales.DetailCookieEmptyValue()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
					default:
						ui.Text(c, attribute.Value).FillWidth().FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Text).Selectable()
					}
				})
			})
		}
	})
	ui.Box(c).Height(12)
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
