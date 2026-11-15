package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	desktoptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// requestLineHeight is the height of the controls of a request line.
const requestLineHeight float32 = 36

// requestLine is the strip above an editor's panes.
func requestLine(c *ui.Context, theme nui.Theme, key string, children func()) {
	row := ui.Row(c.Key(key)).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Padding(8, 10).Background(theme.MutedBackground)
	row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(children)
}

// requestLineBox is a bordered group of a request line.
func requestLineBox(c *ui.Context, theme nui.Theme, key string) ui.Element {
	return ui.Row(c.Key(key)).Height(requestLineHeight).Shrink(0).AlignItems(ui.Center).Radius(4).Border(1, theme.Border).Background(theme.Background).Clip()
}

// urlTextRanges colors the scheme, host, path, query and hash of a URL as it
// is edited.
func urlTextRanges(raw string, theme nui.Theme) []ui.TextRange {
	colors := desktoptheme.URLPalette(theme.Dark)
	parsed := logic.ParseHighlightedURL(raw)
	offset := utf8.RuneCountInString(raw) - utf8.RuneCountInString(strings.TrimLeft(raw, " \t\r\n\v\f"))
	var ranges []ui.TextRange
	add := func(text string, color ui.Color) {
		length := utf8.RuneCountInString(text)
		if length > 0 {
			ranges = append(ranges, ui.TextRange{Start: offset, End: offset + length, Color: color})
		}
		offset += length
	}
	add(parsed.Scheme, theme.Muted)
	add(parsed.Host, colors.Host)
	add(parsed.Path, colors.Path)
	if parsed.HasQuery {
		add("?", colors.QueryPunct)
		for index, item := range parsed.QueryItems {
			if index > 0 {
				add("&", colors.QueryPunct)
			}
			add(item.Key, colors.QueryKey)
			if item.HasEquals {
				add("=", colors.QueryPunct)
				add(item.Value, colors.QueryValue)
			}
		}
	}
	add(parsed.Hash, colors.Hash)
	return ranges
}

// urlField is the URL group of a request line: an optional method before
// the URL, which shows its parts in color, and the MITM proxy toggle after it.
type urlField struct {
	url         *string
	label       string
	placeholder string
	method      func()
	mitm        bool
	toggleMITM  func()
	submit      func(string)
}

func (f urlField) build(c *ui.Context, theme nui.Theme) {
	requestLineBox(c, theme, "url-group").Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
		if f.method != nil {
			f.method()
		}
		ui.Row(c.Key("url")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Padding(0, 5, 0, 0).Children(func() {
			ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				nui.Input(c, theme, f.url, nui.InputOptions{
					Label: f.label, Placeholder: f.placeholder, Variant: nui.None, Size: nui.MD, OnSubmit: f.submit,
					UI: nui.Styles{nui.Root: {PaddingX: nui.Set[float32](12)}},
					Style: func(_, editor, _, _ ui.Element) {
						editor.FontSize(desktoptheme.TextSize(c, 15)).TextRanges(urlTextRanges(*f.url, theme)...)
					},
				}).FillWidth()
			})
			mitmButton(c, theme, f.mitm, f.toggleMITM)
		})
	})
}

// methodSelect is the method chooser at the start of the URL group, tinted
// with the method's color.
func methodSelect(c *ui.Context, theme nui.Theme, method *string) {
	color := desktoptheme.MethodColor(*method)
	box := ui.Row(c.Key("method")).Width(112).Shrink(0).AlignItems(ui.Center).Background(color.Alpha(0.05).Over(theme.Background))
	box.BorderWidth(0, 1, 0, 0).BorderColor(color.Alpha(0.22).Over(theme.Border))
	box.Children(func() {
		options := make([]nui.Option[string], 0, len(requestMethods)+1)
		for _, value := range requestMethods {
			options = append(options, nui.Option[string]{Value: value, Label: value})
		}
		if !containsString(requestMethods, *method) {
			options = append(options, nui.Option[string]{Value: *method, Label: *method})
		}
		nui.Select(c, theme, method, options, nui.SelectOptions{
			Label: "Method", Size: nui.MD, Variant: nui.None,
			UI: nui.Styles{
				nui.Root:     {Foreground: nui.Set(color), Weight: nui.Set(700), PaddingX: nui.Set[float32](12)},
				nui.Trailing: {Foreground: nui.Set(color)},
			},
		}).FillWidth()
	})
}

// mitmButton toggles sending through FlowLens's MITM proxy, filled with the
// accent while on.
func mitmButton(c *ui.Context, theme nui.Theme, active bool, toggle func()) {
	label, icon := locales.WorkspaceHttpRequestEnableMitmProxy(), lucide.Shield()
	if active {
		label, icon = locales.WorkspaceHttpRequestDisableMitmProxy(), lucide.ShieldCheck()
	}
	button := ui.ButtonBase(c.Key("mitm")).Size(26, 26).Shrink(0).Radius(6).Center().Label(label).AccessSelected(active)
	color := theme.Muted
	switch {
	case active:
		button.Background(theme.Primary).Shadow(0, 4, 10, 0, theme.Primary.Alpha(0.24))
		color = theme.Inverted
	case button.Hovered() || button.FocusVisible():
		color = theme.Primary
	}
	if toggle != nil {
		button.OnClick(toggle)
	}
	button.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(color) })
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
}

// primaryAction is the fixed-width send or connect button of a request line.
func primaryAction(c *ui.Context, theme nui.Theme, label, tooltip string, icon *ui.SVG, color nui.Color, kbds []string, run func()) {
	button := nui.Button(c.Key("primary"), theme, label, nui.ButtonOptions{
		Color: color, Size: nui.MD, Leading: icon,
		UI: nui.Styles{nui.Root: {Radius: nui.Set[float32](4)}},
	}).Width(110).Height(requestLineHeight).Shrink(0).Justify(ui.Center)
	button.OnClick(run)
	nui.Tooltip(c, theme, button, tooltip, nui.TooltipOptions{Kbds: kbds})
}
