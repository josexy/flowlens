package capture

import (
	"slices"

	"github.com/egoist/mygo/ui"
	desktoptheme "github.com/josexy/flowlens/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// The traffic table follows FlowLens 1.0.5: a 32 px header band on the
// elevated background, semibold titles in the primary text color, no
// dividers between titles, an accent sort arrow, and a resize strip shown in
// the accent color only while hovered.
const (
	// headerResizeStrip is how wide the hovered resize strip is drawn, as
	// 1.0.5's w-1 handle.
	headerResizeStrip float32 = 4
	// headerResizeGrip is the width at the header cell's end where the
	// table takes a drag as a resize: the table's Space(2).
	headerResizeGrip float32 = 8
	// cellPaddingX is the table's horizontal cell padding, Space(2.5).
	cellPaddingX float32 = 10
)

// tableTheme is the table's theme: chosen rows take a tint of the accent,
// keeping their text and badges in their own colors. Its Border is clear, so
// the table draws no dividers between titles and no resize edge at rest; the
// header cells draw what 1.0.5 showed instead (headerCell).
func tableTheme(theme nui.Theme) *ui.Theme {
	native := theme.Native()
	native.Background = theme.Background
	chosen := selectedRowColor(theme)
	native.Accent, native.InactiveAccent, native.AccentText = chosen, chosen, theme.Text
	native.Border = ui.Transparent
	return native
}

// selectedRowColor is 1.0.5's --app-accent-selected: the primary color at 12%
// over the accented background.
func selectedRowColor(theme nui.Theme) ui.Color {
	return theme.Primary.Alpha(0.12).Over(theme.Accented)
}

// headerBackground is the header band's color, and its color under the
// pointer.
func headerBackground(theme nui.Theme, hovered bool) ui.Color {
	if hovered {
		return theme.Accented
	}
	return theme.Elevated
}

// headerEdgeColor is the resize strip's color while hovered: the accent at
// 65%, as 1.0.5's hover:bg-app-accent hover:opacity-[0.65].
func headerEdgeColor(theme nui.Theme) ui.Color {
	return theme.Primary.Alpha(0.65).Over(headerBackground(theme, true))
}

// headerCell draws a column's header over the one the table builds: the
// table's title, arrow, and edge colors are fixed to its theme, which also
// colors chosen rows. It covers the cell, so the table's own title and arrow
// do not show, and lets input through to the table, which still sorts,
// moves, and resizes.
func (t *Table) headerCell(c *ui.Context, theme nui.Theme, visible []columnDefinition) func(cell ui.Element, col int) {
	return func(cell ui.Element, col int) {
		if col < 0 || col >= len(visible) {
			return
		}
		column := visible[col]
		x, _, over := cell.PointerPosition()
		bounds := cell.Bounds()
		if bounds.H > 0 {
			t.headerBottom = bounds.Y + bounds.H
		}
		resizable := t.resizable(column, visible)
		onEdge := resizable && over && x >= bounds.W-headerResizeGrip
		overlay := ui.Row(c.Key("header-face")).Absolute().Top(0).Bottom(0).Left(0).Right(0).IgnoreInput(true).
			AlignItems(ui.Center).PaddingX(cellPaddingX).Background(headerBackground(theme, cell.Hovered() && !onEdge))
		overlay.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
		overlay.Children(func() {
			ui.Text(c, column.title()).SingleLine().Shrink(1).MinWidth(0).FontSize(desktoptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
			if t.sort.Column == string(column.key) {
				icon := lucide.ArrowUp()
				if t.sort.Descending {
					icon = lucide.ArrowDown()
				}
				ui.Icon(c.Key("sort-arrow"), icon).Size(12, 12).Shrink(0).MarginStart(4).TextColor(theme.Primary).Label("sort-arrow")
			}
			// Keep conditional decoration inside this stable overlay. MyGo
			// appends an unkeyed resize grip after HeaderCell; adding/removing
			// its siblings changes its identity and cancels the captured drag.
			if onEdge {
				ui.Box(c.Key("resize-strip")).Absolute().Top(0).Bottom(1).Right(0).Width(headerResizeStrip).ZIndex(1).IgnoreInput(true).
					Background(headerEdgeColor(theme)).Label("resize-strip")
			}
		})
	}
}

// resizable reports whether the table gives column a resize edge: every
// column but the last when it shares the room left.
func (t *Table) resizable(column columnDefinition, visible []columnDefinition) bool {
	if _, sized := t.list.Columns.Widths[string(column.key)]; sized || column.width > 0 {
		return true
	}
	order := t.displayOrder(visible)
	return len(order) == 0 || order[len(order)-1] != string(column.key)
}

// displayOrder returns the visible columns' keys in the order the table shows
// them.
func (t *Table) displayOrder(visible []columnDefinition) []string {
	shown := make(map[string]bool, len(visible))
	for _, column := range visible {
		shown[string(column.key)] = true
	}
	order := make([]string, 0, len(visible))
	for _, key := range t.list.Columns.Order {
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
