package capture

import (
	"slices"

	"github.com/egoist/mygo/ui"
	desktoptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// cellPaddingX is the table's horizontal cell padding, Space(2.5).
const cellPaddingX float32 = 10

func trafficStripeColor(theme nui.Theme) ui.Color {
	return theme.Accented.Alpha(0.4)
}

// headerBackground is the header band's color, and its color under the
// pointer.
func headerBackground(theme nui.Theme, hovered bool) ui.Color {
	if hovered {
		return theme.Accented
	}
	return theme.Elevated
}

// headerCell draws the traffic header's band, title, and sort arrow. The
// overlay lets input through to MyGo's sorting, moving, and resizing controls.
func (t *Table) headerCell(c *ui.Context, theme nui.Theme, visible []columnDefinition) func(cell ui.Element, col int) {
	return func(cell ui.Element, col int) {
		if col == len(visible) {
			cell.Background(headerBackground(theme, false)).Role(ui.RoleNone)
			return
		}
		if col < 0 || col >= len(visible) {
			return
		}
		column := visible[col]
		bounds := cell.Bounds()
		if bounds.H > 0 {
			t.headerBottom = bounds.Y + bounds.H
		}
		overlay := ui.Row(c.Key("header-face")).Absolute().Top(0).Bottom(0).Left(0).Right(0).IgnoreInput(true).
			AlignItems(ui.Center).PaddingX(cellPaddingX).Background(headerBackground(theme, cell.Hovered()))
		overlay.Children(func() {
			ui.Text(c, column.title()).SingleLine().Shrink(1).MinWidth(0).FontSize(desktoptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
			if t.state.Sort.Column == string(column.key) {
				icon := lucide.ArrowUp()
				if t.state.Sort.Descending {
					icon = lucide.ArrowDown()
				}
				ui.Icon(c.Key("sort-arrow"), icon).Size(12, 12).Shrink(0).MarginStart(4).TextColor(theme.Primary).Label("sort-arrow")
			}
		})
	}
}

// displayOrder returns the visible columns' keys in the order the table shows
// them.
func (t *Table) displayOrder(visible []columnDefinition) []string {
	shown := make(map[string]bool, len(visible))
	for _, column := range visible {
		shown[string(column.key)] = true
	}
	order := make([]string, 0, len(visible))
	for _, key := range t.state.List.Columns.Order {
		if shown[key] && !slices.Contains(order, key) {
			order = append(order, key)
		}
	}
	for _, column := range visible {
		if !slices.Contains(order, string(column.key)) {
			order = append(order, string(column.key))
		}
	}
	return order
}

// rowOutline draws the 1 px outline of a marked row along one cell: its top
// and bottom, and its start or end on the row's first or last cell.
func rowOutline(c *ui.Context, color ui.Color, first, last bool) {
	left, right := float32(0), float32(0)
	if first {
		left = 1
	}
	if last {
		right = 1
	}
	ui.Box(c.Key("outline")).Absolute().Top(0).Bottom(0).Left(0).Right(0).IgnoreInput(true).
		BorderWidth(1, right, 1, left).BorderColor(color).Label("row-outline")
}

// Badge tones of 1.0.5's badgeToneClass, by status class.
type badgeTone struct {
	text       ui.Color
	background ui.Color // over the panel; Transparent for the control color
}

var (
	toneSuccess = badgeTone{ui.Hex("#16a34a"), ui.RGBA(34, 197, 94, 0.14)}
	toneInfo    = badgeTone{ui.Hex("#0284c7"), ui.RGBA(14, 165, 233, 0.14)}
	toneWarning = badgeTone{ui.Hex("#b45309"), ui.RGBA(245, 158, 11, 0.16)}
	toneError   = badgeTone{ui.Hex("#dc2626"), ui.RGBA(239, 68, 68, 0.14)}
)

func statusTone(status int) (badgeTone, bool) {
	switch {
	case status >= 500:
		return toneError, true
	case status >= 400:
		return toneWarning, true
	case status >= 300:
		return toneInfo, true
	case status >= 200:
		return toneSuccess, true
	default:
		return badgeTone{}, false
	}
}

// methodTone is a method's color on a 13% tint of it over the panel, as
// 1.0.5's color-mix(in srgb, color 13%, var(--app-panel-bg)).
func methodTone(method string) badgeTone {
	color := desktoptheme.MethodColor(method)
	return badgeTone{text: color, background: color.Alpha(0.13)}
}

// badge draws a pill whose colors are opaque against the panel, so a chosen,
// inspected, highlighted, or striped row does not change them.
func badge(c *ui.Context, theme nui.Theme, text string, tone badgeTone) ui.Element {
	background, color := theme.Elevated, tone.text
	if tone.background != ui.Transparent {
		background = tone.background.Over(theme.Background)
	}
	if color == ui.Transparent {
		color = theme.Muted
	}
	pill := ui.Box(c).Shrink(0).PaddingX(8).MinWidth(36).MinHeight(20).Radius(10).Center().
		Background(background).Border(1, color.Alpha(0.1).Over(background))
	pill.Children(func() {
		ui.Text(c, text).SingleLine().FontSize(desktoptheme.TextSize(c, 12)).Bold().TextColor(color)
	})
	return pill
}
